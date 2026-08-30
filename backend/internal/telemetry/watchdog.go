package telemetry

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

// Watchdog monitors connectivity and applies 4-tier recovery if link fails.
type Watchdog struct {
	engine   *atengine.Engine
	cfgMgr   *config.Manager
	prober   *PingProber
	mu       sync.Mutex
	stopCh   chan struct{}
	running  bool
	failures int
}

// NewWatchdog creates a connection watchdog.
func NewWatchdog(eng *atengine.Engine, cfgMgr *config.Manager, prober *PingProber) *Watchdog {
	return &Watchdog{
		engine:  eng,
		cfgMgr:  cfgMgr,
		prober:  prober,
		stopCh:  make(chan struct{}),
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
	w.mu.Unlock()

	go w.loop()
}

// Stop halts the watchdog.
func (w *Watchdog) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	close(w.stopCh)
}

func (w *Watchdog) loop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *Watchdog) check() {
	cfg := w.cfgMgr.Get().Watchcat
	if cfg.Enabled == 0 {
		w.failures = 0
		return
	}

	sample := w.prober.ProbeOnce()
	if sample.Success {
		w.failures = 0
		return
	}

	w.failures++
	threshold := cfg.FailThreshold
	if threshold <= 0 {
		threshold = 5
	}

	if w.failures >= threshold {
		w.recoverLink(cfg)
		w.failures = 0
	}
}

func (w *Watchdog) recoverLink(cfg config.WatchcatConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Tier 1: Soft Radio Restart (AT+CFUN=0 then AT+CFUN=1)
	if cfg.Tier1Enabled == 1 {
		_, _ = w.engine.ExecHigh(ctx, "AT+CFUN=0")
		time.Sleep(2 * time.Second)
		_, _ = w.engine.ExecHigh(ctx, "AT+CFUN=1")
		return
	}

	// Tier 2: Network Stack / Route Restart
	if cfg.Tier2Enabled == 1 {
		_ = exec.Command("systemctl", "restart", "network").Run()
		return
	}

	// Tier 4: Modem System Reboot (gated)
	if cfg.Tier4Enabled == 1 {
		_ = exec.Command("reboot").Run()
	}
}
