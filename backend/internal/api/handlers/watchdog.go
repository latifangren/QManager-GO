package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

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
		var status telemetry.WatchdogStatus
		if h.watchdog != nil {
			status = h.watchdog.GetStatus()
		} else {
			status = telemetry.WatchdogStatus{
				Running:             cfg.Enabled == 1,
				State:               "connected",
				CurrentStep:         0,
				Fails:               0,
				RebootsInWindow:     0,
				RebootsLimit:        cfg.MaxRebootsPerHour,
				WindowRemainingSecs: 3600,
				ActiveSimSlot:       1,
				FailoverActive:      false,
			}
		}

		var backupSimSlot *int
		if cfg.BackupSimSlot != "" && cfg.BackupSimSlot != "0" {
			if s, err := strconv.Atoi(cfg.BackupSimSlot); err == nil {
				backupSimSlot = &s
			}
		}

		settingsMap := map[string]interface{}{
			"enabled":              cfg.Enabled == 1,
			"check_interval":       cfg.CheckInterval,
			"fail_threshold":       cfg.FailThreshold,
			"probe_interval":       cfg.ProbeInterval,
			"cooldown":             cfg.Cooldown,
			"tier1_enabled":        cfg.Tier1Enabled == 1,
			"tier2_enabled":        cfg.Tier2Enabled == 1,
			"tier3_enabled":        cfg.Tier3Enabled == 1,
			"tier4_enabled":        cfg.Tier4Enabled == 1,
			"backup_sim_slot":      backupSimSlot,
			"max_reboots_per_hour": cfg.MaxRebootsPerHour,
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"success":       true,
			"settings":      settingsMap,
			"status":        status,
			"auto_disabled": false,
		})
		return
	}

	if r.Method == http.MethodPost {
		var raw map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		action, _ := raw["action"].(string)

		if action == "revert_sim" {
			if h.watchdog != nil {
				h.watchdog.RevertSim()
			}
			Success(w, map[string]interface{}{"success": true, "message": "SIM reverted"})
			return
		}

		// Source settings from nested "settings" if present, else root
		src := raw
		if nested, ok := raw["settings"].(map[string]interface{}); ok {
			src = nested
		}

		toInt := func(v interface{}, def int) int {
			switch val := v.(type) {
			case float64:
				return int(val)
			case int:
				return val
			case bool:
				if val {
					return 1
				}
				return 0
			case string:
				if n, err := strconv.Atoi(val); err == nil {
					return n
				}
			}
			return def
		}

		toBoolInt := func(v interface{}, def int) int {
			switch val := v.(type) {
			case bool:
				if val {
					return 1
				}
				return 0
			case float64:
				if int(val) == 1 {
					return 1
				}
				return 0
			case int:
				if val == 1 {
					return 1
				}
				return 0
			}
			return def
		}

		currentCfg := h.cfgMgr.Get().Watchcat

		if v, ok := src["enabled"]; ok {
			currentCfg.Enabled = toBoolInt(v, currentCfg.Enabled)
		}
		if v, ok := src["check_interval"]; ok {
			currentCfg.CheckInterval = toInt(v, currentCfg.CheckInterval)
		}
		if v, ok := src["fail_threshold"]; ok {
			currentCfg.FailThreshold = toInt(v, currentCfg.FailThreshold)
		}
		if v, ok := src["probe_interval"]; ok {
			currentCfg.ProbeInterval = toInt(v, currentCfg.ProbeInterval)
		}
		if v, ok := src["cooldown"]; ok {
			currentCfg.Cooldown = toInt(v, currentCfg.Cooldown)
		}
		if v, ok := src["tier1_enabled"]; ok {
			currentCfg.Tier1Enabled = toBoolInt(v, currentCfg.Tier1Enabled)
		}
		if v, ok := src["tier2_enabled"]; ok {
			currentCfg.Tier2Enabled = toBoolInt(v, currentCfg.Tier2Enabled)
		}
		if v, ok := src["tier3_enabled"]; ok {
			currentCfg.Tier3Enabled = toBoolInt(v, currentCfg.Tier3Enabled)
		}
		if v, ok := src["tier4_enabled"]; ok {
			currentCfg.Tier4Enabled = toBoolInt(v, currentCfg.Tier4Enabled)
		}
		if v, ok := src["backup_sim_slot"]; ok {
			if v == nil {
				currentCfg.BackupSimSlot = ""
			} else {
				switch val := v.(type) {
				case float64:
					currentCfg.BackupSimSlot = strconv.Itoa(int(val))
				case int:
					currentCfg.BackupSimSlot = strconv.Itoa(val)
				case string:
					currentCfg.BackupSimSlot = val
				}
			}
		}
		if v, ok := src["max_reboots_per_hour"]; ok {
			currentCfg.MaxRebootsPerHour = toInt(v, currentCfg.MaxRebootsPerHour)
		}

		err := h.cfgMgr.Update(func(c *config.Config) {
			c.Watchcat = currentCfg
		})
		if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to save watchdog settings")
			return
		}

		Success(w, map[string]interface{}{"success": true, "message": "Watchdog settings saved"})
	}
}
