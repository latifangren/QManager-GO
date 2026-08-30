package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigManager(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")

	mgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Settings.TempUnit != "celsius" {
		t.Errorf("expected default temp_unit=celsius, got %s", cfg.Settings.TempUnit)
	}

	// Update in-memory and save
	err = mgr.Update(func(c *Config) {
		c.Settings.Hostname = "Modem-RG501Q"
		c.Watchcat.Enabled = 1
	})
	if err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	// Read from disk again
	mgr2, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to reload manager: %v", err)
	}

	cfg2 := mgr2.Get()
	if cfg2.Settings.Hostname != "Modem-RG501Q" {
		t.Errorf("expected Hostname=Modem-RG501Q, got %s", cfg2.Settings.Hostname)
	}
	if cfg2.Watchcat.Enabled != 1 {
		t.Errorf("expected Watchcat.Enabled=1, got %d", cfg2.Watchcat.Enabled)
	}

	_ = os.Remove(confPath)
}

func TestConfigManager_RefuseSymlinkAndDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Refuse directory destination
	dirDest := filepath.Join(tmpDir, "conf_dir")
	_ = os.MkdirAll(dirDest, 0755)

	mgr, _ := NewManager(filepath.Join(tmpDir, "dummy.conf"))
	mgr.path = dirDest
	err := mgr.Save()
	if err == nil {
		t.Errorf("expected Save() to fail when destination is a directory")
	}

	// 2. Refuse symlink destination
	realFile := filepath.Join(tmpDir, "real.conf")
	_ = os.WriteFile(realFile, []byte("{}"), 0644)
	linkPath := filepath.Join(tmpDir, "link.conf")
	if err := os.Symlink(realFile, linkPath); err == nil {
		mgr.path = linkPath
		err = mgr.Save()
		if err == nil {
			t.Errorf("expected Save() to fail when destination is a symlink")
		}
	}
}
