package atengine

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngine_Exec(t *testing.T) {
	mock := NewMockTransport()
	mock.SetResponse("AT", "OK")
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`)

	engine := NewEngine(mock)
	defer engine.Close()

	resp, err := engine.Exec("AT")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Raw != "OK" {
		t.Errorf("expected OK, got %s", resp.Raw)
	}

	resp, err = engine.Exec(`AT+QENG="servingcell"`)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(resp.Raw, "+QENG: \"servingcell\"") {
		t.Errorf("unexpected response: %s", resp.Raw)
	}
}

func TestEngine_PriorityDispatchPreemption(t *testing.T) {
	// Custom blocking/deliberate transport to test priority order
	mock := &slowTransport{
		delay: 20 * time.Millisecond,
	}

	engine := NewEngine(mock)
	defer engine.Close()

	var order []string
	var mu sync.Mutex

	record := func(tag string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, tag)
	}

	// 1. Prime worker with an initial command so it becomes busy
	go func() {
		_, _ = engine.ExecContextWithPriority(context.Background(), "BLOCKER", PriorityNormal)
		record("BLOCKER_DONE")
	}()

	time.Sleep(5 * time.Millisecond) // Ensure worker has picked up BLOCKER

	// 2. Queue multiple Low priority items
	for i := 1; i <= 3; i++ {
		tag := "LOW"
		go func() {
			_, _ = engine.ExecLow(context.Background(), "AT_LOW")
			record(tag)
		}()
	}

	// 3. Queue a Normal priority item
	go func() {
		_, _ = engine.ExecContext(context.Background(), "AT_NORM")
		record("NORM")
	}()

	// 4. Queue a High priority item
	go func() {
		_, _ = engine.ExecHigh(context.Background(), "AT_HIGH")
		record("HIGH")
	}()

	// Wait for all to finish
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 6 {
		t.Fatalf("expected 6 commands completed, got %d: %v", len(order), order)
	}

	// Verify order: BLOCKER_DONE, then HIGH, then NORM, then LOWs
	if order[0] != "BLOCKER_DONE" {
		t.Errorf("expected index 0 to be BLOCKER_DONE, got %s", order[0])
	}
	if order[1] != "HIGH" {
		t.Errorf("expected High priority to run immediately after blocker (index 1), got %s", order[1])
	}
	if order[2] != "NORM" {
		t.Errorf("expected Normal priority to run before Low (index 2), got %s", order[2])
	}
}

func TestEngine_Timeout(t *testing.T) {
	mock := NewMockTransport()
	engine := NewEngine(mock)
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	time.Sleep(15 * time.Millisecond)
	_, err := engine.ExecContext(ctx, "AT+SLOW")
	if err == nil {
		t.Errorf("expected context cancellation error")
	}
}

func TestEngine_ConcurrentStress(t *testing.T) {
	mock := NewMockTransport()
	engine := NewEngine(mock)
	defer engine.Close()

	var completed int32
	var wg sync.WaitGroup

	numGoroutines := 30
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		prio := Priority(i % 3)
		go func(p Priority) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			res, err := engine.ExecContextWithPriority(ctx, "AT", p)
			if err == nil && res.Success {
				atomic.AddInt32(&completed, 1)
			}
		}(prio)
	}

	wg.Wait()

	if atomic.LoadInt32(&completed) != int32(numGoroutines) {
		t.Errorf("expected %d completed commands, got %d", numGoroutines, completed)
	}
}

type slowTransport struct {
	delay time.Duration
}

func (s *slowTransport) Send(ctx context.Context, cmd string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(s.delay):
		return "OK", nil
	}
}

func (s *slowTransport) Close() error {
	return nil
}

func TestDeviceTransport_Terminators(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"AT\r\n\r\nOK\r\n", true},
		{"AT\n\nOK\n", true},
		{"+QENG: ...\r\nOK", true},
		{"\r\nERROR\r\n", true},
		{"+CME ERROR: 100\r\n", true},
		{"+CMS ERROR: 500\r\n", true},
		{"\r\nNO CARRIER\r\n", true},
		{"\r\nBUSY\r\n", true},
		{"some intermediate text without terminator", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isTerminator([]byte(tt.input))
		if got != tt.expected {
			t.Errorf("isTerminator(%q) = %v, expected %v", tt.input, got, tt.expected)
		}
	}
}

func TestDeviceTransport_ReadResponse(t *testing.T) {
	buf := bytes.NewBufferString("\r\n+CPIN: READY\r\n\r\nOK\r\n")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	resp, err := readResponse(ctx, buf)
	if err != nil {
		t.Fatalf("readResponse failed: %v", err)
	}
	if !strings.Contains(resp, "READY") || !strings.Contains(resp, "OK") {
		t.Errorf("unexpected readResponse: %s", resp)
	}

	// Test Error handling
	errBuf := bytes.NewBufferString("\r\n+CME ERROR: 10\r\n")
	_, err = readResponse(ctx, errBuf)
	if !errors.Is(err, ErrATCommand) {
		t.Errorf("expected ErrATCommand, got %v", err)
	}
}

func TestDeviceTransport_NonExistentDevice(t *testing.T) {
	dt := NewDeviceTransport("/dev/non_existent_smd_device")
	defer dt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := dt.Send(ctx, "AT")
	if !errors.Is(err, ErrNoDevice) {
		t.Errorf("expected ErrNoDevice on non-existent device path, got %v", err)
	}
}

func TestAutoDetectTransport(t *testing.T) {
	transport := AutoDetectTransport()
	if transport == nil {
		t.Fatalf("expected non-nil transport")
	}
	defer transport.Close()
}
