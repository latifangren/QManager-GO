package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"qmanager/internal/dpi"
)

var (
	dpiConfigFile     = "/etc/qmanager/dpi_config.json"
	dpiHostlistFile   = "/etc/qmanager/dpi_hostlist.txt"
	dpiVerifyFile     = "/tmp/qmanager_dpi_verify.json"
	dpiInstallFile    = "/tmp/qmanager_dpi_install.json"
	dpiInstallPidFile = "/tmp/qmanager_dpi_install.pid"
)

// VideoOptimizerHandler handles DPI bypass / Traffic Engine / Video Optimizer.
type VideoOptimizerHandler struct {
	mu sync.Mutex
}

// NewVideoOptimizerHandler creates a VideoOptimizerHandler.
func NewVideoOptimizerHandler() *VideoOptimizerHandler {
	return &VideoOptimizerHandler{}
}

// TrafficEngineConfig represents stored engine state.
type TrafficEngineConfig = dpi.Config

func readDpiConfig() TrafficEngineConfig {
	dpi.DPIConfigFile = dpiConfigFile
	return dpi.ReadConfig()
}

func writeDpiConfig(c TrafficEngineConfig) error {
	dpi.DPIConfigFile = dpiConfigFile
	return dpi.WriteConfig(c)
}

func readHostlistDomains() []string {
	dpi.DPIHostlistFile = dpiHostlistFile
	return dpi.ReadHostlist()
}

func countHostlistDomains() int {
	return len(readHostlistDomains())
}

// HandleGet handles GET /api/v1/network/traffic-engine, /api/v1/network/video-optimizer, and /cgi-bin/quecmanager/network/video_optimizer.sh
func (h *VideoOptimizerHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	section := r.URL.Query().Get("section")

	switch action {
	case "verify_status":
		h.getVerifyStatus(w)
		return
	case "install_status":
		h.getInstallStatus(w)
		return
	case "hostlist":
		h.getHostlist(w)
		return
	case "hostlist_section":
		h.getHostlistSection(w)
		return
	}

	if section == "hostlist" {
		h.getHostlistSection(w)
		return
	}

	// Status response
	cfg := readDpiConfig()
	isFullBypass := section == "full_bypass" || section == "masquerade"

	enabled := cfg.VideoOptimizerEnabled
	if isFullBypass {
		enabled = cfg.MasqueradeEnabled
	}

	mgr := dpi.GetManager()
	isRunning := mgr.IsRunning()
	engineEnabled := cfg.VideoOptimizerEnabled || cfg.MasqueradeEnabled
	status := "stopped"
	if engineEnabled {
		if isRunning {
			status = "running"
		} else {
			status = "error"
		}
	}

	uptime := mgr.Uptime()
	pkts := mgr.GetPacketsProcessed()
	domainsLoaded := countHostlistDomains()

	resp := map[string]interface{}{
		"success":              true,
		"enabled":              enabled,
		"status":               status,
		"uptime":               uptime,
		"packets_processed":    pkts,
		"domains_loaded":       domainsLoaded,
		"binary_installed":     true, // Embedded tpws is always ready
		"kernel_module_loaded": true,
		"force_tcp":            cfg.ForceTCP,
		"force_tcp_active":     dpi.IsForceTCPActive(),
	}

	if isFullBypass {
		sni := cfg.SNIDomain
		if sni == "" {
			sni = "speedtest.net"
		}
		resp["sni_domain"] = sni
	}

	JSON(w, http.StatusOK, resp)
}

// VideoOptimizerSavePayload represents the POST request body.
type VideoOptimizerSavePayload struct {
	Action    string   `json:"action"` // "save", "save_full_bypass", "save_masquerade", "save_force_tcp", "install", "uninstall", "verify", "save_hostlist", "restore_hostlist"
	Enabled   *bool    `json:"enabled,omitempty"`
	SNIDomain string   `json:"sni_domain,omitempty"`
	Domains   []string `json:"domains,omitempty"`
}

// HandlePost handles POST /api/v1/network/traffic-engine, /api/v1/network/video-optimizer, and /cgi-bin/quecmanager/network/video_optimizer.sh
func (h *VideoOptimizerHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	var payload VideoOptimizerSavePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch payload.Action {
	case "save", "save_video_optimizer":
		h.handleSaveVideoOptimizer(w, payload)
	case "save_full_bypass", "save_masquerade":
		h.handleSaveFullBypass(w, payload)
	case "save_force_tcp":
		h.handleSaveForceTCP(w, payload)
	case "save_hostlist":
		h.handleSaveHostlist(w, payload)
	case "restore_hostlist":
		h.handleRestoreHostlist(w)
	case "install":
		h.handleInstall(w)
	case "uninstall":
		h.handleUninstall(w)
	case "verify":
		h.handleVerify(w)
	default:
		Error(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s", payload.Action))
	}
}

func (h *VideoOptimizerHandler) handleSaveVideoOptimizer(w http.ResponseWriter, p VideoOptimizerSavePayload) {
	enabled := false
	if p.Enabled != nil {
		enabled = *p.Enabled
	}

	cfg := readDpiConfig()
	cfg.VideoOptimizerEnabled = enabled
	if enabled {
		cfg.MasqueradeEnabled = false // Mutex
	}
	_ = writeDpiConfig(cfg)

	mgr := dpi.GetManager()
	if enabled {
		_ = mgr.StartEngine("video_optimizer")
	} else {
		mgr.StopEngine()
	}

	status := "stopped"
	if enabled && mgr.IsRunning() {
		status = "running"
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": enabled,
		"status":  status,
	})
}

func (h *VideoOptimizerHandler) handleSaveFullBypass(w http.ResponseWriter, p VideoOptimizerSavePayload) {
	enabled := false
	if p.Enabled != nil {
		enabled = *p.Enabled
	}

	cfg := readDpiConfig()
	cfg.MasqueradeEnabled = enabled
	if enabled {
		cfg.VideoOptimizerEnabled = false // Mutex
	}
	if p.SNIDomain != "" {
		cfg.SNIDomain = p.SNIDomain
	}
	_ = writeDpiConfig(cfg)

	mgr := dpi.GetManager()
	if enabled {
		_ = mgr.StartEngine("masquerade")
	} else {
		mgr.StopEngine()
	}

	status := "stopped"
	if enabled && mgr.IsRunning() {
		status = "running"
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"enabled":    enabled,
		"status":     status,
		"sni_domain": cfg.SNIDomain,
	})
}

func (h *VideoOptimizerHandler) handleSaveForceTCP(w http.ResponseWriter, p VideoOptimizerSavePayload) {
	enabled := false
	if p.Enabled != nil {
		enabled = *p.Enabled
	}

	cfg := readDpiConfig()
	cfg.ForceTCP = enabled
	_ = writeDpiConfig(cfg)

	if enabled {
		_ = dpi.ApplyForceTCPRule()
	} else {
		dpi.RemoveForceTCPRule()
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"force_tcp":        enabled,
		"force_tcp_active": dpi.IsForceTCPActive(),
	})
}

func (h *VideoOptimizerHandler) handleSaveHostlist(w http.ResponseWriter, p VideoOptimizerSavePayload) {
	dpi.DPIHostlistFile = dpiHostlistFile
	if err := dpi.WriteHostlist(p.Domains); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to write hostlist")
		return
	}

	cfg := readDpiConfig()
	if cfg.VideoOptimizerEnabled {
		_ = dpi.GetManager().StartEngine("video_optimizer")
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Hostlist saved",
	})
}

func (h *VideoOptimizerHandler) handleRestoreHostlist(w http.ResponseWriter) {
	dpi.DPIHostlistFile = dpiHostlistFile
	if err := dpi.WriteHostlist(dpi.DefaultHostlist); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to restore hostlist")
		return
	}

	cfg := readDpiConfig()
	if cfg.VideoOptimizerEnabled {
		_ = dpi.GetManager().StartEngine("video_optimizer")
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Hostlist restored to default",
	})
}

func (h *VideoOptimizerHandler) handleInstall(w http.ResponseWriter) {
	_ = dpi.GetManager().EnsureBinaryExtracted()
	_ = os.WriteFile(dpiInstallFile, []byte(`{"success":true,"status":"complete","message":"tpws ready"}`), 0644)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  "complete",
	})
}

func (h *VideoOptimizerHandler) handleUninstall(w http.ResponseWriter) {
	dpi.GetManager().StopEngine()
	_ = os.Remove(dpiConfigFile)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Traffic Engine uninstalled",
	})
}

func (h *VideoOptimizerHandler) handleVerify(w http.ResponseWriter) {
	dpi.GetManager().StartVerify()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  "running",
		"message": "Verify started",
	})
}

func (h *VideoOptimizerHandler) getVerifyStatus(w http.ResponseWriter) {
	data, err := os.ReadFile(dpiVerifyFile)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  "idle",
			"message": "No verification run",
		})
		return
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  "idle",
			"message": "No verification run",
		})
		return
	}

	if _, ok := resp["success"]; !ok {
		resp["success"] = true
	}

	JSON(w, http.StatusOK, resp)
}

func (h *VideoOptimizerHandler) getInstallStatus(w http.ResponseWriter) {
	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  "complete",
		"message": "tpws binary embedded and ready",
	})
}

func (h *VideoOptimizerHandler) getHostlist(w http.ResponseWriter) {
	domains := readHostlistDomains()
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(strings.Join(domains, "\n")))
}

func (h *VideoOptimizerHandler) getHostlistSection(w http.ResponseWriter) {
	domains := readHostlistDomains()
	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"domains": domains,
		"count":   len(domains),
	})
}
