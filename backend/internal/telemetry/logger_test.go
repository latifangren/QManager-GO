package telemetry

import (
	"fmt"
	"log"
	"strings"
	"testing"
)

func TestRingBufferLogger_Basic(t *testing.T) {
	buf := NewRingBufferLogger(5)

	buf.Add(LevelInfo, "system", "Message 1")
	buf.Add(LevelWarn, "radio", "Message 2")
	buf.Add(LevelError, "watchdog", "Message 3")

	if buf.Count() != 3 {
		t.Errorf("expected count=3, got %d", buf.Count())
	}

	lines := buf.GetLines(10, "")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	if !strings.Contains(lines[0], "Message 1") {
		t.Errorf("expected line 0 to have Message 1, got %s", lines[0])
	}
	if !strings.Contains(lines[2], "Message 3") {
		t.Errorf("expected line 2 to have Message 3, got %s", lines[2])
	}
}

func TestRingBufferLogger_CapacityWrapAround(t *testing.T) {
	capacity := 4
	buf := NewRingBufferLogger(capacity)

	for i := 1; i <= 10; i++ {
		buf.Add(LevelInfo, "app", fmt.Sprintf("Log entry %d", i))
	}

	if buf.Count() != capacity {
		t.Errorf("expected buffer count to cap at %d, got %d", capacity, buf.Count())
	}

	records := buf.GetRecords(10, "", "", "")
	if len(records) != capacity {
		t.Fatalf("expected %d records after wrap, got %d", capacity, len(records))
	}

	// Should contain entries 7, 8, 9, 10
	if records[0].Message != "Log entry 7" {
		t.Errorf("expected oldest record to be 'Log entry 7', got %s", records[0].Message)
	}
	if records[3].Message != "Log entry 10" {
		t.Errorf("expected newest record to be 'Log entry 10', got %s", records[3].Message)
	}
}

func TestRingBufferLogger_Filtering(t *testing.T) {
	buf := NewRingBufferLogger(10)
	buf.Add(LevelInfo, "poller", "Signal poller tick")
	buf.Add(LevelError, "atengine", "Timeout on /dev/smd11")
	buf.Add(LevelWarn, "watchdog", "Probe failure count: 2")
	buf.Add(LevelInfo, "poller", "Signal poller tick 2")

	// Filter by Level
	errorsOnly := buf.GetRecords(10, "ERROR", "", "")
	if len(errorsOnly) != 1 || errorsOnly[0].Source != "atengine" {
		t.Errorf("expected 1 ERROR record, got %v", errorsOnly)
	}

	// Filter by Source
	pollerOnly := buf.GetRecords(10, "", "poller", "")
	if len(pollerOnly) != 2 {
		t.Errorf("expected 2 poller records, got %d", len(pollerOnly))
	}

	// Filter by Search substring
	smdMatch := buf.GetRecords(10, "", "", "smd11")
	if len(smdMatch) != 1 {
		t.Errorf("expected 1 search match, got %d", len(smdMatch))
	}
}

func TestRingBufferLogger_StdLogIntegration(t *testing.T) {
	logger := GetGlobalLogger()
	logger.Clear()

	log.Println("Test message from standard Go logger")

	if logger.Count() != 1 {
		t.Fatalf("expected standard log capture in ring buffer, count=%d", logger.Count())
	}

	records := logger.GetRecords(1, "", "", "")
	if !strings.Contains(records[0].Message, "Test message from standard Go logger") {
		t.Errorf("unexpected captured message: %s", records[0].Message)
	}
}
