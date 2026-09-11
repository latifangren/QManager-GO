package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	// Launch async scan with 120s timeout and PriorityHigh
	go func() {
		defer func() {
			h.mu.Lock()
			h.scanning = false
			h.mu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Execute AT+QSCAN=3,1 (Full sweep mode)
		res, err := h.engine.ExecContextWithPriority(ctx, `AT+QSCAN=3,1`, atengine.PriorityHigh)
		if err != nil || !strings.Contains(res.Raw, "+QSCAN:") {
			// Fallback to AT+QSCAN=1 if 3,1 is unsupported on this firmware
			res, err = h.engine.ExecContextWithPriority(ctx, `AT+QSCAN=1`, atengine.PriorityHigh)
		}

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

func parseHexOrDec(val string) int {
	val = strings.TrimSpace(val)
	if val == "" || val == "-" {
		return 0
	}
	// Try parsing hex first if it has letters or looks like hex
	if strings.HasPrefix(val, "0x") || strings.HasPrefix(val, "0X") {
		if n, err := strconv.ParseInt(val[2:], 16, 64); err == nil {
			return int(n)
		}
	}
	// If it contains A-F hex chars
	hasHexChar := false
	for _, c := range val {
		if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hasHexChar = true
			break
		}
	}
	if hasHexChar {
		if n, err := strconv.ParseInt(val, 16, 64); err == nil {
			return int(n)
		}
	}
	if n, err := strconv.Atoi(val); err == nil {
		return n
	}
	if n, err := strconv.ParseInt(val, 16, 64); err == nil {
		return int(n)
	}
	return 0
}

func parseBandwidth(val string) int {
	val = strings.TrimSpace(val)
	if val == "" || val == "-" {
		return 0
	}
	bw, _ := strconv.Atoi(val)
	// Normalise LTE resource blocks to MHz: 100->20, 75->15, 50->10, 25->5, 15->3, 6->1
	switch bw {
	case 100:
		return 20
	case 75:
		return 15
	case 50:
		return 10
	case 25:
		return 5
	case 15:
		return 3
	case 6:
		return 1
	default:
		return bw
	}
}

// ParseQScanOutput parses AT+QSCAN response into CellScanItem slice.
// Hardware response format:
// +QSCAN: "LTE",<mcc>,<mnc>,<earfcn>,<pci>,<rsrp>,<rsrq>,<srxlev>,<s_qual>,<cellid_hex>,<tac_hex>,<bandwidth>,<band>
// +QSCAN: "LTE",<mcc>,<mnc>,<earfcn>,<pci>,<rsrp>,<rsrq>,<cellid>,<tac>,<bandwidth>,<band>
// +QSCAN: "NR5G",<mcc>,<mnc>,<arfcn>,<pci>,<rsrp>,<rsrq>,<sinr>,<s_qual>,<cellid_hex>,<tac_hex>,<bandwidth>,<band>,<scs>
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
		if len(parts) < 7 {
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

		var cellID, tac, bw, band int
		var scsPtr *int

		if len(parts) >= 13 {
			// Format: "LTE",mcc,mnc,earfcn,pci,rsrp,rsrq,srxlev,s_qual,cellid,tac,bw,band,[scs]
			cellID = parseHexOrDec(parts[9])
			tac = parseHexOrDec(parts[10])
			bw = parseBandwidth(parts[11])
			band, _ = strconv.Atoi(strings.TrimSpace(parts[12]))
			if len(parts) >= 14 {
				if scs, err := strconv.Atoi(strings.TrimSpace(parts[13])); err == nil {
					scsPtr = &scs
				}
			}
		} else if len(parts) >= 11 {
			// Format: "LTE",mcc,mnc,earfcn,pci,rsrp,rsrq,cellid,tac,bw,band
			cellID = parseHexOrDec(parts[7])
			tac = parseHexOrDec(parts[8])
			bw = parseBandwidth(parts[9])
			band, _ = strconv.Atoi(strings.TrimSpace(parts[10]))
			if len(parts) >= 12 {
				if scs, err := strconv.Atoi(strings.TrimSpace(parts[11])); err == nil {
					scsPtr = &scs
				}
			}
		}

		// Fallback band calculation if band not reported
		if band <= 0 {
			if strings.EqualFold(netType, "LTE") {
				calc := CalculateLTEFrequency(earfcn)
				if len(calc.MatchingBands) > 0 {
					bStr := strings.TrimPrefix(calc.MatchingBands[0].Band, "B")
					band, _ = strconv.Atoi(bStr)
				}
			} else {
				calc := CalculateNRFrequency(earfcn)
				if len(calc.MatchingBands) > 0 {
					bStr := strings.TrimPrefix(calc.MatchingBands[0].Band, "n")
					band, _ = strconv.Atoi(bStr)
				}
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
