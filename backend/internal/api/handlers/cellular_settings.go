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

// CellularSettings holds basic cellular operational parameters.
type CellularSettings struct {
	SimSlot        int    `json:"sim_slot"`
	Cfun           int    `json:"cfun"`
	ModePref       string `json:"mode_pref"`
	Nr5gMode       int    `json:"nr5g_mode"`
	RoamPref       int    `json:"roam_pref"`
	SimDetect      int    `json:"sim_detect"`
	SimDetectLevel int    `json:"sim_detect_level"`
}

// LteAmbrEntry represents an LTE AMBR record.
type LteAmbrEntry struct {
	APN    string `json:"apn"`
	DLKbps int    `json:"dl_kbps"`
	ULKbps int    `json:"ul_kbps"`
}

// Nr5gAmbrEntry represents an NR5G AMBR record.
type Nr5gAmbrEntry struct {
	DNN    string `json:"dnn"`
	DLKbps int    `json:"dl_kbps"`
	ULKbps int    `json:"ul_kbps"`
}

// AmbrData represents combined AMBR telemetry.
type AmbrData struct {
	LTE  []LteAmbrEntry  `json:"lte"`
	NR5G []Nr5gAmbrEntry `json:"nr5g"`
}

// DualSlotEntry represents a physical SIM slot status.
type DualSlotEntry struct {
	Slot   int    `json:"slot"`
	Active bool   `json:"active"`
	ICCID  string `json:"iccid"`
}

// CellularSettingsResponse is returned by GetSettings.
type CellularSettingsResponse struct {
	Success  bool             `json:"success"`
	Settings CellularSettings `json:"settings"`
	Ambr     AmbrData         `json:"ambr"`
	DualSlot *[]DualSlotEntry `json:"dual_slot"`
	Error    string           `json:"error,omitempty"`
}

// CellularSettingsHandler handles basic cellular settings (SIM slot, CFUN, mode pref, etc.).
type CellularSettingsHandler struct {
	engine      *atengine.Engine
	settleDelay time.Duration
	retryDelay  time.Duration
}

// NewCellularSettingsHandler constructs a new CellularSettingsHandler.
func NewCellularSettingsHandler(engine *atengine.Engine) *CellularSettingsHandler {
	return &CellularSettingsHandler{
		engine:      engine,
		settleDelay: 1 * time.Second,
		retryDelay:  500 * time.Millisecond,
	}
}

// SetDelays sets custom settle and retry delays (useful for fast tests).
func (h *CellularSettingsHandler) SetDelays(settle, retry time.Duration) {
	h.settleDelay = settle
	h.retryDelay = retry
}

// GetSettings handles GET /api/v1/cellular/settings and GET /cgi-bin/quecmanager/cellular/settings.sh
func (h *CellularSettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	compoundCmd := `AT+QUIMSLOT?;+CFUN?;+QNWPREFCFG="mode_pref";+QNWPREFCFG="nr5g_disable_mode";+QNWPREFCFG="roam_pref";+QSIMDET?;+QNWCFG="lte_ambr";+QNWCFG="nr5g_ambr"`
	resp, err := h.engine.Exec(compoundCmd)
	if err != nil || resp == nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "read_failed",
			"message": "Unable to read cellular settings from modem",
		})
		return
	}

	hasQuimslot := false
	hasCfun := false

	settings := CellularSettings{
		SimSlot:        1,
		Cfun:           1,
		ModePref:       "AUTO",
		Nr5gMode:       0,
		RoamPref:       255,
		SimDetect:      0,
		SimDetectLevel: 1,
	}

	ambr := AmbrData{
		LTE:  []LteAmbrEntry{},
		NR5G: []Nr5gAmbrEntry{},
	}

	lines := strings.Split(resp.Raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, "+QUIMSLOT:") {
			hasQuimslot = true
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				if val, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					settings.SimSlot = val
				}
			}
		} else if strings.HasPrefix(line, "+CFUN:") {
			hasCfun = true
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				if val, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					settings.Cfun = val
				}
			}
		} else if strings.HasPrefix(line, `+QNWPREFCFG: "mode_pref"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				settings.ModePref = strings.Trim(strings.TrimSpace(parts[1]), "\"")
			}
		} else if strings.HasPrefix(line, `+QNWPREFCFG: "nr5g_disable_mode"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				if val, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					settings.Nr5gMode = val
				}
			}
		} else if strings.HasPrefix(line, `+QNWPREFCFG: "roam_pref"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				if val, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					settings.RoamPref = val
				}
			}
		} else if strings.HasPrefix(line, "+QSIMDET:") {
			valPart := strings.TrimSpace(strings.TrimPrefix(line, "+QSIMDET:"))
			parts := strings.Split(valPart, ",")
			if len(parts) >= 1 {
				if val, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
					settings.SimDetect = val
				}
			}
			if len(parts) >= 2 {
				if val, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					settings.SimDetectLevel = val
				}
			}
		} else if strings.HasPrefix(line, `+QNWCFG: "lte_ambr"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 4 {
				apn := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				dl, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
				ul, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
				ambr.LTE = append(ambr.LTE, LteAmbrEntry{
					APN:    apn,
					DLKbps: dl,
					ULKbps: ul,
				})
			}
		} else if strings.HasPrefix(line, `+QNWCFG: "nr5g_ambr"`) {
			parts := strings.Split(line, ",")
			if len(parts) >= 6 {
				dnn := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				uDL, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
				sDL, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
				uUL, _ := strconv.Atoi(strings.TrimSpace(parts[4]))
				sUL, _ := strconv.Atoi(strings.TrimSpace(parts[5]))
				ambr.NR5G = append(ambr.NR5G, Nr5gAmbrEntry{
					DNN:    dnn,
					DLKbps: uDL * sDL,
					ULKbps: uUL * sUL,
				})
			} else if len(parts) >= 4 {
				dnn := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				dl, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
				ul, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
				ambr.NR5G = append(ambr.NR5G, Nr5gAmbrEntry{
					DNN:    dnn,
					DLKbps: dl,
					ULKbps: ul,
				})
			}
		}
	}

	if !hasQuimslot && !hasCfun {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "read_failed",
			"message": "Unable to read cellular settings from modem",
		})
		return
	}

	var dualSlot *[]DualSlotEntry
	dualResp, dualErr := h.engine.Exec(`AT+QSIMCFG="dual_slot_status"`)
	if dualErr == nil && dualResp != nil {
		for _, l := range strings.Split(dualResp.Raw, "\n") {
			line := strings.TrimSpace(l)
			prefix := `+QSIMCFG: "dual_slot_status",`
			if strings.HasPrefix(line, prefix) {
				tokens := strings.Split(strings.TrimPrefix(line, prefix), ",")
				if len(tokens) >= 12 {
					slot1Active := strings.TrimSpace(tokens[1]) == "1"
					slot1ICCID := strings.Trim(strings.TrimSpace(tokens[5]), "\"")
					slot2Active := strings.TrimSpace(tokens[7]) == "1"
					slot2ICCID := strings.Trim(strings.TrimSpace(tokens[11]), "\"")

					slots := []DualSlotEntry{
						{Slot: 1, Active: slot1Active, ICCID: slot1ICCID},
						{Slot: 2, Active: slot2Active, ICCID: slot2ICCID},
					}
					dualSlot = &slots
					break
				}
			}
		}
	}

	JSON(w, http.StatusOK, CellularSettingsResponse{
		Success:  true,
		Settings: settings,
		Ambr:     ambr,
		DualSlot: dualSlot,
	})
}

// applySimSlot powers down the radio, switches SIM slot, powers up, and verifies with retry.
func (h *CellularSettingsHandler) applySimSlot(targetSlot int) error {
	if _, err := h.engine.Exec("AT+CFUN=0"); err != nil {
		return fmt.Errorf("failed to power down radio: %w", err)
	}

	if h.settleDelay > 0 {
		time.Sleep(h.settleDelay)
	}

	if _, err := h.engine.Exec(fmt.Sprintf("AT+QUIMSLOT=%d", targetSlot)); err != nil {
		return fmt.Errorf("failed to switch SIM slot: %w", err)
	}

	if h.settleDelay > 0 {
		time.Sleep(h.settleDelay)
	}

	if _, err := h.engine.Exec("AT+CFUN=1"); err != nil {
		if h.retryDelay > 0 {
			time.Sleep(h.retryDelay)
		}
		if _, errRetry := h.engine.Exec("AT+CFUN=1"); errRetry != nil {
			return fmt.Errorf("failed to power up radio: %w", errRetry)
		}
	}

	// Read-back verification loop: up to 5 attempts
	var currentSlot int
	expectedPrefix := fmt.Sprintf("+QUIMSLOT: %d", targetSlot)
	for attempt := 1; attempt <= 5; attempt++ {
		resp, err := h.engine.Exec("AT+QUIMSLOT?")
		if err == nil && resp != nil {
			if strings.Contains(resp.Raw, expectedPrefix) {
				return nil
			}
			for _, l := range strings.Split(resp.Raw, "\n") {
				line := strings.TrimSpace(l)
				if strings.HasPrefix(line, "+QUIMSLOT:") {
					parts := strings.Split(line, ":")
					if len(parts) >= 2 {
						val, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
						currentSlot = val
					}
				}
			}
		}
		if attempt < 5 && h.retryDelay > 0 {
			time.Sleep(h.retryDelay)
		}
	}

	return fmt.Errorf("slot switch verification failed: modem remained on slot %d", currentSlot)
}

// CellularSettingsApplyReq represents the payload for ApplySettings.
type CellularSettingsApplyReq struct {
	SimSlot        *int    `json:"sim_slot"`
	Cfun           *int    `json:"cfun"`
	ModePref       *string `json:"mode_pref"`
	Nr5gMode       *int    `json:"nr5g_mode"`
	RoamPref       *int    `json:"roam_pref"`
	SimDetect      *int    `json:"sim_detect"`
	SimDetectLevel *int    `json:"sim_detect_level"`
}

// ApplySettings handles POST /api/v1/cellular/settings and POST /cgi-bin/quecmanager/cellular/settings.sh
func (h *CellularSettingsHandler) ApplySettings(w http.ResponseWriter, r *http.Request) {
	var req CellularSettingsApplyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	appliedFields := make([]string, 0)
	failedFields := make([]string, 0)

	if req.SimSlot != nil {
		slot := *req.SimSlot
		if slot < 1 || slot > 2 {
			failedFields = append(failedFields, "sim_slot")
		} else {
			if err := h.applySimSlot(slot); err != nil {
				failedFields = append(failedFields, "sim_slot")
			} else {
				appliedFields = append(appliedFields, "sim_slot")
			}
		}
	}

	if req.Cfun != nil {
		cfun := *req.Cfun
		if _, err := h.engine.Exec(fmt.Sprintf("AT+CFUN=%d", cfun)); err != nil {
			failedFields = append(failedFields, "cfun")
		} else {
			appliedFields = append(appliedFields, "cfun")
		}
	}

	if req.ModePref != nil {
		modePref := *req.ModePref
		if _, err := h.engine.Exec(fmt.Sprintf(`AT+QNWPREFCFG="mode_pref",%s`, modePref)); err != nil {
			failedFields = append(failedFields, "mode_pref")
		} else {
			appliedFields = append(appliedFields, "mode_pref")
		}
	}

	if req.Nr5gMode != nil {
		nr5gMode := *req.Nr5gMode
		if _, err := h.engine.Exec(fmt.Sprintf(`AT+QNWPREFCFG="nr5g_disable_mode",%d`, nr5gMode)); err != nil {
			failedFields = append(failedFields, "nr5g_mode")
		} else {
			appliedFields = append(appliedFields, "nr5g_mode")
		}
	}

	if req.RoamPref != nil {
		roamPref := *req.RoamPref
		if _, err := h.engine.Exec(fmt.Sprintf(`AT+QNWPREFCFG="roam_pref",%d`, roamPref)); err != nil {
			failedFields = append(failedFields, "roam_pref")
		} else {
			appliedFields = append(appliedFields, "roam_pref")
		}
	}

	if req.SimDetect != nil {
		simDetect := *req.SimDetect
		simDetectLevel := 1
		if req.SimDetectLevel != nil {
			simDetectLevel = *req.SimDetectLevel
		}
		if _, err := h.engine.Exec(fmt.Sprintf("AT+QSIMDET=%d,%d", simDetect, simDetectLevel)); err != nil {
			failedFields = append(failedFields, "sim_detect")
		} else {
			appliedFields = append(appliedFields, "sim_detect")
		}
	}

	if len(failedFields) > 0 {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success":        len(appliedFields) > 0,
			"error":          "partial_failure",
			"message":        "Some cellular settings failed to apply",
			"applied_fields": appliedFields,
			"failed_fields":  failedFields,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"message":        "Cellular settings applied",
		"applied_fields": appliedFields,
	})
}
