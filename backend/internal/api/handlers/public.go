package handlers

import (
	"net/http"
	"os"
	"strings"
	"time"

	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

// PublicHandler handles unauthenticated public overview and preference endpoints.
type PublicHandler struct {
	poller   *telemetry.Poller
	cfgMgr   *config.Manager
	identity platform.Identity
}

// NewPublicHandler creates a new PublicHandler.
func NewPublicHandler(poller *telemetry.Poller, cfgMgr *config.Manager, id platform.Identity) *PublicHandler {
	return &PublicHandler{
		poller:   poller,
		cfgMgr:   cfgMgr,
		identity: id,
	}
}

// Overview handles GET /cgi-bin/quecmanager/public/overview.sh and /api/public/overview
func (h *PublicHandler) Overview(w http.ResponseWriter, r *http.Request) {
	status := h.poller.GetStatus()
	metrics := platform.GetSystemMetrics()

	var bands []map[string]interface{}
	for _, cc := range status.CA {
		bands = append(bands, map[string]interface{}{
			"band": cc.Band,
			"pci":  cc.PCI,
			"rsrp": cc.RSRP,
			"rsrq": cc.RSRQ,
			"sinr": cc.SINR,
		})
	}
	if len(bands) == 0 && status.Band != "" {
		bands = append(bands, map[string]interface{}{
			"band": status.Band,
			"pci":  status.PCID,
			"rsrp": status.RSRP,
			"rsrq": status.RSRQ,
			"sinr": status.SINR,
		})
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"state":            "ok",
		"timestamp":        time.Now().Unix(),
		"modem_reachable":  true,
		"uptime_seconds":   metrics.UptimeSeconds,
		"temperature":      metrics.CpuTempC,
		"network": map[string]interface{}{
			"type":           status.Mode,
			"service_status": status.Network.ServiceStatus,
			"carrier":        status.Operator,
			"bands":          bands,
			"lte_state":      status.LTE.State,
			"nr_state":       status.NR.State,
		},
		"signal": map[string]interface{}{
			"rsrp": status.RSRP,
			"rsrq": status.RSRQ,
			"sinr": status.SINR,
		},
	})
}

// Hostname handles GET /cgi-bin/quecmanager/public/hostname.sh and /api/public/hostname
func (h *PublicHandler) Hostname(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	hn := cfg.Settings.Hostname
	if hn == "" {
		if sysHn, err := os.Hostname(); err == nil && sysHn != "" {
			hn = sysHn
		} else {
			hn = "QManager"
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"hostname": strings.TrimSpace(hn),
	})
}

// Units handles GET /cgi-bin/quecmanager/public/units.sh and /api/public/units
func (h *PublicHandler) Units(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	tempUnit := cfg.Settings.TempUnit
	if tempUnit == "" {
		tempUnit = "celsius"
	}
	distUnit := cfg.Settings.DistanceUnit
	if distUnit == "" {
		distUnit = "km"
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"temp_unit":     tempUnit,
		"distance_unit": distUnit,
	})
}
