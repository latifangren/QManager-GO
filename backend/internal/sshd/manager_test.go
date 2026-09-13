package sshd

import (
	"path/filepath"
	"testing"

	"qmanager/internal/config"
)

func TestManagerLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")
	keyDir := filepath.Join(tmpDir, "ssh_keys")

	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	mgr := NewManager(cfgMgr, keyDir)

	// 1. Initial Status
	status := mgr.GetStatus()
	if !status.Enabled {
		t.Errorf("expected default Enabled=true, got %v", status.Enabled)
	}
	if status.Port != 22 {
		t.Errorf("expected default Port=22, got %d", status.Port)
	}

	// 2. Start (use high unprivileged port to avoid permission denied in test)
	err = mgr.ApplySettings(true, 22222, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@host")
	if err != nil {
		t.Fatalf("ApplySettings failed: %v", err)
	}

	status = mgr.GetStatus()
	if !status.Running {
		t.Errorf("expected SSH server Running=true")
	}
	if status.Port != 22222 {
		t.Errorf("expected Port=22222, got %d", status.Port)
	}
	if status.AuthorizedKeys == "" {
		t.Errorf("expected AuthorizedKeys to be persisted")
	}

	// 3. Stop
	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	status = mgr.GetStatus()
	if status.Running {
		t.Errorf("expected SSH server Running=false after stop")
	}

	// 4. Disable setting
	if err := mgr.ApplySettings(false, 2222, ""); err != nil {
		t.Fatalf("ApplySettings disabled failed: %v", err)
	}
	status = mgr.GetStatus()
	if status.Enabled {
		t.Errorf("expected Enabled=false")
	}
	if status.Port != 2222 {
		t.Errorf("expected Port=2222, got %d", status.Port)
	}
	if status.Running {
		t.Errorf("expected Running=false when disabled")
	}
}
