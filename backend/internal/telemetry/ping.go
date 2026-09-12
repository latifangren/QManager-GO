package telemetry

import (
	"context"
	"math"
	"net"
	"os"
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

// findWanInterface looks for the active cellular network interface dynamically.
func findWanInterface() string {
	// 1. Check kernel default route table from /proc/net/route
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "00000000" { // Destination 0.0.0.0 (default route)
				iface := fields[0]
				if iface != "lo" && !strings.HasPrefix(iface, "bridge") && !strings.HasPrefix(iface, "rndis") {
					return iface
				}
			}
		}
	}

	// 2. Inspect active network interfaces with IP addresses
	ifaces, err := net.Interfaces()
	if err == nil {
		// Prefer rmnet*, wwan*, usb*, qmi*, ppp*, lte* with FlagUp and assigned IP
		for _, iface := range ifaces {
			name := iface.Name
			if (iface.Flags&net.FlagUp != 0) && (iface.Flags&net.FlagLoopback == 0) {
				if strings.HasPrefix(name, "rmnet") ||
					strings.HasPrefix(name, "wwan") ||
					strings.HasPrefix(name, "usb") ||
					strings.HasPrefix(name, "qmi") ||
					strings.HasPrefix(name, "ppp") ||
					strings.HasPrefix(name, "lte") {
					addrs, _ := iface.Addrs()
					if len(addrs) > 0 {
						return name
					}
				}
			}
		}

		// Fallback: any UP interface that is not loopback / bridge / rndis / eth
		for _, iface := range ifaces {
			name := iface.Name
			if (iface.Flags&net.FlagUp != 0) && (iface.Flags&net.FlagLoopback == 0) {
				if !strings.HasPrefix(name, "bridge") &&
					!strings.HasPrefix(name, "rndis") &&
					!strings.HasPrefix(name, "eth") &&
					!strings.HasPrefix(name, "docker") &&
					!strings.HasPrefix(name, "tunl") &&
					!strings.HasPrefix(name, "sit") &&
					!strings.HasPrefix(name, "gre") {
					return name
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
		iface := findWanInterface()
		targetAddr := net.JoinHostPort(host, port)

		// 2. Primary probe: In-process TCP dial with SO_BINDTODEVICE (~10-20ms, zero subprocess forks)
		start := time.Now()
		dialer := &net.Dialer{
			Timeout: dialTimeout,
			Control: func(network, address string, c syscall.RawConn) error {
				return c.Control(func(fd uintptr) {
					bindSocketToDevice(fd, iface)
				})
			},
		}
		conn, err := dialer.Dial("tcp", targetAddr)
		if err == nil {
			elapsed = float64(time.Since(start).Microseconds()) / 1000.0
			success = true
			_ = conn.Close()
		} else if iface != "" {
			// Also try direct TCP dial without device binding if bound dial failed
			startFallback := time.Now()
			connFb, errFb := net.DialTimeout("tcp", targetAddr, dialTimeout)
			if errFb == nil {
				elapsed = float64(time.Since(startFallback).Microseconds()) / 1000.0
				success = true
				_ = connFb.Close()
			}
		}

		// 3. Fallback: If in-process TCP dial failed, try ICMP ping command
		if !success {
			pingBin := getPingBinary()
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
