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
	Default   string
}

// NewMockTransport returns a test transport preloaded with responses.
func NewMockTransport() *MockTransport {
	return &MockTransport{
		Responses: make(map[string]string),
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

func (m *MockTransport) Close() error {
	return nil
}

// DeviceTransport performs direct character device read/write (e.g. /dev/smd11, /dev/smd7, /dev/ttyUSB2).
// Modeled after the direct character device logic in atcli_rust.
type DeviceTransport struct {
	devPath string
	mu      sync.Mutex
}

// NewDeviceTransport opens direct connection to modem character device.
func NewDeviceTransport(devPath string) *DeviceTransport {
	if devPath == "" {
		devPath = "/dev/smd11"
	}
	return &DeviceTransport{
		devPath: devPath,
	}
}

// isTerminator checks if the response stream contains standard 3GPP AT terminators.
func isTerminator(buf []byte) bool {
	str := string(buf)
	if strings.Contains(str, "\r\nOK\r\n") ||
		strings.Contains(str, "\nOK\n") ||
		strings.HasSuffix(strings.TrimSpace(str), "OK") ||
		strings.Contains(str, "\r\nERROR\r\n") ||
		strings.Contains(str, "\nERROR\n") ||
		strings.HasSuffix(strings.TrimSpace(str), "ERROR") ||
		strings.Contains(str, "+CME ERROR:") ||
		strings.Contains(str, "+CMS ERROR:") ||
		strings.Contains(str, "\r\nNO CARRIER\r\n") ||
		strings.Contains(str, "\r\nBUSY\r\n") {
		return true
	}
	return false
}

// readResponse reads from an io.Reader until an AT terminator or context timeout.
func readResponse(ctx context.Context, r io.Reader) (string, error) {
	var out bytes.Buffer
	readBuf := make([]byte, 1024)

	for {
		select {
		case <-ctx.Done():
			return out.String(), ErrTimeout
		default:
		}

		if f, ok := r.(*os.File); ok {
			_ = f.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		}

		n, rErr := r.Read(readBuf)
		if n > 0 {
			out.Write(readBuf[:n])
			if isTerminator(out.Bytes()) {
				break
			}
		}

		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			if os.IsTimeout(rErr) {
				if ctx.Err() != nil {
					return out.String(), ErrTimeout
				}
				if isTerminator(out.Bytes()) {
					break
				}
			}
		}
	}

	res := out.String()
	if strings.Contains(res, "\r\nERROR\r\n") || strings.Contains(res, "\nERROR\n") || strings.Contains(res, "+CME ERROR:") || strings.Contains(res, "+CMS ERROR:") {
		return res, ErrATCommand
	}
	if strings.Contains(res, "\r\nBUSY\r\n") {
		return res, ErrBusy
	}

	return res, nil
}

// Send sends an AT command directly to the character device and reads the response until a terminator or timeout.
func (d *DeviceTransport) Send(ctx context.Context, cmd string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	f, err := os.OpenFile(d.devPath, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("%w: %s (%v)", ErrNoDevice, d.devPath, err)
	}
	defer f.Close()

	cleanCmd := strings.TrimSpace(cmd)
	if !strings.HasSuffix(cleanCmd, "\r") && !strings.HasSuffix(cleanCmd, "\n") {
		cleanCmd += "\r\n"
	}

	if _, err := f.Write([]byte(cleanCmd)); err != nil {
		return "", fmt.Errorf("failed to write to %s: %w", d.devPath, err)
	}

	return readResponse(ctx, f)
}

func (d *DeviceTransport) Close() error {
	return nil
}

// CliTransport invokes the modem CLI utility (/usr/bin/atcli_smd11).
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
	if _, err := os.Stat(c.cliPath); err != nil {
		return "", fmt.Errorf("%w: %s", ErrNoDevice, c.cliPath)
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
func AutoDetectTransport() Transport {
	// 1. Direct Character Devices (Qualcomm SMD / TTY)
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

	// 2. Legacy helper binary if present
	if _, err := os.Stat("/usr/bin/atcli_smd11"); err == nil {
		return NewCliTransport("/usr/bin/atcli_smd11")
	}

	// 3. Fallback to mock transport for testing & local development
	return NewMockTransport()
}
