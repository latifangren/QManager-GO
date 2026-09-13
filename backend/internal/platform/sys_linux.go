//go:build linux

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// RebootModem performs an immediate Linux kernel-level reboot syscall.
func RebootModem() error {
	// Sync dirty filesystem buffers before reboot
	syscall.Sync()
	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

// ReloadDnsmasq sends SIGHUP to dnsmasq using its pidfile without spawning killall.
func ReloadDnsmasq() error {
	pidFiles := []string{
		"/var/run/data/dnsmasq.pid",
		"/var/run/dnsmasq.pid",
		"/var/run/dnsmasq/dnsmasq.pid",
	}

	for _, pf := range pidFiles {
		if data, err := os.ReadFile(pf); err == nil {
			pidStr := strings.TrimSpace(string(data))
			if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
				proc, err := os.FindProcess(pid)
				if err == nil {
					return proc.Signal(syscall.SIGHUP)
				}
			}
		}
	}

	// Fallback scanning /proc for process named dnsmasq
	entries, err := os.ReadDir("/proc")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pid, err := strconv.Atoi(e.Name())
			if err != nil || pid <= 1 {
				continue
			}
			comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
			if err == nil && strings.TrimSpace(string(comm)) == "dnsmasq" {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proc.Signal(syscall.SIGHUP)
					return nil
				}
			}
		}
	}

	return fmt.Errorf("dnsmasq process not found")
}

// IsProcessRunning checks whether at least one process with matching name is running.
func IsProcessRunning(name string) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == name {
			return true
		}
	}
	return false
}

// KillProcessByName sends SIGTERM / SIGKILL to processes matching exact name.
func KillProcessByName(name string, sig syscall.Signal) int {
	killed := 0
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == name {
			if proc, err := os.FindProcess(pid); err == nil {
				if err := proc.Signal(sig); err == nil {
					killed++
				}
			}
		}
	}
	return killed
}
