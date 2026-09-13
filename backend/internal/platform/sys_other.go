//go:build !linux

package platform

import (
	"fmt"
	"syscall"
)

// RebootModem is dummy for non-Linux OS.
func RebootModem() error {
	return fmt.Errorf("reboot only supported on Linux")
}

// ReloadDnsmasq is dummy for non-Linux OS.
func ReloadDnsmasq() error {
	return fmt.Errorf("reload dnsmasq only supported on Linux")
}

// IsProcessRunning is dummy for non-Linux OS.
func IsProcessRunning(name string) bool {
	return false
}

// KillProcessByName is dummy for non-Linux OS.
func KillProcessByName(name string, sig syscall.Signal) int {
	return 0
}
