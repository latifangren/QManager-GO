package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"qmanager/internal/platform"
)

var defaultBandFailoverConfigPath = "/etc/qmanager/band_failover.json"

// BandFailoverConfig represents the persisted failover configuration.
type BandFailoverConfig struct {
	Enabled       bool     `json:"enabled"`
	FailoverBands []string `json:"failover_bands"`
}

// BandFailoverHandler handles band failover status and toggle.
type BandFailoverHandler struct {
	mu             sync.Mutex
	enabled        bool
	activated      bool
	watcherRunning bool
	failoverBands  []string
	configPath     string
}

// NewBandFailoverHandler creates a new BandFailoverHandler.
func NewBandFailoverHandler() *BandFailoverHandler {
	h := &BandFailoverHandler{
		enabled:        false,
		activated:      false,
		watcherRunning: false,
		failoverBands:  []string{"B3", "B1", "B7"},
		configPath:     defaultBandFailoverConfigPath,
	}
	h.loadConfigLocked()
	return h
}

func (h *BandFailoverHandler) loadConfigLocked() {
	if h.configPath == "" {
		return
	}
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return
	}
	var cfg BandFailoverConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	h.enabled = cfg.Enabled
	if cfg.FailoverBands != nil {
		h.failoverBands = cfg.FailoverBands
	}
	// Note: activated and watcherRunning are strictly volatile in RAM.
}

func (h *BandFailoverHandler) saveConfigLocked() error {
	if h.configPath == "" {
		return nil
	}
	cfg := BandFailoverConfig{
		Enabled:       h.enabled,
		FailoverBands: h.failoverBands,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return platform.AtomicWriteFile(h.configPath, data, 0644)
}

// SetStoragePath sets custom config path for testing.
func (h *BandFailoverHandler) SetStoragePath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configPath = path
}

// SetConfigPath sets custom config path for testing (alias for SetStoragePath).
func (h *BandFailoverHandler) SetConfigPath(path string) {
	h.SetStoragePath(path)
}

// LoadConfig reloads the configuration from disk.
func (h *BandFailoverHandler) LoadConfig() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadConfigLocked()
	return nil
}

// GetState returns the current failover state.
func (h *BandFailoverHandler) GetState() (enabled, activated, watcherRunning bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled, h.activated, h.watcherRunning
}

// IsEnabled returns true if failover is enabled.
func (h *BandFailoverHandler) IsEnabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled
}

// SetState updates all failover state fields.
func (h *BandFailoverHandler) SetState(enabled, activated, watcherRunning bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
	h.activated = activated
	h.watcherRunning = watcherRunning
}

// SetEnabled updates the enabled state.
func (h *BandFailoverHandler) SetEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
	_ = h.saveConfigLocked()
}

// SetActivated updates the activated state.
func (h *BandFailoverHandler) SetActivated(activated bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activated = activated
}

// SetWatcherRunning updates the watcher running state.
func (h *BandFailoverHandler) SetWatcherRunning(running bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.watcherRunning = running
}

// Status handles GET /cgi-bin/quecmanager/bands/failover_status.sh and /api/cellular/bands/failover/status
func (h *BandFailoverHandler) Status(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"enabled":         h.enabled,
		"activated":       h.activated,
		"watcher_running": h.watcherRunning,
		"failover_bands":  h.failoverBands,
	})
}

// Toggle handles POST /cgi-bin/quecmanager/bands/failover_toggle.sh and /api/cellular/bands/failover/toggle
func (h *BandFailoverHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Enabled *bool `json:"enabled"`
	}

	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.Enabled != nil {
		h.enabled = *payload.Enabled
	} else {
		h.enabled = !h.enabled
	}

	_ = h.saveConfigLocked()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": h.enabled,
	})
}

// ServeHTTP dispatches requests based on HTTP method.
func (h *BandFailoverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.Status(w, r)
	case http.MethodPost:
		h.Toggle(w, r)
	default:
		Error(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
