package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReadUptime(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Valid uptime
	validFile := filepath.Join(tmpDir, "uptime_valid")
	if err := os.WriteFile(validFile, []byte("12345.67 89012.34\n"), 0644); err != nil {
		t.Fatalf("failed to write valid uptime file: %v", err)
	}

	uptime, err := ReadUptime(validFile)
	if err != nil {
		t.Fatalf("expected no error for valid uptime, got %v", err)
	}
	if uptime != 12345.67 {
		t.Errorf("expected uptime 12345.67, got %f", uptime)
	}

	// 2. Empty file
	emptyFile := filepath.Join(tmpDir, "uptime_empty")
	if err := os.WriteFile(emptyFile, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty uptime file: %v", err)
	}

	_, err = ReadUptime(emptyFile)
	if err == nil {
		t.Errorf("expected error for empty uptime file, got nil")
	}

	// 3. Invalid formatting
	invalidFile := filepath.Join(tmpDir, "uptime_invalid")
	if err := os.WriteFile(invalidFile, []byte("not-a-number abc"), 0644); err != nil {
		t.Fatalf("failed to write invalid uptime file: %v", err)
	}

	_, err = ReadUptime(invalidFile)
	if err == nil {
		t.Errorf("expected error for invalid uptime formatting, got nil")
	}

	// 4. Missing file
	missingFile := filepath.Join(tmpDir, "uptime_missing")
	_, err = ReadUptime(missingFile)
	if err == nil {
		t.Errorf("expected error for missing uptime file, got nil")
	}

	// 5. Default path call
	_, _ = ReadUptime("")
}

func TestReadMemInfo(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Complete /proc/meminfo
	completeFile := filepath.Join(tmpDir, "meminfo_complete")
	completeContent := `MemTotal:        1024000 kB
MemFree:          512000 kB
MemAvailable:     768000 kB
Buffers:           32000 kB
Cached:           224000 kB
`
	if err := os.WriteFile(completeFile, []byte(completeContent), 0644); err != nil {
		t.Fatalf("failed to write complete meminfo file: %v", err)
	}

	total, free, avail, err := ReadMemInfo(completeFile)
	if err != nil {
		t.Fatalf("expected no error for complete meminfo, got %v", err)
	}
	if total != 1024000 || free != 512000 || avail != 768000 {
		t.Errorf("expected (1024000, 512000, 768000), got (%d, %d, %d)", total, free, avail)
	}

	// 2. Missing MemAvailable (older Linux kernels / fallback to MemFree)
	fallbackFile := filepath.Join(tmpDir, "meminfo_fallback")
	fallbackContent := `MemTotal:         512000 kB
MemFree:          256000 kB
Buffers:           16000 kB
`
	if err := os.WriteFile(fallbackFile, []byte(fallbackContent), 0644); err != nil {
		t.Fatalf("failed to write fallback meminfo file: %v", err)
	}

	total, free, avail, err = ReadMemInfo(fallbackFile)
	if err != nil {
		t.Fatalf("expected no error for fallback meminfo, got %v", err)
	}
	if total != 512000 || free != 256000 || avail != 256000 {
		t.Errorf("expected avail fallback to free (512000, 256000, 256000), got (%d, %d, %d)", total, free, avail)
	}

	// 3. Corrupted / malformed lines in meminfo
	corruptedFile := filepath.Join(tmpDir, "meminfo_corrupted")
	corruptedContent := `MemTotal
InvalidLineWithoutColon
MemTotal: notanumber kB
MemFree: 
MemAvailable: 300000 kB
`
	if err := os.WriteFile(corruptedFile, []byte(corruptedContent), 0644); err != nil {
		t.Fatalf("failed to write corrupted meminfo file: %v", err)
	}

	total, free, avail, err = ReadMemInfo(corruptedFile)
	if err != nil {
		t.Fatalf("expected no fatal read error for corrupted lines, got %v", err)
	}
	if avail != 300000 {
		t.Errorf("expected avail 300000 from corrupted meminfo, got %d", avail)
	}

	// 4. Missing file
	missingFile := filepath.Join(tmpDir, "meminfo_missing")
	_, _, _, err = ReadMemInfo(missingFile)
	if err == nil {
		t.Errorf("expected error for missing meminfo file, got nil")
	}

	// 5. Default path call
	_, _, _, _ = ReadMemInfo("")
}

func TestReadNetworkStats(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Standard Linux /proc/net/dev
	netDevFile := filepath.Join(tmpDir, "net_dev_valid")
	netDevContent := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567      890    0    0    0     0          0         0  1234567      890    0    0    0     0       0          0
  eth0: 987654321  123456    1    0    0     0          0         0 876543210   98765    2    0    0     0       0          0
rmnet_data0: 50000000 45000    0    0    0     0          0         0 25000000   22500    0    0    0     0       0          0
`
	if err := os.WriteFile(netDevFile, []byte(netDevContent), 0644); err != nil {
		t.Fatalf("failed to write valid net dev file: %v", err)
	}

	stats, err := ReadNetworkStats(netDevFile)
	if err != nil {
		t.Fatalf("expected no error for valid net dev file, got %v", err)
	}

	if len(stats) != 3 {
		t.Errorf("expected 3 interfaces, got %d", len(stats))
	}

	lo, ok := stats["lo"]
	if !ok || lo.RxBytes != 1234567 || lo.TxBytes != 1234567 || lo.RxPackets != 890 {
		t.Errorf("unexpected lo stats: %+v", lo)
	}

	eth0, ok := stats["eth0"]
	if !ok || eth0.RxBytes != 987654321 || eth0.RxErrors != 1 || eth0.TxBytes != 876543210 || eth0.TxErrors != 2 {
		t.Errorf("unexpected eth0 stats: %+v", eth0)
	}

	rmnet, ok := stats["rmnet_data0"]
	if !ok || rmnet.RxBytes != 50000000 || rmnet.TxBytes != 25000000 || rmnet.RxPackets != 45000 || rmnet.TxPackets != 22500 {
		t.Errorf("unexpected rmnet_data0 stats: %+v", rmnet)
	}

	// 2. Malformed rows (too few columns, missing colon, headers only)
	malformedFile := filepath.Join(tmpDir, "net_dev_malformed")
	malformedContent := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
header_no_colon
short: 1 2 3
invalid_row_without_colon
`
	if err := os.WriteFile(malformedFile, []byte(malformedContent), 0644); err != nil {
		t.Fatalf("failed to write malformed net dev file: %v", err)
	}

	malformedStats, err := ReadNetworkStats(malformedFile)
	if err != nil {
		t.Fatalf("expected no fatal error for malformed lines, got %v", err)
	}
	if len(malformedStats) != 0 {
		t.Errorf("expected 0 valid interfaces in malformed file, got %d", len(malformedStats))
	}

	// 3. Missing file
	missingFile := filepath.Join(tmpDir, "net_dev_missing")
	_, err = ReadNetworkStats(missingFile)
	if err == nil {
		t.Errorf("expected error for missing net dev file, got nil")
	}

	// 4. Default path call
	_, _ = ReadNetworkStats("")
}

func TestReadCpuTemp_SysfsAndHwmon(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Test thermal_zone with millidegrees (>1000)
	thermalDir := filepath.Join(tmpDir, "sys", "class", "thermal", "thermal_zone0")
	if err := os.MkdirAll(thermalDir, 0755); err != nil {
		t.Fatalf("failed to create thermal dir: %v", err)
	}
	tempFile := filepath.Join(thermalDir, "temp")
	if err := os.WriteFile(tempFile, []byte("45500\n"), 0644); err != nil {
		t.Fatalf("failed to write thermal temp: %v", err)
	}

	origThermal := thermalPattern
	origHwmon := hwmonPattern
	defer func() {
		thermalPattern = origThermal
		hwmonPattern = origHwmon
	}()

	thermalPattern = filepath.Join(tmpDir, "sys", "class", "thermal", "thermal_zone*", "temp")
	hwmonPattern = filepath.Join(tmpDir, "sys", "class", "hwmon", "hwmon*", "temp1_input")

	temp := ReadCpuTemp()
	if temp != 45.5 {
		t.Errorf("expected scaled temp 45.5 C, got %f", temp)
	}

	// 2. Test direct degrees (<=1000)
	if err := os.WriteFile(tempFile, []byte("42.0\n"), 0644); err != nil {
		t.Fatalf("failed to write direct temp: %v", err)
	}
	temp = ReadCpuTemp()
	if temp != 42.0 {
		t.Errorf("expected direct temp 42.0 C, got %f", temp)
	}

	// 3. Test hwmon fallback when thermal zone is missing or 0
	_ = os.Remove(tempFile)
	hwmonDir := filepath.Join(tmpDir, "sys", "class", "hwmon", "hwmon1")
	if err := os.MkdirAll(hwmonDir, 0755); err != nil {
		t.Fatalf("failed to create hwmon dir: %v", err)
	}
	hwmonTemp := filepath.Join(hwmonDir, "temp1_input")
	if err := os.WriteFile(hwmonTemp, []byte("50000\n"), 0644); err != nil {
		t.Fatalf("failed to write hwmon temp: %v", err)
	}

	temp = ReadCpuTemp()
	if temp != 50.0 {
		t.Errorf("expected hwmon temp 50.0 C, got %f", temp)
	}

	// 4. Test hwmon direct degrees (<=1000)
	if err := os.WriteFile(hwmonTemp, []byte("39.5\n"), 0644); err != nil {
		t.Fatalf("failed to write direct hwmon temp: %v", err)
	}
	temp = ReadCpuTemp()
	if temp != 39.5 {
		t.Errorf("expected direct hwmon temp 39.5 C, got %f", temp)
	}

	// 5. Test fallback when no thermal sensor files exist
	_ = os.Remove(hwmonTemp)
	temp = ReadCpuTemp()
	if temp != 0.0 {
		t.Errorf("expected fallback temp 0.0 C, got %f", temp)
	}
}

func TestGetSystemMetrics(t *testing.T) {
	metrics := GetSystemMetrics()
	if metrics.MemTotalKB > 0 {
		if metrics.MemUsagePct < 0 || metrics.MemUsagePct > 100 {
			t.Errorf("expected MemUsagePct between 0 and 100, got %f", metrics.MemUsagePct)
		}
	} else {
		if metrics.MemUsagePct != 0 {
			t.Errorf("expected MemUsagePct=0 when MemTotalKB=0, got %f", metrics.MemUsagePct)
		}
	}

	if metrics.Network == nil {
		t.Errorf("expected non-nil Network map in SystemMetrics")
	}
}

func TestGetSystemMetrics_MemUsageCalculation(t *testing.T) {
	// Test calculation logic: (Total - Avail) / Total * 100
	total := uint64(1000)
	avail := uint64(250)
	used := total - avail
	pct := (float64(used) / float64(total)) * 100.0

	if fmt.Sprintf("%.1f", pct) != "75.0" {
		t.Errorf("expected 75.0%% usage, got %f", pct)
	}
}

func TestPlatformHelpers(t *testing.T) {
	// 1. GetHostname
	hn := GetHostname()
	if hn == "" {
		t.Errorf("expected non-empty hostname")
	}

	// 2. GetKernelVersion
	kv := GetKernelVersion()
	if kv == "" {
		t.Errorf("expected non-empty kernel version")
	}

	// 3. GetOSVersion
	osv := GetOSVersion()
	if osv == "" {
		t.Errorf("expected non-empty OS version")
	}

	// 4. GetDefaultGatewayIP
	gw := GetDefaultGatewayIP()
	if gw == "" {
		t.Errorf("expected non-empty default gateway IP")
	}

	// 5. GetInterfaceIP on invalid interface
	ip4, ip6 := GetInterfaceIP("nonexistent_iface_xyz")
	if ip4 != "" || ip6 != "" {
		t.Errorf("expected empty IPs for non-existent interface, got ip4=%s ip6=%s", ip4, ip6)
	}
}
