package telemetry

import (
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

// PingSample represents a single latency probe data point.
type PingSample struct {
	Timestamp int64   `json:"timestamp"`
	LatencyMs float64 `json:"latency_ms"`
	Success   bool    `json:"success"`
}

// PingStats represents aggregated probe metrics over a window.
type PingStats struct {
	Target       string       `json:"target"`
	CurrentMs    float64      `json:"current_ms"`
	MinMs        float64      `json:"min_ms"`
	MaxMs        float64      `json:"max_ms"`
	AvgMs        float64      `json:"avg_ms"`
	JitterMs     float64      `json:"jitter_ms"`
	LossPct      float64      `json:"loss_pct"`
	RecentPoints []PingSample `json:"recent_points"`
}

// PingProber manages continuous latency and jitter measurements.
type PingProber struct {
	target      string
	interval    time.Duration
	windowSize  int
	samples     []PingSample
	mu          sync.RWMutex
	stopCh      chan struct{}
	running     bool
	dialTimeout time.Duration
}

// NewPingProber creates a prober for a given target (e.g., "1.1.1.1:53" or "8.8.8.8:53").
func NewPingProber(target string, interval time.Duration) *PingProber {
	if target == "" {
		target = "1.1.1.1:53"
	}
	if interval < 500*time.Millisecond {
		interval = 1 * time.Second
	}
	return &PingProber{
		target:      target,
		interval:    interval,
		windowSize:  30,
		samples:     make([]PingSample, 0, 30),
		stopCh:      make(chan struct{}),
		dialTimeout: 1500 * time.Millisecond,
	}
}

// SetTarget updates the probe destination target.
func (p *PingProber) SetTarget(target string) {
	if target == "" {
		return
	}
	if !strings.Contains(target, ":") {
		target = target + ":53"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = target
}

// Start begins probe background loop.
func (p *PingProber) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	go p.loop()
}

// Stop halts the prober.
func (p *PingProber) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.running = false
	close(p.stopCh)
}

// ProbeOnce executes a single connection probe and records result.
func (p *PingProber) ProbeOnce() PingSample {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", p.target, p.dialTimeout)
	elapsed := float64(time.Since(start).Microseconds()) / 1000.0

	sample := PingSample{
		Timestamp: time.Now().Unix(),
		LatencyMs: elapsed,
		Success:   err == nil,
	}

	if err == nil {
		_ = conn.Close()
	} else {
		sample.LatencyMs = 0
	}

	p.mu.Lock()
	if len(p.samples) >= p.windowSize {
		p.samples = p.samples[1:]
	}
	p.samples = append(p.samples, sample)
	p.mu.Unlock()

	// Record in-memory ping history point (Zero Flash Wear)
	var latVal *float64
	if sample.Success {
		latVal = &sample.LatencyMs
	}
	loss := 0.0
	if !sample.Success {
		loss = 100.0
	}

	GetGlobalHistory().RecordPing(PingHistoryPoint{
		Timestamp: sample.Timestamp,
		LatencyMs: latVal,
		AvgMs:     latVal,
		MinMs:     latVal,
		MaxMs:     latVal,
		LossPct:   loss,
		JitterMs:  nil,
	})

	return sample
}

// GetStats calculates summary metrics from recent window.
func (p *PingProber) GetStats() PingStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PingStats{
		Target:       p.target,
		RecentPoints: make([]PingSample, len(p.samples)),
	}
	copy(stats.RecentPoints, p.samples)

	if len(p.samples) == 0 {
		return stats
	}

	var sum, minVal, maxVal float64
	var successCount int
	var latencies []float64

	minVal = math.MaxFloat64

	for _, s := range p.samples {
		if s.Success {
			successCount++
			sum += s.LatencyMs
			latencies = append(latencies, s.LatencyMs)
			if s.LatencyMs < minVal {
				minVal = s.LatencyMs
			}
			if s.LatencyMs > maxVal {
				maxVal = s.LatencyMs
			}
			stats.CurrentMs = s.LatencyMs
		}
	}

	total := len(p.samples)
	stats.LossPct = float64(total-successCount) / float64(total) * 100.0

	if successCount > 0 {
		stats.MinMs = minVal
		stats.MaxMs = maxVal
		stats.AvgMs = sum / float64(successCount)

		// Calculate jitter (mean absolute deviation of successive samples)
		if len(latencies) > 1 {
			var diffSum float64
			for i := 1; i < len(latencies); i++ {
				diffSum += math.Abs(latencies[i] - latencies[i-1])
			}
			stats.JitterMs = diffSum / float64(len(latencies)-1)
		}
	} else {
		stats.MinMs = 0
	}

	return stats
}

func (p *PingProber) loop() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.ProbeOnce()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.ProbeOnce()
		}
	}
}
