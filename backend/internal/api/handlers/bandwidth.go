package handlers

import (
	"encoding/json"
	"net/http"

	"qmanager/internal/telemetry/bandwidth"
)

// BandwidthHandler serves real-time and historical bandwidth statistics.
type BandwidthHandler struct {
	collector *bandwidth.Collector
}

// NewBandwidthHandler creates a new BandwidthHandler.
func NewBandwidthHandler(col *bandwidth.Collector) *BandwidthHandler {
	return &BandwidthHandler{
		collector: col,
	}
}

// GetBandwidth handles GET /api/v1/monitoring/bandwidth and /cgi-bin/quecmanager/monitoring/bandwidth.sh.
// Supports optional query parameter: ?interface=rmnet_data0.
func (h *BandwidthHandler) GetBandwidth(w http.ResponseWriter, r *http.Request) {
	if h.collector == nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data":    bandwidth.BandwidthSnapshot{},
		})
		return
	}

	snap := h.collector.GetSnapshot()
	ifaceQuery := r.URL.Query().Get("interface")

	if ifaceQuery != "" {
		filteredInterfaces := make(map[string]bandwidth.IfaceSnapshot)
		if iface, ok := snap.Interfaces[ifaceQuery]; ok {
			filteredInterfaces[ifaceQuery] = iface
		}
		snap.Interfaces = filteredInterfaces
		snap.DefaultInterface = ifaceQuery
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    snap,
	})
}

// ResetBandwidth handles POST /api/v1/monitoring/bandwidth/reset and /cgi-bin/quecmanager/monitoring/bandwidth_reset.sh.
func (h *BandwidthHandler) ResetBandwidth(w http.ResponseWriter, r *http.Request) {
	if h.collector == nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Collector not initialized",
		})
		return
	}

	var payload struct {
		Interface string `json:"interface"`
	}

	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}
	if payload.Interface == "" {
		payload.Interface = r.URL.Query().Get("interface")
	}

	h.collector.Reset(payload.Interface)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"message":   "Bandwidth counters reset",
		"interface": payload.Interface,
	})
}
