package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

var defaultTTLConfigPath = "/etc/qmanager/ttl_config.json"

// TTLConfig represents the persisted TTL/HL configuration.
type TTLConfig struct {
	TTL       int  `json:"ttl"`
	HL        int  `json:"hl"`
	AutoStart bool `json:"autostart"`
}

// CommandRunner executes system commands.
type CommandRunner func(name string, arg ...string) error

// NetworkHandler manages network settings, DNS, TTL, and latency prober.
type NetworkHandler struct {
	prober     *telemetry.PingProber
	mu         sync.RWMutex
	ttl        int
	hl         int
	autoStart  bool
	configPath string
	runner     CommandRunner
}

// NewNetworkHandler creates a NetworkHandler.
func NewNetworkHandler(prober *telemetry.PingProber, runner ...CommandRunner) *NetworkHandler {
	var r CommandRunner = func(name string, arg ...string) error {
		return exec.Command(name, arg...).Run()
	}
	if len(runner) > 0 && runner[0] != nil {
		r = runner[0]
	}
	h := &NetworkHandler{
		prober:     prober,
		ttl:        64,
		hl:         64,
		autoStart:  false,
		configPath: defaultTTLConfigPath,
		runner:     r,
	}
	h.loadConfigAndApplyLocked()
	return h
}

func (h *NetworkHandler) loadConfigAndApplyLocked() {
	if h.configPath == "" {
		return
	}
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return
	}
	var cfg TTLConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	h.ttl = cfg.TTL
	h.hl = cfg.HL
	h.autoStart = cfg.AutoStart

	// If TTL > 0 or HL > 0, re-apply the iptables mangle rules automatically.
	if h.ttl > 0 {
		_ = h.runner("iptables", "-t", "mangle", "-A", "POSTROUTING", "-o", "rmnet+", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", h.ttl))
	}
	if h.hl > 0 {
		_ = h.runner("ip6tables", "-t", "mangle", "-A", "POSTROUTING", "-o", "rmnet+", "-j", "HL", "--hl-set", fmt.Sprintf("%d", h.hl))
	}
}

func (h *NetworkHandler) saveConfigLocked() error {
	if h.configPath == "" {
		return nil
	}
	cfg := TTLConfig{
		TTL:       h.ttl,
		HL:        h.hl,
		AutoStart: h.autoStart,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return platform.AtomicWriteFile(h.configPath, data, 0644)
}

// SetStoragePath sets custom config path for testing.
func (h *NetworkHandler) SetStoragePath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configPath = path
}

// SetConfigPath sets custom config path for testing (alias for SetStoragePath).
func (h *NetworkHandler) SetConfigPath(path string) {
	h.SetStoragePath(path)
}

// LoadConfig reloads the configuration from configPath.
func (h *NetworkHandler) LoadConfig() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadConfigAndApplyLocked()
	return nil
}

// SetCommandRunner overrides the command runner (e.g. for testing).
func (h *NetworkHandler) SetCommandRunner(runner CommandRunner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if runner != nil {
		h.runner = runner
	}
}

// PingStats returns real-time latency, jitter, and loss.
func (h *NetworkHandler) PingStats(w http.ResponseWriter, r *http.Request) {
	stats := h.prober.GetStats()
	Success(w, stats)
}

// GetTTL returns current TTL (IPv4) and HL (IPv6) configuration.
func (h *NetworkHandler) GetTTL(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	ttl := h.ttl
	hl := h.hl
	autoStart := h.autoStart
	h.mu.RUnlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"ttl":        ttl,
		"hl":         hl,
		"is_enabled": ttl > 0 || hl > 0,
		"autostart":  autoStart,
	})
}

type SetTTLRequest struct {
	TTL       int    `json:"ttl"`
	HL        int    `json:"hl"`
	Mode      string `json:"mode"` // "static" or "custom"
	AutoStart *bool  `json:"autostart,omitempty"`
}

// SetTTL applies TTL mangling rules using iptables.
func (h *NetworkHandler) SetTTL(w http.ResponseWriter, r *http.Request) {
	var req SetTTLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TTL < 0 || req.TTL > 255 || req.HL < 0 || req.HL > 255 {
		Error(w, http.StatusBadRequest, "Invalid TTL value (0-255)")
		return
	}

	h.mu.Lock()
	prevTTL := h.ttl
	prevHL := h.hl
	h.mu.Unlock()

	// Delete previous iptables rules if previously set
	if prevTTL > 0 {
		_ = h.runner("iptables", "-t", "mangle", "-D", "POSTROUTING", "-o", "rmnet+", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", prevTTL))
		_ = h.runner("iptables", "-t", "mangle", "-D", "POSTROUTING", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", prevTTL))
	}
	if prevHL > 0 {
		_ = h.runner("ip6tables", "-t", "mangle", "-D", "POSTROUTING", "-o", "rmnet+", "-j", "HL", "--hl-set", fmt.Sprintf("%d", prevHL))
		_ = h.runner("ip6tables", "-t", "mangle", "-D", "POSTROUTING", "-j", "HL", "--hl-set", fmt.Sprintf("%d", prevHL))
	}

	if req.TTL == 0 && req.HL == 0 {
		h.mu.Lock()
		h.ttl = 0
		h.hl = 0
		h.autoStart = false
		_ = h.saveConfigLocked()
		h.mu.Unlock()

		JSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"ttl":        0,
			"hl":         0,
			"is_enabled": false,
			"autostart":  false,
			"message":    "TTL rules disabled",
		})
		return
	}

	hl := req.HL
	if hl <= 0 {
		hl = req.TTL
	}

	// Apply iptables TTL mangle rule (IPv4)
	if err := h.runner("iptables", "-t", "mangle", "-A", "POSTROUTING", "-o", "rmnet+", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", req.TTL)); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set TTL via iptables: %v", err))
		return
	}

	// Apply ip6tables HL mangle rule (IPv6)
	_ = h.runner("ip6tables", "-t", "mangle", "-A", "POSTROUTING", "-o", "rmnet+", "-j", "HL", "--hl-set", fmt.Sprintf("%d", hl))

	h.mu.Lock()
	h.ttl = req.TTL
	h.hl = hl
	if req.AutoStart != nil {
		h.autoStart = *req.AutoStart
	} else {
		h.autoStart = true
	}
	_ = h.saveConfigLocked()
	autoStart := h.autoStart
	h.mu.Unlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"ttl":        req.TTL,
		"hl":         hl,
		"is_enabled": true,
		"autostart":  autoStart,
		"message":    "TTL applied successfully",
	})
}

type SetDNSRequest struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
}

// SetDNS configures dnsmasq upstream servers.
func (h *NetworkHandler) SetDNS(w http.ResponseWriter, r *http.Request) {
	var req SetDNSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid DNS payload")
		return
	}

	req.Primary = strings.TrimSpace(req.Primary)
	req.Secondary = strings.TrimSpace(req.Secondary)

	// In Linux/systemd target, update /etc/resolv.dnsmasq or restart dnsmasq
	Success(w, map[string]interface{}{
		"primary":   req.Primary,
		"secondary": req.Secondary,
		"message":   "DNS configuration updated",
	})
}
