package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"qmanager/internal/atengine"
)

// LTEFreqEntry represents a locked LTE EARFCN.
type LTEFreqEntry struct {
	EARFCN int `json:"earfcn"`
}

// NRFreqEntry represents a locked NR ARFCN and SCS.
type NRFreqEntry struct {
	ARFCN int `json:"arfcn"`
	SCS   int `json:"scs"`
}

// FrequencyModemState holds the current frequency and tower lock status.
type FrequencyModemState struct {
	LTELocked    bool           `json:"lte_locked"`
	LTEEntries   []LTEFreqEntry `json:"lte_entries"`
	NRLocked     bool           `json:"nr_locked"`
	NREntries    []NRFreqEntry  `json:"nr_entries"`
	TowerLockLTE *bool          `json:"tower_lock_lte"` // tri-state: true, false, null
	TowerLockNR  *bool          `json:"tower_lock_nr"`  // tri-state: true, false, null
}

// FrequencyStatusResponse is the envelope for GET frequency status.
type FrequencyStatusResponse struct {
	Success    bool                `json:"success"`
	ModemState FrequencyModemState `json:"modem_state"`
}

// FrequencyLockHandler handles frequency locking / unlocking.
type FrequencyLockHandler struct {
	engine *atengine.Engine
}

// NewFrequencyLockHandler creates a new FrequencyLockHandler.
func NewFrequencyLockHandler(engine *atengine.Engine) *FrequencyLockHandler {
	return &FrequencyLockHandler{
		engine: engine,
	}
}

// Status handles GET /api/v1/cellular/frequency and GET /cgi-bin/quecmanager/frequency/status.sh
func (h *FrequencyLockHandler) Status(w http.ResponseWriter, r *http.Request) {
	// Query compound frequency lock info: AT+QNWCFG="lte_earfcn_lock";+QNWCFG="nr5g_earfcn_lock"
	rawResp, err := h.engine.Exec(`AT+QNWCFG="lte_earfcn_lock";+QNWCFG="nr5g_earfcn_lock"`)
	if err != nil {
		Error(w, http.StatusOK, "Unable to read frequency lock state from modem")
		return
	}

	state := ParseFrequencyStatus(rawResp.Raw)

	// Check tower lock state for mutual exclusion gating
	// 1. LTE Tower Lock: AT+QNWLOCK="common/4g"
	lteTowerResp, err := h.engine.Exec(`AT+QNWLOCK="common/4g"`)
	if err != nil || strings.TrimSpace(lteTowerResp.Raw) == "" {
		state.TowerLockLTE = nil // null / unknown
	} else {
		locked := parseTowerLockLTE(lteTowerResp.Raw)
		state.TowerLockLTE = &locked
	}

	// Small pause between commands
	time.Sleep(50 * time.Millisecond)

	// 2. NR Tower Lock: AT+QNWLOCK="common/5g"
	nrTowerResp, err := h.engine.Exec(`AT+QNWLOCK="common/5g"`)
	if err != nil || strings.TrimSpace(nrTowerResp.Raw) == "" {
		state.TowerLockNR = nil // null / unknown
	} else {
		locked := parseTowerLockNR(nrTowerResp.Raw)
		state.TowerLockNR = &locked
	}

	JSON(w, http.StatusOK, FrequencyStatusResponse{
		Success:    true,
		ModemState: state,
	})
}

// ParseFrequencyStatus parses +QNWCFG: "lte_earfcn_lock" and "nr5g_earfcn_lock" responses.
func ParseFrequencyStatus(raw string) FrequencyModemState {
	state := FrequencyModemState{
		LTEEntries: []LTEFreqEntry{},
		NREntries:  []NRFreqEntry{},
	}

	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.Contains(line, `+QNWCFG: "lte_earfcn_lock"`) || strings.Contains(line, `+QNWCFG: "lte_earfcn_lock",`) || strings.Contains(line, `+QNWCFG:"lte_earfcn_lock"`) {
			// Format: +QNWCFG: "lte_earfcn_lock",<count>,<earfcn1>[:<earfcn2>...]
			idx := strings.Index(line, `"lte_earfcn_lock",`)
			if idx != -1 {
				params := strings.TrimSpace(line[idx+len(`"lte_earfcn_lock",`):])
				parts := strings.Split(params, ",")
				if len(parts) >= 1 {
					count, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
					if count > 0 && len(parts) >= 2 {
						state.LTELocked = true
						earfcns := strings.Split(strings.TrimSpace(parts[1]), ":")
						for _, eStr := range earfcns {
							eStr = strings.TrimSpace(eStr)
							if val, err := strconv.Atoi(eStr); err == nil && eStr != "" {
								state.LTEEntries = append(state.LTEEntries, LTEFreqEntry{EARFCN: val})
							}
						}
					}
				}
			}
		} else if strings.Contains(line, `+QNWCFG: "nr5g_earfcn_lock"`) || strings.Contains(line, `+QNWCFG: "nr5g_earfcn_lock",`) || strings.Contains(line, `+QNWCFG:"nr5g_earfcn_lock"`) {
			// Format: +QNWCFG: "nr5g_earfcn_lock",<count>,<arfcn1>:<scs1>[:<arfcn2>:<scs2>...]
			idx := strings.Index(line, `"nr5g_earfcn_lock",`)
			if idx != -1 {
				params := strings.TrimSpace(line[idx+len(`"nr5g_earfcn_lock",`):])
				parts := strings.Split(params, ",")
				if len(parts) >= 1 {
					count, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
					if count > 0 && len(parts) >= 2 {
						state.NRLocked = true
						tokens := strings.Split(strings.TrimSpace(parts[1]), ":")
						for i := 0; i+1 < len(tokens); i += 2 {
							arfcn, err1 := strconv.Atoi(strings.TrimSpace(tokens[i]))
							scs, err2 := strconv.Atoi(strings.TrimSpace(tokens[i+1]))
							if err1 == nil && err2 == nil {
								state.NREntries = append(state.NREntries, NRFreqEntry{
									ARFCN: arfcn,
									SCS:   scs,
								})
							}
						}
					}
				}
			}
		}
	}

	return state
}

func parseTowerLockLTE(raw string) bool {
	// Format: +QNWLOCK: "common/4g",<num_cells>,...
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, `+QNWLOCK: "common/4g"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				numCells, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				return err == nil && numCells > 0
			}
		}
	}
	return false
}

func parseTowerLockNR(raw string) bool {
	// Format: +QNWLOCK: "common/5g",<num_cells>,...
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, `+QNWLOCK: "common/5g"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				numCells, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				return err == nil && numCells > 0
			}
		}
	}
	return false
}

// FrequencyLockRequest payload for lock.sh / lock API.
type FrequencyLockRequest struct {
	Action     string         `json:"action"`                // "lock" or "unlock"
	RAT        string         `json:"rat"`                   // "lte" or "nr5g"
	LTEEntries []LTEFreqEntry `json:"lte_entries,omitempty"` // For LTE lock
	NREntries  []NRFreqEntry  `json:"nr_entries,omitempty"`  // For NR lock
}

// Lock handles POST /api/v1/cellular/frequency/lock and POST /cgi-bin/quecmanager/frequency/lock.sh
func (h *FrequencyLockHandler) Lock(w http.ResponseWriter, r *http.Request) {
	var req FrequencyLockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	req.RAT = strings.ToLower(strings.TrimSpace(req.RAT))
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))

	if req.RAT != "lte" && req.RAT != "nr5g" {
		Error(w, http.StatusBadRequest, "rat must be 'lte' or 'nr5g'")
		return
	}
	if req.Action != "lock" && req.Action != "unlock" {
		Error(w, http.StatusBadRequest, "action must be 'lock' or 'unlock'")
		return
	}

	if req.RAT == "lte" {
		h.handleLTELock(w, req)
	} else {
		h.handleNRLock(w, req)
	}
}

func (h *FrequencyLockHandler) handleLTELock(w http.ResponseWriter, req FrequencyLockRequest) {
	if req.Action == "unlock" {
		// AT+QNWCFG="lte_earfcn_lock",0
		_, err := h.engine.Exec(`AT+QNWCFG="lte_earfcn_lock",0`)
		if err != nil {
			Error(w, http.StatusOK, "Failed to clear LTE frequency lock")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"action":  "unlock",
			"rat":     "lte",
			"message": "LTE frequency lock cleared",
		})
		return
	}

	// Action == "lock": Check Tower Lock Mutual Exclusion
	lteTowerResp, err := h.engine.Exec(`AT+QNWLOCK="common/4g"`)
	if err != nil || strings.TrimSpace(lteTowerResp.Raw) == "" {
		// Retry once before failing closed
		time.Sleep(100 * time.Millisecond)
		lteTowerResp, err = h.engine.Exec(`AT+QNWLOCK="common/4g"`)
	}
	if err != nil || strings.TrimSpace(lteTowerResp.Raw) == "" {
		Error(w, http.StatusOK, "Could not verify LTE tower lock state — refusing frequency lock. Try again.")
		return
	}
	if parseTowerLockLTE(lteTowerResp.Raw) {
		Error(w, http.StatusOK, "LTE Tower Lock is currently active. Unlock towers before applying a frequency lock.")
		return
	}

	// Validate LTE entries
	count := len(req.LTEEntries)
	if count < 1 || count > 32 {
		Error(w, http.StatusBadRequest, "lte_entries must contain 1-32 EARFCNs")
		return
	}

	var earfcns []string
	for _, e := range req.LTEEntries {
		if e.EARFCN < 0 || e.EARFCN > 262143 {
			Error(w, http.StatusBadRequest, fmt.Sprintf("EARFCN out of range (0-262143): %d", e.EARFCN))
			return
		}
		earfcns = append(earfcns, strconv.Itoa(e.EARFCN))
	}

	cmd := fmt.Sprintf(`AT+QNWCFG="lte_earfcn_lock",%d,%s`, count, strings.Join(earfcns, ":"))
	_, err = h.engine.Exec(cmd)
	if err != nil {
		Error(w, http.StatusOK, "Failed to apply LTE frequency lock")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"action":      "lock",
		"rat":         "lte",
		"count":       count,
		"earfcn_list": strings.Join(earfcns, ":"),
		"message":     fmt.Sprintf("LTE frequency locked to %d carrier(s)", count),
	})
}

func (h *FrequencyLockHandler) handleNRLock(w http.ResponseWriter, req FrequencyLockRequest) {
	if req.Action == "unlock" {
		// AT+QNWCFG="nr5g_earfcn_lock",0
		_, err := h.engine.Exec(`AT+QNWCFG="nr5g_earfcn_lock",0`)
		if err != nil {
			Error(w, http.StatusOK, "Failed to clear NR5G frequency lock")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"action":  "unlock",
			"rat":     "nr5g",
			"message": "NR5G frequency lock cleared",
		})
		return
	}

	// Action == "lock": Check Tower Lock Mutual Exclusion
	nrTowerResp, err := h.engine.Exec(`AT+QNWLOCK="common/5g"`)
	if err != nil || strings.TrimSpace(nrTowerResp.Raw) == "" {
		// Retry once before failing closed
		time.Sleep(100 * time.Millisecond)
		nrTowerResp, err = h.engine.Exec(`AT+QNWLOCK="common/5g"`)
	}
	if err != nil || strings.TrimSpace(nrTowerResp.Raw) == "" {
		Error(w, http.StatusOK, "Could not verify NR5G tower lock state — refusing frequency lock. Try again.")
		return
	}
	if parseTowerLockNR(nrTowerResp.Raw) {
		Error(w, http.StatusOK, "NR5G Tower Lock is currently active. Unlock towers before applying a frequency lock.")
		return
	}

	// Validate NR entries
	count := len(req.NREntries)
	if count < 1 || count > 32 {
		Error(w, http.StatusBadRequest, "nr_entries must contain 1-32 ARFCNs")
		return
	}

	var pairs []string
	for _, e := range req.NREntries {
		if e.ARFCN < 0 || e.ARFCN > 3279165 {
			Error(w, http.StatusBadRequest, fmt.Sprintf("ARFCN out of range (0-3279165): %d", e.ARFCN))
			return
		}
		scs := e.SCS
		if scs != 15 && scs != 30 && scs != 60 && scs != 120 && scs != 240 {
			scs = 30 // Default 30kHz
		}
		pairs = append(pairs, fmt.Sprintf("%d:%d", e.ARFCN, scs))
	}

	cmd := fmt.Sprintf(`AT+QNWCFG="nr5g_earfcn_lock",%d,%s`, count, strings.Join(pairs, ":"))
	_, err = h.engine.Exec(cmd)
	if err != nil {
		Error(w, http.StatusOK, "Failed to apply NR5G frequency lock")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"action":     "lock",
		"rat":        "nr5g",
		"count":      count,
		"arfcn_list": strings.Join(pairs, ":"),
		"message":    fmt.Sprintf("NR5G frequency locked to %d carrier(s)", count),
	})
}
