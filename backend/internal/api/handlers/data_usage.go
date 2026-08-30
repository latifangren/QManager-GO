package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"qmanager/internal/platform"
)

// DataUsageHandler manages in-memory data usage accounting without continuous flash writes.
type DataUsageHandler struct {
	mu           sync.Mutex
	accumulatedRx uint64
	accumulatedTx uint64
	lastResetTs   int64
	lastUpdateTs  int64
}

// NewDataUsageHandler creates a new DataUsageHandler.
func NewDataUsageHandler() *DataUsageHandler {
	now := time.Now().Unix()
	return &DataUsageHandler{
		lastResetTs:  now,
		lastUpdateTs: now,
	}
}

// GetDataUsed handles GET /cgi-bin/quecmanager/network/data_used.sh and /api/network/data-usage
func (h *DataUsageHandler) GetDataUsed(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	netStats, _ := platform.ReadNetworkStats("")
	var curRx, curTx uint64
	selectedIface := "rmnet_data0"

	if iface, ok := netStats["rmnet_data0"]; ok {
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	} else if iface, ok := netStats["wwan0"]; ok {
		selectedIface = "wwan0"
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	} else if iface, ok := netStats["eth0"]; ok {
		selectedIface = "eth0"
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	}

	h.lastUpdateTs = time.Now().Unix()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":              true,
		"accumulated_rx_bytes": h.accumulatedRx + curRx,
		"accumulated_tx_bytes": h.accumulatedTx + curTx,
		"selected_counter":     selectedIface,
		"last_update_ts":       h.lastUpdateTs,
		"last_reset_ts":        h.lastResetTs,
		"modem_reset_count":    0,
		"stale":                false,
	})
}

// ResetDataUsed handles POST /cgi-bin/quecmanager/network/data_used_reset.sh and /api/network/data-usage/reset
func (h *DataUsageHandler) ResetDataUsed(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	h.accumulatedRx = 0
	h.accumulatedTx = 0
	h.lastResetTs = time.Now().Unix()
	h.lastUpdateTs = h.lastResetTs

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Data usage counters reset successfully",
		"reset_at": h.lastResetTs,
	})
}
