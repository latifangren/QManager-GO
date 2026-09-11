package telemetry

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func newTestWatchdog(t *testing.T) (*Watchdog, *atengine.MockTransport, *config.Manager) {
	t.Helper()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() {
		_ = eng.Close()
	})

	confPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	prober := NewPingProber("127.0.0.1:9", 50*time.Millisecond)
	wd := NewWatchdog(eng, cfgMgr, prober)
	return wd, mock, cfgMgr
}

// 1. 4-tier watchdog escalation sequence (Tier 1: CGATT reset, Tier 2: CFUN radio restart, Tier 3: SIM slot failover, Tier 4: Modem reboot)
func TestWatchdog_4TierEscalationSequence(t *testing.T) {
	wd, mock, cfgMgr := newTestWatchdog(t)

	err := cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.FailThreshold = 2
		c.Watchcat.Cooldown = 10
		c.Watchcat.Tier1Enabled = 1
		c.Watchcat.Tier2Enabled = 1
		c.Watchcat.Tier3Enabled = 1
		c.Watchcat.Tier4Enabled = 1
		c.Watchcat.BackupSimSlot = "2"
		c.Watchcat.MaxRebootsPerHour = 5
	})
	if err != nil {
		t.Fatalf("config update error: %v", err)
	}

	var simTime = time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	var rebootCount int32
	var probeSuccess bool

	wd.SetExecutor(WatchdogExecutor{
		RebootFunc: func(ctx context.Context) error {
			atomic.AddInt32(&rebootCount, 1)
			return nil
		},
		SleepFunc: func(d time.Duration) {},
		GetTimeFunc: func() time.Time {
			return simTime
		},
		ProbeFunc: func() PingSample {
			return PingSample{Success: probeSuccess}
		},
	})

	// Preload expected AT responses
	mock.SetResponse("AT+CGATT=0", "OK")
	mock.SetResponse("AT+CGATT=1", "OK")
	mock.SetResponse("AT+CFUN=0", "OK")
	mock.SetResponse("AT+CFUN=1", "OK")
	mock.SetResponse("AT+QUIMSLOT=2", "OK")

	// --- Step 1: Probe Fail 1 (Threshold is 2) ---
	probeSuccess = false
	wd.CheckOnce()
	if fails := wd.GetFailures(); fails != 1 {
		t.Fatalf("expected 1 failure, got %d", fails)
	}
	if tier := wd.GetCurrentTier(); tier != 0 {
		t.Fatalf("expected current tier 0 before threshold, got %d", tier)
	}
	if len(mock.GetHistory()) != 0 {
		t.Fatalf("expected no AT commands before threshold, got %v", mock.GetHistory())
	}

	// --- Step 2: Probe Fail 2 -> Tier 1 Action (CGATT Reset) ---
	mock.ClearHistory()
	wd.CheckOnce()
	if tier := wd.GetCurrentTier(); tier != 1 {
		t.Fatalf("expected current tier 1 after threshold, got %d", tier)
	}
	if fails := wd.GetFailures(); fails != 0 {
		t.Fatalf("expected failures reset to 0 after recovery action, got %d", fails)
	}
	history := mock.GetHistory()
	if len(history) < 2 || history[0] != "AT+CGATT=0" || history[1] != "AT+CGATT=1" {
		t.Fatalf("expected Tier 1 AT commands [AT+CGATT=0, AT+CGATT=1], got %v", history)
	}

	// --- Step 3: Advance past cooldown -> Probe Fails x2 -> Tier 2 Action (CFUN Restart) ---
	simTime = simTime.Add(15 * time.Second)
	mock.ClearHistory()
	wd.CheckOnce() // Fail 1
	wd.CheckOnce() // Fail 2 -> Triggers Tier 2
	if tier := wd.GetCurrentTier(); tier != 2 {
		t.Fatalf("expected current tier 2, got %d", tier)
	}
	history = mock.GetHistory()
	if len(history) < 2 || history[0] != "AT+CFUN=0" || history[1] != "AT+CFUN=1" {
		t.Fatalf("expected Tier 2 AT commands [AT+CFUN=0, AT+CFUN=1], got %v", history)
	}

	// --- Step 4: Advance past cooldown -> Probe Fails x2 -> Tier 3 Action (SIM Failover) ---
	simTime = simTime.Add(15 * time.Second)
	mock.ClearHistory()
	wd.CheckOnce() // Fail 1
	wd.CheckOnce() // Fail 2 -> Triggers Tier 3
	if tier := wd.GetCurrentTier(); tier != 3 {
		t.Fatalf("expected current tier 3, got %d", tier)
	}
	history = mock.GetHistory()
	if len(history) < 3 || history[0] != "AT+CFUN=0" || history[1] != "AT+QUIMSLOT=2" || history[2] != "AT+CFUN=1" {
		t.Fatalf("expected Tier 3 AT commands [AT+CFUN=0, AT+QUIMSLOT=2, AT+CFUN=1], got %v", history)
	}
	status := wd.GetStatus()
	if status.ActiveSimSlot != 2 || !status.FailoverActive {
		t.Fatalf("expected active SIM slot 2 and failover active, got slot=%d failover=%v", status.ActiveSimSlot, status.FailoverActive)
	}

	// --- Step 5: Advance past cooldown -> Probe Fails x2 -> Tier 4 Action (Modem Reboot) ---
	simTime = simTime.Add(15 * time.Second)
	mock.ClearHistory()
	wd.CheckOnce() // Fail 1
	wd.CheckOnce() // Fail 2 -> Triggers Tier 4
	if tier := wd.GetCurrentTier(); tier != 4 {
		t.Fatalf("expected current tier 4, got %d", tier)
	}
	if count := atomic.LoadInt32(&rebootCount); count != 1 {
		t.Fatalf("expected 1 reboot invocation, got %d", count)
	}
}

// 2. Failure counter increment on probe failure and immediate reset to 0 on successful probe.
func TestWatchdog_FailureCounterTransitions(t *testing.T) {
	tests := []struct {
		name          string
		probeResults  []bool
		expectedFails int
		expectedTier  int
	}{
		{
			name:          "single failure increments counter",
			probeResults:  []bool{false},
			expectedFails: 1,
			expectedTier:  0,
		},
		{
			name:          "multiple failures increment counter consecutively",
			probeResults:  []bool{false, false, false},
			expectedFails: 3,
			expectedTier:  0,
		},
		{
			name:          "success immediately resets counter and tier",
			probeResults:  []bool{false, false, false, true},
			expectedFails: 0,
			expectedTier:  0,
		},
		{
			name:          "failure after reset starts from 1",
			probeResults:  []bool{false, false, true, false},
			expectedFails: 1,
			expectedTier:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd, _, cfgMgr := newTestWatchdog(t)
			_ = cfgMgr.Update(func(c *config.Config) {
				c.Watchcat.Enabled = 1
				c.Watchcat.FailThreshold = 10
				c.Watchcat.Cooldown = 60
			})

			simTime := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
			var curProbeSuccess bool

			wd.SetExecutor(WatchdogExecutor{
				GetTimeFunc: func() time.Time { return simTime },
				SleepFunc:   func(d time.Duration) {},
				ProbeFunc: func() PingSample {
					return PingSample{Success: curProbeSuccess}
				},
			})

			for _, res := range tt.probeResults {
				curProbeSuccess = res
				wd.CheckOnce()
			}

			if fails := wd.GetFailures(); fails != tt.expectedFails {
				t.Errorf("expected failures=%d, got %d", tt.expectedFails, fails)
			}
			if tier := wd.GetCurrentTier(); tier != tt.expectedTier {
				t.Errorf("expected tier=%d, got %d", tt.expectedTier, tier)
			}
		})
	}
}

// 3. Clock-step guard (1970 NTP jump) safety behavior.
func TestWatchdog_ClockStepGuard_1970(t *testing.T) {
	wd, mock, cfgMgr := newTestWatchdog(t)

	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.FailThreshold = 2
		c.Watchcat.Tier1Enabled = 1
		c.Watchcat.Tier4Enabled = 1
	})

	var rebootCount int32
	var simTime = time.Date(1970, 1, 1, 0, 0, 30, 0, time.UTC)

	wd.SetExecutor(WatchdogExecutor{
		RebootFunc: func(ctx context.Context) error {
			atomic.AddInt32(&rebootCount, 1)
			return nil
		},
		SleepFunc: func(d time.Duration) {},
		GetTimeFunc: func() time.Time {
			return simTime
		},
		ProbeFunc: func() PingSample {
			return PingSample{Success: false}
		},
	})

	// 10 failing probes in year 1970
	for i := 0; i < 10; i++ {
		wd.CheckOnce()
	}

	// Clock step guard must prevent any destructive recovery or counter corruption
	if len(mock.GetHistory()) != 0 {
		t.Fatalf("expected 0 AT commands executed during 1970 boot state, got %v", mock.GetHistory())
	}
	if count := atomic.LoadInt32(&rebootCount); count != 0 {
		t.Fatalf("expected 0 reboots during 1970 boot state, got %d", count)
	}

	// System clock steps forward past NTP sync (e.g. 2026)
	simTime = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	wd.CheckOnce() // Fail 1
	wd.CheckOnce() // Fail 2 -> Now reaches threshold in valid clock epoch
	if tier := wd.GetCurrentTier(); tier != 1 {
		t.Fatalf("expected tier 1 executed after clock stepped to 2026, got %d", tier)
	}
	if len(mock.GetHistory()) < 2 {
		t.Fatalf("expected AT commands executed after clock step, got %v", mock.GetHistory())
	}
}

// 4. Reboot rate limiter / cooldown window.
func TestWatchdog_RebootRateLimiter(t *testing.T) {
	wd, _, cfgMgr := newTestWatchdog(t)

	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.FailThreshold = 1
		c.Watchcat.Cooldown = 5
		c.Watchcat.Tier1Enabled = 0
		c.Watchcat.Tier2Enabled = 0
		c.Watchcat.Tier3Enabled = 0
		c.Watchcat.Tier4Enabled = 1
		c.Watchcat.MaxRebootsPerHour = 2
	})

	var rebootCount int32
	var simTime = time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)

	wd.SetExecutor(WatchdogExecutor{
		RebootFunc: func(ctx context.Context) error {
			atomic.AddInt32(&rebootCount, 1)
			return nil
		},
		SleepFunc: func(d time.Duration) {},
		GetTimeFunc: func() time.Time {
			return simTime
		},
		ProbeFunc: func() PingSample {
			return PingSample{Success: false}
		},
	})

	// Reboot #1 at 10:00
	wd.CheckOnce()
	if count := atomic.LoadInt32(&rebootCount); count != 1 {
		t.Fatalf("expected 1 reboot, got %d", count)
	}

	// Reboot #2 at 10:10 (past cooldown, within 1h)
	simTime = simTime.Add(10 * time.Minute)
	wd.CheckOnce()
	if count := atomic.LoadInt32(&rebootCount); count != 2 {
		t.Fatalf("expected 2 reboots, got %d", count)
	}

	// Attempt #3 at 10:20 (past cooldown, 2 reboots already in 1 hour -> rate limit reached!)
	simTime = simTime.Add(10 * time.Minute)
	wd.CheckOnce()
	if count := atomic.LoadInt32(&rebootCount); count != 2 {
		t.Fatalf("expected reboot to be skipped by rate limiter (count remained 2), got %d", count)
	}

	// Attempt #4 at 11:05 (first reboot at 10:00 is > 1 hour ago -> 1 reboot in window < 2)
	simTime = simTime.Add(45 * time.Minute)
	wd.CheckOnce()
	if count := atomic.LoadInt32(&rebootCount); count != 3 {
		t.Fatalf("expected reboot allowed after window roll over (count=3), got %d", count)
	}
}

// 4b. Cooldown window tests
func TestWatchdog_CooldownWindow(t *testing.T) {
	wd, mock, cfgMgr := newTestWatchdog(t)

	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.FailThreshold = 1
		c.Watchcat.Cooldown = 60
		c.Watchcat.Tier1Enabled = 1
		c.Watchcat.Tier2Enabled = 1
	})

	var simTime = time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	var probeSuccess bool

	wd.SetExecutor(WatchdogExecutor{
		SleepFunc: func(d time.Duration) {},
		GetTimeFunc: func() time.Time {
			return simTime
		},
		ProbeFunc: func() PingSample {
			return PingSample{Success: probeSuccess}
		},
	})

	// Trigger Tier 1 at t=0s
	probeSuccess = false
	wd.CheckOnce()
	if tier := wd.GetCurrentTier(); tier != 1 {
		t.Fatalf("expected Tier 1 triggered, got %d", tier)
	}
	mock.ClearHistory()

	// Probe fails at t=10s (during 60s cooldown) -> no new recovery, no failures increment
	simTime = simTime.Add(10 * time.Second)
	wd.CheckOnce()
	if len(mock.GetHistory()) != 0 {
		t.Fatalf("expected no actions during cooldown window, got %v", mock.GetHistory())
	}
	if fails := wd.GetFailures(); fails != 0 {
		t.Fatalf("expected 0 failures during cooldown, got %d", fails)
	}

	// Probe succeeds at t=20s (during cooldown) -> clears cooldown immediately
	simTime = simTime.Add(10 * time.Second)
	probeSuccess = true
	wd.CheckOnce()
	status := wd.GetStatus()
	if status.State != "connected" {
		t.Fatalf("expected status 'connected' after probe recovery during cooldown, got %s", status.State)
	}

	// Subsequent fail at t=30s -> normal failure detection resumes
	simTime = simTime.Add(10 * time.Second)
	probeSuccess = false
	wd.CheckOnce()
	if tier := wd.GetCurrentTier(); tier != 1 {
		t.Fatalf("expected Tier 1 to trigger on new failure cycle, got %d", tier)
	}
}

// 5. Tier skipping when certain tiers are disabled
func TestWatchdog_TierSkipping(t *testing.T) {
	wd, mock, cfgMgr := newTestWatchdog(t)

	// Enable only Tier 2 and Tier 4 (Tier 1 & 3 disabled)
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.FailThreshold = 1
		c.Watchcat.Cooldown = 10
		c.Watchcat.Tier1Enabled = 0
		c.Watchcat.Tier2Enabled = 1
		c.Watchcat.Tier3Enabled = 0
		c.Watchcat.Tier4Enabled = 1
		c.Watchcat.MaxRebootsPerHour = 5
	})

	var rebootCount int32
	var simTime = time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)

	wd.SetExecutor(WatchdogExecutor{
		RebootFunc: func(ctx context.Context) error {
			atomic.AddInt32(&rebootCount, 1)
			return nil
		},
		SleepFunc: func(d time.Duration) {},
		GetTimeFunc: func() time.Time {
			return simTime
		},
		ProbeFunc: func() PingSample {
			return PingSample{Success: false}
		},
	})

	// 1st failure -> Skips Tier 1, triggers Tier 2
	wd.CheckOnce()
	if tier := wd.GetCurrentTier(); tier != 2 {
		t.Fatalf("expected Tier 2 executed (Tier 1 skipped), got %d", tier)
	}
	hist := mock.GetHistory()
	if len(hist) < 2 || hist[0] != "AT+CFUN=0" || hist[1] != "AT+CFUN=1" {
		t.Fatalf("expected Tier 2 commands, got %v", hist)
	}

	// 2nd failure -> Skips Tier 3, triggers Tier 4 reboot
	simTime = simTime.Add(15 * time.Second)
	wd.CheckOnce()
	if tier := wd.GetCurrentTier(); tier != 4 {
		t.Fatalf("expected Tier 4 executed (Tier 3 skipped), got %d", tier)
	}
	if count := atomic.LoadInt32(&rebootCount); count != 1 {
		t.Fatalf("expected reboot executed, got %d", count)
	}
}

// 6. Clean start/stop lifecycle without goroutine leaks or panics
func TestWatchdog_Lifecycle(t *testing.T) {
	wd, _, cfgMgr := newTestWatchdog(t)
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.CheckInterval = 1
	})

	// Test idempotent Start()
	wd.Start()
	wd.Start()

	time.Sleep(50 * time.Millisecond)

	// Test idempotent Stop()
	wd.Stop()
	wd.Stop()

	// Test re-starting after Stop()
	wd.Start()
	time.Sleep(50 * time.Millisecond)
	wd.Stop()
}

// 7. Status reporting and Disabled state
func TestWatchdog_StatusAndDisabled(t *testing.T) {
	wd, _, cfgMgr := newTestWatchdog(t)

	// Config disabled
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 0
		c.Watchcat.FailThreshold = 2
	})

	wd.CheckOnce()
	st := wd.GetStatus()
	if st.State != "disabled" {
		t.Fatalf("expected state 'disabled', got %s", st.State)
	}
	if st.Running {
		t.Fatalf("expected running=false when disabled")
	}

	// Enable config
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Watchcat.Enabled = 1
	})
	st = wd.GetStatus()
	if st.State != "connected" {
		t.Fatalf("expected state 'connected', got %s", st.State)
	}
}

func TestNewWatchdog_Configurations(t *testing.T) {
	// Test instantiation with nil prober / engine / config
	wdNil := NewWatchdog(nil, nil, nil)
	if wdNil == nil {
		t.Fatalf("expected non-nil Watchdog instance")
	}

	// Verify default executor fallback
	sample := wdNil.executor.ProbeFunc()
	if sample.Success {
		t.Errorf("expected default probe without prober to return Success=false")
	}
	uptime := wdNil.executor.GetUptimeFunc()
	if uptime < 0 {
		t.Errorf("unexpected negative uptime: %f", uptime)
	}
}
