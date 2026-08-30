package telemetry

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// LogLevel represents log severity.
type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

// LogRecord represents a structured in-memory log line.
type LogRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Level     LogLevel  `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

// Format returns standard formatted string for log entry.
func (r LogRecord) Format() string {
	return fmt.Sprintf("[%s] [%s] [%s] %s", r.Timestamp.Format("2006-01-02 15:04:05"), r.Level, r.Source, r.Message)
}

// RingBufferLogger maintains a thread-safe, fixed-size circular buffer in RAM.
// It completely prevents continuous disk wear on raw NAND/UBIFS storage.
type RingBufferLogger struct {
	capacity int
	entries  []LogRecord
	head     int
	count    int
	mu       sync.RWMutex
}

var (
	defaultLogger *RingBufferLogger
	loggerOnce    sync.Once
)

// GetGlobalLogger returns the shared process-wide in-memory ring buffer logger.
func GetGlobalLogger() *RingBufferLogger {
	loggerOnce.Do(func() {
		defaultLogger = NewRingBufferLogger(1000)
		// Hook into standard Go log package as an io.Writer
		log.SetOutput(defaultLogger)
		log.SetFlags(0) // Timestamping handled by RingBufferLogger
	})
	return defaultLogger
}

// NewRingBufferLogger creates an in-memory fixed-capacity logger.
func NewRingBufferLogger(capacity int) *RingBufferLogger {
	if capacity <= 0 {
		capacity = 500
	}
	return &RingBufferLogger{
		capacity: capacity,
		entries:  make([]LogRecord, capacity),
		head:     0,
		count:    0,
	}
}

// Add appends a new structured log entry into the circular buffer.
func (b *RingBufferLogger) Add(level LogLevel, source, message string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	rec := LogRecord{
		Timestamp: time.Now(),
		Level:     level,
		Source:    source,
		Message:   strings.TrimSpace(message),
	}

	b.entries[b.head] = rec
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
}

// Write satisfies io.Writer to intercept standard Go log.Printf / log.Println calls.
func (b *RingBufferLogger) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		b.Add(LevelInfo, "daemon", msg)
	}
	return len(p), nil
}

// GetRecords returns chronological log entries (oldest to newest) with optional filtering.
func (b *RingBufferLogger) GetRecords(max int, levelFilter, sourceFilter, search string) []LogRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return nil
	}

	result := make([]LogRecord, 0, b.count)
	startIdx := (b.head - b.count + b.capacity) % b.capacity

	levelFilter = strings.ToUpper(strings.TrimSpace(levelFilter))
	sourceFilter = strings.ToLower(strings.TrimSpace(sourceFilter))
	search = strings.ToLower(strings.TrimSpace(search))

	for i := 0; i < b.count; i++ {
		idx := (startIdx + i) % b.capacity
		rec := b.entries[idx]

		if levelFilter != "" && strings.ToUpper(string(rec.Level)) != levelFilter {
			continue
		}
		if sourceFilter != "" && !strings.Contains(strings.ToLower(rec.Source), sourceFilter) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(rec.Message), search) && !strings.Contains(strings.ToLower(rec.Source), search) {
			continue
		}

		result = append(result, rec)
	}

	if max > 0 && len(result) > max {
		// Return the most recent 'max' items
		result = result[len(result)-max:]
	}

	return result
}

// GetLines returns formatted string lines matching filter parameters.
func (b *RingBufferLogger) GetLines(max int, filter string) []string {
	records := b.GetRecords(max, "", "", filter)
	lines := make([]string, len(records))
	for i, r := range records {
		lines[i] = r.Format()
	}
	return lines
}

// Clear empties the ring buffer.
func (b *RingBufferLogger) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.count = 0
}

// Count returns the number of active log entries in memory.
func (b *RingBufferLogger) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// Ensure interface satisfaction
var _ io.Writer = (*RingBufferLogger)(nil)
