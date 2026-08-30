package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	mtuFirewallFile = "/etc/firewall.user.mtu"
)

// NetworkMTUHandler manages WAN/interface MTU configurations.
type NetworkMTUHandler struct{}

// NewNetworkMTUHandler creates a NetworkMTUHandler.
func NewNetworkMTUHandler() *NetworkMTUHandler {
	return &NetworkMTUHandler{}
}

// GetMTU handles GET /api/v1/network/mtu and /cgi-bin/quecmanager/network/mtu.sh
func (h *NetworkMTUHandler) GetMTU(w http.ResponseWriter, r *http.Request) {
	currentMTU := 1500
	if mtu, err := getCurrentWANMTU(); err == nil && mtu > 0 {
		currentMTU = mtu
	}

	isEnabled := false
	var configuredMTU *int
	if data, err := os.ReadFile(mtuFirewallFile); err == nil && len(data) > 0 {
		isEnabled = true
		lines := strings.Split(string(data), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.Contains(l, "mtu") {
				parts := strings.Fields(l)
				for i, p := range parts {
					if p == "mtu" && i+1 < len(parts) {
						if val, err := strconv.Atoi(parts[i+1]); err == nil {
							configuredMTU = &val
							break
						}
					}
				}
			}
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"current_mtu":    currentMTU,
		"configured_mtu": configuredMTU,
		"is_enabled":     isEnabled,
	})
}

// MTUSavePayload represents the MTU request body.
type MTUSavePayload struct {
	MTU interface{} `json:"mtu"` // int (576-9000) or "disable"
}

// SetMTU handles POST /api/v1/network/mtu and /cgi-bin/quecmanager/network/mtu.sh
func (h *NetworkMTUHandler) SetMTU(w http.ResponseWriter, r *http.Request) {
	var payload MTUSavePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Check if disable
	if strVal, ok := payload.MTU.(string); ok && strings.EqualFold(strVal, "disable") {
		_ = os.Remove(mtuFirewallFile)
		// Reset interfaces to default 1500
		_ = applyMTUToInterfaces(1500)

		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Custom MTU disabled, reverted to default (1500)",
		})
		return
	}

	var mtuVal int
	if numVal, ok := payload.MTU.(float64); ok {
		mtuVal = int(numVal)
	} else if strVal, ok := payload.MTU.(string); ok {
		var err error
		mtuVal, err = strconv.Atoi(strVal)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid MTU value")
			return
		}
	} else {
		Error(w, http.StatusBadRequest, "MTU value is required")
		return
	}

	if mtuVal < 576 || mtuVal > 9000 {
		Error(w, http.StatusBadRequest, "MTU must be between 576 and 9000")
		return
	}

	// Persist to firewall file
	content := fmt.Sprintf("#!/bin/sh\n# QManager custom MTU config\nfor iface in /sys/class/net/rmnet_data*; do\n  [ -e \"$iface\" ] || continue\n  ip link set dev $(basename \"$iface\") mtu %d 2>/dev/null || true\ndone\n", mtuVal)
	_ = os.MkdirAll(filepath.Dir(mtuFirewallFile), 0755)
	_ = os.WriteFile(mtuFirewallFile, []byte(content), 0755)

	// Apply immediately to interfaces
	_ = applyMTUToInterfaces(mtuVal)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("MTU set to %d", mtuVal),
		"mtu":     mtuVal,
	})
}

func getCurrentWANMTU() (int, error) {
	// Look at /sys/class/net/rmnet_data*/mtu
	files, err := filepath.Glob("/sys/class/net/rmnet_data*/mtu")
	if err == nil && len(files) > 0 {
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err == nil {
				val, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err == nil && val > 0 {
					return val, nil
				}
			}
		}
	}
	return 1500, nil
}

func applyMTUToInterfaces(mtu int) error {
	files, _ := filepath.Glob("/sys/class/net/rmnet_data*")
	for _, f := range files {
		iface := filepath.Base(f)
		_ = exec.Command("ip", "link", "set", "dev", iface, "mtu", strconv.Itoa(mtu)).Run()
	}
	return nil
}
