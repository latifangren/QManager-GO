package atengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	ErrTimeout   = errors.New("at command timed out")
	ErrBusy      = errors.New("modem busy")
	ErrATCommand = errors.New("at command returned error")
	ErrNoDevice  = errors.New("at device not found")
	ErrQueueFull = errors.New("at command queue full")
)

// Transport is the low-level interface for sending raw AT commands.
type Transport interface {
	Send(ctx context.Context, cmd string) (string, error)
	Close() error
}

// MockTransport allows in-memory simulation and testing of AT commands.
type MockTransport struct {
	mu        sync.Mutex
	Responses map[string]string
	History   []string
	Default   string
}

// NewMockTransport returns a test transport preloaded with responses.
func NewMockTransport() *MockTransport {
	return &MockTransport{
		Responses: make(map[string]string),
		History:   make([]string, 0),
		Default:   "OK",
	}
}

func (m *MockTransport) Send(ctx context.Context, cmd string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	trimmed := strings.TrimSpace(cmd)
	m.History = append(m.History, trimmed)
	if resp, ok := m.Responses[trimmed]; ok {
		return resp, nil
	}
	return m.Default, nil
}

func (m *MockTransport) SetResponse(cmd, resp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[strings.TrimSpace(cmd)] = resp
}

// GetHistory returns a thread-safe copy of all AT commands sent.
func (m *MockTransport) GetHistory() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst := make([]string, len(m.History))
	copy(dst, m.History)
	return dst
}

// ClearHistory resets the recorded command history.
func (m *MockTransport) ClearHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.History = make([]string, 0)
}

func (m *MockTransport) Close() error {
	return nil
}

// DeviceTransport communicates directly with modem character device nodes (e.g. /dev/smd11).
type DeviceTransport struct {
	devPath  string
	lockPath string
	mu       sync.Mutex
}

// NewDeviceTransport opens direct connection to modem character device.
func NewDeviceTransport(devPath string) *DeviceTransport {
	if devPath == "" {
		devPath = "/dev/smd11"
	}
	lockPath := "/var/lock/qmanager.lock"
	if _, err := os.Stat("/var/lock"); err != nil {
		lockPath = "/tmp/qmanager_at.lock"
	}
	return &DeviceTransport{
		devPath:  devPath,
		lockPath: lockPath,
	}
}

// isTerminator checks if the response stream contains standard 3GPP AT terminators line-by-line.
func isTerminator(buf []byte) bool {
	lines := strings.Split(string(buf), "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if trimmed == "OK" ||
			trimmed == "ERROR" ||
			strings.HasPrefix(trimmed, "+CME ERROR:") ||
			strings.HasPrefix(trimmed, "+CMS ERROR:") ||
			trimmed == "NO CARRIER" ||
			trimmed == "BUSY" ||
			trimmed == "CONNECT" ||
			strings.HasPrefix(trimmed, "CONNECT ") ||
			trimmed == "RING" {
			return true
		}
	}
	return false
}

// evaluateResponseTerminator scans lines in the response buffer to detect final AT result codes.
// Returns whether response is terminated and any corresponding error.
func evaluateResponseTerminator(resp string) (bool, error) {
	lines := strings.Split(resp, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if trimmed == "OK" || trimmed == "CONNECT" || strings.HasPrefix(trimmed, "CONNECT ") || trimmed == "RING" {
			return true, nil
		}
		if trimmed == "ERROR" || strings.HasPrefix(trimmed, "+CME ERROR:") || strings.HasPrefix(trimmed, "+CMS ERROR:") || trimmed == "NO CARRIER" {
			return true, ErrATCommand
		}
		if trimmed == "BUSY" {
			return true, ErrBusy
		}
	}
	return false, nil
}

// readResponse reads from an io.Reader dynamically until an AT terminator, EOF, or context timeout.
func readResponse(ctx context.Context, r io.Reader) (string, error) {
	type readResult struct {
		data []byte
		err  error
	}

	readChan := make(chan readResult, 1)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				readChan <- readResult{data: chunk, err: err}
			} else if err != nil {
				readChan <- readResult{data: nil, err: err}
				return
			}
		}
	}()

	var out bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			return out.String(), ErrTimeout
		case res := <-readChan:
			if len(res.data) > 0 {
				out.Write(res.data)
				if terminated, termErr := evaluateResponseTerminator(out.String()); terminated {
					return out.String(), termErr
				}
			}
			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					resp := out.String()
					if terminated, termErr := evaluateResponseTerminator(resp); terminated {
						return resp, termErr
					}
					return resp, nil
				}
				return out.String(), res.err
			}
		}
	}
}

// Send sends an AT command directly to the character device and reads the response until a terminator or timeout.
func (d *DeviceTransport) Send(ctx context.Context, cmd string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	lockFile, err := acquireFileLock(d.lockPath)
	if err != nil {
		return "", fmt.Errorf("failed to acquire lock %s: %w", d.lockPath, err)
	}
	defer releaseFileLock(lockFile)

	var f *os.File
	var openErr error
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return "", ErrTimeout
		default:
		}
		f, openErr = os.OpenFile(d.devPath, os.O_RDWR, 0)
		if openErr == nil {
			break
		}
		if isEBUSY(openErr) && attempt < 2 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		break
	}
	if openErr != nil {
		return "", fmt.Errorf("%w: %s (%v)", ErrNoDevice, d.devPath, openErr)
	}
	defer f.Close()

	cleanCmd := strings.TrimSpace(cmd)
	if !strings.HasSuffix(cleanCmd, "\r") && !strings.HasSuffix(cleanCmd, "\n") {
		cleanCmd += "\r\n"
	}

	var writeErr error
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return "", ErrTimeout
		default:
		}
		_, writeErr = f.Write([]byte(cleanCmd))
		if writeErr == nil {
			break
		}
		if isEBUSY(writeErr) && attempt < 2 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		break
	}
	if writeErr != nil {
		return "", fmt.Errorf("failed to write to %s: %w", d.devPath, writeErr)
	}

	return readDeviceResponse(ctx, f)
}

func (d *DeviceTransport) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return nil
}

// CliTransport invokes the modem CLI utility (/usr/bin/atcli_smd11, /usr/bin/qcmd, etc.).
type CliTransport struct {
	cliPath string
}

func NewCliTransport(path string) *CliTransport {
	if path == "" {
		path = "/usr/bin/atcli_smd11"
	}
	return &CliTransport{cliPath: path}
}

func (c *CliTransport) Send(ctx context.Context, cmd string) (string, error) {
	if _, err := exec.LookPath(c.cliPath); err != nil {
		if _, statErr := os.Stat(c.cliPath); statErr != nil {
			return "", fmt.Errorf("%w: %s", ErrNoDevice, c.cliPath)
		}
	}

	cmdExec := exec.CommandContext(ctx, c.cliPath, cmd)
	out, err := cmdExec.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrTimeout
		}
		return string(out), fmt.Errorf("cli error: %w: %s", err, string(out))
	}

	return string(out), nil
}

func (c *CliTransport) Close() error {
	return nil
}

// AutoDetectTransport automatically detects the optimal transport for the modem platform.
// If customDevice is provided (or non-empty), it attempts to open that specific device first.
func AutoDetectTransport(customDevice ...string) Transport {
	if len(customDevice) > 0 && customDevice[0] != "" {
		dev := customDevice[0]
		if _, err := os.Stat(dev); err == nil {
			return NewDeviceTransport(dev)
		}
		if p, err := exec.LookPath(dev); err == nil {
			return NewCliTransport(p)
		}
		// If custom device is a COM port or explicit path, construct device transport directly
		return NewDeviceTransport(dev)
	}

	// 1. Check if qcmd executable exists
	qcmdCandidates := []string{
		"/usr/bin/qcmd",
		"/opt/bin/qcmd",
	}
	for _, p := range qcmdCandidates {
		if _, err := os.Stat(p); err == nil {
			return NewCliTransport(p)
		}
	}
	if p, err := exec.LookPath("qcmd"); err == nil {
		return NewCliTransport(p)
	}

	// 2. Check if atcli_smd11 exists
	atcliCandidates := []string{
		"/usr/bin/atcli_smd11",
		"/usr/local/bin/atcli_smd11",
	}
	for _, p := range atcliCandidates {
		if _, err := os.Stat(p); err == nil {
			return NewCliTransport(p)
		}
	}
	if p, err := exec.LookPath("atcli_smd11"); err == nil {
		return NewCliTransport(p)
	}

	// 3. Direct Character Devices (Qualcomm SMD / TTY)
	deviceCandidates := []string{
		"/dev/smd11",
		"/dev/smd7",
		"/dev/ttyUSB2",
	}

	for _, dev := range deviceCandidates {
		if _, err := os.Stat(dev); err == nil {
			return NewDeviceTransport(dev)
		}
	}

	// 4. Fallback to mock transport for testing & local development
	return NewMockTransport()
}
