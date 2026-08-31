package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestNewDefaultConfig(t *testing.T) {
	cfg := NewDefaultConfig()

	// Verify Watchcat defaults
	if cfg.Watchcat.Enabled != 0 {
		t.Errorf("expected Watchcat.Enabled=0, got %d", cfg.Watchcat.Enabled)
	}
	if cfg.Watchcat.CheckInterval != 10 {
		t.Errorf("expected Watchcat.CheckInterval=10, got %d", cfg.Watchcat.CheckInterval)
	}
	if cfg.Watchcat.FailThreshold != 5 {
		t.Errorf("expected Watchcat.FailThreshold=5, got %d", cfg.Watchcat.FailThreshold)
	}
	if cfg.Watchcat.ProbeInterval != 5 {
		t.Errorf("expected Watchcat.ProbeInterval=5, got %d", cfg.Watchcat.ProbeInterval)
	}
	if cfg.Watchcat.Cooldown != 60 {
		t.Errorf("expected Watchcat.Cooldown=60, got %d", cfg.Watchcat.Cooldown)
	}
	if cfg.Watchcat.Tier1Enabled != 1 || cfg.Watchcat.Tier2Enabled != 1 || cfg.Watchcat.Tier3Enabled != 0 || cfg.Watchcat.Tier4Enabled != 1 {
		t.Errorf("unexpected watchcat tiers: %+v", cfg.Watchcat)
	}
	if cfg.Watchcat.MaxRebootsPerHour != 3 {
		t.Errorf("expected MaxRebootsPerHour=3, got %d", cfg.Watchcat.MaxRebootsPerHour)
	}

	// Verify Settings defaults
	if cfg.Settings.TempUnit != "celsius" {
		t.Errorf("expected TempUnit=celsius, got %s", cfg.Settings.TempUnit)
	}
	if cfg.Settings.DistanceUnit != "km" {
		t.Errorf("expected DistanceUnit=km, got %s", cfg.Settings.DistanceUnit)
	}
	if cfg.Settings.Timezone != "UTC0" || cfg.Settings.Zonename != "UTC" {
		t.Errorf("unexpected timezone: %s / %s", cfg.Settings.Timezone, cfg.Settings.Zonename)
	}
	if cfg.Settings.SchedRebootTime != "04:00" || cfg.Settings.SchedRebootDays != "0,1,2,3,4,5,6" {
		t.Errorf("unexpected sched reboot: %s on %s", cfg.Settings.SchedRebootTime, cfg.Settings.SchedRebootDays)
	}

	// Verify Update defaults
	if cfg.Update.IncludePrerelease != 1 || cfg.Update.AutoUpdateEnabled != 0 || cfg.Update.AutoUpdateTime != "03:00" {
		t.Errorf("unexpected update config: %+v", cfg.Update)
	}
}

func TestNewManager_AutoCreateDefault(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "nested", "dir", "qmanager.conf")

	// File doesn't exist yet, NewManager should create directory and default file
	mgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create manager on non-existent path: %v", err)
	}

	if _, err := os.Stat(confPath); err != nil {
		t.Fatalf("expected config file to be created on disk, stat error: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Settings.TempUnit != "celsius" {
		t.Errorf("expected default TempUnit celsius, got %s", cfg.Settings.TempUnit)
	}

	// Verify Get returns a copy that does not mutate internal state
	cfgCopy := mgr.Get()
	cfgCopy.Settings.Hostname = "ModifiedLocally"
	if mgr.Get().Settings.Hostname == "ModifiedLocally" {
		t.Errorf("Get() did not return an isolated copy of Config")
	}
}

func TestNewManager_DefaultPath(t *testing.T) {
	// Calling NewManager with empty path defaults to DefaultConfigPath
	mgr, err := NewManager("")
	if err != nil {
		if mgr == nil || mgr.path != DefaultConfigPath {
			t.Errorf("expected manager path to be DefaultConfigPath, got %+v", mgr)
		}
	} else {
		if mgr.path != DefaultConfigPath {
			t.Errorf("expected manager path to be DefaultConfigPath, got %s", mgr.path)
		}
	}
}

func TestConfigManager_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")

	mgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Update in-memory and save
	err = mgr.Update(func(c *Config) {
		c.Settings.Hostname = "Modem-RG501Q"
		c.Watchcat.Enabled = 1
		c.Watchcat.BackupSimSlot = "2"
		c.Update.AutoUpdateEnabled = 1
	})
	if err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	// Explicit Save
	err = mgr.Save()
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Read from disk again with new manager instance
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
	if cfg2.Watchcat.BackupSimSlot != "2" {
		t.Errorf("expected BackupSimSlot=2, got %s", cfg2.Watchcat.BackupSimSlot)
	}
	if cfg2.Update.AutoUpdateEnabled != 1 {
		t.Errorf("expected AutoUpdateEnabled=1, got %d", cfg2.Update.AutoUpdateEnabled)
	}

	// Verify no temporary files remain in directory
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "qmanager.conf" {
			t.Errorf("unexpected lingering file in config dir: %s", entry.Name())
		}
	}
}

func TestConfigManager_RefuseSymlinkAndDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Refuse directory destination
	dirDest := filepath.Join(tmpDir, "conf_dir")
	if err := os.MkdirAll(dirDest, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	mgr, err := NewManager(filepath.Join(tmpDir, "dummy.conf"))
	if err != nil {
		t.Fatalf("failed to create dummy manager: %v", err)
	}
	mgr.path = dirDest
	err = mgr.Save()
	if err == nil {
		t.Errorf("expected Save() to fail when destination is a directory")
	}

	// 2. Refuse symlink destination
	realFile := filepath.Join(tmpDir, "real.conf")
	if err := os.WriteFile(realFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create real file: %v", err)
	}
	linkPath := filepath.Join(tmpDir, "link.conf")
	if err := os.Symlink(realFile, linkPath); err == nil {
		mgr.path = linkPath
		err = mgr.Save()
		if err == nil {
			t.Errorf("expected Save() to fail when destination is a symlink")
		}
	}
}

func TestConfigManager_InvalidParentPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a regular file
	regularFile := filepath.Join(tmpDir, "regular_file")
	if err := os.WriteFile(regularFile, []byte("plain file"), 0644); err != nil {
		t.Fatalf("failed to write regular file: %v", err)
	}

	// Parent path is a regular file (not a directory)
	invalidConfPath := filepath.Join(regularFile, "qmanager.conf")
	_, err := NewManager(invalidConfPath)
	if err == nil {
		t.Fatalf("expected NewManager to fail when parent path is not a directory")
	}

	// Attempt save with manager pointing to a subpath of a file (MkdirAll parent fails)
	mgr, _ := NewManager(filepath.Join(tmpDir, "valid.conf"))
	mgr.path = filepath.Join(regularFile, "sub", "qmanager.conf")
	if err := mgr.Save(); err == nil {
		t.Errorf("expected Save() to fail when MkdirAll parent fails")
	}

	// Point manager path where parent is directly the regular file
	mgr.path = filepath.Join(regularFile, "qmanager.conf")
	if err := mgr.Save(); err == nil {
		t.Errorf("expected Save() to fail when parent is regular file")
	}
}

func TestConfigManager_StatErrorNonNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(filepath.Join(tmpDir, "init.conf"))
	if err != nil {
		t.Fatalf("failed to create init manager: %v", err)
	}

	// Path containing null character or invalid device prefix causes os.Stat to fail with error other than NotExist
	mgr.path = filepath.Join("invalid\x00path", "config.conf")
	err = mgr.Save()
	if err == nil {
		t.Errorf("expected Save() to fail on invalid path")
	}
}

func TestConfigManager_CorruptedAndTruncatedJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Corrupted JSON syntax
	corruptedPath := filepath.Join(tmpDir, "corrupted.conf")
	if err := os.WriteFile(corruptedPath, []byte("{ invalid json : "), 0644); err != nil {
		t.Fatalf("failed to write corrupted config: %v", err)
	}

	_, err := NewManager(corruptedPath)
	if err == nil {
		t.Fatalf("expected error on corrupted JSON, got nil")
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) && !errors.Is(err, syntaxErr) {
		if err.Error() == "" {
			t.Errorf("expected non-empty error message")
		}
	}

	// 2. Truncated JSON
	truncatedPath := filepath.Join(tmpDir, "truncated.conf")
	if err := os.WriteFile(truncatedPath, []byte(`{"watchcat": {"enabled": 1`), 0644); err != nil {
		t.Fatalf("failed to write truncated config: %v", err)
	}

	_, err = NewManager(truncatedPath)
	if err == nil {
		t.Fatalf("expected error on truncated JSON, got nil")
	}
}

func TestConfigManager_AtomicWriteVerification(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "atomic.conf")

	mgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Verify content written to disk matches struct
	err = mgr.Update(func(c *Config) {
		c.Settings.Hostname = "Modem-Atomic"
		c.Watchcat.Cooldown = 120
	})
	if err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	// Read raw bytes from disk
	raw, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read raw config file: %v", err)
	}

	var parsed Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("failed to unmarshal written config: %v", err)
	}

	if parsed.Settings.Hostname != "Modem-Atomic" {
		t.Errorf("expected Hostname=Modem-Atomic, got %s", parsed.Settings.Hostname)
	}
	if parsed.Watchcat.Cooldown != 120 {
		t.Errorf("expected Cooldown=120, got %d", parsed.Watchcat.Cooldown)
	}
}

func TestConfigManager_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager_concurrent.conf")

	mgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrently read config
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg := mgr.Get()
				_ = cfg.Settings.TempUnit
				_ = cfg.Watchcat.Enabled
				runtime.Gosched()
			}
		}()
	}

	// Concurrently update config
	for i := 0; i < goroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = mgr.Update(func(c *Config) {
					c.Settings.Hostname = fmt.Sprintf("host-%d-%d", idx, j)
					c.Watchcat.CheckInterval = 10 + (j % 5)
				})
				runtime.Gosched()
			}
		}()
	}

	// Concurrently save config
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = mgr.Save()
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()

	// Verify final state is valid JSON on disk
	finalMgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to load final manager after concurrent updates: %v", err)
	}

	finalCfg := finalMgr.Get()
	if finalCfg.Settings.TempUnit != "celsius" {
		t.Errorf("expected TempUnit=celsius, got %s", finalCfg.Settings.TempUnit)
	}
	if finalCfg.Watchcat.CheckInterval < 10 || finalCfg.Watchcat.CheckInterval > 15 {
		t.Errorf("unexpected final CheckInterval: %d", finalCfg.Watchcat.CheckInterval)
	}
}

func TestConfigManager_SaveLockedEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Parent path is not a directory
	nonDirFile := filepath.Join(tmpDir, "dummy_file")
	_ = os.WriteFile(nonDirFile, []byte("plain text"), 0644)
	invalidChildPath := filepath.Join(nonDirFile, "qmanager.conf")

	mgrInvalid := &Manager{path: invalidChildPath, cfg: NewDefaultConfig()}
	if err := mgrInvalid.Save(); err == nil {
		t.Errorf("expected error when saving to path with non-directory parent")
	}

	// 2. Target path is a directory (should refuse)
	dirTarget := filepath.Join(tmpDir, "target_is_dir")
	_ = os.MkdirAll(dirTarget, 0755)

	mgrDir := &Manager{path: dirTarget, cfg: NewDefaultConfig()}
	if err := mgrDir.Save(); err == nil {
		t.Errorf("expected error when target path is a directory")
	}

	// 3. Custom config updates
	validPath := filepath.Join(tmpDir, "custom.conf")
	mgrValid, err := NewManager(validPath)
	if err != nil {
		t.Fatalf("failed to init valid manager: %v", err)
	}

	err = mgrValid.Update(func(c *Config) {
		c.Watchcat.Enabled = 1
		c.Watchcat.BackupSimSlot = "2"
		c.Settings.TempUnit = "fahrenheit"
		c.Settings.DistanceUnit = "miles"
		c.Settings.Hostname = "EdgeModem"
		c.Settings.Timezone = "EST+5EDT"
		c.Settings.Zonename = "America/New_York"
		c.Settings.SchedRebootEnabled = 1
		c.Settings.SchedRebootTime = "02:15"
		c.Settings.SchedRebootDays = "1,3,5"
		c.Update.AutoUpdateEnabled = 1
		c.Update.AutoUpdateTime = "04:30"
		c.Update.IncludePrerelease = 0
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Reload and verify all custom fields persisted cleanly
	mgrReloaded, err := NewManager(validPath)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	cfg := mgrReloaded.Get()
	if cfg.Settings.TempUnit != "fahrenheit" || cfg.Settings.DistanceUnit != "miles" || cfg.Settings.Hostname != "EdgeModem" {
		t.Errorf("unexpected settings after reload: %+v", cfg.Settings)
	}
	if cfg.Watchcat.BackupSimSlot != "2" || cfg.Watchcat.Enabled != 1 {
		t.Errorf("unexpected watchcat after reload: %+v", cfg.Watchcat)
	}
	if cfg.Update.AutoUpdateEnabled != 1 || cfg.Update.AutoUpdateTime != "04:30" || cfg.Update.IncludePrerelease != 0 {
		t.Errorf("unexpected update after reload: %+v", cfg.Update)
	}
}
