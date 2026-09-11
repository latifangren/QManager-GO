package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/atengine"
)

// NeighbourCell represents one neighbour cell from AT+QENG="neighbourcell" matching frontend NeighbourCellResult.
type NeighbourCell struct {
	ID             string `json:"id"`
	NetworkType    string `json:"networkType"`
	CellType       string `json:"cellType"`
	Frequency      int    `json:"frequency"`
	EARFCN         int    `json:"earfcn"`
	PCI            int    `json:"pci"`
	SignalStrength int    `json:"signalStrength"`
	RSRP           int    `json:"rsrp"`
	RSRQ           *int   `json:"rsrq,omitempty"`
	RSSI           *int   `json:"rssi,omitempty"`
	SINR           *int   `json:"sinr,omitempty"`
	Band           int    `json:"band,omitempty"`
}

// NeighbourScannerHandler manages AT+QENG="neighbourcell" queries in memory (RAM-First).
type NeighbourScannerHandler struct {
	engine   *atengine.Engine
	mu       sync.RWMutex
	scanning bool
	status   string // "idle", "running", "complete", "error"
	results  []NeighbourCell
	err      string
}

// NewNeighbourScannerHandler creates a NeighbourScannerHandler.
func NewNeighbourScannerHandler(engine *atengine.Engine) *NeighbourScannerHandler {
	return &NeighbourScannerHandler{
		engine: engine,
		status: "idle",
	}
}

// StartScan handles POST /api/v1/cellular/neighbour/start and /cgi-bin/quecmanager/at_cmd/neighbour_scan_start.sh
func (h *NeighbourScannerHandler) StartScan(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.scanning {
		h.mu.Unlock()
		Error(w, http.StatusConflict, "Neighbour scan already in progress")
		return
	}
	h.scanning = true
	h.status = "running"
	h.results = nil
	h.err = ""
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			h.scanning = false
			h.mu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		res, err := h.engine.ExecContextWithPriority(ctx, `AT+QENG="neighbourcell"`, atengine.PriorityHigh)
		h.mu.Lock()
		defer h.mu.Unlock()

		if err != nil || !strings.Contains(res.Raw, "+QENG:") {
			errMsg := "Neighbourcell command failed"
			if err != nil {
				errMsg = err.Error()
			}
			h.status = "error"
			h.err = errMsg
			return
		}

		cells := ParseNeighbourCellOutput(res.Raw)
		h.results = cells
		h.status = "complete"
		h.err = ""
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Neighbour scan initiated",
	})
}

// ScanStatus handles GET /api/v1/cellular/neighbour/status and /cgi-bin/quecmanager/at_cmd/neighbour_scan_status.sh
func (h *NeighbourScannerHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
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

func parseSignedInt(val string) (int, bool) {
	val = strings.TrimSpace(val)
	if val == "" || val == "-" {
		return 0, false
	}
	v, err := strconv.Atoi(val)
	return v, err == nil
}

// ParseNeighbourCellOutput parses AT+QENG="neighbourcell" responses.
// Example outputs:
// +QENG: "neighbourcell intra","LTE",325,181,-17,-109,-81,-,-,-,-,-,-
// +QENG: "neighbourcell inter","LTE",1325,418,-20,-107,-77,-,-,-,-,-
// +QENG: "neighbourcell","NR5G",504990,123,-80,-10,15
func ParseNeighbourCellOutput(raw string) []NeighbourCell {
	var cells []NeighbourCell
	lines := strings.Split(raw, "\n")
	idx := 1

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "+QENG:") || !strings.Contains(l, "neighbourcell") {
			continue
		}

		trimmed := strings.TrimPrefix(l, "+QENG:")
		parts := strings.Split(trimmed, ",")
		if len(parts) < 4 {
			continue
		}

		firstTag := strings.Trim(parts[0], "\" ")
		netType := strings.Trim(parts[1], "\" ")

		cellType := "inter"
		if strings.Contains(strings.ToLower(firstTag), "intra") {
			cellType = "intra"
		} else if strings.EqualFold(netType, "NR5G") || strings.EqualFold(netType, "NR5G-NSA") {
			cellType = "nr5g"
		}

		earfcn, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		pci, _ := strconv.Atoi(strings.TrimSpace(parts[3]))

		rsrp := 0
		var rsrqPtr, rssiPtr, sinrPtr *int

		if strings.EqualFold(netType, "LTE") {
			var val4, val5 *int
			if len(parts) >= 5 {
				if v, ok := parseSignedInt(parts[4]); ok {
					val4 = &v
				}
			}
			if len(parts) >= 6 {
				if v, ok := parseSignedInt(parts[5]); ok {
					val5 = &v
				}
			}

			if val4 != nil && val5 != nil {
				if *val4 < -40 && *val5 >= -40 {
					rsrp = *val4
					rsrqPtr = val5
				} else if *val4 >= -40 && *val5 < -40 {
					rsrqPtr = val4
					rsrp = *val5
				} else {
					rsrp = *val4
					rsrqPtr = val5
				}
			} else if val4 != nil {
				rsrp = *val4
			}

			if len(parts) >= 7 {
				if v, ok := parseSignedInt(parts[6]); ok {
					rssiPtr = &v
				}
			}
			if len(parts) >= 9 {
				if v, ok := parseSignedInt(parts[8]); ok {
					sinrPtr = &v
				}
			}
		} else if strings.EqualFold(netType, "NR5G") || strings.EqualFold(netType, "NR5G-NSA") {
			// NR5G: parts[4] = rsrp, parts[5] = rsrq, parts[6] = sinr
			if len(parts) >= 5 {
				if v, ok := parseSignedInt(parts[4]); ok {
					rsrp = v
				}
			}
			if len(parts) >= 6 {
				if v, ok := parseSignedInt(parts[5]); ok {
					rsrqPtr = &v
				}
			}
			if len(parts) >= 7 {
				if v, ok := parseSignedInt(parts[6]); ok {
					sinrPtr = &v
				}
			}
		}

		// Calculate band from EARFCN if available
		band := 0
		if earfcn > 0 {
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

		cells = append(cells, NeighbourCell{
			ID:             strconv.Itoa(idx),
			NetworkType:    netType,
			CellType:       cellType,
			Frequency:      earfcn,
			EARFCN:         earfcn,
			PCI:            pci,
			SignalStrength: rsrp,
			RSRP:           rsrp,
			RSRQ:           rsrqPtr,
			RSSI:           rssiPtr,
			SINR:           sinrPtr,
			Band:           band,
		})
		idx++
	}

	return cells
}
