package telemetry

import (
	"context"
	"math"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

func getPingBinary() string {
	for _, path := range []string{"/bin/ping", "/usr/bin/ping", "ping"} {
		if p, err := exec.LookPath(path); err == nil {
			return p
		}
	}
	return "/bin/ping"
}

// findWanInterface looks for the active cellular network interface.
func findWanInterface() string {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if strings.HasPrefix(iface.Name, "rmnet_data") ||
				strings.HasPrefix(iface.Name, "rmnet_mhi") ||
				strings.HasPrefix(iface.Name, "wwan") {
				if (iface.Flags & net.FlagUp) != 0 {
					return iface.Name
				}
			}
		}
	}
	return "rmnet_data0"
}

// parsePingOutput extracts latency in ms from ping stdout.
func parsePingOutput(output string) (float64, bool) {
	if idx := strings.Index(output, "time="); idx != -1 {
		rest := output[idx+5:]
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			valStr := strings.TrimSuffix(fields[0], "ms")
			if lat, err := strconv.ParseFloat(valStr, 64); err == nil && lat > 0 {
				return lat, true
			}
		}
	}
	if idx := strings.Index(output, "rtt min/avg/max/mdev = "); idx != -1 {
		rest := output[idx+23:]
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			if lat, err := strconv.ParseFloat(parts[1], 64); err == nil && lat > 0 {
				return lat, true
			}
		}
	}
	return 0, false
}

// ProbeOnce executes a single connection probe and records result.
func (p *PingProber) ProbeOnce() PingSample {
	p.mu.RLock()
	target := p.target
	dialTimeout := p.dialTimeout
	p.mu.RUnlock()

	host := target
	port := "53"
	if h, pt, err := net.SplitHostPort(target); err == nil {
		host = h
		port = pt
	}

	var elapsed float64
	var success bool

	// 1. If targeting loopback/local, test directly via TCP dial
	if host == "127.0.0.1" || host == "localhost" || strings.HasPrefix(host, "127.") {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", target, dialTimeout)
		elapsed = float64(time.Since(start).Microseconds()) / 1000.0
		success = err == nil
		if conn != nil {
			_ = conn.Close()
		}
	} else {
		// 2. Try ICMP ping on cellular interface (handles modem PBR routing tables)
		pingBin := getPingBinary()
		iface := findWanInterface()

		var cmd *exec.Cmd
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if iface != "" {
			cmd = exec.CommandContext(ctx, pingBin, "-I", iface, "-c", "1", "-W", "2", host)
		} else {
			cmd = exec.CommandContext(ctx, pingBin, "-c", "1", "-W", "2", host)
		}
		out, err := cmd.CombinedOutput()
		cancel()

		if err == nil {
			if lat, ok := parsePingOutput(string(out)); ok {
				elapsed = lat
				success = true
			}
		}

		// If failed with specific iface, try without -I
		if !success && iface != "" {
			ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
			out2, err2 := exec.CommandContext(ctx2, pingBin, "-c", "1", "-W", "2", host).CombinedOutput()
			cancel2()
			if err2 == nil {
				if lat, ok := parsePingOutput(string(out2)); ok {
					elapsed = lat
					success = true
				}
			}
		}

		// 3. Fallback to TCP dial with SO_BINDTODEVICE
		if !success {
			start := time.Now()
			dialer := &net.Dialer{
				Timeout: dialTimeout,
				Control: func(network, address string, c syscall.RawConn) error {
					return c.Control(func(fd uintptr) {
						if iface != "" {
							_ = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, 25, iface) // 25 = SO_BINDTODEVICE
						}
					})
				},
			}
			targetAddr := net.JoinHostPort(host, port)
			conn, err := dialer.Dial("tcp", targetAddr)
			elapsed = float64(time.Since(start).Microseconds()) / 1000.0
			success = err == nil
			if conn != nil {
				_ = conn.Close()
			}
		}
	}

	sample := PingSample{
		Timestamp: time.Now().Unix(),
		LatencyMs: elapsed,
		Success:   success,
	}

	if !success {
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
	minVal = math.MaxFloat64
	successCount := 0

	for _, s := range p.samples {
		if s.Success {
			successCount++
			sum += s.LatencyMs
			if s.LatencyMs < minVal {
				minVal = s.LatencyMs
			}
			if s.LatencyMs > maxVal {
				maxVal = s.LatencyMs
			}
		}
	}

	total := len(p.samples)
	lossCount := total - successCount
	stats.LossPct = (float64(lossCount) / float64(total)) * 100.0

	if successCount > 0 {
		stats.AvgMs = sum / float64(successCount)
		stats.MinMs = minVal
		stats.MaxMs = maxVal
		stats.CurrentMs = p.samples[total-1].LatencyMs

		// Calculate RFC 3550 style Mean Absolute Difference Jitter
		if successCount > 1 {
			var diffSum float64
			var prev float64
			first := true
			for _, s := range p.samples {
				if s.Success {
					if !first {
						diffSum += math.Abs(s.LatencyMs - prev)
					}
					prev = s.LatencyMs
					first = false
				}
			}
			stats.JitterMs = diffSum / float64(successCount-1)
		}
	} else {
		stats.MinMs = 0
		stats.MaxMs = 0
		stats.AvgMs = 0
		stats.CurrentMs = 0
		stats.JitterMs = 0
	}

	return stats
}

func (p *PingProber) loop() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Initial probe immediately
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
