package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetIPAStatus_SafeFallback(t *testing.T) {
	status := GetIPAStatus()
	// Should return valid object without panic
	if status.Offload == "" || status.Driver == "" || status.Daemon == "" {
		t.Errorf("expected non-empty fields in IPAStatus, got: %+v", status)
	}
}

func TestCheckIPAStatus_SupportedAndPerfDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	devIPA := filepath.Join(tmpDir, "ipa")
	if err := os.WriteFile(devIPA, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create fake device: %v", err)
	}

	fakeChecker := func(proc string) bool {
		return proc == "ipacm_perf"
	}

	status := checkIPAStatus([]string{devIPA}, fakeChecker)
	if !status.Supported {
		t.Errorf("expected Supported=true")
	}
	if status.Driver != "qualcomm_ipa" {
		t.Errorf("expected Driver='qualcomm_ipa', got '%s'", status.Driver)
	}
	if status.Daemon != "ipacm_perf" {
		t.Errorf("expected Daemon='ipacm_perf', got '%s'", status.Daemon)
	}
	if !status.Active {
		t.Errorf("expected Active=true")
	}
	if status.Offload != "hardware" {
		t.Errorf("expected Offload='hardware', got '%s'", status.Offload)
	}
}

func TestCheckIPAStatus_SupportedAndStandardDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	devWwan := filepath.Join(tmpDir, "wwan_ioctl")
	if err := os.WriteFile(devWwan, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create fake device: %v", err)
	}

	fakeChecker := func(proc string) bool {
		return proc == "ipacm"
	}

	status := checkIPAStatus([]string{devWwan}, fakeChecker)
	if !status.Supported {
		t.Errorf("expected Supported=true")
	}
	if status.Driver != "qualcomm_ipa" {
		t.Errorf("expected Driver='qualcomm_ipa', got '%s'", status.Driver)
	}
	if status.Daemon != "ipacm" {
		t.Errorf("expected Daemon='ipacm', got '%s'", status.Daemon)
	}
	if !status.Active {
		t.Errorf("expected Active=true")
	}
	if status.Offload != "hardware" {
		t.Errorf("expected Offload='hardware', got '%s'", status.Offload)
	}
}

func TestCheckIPAStatus_NotSupportedNoDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	devNonExistent := filepath.Join(tmpDir, "nonexistent")

	fakeChecker := func(proc string) bool {
		return false
	}

	status := checkIPAStatus([]string{devNonExistent}, fakeChecker)
	if status.Supported {
		t.Errorf("expected Supported=false")
	}
	if status.Driver != "none" {
		t.Errorf("expected Driver='none', got '%s'", status.Driver)
	}
	if status.Daemon != "none" {
		t.Errorf("expected Daemon='none', got '%s'", status.Daemon)
	}
	if status.Active {
		t.Errorf("expected Active=false")
	}
	if status.Offload != "software" {
		t.Errorf("expected Offload='software', got '%s'", status.Offload)
	}
}

func TestCheckIPAStatus_DeviceExistsDaemonInactive(t *testing.T) {
	tmpDir := t.TempDir()
	devIPA := filepath.Join(tmpDir, "ipa")
	if err := os.WriteFile(devIPA, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create fake device: %v", err)
	}

	fakeChecker := func(proc string) bool {
		return false
	}

	status := checkIPAStatus([]string{devIPA}, fakeChecker)
	if !status.Supported {
		t.Errorf("expected Supported=true")
	}
	if status.Driver != "qualcomm_ipa" {
		t.Errorf("expected Driver='qualcomm_ipa', got '%s'", status.Driver)
	}
	if status.Daemon != "none" {
		t.Errorf("expected Daemon='none', got '%s'", status.Daemon)
	}
	if status.Active {
		t.Errorf("expected Active=false")
	}
	if status.Offload != "software" {
		t.Errorf("expected Offload='software', got '%s'", status.Offload)
	}
}
