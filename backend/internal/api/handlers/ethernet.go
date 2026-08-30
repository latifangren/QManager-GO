package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"qmanager/internal/platform"
)

// EthernetHandler manages Ethernet link status.
type EthernetHandler struct {
	ifacePath string
}

// NewEthernetHandler creates a new EthernetHandler.
func NewEthernetHandler() *EthernetHandler {
	return &EthernetHandler{
		ifacePath: "/sys/class/net/eth0",
	}
}

// HandleEthernet handles GET /cgi-bin/quecmanager/network/ethernet.sh and /api/network/ethernet
func (h *EthernetHandler) HandleEthernet(w http.ResponseWriter, r *http.Request) {
	linkUp := false
	speedMbps := 0
	duplex := "unknown"
	mtu := 1500

	if data, err := os.ReadFile(filepath.Join(h.ifacePath, "operstate")); err == nil {
		state := strings.TrimSpace(string(data))
		linkUp = state == "up"
	}

	if data, err := os.ReadFile(filepath.Join(h.ifacePath, "speed")); err == nil {
		if s, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && s > 0 {
			speedMbps = s
		}
	}

	if data, err := os.ReadFile(filepath.Join(h.ifacePath, "duplex")); err == nil {
		duplex = strings.TrimSpace(string(data))
	}

	if data, err := os.ReadFile(filepath.Join(h.ifacePath, "mtu")); err == nil {
		if m, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && m > 0 {
			mtu = m
		}
	}

	netStats, _ := platform.ReadNetworkStats("")
	ethStats := netStats["eth0"]

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"interface":  "eth0",
		"link_up":    linkUp,
		"speed_mbps": speedMbps,
		"duplex":     duplex,
		"mtu":        mtu,
		"rx_bytes":   ethStats.RxBytes,
		"tx_bytes":   ethStats.TxBytes,
		"rx_packets": ethStats.RxPackets,
		"tx_packets": ethStats.TxPackets,
		"rx_errors":  ethStats.RxErrors,
		"tx_errors":  ethStats.TxErrors,
	})
}
