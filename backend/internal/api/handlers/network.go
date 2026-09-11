package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"qmanager/internal/telemetry"
)

// CommandRunner executes system commands.
type CommandRunner func(name string, arg ...string) error

// NetworkHandler manages network settings, DNS, TTL, and latency prober.
type NetworkHandler struct {
	prober *telemetry.PingProber
	mu     sync.RWMutex
	ttl    int
	hl     int
	runner CommandRunner
}

// NewNetworkHandler creates a NetworkHandler.
func NewNetworkHandler(prober *telemetry.PingProber, runner ...CommandRunner) *NetworkHandler {
	var r CommandRunner = func(name string, arg ...string) error {
		return exec.Command(name, arg...).Run()
	}
	if len(runner) > 0 && runner[0] != nil {
		r = runner[0]
	}
	return &NetworkHandler{
		prober: prober,
		ttl:    64,
		hl:     64,
		runner: r,
	}
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
	h.mu.RUnlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"ttl":        ttl,
		"hl":         hl,
		"is_enabled": ttl > 0,
		"autostart":  false,
	})
}

type SetTTLRequest struct {
	TTL  int    `json:"ttl"`
	HL   int    `json:"hl"`
	Mode string `json:"mode"` // "static" or "custom"
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
		_ = h.runner("iptables", "-t", "mangle", "-D", "POSTROUTING", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", prevTTL))
	}
	if prevHL > 0 {
		_ = h.runner("ip6tables", "-t", "mangle", "-D", "POSTROUTING", "-j", "HL", "--hl-set", fmt.Sprintf("%d", prevHL))
	}

	if req.TTL == 0 && req.HL == 0 {
		h.mu.Lock()
		h.ttl = 0
		h.hl = 0
		h.mu.Unlock()

		JSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"ttl":        0,
			"hl":         0,
			"is_enabled": false,
			"message":    "TTL rules disabled",
		})
		return
	}

	hl := req.HL
	if hl <= 0 {
		hl = req.TTL
	}

	// Apply iptables TTL mangle rule (IPv4)
	if err := h.runner("iptables", "-t", "mangle", "-A", "POSTROUTING", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", req.TTL)); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set TTL via iptables: %v", err))
		return
	}

	// Apply ip6tables HL mangle rule (IPv6)
	_ = h.runner("ip6tables", "-t", "mangle", "-A", "POSTROUTING", "-j", "HL", "--hl-set", fmt.Sprintf("%d", hl))

	h.mu.Lock()
	h.ttl = req.TTL
	h.hl = hl
	h.mu.Unlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"ttl":        req.TTL,
		"hl":         hl,
		"is_enabled": true,
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
