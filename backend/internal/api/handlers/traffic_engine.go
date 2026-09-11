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
	"time"
)

const (
	sentinelBegin = "# QMANAGER-CUSTOM-DNS-BEGIN v1"
	sentinelEnd   = "# QMANAGER-CUSTOM-DNS-END v1"
	maxDNSServers = 4
)

var (
	dnsmasqConfPath = "/etc/data/dnsmasq.conf"
	customDNSConfig = "/etc/qmanager/custom_dns.json"
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
	resp := h.buildFullResponse()
	JSON(w, http.StatusOK, resp)
}

func (h *CustomDNSHandler) buildFullResponse() map[string]interface{} {
	cfg := readCustomDNSConfig()

	// Detect if dnsmasq exists
	dnsmasqAvailable := true
	if _, err := os.Stat("/etc/data/dnsmasq.conf"); os.IsNotExist(err) {
		if _, err := exec.LookPath("dnsmasq"); err != nil {
			dnsmasqAvailable = false
		}
	}

	// Read carrier resolv.conf if available
	carrierServers := make([]string, 0)
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "nameserver") {
				parts := strings.Fields(l)
				if len(parts) >= 2 {
					carrierServers = append(carrierServers, parts[1])
				}
			}
		}
	}

	servers := cfg.Servers
	if servers == nil {
		servers = make([]string, 0)
	}

	currentUpstream := carrierServers
	currentSource := "carrier"
	if cfg.Enabled && len(servers) > 0 {
		currentUpstream = servers
		currentSource = "custom"
	}

	if currentUpstream == nil {
		currentUpstream = make([]string, 0)
	}

	return map[string]interface{}{
		"success":            true,
		"ok":                 true,
		"enabled":            cfg.Enabled,
		"ignore_carrier":     cfg.IgnoreCarrier,
		"ignoreCarrier":      cfg.IgnoreCarrier,
		"servers":            servers,
		"dns_mode":           "LOCAL",
		"dnsMode":            "LOCAL",
		"dnsmasq_available":  dnsmasqAvailable,
		"available":          dnsmasqAvailable,
		"max_resolvers":      maxDNSServers,
		"maxResolvers":       maxDNSServers,
		"current_upstream":   currentUpstream,
		"currentUpstream":    currentUpstream,
		"current_source":     currentSource,
		"currentSource":      currentSource,
		"passthrough_bypass": false,
		"passthroughBypass":  false,
		"block_corrupt":      false,
		"blockCorrupt":       false,
	}
}

// CustomDNSSavePayload represents the save payload.
type CustomDNSSavePayload struct {
	Action        string      `json:"action"` // "save" or "clear"
	Enabled       *bool       `json:"enabled,omitempty"`
	Servers       interface{} `json:"servers,omitempty"` // can be []string, []interface{}, or string (comma-separated)
	IgnoreCarrier *bool       `json:"ignore_carrier,omitempty"`
}

// HandlePost handles POST /api/v1/network/dns and /cgi-bin/quecmanager/network/custom_dns.sh
func (h *CustomDNSHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	var rawBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		JSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"ok":      false,
			"error":   "Invalid JSON payload",
		})
		return
	}

	action, _ := rawBody["action"].(string)
	if action == "clear" {
		cfg := CustomDNSConfig{
			Enabled:       false,
			Servers:       []string{},
			IgnoreCarrier: false,
		}
		_ = writeCustomDNSConfig(cfg)
		_ = updateDnsmasqConf(cfg)
		applied := h.buildFullResponse()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"ok":      true,
			"message": "Custom DNS cleared",
			"applied": applied,
		})
		return
	}

	enabled := false
	if v, ok := rawBody["enabled"].(bool); ok {
		enabled = v
	}

	ignoreCarrier := false
	if v, ok := rawBody["ignore_carrier"].(bool); ok {
		ignoreCarrier = v
	} else if v, ok := rawBody["ignoreCarrier"].(bool); ok {
		ignoreCarrier = v
	}

	var rawServersList []string
	if sVal, exists := rawBody["servers"]; exists {
		switch v := sVal.(type) {
		case string:
			for _, part := range strings.Split(v, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					rawServersList = append(rawServersList, part)
				}
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						rawServersList = append(rawServersList, s)
					}
				}
			}
		case []string:
			for _, s := range v {
				s = strings.TrimSpace(s)
				if s != "" {
					rawServersList = append(rawServersList, s)
				}
			}
		}
	}

	var validServers []string
	if enabled {
		if len(rawServersList) == 0 {
			JSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"ok":      false,
				"field":   "servers",
				"error":   "At least one DNS server is required when enabled",
			})
			return
		}
		if len(rawServersList) > maxDNSServers {
			JSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"ok":      false,
				"field":   "servers",
				"error":   fmt.Sprintf("Maximum %d DNS servers allowed", maxDNSServers),
			})
			return
		}

		for _, s := range rawServersList {
			ip := net.ParseIP(s)
			if ip == nil {
				JSON(w, http.StatusBadRequest, map[string]interface{}{
					"success": false,
					"ok":      false,
					"field":   "servers",
					"error":   fmt.Sprintf("Invalid IP address: %s", s),
				})
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
		JSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"ok":      false,
			"error":   "Failed to persist DNS config",
		})
		return
	}

	if err := updateDnsmasqConf(cfg); err != nil {
		JSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"ok":      false,
			"error":   fmt.Sprintf("Failed to update dnsmasq.conf: %v", err),
		})
		return
	}

	applied := h.buildFullResponse()
	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"ok":      true,
		"message": "Custom DNS applied successfully",
		"enabled": enabled,
		"servers": validServers,
		"applied": applied,
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
		content = content + block.String()
	}

	dir := filepath.Dir(dnsmasqConfPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", dnsmasqConfPath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	if err := os.Rename(tmpFile, dnsmasqConfPath); err != nil {
		return err
	}

	// Reload dnsmasq if running
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
	dir := filepath.Dir(customDNSConfig)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", customDNSConfig, time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	return os.Rename(tmpFile, customDNSConfig)
}
