package telemetry

import (
	"fmt"
	"math"
	"net"
	"os"
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
	if strings.TrimSpace(target) == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = target
}

// Start begins continuous background probing.
func (p *PingProber) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopCh = make(chan struct{})
	p.mu.Unlock()

	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		// Initial immediate probe
		p.recordSample(p.probeTarget())

		for {
			select {
			case <-p.stopCh:
				return
			case <-ticker.C:
				p.recordSample(p.probeTarget())
			}
		}
	}()
}

// Stop halts the background prober loop.
func (p *PingProber) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	close(p.stopCh)
	p.running = false
}

// ProbeOnce executes an immediate synchronous probe and records it.
func (p *PingProber) ProbeOnce() PingSample {
	s := p.probeTarget()
	p.recordSample(s)
	return s
}

// recordSample appends a sample and trims to window size.
func (p *PingProber) recordSample(s PingSample) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.samples = append(p.samples, s)
	if len(p.samples) > p.windowSize {
		p.samples = p.samples[len(p.samples)-p.windowSize:]
	}
}

// GetStats returns calculated jitter, loss, and latency metrics.
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

	var totalMs, minMs, maxMs float64
	var successCount int
	minMs = math.MaxFloat64
	var latencies []float64

	for _, s := range p.samples {
		if s.Success {
			successCount++
			totalMs += s.LatencyMs
			if s.LatencyMs < minMs {
				minMs = s.LatencyMs
			}
			if s.LatencyMs > maxMs {
				maxMs = s.LatencyMs
			}
			latencies = append(latencies, s.LatencyMs)
			stats.CurrentMs = s.LatencyMs
		}
	}

	totalSamples := len(p.samples)
	stats.LossPct = float64(totalSamples-successCount) / float64(totalSamples) * 100.0

	if successCount > 0 {
		stats.AvgMs = totalMs / float64(successCount)
		stats.MinMs = minMs
		stats.MaxMs = maxMs

		// Calculate jitter (RFC 1889 mean difference between consecutive successful samples)
		if len(latencies) > 1 {
			var jitterSum float64
			for i := 1; i < len(latencies); i++ {
				jitterSum += math.Abs(latencies[i] - latencies[i-1])
			}
			stats.JitterMs = jitterSum / float64(len(latencies)-1)
		}
	}

	return stats
}

// getPingBinary finds ping binary if available (for test/legacy support).
func getPingBinary() string {
	for _, p := range []string{"/bin/ping", "/usr/bin/ping", "ping"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "ping"
}

// findWanInterface looks for active cellular network interface dynamically.
func findWanInterface() string {
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "00000000" { // Destination 0.0.0.0
				iface := fields[0]
				if iface != "lo" && !strings.HasPrefix(iface, "bridge") && !strings.HasPrefix(iface, "rndis") {
					return iface
				}
			}
		}
	}

	ifaces, err := net.Interfaces()
	if err == nil {
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
	}
	return ""
}

// InProcessICMPProbe sends an ICMP Echo Request in-process without spawning external subprocesses.
func InProcessICMPProbe(targetIP string, iface string, timeout time.Duration) (float64, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		fd, err = syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
		if err != nil {
			return 0, err
		}
	}
	defer syscall.Close(fd)

	if iface != "" {
		_ = syscall.BindToDevice(fd, iface)
	}

	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv)

	ip := net.ParseIP(targetIP)
	if ip == nil {
		ips, err := net.LookupIP(targetIP)
		if err != nil || len(ips) == 0 {
			return 0, fmt.Errorf("invalid host %s", targetIP)
		}
		ip = ips[0]
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0, fmt.Errorf("ipv4 only supported")
	}

	var sa syscall.SockaddrInet4
	copy(sa.Addr[:], ip4)

	// ICMP Echo packet (8 bytes header + payload)
	packet := []byte{
		8, 0, // Type 8 (Echo), Code 0
		0, 0, // Checksum placeholder
		0x12, 0x34, // Identifier
		0x00, 0x01, // Sequence
		'Q', 'M', 'A', 'N', 'A', 'G', 'E', 'R',
	}

	var csum uint32
	for i := 0; i < len(packet)-1; i += 2 {
		csum += uint32(packet[i])<<8 | uint32(packet[i+1])
	}
	if len(packet)%2 == 1 {
		csum += uint32(packet[len(packet)-1]) << 8
	}
	for (csum >> 16) > 0 {
		csum = (csum & 0xffff) + (csum >> 16)
	}
	csum = ^csum
	packet[2] = byte(csum >> 8)
	packet[3] = byte(csum & 0xff)

	start := time.Now()
	if err := syscall.Sendto(fd, packet, 0, &sa); err != nil {
		return 0, err
	}

	buf := make([]byte, 512)
	_, _, err = syscall.Recvfrom(fd, buf, 0)
	if err != nil {
		return 0, err
	}
	elapsed := float64(time.Since(start).Microseconds()) / 1000.0
	return elapsed, nil
}

// probeTarget conducts pure in-process latency measurement (ICMP Raw Socket + TCP Dial fallback).
func (p *PingProber) probeTarget() PingSample {
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

	// 1. Loopback check
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

		// 2. Primary: Pure in-process raw ICMP ping (<1ms overhead, zero forks)
		lat, err := InProcessICMPProbe(host, iface, dialTimeout)
		if err == nil && lat > 0 {
			elapsed = lat
			success = true
		} else {
			// 3. Fallback: In-process TCP dial with socket binding
			start := time.Now()
			targetAddr := net.JoinHostPort(host, port)
			dialer := &net.Dialer{
				Timeout: dialTimeout,
				Control: func(network, address string, c syscall.RawConn) error {
					return c.Control(func(fd uintptr) {
						if iface != "" {
							_ = syscall.BindToDevice(int(fd), iface)
						}
					})
				},
			}
			conn, errDial := dialer.Dial("tcp", targetAddr)
			if errDial == nil {
				elapsed = float64(time.Since(start).Microseconds()) / 1000.0
				success = true
				_ = conn.Close()
			} else {
				// Direct TCP dial without binding
				startFb := time.Now()
				connFb, errFb := net.DialTimeout("tcp", targetAddr, dialTimeout)
				if errFb == nil {
					elapsed = float64(time.Since(startFb).Microseconds()) / 1000.0
					success = true
					_ = connFb.Close()
				}
			}
		}
	}

	if !success {
		elapsed = 0
	}

	sample := PingSample{
		Timestamp: time.Now().Unix(),
		LatencyMs: math.Round(elapsed*100) / 100,
		Success:   success,
	}

	return sample
}

// parsePingOutput parses standard ping CLI string output (kept for backward unit test compatibility).
func parsePingOutput(out string) (float64, bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "time=") {
			idx := strings.Index(line, "time=")
			sub := line[idx+5:]
			fields := strings.Fields(sub)
			if len(fields) > 0 {
				valStr := strings.TrimSuffix(fields[0], "ms")
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					return val, true
				}
			}
		}
		if strings.HasPrefix(line, "rtt min/avg/max/mdev = ") || strings.HasPrefix(line, "round-trip min/avg/max = ") {
			parts := strings.Split(line, "=")
			if len(parts) >= 2 {
				valParts := strings.Split(strings.TrimSpace(parts[1]), "/")
				if len(valParts) >= 2 {
					if avg, err := strconv.ParseFloat(valParts[1], 64); err == nil {
						return avg, true
					}
				}
			}
		}
	}
	return 0, false
}
