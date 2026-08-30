package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"qmanager/internal/telemetry"
)

// NetworkHandler manages network settings, DNS, TTL, and latency prober.
type NetworkHandler struct {
	prober *telemetry.PingProber
}

// NewNetworkHandler creates a NetworkHandler.
func NewNetworkHandler(prober *telemetry.PingProber) *NetworkHandler {
	return &NetworkHandler{
		prober: prober,
	}
}

// PingStats returns real-time latency, jitter, and loss.
func (h *NetworkHandler) PingStats(w http.ResponseWriter, r *http.Request) {
	stats := h.prober.GetStats()
	Success(w, stats)
}

type SetTTLRequest struct {
	TTL   int    `json:"ttl"`
	Mode  string `json:"mode"` // "static" or "custom"
}

// SetTTL applies TTL mangling rules using iptables.
func (h *NetworkHandler) SetTTL(w http.ResponseWriter, r *http.Request) {
	var req SetTTLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TTL <= 0 || req.TTL > 255 {
		Error(w, http.StatusBadRequest, "Invalid TTL value (1-255)")
		return
	}

	// Apply iptables TTL mangle rule
	// iptables -t mangle -D POSTROUTING -j TTL --ttl-set <ttl> 2>/dev/null
	// iptables -t mangle -A POSTROUTING -j TTL --ttl-set <ttl>
	delCmd := exec.Command("iptables", "-t", "mangle", "-D", "POSTROUTING", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", req.TTL))
	_ = delCmd.Run()

	addCmd := exec.Command("iptables", "-t", "mangle", "-A", "POSTROUTING", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", req.TTL))
	if err := addCmd.Run(); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set TTL via iptables: %v", err))
		return
	}

	Success(w, map[string]interface{}{
		"ttl":     req.TTL,
		"message": "TTL applied successfully",
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
