package bandwidth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultStoragePath   = "/etc/qmanager/bandwidth.json"
	DefaultProcNetDev    = "/proc/net/dev"
	DefaultFlushInterval = 1 * time.Hour
	DefaultInterfaceName = "rmnet_data0"
)

// Option configures the Collector instance.
type Option func(*Collector)

// WithDevPath overrides the default /proc/net/dev path for testing.
func WithDevPath(path string) Option {
	return func(c *Collector) {
		if path != "" {
			c.devPath = path
		}
	}
}

// WithFlushInterval overrides the default disk persistence interval.
func WithFlushInterval(interval time.Duration) Option {
	return func(c *Collector) {
		if interval > 0 {
			c.flushInterval = interval
		}
	}
}

// WithTimeFunc overrides time.Now for deterministic testing.
func WithTimeFunc(fn func() time.Time) Option {
	return func(c *Collector) {
		if fn != nil {
			c.nowFunc = fn
		}
	}
}

// WithDefaultInterface sets the default primary network interface name.
func WithDefaultInterface(iface string) Option {
	return func(c *Collector) {
		if iface != "" {
			c.defaultInterface = iface
		}
	}
}

type rawCounters struct {
	rxBytes uint64
	txBytes uint64
	readAt  time.Time
}

type ifaceData struct {
	name         string
	currentRxBps float64
	currentTxBps float64
	totalRxBytes uint64
	totalTxBytes uint64
	todayRxBytes uint64
	todayTxBytes uint64
	lastDate     string
	prevCounters rawCounters
	hasPrev      bool

	realtime []RealtimePoint // up to 60 points
	hourly   []HourlyBucket  // up to 24 buckets
	daily    []DailyBucket   // up to 30 buckets
	monthly  []MonthlyBucket // up to 12 buckets
}

// Collector monitors network interfaces, tracks bandwidth rates, and maintains rolling time buckets.
type Collector struct {
	mu               sync.RWMutex
	storagePath      string
	devPath          string
	flushInterval    time.Duration
	defaultInterface string
	nowFunc          func() time.Time
	interfaces       map[string]*ifaceData
	stopCh           chan struct{}
	running          bool
}

// NewCollector constructs a new bandwidth Collector.
func NewCollector(storagePath string, opts ...Option) *Collector {
	if storagePath == "" {
		storagePath = DefaultStoragePath
	}

	c := &Collector{
		storagePath:      storagePath,
		devPath:          DefaultProcNetDev,
		flushInterval:    DefaultFlushInterval,
		defaultInterface: DefaultInterfaceName,
		nowFunc:          time.Now,
		interfaces:       make(map[string]*ifaceData),
	}

	for _, opt := range opts {
		opt(c)
	}

	_ = c.LoadFromDisk()
	return c
}

func (c *Collector) getOrCreateIfaceLocked(name string) *ifaceData {
	if it, ok := c.interfaces[name]; ok {
		return it
	}
	it := &ifaceData{
		name:     name,
		realtime: make([]RealtimePoint, 0, 60),
		hourly:   make([]HourlyBucket, 0, 24),
		daily:    make([]DailyBucket, 0, 30),
		monthly:  make([]MonthlyBucket, 0, 12),
	}
	c.interfaces[name] = it
	return it
}

// Collect reads and parses /proc/net/dev, calculates data rates, and aggregates buckets.
func (c *Collector) Collect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()

	file, err := os.Open(c.devPath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineIdx := 0

	for scanner.Scan() {
		lineIdx++
		if lineIdx <= 2 {
			continue // Skip /proc/net/dev headers
		}

		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		ifaceName := strings.TrimSpace(parts[0])
		if !isTargetInterface(ifaceName) {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}

		rxBytes, errRx := strconv.ParseUint(fields[0], 10, 64)
		txBytes, errTx := strconv.ParseUint(fields[8], 10, 64)
		if errRx != nil || errTx != nil {
			continue
		}

		it := c.getOrCreateIfaceLocked(ifaceName)

		if !it.hasPrev {
			it.prevCounters = rawCounters{rxBytes: rxBytes, txBytes: txBytes, readAt: now}
			it.hasPrev = true
			continue
		}

		dt := now.Sub(it.prevCounters.readAt).Seconds()
		if dt <= 0.05 {
			continue
		}

		var rxDiff, txDiff uint64
		if rxBytes >= it.prevCounters.rxBytes {
			rxDiff = rxBytes - it.prevCounters.rxBytes
		} else {
			rxDiff = rxBytes // Handle counter rollover or reboot reset
		}

		if txBytes >= it.prevCounters.txBytes {
			txDiff = txBytes - it.prevCounters.txBytes
		} else {
			txDiff = txBytes
		}

		it.prevCounters = rawCounters{rxBytes: rxBytes, txBytes: txBytes, readAt: now}

		rxBps := float64(rxDiff) * 8.0 / dt
		txBps := float64(txDiff) * 8.0 / dt

		it.currentRxBps = rxBps
		it.currentTxBps = txBps
		it.totalRxBytes += rxDiff
		it.totalTxBytes += txDiff

		// Append to rolling 60-sample realtime buffer
		it.realtime = append(it.realtime, RealtimePoint{
			Timestamp: now.Unix(),
			RxBps:     rxBps,
			TxBps:     txBps,
		})
		if len(it.realtime) > 60 {
			it.realtime = it.realtime[len(it.realtime)-60:]
		}

		// 1970 Clock-step guard: skip historical date/month buckets if year < 2024
		if now.Year() < 2024 {
			continue
		}

		currentDate := now.Format("2006-01-02")
		currentHour := now.Format("2006-01-02 15:00")
		currentMonth := now.Format("2006-01")

		// Update Today counters
		if it.lastDate != currentDate {
			it.lastDate = currentDate
			it.todayRxBytes = 0
			it.todayTxBytes = 0
		}
		it.todayRxBytes += rxDiff
		it.todayTxBytes += txDiff

		// Rollup Hourly (last 24h)
		if len(it.hourly) > 0 && it.hourly[len(it.hourly)-1].Hour == currentHour {
			it.hourly[len(it.hourly)-1].RxBytes += rxDiff
			it.hourly[len(it.hourly)-1].TxBytes += txDiff
		} else {
			it.hourly = append(it.hourly, HourlyBucket{
				Hour:      currentHour,
				Timestamp: now.Unix(),
				RxBytes:   rxDiff,
				TxBytes:   txDiff,
			})
			if len(it.hourly) > 24 {
				it.hourly = it.hourly[len(it.hourly)-24:]
			}
		}

		// Rollup Daily (last 30d)
		if len(it.daily) > 0 && it.daily[len(it.daily)-1].Date == currentDate {
			it.daily[len(it.daily)-1].RxBytes += rxDiff
			it.daily[len(it.daily)-1].TxBytes += txDiff
		} else {
			it.daily = append(it.daily, DailyBucket{
				Date:      currentDate,
				Timestamp: now.Unix(),
				RxBytes:   rxDiff,
				TxBytes:   txDiff,
			})
			if len(it.daily) > 30 {
				it.daily = it.daily[len(it.daily)-30:]
			}
		}

		// Rollup Monthly (last 12mo)
		if len(it.monthly) > 0 && it.monthly[len(it.monthly)-1].Month == currentMonth {
			it.monthly[len(it.monthly)-1].RxBytes += rxDiff
			it.monthly[len(it.monthly)-1].TxBytes += txDiff
		} else {
			it.monthly = append(it.monthly, MonthlyBucket{
				Month:     currentMonth,
				Timestamp: now.Unix(),
				RxBytes:   rxDiff,
				TxBytes:   txDiff,
			})
			if len(it.monthly) > 12 {
				it.monthly = it.monthly[len(it.monthly)-12:]
			}
		}
	}
}

func isTargetInterface(name string) bool {
	targets := []string{"rmnet_data0", "bridge0", "eth0", "usb0", "rndis0", "wlan0"}
	for _, t := range targets {
		if strings.HasPrefix(name, t) || name == t {
			return true
		}
	}
	return false
}

// GetSnapshot generates an immutable snapshot of all monitored interfaces.
func (c *Collector) GetSnapshot() BandwidthSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getSnapshotLocked()
}

func (c *Collector) getSnapshotLocked() BandwidthSnapshot {
	now := c.nowFunc().Unix()
	ifaces := make(map[string]IfaceSnapshot, len(c.interfaces))

	for name, it := range c.interfaces {
		rtCopy := make([]RealtimePoint, len(it.realtime))
		copy(rtCopy, it.realtime)

		hourlyCopy := make([]HourlyBucket, len(it.hourly))
		copy(hourlyCopy, it.hourly)

		dailyCopy := make([]DailyBucket, len(it.daily))
		copy(dailyCopy, it.daily)

		monthlyCopy := make([]MonthlyBucket, len(it.monthly))
		copy(monthlyCopy, it.monthly)

		ifaces[name] = IfaceSnapshot{
			Name:         it.name,
			CurrentRxBps: it.currentRxBps,
			CurrentTxBps: it.currentTxBps,
			TotalRxBytes: it.totalRxBytes,
			TotalTxBytes: it.totalTxBytes,
			TodayRxBytes: it.todayRxBytes,
			TodayTxBytes: it.todayTxBytes,
			Realtime:     rtCopy,
			Hourly:       hourlyCopy,
			Daily:        dailyCopy,
			Monthly:      monthlyCopy,
		}
	}

	return BandwidthSnapshot{
		Timestamp:        now,
		Interfaces:       ifaces,
		DefaultInterface: c.defaultInterface,
	}
}

// Reset clears traffic counters for a specific interface or all interfaces.
func (c *Collector) Reset(iface string) {
	c.mu.Lock()
	if iface == "" || iface == "all" {
		for _, it := range c.interfaces {
			resetIfaceData(it)
		}
	} else if it, ok := c.interfaces[iface]; ok {
		resetIfaceData(it)
	}
	c.mu.Unlock()

	_ = c.FlushToDisk()
}

func resetIfaceData(it *ifaceData) {
	it.currentRxBps = 0
	it.currentTxBps = 0
	it.totalRxBytes = 0
	it.totalTxBytes = 0
	it.todayRxBytes = 0
	it.todayTxBytes = 0
	it.realtime = make([]RealtimePoint, 0, 60)
	it.hourly = make([]HourlyBucket, 0, 24)
	it.daily = make([]DailyBucket, 0, 30)
	it.monthly = make([]MonthlyBucket, 0, 12)
}

// FlushToDisk writes the current bandwidth state to storage atomically.
func (c *Collector) FlushToDisk() error {
	c.mu.RLock()
	snap := c.getSnapshotLocked()
	c.mu.RUnlock()

	dir := filepath.Dir(c.storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", c.storagePath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	_ = os.Remove(c.storagePath)
	return os.Rename(tmpFile, c.storagePath)
}

// LoadFromDisk restores persisted bandwidth data on daemon startup.
func (c *Collector) LoadFromDisk() error {
	data, err := os.ReadFile(c.storagePath)
	if err != nil {
		return err
	}

	var snap BandwidthSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for name, ifSnap := range snap.Interfaces {
		it := c.getOrCreateIfaceLocked(name)
		it.totalRxBytes = ifSnap.TotalRxBytes
		it.totalTxBytes = ifSnap.TotalTxBytes
		it.todayRxBytes = ifSnap.TodayRxBytes
		it.todayTxBytes = ifSnap.TodayTxBytes
		it.hourly = ifSnap.Hourly
		it.daily = ifSnap.Daily
		it.monthly = ifSnap.Monthly
	}
	if snap.DefaultInterface != "" {
		c.defaultInterface = snap.DefaultInterface
	}
	return nil
}

// Start launches the background worker loops for periodic collection and disk flush.
func (c *Collector) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	go func() {
		collectTicker := time.NewTicker(1 * time.Second)
		flushTicker := time.NewTicker(c.flushInterval)
		defer collectTicker.Stop()
		defer flushTicker.Stop()

		for {
			select {
			case <-c.stopCh:
				return
			case <-collectTicker.C:
				c.Collect()
			case <-flushTicker.C:
				_ = c.FlushToDisk()
			}
		}
	}()
}

// Close gracefully terminates background loops and flushes state to disk.
func (c *Collector) Close() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return c.FlushToDisk()
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()

	return c.FlushToDisk()
}
