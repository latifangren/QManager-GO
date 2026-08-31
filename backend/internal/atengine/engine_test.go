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

func TestEngine_ExecutePriorityWrappersAndTimeout(t *testing.T) {
	mock := NewMockTransport()
	mock.SetResponse("AT", "OK")
	mock.SetResponse("AT+HIGH", "OK_HIGH")
	mock.SetResponse("AT+NORM", "OK_NORM")
	mock.SetResponse("AT+LOW", "OK_LOW")

	engine := NewEngine(mock)
	defer engine.Close()

	// Test SetTimeout / GetTimeout
	engine.SetTimeout(10 * time.Second)
	if engine.GetTimeout() != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", engine.GetTimeout())
	}

	ctx := context.Background()

	// Execute (Normal)
	raw, err := engine.Execute(ctx, "AT+NORM")
	if err != nil || raw != "OK_NORM" {
		t.Errorf("Execute failed: raw=%s, err=%v", raw, err)
	}

	// ExecuteHigh
	raw, err = engine.ExecuteHigh(ctx, "AT+HIGH")
	if err != nil || raw != "OK_HIGH" {
		t.Errorf("ExecuteHigh failed: raw=%s, err=%v", raw, err)
	}

	// ExecuteLow
	raw, err = engine.ExecuteLow(ctx, "AT+LOW")
	if err != nil || raw != "OK_LOW" {
		t.Errorf("ExecuteLow failed: raw=%s, err=%v", raw, err)
	}

	// ExecuteWithPriority
	raw, err = engine.ExecuteWithPriority(ctx, "AT", PriorityHigh)
	if err != nil || raw != "OK" {
		t.Errorf("ExecuteWithPriority failed: raw=%s, err=%v", raw, err)
	}
}

// 1. 3-tier priority queue arbitration (PriorityHigh, PriorityNormal, PriorityLow)
func TestEngine_PriorityDispatchPreemption(t *testing.T) {
	mock := &slowTransport{
		delay: 25 * time.Millisecond,
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

	// Prime worker with an initial command so it becomes busy
	go func() {
		_, _ = engine.ExecContextWithPriority(context.Background(), "BLOCKER", PriorityNormal)
		record("BLOCKER_DONE")
	}()

	time.Sleep(5 * time.Millisecond) // Ensure worker has picked up BLOCKER

	// Queue multiple Low priority items
	for i := 1; i <= 3; i++ {
		go func() {
			_, _ = engine.ExecLow(context.Background(), "AT_LOW")
			record("LOW")
		}()
	}

	// Queue a Normal priority item
	go func() {
		_, _ = engine.ExecContext(context.Background(), "AT_NORM")
		record("NORM")
	}()

	// Queue a High priority item
	go func() {
		_, _ = engine.ExecHigh(context.Background(), "AT_HIGH")
		record("HIGH")
	}()

	// Wait for all to finish
	time.Sleep(250 * time.Millisecond)

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

// 1b. Saturated low-priority queue does not block high-priority emergency command, and drops when full.
func TestEngine_LowPriorityQueueSaturationAndDropping(t *testing.T) {
	mock := &slowTransport{
		delay: 50 * time.Millisecond,
	}

	engine := NewEngine(mock)
	defer engine.Close()

	// Make worker busy with a long-running normal command
	go func() {
		_, _ = engine.ExecContextWithPriority(context.Background(), "BLOCKER", PriorityNormal)
	}()

	time.Sleep(5 * time.Millisecond)

	// Scribe 64 items to low channel (capacity is 64)
	var droppedCount int32
	var queuedCount int32
	var wg sync.WaitGroup

	for i := 0; i < 70; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := engine.ExecLow(context.Background(), "AT_LOW_BURST")
			if errors.Is(err, ErrQueueFull) {
				atomic.AddInt32(&droppedCount, 1)
			} else if err == nil {
				atomic.AddInt32(&queuedCount, 1)
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&droppedCount) < 6 {
		t.Errorf("expected at least 6 low-priority commands dropped due to full queue, got %d", droppedCount)
	}

	// Now send a High priority emergency command - must succeed promptly without being blocked by low queue saturation
	start := time.Now()
	res, err := engine.ExecHigh(context.Background(), "AT+CFUN=0")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected ExecHigh to succeed, got error: %v", err)
	}
	if res.Command != "AT+CFUN=0" {
		t.Errorf("unexpected command: %s", res.Command)
	}
	// High command should only wait for BLOCKER (50ms) and its own execution (50ms), not the whole 64 low queue
	if elapsed > 200*time.Millisecond {
		t.Errorf("ExecHigh took too long (%v), was likely starved by low-priority backlog", elapsed)
	}
}

// 2. Context cancellation and timeouts (verify cancelled requests don't hang worker loop or leak goroutines)
func TestEngine_ContextCancellationAndTimeout(t *testing.T) {
	mock := &slowTransport{
		delay: 100 * time.Millisecond,
	}

	engine := NewEngine(mock)
	defer engine.Close()

	// Case 1: Cancel context before worker processes it
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-canceled

	_, err := engine.ExecContext(ctx, "AT+FAST_CANCEL")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}

	// Case 2: Timeout while waiting in queue
	go func() {
		_, _ = engine.ExecContextWithPriority(context.Background(), "BLOCKER", PriorityNormal)
	}()
	time.Sleep(5 * time.Millisecond)

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelTimeout()

	_, err = engine.ExecContext(ctxTimeout, "AT+TIMEOUT")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}

	// Case 3: Engine continues functioning cleanly after cancellations
	res, err := engine.ExecContext(context.Background(), "AT+RECOVERY_CHECK")
	if err != nil {
		t.Fatalf("expected recovery check to succeed, got error: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success=true")
	}
}

// 3. Low-allocation parser helpers and Result status
func TestEngine_ResultStatusAndIsATError(t *testing.T) {
	mock := NewMockTransport()
	mock.SetResponse("AT+CME ERROR: 10", "+CME ERROR: 10")
	mock.SetResponse("AT+CMS ERROR: 302", "+CMS ERROR: 302")
	mock.SetResponse("AT+ERR", "ERROR")
	mock.SetResponse("AT+OK", "OK")

	engine := NewEngine(mock)
	defer engine.Close()

	tests := []struct {
		cmd        string
		expectErr  bool
		expectSucc bool
	}{
		{"AT+OK", false, true},
		{"AT+CME ERROR: 10", true, false},
		{"AT+CMS ERROR: 302", true, false},
		{"AT+ERR", true, false},
	}

	for _, tt := range tests {
		res, err := engine.Exec(tt.cmd)
		if tt.expectErr && err == nil {
			t.Errorf("cmd %s: expected error, got nil", tt.cmd)
		}
		if !tt.expectErr && err != nil {
			t.Errorf("cmd %s: expected no error, got %v", tt.cmd, err)
		}
		if res.Success != tt.expectSucc {
			t.Errorf("cmd %s: expected success=%v, got %v", tt.cmd, tt.expectSucc, res.Success)
		}
	}
}

// 4. Error handling on disconnected transport / EOF / invalid AT responses
func TestEngine_TransportErrorHandling(t *testing.T) {
	errTransport := &errorTransport{err: errors.New("device disconnected (EOF)")}
	engine := NewEngine(errTransport)
	defer engine.Close()

	res, err := engine.Exec("AT")
	if err == nil {
		t.Fatalf("expected error from disconnected transport, got nil")
	}
	if res.Success {
		t.Errorf("expected res.Success=false on transport error")
	}
	if !strings.Contains(err.Error(), "device disconnected") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// 5. Batch execution
func TestEngine_ExecuteBatch(t *testing.T) {
	mock := NewMockTransport()
	mock.SetResponse("AT+GMR", "RM520NGLAAR01A07M4G\r\nOK")
	mock.SetResponse("AT+CPIN?", "+CPIN: READY\r\nOK")

	engine := NewEngine(mock)
	defer engine.Close()

	results, err := engine.ExecuteBatch(context.Background(), []string{"AT+GMR", "AT+CPIN?"})
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 batch results, got %d", len(results))
	}
	if !results[0].Success || !results[1].Success {
		t.Errorf("expected all batch results to succeed: %+v", results)
	}
}

// 6. Engine Close and idempotent shutdown
func TestEngine_CloseLifecycle(t *testing.T) {
	mock := NewMockTransport()
	engine := NewEngine(mock)

	// Calling Close() multiple times must not panic
	if err := engine.Close(); err != nil {
		t.Errorf("first Close error: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Errorf("second Close error: %v", err)
	}

	// Commands after Close must return closed error
	_, err := engine.Exec("AT")
	if err == nil || !strings.Contains(err.Error(), "engine closed") {
		t.Fatalf("expected 'engine closed' error after Close(), got %v", err)
	}
}

// 7. Helper transport implementations
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

type errorTransport struct {
	err error
}

func (e *errorTransport) Send(ctx context.Context, cmd string) (string, error) {
	return "", e.err
}

func (e *errorTransport) Close() error {
	return nil
}

func TestReadResponse_Terminators(t *testing.T) {
	tests := []struct {
		data     string
		expected bool
	}{
		{"\r\nOK\r\n", true},
		{"\r\nERROR\r\n", true},
		{"\r\n+CME ERROR: 10\r\n", true},
		{"\r\n+CMS ERROR: 500\r\n", true},
		{"\r\nBUSY\r\n", true},
		{"\r\nNO CARRIER\r\n", true},
		{"+QENG: ...\r\nOK", true},
		{"INCOMPLETE_DATA", false},
	}

	for _, tt := range tests {
		got := isTerminator([]byte(tt.data))
		if got != tt.expected {
			t.Errorf("isTerminator(%q) = %v; want %v", tt.data, got, tt.expected)
		}
	}
}

func TestReadResponse_BufferStream(t *testing.T) {
	buf := bytes.NewBufferString("\r\n+CPIN: READY\r\n\r\nOK\r\n")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resp, err := readResponse(ctx, buf)
	if err != nil {
		t.Fatalf("readResponse failed: %v", err)
	}
	if !strings.Contains(resp, "READY") || !strings.Contains(resp, "OK") {
		t.Errorf("unexpected readResponse: %s", resp)
	}
}
