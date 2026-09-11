package handlers

import (
	"net/http"
	"sync"
	"time"

	"qmanager/internal/platform"
)

// DataUsageHandler manages in-memory data usage accounting without continuous flash writes.
type DataUsageHandler struct {
	mu            sync.Mutex
	netDevPath    string
	lastRx        uint64
	lastTx        uint64
	accumulatedRx uint64
	accumulatedTx uint64
	resetCount    int
	lastResetTs   int64
	lastUpdateTs  int64
	initialized   bool
}

// NewDataUsageHandler creates a new DataUsageHandler.
func NewDataUsageHandler(optionalPath ...string) *DataUsageHandler {
	now := time.Now().Unix()
	var path string
	if len(optionalPath) > 0 {
		path = optionalPath[0]
	}
	return &DataUsageHandler{
		netDevPath:   path,
		lastResetTs:  now,
		lastUpdateTs: now,
	}
}

// SetNetDevPath sets custom /proc/net/dev path for testing.
func (h *DataUsageHandler) SetNetDevPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.netDevPath = path
}

// GetDataUsed handles GET /cgi-bin/quecmanager/network/data_used.sh and /api/network/data-usage
func (h *DataUsageHandler) GetDataUsed(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	netStats, _ := platform.ReadNetworkStats(h.netDevPath)
	var curRx, curTx uint64
	selectedIface := "none"

	if iface, ok := netStats["rmnet_ipa0"]; ok {
		selectedIface = "rmnet_ipa0"
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	} else if iface, ok := netStats["rmnet_data0"]; ok {
		selectedIface = "rmnet_data0"
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	} else if iface, ok := netStats["wwan0"]; ok {
		selectedIface = "wwan0"
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	}

	if selectedIface != "none" {
		if !h.initialized {
			h.lastRx = curRx
			h.lastTx = curTx
			h.initialized = true
		} else {
			if curRx >= h.lastRx {
				h.accumulatedRx += (curRx - h.lastRx)
			} else {
				// Interface reset or counter rollover
				h.accumulatedRx += curRx
				h.resetCount++
			}
			h.lastRx = curRx

			if curTx >= h.lastTx {
				h.accumulatedTx += (curTx - h.lastTx)
			} else {
				h.accumulatedTx += curTx
			}
			h.lastTx = curTx
		}
	}

	h.lastUpdateTs = time.Now().Unix()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":              true,
		"accumulated_rx_bytes": h.accumulatedRx,
		"accumulated_tx_bytes": h.accumulatedTx,
		"selected_counter":     selectedIface,
		"last_update_ts":       h.lastUpdateTs,
		"last_reset_ts":        h.lastResetTs,
		"modem_reset_count":    h.resetCount,
		"stale":                false,
	})
}

// ResetDataUsed handles POST /cgi-bin/quecmanager/network/data_used_reset.sh and /api/network/data-usage/reset
func (h *DataUsageHandler) ResetDataUsed(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	netStats, _ := platform.ReadNetworkStats(h.netDevPath)
	var curRx, curTx uint64
	if iface, ok := netStats["rmnet_ipa0"]; ok {
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	} else if iface, ok := netStats["rmnet_data0"]; ok {
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	} else if iface, ok := netStats["wwan0"]; ok {
		curRx = iface.RxBytes
		curTx = iface.TxBytes
	}

	h.accumulatedRx = 0
	h.accumulatedTx = 0
	h.lastRx = curRx
	h.lastTx = curTx
	h.initialized = true
	h.lastResetTs = time.Now().Unix()
	h.lastUpdateTs = h.lastResetTs

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "Data usage counters reset",
		"reset_at": h.lastResetTs,
	})
}
