package bandwidth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sampleProcNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000000     100    0    0    0     0          0         0  1000000     100    0    0    0     0       0          0
rmnet_data0: 50000000    5000    0    0    0     0          0         0 10000000    2000    0    0    0     0       0          0
bridge0: 20000000    3000    0    0    0     0          0         0  5000000    1000    0    0    0     0       0          0
  eth0: 1000000      50    0    0    0     0          0         0  1000000      50    0    0    0     0       0          0
`

const sampleProcNetDevDelta = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000000     100    0    0    0     0          0         0  1000000     100    0    0    0     0       0          0
rmnet_data0: 50125000    5010    0    0    0     0          0         0 10062500    2005    0    0    0     0       0          0
bridge0: 20050000    3010    0    0    0     0          0         0  5025000    1005    0    0    0     0       0          0
  eth0: 1000000      50    0    0    0     0          0         0  1000000      50    0    0    0     0       0          0
`

func TestCollector_ParseAndRateCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	devPath := filepath.Join(tmpDir, "net_dev")
	storePath := filepath.Join(tmpDir, "bandwidth.json")

	if err := os.WriteFile(devPath, []byte(sampleProcNetDev), 0644); err != nil {
		t.Fatalf("failed to write dev file: %v", err)
	}

	currentTime := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	timeFunc := func() time.Time { return currentTime }

	col := NewCollector(storePath, WithDevPath(devPath), WithTimeFunc(timeFunc))

	// First collection initializes baselines
	col.Collect()

	snap1 := col.GetSnapshot()
	rmnet1, ok := snap1.Interfaces["rmnet_data0"]
	if !ok {
		t.Fatalf("expected rmnet_data0 interface in snapshot")
	}
	if rmnet1.CurrentRxBps != 0 || rmnet1.CurrentTxBps != 0 {
		t.Errorf("expected 0 rate on initial collection, got rx=%f, tx=%f", rmnet1.CurrentRxBps, rmnet1.CurrentTxBps)
	}

	// Advance time by exactly 1 second and write delta
	currentTime = currentTime.Add(1 * time.Second)
	// Delta for rmnet_data0:
	// rx: 50125000 - 50000000 = 125,000 bytes. 125,000 * 8 / 1s = 1,000,000 bps (1 Mbps)
	// tx: 10062500 - 10000000 = 62,500 bytes.  62,500 * 8 / 1s = 500,000 bps (0.5 Mbps)
	if err := os.WriteFile(devPath, []byte(sampleProcNetDevDelta), 0644); err != nil {
		t.Fatalf("failed to write delta file: %v", err)
	}

	col.Collect()

	snap2 := col.GetSnapshot()
	rmnet2 := snap2.Interfaces["rmnet_data0"]

	if rmnet2.CurrentRxBps != 1000000.0 {
		t.Errorf("expected CurrentRxBps = 1000000, got %f", rmnet2.CurrentRxBps)
	}
	if rmnet2.CurrentTxBps != 500000.0 {
		t.Errorf("expected CurrentTxBps = 500000, got %f", rmnet2.CurrentTxBps)
	}
	if rmnet2.TodayRxBytes != 125000 {
		t.Errorf("expected TodayRxBytes = 125000, got %d", rmnet2.TodayRxBytes)
	}
	if rmnet2.TodayTxBytes != 62500 {
		t.Errorf("expected TodayTxBytes = 62500, got %d", rmnet2.TodayTxBytes)
	}
	if len(rmnet2.Realtime) != 1 {
		t.Errorf("expected 1 realtime point, got %d", len(rmnet2.Realtime))
	}
	if len(rmnet2.Hourly) != 1 || rmnet2.Hourly[0].RxBytes != 125000 {
		t.Errorf("expected 1 hourly bucket with 125000 bytes, got %+v", rmnet2.Hourly)
	}
	if len(rmnet2.Daily) != 1 || rmnet2.Daily[0].RxBytes != 125000 {
		t.Errorf("expected 1 daily bucket with 125000 bytes, got %+v", rmnet2.Daily)
	}
	if len(rmnet2.Monthly) != 1 || rmnet2.Monthly[0].RxBytes != 125000 {
		t.Errorf("expected 1 monthly bucket with 125000 bytes, got %+v", rmnet2.Monthly)
	}
}

func TestCollector_RolloverHandling(t *testing.T) {
	tmpDir := t.TempDir()
	devPath := filepath.Join(tmpDir, "net_dev")
	storePath := filepath.Join(tmpDir, "bandwidth.json")

	// High counter values before rollover/reset
	initContent := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
rmnet_data0: 4294967200    5000    0    0    0     0          0         0 4294967200    2000    0    0    0     0       0          0
`
	_ = os.WriteFile(devPath, []byte(initContent), 0644)

	currentTime := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	col := NewCollector(storePath, WithDevPath(devPath), WithTimeFunc(func() time.Time { return currentTime }))
	col.Collect()

	// Rollover/reset occurs: counter drops to low numbers
	rolloverContent := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
rmnet_data0: 100000    10    0    0    0     0          0         0 50000    5    0    0    0     0       0          0
`
	_ = os.WriteFile(devPath, []byte(rolloverContent), 0644)
	currentTime = currentTime.Add(1 * time.Second)
	col.Collect()

	snap := col.GetSnapshot()
	rmnet := snap.Interfaces["rmnet_data0"]
	// Should treat new value as delta instead of underflowing negative
	if rmnet.TodayRxBytes != 100000 {
		t.Errorf("expected TodayRxBytes = 100000 after rollover, got %d", rmnet.TodayRxBytes)
	}
	if rmnet.TodayTxBytes != 50000 {
		t.Errorf("expected TodayTxBytes = 50000 after rollover, got %d", rmnet.TodayTxBytes)
	}
}

func TestCollector_1970ClockGuard(t *testing.T) {
	tmpDir := t.TempDir()
	devPath := filepath.Join(tmpDir, "net_dev")
	storePath := filepath.Join(tmpDir, "bandwidth.json")

	_ = os.WriteFile(devPath, []byte(sampleProcNetDev), 0644)

	// Year 1970 (modem unsynced boot)
	clock1970 := time.Date(1970, 1, 1, 0, 0, 30, 0, time.UTC)
	col := NewCollector(storePath, WithDevPath(devPath), WithTimeFunc(func() time.Time { return clock1970 }))
	col.Collect()

	// Advance by 1s
	clock1970 = clock1970.Add(1 * time.Second)
	_ = os.WriteFile(devPath, []byte(sampleProcNetDevDelta), 0644)
	col.Collect()

	snap := col.GetSnapshot()
	rmnet := snap.Interfaces["rmnet_data0"]

	// Realtime rate is updated
	if rmnet.CurrentRxBps <= 0 {
		t.Errorf("expected realtime rate to compute even in 1970, got %f", rmnet.CurrentRxBps)
	}

	// But historical date/month buckets must NOT be created for 1970
	if len(rmnet.Hourly) != 0 {
		t.Errorf("expected 0 hourly buckets during 1970 clock step, got %d", len(rmnet.Hourly))
	}
	if len(rmnet.Daily) != 0 {
		t.Errorf("expected 0 daily buckets during 1970 clock step, got %d", len(rmnet.Daily))
	}
	if len(rmnet.Monthly) != 0 {
		t.Errorf("expected 0 monthly buckets during 1970 clock step, got %d", len(rmnet.Monthly))
	}
}

func TestCollector_FlushAndLoadFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	devPath := filepath.Join(tmpDir, "net_dev")
	storePath := filepath.Join(tmpDir, "bandwidth.json")

	_ = os.WriteFile(devPath, []byte(sampleProcNetDev), 0644)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	col1 := NewCollector(storePath, WithDevPath(devPath), WithTimeFunc(func() time.Time { return now }))
	col1.Collect()

	now = now.Add(1 * time.Second)
	_ = os.WriteFile(devPath, []byte(sampleProcNetDevDelta), 0644)
	col1.Collect()

	if err := col1.FlushToDisk(); err != nil {
		t.Fatalf("FlushToDisk failed: %v", err)
	}

	// Verify persistence file exists and has content
	if fi, err := os.Stat(storePath); err != nil || fi.Size() == 0 {
		t.Fatalf("bandwidth file not created: %v", err)
	}

	// Create new collector instance and load from disk
	col2 := NewCollector(storePath, WithDevPath(devPath), WithTimeFunc(func() time.Time { return now }))
	snap2 := col2.GetSnapshot()
	rmnet2, ok := snap2.Interfaces["rmnet_data0"]
	if !ok {
		t.Fatalf("expected rmnet_data0 in loaded snapshot")
	}
	if rmnet2.TotalRxBytes != 125000 {
		t.Errorf("expected TotalRxBytes=125000 after load, got %d", rmnet2.TotalRxBytes)
	}
	if len(rmnet2.Daily) != 1 {
		t.Errorf("expected 1 daily bucket loaded, got %d", len(rmnet2.Daily))
	}
}

func TestCollector_Reset(t *testing.T) {
	tmpDir := t.TempDir()
	devPath := filepath.Join(tmpDir, "net_dev")
	storePath := filepath.Join(tmpDir, "bandwidth.json")

	_ = os.WriteFile(devPath, []byte(sampleProcNetDev), 0644)
	now := time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC)
	col := NewCollector(storePath, WithDevPath(devPath), WithTimeFunc(func() time.Time { return now }))
	col.Collect()

	now = now.Add(1 * time.Second)
	_ = os.WriteFile(devPath, []byte(sampleProcNetDevDelta), 0644)
	col.Collect()

	// Reset single interface
	col.Reset("rmnet_data0")

	snap := col.GetSnapshot()
	rmnet := snap.Interfaces["rmnet_data0"]
	if rmnet.TodayRxBytes != 0 || rmnet.TotalRxBytes != 0 || len(rmnet.Daily) != 0 {
		t.Errorf("expected reset rmnet_data0, got today=%d total=%d daily=%d", rmnet.TodayRxBytes, rmnet.TotalRxBytes, len(rmnet.Daily))
	}

	// Reset all
	col.Reset("all")
	snapAll := col.GetSnapshot()
	for name, iface := range snapAll.Interfaces {
		if iface.TotalRxBytes != 0 {
			t.Errorf("expected reset for %s, got TotalRxBytes=%d", name, iface.TotalRxBytes)
		}
	}

	// Close lifecycle test
	if err := col.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
