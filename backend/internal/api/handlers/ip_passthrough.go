package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"qmanager/internal/atengine"
)

var (
	ipptConfigPath = "/etc/qmanager/ippt_config.json"
)

// IPPassthroughConfig represents the stored IPPT setting.
type IPPassthroughConfig struct {
	Mode     string `json:"mode"`      // "disabled", "eth", "usb"
	MAC      string `json:"mac"`       // Target MAC address (for eth)
	NAT      string `json:"nat"`       // "0" (WithoutNAT), "1" (WithNAT)
	USBMode  string `json:"usb_mode"`  // "0"=rmnet, "1"=ecm, "2"=mbim, "3"=rndis
	DNSProxy string `json:"dns_proxy"` // "enabled", "disabled"
}

// IPPassthroughHandler handles bridge mode and IP passthrough.
type IPPassthroughHandler struct {
	engine *atengine.Engine
}

// NewIPPassthroughHandler creates a new IPPassthroughHandler.
func NewIPPassthroughHandler(engine *atengine.Engine) *IPPassthroughHandler {
	return &IPPassthroughHandler{
		engine: engine,
	}
}

// Status handles GET /api/v1/network/passthrough and /cgi-bin/quecmanager/network/ip_passthrough.sh
func (h *IPPassthroughHandler) Status(w http.ResponseWriter, r *http.Request) {
	cfg := readIPPTConfig()

	// If config file not present, query modem via AT+QMAP
	if cfg.Mode == "" {
		cfg.Mode = "disabled"
		cfg.NAT = "1"
		cfg.USBMode = "1"
		cfg.DNSProxy = "disabled"

		res, err := h.engine.Exec(`AT+QMAP="MPDN_rule";+QMAP="IPPT_NAT";+QCFG="usbnet";+QMAP="DHCPV4DNS"`)
		if err == nil {
			lines := strings.Split(res.Raw, "\n")
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if strings.Contains(l, "MPDN_rule") {
					parts := strings.Split(l, ",")
					if len(parts) >= 5 {
						modeVal := strings.TrimSpace(parts[4])
						switch modeVal {
						case "1":
							cfg.Mode = "eth"
						case "3":
							cfg.Mode = "usb"
						default:
							cfg.Mode = "disabled"
						}
					}
					if len(parts) >= 7 {
						cfg.MAC = strings.Trim(parts[6], "\" ")
					}
				} else if strings.Contains(l, "IPPT_NAT") {
					parts := strings.Split(l, ",")
					if len(parts) >= 2 {
						cfg.NAT = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(l, "usbnet") {
					parts := strings.Split(l, ",")
					if len(parts) >= 2 {
						cfg.USBMode = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(l, "DHCPV4DNS") {
					parts := strings.Split(l, ",")
					if len(parts) >= 2 {
						dnsVal := strings.Trim(parts[1], "\" ")
						if strings.EqualFold(dnsVal, "enable") {
							cfg.DNSProxy = "enabled"
						}
					}
				}
			}
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"passthrough_mode": cfg.Mode,
		"target_mac":       cfg.MAC,
		"ippt_nat":         cfg.NAT,
		"usb_mode":         cfg.USBMode,
		"dns_proxy":        cfg.DNSProxy,
	})
}

// IPPTSavePayload represents POST parameters.
type IPPTSavePayload struct {
	Action   string `json:"action"` // "apply"
	Mode     string `json:"mode"`
	MAC      string `json:"mac"`
	NAT      string `json:"nat"`
	USBMode  string `json:"usb_mode"`
	DNSProxy string `json:"dns_proxy"`
}

// Apply handles POST /api/v1/network/passthrough and /cgi-bin/quecmanager/network/ip_passthrough.sh
func (h *IPPassthroughHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var p IPPTSavePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	mode := strings.ToLower(p.Mode)
	if mode != "disabled" && mode != "eth" && mode != "usb" {
		Error(w, http.StatusBadRequest, "Invalid mode (must be disabled, eth, or usb)")
		return
	}

	mac := strings.TrimSpace(p.MAC)
	if mode == "eth" {
		if !validateMAC(mac) {
			Error(w, http.StatusBadRequest, "Invalid MAC address format for Ethernet passthrough")
			return
		}
	}

	nat := "1"
	if p.NAT == "0" || p.NAT == "1" {
		nat = p.NAT
	}

	usbMode := "1"
	if p.USBMode >= "0" && p.USBMode <= "3" {
		usbMode = p.USBMode
	}

	dnsProxy := "disabled"
	if strings.EqualFold(p.DNSProxy, "enabled") {
		dnsProxy = "enabled"
	}

	// 1. Build AT command list
	var cmds []string

	// NAT mode
	cmds = append(cmds, fmt.Sprintf(`AT+QMAP="IPPT_NAT",%s`, nat))

	// DNS Offloading
	dnsParam := "disable"
	if dnsProxy == "enabled" {
		dnsParam = "enable"
	}
	cmds = append(cmds, fmt.Sprintf(`AT+QMAP="DHCPV4DNS","%s"`, dnsParam))

	// Mode rules
	switch mode {
	case "disabled":
		cmds = append(cmds, `AT+QMAP="MPDN_rule",0`)
		cmds = append(cmds, `AT+QMAPWAC=1`)
	case "eth":
		cmds = append(cmds, fmt.Sprintf(`AT+QMAP="MPDN_rule",0,1,0,1,1,"%s"`, mac))
	case "usb":
		cmds = append(cmds, `AT+QMAP="MPDN_rule",0,1,0,3,1`)
		cmds = append(cmds, fmt.Sprintf(`AT+QCFG="usbnet",%s`, usbMode))
	}

	// Execute commands
	for _, cmd := range cmds {
		_, _ = h.engine.Exec(cmd)
	}

	// Save configuration
	cfg := IPPassthroughConfig{
		Mode:     mode,
		MAC:      mac,
		NAT:      nat,
		USBMode:  usbMode,
		DNSProxy: dnsProxy,
	}
	_ = writeIPPTConfig(cfg)

	// Trigger reboot after flush
	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("reboot").Run()
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "IP Passthrough settings applied. Rebooting...",
	})
}

func validateMAC(mac string) bool {
	pattern := `^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`
	matched, _ := regexp.MatchString(pattern, mac)
	return matched
}

func readIPPTConfig() IPPassthroughConfig {
	data, err := os.ReadFile(ipptConfigPath)
	if err != nil {
		return IPPassthroughConfig{}
	}
	var c IPPassthroughConfig
	_ = json.Unmarshal(data, &c)
	return c
}

func writeIPPTConfig(c IPPassthroughConfig) error {
	_ = os.MkdirAll(filepath.Dir(ipptConfigPath), 0755)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ipptConfigPath, data, 0644)
}
