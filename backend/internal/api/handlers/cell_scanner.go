package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"qmanager/internal/atengine"
)

var (
	cellScanPidFile    = "/tmp/qmanager_cell_scan.pid"
	cellScanResultFile = "/tmp/qmanager_cell_scan_result.json"
	cellScanErrorFile  = "/tmp/qmanager_cell_scan_error"
	longRunningFlag    = "/tmp/qmanager_long_running"
)

// CellScanItem represents one detected cell.
type CellScanItem struct {
	ID             string   `json:"id"`
	NetworkType    string   `json:"networkType"`
	EARFCN         int      `json:"earfcn"`
	PCI            int      `json:"pci"`
	Band           int      `json:"band"`
	Bandwidth      int      `json:"bandwidth"`
	CellID         int      `json:"cellID"`
	TAC            int      `json:"tac"`
	SignalStrength int      `json:"signalStrength"`
	RSRQ           *int     `json:"rsrq"`
	MCC            int      `json:"mcc"`
	MNC            int      `json:"mnc"`
	Provider       string   `json:"provider"`
	SCS            *int     `json:"scs,omitempty"`
}

// CellScannerHandler manages asynchronous AT+QSCAN sweeps.
type CellScannerHandler struct {
	engine   *atengine.Engine
	mu       sync.Mutex
	scanning bool
}

// NewCellScannerHandler creates a new CellScannerHandler.
func NewCellScannerHandler(engine *atengine.Engine) *CellScannerHandler {
	return &CellScannerHandler{
		engine: engine,
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
	h.mu.Unlock()

	// Clear previous result and error
	_ = os.Remove(cellScanResultFile)
	_ = os.Remove(cellScanErrorFile)
	_ = os.WriteFile(cellScanPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	_ = os.WriteFile(longRunningFlag, []byte("cell_scanner"), 0644)

	// Launch async scan
	go func() {
		defer func() {
			h.mu.Lock()
			h.scanning = false
			h.mu.Unlock()
			_ = os.Remove(cellScanPidFile)
			_ = os.Remove(longRunningFlag)
		}()

		// Execute AT+QSCAN="all"
		res, err := h.engine.Exec(`AT+QSCAN="all"`)
		if err != nil || !strings.Contains(res.Raw, "+QSCAN:") {
			errMsg := "QSCAN execution failed"
			if err != nil {
				errMsg = err.Error()
			}
			_ = os.WriteFile(cellScanErrorFile, []byte(errMsg), 0644)
			return
		}

		cells := ParseQScanOutput(res.Raw)
		data, err := json.MarshalIndent(cells, "", "  ")
		if err == nil {
			_ = os.WriteFile(cellScanResultFile, data, 0644)
		}
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Cell scan initiated",
	})
}

// ScanStatus handles GET /api/v1/cellular/scanner/status and /cgi-bin/quecmanager/at_cmd/cell_scan_status.sh
func (h *CellScannerHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	running := h.scanning
	h.mu.Unlock()

	if running {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "running",
		})
		return
	}

	if errData, err := os.ReadFile(cellScanErrorFile); err == nil && len(errData) > 0 {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "error",
			"error":  string(errData),
		})
		return
	}

	if resData, err := os.ReadFile(cellScanResultFile); err == nil {
		var items []CellScanItem
		if err := json.Unmarshal(resData, &items); err == nil {
			JSON(w, http.StatusOK, map[string]interface{}{
				"status":  "complete",
				"results": items,
				"count":   len(items),
			})
			return
		}
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
