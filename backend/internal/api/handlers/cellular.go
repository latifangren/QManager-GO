package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"qmanager/internal/atengine"
	"qmanager/internal/telemetry"
)

// CellularHandler handles cellular telemetry, AT queries, and locks.
type CellularHandler struct {
	engine *atengine.Engine
	poller *telemetry.Poller
}

// NewCellularHandler creates a CellularHandler.
func NewCellularHandler(eng *atengine.Engine, poller *telemetry.Poller) *CellularHandler {
	return &CellularHandler{
		engine: eng,
		poller: poller,
	}
}

// Status returns current telemetry from poller.
func (h *CellularHandler) Status(w http.ResponseWriter, r *http.Request) {
	status := h.poller.GetStatus()
	JSON(w, http.StatusOK, status)
}

type SendATRequest struct {
	Command string `json:"command"`
}

// SendCommand executes a direct AT command via engine queue.
func (h *CellularHandler) SendCommand(w http.ResponseWriter, r *http.Request) {
	var req SendATRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		Error(w, http.StatusBadRequest, "Invalid or missing 'command' field")
		return
	}

	res, err := h.engine.Exec(req.Command)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success":  false,
			"response": res.Raw,
			"error":    err.Error(),
			"command":  req.Command,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"response": res.Raw,
		"command":  req.Command,
	})
}

// GetBands returns active band locking configuration.
func (h *CellularHandler) GetBands(w http.ResponseWriter, r *http.Request) {
	res, err := h.engine.Exec(`AT+QNWPREFCFG="gw_band";+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to read band configuration")
		return
	}

	Success(w, map[string]interface{}{
		"raw": res.Raw,
	})
}

type SetBandsRequest struct {
	LTEBands []string `json:"lte_bands"`
	NRBands  []string `json:"nr_bands"`
}

// LockBands applies band mask settings.
func (h *CellularHandler) LockBands(w http.ResponseWriter, r *http.Request) {
	var req SetBandsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid band configuration payload")
		return
	}

	var cmds []string
	if len(req.LTEBands) > 0 {
		lteMask := strings.Join(req.LTEBands, ":")
		cmds = append(cmds, fmt.Sprintf(`AT+QNWPREFCFG="lte_band",%s`, lteMask))
	}
	if len(req.NRBands) > 0 {
		nrMask := strings.Join(req.NRBands, ":")
		cmds = append(cmds, fmt.Sprintf(`AT+QNWPREFCFG="nr5g_band",%s`, nrMask))
	}

	for _, cmd := range cmds {
		if _, err := h.engine.Exec(cmd); err != nil {
			Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed executing %s: %v", cmd, err))
			return
		}
	}

	Success(w, map[string]string{"message": "Bands updated successfully"})
}

// LockTower handles PCI/EARFCN locking via AT+QNWLOCK.
func (h *CellularHandler) LockTower(w http.ResponseWriter, r *http.Request) {
	type TowerLockReq struct {
		Mode   string `json:"mode"`   // "4g" or "5g"
		EARFCN int    `json:"earfcn"`
		PCID   int    `json:"pcid"`
		SCS    int    `json:"scs,omitempty"` // For 5G NR
	}

	var req TowerLockReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid tower lock payload")
		return
	}

	var atCmd string
	if req.Mode == "5g" {
		scs := req.SCS
		if scs == 0 {
			scs = 30 // Default 30kHz for Sub-6
		}
		atCmd = fmt.Sprintf(`AT+QNWLOCK="common/5g",1,%d,%d,%d`, req.EARFCN, req.PCID, scs)
	} else {
		atCmd = fmt.Sprintf(`AT+QNWLOCK="common/4g",1,%d,%d`, req.EARFCN, req.PCID)
	}

	if _, err := h.engine.Exec(atCmd); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Tower lock failed: %v", err))
		return
	}

	Success(w, map[string]string{"message": "Tower locked successfully"})
}

// UnlockTower resets cell locks.
func (h *CellularHandler) UnlockTower(w http.ResponseWriter, r *http.Request) {
	cmds := []string{
		`AT+QNWLOCK="common/4g",0`,
		`AT+QNWLOCK="common/5g",0`,
		`AT+QNWLOCK="save_ctrl",0`,
	}

	for _, c := range cmds {
		_, _ = h.engine.Exec(c)
	}

	Success(w, map[string]string{"message": "Tower locks cleared"})
}
