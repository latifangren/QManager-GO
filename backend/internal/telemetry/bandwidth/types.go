package bandwidth

// HourlyBucket represents aggregated bandwidth usage within a specific hour (e.g. "2026-09-12 14:00").
type HourlyBucket struct {
	Hour      string `json:"hour"`
	Timestamp int64  `json:"timestamp"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
}

// DailyBucket represents aggregated bandwidth usage for a calendar day (e.g. "2026-09-12").
type DailyBucket struct {
	Date      string `json:"date"`
	Timestamp int64  `json:"timestamp"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
}

// MonthlyBucket represents aggregated bandwidth usage for a calendar month (e.g. "2026-09").
type MonthlyBucket struct {
	Month     string `json:"month"`
	Timestamp int64  `json:"timestamp"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
}

// RealtimePoint represents a 1-second instantaneous rate sample (bits per second).
type RealtimePoint struct {
	Timestamp int64   `json:"timestamp"`
	RxBps     float64 `json:"rx_bps"`
	TxBps     float64 `json:"tx_bps"`
}

// IfaceSnapshot represents the real-time and historical bandwidth state for a network interface.
type IfaceSnapshot struct {
	Name         string          `json:"name"`
	CurrentRxBps float64         `json:"current_rx_bps"`
	CurrentTxBps float64         `json:"current_tx_bps"`
	TotalRxBytes uint64          `json:"total_rx_bytes"`
	TotalTxBytes uint64          `json:"total_tx_bytes"`
	TodayRxBytes uint64          `json:"today_rx_bytes"`
	TodayTxBytes uint64          `json:"today_tx_bytes"`
	Realtime     []RealtimePoint `json:"realtime"` // last 60s
	Hourly       []HourlyBucket  `json:"hourly"`   // last 24h
	Daily        []DailyBucket   `json:"daily"`    // last 30d
	Monthly      []MonthlyBucket `json:"monthly"`  // last 12mo
}

// BandwidthSnapshot is the payload returned by API queries and serialized to disk.
type BandwidthSnapshot struct {
	Timestamp        int64                    `json:"timestamp"`
	Interfaces       map[string]IfaceSnapshot `json:"interfaces"`
	DefaultInterface string                   `json:"default_interface"`
}
