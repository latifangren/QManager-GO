package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func TestClockSaneAndGuards(t *testing.T) {
	// Year 1970
	t1970 := time.Date(1970, 1, 1, 0, 0, 24, 0, time.UTC)
	if ClockSane(t1970) {
		t.Errorf("expected 1970 to fail ClockSane")
	}

	// Year 2023
	t2023 := time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC)
	if ClockSane(t2023) {
		t.Errorf("expected 2023 to fail ClockSane")
	}

	// Year 2026
	t2026 := time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)
	if !ClockSane(t2026) {
		t.Errorf("expected 2026 to pass ClockSane")
	}

	// Boot settled
	if BootSettled(50, 300) {
		t.Errorf("50s uptime should not be settled against 300s")
	}
	if !BootSettled(350, 300) {
		t.Errorf("350s uptime should be settled against 300s")
	}

	// Now matches HHMM
	now4AM := time.Date(2026, 8, 30, 4, 5, 0, 0, time.UTC)
	if !NowMatchesHHMM("04:00", now4AM, 10) {
		t.Errorf("04:05 should match 04:00 with 10 min tolerance")
	}
	if NowMatchesHHMM("04:00", now4AM, 2) {
		t.Errorf("04:05 should NOT match 04:00 with 2 min tolerance")
	}

	// Midnight wrap
	nowMidnight := time.Date(2026, 8, 30, 0, 2, 0, 0, time.UTC)
	if !NowMatchesHHMM("23:58", nowMidnight, 10) {
		t.Errorf("00:02 should match 23:58 across midnight")
	}

	// Day matches
	if !DayMatches("0,1,2,3,4,5,6", time.Sunday) {
		t.Errorf("all days should match Sunday")
	}
	if DayMatches("1,2,3,4,5", time.Sunday) {
		t.Errorf("weekdays only should not match Sunday")
	}
}

func TestScheduler_1970ClockStepGuard(t *testing.T) {
	tempDir := t.TempDir()
	confPath := filepath.Join(tempDir, "qmanager.conf")
	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	_ = cfgMgr.Update(func(c *config.Config) {
		c.Settings.SchedRebootEnabled = 1
		c.Settings.SchedRebootTime = "04:00"
		c.Settings.SchedRebootDays = "0,1,2,3,4,5,6"
		c.Update.AutoUpdateEnabled = 1
		c.Update.AutoUpdateTime = "04:00"
	})

	eng := atengine.NewEngine(atengine.NewMockTransport())
	sched := NewScheduler(eng, cfgMgr)

	var rebootTriggered int32
	var updateTriggered int32

	sched.SetExecutor(JobExecutor{
		RebootFunc: func(ctx context.Context) error {
			atomic.AddInt32(&rebootTriggered, 1)
			return nil
		},
		AutoUpdateFunc: func(ctx context.Context) error {
			atomic.AddInt32(&updateTriggered, 1)
			return nil
		},
		GetTimeFunc: func() time.Time {
			// Simulate 1970 uncalibrated RTC boot step
			return time.Date(1970, 1, 1, 4, 0, 0, 0, time.UTC)
		},
		GetUptimeFunc: func() float64 {
			return 24.0
		},
	})

	// Evaluate in 1970
	sched.Evaluate()

	if atomic.LoadInt32(&rebootTriggered) != 0 {
		t.Errorf("reboot triggered during 1970 boot window! 1970 guard failed")
	}
	if atomic.LoadInt32(&updateTriggered) != 0 {
		t.Errorf("auto-update triggered during 1970 boot window! 1970 guard failed")
	}
}

func TestScheduler_JobsTriggerWhenSane(t *testing.T) {
	tempDir := t.TempDir()
	confPath := filepath.Join(tempDir, "qmanager.conf")
	towerPath := filepath.Join(tempDir, "tower_lock.json")

	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	_ = cfgMgr.Update(func(c *config.Config) {
		c.Settings.SchedRebootEnabled = 1
		c.Settings.SchedRebootTime = "04:00"
		c.Settings.SchedRebootDays = "0,1,2,3,4,5,6"
		c.Update.AutoUpdateEnabled = 1
		c.Update.AutoUpdateTime = "04:00"
	})

	// Write tower lock schedule
	towerData := `{
		"schedule": {
			"enabled": true,
			"start_time": "04:00",
			"end_time": "08:00",
			"days": [0,1,2,3,4,5,6]
		}
	}`
	_ = os.WriteFile(towerPath, []byte(towerData), 0644)

	eng := atengine.NewEngine(atengine.NewMockTransport())
	sched := NewScheduler(eng, cfgMgr)
	sched.SetTowerConfigPath(towerPath)

	var rebootCount int32
	var towerApplyCount int32
	var updateCount int32

	sched.SetExecutor(JobExecutor{
		RebootFunc: func(ctx context.Context) error {
			atomic.AddInt32(&rebootCount, 1)
			return nil
		},
		TowerApplyFunc: func(ctx context.Context) error {
			atomic.AddInt32(&towerApplyCount, 1)
			return nil
		},
		AutoUpdateFunc: func(ctx context.Context) error {
			atomic.AddInt32(&updateCount, 1)
			return nil
		},
		GetTimeFunc: func() time.Time {
			return time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)
		},
		GetUptimeFunc: func() float64 {
			return 600.0
		},
	})

	// Evaluate when clock is sane (2026) and time matches 04:00
	sched.Evaluate()

	if atomic.LoadInt32(&rebootCount) != 1 {
		t.Errorf("expected 1 reboot trigger, got %d", atomic.LoadInt32(&rebootCount))
	}
	if atomic.LoadInt32(&towerApplyCount) != 1 {
		t.Errorf("expected 1 tower apply trigger, got %d", atomic.LoadInt32(&towerApplyCount))
	}
	if atomic.LoadInt32(&updateCount) != 1 {
		t.Errorf("expected 1 update trigger, got %d", atomic.LoadInt32(&updateCount))
	}

	// Immediate duplicate evaluation should not re-trigger same slot
	sched.Evaluate()
	if atomic.LoadInt32(&rebootCount) != 1 {
		t.Errorf("duplicate trigger occurred for reboot in same slot")
	}
}

func TestScheduler_LifecycleAndTowerTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)

	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	sched := NewScheduler(eng, cfgMgr)

	// Test BootSettled helper
	if BootSettled(100.0, 300.0) {
		t.Errorf("BootSettled(100, 300) should be false")
	}
	if !BootSettled(350.0, 300.0) {
		t.Errorf("BootSettled(350, 300) should be true")
	}

	// Test Start/Stop lifecycle
	sched.Start()
	sched.Start() // Idempotent
	time.Sleep(50 * time.Millisecond)
	sched.Stop()
	sched.Stop() // Idempotent

	// Test Tower Clear transition
	towerPath := filepath.Join(tmpDir, "tower_lock.json")
	towerData := `{
		"lte": { "enabled": true },
		"schedule": {
			"enabled": true,
			"start_time": "22:00",
			"end_time": "08:00",
			"days": [0,1,2,3,4,5,6]
		}
	}`
	_ = os.WriteFile(towerPath, []byte(towerData), 0644)
	sched.SetTowerConfigPath(towerPath)

	var towerClearCount int32
	sched.SetExecutor(JobExecutor{
		TowerClearFunc: func(ctx context.Context) error {
			atomic.AddInt32(&towerClearCount, 1)
			return nil
		},
		GetTimeFunc: func() time.Time {
			// 08:00 is end time -> should trigger TowerClearFunc
			return time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)
		},
		GetUptimeFunc: func() float64 { return 1000.0 },
	})

	sched.Evaluate()
	if atomic.LoadInt32(&towerClearCount) != 1 {
		t.Errorf("expected 1 tower clear action at 08:00, got %d", towerClearCount)
	}
}
