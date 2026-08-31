package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"qmanager/internal/atengine"
)

// CellScanItem represents one detected cell.
type CellScanItem struct {
	ID             string `json:"id"`
	NetworkType    string `json:"networkType"`
	EARFCN         int    `json:"earfcn"`
	PCI            int    `json:"pci"`
	Band           int    `json:"band"`
	Bandwidth      int    `json:"bandwidth"`
	CellID         int    `json:"cellID"`
	TAC            int    `json:"tac"`
	SignalStrength int    `json:"signalStrength"`
	RSRQ           *int   `json:"rsrq"`
	MCC            int    `json:"mcc"`
	MNC            int    `json:"mnc"`
	Provider       string `json:"provider"`
	SCS            *int   `json:"scs,omitempty"`
}

// CellScannerHandler manages asynchronous AT+QSCAN sweeps in memory (RAM-First).
type CellScannerHandler struct {
	engine   *atengine.Engine
	mu       sync.RWMutex
	scanning bool
	status   string // "idle", "running", "complete", "error"
	results  []CellScanItem
	err      string
}

// NewCellScannerHandler creates a new CellScannerHandler.
func NewCellScannerHandler(engine *atengine.Engine) *CellScannerHandler {
	return &CellScannerHandler{
		engine: engine,
		status: "idle",
	}
}

// StartScan handles POST /api/v1/cellular/scanner/start and /cgi-bin/quecmanager/at_cmd/cell_scan_start.sh
func (h *CellScannerHandler) StartScan(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.scanning {
		h.mu.Unlock()
		Error(w, http.StatusConflict, "Scan already in progress")
		return
	}
	h.scanning = true
	h.status = "running"
	h.results = nil
	h.err = ""
	h.mu.Unlock()

	// Launch async scan
	go func() {
		defer func() {
			h.mu.Lock()
			h.scanning = false
			h.mu.Unlock()
		}()

		// Execute AT+QSCAN="all"
		res, err := h.engine.Exec(`AT+QSCAN="all"`)
		h.mu.Lock()
		defer h.mu.Unlock()

		if err != nil || !strings.Contains(res.Raw, "+QSCAN:") {
			errMsg := "QSCAN execution failed"
			if err != nil {
				errMsg = err.Error()
			}
			h.status = "error"
			h.err = errMsg
			return
		}

		cells := ParseQScanOutput(res.Raw)
		h.results = cells
		h.status = "complete"
		h.err = ""
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Cell scan initiated",
	})
}

// ScanStatus handles GET /api/v1/cellular/scanner/status and /cgi-bin/quecmanager/at_cmd/cell_scan_status.sh
func (h *CellScannerHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.scanning || h.status == "running" {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "running",
		})
		return
	}

	if h.status == "error" {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "error",
			"error":  h.err,
		})
		return
	}

	if h.status == "complete" {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status":  "complete",
			"results": h.results,
			"count":   len(h.results),
		})
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"status": "idle",
	})
}

// ParseQScanOutput parses AT+QSCAN response into CellScanItem slice.
// Hardware response format:
// +QSCAN: "LTE",<mcc>,<mnc>,<earfcn>,<pci>,<rsrp>,<rsrq>,<cellid>,<tac>,<bandwidth>,<band>
// +QSCAN: "NR5G",<mcc>,<mnc>,<arfcn>,<pci>,<rsrp>,<rsrq>,<cellid>,<tac>,<bandwidth>,<band>,<scs>
func ParseQScanOutput(raw string) []CellScanItem {
	var items []CellScanItem
	lines := strings.Split(raw, "\n")
	idx := 1

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "+QSCAN:") {
			continue
		}

		trimmed := strings.TrimPrefix(l, "+QSCAN:")
		parts := strings.Split(trimmed, ",")
		if len(parts) < 11 {
			continue
		}

		netType := strings.Trim(parts[0], "\" ")
		mcc, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		mnc, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		earfcn, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
		pci, _ := strconv.Atoi(strings.TrimSpace(parts[4]))
		rsrp, _ := strconv.Atoi(strings.TrimSpace(parts[5]))

		var rsrqPtr *int
		if r, err := strconv.Atoi(strings.TrimSpace(parts[6])); err == nil {
			rsrqPtr = &r
		}

		cellID, _ := strconv.Atoi(strings.TrimSpace(parts[7]))
		tac, _ := strconv.Atoi(strings.TrimSpace(parts[8]))
		bw, _ := strconv.Atoi(strings.TrimSpace(parts[9]))
		band, _ := strconv.Atoi(strings.TrimSpace(parts[10]))

		var scsPtr *int
		if len(parts) >= 12 {
			if scs, err := strconv.Atoi(strings.TrimSpace(parts[11])); err == nil {
				scsPtr = &scs
			}
		}

		provider := fmt.Sprintf("%03d/%02d", mcc, mnc)

		items = append(items, CellScanItem{
			ID:             strconv.Itoa(idx),
			NetworkType:    netType,
			EARFCN:         earfcn,
			PCI:            pci,
			Band:           band,
			Bandwidth:      bw,
			CellID:         cellID,
			TAC:            tac,
			SignalStrength: rsrp,
			RSRQ:           rsrqPtr,
			MCC:            mcc,
			MNC:            mnc,
			Provider:       provider,
			SCS:            scsPtr,
		})
		idx++
	}

	return items
}
