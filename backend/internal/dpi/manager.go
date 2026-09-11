package dpi

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed embeds/tpws
var embeddedFS embed.FS

const (
	// Port for tpws redirect
	DPIPort = "989"
	DPIBindAddr = "0.0.0.0"
	DPIRAMBinary = "/tmp/tpws"
)

var (
	DPIHostlistFile = "/etc/qmanager/dpi_hostlist.txt"
	DPIConfigFile = "/etc/qmanager/dpi_config.json"
)

// DefaultHostlist contains standard video streaming / CDN domains.
var DefaultHostlist = []string{
	"googlevideo.com",
	"youtube.com",
	"youtu.be",
	"ytimg.com",
	"netflix.com",
	"nflxvideo.net",
	"nflxext.com",
	"nflximg.net",
	"tiktokcdn.com",
	"tiktokv.com",
	"byteoversea.com",
	"ibytedtos.com",
	"fast.com",
}

// Config represents persistent traffic engine state.
type Config struct {
	VideoOptimizerEnabled bool   `json:"video_optimizer_enabled"`
	MasqueradeEnabled     bool   `json:"masquerade_enabled"`
	SNIDomain             string `json:"sni_domain"`
}

// Manager coordinates the lifecycle of tpws binary extraction, process execution, and iptables rules.
type Manager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	startTime time.Time
}

var globalManager = &Manager{}

// GetManager returns the global DPI Manager instance.
func GetManager() *Manager {
	return globalManager
}

// EnsureBinaryExtracted checks if /tmp/tpws exists and is executable. If not, extracts from embedded FS.
func (m *Manager) EnsureBinaryExtracted() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already extracted and valid
	if fi, err := os.Stat(DPIRAMBinary); err == nil && fi.Size() > 0 && fi.Mode()&0111 != 0 {
		return nil
	}

	data, err := embeddedFS.ReadFile("embeds/tpws")
	if err != nil {
		return fmt.Errorf("failed to read embedded tpws binary: %w", err)
	}

	tmpExtract := fmt.Sprintf("%s.tmp.%d", DPIRAMBinary, time.Now().UnixNano())
	if err := os.WriteFile(tmpExtract, data, 0755); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmpExtract, err)
	}

	if err := os.Rename(tmpExtract, DPIRAMBinary); err != nil {
		_ = os.Remove(tmpExtract)
		return fmt.Errorf("failed to move tpws to %s: %w", DPIRAMBinary, err)
	}

	_ = os.Chmod(DPIRAMBinary, 0755)
	return nil
}

// IsBinaryAvailable returns true since tpws is embedded in binary.
func (m *Manager) IsBinaryAvailable() bool {
	return true
}

// ReadConfig loads the config from disk, returning default if not present.
func ReadConfig() Config {
	data, err := os.ReadFile(DPIConfigFile)
	if err != nil {
		return Config{
			VideoOptimizerEnabled: false,
			MasqueradeEnabled:     false,
			SNIDomain:             "speedtest.net",
		}
	}
	var c Config
	_ = json.Unmarshal(data, &c)
	return c
}

// WriteConfig stores the config to disk atomically.
func WriteConfig(c Config) error {
	dir := filepath.Dir(DPIConfigFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", DPIConfigFile, time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	return os.Rename(tmpFile, DPIConfigFile)
}

// EnsureHostlistFile ensures /etc/qmanager/dpi_hostlist.txt exists with default domains if empty.
func EnsureHostlistFile() {
	if _, err := os.Stat(DPIHostlistFile); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(DPIHostlistFile), 0755)
		content := strings.Join(DefaultHostlist, "\n") + "\n"
		_ = os.WriteFile(DPIHostlistFile, []byte(content), 0644)
	}
}

// ReadHostlist returns current hostlist domains.
func ReadHostlist() []string {
	EnsureHostlistFile()
	data, err := os.ReadFile(DPIHostlistFile)
	if err != nil {
		return DefaultHostlist
	}
	lines := strings.Split(string(data), "\n")
	var res []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			res = append(res, l)
		}
	}
	return res
}

// WriteHostlist updates the hostlist file atomically.
func WriteHostlist(domains []string) error {
	dir := filepath.Dir(DPIHostlistFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content := strings.Join(domains, "\n") + "\n"
	tmpFile := fmt.Sprintf("%s.tmp.%d", DPIHostlistFile, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, DPIHostlistFile)
}

// ApplyIptablesRule inserts REDIRECT rule for bridge0 LAN on ports 80 & 443.
func ApplyIptablesRule() error {
	RemoveIptablesRule()

	// Direct iptables command
	cmd := exec.Command("iptables", "-w", "5", "-t", "nat", "-I", "PREROUTING",
		"-i", "bridge0", "-p", "tcp", "-m", "multiport", "--dports", "80,443",
		"-j", "REDIRECT", "--to-ports", DPIPort)
	_ = cmd.Run()
	return nil
}

// RemoveIptablesRule flushes iptables redirect rules for port 989.
func RemoveIptablesRule() {
	for i := 0; i < 8; i++ {
		cmd := exec.Command("iptables", "-w", "5", "-t", "nat", "-D", "PREROUTING",
			"-i", "bridge0", "-p", "tcp", "-m", "multiport", "--dports", "80,443",
			"-j", "REDIRECT", "--to-ports", DPIPort)
		if err := cmd.Run(); err != nil {
			break
		}
	}
}

// StartEngine starts tpws process from /tmp/tpws with corresponding mode args.
func (m *Manager) StartEngine(mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop any existing process first
	m.stopLocked()

	// Ensure binary in RAM
	data, err := embeddedFS.ReadFile("embeds/tpws")
	if err == nil {
		_ = os.WriteFile(DPIRAMBinary, data, 0755)
	}

	EnsureHostlistFile()

	var args []string
	args = append(args, "--port="+DPIPort, "--bind-addr="+DPIBindAddr)
	args = append(args, "--filter-l7=tls,http", "--split-pos=1,midsld,sniext+1", "--disorder=tls")

	if mode == "video_optimizer" {
		args = append(args, "--hostlist="+DPIHostlistFile)
	}

	cmd := exec.Command(DPIRAMBinary, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tpws: %w", err)
	}

	m.cmd = cmd
	m.startTime = time.Now()

	// Apply firewall rule
	_ = ApplyIptablesRule()

	return nil
}

// StopEngine stops tpws and removes iptables rules.
func (m *Manager) StopEngine() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	RemoveIptablesRule()

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_ = m.cmd.Wait()
		m.cmd = nil
	}

	// Also kill any orphaned tpws process on device
	_ = exec.Command("killall", "tpws").Run()
	_ = exec.Command("pkill", "-9", "-f", "tpws").Run()
}

// IsRunning checks if tpws process is active.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		if err := m.cmd.Process.Signal(os.Signal(nil)); err == nil {
			return true
		}
	}
	// Fallback check
	out, err := exec.Command("pgrep", "-f", "tpws").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// Uptime returns formatted uptime string.
func (m *Manager) Uptime() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.startTime.IsZero() {
		return "0m"
	}
	dur := time.Since(m.startTime)
	mins := int(dur.Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh %dm", mins/60, mins%60)
}

// GetPacketsProcessed counts packets hitting iptables rule if possible.
func (m *Manager) GetPacketsProcessed() int64 {
	out, err := exec.Command("iptables", "-t", "nat", "-L", "PREROUTING", "-v", "-n", "-x").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		if strings.Contains(l, "redir ports 989") || strings.Contains(l, "REDIRECT") && strings.Contains(l, "989") {
			fields := strings.Fields(l)
			if len(fields) > 0 {
				if pkts, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
					return pkts
				}
			}
		}
	}
	return 0
}

// SyncState synchronizes configuration state with daemon process (called on boot and config change).
func SyncState() {
	cfg := ReadConfig()
	mgr := GetManager()
	if cfg.VideoOptimizerEnabled {
		_ = mgr.StartEngine("video_optimizer")
	} else if cfg.MasqueradeEnabled {
		_ = mgr.StartEngine("masquerade")
	} else {
		mgr.StopEngine()
	}
}
