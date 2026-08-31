package telemetry

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
)

// WatchdogExecutor defines injectable hooks for watchdog actions (testability).
type WatchdogExecutor struct {
	RebootFunc         func(ctx context.Context) error
	NetworkRestartFunc func(ctx context.Context) error
	SleepFunc          func(d time.Duration)
	GetTimeFunc        func() time.Time
	GetUptimeFunc      func() float64
	ProbeFunc          func() PingSample
}

// WatchdogStatus represents the live diagnostic status of the connection watchdog.
type WatchdogStatus struct {
	Running             bool   `json:"running"`
	State               string `json:"state"`
	CurrentStep         int    `json:"current_step"`
	Fails               int    `json:"fails"`
	RebootsInWindow     int    `json:"reboots_in_window"`
	RebootsLimit        int    `json:"reboots_limit"`
	WindowRemainingSecs int    `json:"window_remaining_secs"`
	ActiveSimSlot       int    `json:"active_sim_slot"`
	FailoverActive      bool   `json:"failover_active"`
}

// Watchdog monitors connectivity and applies 4-tier recovery if link fails.
type Watchdog struct {
	engine         *atengine.Engine
	cfgMgr         *config.Manager
	prober         *PingProber
	mu             sync.Mutex
	stopCh         chan struct{}
	running        bool
	failures       int
	currentTier    int
	lastActionAt   time.Time
	cooldownUntil  time.Time
	rebootTimes    []time.Time
	activeSimSlot  int
	failoverActive bool
	executor       WatchdogExecutor
}

// NewWatchdog creates a connection watchdog.
func NewWatchdog(eng *atengine.Engine, cfgMgr *config.Manager, prober *PingProber) *Watchdog {
	w := &Watchdog{
		engine:        eng,
		cfgMgr:        cfgMgr,
		prober:        prober,
		stopCh:        make(chan struct{}),
		activeSimSlot: 1,
	}
	w.executor = WatchdogExecutor{
		RebootFunc: func(ctx context.Context) error {
			log.Println("[Watchdog] Executing emergency modem reboot (Tier 4)")
			return exec.CommandContext(ctx, "reboot").Run()
		},
		NetworkRestartFunc: func(ctx context.Context) error {
			log.Println("[Watchdog] Executing network restart")
			return exec.CommandContext(ctx, "systemctl", "restart", "network").Run()
		},
		SleepFunc: func(d time.Duration) {
			time.Sleep(d)
		},
		GetTimeFunc: func() time.Time {
			return time.Now()
		},
		GetUptimeFunc: func() float64 {
			up, err := platform.ReadUptime("")
			if err != nil {
				return 0
			}
			return up
		},
		ProbeFunc: func() PingSample {
			if prober != nil {
				return prober.ProbeOnce()
			}
			return PingSample{Success: false}
		},
	}
	return w
}

// SetExecutor overrides action handlers (primarily for unit tests).
func (w *Watchdog) SetExecutor(exec WatchdogExecutor) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if exec.RebootFunc != nil {
		w.executor.RebootFunc = exec.RebootFunc
	}
	if exec.NetworkRestartFunc != nil {
		w.executor.NetworkRestartFunc = exec.NetworkRestartFunc
	}
	if exec.SleepFunc != nil {
		w.executor.SleepFunc = exec.SleepFunc
	}
	if exec.GetTimeFunc != nil {
		w.executor.GetTimeFunc = exec.GetTimeFunc
	}
	if exec.GetUptimeFunc != nil {
		w.executor.GetUptimeFunc = exec.GetUptimeFunc
	}
	if exec.ProbeFunc != nil {
		w.executor.ProbeFunc = exec.ProbeFunc
	}
}

// Start begins watchdog monitoring loop.
func (w *Watchdog) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	go w.loop()
}

// Stop halts the watchdog.
func (w *Watchdog) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()
}

// CheckOnce executes a single watchdog evaluation cycle.
func (w *Watchdog) CheckOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()

	cfg := w.cfgMgr.Get().Watchcat
	if cfg.Enabled == 0 {
		w.failures = 0
		w.currentTier = 0
		return
	}

	now := w.executor.GetTimeFunc()
	if !ClockSane(now) {
		log.Println("[Watchdog] Clock not sane (pre-2024 / 1970 boot state), skipping recovery check")
		return
	}

	// Check cooldown window
	if !w.cooldownUntil.IsZero() && now.Before(w.cooldownUntil) {
		sample := w.executor.ProbeFunc()
		if sample.Success {
			// Recovered during cooldown
			w.failures = 0
			w.currentTier = 0
			w.cooldownUntil = time.Time{}
		}
		return
	}

	sample := w.executor.ProbeFunc()
	if sample.Success {
		w.failures = 0
		w.currentTier = 0
		return
	}

	w.failures++
	threshold := cfg.FailThreshold
	if threshold <= 0 {
		threshold = 5
	}

	if w.failures >= threshold {
		w.escalateAndRecoverLocked(cfg, now)
		w.failures = 0
	}
}

func (w *Watchdog) countRecentRebootsLocked(now time.Time) int {
	var recent []time.Time
	oneHourAgo := now.Add(-1 * time.Hour)
	for _, t := range w.rebootTimes {
		if t.After(oneHourAgo) {
			recent = append(recent, t)
		}
	}
	w.rebootTimes = recent
	return len(recent)
}

func (w *Watchdog) getNextTierLocked(cfg config.WatchcatConfig) int {
	// Find next enabled tier > w.currentTier
	for t := w.currentTier + 1; t <= 4; t++ {
		if isTierEnabled(cfg, t) {
			return t
		}
	}
	// If none found above current, wrap to lowest enabled tier
	for t := 1; t <= 4; t++ {
		if isTierEnabled(cfg, t) {
			return t
		}
	}
	return 0
}

func isTierEnabled(cfg config.WatchcatConfig, tier int) bool {
	switch tier {
	case 1:
		return cfg.Tier1Enabled == 1
	case 2:
		return cfg.Tier2Enabled == 1
	case 3:
		return cfg.Tier3Enabled == 1
	case 4:
		return cfg.Tier4Enabled == 1
	default:
		return false
	}
}

func (w *Watchdog) escalateAndRecoverLocked(cfg config.WatchcatConfig, now time.Time) {
	nextTier := w.getNextTierLocked(cfg)
	if nextTier == 0 {
		log.Println("[Watchdog] All recovery tiers disabled, skipping action")
		return
	}

	w.currentTier = nextTier
	w.lastActionAt = now

	cooldownSec := cfg.Cooldown
	if cooldownSec <= 0 {
		cooldownSec = 60
	}
	w.cooldownUntil = now.Add(time.Duration(cooldownSec) * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch nextTier {
	case 1:
		// Tier 1: CGATT Data Attach / Registration Reset
		log.Println("[Watchdog] Escalation Tier 1: Executing CGATT reset")
		if w.engine != nil {
			_, _ = w.engine.ExecHigh(ctx, "AT+CGATT=0")
			w.executor.SleepFunc(1 * time.Second)
			_, _ = w.engine.ExecHigh(ctx, "AT+CGATT=1")
		}
		GetGlobalHistory().RecordEvent(NetworkEventItem{
			Timestamp: now.Unix(),
			Type:      "watchdog_recovery",
			Message:   "Tier 1: CGATT reset triggered",
			Severity:  "warn",
		})

	case 2:
		// Tier 2: CFUN Soft Radio Restart
		log.Println("[Watchdog] Escalation Tier 2: Executing CFUN radio restart")
		if w.engine != nil {
			_, _ = w.engine.ExecHigh(ctx, "AT+CFUN=0")
			w.executor.SleepFunc(2 * time.Second)
			_, _ = w.engine.ExecHigh(ctx, "AT+CFUN=1")
		}
		GetGlobalHistory().RecordEvent(NetworkEventItem{
			Timestamp: now.Unix(),
			Type:      "watchdog_recovery",
			Message:   "Tier 2: CFUN radio restart triggered",
			Severity:  "warn",
		})

	case 3:
		// Tier 3: Dual-SIM Slot Failover
		slot := cfg.BackupSimSlot
		if slot == "" {
			slot = "2"
		}
		log.Printf("[Watchdog] Escalation Tier 3: Switching to SIM slot %s\n", slot)
		if w.engine != nil {
			_, _ = w.engine.ExecHigh(ctx, "AT+CFUN=0")
			w.executor.SleepFunc(1 * time.Second)
			_, _ = w.engine.ExecHigh(ctx, fmt.Sprintf("AT+QUIMSLOT=%s", slot))
			w.executor.SleepFunc(1 * time.Second)
			_, _ = w.engine.ExecHigh(ctx, "AT+CFUN=1")
		}
		w.failoverActive = true
		if s, err := strconv.Atoi(slot); err == nil {
			w.activeSimSlot = s
		}
		GetGlobalHistory().RecordEvent(NetworkEventItem{
			Timestamp: now.Unix(),
			Type:      "watchdog_recovery",
			Message:   fmt.Sprintf("Tier 3: Switched to SIM slot %s", slot),
			Severity:  "warn",
		})

	case 4:
		// Tier 4: Modem System Reboot (Rate-limited)
		reboots := w.countRecentRebootsLocked(now)
		limit := cfg.MaxRebootsPerHour
		if limit <= 0 {
			limit = 3
		}

		if reboots >= limit {
			log.Printf("[Watchdog] Tier 4 reboot rate limit reached (%d/%d in 1h), skipping reboot\n", reboots, limit)
			GetGlobalHistory().RecordEvent(NetworkEventItem{
				Timestamp: now.Unix(),
				Type:      "watchdog_rate_limit",
				Message:   fmt.Sprintf("Tier 4 reboot skipped: rate limit (%d/%d) reached", reboots, limit),
				Severity:  "error",
			})
			return
		}

		w.rebootTimes = append(w.rebootTimes, now)
		log.Println("[Watchdog] Escalation Tier 4: Executing modem reboot")
		GetGlobalHistory().RecordEvent(NetworkEventItem{
			Timestamp: now.Unix(),
			Type:      "watchdog_recovery",
			Message:   "Tier 4: System reboot triggered",
			Severity:  "critical",
		})
		if w.executor.RebootFunc != nil {
			_ = w.executor.RebootFunc(ctx)
		}
	}
}

// GetStatus returns the current live status of the watchdog.
func (w *Watchdog) GetStatus() WatchdogStatus {
	w.mu.Lock()
	defer w.mu.Unlock()

	cfg := w.cfgMgr.Get().Watchcat
	limit := cfg.MaxRebootsPerHour
	if limit <= 0 {
		limit = 3
	}

	now := w.executor.GetTimeFunc()
	reboots := w.countRecentRebootsLocked(now)

	state := "connected"
	if cfg.Enabled == 0 {
		state = "disabled"
	} else if !w.cooldownUntil.IsZero() && now.Before(w.cooldownUntil) {
		state = "cooldown"
	} else if w.failures > 0 {
		state = "suspect"
	}

	windowRemaining := 3600
	if len(w.rebootTimes) > 0 {
		oldest := w.rebootTimes[0]
		elapsed := int(now.Sub(oldest).Seconds())
		if elapsed < 3600 {
			windowRemaining = 3600 - elapsed
		}
	}

	return WatchdogStatus{
		Running:             w.running && cfg.Enabled == 1,
		State:               state,
		CurrentStep:         w.currentTier,
		Fails:               w.failures,
		RebootsInWindow:     reboots,
		RebootsLimit:        limit,
		WindowRemainingSecs: windowRemaining,
		ActiveSimSlot:       w.activeSimSlot,
		FailoverActive:      w.failoverActive,
	}
}

// GetFailures returns current consecutive failure count.
func (w *Watchdog) GetFailures() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failures
}

// GetCurrentTier returns current recovery tier.
func (w *Watchdog) GetCurrentTier() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentTier
}

func (w *Watchdog) loop() {
	interval := 10 * time.Second
	cfg := w.cfgMgr.Get().Watchcat
	if cfg.CheckInterval > 0 {
		interval = time.Duration(cfg.CheckInterval) * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.CheckOnce()
		}
	}
}
