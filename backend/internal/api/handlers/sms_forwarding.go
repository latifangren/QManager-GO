package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/telemetry"
)

// SMSForwardingResponse matches GET /cellular/sms_forwarding.sh response.
type SMSForwardingResponse struct {
	Success  bool                            `json:"success"`
	Settings telemetry.SMSForwardingSettings `json:"settings"`
	Failures []telemetry.SMSForwardingFailure `json:"failures"`
	Error    string                          `json:"error,omitempty"`
	Detail   string                          `json:"detail,omitempty"`
}

// SMSForwardingHandler handles SMS forwarding settings and triggers.
type SMSForwardingHandler struct {
	engine       *atengine.Engine
	configMgr    *config.Manager
	configPath   string
	failuresPath string
	reloadFlag   string
	mu           sync.Mutex
}

// NewSMSForwardingHandler creates a new SMSForwardingHandler.
func NewSMSForwardingHandler(eng *atengine.Engine, cfgMgr *config.Manager) *SMSForwardingHandler {
	cfgPath := os.Getenv("SMS_FORWARD_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = telemetry.DefaultSMSForwardConfig
	}
	failPath := os.Getenv("SMS_FORWARD_FAILURES_PATH")
	if failPath == "" {
		failPath = telemetry.DefaultSMSForwardFailures
	}
	reload := os.Getenv("SMS_FORWARD_RELOAD_FLAG")
	if reload == "" {
		reload = telemetry.DefaultSMSForwardReload
	}

	return &SMSForwardingHandler{
		engine:       eng,
		configMgr:    cfgMgr,
		configPath:   cfgPath,
		failuresPath: failPath,
		reloadFlag:   reload,
	}
}

// GetSettings handles GET /cgi-bin/quecmanager/cellular/sms_forwarding.sh and GET /api/v1/cellular/sms/forwarding
func (h *SMSForwardingHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	settings := h.readSettings()
	failures := h.readFailures()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SMSForwardingResponse{
		Success:  true,
		Settings: settings,
		Failures: failures,
	})
}

// HandleAction handles POST /cgi-bin/quecmanager/cellular/sms_forwarding.sh and POST /api/v1/cellular/sms/forwarding
func (h *SMSForwardingHandler) HandleAction(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Action         string                        `json:"action"`
		Enabled        interface{}                   `json:"enabled"`
		TargetPhone    string                        `json:"target_phone"`
		EmailEnabled   *bool                         `json:"email_enabled"`
		EmailAddress   string                        `json:"email_address"`
		WebhookEnabled *bool                         `json:"webhook_enabled"`
		WebhookURL     string                        `json:"webhook_url"`
		KeywordFilter  string                        `json:"keyword_filter"`
		Rules          []telemetry.SMSForwardingRule `json:"rules"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch payload.Action {
	case "save_settings", "save":
		h.handleSaveSettings(w, payload.Enabled, payload.TargetPhone, payload.EmailEnabled, payload.EmailAddress, payload.WebhookEnabled, payload.WebhookURL, payload.KeywordFilter, payload.Rules)
	case "clear_failures", "clear":
		h.handleClearFailures(w)
	default:
		Error(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s", payload.Action))
	}
}

func (h *SMSForwardingHandler) handleSaveSettings(
	w http.ResponseWriter,
	enabledRaw interface{},
	targetPhone string,
	emailEnabled *bool,
	emailAddress string,
	webhookEnabled *bool,
	webhookURL string,
	keywordFilter string,
	rules []telemetry.SMSForwardingRule,
) {
	enabled := parseBoolFlexible(enabledRaw)

	if enabled && targetPhone != "" {
		if !telemetry.ValidateTargetPhone(targetPhone) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "invalid_target_phone",
				"detail":  "target phone must be a valid E.164-ish number (7-15 digits, non-zero first digit)",
			})
			return
		}
	}

	settings := h.readSettings()
	settings.Enabled = enabled
	settings.TargetPhone = strings.TrimSpace(targetPhone)
	if emailEnabled != nil {
		settings.EmailEnabled = *emailEnabled
	}
	if emailAddress != "" || emailEnabled != nil {
		settings.EmailAddress = strings.TrimSpace(emailAddress)
	}
	if webhookEnabled != nil {
		settings.WebhookEnabled = *webhookEnabled
	}
	if webhookURL != "" || webhookEnabled != nil {
		settings.WebhookURL = strings.TrimSpace(webhookURL)
	}
	if keywordFilter != "" {
		settings.KeywordFilter = strings.TrimSpace(keywordFilter)
	}
	if rules != nil {
		settings.Rules = rules
	}

	if err := h.writeSettings(settings); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "save_failed",
			"detail":  err.Error(),
		})
		return
	}

	// Notify background worker or systemd daemon
	h.notifyReloadOrToggleDaemon(settings.Enabled)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"settings": map[string]interface{}{
			"enabled":      settings.Enabled,
			"target_phone": settings.TargetPhone,
		},
	})
}

func (h *SMSForwardingHandler) handleClearFailures(w http.ResponseWriter) {
	_ = os.Remove(h.failuresPath)
	Success(w, map[string]interface{}{"success": true})
}

func (h *SMSForwardingHandler) readSettings() telemetry.SMSForwardingSettings {
	var s telemetry.SMSForwardingSettings
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return telemetry.SMSForwardingSettings{Enabled: false, TargetPhone: ""}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return telemetry.SMSForwardingSettings{Enabled: false, TargetPhone: ""}
	}
	return s
}

func (h *SMSForwardingHandler) writeSettings(s telemetry.SMSForwardingSettings) error {
	dir := filepath.Dir(h.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmpFile := h.configPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, h.configPath)
}

func (h *SMSForwardingHandler) readFailures() []telemetry.SMSForwardingFailure {
	var list []telemetry.SMSForwardingFailure
	data, err := os.ReadFile(h.failuresPath)
	if err != nil {
		return []telemetry.SMSForwardingFailure{}
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return []telemetry.SMSForwardingFailure{}
	}
	return list
}

func (h *SMSForwardingHandler) notifyReloadOrToggleDaemon(enabled bool) {
	// 1. Touch reload flag
	_ = os.WriteFile(h.reloadFlag, []byte("1"), 0644)

	// 2. Best-effort systemctl service toggle if systemctl is present
	if _, err := exec.LookPath("systemctl"); err == nil {
		if enabled {
			_ = exec.Command("systemctl", "enable", "--now", telemetry.DefaultSMSForwardUnitName).Run()
		} else {
			_ = exec.Command("systemctl", "stop", telemetry.DefaultSMSForwardUnitName).Run()
			_ = exec.Command("systemctl", "disable", telemetry.DefaultSMSForwardUnitName).Run()
		}
	}
}

func parseBoolFlexible(val interface{}) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "true" || v == "1" || v == "yes" || v == "on"
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return false
}
