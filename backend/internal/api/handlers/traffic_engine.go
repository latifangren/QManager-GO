package handlers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	dnsmasqConfPath = "/etc/data/dnsmasq.conf"
	customDNSConfig = "/etc/qmanager/custom_dns.json"
	sentinelBegin   = "# QMANAGER-CUSTOM-DNS-BEGIN v1"
	sentinelEnd     = "# QMANAGER-CUSTOM-DNS-END v1"
	maxDNSServers   = 4
)

// CustomDNSConfig represents stored DNS configuration.
type CustomDNSConfig struct {
	Enabled       bool     `json:"enabled"`
	Servers       []string `json:"servers"`
	IgnoreCarrier bool     `json:"ignore_carrier"`
}

// CustomDNSHandler manages custom upstream DNS servers.
type CustomDNSHandler struct{}

// NewCustomDNSHandler creates a CustomDNSHandler.
func NewCustomDNSHandler() *CustomDNSHandler {
	return &CustomDNSHandler{}
}

// HandleGet handles GET /api/v1/network/dns and /cgi-bin/quecmanager/network/custom_dns.sh
func (h *CustomDNSHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	cfg := readCustomDNSConfig()

	// Detect if dnsmasq exists
	dnsmasqAvailable := true
	if _, err := os.Stat("/etc/data/dnsmasq.conf"); os.IsNotExist(err) {
		if _, err := exec.LookPath("dnsmasq"); err != nil {
			dnsmasqAvailable = false
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"enabled":            cfg.Enabled,
		"servers":            cfg.Servers,
		"ignore_carrier":     cfg.IgnoreCarrier,
		"dnsmasq_available":  dnsmasqAvailable,
		"dns_mode":           "LOCAL",
		"passthrough_bypass": false,
	})
}

// CustomDNSSavePayload represents the save payload.
type CustomDNSSavePayload struct {
	Action        string   `json:"action"` // "save" or "clear"
	Enabled       *bool    `json:"enabled,omitempty"`
	Servers       []string `json:"servers,omitempty"`
	IgnoreCarrier *bool    `json:"ignore_carrier,omitempty"`
}

// HandlePost handles POST /api/v1/network/dns and /cgi-bin/quecmanager/network/custom_dns.sh
func (h *CustomDNSHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	var payload CustomDNSSavePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if payload.Action == "clear" {
		cfg := CustomDNSConfig{
			Enabled:       false,
			Servers:       []string{},
			IgnoreCarrier: false,
		}
		_ = writeCustomDNSConfig(cfg)
		_ = updateDnsmasqConf(cfg)
		Success(w, map[string]string{"message": "Custom DNS cleared"})
		return
	}

	enabled := false
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	ignoreCarrier := false
	if payload.IgnoreCarrier != nil {
		ignoreCarrier = *payload.IgnoreCarrier
	}

	var validServers []string
	if enabled {
		if len(payload.Servers) == 0 {
			Error(w, http.StatusBadRequest, "At least one DNS server is required when enabled")
			return
		}
		if len(payload.Servers) > maxDNSServers {
			Error(w, http.StatusBadRequest, fmt.Sprintf("Maximum %d DNS servers allowed", maxDNSServers))
			return
		}

		for _, s := range payload.Servers {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			ip := net.ParseIP(s)
			if ip == nil {
				Error(w, http.StatusBadRequest, fmt.Sprintf("Invalid IP address: %s", s))
				return
			}
			validServers = append(validServers, s)
		}
	}

	cfg := CustomDNSConfig{
		Enabled:       enabled,
		Servers:       validServers,
		IgnoreCarrier: ignoreCarrier,
	}

	if err := writeCustomDNSConfig(cfg); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to persist DNS config")
		return
	}

	if err := updateDnsmasqConf(cfg); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update dnsmasq.conf: %v", err))
		return
	}

	Success(w, map[string]interface{}{
		"message": "Custom DNS applied successfully",
		"enabled": enabled,
		"servers": validServers,
	})
}

func updateDnsmasqConf(cfg CustomDNSConfig) error {
	existingData, _ := os.ReadFile(dnsmasqConfPath)
	content := string(existingData)

	// Strip existing sentinel block if present
	if strings.Contains(content, sentinelBegin) && strings.Contains(content, sentinelEnd) {
		startIdx := strings.Index(content, sentinelBegin)
		endIdx := strings.Index(content, sentinelEnd) + len(sentinelEnd)
		content = strings.TrimSpace(content[:startIdx] + content[endIdx:])
	}

	if cfg.Enabled && len(cfg.Servers) > 0 {
		var block strings.Builder
		block.WriteString("\n\n" + sentinelBegin + "\n")
		if cfg.IgnoreCarrier {
			block.WriteString("no-resolv\n")
		}
		for _, s := range cfg.Servers {
			block.WriteString(fmt.Sprintf("server=%s\n", s))
		}
		block.WriteString(sentinelEnd + "\n")
		content += block.String()
	}

	_ = os.MkdirAll(filepath.Dir(dnsmasqConfPath), 0755)
	err := os.WriteFile(dnsmasqConfPath, []byte(content), 0644)
	if err != nil {
		return err
	}

	// Reload dnsmasq
	_ = exec.Command("killall", "-HUP", "dnsmasq").Run()
	return nil
}

func readCustomDNSConfig() CustomDNSConfig {
	data, err := os.ReadFile(customDNSConfig)
	if err != nil {
		return CustomDNSConfig{
			Enabled:       false,
			Servers:       []string{},
			IgnoreCarrier: false,
		}
	}
	var c CustomDNSConfig
	_ = json.Unmarshal(data, &c)
	return c
}

func writeCustomDNSConfig(c CustomDNSConfig) error {
	_ = os.MkdirAll(filepath.Dir(customDNSConfig), 0755)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(customDNSConfig, data, 0644)
}
