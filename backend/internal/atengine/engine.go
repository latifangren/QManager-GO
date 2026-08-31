package atengine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Priority levels for AT command execution.
type Priority int

const (
	PriorityHigh   Priority = 0 // Watchdog recovery, emergency radio restart (CFUN)
	PriorityNormal Priority = 1 // User UI requests, AT terminal, manual mutations
	PriorityLow    Priority = 2 // 1 Hz background telemetry polling
)

// Result encapsulates structured response information for an executed AT command.
type Result struct {
	Command  string        `json:"command"`
	Raw      string        `json:"raw"`
	Duration time.Duration `json:"duration_ms"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
}

type commandRequest struct {
	ctx      context.Context
	cmd      string
	priority Priority
	respChan chan commandResponse
}

type commandResponse struct {
	raw string
	err error
}

// Engine manages thread-safe, prioritized AT command dispatch and parsing over direct transport.
type Engine struct {
	transport Transport
	timeout   time.Duration

	highChan chan commandRequest
	normChan chan commandRequest
	lowChan  chan commandRequest
	stopChan chan struct{}
	wg       sync.WaitGroup

	mu sync.Mutex
}

// NewEngine creates an Engine and starts the 3-tier priority actor worker.
func NewEngine(transport Transport) *Engine {
	e := &Engine{
		transport: transport,
		timeout:   5 * time.Second,
		highChan:  make(chan commandRequest, 64),
		normChan:  make(chan commandRequest, 128),
		lowChan:   make(chan commandRequest, 64),
		stopChan:  make(chan struct{}),
	}

	e.wg.Add(1)
	go e.workerLoop()

	return e
}

// SetTimeout configures global default timeout per AT command.
func (e *Engine) SetTimeout(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.timeout = d
}

// GetTimeout returns the current global timeout.
func (e *Engine) GetTimeout() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.timeout
}

// Close cleanly stops the priority worker goroutine.
func (e *Engine) Close() error {
	e.mu.Lock()
	select {
	case <-e.stopChan:
		// already closed
		e.mu.Unlock()
		return nil
	default:
		close(e.stopChan)
	}
	e.mu.Unlock()

	e.wg.Wait()
	if e.transport != nil {
		return e.transport.Close()
	}
	return nil
}

// workerLoop is the single dedicated executor that services highChan first, then normChan, then lowChan.
func (e *Engine) workerLoop() {
	defer e.wg.Done()

	for {
		// 1. Always prioritize High
		select {
		case <-e.stopChan:
			return
		case req := <-e.highChan:
			e.processRequest(req)
			continue
		default:
		}

		// 2. High or Normal
		select {
		case <-e.stopChan:
			return
		case req := <-e.highChan:
			e.processRequest(req)
			continue
		case req := <-e.normChan:
			e.processRequest(req)
			continue
		default:
		}

		// 3. High, Normal, or Low (blocking wait)
		select {
		case <-e.stopChan:
			return
		case req := <-e.highChan:
			e.processRequest(req)
		case req := <-e.normChan:
			e.processRequest(req)
		case req := <-e.lowChan:
			e.processRequest(req)
		}
	}
}

func (e *Engine) processRequest(req commandRequest) {
	// Check if context expired before execution
	if err := req.ctx.Err(); err != nil {
		select {
		case req.respChan <- commandResponse{err: err}:
		default:
		}
		return
	}

	raw, err := e.transport.Send(req.ctx, req.cmd)
	select {
	case req.respChan <- commandResponse{raw: raw, err: err}:
	default:
	}
}

// ExecuteWithPriority executes an AT command with explicit priority.
func (e *Engine) ExecuteWithPriority(ctx context.Context, cmd string, prio Priority) (string, error) {
	res, err := e.ExecContextWithPriority(ctx, cmd, prio)
	if err != nil {
		return "", err
	}
	return res.Raw, nil
}

// Execute runs an AT command with Normal priority.
func (e *Engine) Execute(ctx context.Context, cmd string) (string, error) {
	return e.ExecuteWithPriority(ctx, cmd, PriorityNormal)
}

// ExecuteHigh runs an AT command with High priority.
func (e *Engine) ExecuteHigh(ctx context.Context, cmd string) (string, error) {
	return e.ExecuteWithPriority(ctx, cmd, PriorityHigh)
}

// ExecuteLow runs an AT command with Low priority.
func (e *Engine) ExecuteLow(ctx context.Context, cmd string) (string, error) {
	return e.ExecuteWithPriority(ctx, cmd, PriorityLow)
}

// ExecContextWithPriority executes an AT command with explicit priority and returns a structured *Result.
func (e *Engine) ExecContextWithPriority(ctx context.Context, cmd string, prio Priority) (*Result, error) {
	start := time.Now()
	cleanCmd := strings.TrimSpace(cmd)

	defaultTimeout := e.GetTimeout()
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && defaultTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}

	req := commandRequest{
		ctx:      ctx,
		cmd:      cleanCmd,
		priority: prio,
		respChan: make(chan commandResponse, 1),
	}

	// Submit to appropriate priority queue
	switch prio {
	case PriorityHigh:
		select {
		case <-e.stopChan:
			return nil, fmt.Errorf("engine closed")
		case <-ctx.Done():
			return nil, ctx.Err()
		case e.highChan <- req:
		}
	case PriorityNormal:
		select {
		case <-e.stopChan:
			return nil, fmt.Errorf("engine closed")
		case <-ctx.Done():
			return nil, ctx.Err()
		case e.normChan <- req:
		}
	case PriorityLow:
		select {
		case <-e.stopChan:
			return nil, fmt.Errorf("engine closed")
		case <-ctx.Done():
			return nil, ctx.Err()
		case e.lowChan <- req:
		default:
			// Low priority queue is saturated / full; drop immediately without blocking caller
			return nil, ErrQueueFull
		}
	}

	// Wait for response or cancellation
	var resp commandResponse
	select {
	case <-e.stopChan:
		return nil, fmt.Errorf("engine closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp = <-req.respChan:
	}

	elapsed := time.Since(start)
	raw := resp.raw
	err := resp.err

	res := &Result{
		Command:  cleanCmd,
		Raw:      raw,
		Duration: elapsed,
		Success:  err == nil && !isATError(raw),
	}

	if err != nil {
		res.Error = err.Error()
		return res, err
	}

	if !res.Success {
		res.Error = "AT command returned ERROR or invalid response"
		return res, fmt.Errorf("%w: %s", ErrATCommand, raw)
	}

	return res, nil
}

// ExecContext runs an AT command respecting context with Normal priority.
func (e *Engine) ExecContext(ctx context.Context, cmd string) (*Result, error) {
	return e.ExecContextWithPriority(ctx, cmd, PriorityNormal)
}

// Exec runs a single AT command with Normal priority.
func (e *Engine) Exec(cmd string) (*Result, error) {
	return e.ExecContext(context.Background(), cmd)
}

// ExecHigh runs an AT command with High priority.
func (e *Engine) ExecHigh(ctx context.Context, cmd string) (*Result, error) {
	return e.ExecContextWithPriority(ctx, cmd, PriorityHigh)
}

// ExecLow runs an AT command with Low priority.
func (e *Engine) ExecLow(ctx context.Context, cmd string) (*Result, error) {
	return e.ExecContextWithPriority(ctx, cmd, PriorityLow)
}

// ExecuteBatch runs multiple commands sequentially under Normal priority.
func (e *Engine) ExecuteBatch(ctx context.Context, commands []string) ([]*Result, error) {
	results := make([]*Result, 0, len(commands))
	for _, cmd := range commands {
		res, err := e.ExecContext(ctx, cmd)
		if res != nil {
			results = append(results, res)
		}
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

func isATError(resp string) bool {
	t := strings.TrimSpace(resp)
	return strings.HasSuffix(t, "ERROR") ||
		strings.Contains(t, "+CME ERROR:") ||
		strings.Contains(t, "+CMS ERROR:") ||
		strings.Contains(t, "NO CARRIER") ||
		strings.Contains(t, "BUSY")
}
