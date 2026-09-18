package platform

import (
	"os"
	"runtime"
)

// IPAStatus represents the hardware offload and acceleration state of Qualcomm IP Accelerator (IPA).
type IPAStatus struct {
	Supported bool   `json:"supported"`
	Active    bool   `json:"active"`
	Driver    string `json:"driver"`
	Daemon    string `json:"daemon"`
	Offload   string `json:"offload"`
}

var defaultIPADevicePaths = []string{"/dev/ipa", "/dev/wwan_ioctl"}

// GetIPAStatus checks whether Qualcomm IPA hardware acceleration is supported and active.
func GetIPAStatus() IPAStatus {
	if runtime.GOOS != "linux" {
		return IPAStatus{
			Supported: false,
			Active:    false,
			Driver:    "none",
			Daemon:    "none",
			Offload:   "software",
		}
	}
	return checkIPAStatus(defaultIPADevicePaths, IsProcessRunning)
}

// checkIPAStatus evaluates IPA support and daemon state given device paths and a process checker.
func checkIPAStatus(devPaths []string, procChecker func(string) bool) IPAStatus {
	status := IPAStatus{
		Supported: false,
		Active:    false,
		Driver:    "none",
		Daemon:    "none",
		Offload:   "software",
	}

	for _, p := range devPaths {
		if _, err := os.Stat(p); err == nil {
			status.Supported = true
			status.Driver = "qualcomm_ipa"
			break
		}
	}

	if procChecker != nil {
		if procChecker("ipacm_perf") {
			status.Daemon = "ipacm_perf"
			status.Active = true
			status.Offload = "hardware"
		} else if procChecker("ipacm") {
			status.Daemon = "ipacm"
			status.Active = true
			status.Offload = "hardware"
		}
	}

	return status
}
