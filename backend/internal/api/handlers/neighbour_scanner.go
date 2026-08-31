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
	neighbourScanPidFile    = "/tmp/qmanager_neighbour_scan.pid"
	neighbourScanResultFile = "/tmp/qmanager_neighbour_scan_result.json"
	neighbourScanErrorFile  = "/tmp/qmanager_neighbour_scan_error"
)

// NeighbourCell represents one neighbour cell from AT+QENG="neighbourcell".
type NeighbourCell struct {
	ID             string `json:"id"`
	NetworkType    string `json:"networkType"`
	EARFCN         int    `json:"earfcn"`
	PCI            int    `json:"pci"`
	RSRP           int    `json:"rsrp"`
	RSRQ           *int   `json:"rsrq,omitempty"`
	RSSI           *int   `json:"rssi,omitempty"`
	SINR           *int   `json:"sinr,omitempty"`
	Band           int    `json:"band,omitempty"`
}

// NeighbourScannerHandler manages AT+QENG="neighbourcell" queries.
type NeighbourScannerHandler struct {
	engine   *atengine.Engine
	mu       sync.Mutex
	scanning bool
}

// NewNeighbourScannerHandler creates a NeighbourScannerHandler.
func NewNeighbourScannerHandler(engine *atengine.Engine) *NeighbourScannerHandler {
	return &NeighbourScannerHandler{
		engine: engine,
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
	h.mu.Unlock()

	_ = os.Remove(neighbourScanResultFile)
	_ = os.Remove(neighbourScanErrorFile)
	_ = os.WriteFile(neighbourScanPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)

	go func() {
		defer func() {
			h.mu.Lock()
			h.scanning = false
			h.mu.Unlock()
			_ = os.Remove(neighbourScanPidFile)
		}()

		res, err := h.engine.Exec(`AT+QENG="neighbourcell"`)
		if err != nil || !strings.Contains(res.Raw, "+QENG:") {
			errMsg := "Neighbourcell command failed"
			if err != nil {
				errMsg = err.Error()
			}
			_ = os.WriteFile(neighbourScanErrorFile, []byte(errMsg), 0644)
			return
		}

		cells := ParseNeighbourCellOutput(res.Raw)
		data, err := json.MarshalIndent(cells, "", "  ")
		if err == nil {
			_ = os.WriteFile(neighbourScanResultFile, data, 0644)
		}
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Neighbour scan initiated",
	})
}

// ScanStatus handles GET /api/v1/cellular/neighbour/status and /cgi-bin/quecmanager/at_cmd/neighbour_scan_status.sh
func (h *NeighbourScannerHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	running := h.scanning
	h.mu.Unlock()

	if running {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "running",
		})
		return
	}

	if errData, err := os.ReadFile(neighbourScanErrorFile); err == nil && len(errData) > 0 {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "error",
			"error":  string(errData),
		})
		return
	}

	if resData, err := os.ReadFile(neighbourScanResultFile); err == nil {
		var items []NeighbourCell
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

// ParseNeighbourCellOutput parses AT+QENG="neighbourcell" responses.
// Example outputs:
// +QENG: "neighbourcell intra","LTE",1675,218,-85,-9,-62,0,18,0,-
// +QENG: "neighbourcell inter","LTE",1675,219,-88,-11,-65,0,15,0,-
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

		netType := strings.Trim(parts[1], "\" ")
		earfcn, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		pci, _ := strconv.Atoi(strings.TrimSpace(parts[3]))

		rsrp := -140
		var rsrqPtr, rssiPtr, sinrPtr *int

		if strings.EqualFold(netType, "LTE") {
			if len(parts) >= 5 {
				rsrp, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
			}
			if len(parts) >= 6 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[5])); err == nil {
					rsrqPtr = &v
				}
			}
			if len(parts) >= 7 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[6])); err == nil {
					rssiPtr = &v
				}
			}
			if len(parts) >= 9 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[8])); err == nil {
					sinrPtr = &v
				}
			}
		} else if strings.EqualFold(netType, "NR5G") {
			if len(parts) >= 5 {
				rsrp, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
			}
			if len(parts) >= 6 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[5])); err == nil {
					rsrqPtr = &v
				}
			}
			if len(parts) >= 7 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[6])); err == nil {
					sinrPtr = &v
				}
			}
		}

		cells = append(cells, NeighbourCell{
			ID:          fmt.Sprintf("n-%d", idx),
			NetworkType: netType,
			EARFCN:      earfcn,
			PCI:         pci,
			RSRP:        rsrp,
			RSRQ:        rsrqPtr,
			RSSI:        rssiPtr,
			SINR:        sinrPtr,
		})
		idx++
	}

	return cells
}
