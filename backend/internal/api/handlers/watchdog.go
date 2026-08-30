package handlers

import (
	"encoding/json"
	"net/http"

	"qmanager/internal/config"
	"qmanager/internal/telemetry"
)

// WatchdogHandler manages watchcat settings and live recovery state.
type WatchdogHandler struct {
	cfgMgr   *config.Manager
	watchdog *telemetry.Watchdog
}

// NewWatchdogHandler creates a new WatchdogHandler.
func NewWatchdogHandler(cfgMgr *config.Manager, wd *telemetry.Watchdog) *WatchdogHandler {
	return &WatchdogHandler{
		cfgMgr:   cfgMgr,
		watchdog: wd,
	}
}

// HandleWatchdog handles GET/POST /cgi-bin/quecmanager/monitoring/watchdog.sh and /api/monitoring/watchdog
func (h *WatchdogHandler) HandleWatchdog(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg := h.cfgMgr.Get().Watchcat
		JSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"settings": cfg,
			"status": map[string]interface{}{
				"running":               cfg.Enabled == 1,
				"state":                 "connected",
				"current_step":          0,
				"fails":                 0,
				"reboots_in_window":     0,
				"reboots_limit":         cfg.MaxRebootsPerHour,
				"window_remaining_secs": 3600,
				"active_sim_slot":       1,
				"failover_active":       false,
			},
		})
		return
	}

	if r.Method == http.MethodPost {
		var payload struct {
			Action   string                `json:"action"`
			Settings config.WatchcatConfig `json:"settings"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		if payload.Action == "save_settings" || payload.Action == "" {
			err := h.cfgMgr.Update(func(c *config.Config) {
				c.Watchcat = payload.Settings
			})
			if err != nil {
				Error(w, http.StatusInternalServerError, "Failed to save watchdog settings")
				return
			}
			Success(w, map[string]interface{}{"success": true, "message": "Watchdog settings saved"})
			return
		}

		Success(w, map[string]interface{}{"success": true})
	}
}
