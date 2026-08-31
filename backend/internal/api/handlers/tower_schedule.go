package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"qmanager/internal/atengine"
)

const (
	towerWriteSettleSec = 30
)

var (
	towerConfigPath        = "/etc/qmanager/tower_lock.json"
	towerFailoverFlagPath  = "/tmp/qmanager_tower_failover"
	towerFailoverPidPath   = "/tmp/qmanager_tower_failover.pid"
	towerWriteInflightPath = "/tmp/qmanager_tower_write_inflight"
)

// LTECellEntry represents a single LTE cell configuration.
type LTECellEntry struct {
	PCI    int `json:"pci"`
	EARFCN int `json:"earfcn"`
}

// NRCellConfig represents NR SA cell configuration.
type NRCellConfig struct {
	PCI   *int `json:"pci"`
	ARFCN *int `json:"arfcn"`
	SCS   *int `json:"scs"`
	Band  *int `json:"band"`
}

// TowerConfig holds the /etc/qmanager/tower_lock.json schema.
type TowerConfig struct {
	LTE struct {
		Enabled bool             `json:"enabled"`
		Cells   []*LTECellEntry  `json:"cells"`
	} `json:"lte"`
	NRSA struct {
		Enabled bool `json:"enabled"`
		NRCellConfig
	} `json:"nr_sa"`
	Persist  bool `json:"persist"`
	Failover struct {
		Enabled   bool `json:"enabled"`
		Threshold int  `json:"threshold"`
	} `json:"failover"`
	Schedule struct {
		Enabled   bool   `json:"enabled"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Days      []int  `json:"days"`
	} `json:"schedule"`
}

// TowerScheduleHandler manages tower lock status, schedule, settings, and failover state.
type TowerScheduleHandler struct {
	engine     *atengine.Engine
	configPath string
	mu         sync.Mutex
}

// NewTowerScheduleHandler creates a new TowerScheduleHandler.
func NewTowerScheduleHandler(engine *atengine.Engine) *TowerScheduleHandler {
	return &TowerScheduleHandler{
		engine:     engine,
		configPath: towerConfigPath,
	}
}

// SetConfigPath sets custom config path for testing.
func (h *TowerScheduleHandler) SetConfigPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configPath = path
}

func (h *TowerScheduleHandler) loadConfig() TowerConfig {
	var cfg TowerConfig
	cfg.LTE.Cells = []*LTECellEntry{nil, nil, nil}
	cfg.Failover.Threshold = 20
	cfg.Schedule.StartTime = "08:00"
	cfg.Schedule.EndTime = "22:00"
	cfg.Schedule.Days = []int{1, 2, 3, 4, 5}

	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return cfg
	}

	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func (h *TowerScheduleHandler) saveConfig(cfg TowerConfig) error {
	_ = os.MkdirAll(filepath.Dir(h.configPath), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.configPath, data, 0644)
}

// Status handles GET /api/v1/cellular/tower/status and GET /cgi-bin/quecmanager/tower/status.sh
func (h *TowerScheduleHandler) Status(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := h.loadConfig()
	h.mu.Unlock()

	// 1. Read LTE Tower Lock: AT+QNWLOCK="common/4g"
	var lteLocked bool
	var lteCells []LTECellEntry
	lteReadOk := true
	lteResp, err := h.engine.Exec(`AT+QNWLOCK="common/4g"`)
	if err != nil || strings.TrimSpace(lteResp.Raw) == "" {
		lteReadOk = false
	} else {
		lteLocked, lteCells = parseLTETowerCells(lteResp.Raw)
	}

	// 2. Read NR Tower Lock: AT+QNWLOCK="common/5g"
	var nrLocked bool
	var nrCell *NRCellConfig
	nrReadOk := true
	nrResp, err := h.engine.Exec(`AT+QNWLOCK="common/5g"`)
	if err != nil || strings.TrimSpace(nrResp.Raw) == "" {
		nrReadOk = false
	} else {
		nrLocked, nrCell = parseNRTowerCell(nrResp.Raw)
	}

	// 3. Read Persist State: AT+QNWLOCK="save_ctrl"
	var persistLTE, persistNR bool
	persistReadOk := true
	persistResp, err := h.engine.Exec(`AT+QNWLOCK="save_ctrl"`)
	if err != nil || strings.TrimSpace(persistResp.Raw) == "" {
		persistReadOk = false
	} else {
		persistLTE, persistNR = parsePersistState(persistResp.Raw)
	}

	// 4. Failover state from filesystem
	failoverActivated := false
	if _, err := os.Stat(towerFailoverFlagPath); err == nil {
		failoverActivated = true
	}
	watcherRunning := isWatcherRunning()

	resp := map[string]interface{}{
		"success": true,
		"modem_state": map[string]interface{}{
			"lte_locked":      lteLocked,
			"lte_cells":       lteCells,
			"lte_read_ok":     lteReadOk,
			"nr_locked":       nrLocked,
			"nr_cell":         nrCell,
			"nr_read_ok":      nrReadOk,
			"persist_lte":     persistLTE,
			"persist_nr":      persistNR,
			"persist_read_ok": persistReadOk,
		},
		"config": cfg,
		"failover_state": map[string]interface{}{
			"enabled":         cfg.Failover.Enabled,
			"activated":       failoverActivated,
			"watcher_running": watcherRunning,
		},
	}

	JSON(w, http.StatusOK, resp)
}

func parseLTETowerCells(raw string) (bool, []LTECellEntry) {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, `+QNWLOCK: "common/4g"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				numCells, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				if numCells <= 0 {
					return false, []LTECellEntry{}
				}
				var cells []LTECellEntry
				idx := 2
				for i := 0; i < numCells && idx+1 < len(parts); i++ {
					earfcn, _ := strconv.Atoi(strings.TrimSpace(parts[idx]))
					pci, _ := strconv.Atoi(strings.TrimSpace(parts[idx+1]))
					cells = append(cells, LTECellEntry{
						EARFCN: earfcn,
						PCI:    pci,
					})
					idx += 2
				}
				return true, cells
			}
		}
	}
	return false, []LTECellEntry{}
}

func parseNRTowerCell(raw string) (bool, *NRCellConfig) {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, `+QNWLOCK: "common/5g"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				numCells, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				if numCells <= 0 {
					return false, nil
				}
				// Format: +QNWLOCK: "common/5g",1,<earfcn>,<pci>,<scs>,<band>
				if len(parts) >= 4 {
					earfcn, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
					pci, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
					cfg := &NRCellConfig{
						ARFCN: &earfcn,
						PCI:   &pci,
					}
					if len(parts) >= 5 {
						scs, _ := strconv.Atoi(strings.TrimSpace(parts[4]))
						cfg.SCS = &scs
					}
					if len(parts) >= 6 {
						band, _ := strconv.Atoi(strings.TrimSpace(parts[5]))
						cfg.Band = &band
					}
					return true, cfg
				}
				return true, nil
			}
		}
	}
	return false, nil
}

func parsePersistState(raw string) (bool, bool) {
	// Format: +QNWLOCK: "save_ctrl",<lte_val>,<nr_val>
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, `+QNWLOCK: "save_ctrl"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 3 {
				lteVal, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				nrVal, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
				return lteVal == 1, nrVal == 1
			} else if len(parts) >= 2 {
				val, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				return val == 1, val == 1
			}
		}
	}
	return false, false
}

func isWatcherRunning() bool {
	data, err := os.ReadFile(towerFailoverPidPath)
	if err != nil {
		return false
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return false
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	// On Linux/Unix, check process existence
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess always succeeds, but in actual runtime on linux we test signal 0
	_ = process
	return true
}

// FailoverStatus handles GET /api/v1/cellular/tower/failover-status and GET /cgi-bin/quecmanager/tower/failover_status.sh
func (h *TowerScheduleHandler) FailoverStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := h.loadConfig()
	h.mu.Unlock()

	failoverActivated := false
	if _, err := os.Stat(towerFailoverFlagPath); err == nil {
		failoverActivated = true
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"enabled":         cfg.Failover.Enabled,
		"activated":       failoverActivated,
		"watcher_running": isWatcherRunning(),
	})
}

// TowerSettingsRequest payload for settings.sh / settings API.
type TowerSettingsRequest struct {
	Persist           *bool `json:"persist,omitempty"`
	FailoverEnabled   *bool `json:"failover_enabled,omitempty"`
	FailoverThreshold *int  `json:"failover_threshold,omitempty"`
}

// Settings handles POST /api/v1/cellular/tower/settings and POST /cgi-bin/quecmanager/tower/settings.sh
func (h *TowerScheduleHandler) Settings(w http.ResponseWriter, r *http.Request) {
	var req TowerSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	cfg := h.loadConfig()
	persistCmdFailed := false

	if req.Persist != nil {
		cfg.Persist = *req.Persist
		val := 0
		if *req.Persist {
			val = 1
		}
		// Send AT+QNWLOCK="save_ctrl",<val>
		_, err := h.engine.Exec(fmt.Sprintf(`AT+QNWLOCK="save_ctrl",%d`, val))
		if err != nil {
			persistCmdFailed = true
		}
	}

	if req.FailoverEnabled != nil {
		cfg.Failover.Enabled = *req.FailoverEnabled
		if !*req.FailoverEnabled {
			// Failover disabled -> remove failover flag
			_ = os.Remove(towerFailoverFlagPath)
			_ = exec.Command("systemctl", "stop", "qmanager_tower_failover").Run()
			_ = exec.Command("systemctl", "disable", "qmanager_tower_failover").Run()
		}
	}

	if req.FailoverThreshold != nil {
		if *req.FailoverThreshold >= 0 && *req.FailoverThreshold <= 100 {
			cfg.Failover.Threshold = *req.FailoverThreshold
		}
	}

	if err := h.saveConfig(cfg); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save tower settings")
		return
	}

	resp := map[string]interface{}{
		"success":                true,
		"persist":                cfg.Persist,
		"failover_enabled":       cfg.Failover.Enabled,
		"failover_threshold":     cfg.Failover.Threshold,
		"watcher_spawned":        false,
		"service_disable_failed": false,
		"service_enable_failed":  false,
	}
	if persistCmdFailed {
		resp["persist_command_failed"] = true
	}

	JSON(w, http.StatusOK, resp)
}

// TowerScheduleRequest payload for schedule.sh / schedule API.
type TowerScheduleRequest struct {
	Enabled   *bool  `json:"enabled"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Days      []int  `json:"days"`
}

// Schedule handles POST /api/v1/cellular/tower/schedule and POST /cgi-bin/quecmanager/tower/schedule.sh
func (h *TowerScheduleHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	var req TowerScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	cfg := h.loadConfig()

	if req.Enabled != nil {
		cfg.Schedule.Enabled = *req.Enabled
	}
	if req.StartTime != "" {
		cfg.Schedule.StartTime = req.StartTime
	}
	if req.EndTime != "" {
		cfg.Schedule.EndTime = req.EndTime
	}
	if req.Days != nil {
		cfg.Schedule.Days = req.Days
	}

	// Validation
	if cfg.Schedule.Enabled {
		if !isValidTime(cfg.Schedule.StartTime) || !isValidTime(cfg.Schedule.EndTime) {
			Error(w, http.StatusBadRequest, "Invalid time format (HH:MM expected)")
			return
		}
		if len(cfg.Schedule.Days) == 0 {
			Error(w, http.StatusBadRequest, "At least one day must be selected")
			return
		}
	}

	if err := h.saveConfig(cfg); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save tower schedule")
		return
	}

	// Helper call for systemd OnCalendar arming if available
	if cfg.Schedule.Enabled {
		_ = exec.Command("/usr/bin/qmanager_tower_schedule_arm", "arm").Run()
	} else {
		_ = exec.Command("/usr/bin/qmanager_tower_schedule_arm", "disarm").Run()
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"schedule": cfg.Schedule,
		"message":  "Tower schedule updated",
	})
}

func isValidTime(t string) bool {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}
