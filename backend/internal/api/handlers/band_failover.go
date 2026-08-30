package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
)

// BandFailoverHandler handles band failover status and toggle.
type BandFailoverHandler struct {
	mu             sync.Mutex
	enabled        bool
	activated      bool
	watcherRunning bool
	failoverBands  []string
}

// NewBandFailoverHandler creates a new BandFailoverHandler.
func NewBandFailoverHandler() *BandFailoverHandler {
	return &BandFailoverHandler{
		enabled:        false,
		activated:      false,
		watcherRunning: false,
		failoverBands:  []string{"B3", "B1", "B7"},
	}
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

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": h.enabled,
	})
}
