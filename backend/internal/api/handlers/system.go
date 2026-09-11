package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

// SystemHandler provides system metrics, hardware profile, and reboot controls.
type SystemHandler struct {
	identity platform.Identity
	cfgMgr   *config.Manager
	poller   *telemetry.Poller
}

// NewSystemHandler creates a SystemHandler.
func NewSystemHandler(id platform.Identity, cfgMgr *config.Manager, poller ...*telemetry.Poller) *SystemHandler {
	var p *telemetry.Poller
	if len(poller) > 0 {
		p = poller[0]
	}
	return &SystemHandler{
		identity: id,
		cfgMgr:   cfgMgr,
		poller:   p,
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

	imei := ""
	buildDate := h.identity.PackageTime
	if h.poller != nil {
		data := h.poller.GetStatus()
		if data != nil {
			if data.IMEI != "" {
				imei = data.IMEI
			}
			if data.Device.BuildDate != "" {
				buildDate = data.Device.BuildDate
			}
		}
	}

	// Fetch public IP via curl / rmnet_data0 if available
	publicIPv4, publicIPv6 := fetchPublicIPs()

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
			"build_date":   buildDate,
			"imei":         imei,
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
			"public_ipv4": publicIPv4,
			"public_ipv6": publicIPv6,
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

var (
	cachedPublicIPv4 string
	cachedPublicIPv6 string
	lastPublicIPTime time.Time
)

func fetchPublicIPs() (string, string) {
	if time.Since(lastPublicIPTime) < 30*time.Second && (cachedPublicIPv4 != "" || cachedPublicIPv6 != "") {
		return cachedPublicIPv4, cachedPublicIPv6
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try IPv4
	cmd4 := exec.CommandContext(ctx, "curl", "-s", "-m", "2", "https://api.ipify.org")
	if out, err := cmd4.Output(); err == nil && len(out) > 0 {
		cachedPublicIPv4 = strings.TrimSpace(string(out))
	} else {
		// Fallback to rmnet_data0 if interface bound
		cmd4Fallback := exec.CommandContext(ctx, "curl", "--interface", "rmnet_data0", "-s", "-m", "2", "http://api.ipify.org")
		if outF, errF := cmd4Fallback.Output(); errF == nil && len(outF) > 0 {
			cachedPublicIPv4 = strings.TrimSpace(string(outF))
		}
	}

	// Try IPv6
	cmd6 := exec.CommandContext(ctx, "curl", "-s", "-m", "2", "https://api64.ipify.org")
	if out, err := cmd6.Output(); err == nil && len(out) > 0 {
		res := strings.TrimSpace(string(out))
		if strings.Contains(res, ":") {
			cachedPublicIPv6 = res
		}
	}

	lastPublicIPTime = time.Now()
	return cachedPublicIPv4, cachedPublicIPv6
}

// GetConfig returns complete active config.
func (h *SystemHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()

	var days []int
	if strings.TrimSpace(cfg.Settings.SchedRebootDays) != "" {
		parts := strings.Split(cfg.Settings.SchedRebootDays, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if d, err := strconv.Atoi(p); err == nil {
				days = append(days, d)
			}
		}
	}
	if days == nil {
		days = []int{}
	}

	settings := map[string]interface{}{
		"hostname":        cfg.Settings.Hostname,
		"temp_unit":       cfg.Settings.TempUnit,
		"distance_unit":   cfg.Settings.DistanceUnit,
		"timezone":        cfg.Settings.Timezone,
		"zonename":        cfg.Settings.Zonename,
		"sms_tool_device": cfg.Settings.SmsToolDevice,
	}

	scheduledReboot := map[string]interface{}{
		"enabled": cfg.Settings.SchedRebootEnabled == 1,
		"time":    cfg.Settings.SchedRebootTime,
		"days":    days,
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"settings":         settings,
		"scheduled_reboot": scheduledReboot,
		"data":             cfg,
	})
}

// SaveConfig updates and persists config.
func (h *SystemHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		Error(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}

	action, _ := rawMap["action"].(string)

	switch action {
	case "save_settings":
		err := h.cfgMgr.Update(func(c *config.Config) {
			if v, ok := rawMap["hostname"].(string); ok {
				c.Settings.Hostname = v
			}
			if v, ok := rawMap["temp_unit"].(string); ok {
				c.Settings.TempUnit = v
			}
			if v, ok := rawMap["distance_unit"].(string); ok {
				c.Settings.DistanceUnit = v
			}
			if v, ok := rawMap["timezone"].(string); ok {
				c.Settings.Timezone = v
			}
			if v, ok := rawMap["zonename"].(string); ok {
				c.Settings.Zonename = v
			}
			if v, ok := rawMap["sms_tool_device"].(string); ok {
				c.Settings.SmsToolDevice = v
			}
		})
		if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to save settings")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success":               true,
			"message":               "Settings saved",
			"timezone_apply_status": "applied",
		})
		return

	case "save_scheduled_reboot":
		var enabledInt int
		var enabledBool bool
		if b, ok := rawMap["enabled"].(bool); ok {
			enabledBool = b
			if b {
				enabledInt = 1
			}
		} else if f, ok := rawMap["enabled"].(float64); ok {
			enabledInt = int(f)
			enabledBool = enabledInt == 1
		}

		timeStr, _ := rawMap["time"].(string)
		if timeStr == "" {
			timeStr = "04:00"
		}

		var daysList []int
		var daysStrList []string
		if daysRaw, ok := rawMap["days"].([]interface{}); ok {
			for _, d := range daysRaw {
				switch val := d.(type) {
				case float64:
					daysList = append(daysList, int(val))
					daysStrList = append(daysStrList, strconv.Itoa(int(val)))
				case int:
					daysList = append(daysList, val)
					daysStrList = append(daysStrList, strconv.Itoa(val))
				case string:
					if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
						daysList = append(daysList, i)
						daysStrList = append(daysStrList, strconv.Itoa(i))
					}
				}
			}
		}
		if daysList == nil {
			daysList = []int{}
		}
		daysStr := strings.Join(daysStrList, ",")

		err := h.cfgMgr.Update(func(c *config.Config) {
			c.Settings.SchedRebootEnabled = enabledInt
			c.Settings.SchedRebootTime = timeStr
			c.Settings.SchedRebootDays = daysStr
		})
		if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to save scheduled reboot")
			return
		}

		scheduledReboot := map[string]interface{}{
			"enabled": enabledBool,
			"time":    timeStr,
			"days":    daysList,
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"success":          true,
			"armed":            true,
			"message":          "Scheduled reboot saved",
			"scheduled_reboot": scheduledReboot,
		})
		return

	default:
		var newCfg config.Config
		if err := json.Unmarshal(bodyBytes, &newCfg); err != nil {
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
