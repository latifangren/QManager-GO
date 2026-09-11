package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/platform"
)

// EthernetSpeedLimitReq represents the request body for changing ethernet speed limit.
type EthernetSpeedLimitReq struct {
	SpeedLimit interface{} `json:"speed_limit"`
}

// EthernetHandler manages Ethernet link status.
type EthernetHandler struct {
	mu         sync.Mutex
	ifacePath  string
	speedLimit string
}

// NewEthernetHandler creates a new EthernetHandler.
func NewEthernetHandler() *EthernetHandler {
	return &EthernetHandler{
		ifacePath:  "/sys/class/net/eth0",
		speedLimit: "auto",
	}
}

// HandleEthernet handles GET and POST for /cgi-bin/quecmanager/network/ethernet.sh and /api/network/ethernet
func (h *EthernetHandler) HandleEthernet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.handlePostEthernet(w, r)
		return
	}

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

	h.mu.Lock()
	savedLimit := h.speedLimit
	if savedLimit == "" {
		savedLimit = "auto"
	}
	h.mu.Unlock()

	autoNeg := "on"
	if savedLimit != "auto" {
		autoNeg = "off"
	}

	interfacePresent := true
	if _, err := os.Stat(h.ifacePath); os.IsNotExist(err) {
		interfacePresent = false
	}

	linkStatus := "down"
	if linkUp {
		linkStatus = "up"
	}

	speedStr := "Unknown"
	if speedMbps > 0 {
		speedStr = fmt.Sprintf("%dMb/s", speedMbps)
	}

	ifaceName := filepath.Base(h.ifacePath)
	if ifaceName == "" || ifaceName == "." || ifaceName == "/" {
		ifaceName = "eth0"
	}

	netStats, _ := platform.ReadNetworkStats("")
	ethStats := netStats[ifaceName]

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":                   true,
		"interface":                 ifaceName,
		"interface_present":         interfacePresent,
		"link_up":                   linkUp,
		"link_status":               linkStatus,
		"speed":                     speedStr,
		"speed_mbps":                speedMbps,
		"duplex":                    duplex,
		"auto_negotiation":          autoNeg,
		"speed_limit":               savedLimit,
		"supports_2500":             true,
		"disconnect_window_seconds": 8,
		"mtu":                       mtu,
		"rx_bytes":                  ethStats.RxBytes,
		"tx_bytes":                  ethStats.TxBytes,
		"rx_packets":                ethStats.RxPackets,
		"tx_packets":                ethStats.TxPackets,
		"rx_errors":                 ethStats.RxErrors,
		"tx_errors":                 ethStats.TxErrors,
	})
}

func (h *EthernetHandler) handlePostEthernet(w http.ResponseWriter, r *http.Request) {
	var req EthernetSpeedLimitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if req.SpeedLimit == nil {
		Error(w, http.StatusBadRequest, "Invalid speed_limit: must be auto, 10, 100, 1000, or 2500")
		return
	}

	var speedLimit string
	switch v := req.SpeedLimit.(type) {
	case string:
		speedLimit = strings.TrimSpace(v)
		if speedLimit == "0" {
			speedLimit = "auto"
		}
	case float64:
		intVal := int(v)
		if intVal == 0 {
			speedLimit = "auto"
		} else {
			speedLimit = strconv.Itoa(intVal)
		}
	case int:
		if v == 0 {
			speedLimit = "auto"
		} else {
			speedLimit = strconv.Itoa(v)
		}
	case int64:
		if v == 0 {
			speedLimit = "auto"
		} else {
			speedLimit = strconv.FormatInt(v, 10)
		}
	default:
		Error(w, http.StatusBadRequest, "Invalid speed_limit: must be auto, 10, 100, 1000, or 2500")
		return
	}

	// Whitelist speed_limit: must be "auto", "10", "100", "1000", or "2500"
	switch speedLimit {
	case "auto", "10", "100", "1000", "2500":
		// valid
	default:
		Error(w, http.StatusBadRequest, "Invalid speed_limit: must be auto, 10, 100, 1000, or 2500")
		return
	}

	h.mu.Lock()
	h.speedLimit = speedLimit
	h.mu.Unlock()

	ifaceName := filepath.Base(h.ifacePath)
	if ifaceName == "" || ifaceName == "." || ifaceName == "/" {
		ifaceName = "eth0"
	}

	// If ethtool exists and on Linux, invoke ethtool to set speed/autoneg with a 3-second timeout
	if runtime.GOOS == "linux" {
		if ethtoolPath, err := exec.LookPath("ethtool"); err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if speedLimit == "auto" {
				_ = exec.CommandContext(ctx, ethtoolPath, "-s", ifaceName, "autoneg", "on").Run()
			} else {
				_ = exec.CommandContext(ctx, ethtoolPath, "-s", ifaceName, "speed", speedLimit, "duplex", "full", "autoneg", "off").Run()
			}
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":                   true,
		"interface":                 ifaceName,
		"speed_limit":               speedLimit,
		"disconnect_window_seconds": 8,
		"message":                   "Ethernet speed updated",
	})
}
