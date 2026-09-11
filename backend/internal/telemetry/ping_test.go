package telemetry

import (
	"net"
	"testing"
	"time"
)

func TestPingProber_BasicAndStats(t *testing.T) {
	// Start a local TCP listener to act as target
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test TCP listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	prober := NewPingProber(addr, 100*time.Millisecond)

	// Execute single probe against live target
	s1 := prober.ProbeOnce()
	if !s1.Success {
		t.Errorf("expected probe against local listener to succeed")
	}
	if s1.LatencyMs < 0 {
		t.Errorf("expected positive latency, got %f", s1.LatencyMs)
	}

	// Execute another probe
	s2 := prober.ProbeOnce()
	if !s2.Success {
		t.Errorf("expected second probe to succeed")
	}

	stats := prober.GetStats()
	if stats.Target != addr {
		t.Errorf("expected Target=%s, got %s", addr, stats.Target)
	}
	if stats.LossPct != 0.0 {
		t.Errorf("expected 0%% loss, got %f", stats.LossPct)
	}
	if stats.MinMs < 0 || stats.MaxMs < 0 || stats.AvgMs < 0 {
		t.Errorf("expected valid non-negative min/max/avg metrics: %+v", stats)
	}
	if len(stats.RecentPoints) != 2 {
		t.Errorf("expected 2 recent points, got %d", len(stats.RecentPoints))
	}
}

func TestPingProber_FailedTargetAndLoss(t *testing.T) {
	// Probe a non-routable address / closed port to trigger failure and loss calculation
	prober := NewPingProber("127.0.0.1:1", 100*time.Millisecond)
	prober.dialTimeout = 50 * time.Millisecond

	s1 := prober.ProbeOnce()
	if s1.Success {
		t.Errorf("expected probe to fail on closed port 1")
	}
	if s1.LatencyMs != 0 {
		t.Errorf("expected LatencyMs=0 on failure, got %f", s1.LatencyMs)
	}

	stats := prober.GetStats()
	if stats.LossPct != 100.0 {
		t.Errorf("expected 100%% packet loss, got %f", stats.LossPct)
	}
	if stats.MinMs != 0 {
		t.Errorf("expected MinMs=0 on full loss, got %f", stats.MinMs)
	}
}

func TestPingProber_WindowAndJitter(t *testing.T) {
	prober := NewPingProber("127.0.0.1:53", 100*time.Millisecond)
	prober.windowSize = 5

	// Manually inject samples to test window trimming and jitter formula
	prober.mu.Lock()
	prober.samples = []PingSample{
		{Timestamp: 100, LatencyMs: 10.0, Success: true},
		{Timestamp: 101, LatencyMs: 20.0, Success: true},
		{Timestamp: 102, LatencyMs: 15.0, Success: true},
		{Timestamp: 103, LatencyMs: 25.0, Success: true},
		{Timestamp: 104, LatencyMs: 30.0, Success: true},
	}
	prober.mu.Unlock()

	stats := prober.GetStats()
	if stats.CurrentMs != 30.0 {
		t.Errorf("expected CurrentMs=30.0, got %f", stats.CurrentMs)
	}
	if stats.MinMs != 10.0 || stats.MaxMs != 30.0 {
		t.Errorf("expected Min=10, Max=30, got Min=%f, Max=%f", stats.MinMs, stats.MaxMs)
	}
	if stats.AvgMs != 20.0 {
		t.Errorf("expected Avg=20.0, got %f", stats.AvgMs)
	}
	if stats.JitterMs <= 0 {
		t.Errorf("expected positive jitter calculation, got %f", stats.JitterMs)
	}

	// Empty sample stats check
	emptyProber := NewPingProber("8.8.8.8:53", 1*time.Second)
	emptyStats := emptyProber.GetStats()
	if len(emptyStats.RecentPoints) != 0 || emptyStats.CurrentMs != 0 {
		t.Errorf("expected empty stats for unprobed target: %+v", emptyStats)
	}
}

func TestPingProber_StartStop(t *testing.T) {
	prober := NewPingProber("127.0.0.1:1", 50*time.Millisecond)
	prober.dialTimeout = 20 * time.Millisecond

	prober.Start()
	prober.Start() // Idempotent start
	time.Sleep(120 * time.Millisecond)
	prober.Stop()
	prober.Stop() // Idempotent stop
}
