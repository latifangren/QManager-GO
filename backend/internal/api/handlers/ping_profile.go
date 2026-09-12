package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"qmanager/internal/telemetry"
)

var defaultPingProfilePath = "/etc/qmanager/ping_profile.json"

// PingProfileSettings represents the JSON structure of ping profile.
type PingProfileSettings struct {
	TargetHost1 string `json:"target_host_1"`
	TargetHost2 string `json:"target_host_2"`
	TargetIP1   string `json:"target_ip_1"`
	TargetIP2   string `json:"target_ip_2"`
	Profile     string `json:"profile,omitempty"`
	TargetIPv4  string `json:"target_ipv4,omitempty"`
	TargetIPv6  string `json:"target_ipv6,omitempty"`
	IntervalSec int    `json:"interval_sec,omitempty"`
	TimeoutSec  int    `json:"timeout_sec,omitempty"`
	FailCount   int    `json:"fail_count,omitempty"`
}

// PingProfileResponse represents the response envelope.
type PingProfileResponse struct {
	Success     bool                `json:"success"`
	Profile     string              `json:"profile,omitempty"`
	TargetIPv4  string              `json:"target_ipv4,omitempty"`
	TargetIPv6  string              `json:"target_ipv6,omitempty"`
	IntervalSec int                 `json:"interval_sec,omitempty"`
	TimeoutSec  int                 `json:"timeout_sec,omitempty"`
	FailCount   int                 `json:"fail_count,omitempty"`
	TargetHost1 string              `json:"target_host_1,omitempty"`
	TargetHost2 string              `json:"target_host_2,omitempty"`
	TargetIP1   string              `json:"target_ip_1,omitempty"`
	TargetIP2   string              `json:"target_ip_2,omitempty"`
	Settings    PingProfileSettings `json:"settings"`
	Error       string              `json:"error,omitempty"`
	Detail      string              `json:"detail,omitempty"`
}

// PingProfileHandler manages probe target settings and sensitivity profiles.
type PingProfileHandler struct {
	mu          sync.RWMutex
	prober      *telemetry.PingProber
	filePath    string
	targetHost1 string
	targetHost2 string
	targetIP1   string
	targetIP2   string
	profile     string
	targetIPv4  string
	targetIPv6  string
	intervalSec int
	timeoutSec  int
	failCount   int
}

// NewPingProfileHandler creates a PingProfileHandler.
func NewPingProfileHandler(prober *telemetry.PingProber, optionalPath ...string) *PingProfileHandler {
	path := defaultPingProfilePath
	if len(optionalPath) > 0 && optionalPath[0] != "" {
		path = optionalPath[0]
	}

	h := &PingProfileHandler{
		prober:      prober,
		filePath:    path,
		targetHost1: "cloudflare.com",
		targetHost2: "google.com",
		targetIP1:   "1.1.1.1",
		targetIP2:   "8.8.8.8",
		profile:     "regular",
		targetIPv4:  "1.1.1.1",
		targetIPv6:  "2606:4700:4700::1111",
		intervalSec: 2,
		timeoutSec:  1,
		failCount:   3,
	}

	_ = h.loadConfig()
	return h
}

// SetFilePath updates storage path and reloads config.
func (h *PingProfileHandler) SetFilePath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.filePath = path
	h.targetHost1 = "cloudflare.com"
	h.targetHost2 = "google.com"
	h.targetIP1 = "1.1.1.1"
	h.targetIP2 = "8.8.8.8"
	h.profile = "regular"
	h.targetIPv4 = "1.1.1.1"
	h.targetIPv6 = "2606:4700:4700::1111"
	h.intervalSec = 2
	h.timeoutSec = 1
	h.failCount = 3
	_ = h.loadConfig()
}

func (h *PingProfileHandler) loadConfig() error {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["target_host_1"].(string); ok && v != "" {
		h.targetHost1 = v
	}
	if v, ok := raw["target_host_2"].(string); ok && v != "" {
		h.targetHost2 = v
	}
	if v, ok := raw["target_ip_1"].(string); ok && v != "" {
		h.targetIP1 = v
	}
	if v, ok := raw["target_ip_2"].(string); ok && v != "" {
		h.targetIP2 = v
	}
	if v, ok := raw["profile"].(string); ok && v != "" {
		h.profile = v
	}
	if v, ok := raw["target_ipv4"].(string); ok && v != "" {
		h.targetIPv4 = v
		if h.prober != nil {
			h.prober.SetTarget(v)
		}
	}
	if v, ok := raw["target_ipv6"].(string); ok && v != "" {
		h.targetIPv6 = v
	}
	if v, ok := raw["interval_sec"].(float64); ok && v > 0 {
		h.intervalSec = int(v)
	}
	if v, ok := raw["timeout_sec"].(float64); ok && v > 0 {
		h.timeoutSec = int(v)
	}
	if v, ok := raw["fail_count"].(float64); ok && v > 0 {
		h.failCount = int(v)
	}

	return nil
}

func (h *PingProfileHandler) saveConfig() error {
	dir := filepath.Dir(h.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed creating config dir %s: %w", dir, err)
	}

	cfg := map[string]interface{}{
		"target_host_1": h.targetHost1,
		"target_host_2": h.targetHost2,
		"target_ip_1":   h.targetIP1,
		"target_ip_2":   h.targetIP2,
		"profile":       h.profile,
		"target_ipv4":   h.targetIPv4,
		"target_ipv6":   h.targetIPv6,
		"interval_sec":  h.intervalSec,
		"timeout_sec":   h.timeoutSec,
		"fail_count":    h.failCount,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed marshalling ping profile: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", h.filePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed writing data: %w", err)
	}

	if err := os.Rename(tmpPath, h.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed renaming to %s: %w", h.filePath, err)
	}

	return nil
}

// Handle handles GET and POST for /api/v1/settings/ping-profile and /cgi-bin/quecmanager/settings/ping_profile.sh
func (h *PingProfileHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.Get(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.Save(w, r)
		return
	}
	Error(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// Get returns current ping profile settings.
func (h *PingProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	settings := PingProfileSettings{
		TargetHost1: h.targetHost1,
		TargetHost2: h.targetHost2,
		TargetIP1:   h.targetIP1,
		TargetIP2:   h.targetIP2,
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
	}

	resp := PingProfileResponse{
		Success:     true,
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
		TargetHost1: h.targetHost1,
		TargetHost2: h.targetHost2,
		TargetIP1:   h.targetIP1,
		TargetIP2:   h.targetIP2,
		Settings:    settings,
	}

	JSON(w, http.StatusOK, resp)
}

// Save updates ping profile settings.
func (h *PingProfileHandler) Save(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Action      string  `json:"action"`
		TargetHost1 *string `json:"target_host_1"`
		TargetHost2 *string `json:"target_host_2"`
		TargetIP1   *string `json:"target_ip_1"`
		TargetIP2   *string `json:"target_ip_2"`
		Profile     *string `json:"profile"`
		TargetIPv4  *string `json:"target_ipv4"`
		TargetIPv6  *string `json:"target_ipv6"`
		IntervalSec *int    `json:"interval_sec"`
		TimeoutSec  *int    `json:"timeout_sec"`
		FailCount   *int    `json:"fail_count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if payload.TargetHost1 != nil && *payload.TargetHost1 != "" {
		h.targetHost1 = *payload.TargetHost1
	}
	if payload.TargetHost2 != nil && *payload.TargetHost2 != "" {
		h.targetHost2 = *payload.TargetHost2
	}
	if payload.TargetIP1 != nil && *payload.TargetIP1 != "" {
		h.targetIP1 = *payload.TargetIP1
	}
	if payload.TargetIP2 != nil && *payload.TargetIP2 != "" {
		h.targetIP2 = *payload.TargetIP2
	}
	if payload.Profile != nil && *payload.Profile != "" {
		h.profile = *payload.Profile
	}
	if payload.TargetIPv4 != nil && *payload.TargetIPv4 != "" {
		h.targetIPv4 = *payload.TargetIPv4
		if h.prober != nil {
			h.prober.SetTarget(h.targetIPv4)
		}
	}
	if payload.TargetIPv6 != nil && *payload.TargetIPv6 != "" {
		h.targetIPv6 = *payload.TargetIPv6
	}
	if payload.IntervalSec != nil && *payload.IntervalSec > 0 {
		h.intervalSec = *payload.IntervalSec
	}
	if payload.TimeoutSec != nil && *payload.TimeoutSec > 0 {
		h.timeoutSec = *payload.TimeoutSec
	}
	if payload.FailCount != nil && *payload.FailCount > 0 {
		h.failCount = *payload.FailCount
	}

	if err := h.saveConfig(); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save ping profile: %v", err))
		return
	}

	settings := PingProfileSettings{
		TargetHost1: h.targetHost1,
		TargetHost2: h.targetHost2,
		TargetIP1:   h.targetIP1,
		TargetIP2:   h.targetIP2,
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
	}

	resp := PingProfileResponse{
		Success:     true,
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
		TargetHost1: h.targetHost1,
		TargetHost2: h.targetHost2,
		TargetIP1:   h.targetIP1,
		TargetIP2:   h.targetIP2,
		Settings:    settings,
	}

	JSON(w, http.StatusOK, resp)
}
