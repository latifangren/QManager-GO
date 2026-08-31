package atengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMockTransport_History(t *testing.T) {
	mock := NewMockTransport()
	mock.SetResponse("AT+GMR", "RM520NGLAAR01A07M4G\r\nOK")

	ctx := context.Background()
	_, _ = mock.Send(ctx, "AT")
	_, _ = mock.Send(ctx, "AT+GMR")

	history := mock.GetHistory()
	if len(history) != 2 || history[0] != "AT" || history[1] != "AT+GMR" {
		t.Fatalf("unexpected history: %v", history)
	}

	mock.ClearHistory()
	if len(mock.GetHistory()) != 0 {
		t.Fatalf("expected empty history after ClearHistory, got %v", mock.GetHistory())
	}
}

func TestNewDeviceTransport(t *testing.T) {
	dev := NewDeviceTransport("")
	if dev.devPath != "/dev/smd11" {
		t.Errorf("expected default devPath /dev/smd11, got %s", dev.devPath)
	}
	_ = dev.Close()

	customDev := NewDeviceTransport("/dev/smd11")
	if customDev.devPath != "/dev/smd11" {
		t.Errorf("expected devPath /dev/smd11, got %s", customDev.devPath)
	}

	// Send to non-existent device returns error
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := customDev.Send(ctx, "AT")
	if err == nil {
		t.Errorf("expected error opening non-existent device /dev/smd11 on non-modem host")
	}
}

func TestNewCliTransport(t *testing.T) {
	cli := NewCliTransport("")
	if cli.cliPath != "/usr/bin/atcli_smd11" {
		t.Errorf("expected default cliPath /usr/bin/atcli_smd11, got %s", cli.cliPath)
	}
	_ = cli.Close()

	customCli := NewCliTransport("/tmp/nonexistent_cli")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := customCli.Send(ctx, "AT")
	if err == nil {
		t.Errorf("expected error invoking non-existent cli path")
	}
}

func TestAutoDetectTransport(t *testing.T) {
	tmpDir := t.TempDir()
	fakeDev := filepath.Join(tmpDir, "smd11")
	_ = os.WriteFile(fakeDev, []byte(""), 0644)

	// Fallback to MockTransport when no candidate devices exist
	transport := AutoDetectTransport()
	if transport == nil {
		t.Fatalf("expected non-nil transport")
	}
	_ = transport.Close()
}
