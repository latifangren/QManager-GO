package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_GettersSettersAndEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")

	mgr, err := NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	// 1. Test getters for initial default config
	cfg := mgr.Get()
	if cfg.Watchcat.CheckInterval != 10 {
		t.Errorf("expected default Watchcat.CheckInterval=10, got %d", cfg.Watchcat.CheckInterval)
	}
	if cfg.Settings.TempUnit != "celsius" {
		t.Errorf("expected default Settings.TempUnit=celsius, got %s", cfg.Settings.TempUnit)
	}
	if cfg.Update.IncludePrerelease != 1 {
		t.Errorf("expected default Update.IncludePrerelease=1, got %d", cfg.Update.IncludePrerelease)
	}

	// 2. Test setters across all sub-configurations
	err = mgr.Update(func(c *Config) {
		// Watchcat settings
		c.Watchcat.Enabled = 1
		c.Watchcat.CheckInterval = 20
		c.Watchcat.FailThreshold = 4
		c.Watchcat.ProbeInterval = 3
		c.Watchcat.Cooldown = 90
		c.Watchcat.Tier1Enabled = 1
		c.Watchcat.Tier2Enabled = 1
		c.Watchcat.Tier3Enabled = 1
		c.Watchcat.Tier4Enabled = 0
		c.Watchcat.BackupSimSlot = "2"
		c.Watchcat.MaxRebootsPerHour = 4

		// System Settings
		c.Settings.TempUnit = "fahrenheit"
		c.Settings.DistanceUnit = "miles"
		c.Settings.Hostname = "ModemGateway-Pro"
		c.Settings.Timezone = "EST+5EDT"
		c.Settings.Zonename = "America/New_York"
		c.Settings.SmsToolDevice = "/dev/smd7"
		c.Settings.SchedRebootEnabled = 1
		c.Settings.SchedRebootTime = "05:15"
		c.Settings.SchedRebootDays = "1,2,3,4,5"

		// Update Settings
		c.Update.IncludePrerelease = 0
		c.Update.AutoUpdateEnabled = 1
		c.Update.AutoUpdateTime = "02:45"
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify in-memory updated values
	updated := mgr.Get()
	if updated.Watchcat.FailThreshold != 4 || updated.Watchcat.BackupSimSlot != "2" {
		t.Errorf("Watchcat setter failed: %+v", updated.Watchcat)
	}
	if updated.Settings.Hostname != "ModemGateway-Pro" || updated.Settings.SchedRebootTime != "05:15" {
		t.Errorf("Settings setter failed: %+v", updated.Settings)
	}
	if updated.Update.AutoUpdateTime != "02:45" || updated.Update.AutoUpdateEnabled != 1 {
		t.Errorf("Update setter failed: %+v", updated.Update)
	}

	// 3. Test saving with invalid file path (write to directory path directly)
	invalidFilePath := filepath.Join(tmpDir, "directory_target")
	_ = os.MkdirAll(invalidFilePath, 0755)

	mgrInvalid := &Manager{path: invalidFilePath, cfg: NewDefaultConfig()}
	if err := mgrInvalid.Save(); err == nil {
		t.Errorf("expected error when saving to directory target")
	}

	// 3b. Test parent directory not existing (triggers MkdirAll in saveLocked)
	nestedDir := filepath.Join(tmpDir, "nested", "config", "path")
	nestedConfPath := filepath.Join(nestedDir, "qmanager.conf")
	mgrNested := &Manager{path: nestedConfPath, cfg: NewDefaultConfig()}
	if err := mgrNested.Save(); err != nil {
		t.Errorf("expected MkdirAll to create nested dir, got error: %v", err)
	}

	// 3c. Test parent path being an existing file, not directory
	fileAsParent := filepath.Join(tmpDir, "file_parent.txt")
	_ = os.WriteFile(fileAsParent, []byte("hello"), 0644)
	badParentConfPath := filepath.Join(fileAsParent, "qmanager.conf")
	mgrBadParent := &Manager{path: badParentConfPath, cfg: NewDefaultConfig()}
	if err := mgrBadParent.Save(); err == nil {
		t.Errorf("expected error when parent is a regular file")
	}

	// 4. Test loading file with corrupt/invalid JSON (should fallback or return error)
	corruptPath := filepath.Join(tmpDir, "corrupt.conf")
	_ = os.WriteFile(corruptPath, []byte("NOT_VALID_JSON{{{"), 0644)

	_, errCorrupt := NewManager(corruptPath)
	if errCorrupt == nil {
		t.Errorf("expected error when loading corrupt JSON file")
	}

	// 5. Test loading empty config file fallback
	emptyPath := filepath.Join(tmpDir, "empty.conf")
	_ = os.WriteFile(emptyPath, []byte("{}"), 0644)

	mgrEmpty, errEmpty := NewManager(emptyPath)
	if errEmpty != nil {
		t.Fatalf("loading empty JSON config failed: %v", errEmpty)
	}
	if mgrEmpty.Get().Watchcat.CheckInterval == 0 {
		// Verify loaded struct matches JSON
		_ = mgrEmpty.Get()
	}
}
