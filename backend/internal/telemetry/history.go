package telemetry

import (
	"sync"
	"time"
)

// SignalHistoryPoint represents one sample in signal history matching SignalHistoryEntry.
type SignalHistoryPoint struct {
	TS      int64  `json:"ts"`
	LteRsrp []*int `json:"lte_rsrp"`
	LteRsrq []*int `json:"lte_rsrq"`
	LteSinr []*int `json:"lte_sinr"`
	NrRsrp  []*int `json:"nr_rsrp"`
	NrRsrq  []*int `json:"nr_rsrq"`
	NrSinr  []*int `json:"nr_sinr"`
}

// PingHistoryPoint represents one aggregated ping sample.
type PingHistoryPoint struct {
	Timestamp int64    `json:"ts"`
	LatencyMs *float64 `json:"lat"`
	AvgMs     *float64 `json:"avg"`
	MinMs     *float64 `json:"min"`
	MaxMs     *float64 `json:"max"`
	LossPct   float64  `json:"loss"`
	JitterMs  *float64 `json:"jit"`
}

// NetworkEventItem represents one recent network event.
type NetworkEventItem struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Severity  string `json:"severity"` // "info", "warning", "error"
}

// TelemetryHistory maintains thread-safe in-memory circular buffers for Signal, Ping, and Events.
// All data is kept strictly in RAM (Zero Flash Wear policy).
type TelemetryHistory struct {
	mu           sync.RWMutex
	signalBuffer []SignalHistoryPoint
	signalCap    int
	signalHead   int
	signalCount  int

	pingBuffer   []PingHistoryPoint
	pingCap      int
	pingHead     int
	pingCount    int

	eventBuffer  []NetworkEventItem
	eventCap     int
	eventHead    int
	eventCount   int
}

var (
	globalHistory *TelemetryHistory
	historyOnce   sync.Once
)

// GetGlobalHistory returns the shared singleton TelemetryHistory manager.
func GetGlobalHistory() *TelemetryHistory {
	historyOnce.Do(func() {
		globalHistory = NewTelemetryHistory(1800, 1440, 500)
	})
	return globalHistory
}

// NewTelemetryHistory creates a new TelemetryHistory manager with specified capacities.
func NewTelemetryHistory(signalCap, pingCap, eventCap int) *TelemetryHistory {
	if signalCap <= 0 {
		signalCap = 1800 // 30 mins at 1 Hz
	}
	if pingCap <= 0 {
		pingCap = 1440 // 24 hours at 1/min
	}
	if eventCap <= 0 {
		eventCap = 500
	}

	return &TelemetryHistory{
		signalBuffer: make([]SignalHistoryPoint, signalCap),
		signalCap:    signalCap,
		pingBuffer:   make([]PingHistoryPoint, pingCap),
		pingCap:      pingCap,
		eventBuffer:  make([]NetworkEventItem, eventCap),
		eventCap:     eventCap,
	}
}

// RecordSignal appends a signal sample into the ring buffer.
func (h *TelemetryHistory) RecordSignal(pt SignalHistoryPoint) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if pt.TS == 0 {
		pt.TS = time.Now().Unix()
	}

	h.signalBuffer[h.signalHead] = pt
	h.signalHead = (h.signalHead + 1) % h.signalCap
	if h.signalCount < h.signalCap {
		h.signalCount++
	}
}

// GetSignalHistory returns chronological signal entries.
func (h *TelemetryHistory) GetSignalHistory(max int) []SignalHistoryPoint {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.signalCount == 0 {
		return []SignalHistoryPoint{}
	}

	result := make([]SignalHistoryPoint, 0, h.signalCount)
	startIdx := (h.signalHead - h.signalCount + h.signalCap) % h.signalCap

	for i := 0; i < h.signalCount; i++ {
		idx := (startIdx + i) % h.signalCap
		pt := h.signalBuffer[idx]
		if pt.LteRsrp == nil {
			pt.LteRsrp = []*int{nil, nil, nil, nil}
		}
		if pt.LteRsrq == nil {
			pt.LteRsrq = []*int{nil, nil, nil, nil}
		}
		if pt.LteSinr == nil {
			pt.LteSinr = []*int{nil, nil, nil, nil}
		}
		if pt.NrRsrp == nil {
			pt.NrRsrp = []*int{nil, nil, nil, nil}
		}
		if pt.NrRsrq == nil {
			pt.NrRsrq = []*int{nil, nil, nil, nil}
		}
		if pt.NrSinr == nil {
			pt.NrSinr = []*int{nil, nil, nil, nil}
		}
		result = append(result, pt)
	}

	if max > 0 && len(result) > max {
		result = result[len(result)-max:]
	}

	if result == nil {
		return []SignalHistoryPoint{}
	}
	return result
}

// RecordPing appends a ping measurement into the ring buffer.
func (h *TelemetryHistory) RecordPing(pt PingHistoryPoint) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if pt.Timestamp == 0 {
		pt.Timestamp = time.Now().Unix()
	}

	h.pingBuffer[h.pingHead] = pt
	h.pingHead = (h.pingHead + 1) % h.pingCap
	if h.pingCount < h.pingCap {
		h.pingCount++
	}
}

// GetPingHistory returns chronological ping entries.
func (h *TelemetryHistory) GetPingHistory(max int) []PingHistoryPoint {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.pingCount == 0 {
		return []PingHistoryPoint{}
	}

	result := make([]PingHistoryPoint, 0, h.pingCount)
	startIdx := (h.pingHead - h.pingCount + h.pingCap) % h.pingCap

	for i := 0; i < h.pingCount; i++ {
		idx := (startIdx + i) % h.pingCap
		result = append(result, h.pingBuffer[idx])
	}

	if max > 0 && len(result) > max {
		result = result[len(result)-max:]
	}

	if result == nil {
		return []PingHistoryPoint{}
	}
	return result
}

// RecordEvent appends an event into the ring buffer.
func (h *TelemetryHistory) RecordEvent(evt NetworkEventItem) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if evt.Timestamp == 0 {
		evt.Timestamp = time.Now().Unix()
	}

	h.eventBuffer[h.eventHead] = evt
	h.eventHead = (h.eventHead + 1) % h.eventCap
	if h.eventCount < h.eventCap {
		h.eventCount++
	}
}

// GetEvents returns chronological network events.
func (h *TelemetryHistory) GetEvents(max int) []NetworkEventItem {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.eventCount == 0 {
		return []NetworkEventItem{}
	}

	result := make([]NetworkEventItem, 0, h.eventCount)
	startIdx := (h.eventHead - h.eventCount + h.eventCap) % h.eventCap

	for i := 0; i < h.eventCount; i++ {
		idx := (startIdx + i) % h.eventCap
		result = append(result, h.eventBuffer[idx])
	}

	if max > 0 && len(result) > max {
		result = result[len(result)-max:]
	}

	if result == nil {
		return []NetworkEventItem{}
	}
	return result
}
