package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

	"qmanager/internal/config"
	"qmanager/internal/platform"
)

// SystemHandler provides system metrics, hardware profile, and reboot controls.
type SystemHandler struct {
	identity platform.Identity
	cfgMgr   *config.Manager
}

// NewSystemHandler creates a SystemHandler.
func NewSystemHandler(id platform.Identity, cfgMgr *config.Manager) *SystemHandler {
	return &SystemHandler{
		identity: id,
		cfgMgr:   cfgMgr,
	}
}

// Info returns device platform info & identity.
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
	metrics := platform.GetSystemMetrics()

	manufacturer := "Quectel"
	if h.identity.CustomName != "" && h.identity.CustomName != "unknown" && h.identity.CustomName != "STD" {
		manufacturer = h.identity.CustomName
	}

	lteRelease := "Rel 15"
	nrRelease := "Rel 15"
	if h.identity.IsSDX65 {
		lteRelease = "Rel 16"
		nrRelease = "Rel 16"
	}

	lanGateway := platform.GetDefaultGatewayIP()
	wanIPv4, wanIPv6 := platform.GetInterfaceIP("rmnet_data0")
	if wanIPv4 == "" && wanIPv6 == "" {
		wanIPv4, wanIPv6 = platform.GetInterfaceIP("rmnet_mhi0")
	}
	if wanIPv4 == "" && wanIPv6 == "" {
		wanIPv4, wanIPv6 = platform.GetInterfaceIP("wwan0")
	}

	hostname := platform.GetHostname()
	kernelVersion := platform.GetKernelVersion()
	osVersion := platform.GetOSVersion()

	payload := map[string]interface{}{
		"success": true,
		"device": map[string]interface{}{
			"model":        h.identity.Model,
			"manufacturer": manufacturer,
			"firmware":     h.identity.Revision,
			"serial":       h.identity.Serial,
			"build_date":   "",
			"imei":         "",
		},
		"3gpp_release": map[string]interface{}{
			"lte":  lteRelease,
			"nr5g": nrRelease,
		},
		"network": map[string]interface{}{
			"device_ip":   lanGateway,
			"lan_gateway": lanGateway,
			"wan_ipv4":    wanIPv4,
			"wan_ipv6":    wanIPv6,
			"public_ipv4": "",
			"public_ipv6": "",
		},
		"system": map[string]interface{}{
			"hostname":        hostname,
			"kernel_version":  kernelVersion,
			"openwrt_version": osVersion,
		},
		"identity": h.identity,
		"metrics":  metrics,
	}

	payload["data"] = map[string]interface{}{
		"identity": h.identity,
		"metrics":  metrics,
	}

	JSON(w, http.StatusOK, payload)
}

// GetConfig returns complete active config.
func (h *SystemHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	Success(w, cfg)
}

// SaveConfig updates and persists config.
func (h *SystemHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		Error(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}

	err := h.cfgMgr.Update(func(c *config.Config) {
		*c = newCfg
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	Success(w, map[string]string{"message": "Configuration saved"})
}

// Reboot safely reboots modem.
func (h *SystemHandler) Reboot(w http.ResponseWriter, r *http.Request) {
	Success(w, map[string]string{"message": "Modem reboot initiated"})

	// Run async reboot after response is flushed
	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("reboot").Run()
	}()
}
