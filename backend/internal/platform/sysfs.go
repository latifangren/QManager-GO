package platform

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	thermalPattern = "/sys/class/thermal/thermal_zone*/temp"
	hwmonPattern   = "/sys/class/hwmon/hwmon*/temp1_input"
)

// NetworkStats holds interface transfer counters from /proc/net/dev.
type NetworkStats struct {
	Interface string `json:"interface"`
	RxBytes   uint64 `json:"rx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	TxBytes   uint64 `json:"tx_bytes"`
	TxPackets uint64 `json:"tx_packets"`
	TxErrors  uint64 `json:"tx_errors"`
}

// SystemMetrics holds memory, CPU temperature, and uptime metrics.
type SystemMetrics struct {
	UptimeSeconds float64                 `json:"uptime_seconds"`
	MemTotalKB    uint64                  `json:"mem_total_kb"`
	MemFreeKB     uint64                  `json:"mem_free_kb"`
	MemAvailKB    uint64                  `json:"mem_available_kb"`
	MemUsagePct   float64                 `json:"mem_usage_pct"`
	CpuTempC      float64                 `json:"cpu_temp_c"`
	Network       map[string]NetworkStats `json:"network"`
}

// ReadUptime reads /proc/uptime in seconds.
func ReadUptime(path string) (float64, error) {
	if path == "" {
		path = "/proc/uptime"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("invalid uptime format")
	}
	return strconv.ParseFloat(fields[0], 64)
}

// ReadMemInfo reads /proc/meminfo.
func ReadMemInfo(path string) (total, free, avail uint64, err error) {
	if path == "" {
		path = "/proc/meminfo"
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valFields := strings.Fields(parts[1])
		if len(valFields) == 0 {
			continue
		}
		val, _ := strconv.ParseUint(valFields[0], 10, 64)

		switch key {
		case "MemTotal":
			total = val
		case "MemFree":
			free = val
		case "MemAvailable":
			avail = val
		}
	}
	if avail == 0 && free > 0 {
		avail = free
	}
	return total, free, avail, nil
}

// ReadCpuTemp searches /sys/class/thermal or hwmon for modem CPU temperature.
func ReadCpuTemp() float64 {
	// 1. Check thermal_zone
	zones, err := filepath.Glob(thermalPattern)
	if err == nil {
		for _, z := range zones {
			if strings.Contains(z, "cooling_device") {
				continue
			}
			if data, err := os.ReadFile(z); err == nil {
				val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
				if err == nil && val > 0 {
					if val > 1000 {
						return val / 1000.0
					}
					return val
				}
			}
		}
	}

	// 2. Check hwmon
	hwmon, err := filepath.Glob(hwmonPattern)
	if err == nil {
		for _, h := range hwmon {
			if data, err := os.ReadFile(h); err == nil {
				val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
				if err == nil && val > 0 {
					if val > 1000 {
						return val / 1000.0
					}
					return val
				}
			}
		}
	}

	return 0.0
}

// ReadNetworkStats parses /proc/net/dev.
func ReadNetworkStats(path string) (map[string]NetworkStats, error) {
	if path == "" {
		path = "/proc/net/dev"
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stats := make(map[string]NetworkStats)
	scanner := bufio.NewScanner(file)
	lineIdx := 0

	for scanner.Scan() {
		lineIdx++
		if lineIdx <= 2 { // Skip header lines
			continue
		}
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}

		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseUint(fields[1], 10, 64)
		rxErrors, _ := strconv.ParseUint(fields[2], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		txPackets, _ := strconv.ParseUint(fields[9], 10, 64)
		txErrors, _ := strconv.ParseUint(fields[10], 10, 64)

		stats[iface] = NetworkStats{
			Interface: iface,
			RxBytes:   rxBytes,
			RxPackets: rxPackets,
			RxErrors:  rxErrors,
			TxBytes:   txBytes,
			TxPackets: txPackets,
			TxErrors:  txErrors,
		}
	}

	return stats, nil
}

// GetSystemMetrics compiles all system status counters into one object.
func GetSystemMetrics() SystemMetrics {
	m := SystemMetrics{
		Network: make(map[string]NetworkStats),
	}

	m.UptimeSeconds, _ = ReadUptime("")
	m.MemTotalKB, m.MemFreeKB, m.MemAvailKB, _ = ReadMemInfo("")
	if m.MemTotalKB > 0 {
		used := m.MemTotalKB - m.MemAvailKB
		m.MemUsagePct = (float64(used) / float64(m.MemTotalKB)) * 100.0
	}
	m.CpuTempC = ReadCpuTemp()
	if net, err := ReadNetworkStats(""); err == nil {
		m.Network = net
	}

	return m
}

// GetInterfaceIP returns the first valid IPv4 and IPv6 addresses for a given interface name.
func GetInterfaceIP(ifaceName string) (ipv4, ipv6 string) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", ""
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", ""
	}

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}

		if ip == nil || ip.IsLoopback() {
			continue
		}

		if ip4 := ip.To4(); ip4 != nil {
			if ipv4 == "" {
				ipv4 = ip4.String()
			}
		} else if ip.To16() != nil {
			if ipv6 == "" && !ip.IsLinkLocalUnicast() {
				ipv6 = ip.String()
			}
		}
	}

	return ipv4, ipv6
}

// GetDefaultGatewayIP scans active local interfaces (bridge0, eth0, ecm0, rndis0) for an IP address.
func GetDefaultGatewayIP() string {
	candidates := []string{"bridge0", "br0", "eth0", "ecm0", "rndis0", "usb0"}
	for _, name := range candidates {
		ip4, _ := GetInterfaceIP(name)
		if ip4 != "" {
			return ip4
		}
	}

	// Fallback to iterating all non-loopback interfaces
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
				continue
			}
			ip4, _ := GetInterfaceIP(iface.Name)
			if ip4 != "" && !strings.HasPrefix(iface.Name, "rmnet") {
				return ip4
			}
		}
	}

	return "192.168.225.1"
}

// GetKernelVersion reads the active Linux kernel release version.
func GetKernelVersion() string {
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		ver := strings.TrimSpace(string(data))
		if ver != "" {
			return ver
		}
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		ver := strings.TrimSpace(string(out))
		if ver != "" {
			return ver
		}
	}
	return "4.14.206"
}

// GetHostname returns the machine hostname from OS or /proc.
func GetHostname() string {
	if hn, err := os.Hostname(); err == nil && hn != "" {
		return hn
	}
	if data, err := os.ReadFile("/proc/sys/kernel/hostname"); err == nil {
		hn := strings.TrimSpace(string(data))
		if hn != "" {
			return hn
		}
	}
	return "sdxprairie"
}

// GetOSVersion extracts the OS or OpenWrt/Yocto distribution version.
func GetOSVersion() string {
	// 1. Check /etc/openwrt_release
	if file, err := os.Open("/etc/openwrt_release"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "DISTRIB_DESCRIPTION=") {
				return strings.Trim(strings.TrimPrefix(line, "DISTRIB_DESCRIPTION="), "\"'")
			}
		}
	}

	// 2. Check /etc/os-release
	if file, err := os.Open("/etc/os-release"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"'")
			}
		}
	}

	return "QManager Embedded Linux"
}
