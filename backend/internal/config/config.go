package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	DefaultConfigPath = "/etc/qmanager/qmanager.conf"
)

// WatchcatConfig holds connection watchdog parameters.
type WatchcatConfig struct {
	Enabled           int    `json:"enabled"`
	CheckInterval     int    `json:"check_interval"`
	FailThreshold     int    `json:"fail_threshold"`
	ProbeInterval     int    `json:"probe_interval"`
	Cooldown          int    `json:"cooldown"`
	Tier1Enabled      int    `json:"tier1_enabled"`
	Tier2Enabled      int    `json:"tier2_enabled"`
	Tier3Enabled      int    `json:"tier3_enabled"`
	Tier4Enabled      int    `json:"tier4_enabled"`
	BackupSimSlot     string `json:"backup_sim_slot"`
	MaxRebootsPerHour int    `json:"max_reboots_per_hour"`
}

// SystemSettings holds unit preferences and scheduling.
type SystemSettings struct {
	TempUnit           string `json:"temp_unit"`
	DistanceUnit       string `json:"distance_unit"`
	Hostname           string `json:"hostname"`
	Timezone           string `json:"timezone"`
	Zonename           string `json:"zonename"`
	SmsToolDevice      string `json:"sms_tool_device"`
	SchedRebootEnabled int    `json:"sched_reboot_enabled"`
	SchedRebootTime    string `json:"sched_reboot_time"`
	SchedRebootDays    string `json:"sched_reboot_days"`
}

// UpdateConfig holds OTA settings.
type UpdateConfig struct {
	IncludePrerelease int    `json:"include_prerelease"`
	AutoUpdateEnabled int    `json:"auto_update_enabled"`
	AutoUpdateTime    string `json:"auto_update_time"`
}

// Config represents the complete /etc/qmanager/qmanager.conf schema.
type Config struct {
	Watchcat WatchcatConfig `json:"watchcat"`
	Settings SystemSettings `json:"settings"`
	Update   UpdateConfig   `json:"update"`
}

// Manager coordinates thread-safe config read, write, and persistence.
type Manager struct {
	path string
	mu   sync.RWMutex
	cfg  Config
}

// NewDefaultConfig returns default values matching scripts/usr/lib/qmanager/config.sh.
func NewDefaultConfig() Config {
	return Config{
		Watchcat: WatchcatConfig{
			Enabled:           0,
			CheckInterval:     10,
			FailThreshold:     5,
			ProbeInterval:     5,
			Cooldown:          60,
			Tier1Enabled:      1,
			Tier2Enabled:      1,
			Tier3Enabled:      0,
			Tier4Enabled:      1,
			BackupSimSlot:     "",
			MaxRebootsPerHour: 3,
		},
		Settings: SystemSettings{
			TempUnit:           "celsius",
			DistanceUnit:       "km",
			Hostname:           "",
			Timezone:           "UTC0",
			Zonename:           "UTC",
			SmsToolDevice:      "",
			SchedRebootEnabled: 0,
			SchedRebootTime:    "04:00",
			SchedRebootDays:    "0,1,2,3,4,5,6",
		},
		Update: UpdateConfig{
			IncludePrerelease: 1,
			AutoUpdateEnabled: 0,
			AutoUpdateTime:    "03:00",
		},
	}
}

// NewManager loads config from path or creates defaults if missing.
func NewManager(path string) (*Manager, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	m := &Manager{
		path: path,
		cfg:  NewDefaultConfig(),
	}

	if err := m.load(); err != nil {
		// If file doesn't exist, create it with defaults
		if os.IsNotExist(err) {
			if sErr := m.Save(); sErr != nil {
				return m, fmt.Errorf("failed to save initial default config: %w", sErr)
			}
			return m, nil
		}
		return m, fmt.Errorf("failed to load config from %s: %w", path, err)
	}

	return m, nil
}

func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid json in %s: %w", m.path, err)
	}

	m.cfg = cfg
	return nil
}

// Get returns an in-memory copy of the active config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Update modifies the config in-memory and persists atomically.
func (m *Manager) Update(fn func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fn(&m.cfg)
	return m.saveLocked()
}

// Save writes config to disk atomically via temporary file.
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked()
}

func (m *Manager) saveLocked() error {
	dir := filepath.Dir(m.path)

	// Refuse if parent directory doesn't exist or is not a directory
	dirInfo, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
				return fmt.Errorf("failed to create config dir %s: %w", dir, mkErr)
			}
		} else {
			return fmt.Errorf("failed to stat config dir %s: %w", dir, err)
		}
	} else if !dirInfo.IsDir() {
		return fmt.Errorf("config parent path %s is not a directory", dir)
	}

	// Refuse to write through symlinks or existing directory destinations
	if fi, err := os.Lstat(m.path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write config: %s is a symlink", m.path)
		}
		if fi.IsDir() {
			return fmt.Errorf("refusing to write config: %s is a directory", m.path)
		}
	}

	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	// Unique temporary file in same directory
	tmpPath := filepath.Join(dir, fmt.Sprintf(".qmanager.tmp.%d", time.Now().UnixNano()))

	// Symlink check on tmp path
	if fi, err := os.Lstat(tmpPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("temp path is a symlink: %s", tmpPath)
		}
		_ = os.Remove(tmpPath)
	}

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open tmp config %s: %w", tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tmp config: %w", err)
	}

	// Flush to disk blocks
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync tmp config: %w", err)
	}
	_ = f.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, m.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace config %s: %w", m.path, err)
	}

	return nil
}
