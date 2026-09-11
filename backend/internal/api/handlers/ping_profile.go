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
	Profile     string `json:"profile"`
	TargetIPv4  string `json:"target_ipv4"`
	TargetIPv6  string `json:"target_ipv6"`
	IntervalSec int    `json:"interval_sec"`
	TimeoutSec  int    `json:"timeout_sec"`
	FailCount   int    `json:"fail_count"`
}

// PingProfileHandler manages probe target settings and sensitivity profiles.
type PingProfileHandler struct {
	mu          sync.RWMutex
	prober      *telemetry.PingProber
	filePath    string
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

	var cfg PingProfileSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.Profile != "" {
		h.profile = cfg.Profile
	}
	if cfg.TargetIPv4 != "" {
		h.targetIPv4 = cfg.TargetIPv4
	}
	if cfg.TargetIPv6 != "" {
		h.targetIPv6 = cfg.TargetIPv6
	}
	if cfg.IntervalSec > 0 {
		h.intervalSec = cfg.IntervalSec
	}
	if cfg.TimeoutSec > 0 {
		h.timeoutSec = cfg.TimeoutSec
	}
	if cfg.FailCount > 0 {
		h.failCount = cfg.FailCount
	}

	if h.prober != nil && h.targetIPv4 != "" {
		h.prober.SetTarget(h.targetIPv4)
	}

	return nil
}

func (h *PingProfileHandler) saveConfig() error {
	dir := filepath.Dir(h.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed creating dir %s: %w", dir, err)
	}

	cfg := PingProfileSettings{
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed marshalling ping profile: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", h.filePath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed opening temp file %s: %w", tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed writing data: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed fsync: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed closing temp file: %w", err)
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

	resp := PingProfileSettings{
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
	}

	JSON(w, http.StatusOK, resp)
}

// Save updates ping profile settings.
func (h *PingProfileHandler) Save(w http.ResponseWriter, r *http.Request) {
	var payload struct {
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

	resp := PingProfileSettings{
		Profile:     h.profile,
		TargetIPv4:  h.targetIPv4,
		TargetIPv6:  h.targetIPv6,
		IntervalSec: h.intervalSec,
		TimeoutSec:  h.timeoutSec,
		FailCount:   h.failCount,
	}

	JSON(w, http.StatusOK, resp)
}
