package dpi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// setupTestPaths redirects package file paths to a temporary directory for test isolation.
func setupTestPaths(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	origRAMBinary := DPIRAMBinary
	origHostlist := DPIHostlistFile
	origConfig := DPIConfigFile
	origVerify := DPIVerifyFile
	origInstall := DPIInstallFile

	DPIRAMBinary = filepath.Join(dir, "tpws")
	DPIHostlistFile = filepath.Join(dir, "dpi_hostlist.txt")
	DPIConfigFile = filepath.Join(dir, "dpi_config.json")
	DPIVerifyFile = filepath.Join(dir, "qmanager_dpi_verify.json")
	DPIInstallFile = filepath.Join(dir, "qmanager_dpi_install.json")

	t.Cleanup(func() {
		DPIRAMBinary = origRAMBinary
		DPIHostlistFile = origHostlist
		DPIConfigFile = origConfig
		DPIVerifyFile = origVerify
		DPIInstallFile = origInstall
	})

	return dir
}

func TestEnsureBinaryExtracted(t *testing.T) {
	setupTestPaths(t)
	mgr := GetManager()

	// Initially, DPIRAMBinary does not exist in temp dir
	if _, err := os.Stat(DPIRAMBinary); err == nil {
		t.Fatalf("DPIRAMBinary %s already exists before extraction", DPIRAMBinary)
	}

	// Extract binary from embedded FS
	if err := mgr.EnsureBinaryExtracted(); err != nil {
		t.Fatalf("EnsureBinaryExtracted failed: %v", err)
	}

	// Verify extracted file exists and has content
	fi, err := os.Stat(DPIRAMBinary)
	if err != nil {
		t.Fatalf("Stat on extracted %s failed: %v", DPIRAMBinary, err)
	}
	if fi.Size() == 0 {
		t.Errorf("extracted binary has size 0")
	}

	// Second extraction should be idempotent and use cached path
	if err := mgr.EnsureBinaryExtracted(); err != nil {
		t.Fatalf("Second EnsureBinaryExtracted call failed: %v", err)
	}

	if !mgr.IsBinaryAvailable() {
		t.Errorf("expected IsBinaryAvailable to return true")
	}
}

func TestLoadConfig_SaveConfig(t *testing.T) {
	t.Run("NonExistentFile", func(t *testing.T) {
		setupTestPaths(t)

		cfg := LoadConfig()
		if cfg.VideoOptimizerEnabled != false {
			t.Errorf("expected VideoOptimizerEnabled=false, got %v", cfg.VideoOptimizerEnabled)
		}
		if cfg.MasqueradeEnabled != false {
			t.Errorf("expected MasqueradeEnabled=false, got %v", cfg.MasqueradeEnabled)
		}
		if cfg.SNIDomain != "speedtest.net" {
			t.Errorf("expected SNIDomain='speedtest.net', got %q", cfg.SNIDomain)
		}
		if cfg.ForceTCP != false {
			t.Errorf("expected ForceTCP=false, got %v", cfg.ForceTCP)
		}

		// Also check ReadConfig directly and via Manager
		readCfg := ReadConfig()
		if readCfg != cfg {
			t.Errorf("ReadConfig mismatch: %+v vs %+v", readCfg, cfg)
		}
		mgrCfg := GetManager().LoadConfig()
		if mgrCfg != cfg {
			t.Errorf("Manager.LoadConfig mismatch: %+v vs %+v", mgrCfg, cfg)
		}
	})

	t.Run("ValidJSONWriteAndReadBack", func(t *testing.T) {
		setupTestPaths(t)

		expected := Config{
			VideoOptimizerEnabled: true,
			MasqueradeEnabled:     false,
			SNIDomain:             "test.cdn.example.com",
			ForceTCP:              true,
		}

		if err := SaveConfig(expected); err != nil {
			t.Fatalf("SaveConfig failed: %v", err)
		}

		loaded := LoadConfig()
		if loaded != expected {
			t.Errorf("loaded config mismatch: got %+v, want %+v", loaded, expected)
		}

		// Update via Manager.SaveConfig with empty SNIDomain, which should default to speedtest.net
		update := Config{
			VideoOptimizerEnabled: false,
			MasqueradeEnabled:     true,
			SNIDomain:             "",
			ForceTCP:              false,
		}
		if err := GetManager().SaveConfig(update); err != nil {
			t.Fatalf("Manager.SaveConfig failed: %v", err)
		}

		loadedUpdate := LoadConfig()
		if loadedUpdate.SNIDomain != "speedtest.net" {
			t.Errorf("expected default SNIDomain 'speedtest.net' when empty, got %q", loadedUpdate.SNIDomain)
		}
		if !loadedUpdate.MasqueradeEnabled {
			t.Errorf("expected MasqueradeEnabled=true")
		}
	})

	t.Run("CorruptedJSON", func(t *testing.T) {
		setupTestPaths(t)

		// Corrupted JSON structure
		if err := os.WriteFile(DPIConfigFile, []byte("{invalid json structure"), 0644); err != nil {
			t.Fatalf("failed to write corrupted config: %v", err)
		}

		cfg := LoadConfig()
		if cfg.VideoOptimizerEnabled != false {
			t.Errorf("expected default VideoOptimizerEnabled=false for corrupted JSON, got %v", cfg.VideoOptimizerEnabled)
		}
		if cfg.SNIDomain != "speedtest.net" {
			t.Errorf("expected default SNIDomain='speedtest.net' for corrupted JSON, got %q", cfg.SNIDomain)
		}

		// Completely non-JSON data
		if err := os.WriteFile(DPIConfigFile, []byte("NOT_A_JSON_PAYLOAD_PLAIN_TEXT"), 0644); err != nil {
			t.Fatalf("failed to write non-JSON config: %v", err)
		}

		cfgRaw := LoadConfig()
		if cfgRaw.SNIDomain != "speedtest.net" {
			t.Errorf("expected default SNIDomain='speedtest.net' for non-JSON config, got %q", cfgRaw.SNIDomain)
		}
	})
}

func TestEnsureHostlist_ReadWrite(t *testing.T) {
	t.Run("MissingHostlistReturnsDefault", func(t *testing.T) {
		setupTestPaths(t)

		EnsureHostlistFile()
		if _, err := os.Stat(DPIHostlistFile); err != nil {
			t.Fatalf("EnsureHostlistFile did not create file: %v", err)
		}

		domains := ReadHostlist()
		if !reflect.DeepEqual(domains, DefaultHostlist) {
			t.Errorf("expected DefaultHostlist, got %v", domains)
		}
	})

	t.Run("EmptyHostlistReturnsDefault", func(t *testing.T) {
		setupTestPaths(t)

		if err := os.WriteFile(DPIHostlistFile, []byte(""), 0644); err != nil {
			t.Fatalf("failed to write empty file: %v", err)
		}

		domains := ReadHostlist()
		if !reflect.DeepEqual(domains, DefaultHostlist) {
			t.Errorf("expected DefaultHostlist for empty file, got %v", domains)
		}
	})

	t.Run("OnlyCommentsAndBlankLinesReturnsDefault", func(t *testing.T) {
		setupTestPaths(t)

		content := "# Comment 1\n\n  # Comment 2\n   \n"
		if err := os.WriteFile(DPIHostlistFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write comments file: %v", err)
		}

		domains := ReadHostlist()
		if !reflect.DeepEqual(domains, DefaultHostlist) {
			t.Errorf("expected DefaultHostlist for comments-only file, got %v", domains)
		}
	})

	t.Run("WriteAndReadCustomHostlist", func(t *testing.T) {
		setupTestPaths(t)

		custom := []string{"custom1.com", "custom2.org", "cdn.streaming.net"}
		if err := WriteHostlist(custom); err != nil {
			t.Fatalf("WriteHostlist failed: %v", err)
		}

		loaded := ReadHostlist()
		if !reflect.DeepEqual(loaded, custom) {
			t.Errorf("ReadHostlist mismatch: got %v, want %v", loaded, custom)
		}
	})

	t.Run("FilterCommentsAndWhitespace", func(t *testing.T) {
		setupTestPaths(t)

		fileContent := `# Top comment
alpha.com

# Another section
   beta.org   
gamma.net
# End comment
`
		if err := os.WriteFile(DPIHostlistFile, []byte(fileContent), 0644); err != nil {
			t.Fatalf("failed to write hostlist: %v", err)
		}

		loaded := ReadHostlist()
		expected := []string{"alpha.com", "beta.org", "gamma.net"}
		if !reflect.DeepEqual(loaded, expected) {
			t.Errorf("filtered hostlist mismatch: got %v, want %v", loaded, expected)
		}
	})
}

func TestManager_GetManager_Uptime(t *testing.T) {
	m1 := GetManager()
	if m1 == nil {
		t.Fatalf("GetManager returned nil")
	}
	m2 := GetManager()
	if m1 != m2 {
		t.Errorf("GetManager returned different instances: %p vs %p", m1, m2)
	}

	t.Run("UptimeNilCmd", func(t *testing.T) {
		m := &Manager{cmd: nil}
		if got := m.Uptime(); got != "0m" {
			t.Errorf("expected '0m' for nil cmd, got %q", got)
		}
	})

	t.Run("UptimeZeroStartTime", func(t *testing.T) {
		m := &Manager{cmd: &exec.Cmd{}, startTime: time.Time{}}
		if got := m.Uptime(); got != "0m" {
			t.Errorf("expected '0m' for zero startTime, got %q", got)
		}
	})

	t.Run("UptimeMinutes", func(t *testing.T) {
		m := &Manager{
			cmd:       &exec.Cmd{},
			startTime: time.Now().Add(-15*time.Minute - 5*time.Second),
		}
		got := m.Uptime()
		if got != "15m" {
			t.Errorf("expected '15m', got %q", got)
		}
	})

	t.Run("UptimeHoursAndMinutes", func(t *testing.T) {
		m := &Manager{
			cmd:       &exec.Cmd{},
			startTime: time.Now().Add(-135*time.Minute - 5*time.Second),
		}
		got := m.Uptime()
		if got != "2h 15m" {
			t.Errorf("expected '2h 15m', got %q", got)
		}
	})
}

func TestGetVerifyStatus(t *testing.T) {
	t.Run("FileDoesNotExist", func(t *testing.T) {
		setupTestPaths(t)
		_ = os.Remove(DPIVerifyFile)

		res := GetVerifyStatus()
		if !res.Success || res.Status != "idle" || res.Message != "No verification run" {
			t.Errorf("unexpected idle result: %+v", res)
		}

		mgrRes := GetManager().GetVerifyStatus()
		if mgrRes.Status != "idle" {
			t.Errorf("Manager.GetVerifyStatus unexpected status: %s", mgrRes.Status)
		}
	})

	t.Run("ValidJSON", func(t *testing.T) {
		setupTestPaths(t)

		expected := VerifyResult{
			Success:   true,
			Status:    "complete",
			Timestamp: "2026-09-12T12:00:00Z",
			WithoutBypass: &SpeedSample{
				SpeedMbps: 12.4,
				Throttled: true,
			},
			WithBypass: &SpeedSample{
				SpeedMbps: 45.8,
				Throttled: false,
			},
			Reference: &VerifyReference{
				SpeedMbps: 50.0,
				Source:    "cloudflare",
			},
			Improvement: "3.7x",
			Message:     "Verification complete",
			Detail:      "Test detail",
		}

		data, err := json.MarshalIndent(expected, "", "  ")
		if err != nil {
			t.Fatalf("MarshalIndent failed: %v", err)
		}
		if err := os.WriteFile(DPIVerifyFile, data, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		res := GetVerifyStatus()
		if !res.Success {
			t.Errorf("expected Success=true")
		}
		if res.Status != "complete" {
			t.Errorf("expected Status='complete', got %q", res.Status)
		}
		if res.Improvement != "3.7x" {
			t.Errorf("expected Improvement='3.7x', got %q", res.Improvement)
		}
		if res.WithoutBypass == nil || res.WithoutBypass.SpeedMbps != 12.4 || !res.WithoutBypass.Throttled {
			t.Errorf("WithoutBypass mismatch: %+v", res.WithoutBypass)
		}
		if res.WithBypass == nil || res.WithBypass.SpeedMbps != 45.8 || res.WithBypass.Throttled {
			t.Errorf("WithBypass mismatch: %+v", res.WithBypass)
		}
		if res.Reference == nil || res.Reference.SpeedMbps != 50.0 || res.Reference.Source != "cloudflare" {
			t.Errorf("Reference mismatch: %+v", res.Reference)
		}
	})

	t.Run("MalformedJSON", func(t *testing.T) {
		setupTestPaths(t)

		if err := os.WriteFile(DPIVerifyFile, []byte("{bad json"), 0644); err != nil {
			t.Fatalf("failed to write malformed verify file: %v", err)
		}

		res := GetVerifyStatus()
		if !res.Success || res.Status != "idle" || res.Message != "No verification run" {
			t.Errorf("expected idle default on malformed JSON, got %+v", res)
		}
	})
}

func TestMeasureThroughput(t *testing.T) {
	t.Run("NormalDownload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(15 * time.Millisecond)
			data := make([]byte, 128*1024)
			_, _ = w.Write(data)
		}))
		defer srv.Close()

		mbps := measureThroughput(srv.URL, 2*time.Second)
		if mbps <= 0 {
			t.Errorf("expected mbps > 0, got %f", mbps)
		}
	})

	t.Run("EmptyResponse", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		mbps := measureThroughput(srv.URL, 2*time.Second)
		if mbps != 0 {
			t.Errorf("expected mbps=0 for empty response, got %f", mbps)
		}
	})

	t.Run("UnreachableServer", func(t *testing.T) {
		mbps := measureThroughput("http://127.0.0.1:0/unreachable", 100*time.Millisecond)
		if mbps != 0 {
			t.Errorf("expected mbps=0 for unreachable server, got %f", mbps)
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte("too late"))
		}))
		defer srv.Close()

		mbps := measureThroughput(srv.URL, 50*time.Millisecond)
		if mbps != 0 {
			t.Errorf("expected mbps=0 for timeout, got %f", mbps)
		}
	})
}

func TestIptablesRuleOperations(t *testing.T) {
	// These functions should handle iptables errors gracefully without panicking
	if err := ApplyIptablesRule(); err != nil {
		t.Errorf("ApplyIptablesRule returned unexpected error: %v", err)
	}

	RemoveIptablesRule() // Should not panic

	if err := ApplyForceTCPRule(); err != nil {
		t.Errorf("ApplyForceTCPRule returned unexpected error: %v", err)
	}

	RemoveForceTCPRule() // Should not panic

	// IsForceTCPActive returns false when iptables fails or no rule found
	_ = IsForceTCPActive()

	// GetPacketsProcessed returns 0 when iptables fails
	mgr := GetManager()
	pkts := mgr.GetPacketsProcessed()
	if pkts < 0 {
		t.Errorf("expected non-negative packets, got %d", pkts)
	}
}

func TestMathRound(t *testing.T) {
	tests := []struct {
		val       float64
		precision int
		want      float64
	}{
		{val: 45.84, precision: 1, want: 45.8},
		{val: 45.86, precision: 1, want: 45.9},
		{val: 12.345, precision: 2, want: 12.35},
		{val: 10.0, precision: 0, want: 10.0},
	}

	for _, tt := range tests {
		got := mathRound(tt.val, tt.precision)
		if got != tt.want {
			t.Errorf("mathRound(%v, %d) = %v, want %v", tt.val, tt.precision, got, tt.want)
		}
	}
}

func TestSyncStateAndEngineLifecycle(t *testing.T) {
	setupTestPaths(t)

	// Test SyncState with stopped config
	if err := SaveConfig(Config{
		VideoOptimizerEnabled: false,
		MasqueradeEnabled:     false,
		ForceTCP:              false,
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	SyncState()

	mgr := GetManager()
	if mgr.IsRunning() {
		t.Errorf("expected engine not running with disabled config")
	}

	// Test StopEngine when already stopped
	mgr.StopEngine()

	// Test SyncState with ForceTCP
	if err := SaveConfig(Config{
		VideoOptimizerEnabled: false,
		MasqueradeEnabled:     false,
		ForceTCP:              true,
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	SyncState()
}

func TestWriteConfig(t *testing.T) {
	t.Run("ValidConfigAndFormatting", func(t *testing.T) {
		setupTestPaths(t)

		cfg := Config{
			VideoOptimizerEnabled: true,
			MasqueradeEnabled:     false,
			SNIDomain:             "cdn.streaming.test",
			ForceTCP:              true,
		}

		if err := WriteConfig(cfg); err != nil {
			t.Fatalf("WriteConfig failed: %v", err)
		}

		data, err := os.ReadFile(DPIConfigFile)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "  \"video_optimizer_enabled\": true") {
			t.Errorf("expected indented JSON format, got: %s", content)
		}
		if !strings.Contains(content, "cdn.streaming.test") {
			t.Errorf("expected SNIDomain in file content, got: %s", content)
		}

		loaded := ReadConfig()
		if loaded != cfg {
			t.Errorf("loaded config mismatch: got %+v, want %+v", loaded, cfg)
		}
	})

	t.Run("PathCreationError", func(t *testing.T) {
		dir := setupTestPaths(t)

		// Create a regular file to block directory creation
		blocker := filepath.Join(dir, "blocker_file")
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatalf("failed to write blocker file: %v", err)
		}

		// Destination path has blocker file as parent directory
		DPIConfigFile = filepath.Join(blocker, "sub_dir", "config.json")
		err := WriteConfig(Config{VideoOptimizerEnabled: true})
		if err == nil {
			t.Errorf("expected error when directory creation fails, got nil")
		}
	})
}

func TestStartEngine_FailedStart(t *testing.T) {
	dir := setupTestPaths(t)
	mgr := GetManager()

	// Point DPIRAMBinary to non-existent directory and file
	DPIRAMBinary = filepath.Join(dir, "non_existent_folder", "invalid_bin_path")

	errOpt := mgr.StartEngine("video_optimizer")
	if errOpt == nil || !strings.Contains(errOpt.Error(), "failed to start tpws") {
		t.Errorf("expected 'failed to start tpws' error for video_optimizer mode, got: %v", errOpt)
	}

	errDef := mgr.StartEngine("default")
	if errDef == nil || !strings.Contains(errDef.Error(), "failed to start tpws") {
		t.Errorf("expected 'failed to start tpws' error for default mode, got: %v", errDef)
	}
}

func TestStartEngine_ValidStart_And_Stop(t *testing.T) {
	setupTestPaths(t)

	// Create and start a dummy process to test stopLocked and StopEngine logic
	var cmd *exec.Cmd
	if os.PathSeparator == '\\' {
		cmd = exec.Command("cmd", "/c", "ping -n 10 127.0.0.1 >nul")
	} else {
		cmd = exec.Command("sleep", "10")
	}

	if err := cmd.Start(); err == nil {
		m := &Manager{
			cmd:       cmd,
			startTime: time.Now(),
		}

		if !m.IsRunning() {
			t.Errorf("expected IsRunning()=true for active process")
		}

		m.StopEngine()

		if m.cmd != nil {
			t.Errorf("expected m.cmd == nil after StopEngine()")
		}
		if m.IsRunning() {
			t.Errorf("expected IsRunning()=false after StopEngine()")
		}
	}

	// Test stopLocked directly when cmd is nil
	mNil := &Manager{}
	mNil.stopLocked()
	if mNil.cmd != nil {
		t.Errorf("expected mNil.cmd to remain nil")
	}
}

func TestStartVerify_Flow(t *testing.T) {
	setupTestPaths(t)
	mgr := GetManager()

	// 1. Initial StartVerify
	mgr.StartVerify()

	// Verify running state was immediately written
	data, err := os.ReadFile(DPIVerifyFile)
	if err != nil {
		t.Fatalf("failed to read verify file: %v", err)
	}
	var runningRes VerifyResult
	if err := json.Unmarshal(data, &runningRes); err != nil {
		t.Fatalf("failed to unmarshal verify file: %v", err)
	}
	if runningRes.Status != "running" {
		t.Errorf("expected status 'running', got %q", runningRes.Status)
	}

	// 2. Calling StartVerify again while running should return early
	mgr.StartVerify()

	// 3. Wait for background verification goroutine to finish (up to 8 seconds)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		status := GetVerifyStatus()
		if status.Status == "complete" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	finalStatus := GetVerifyStatus()
	if finalStatus.Status != "complete" {
		t.Errorf("expected final status 'complete', got %q", finalStatus.Status)
	}
	if finalStatus.Improvement == "" {
		t.Errorf("expected non-empty improvement in final status")
	}
	if finalStatus.WithoutBypass == nil || finalStatus.WithBypass == nil {
		t.Errorf("expected non-nil speed samples in final status")
	}
}

func TestParseIptablesPackets(t *testing.T) {
	// 1. Valid line with "redir ports 989"
	line1 := " 12345 567890 REDIRECT tcp -- * * 0.0.0.0/0 0.0.0.0/0 multiport dports 80,443 redir ports 989"
	if pkts := parseIptablesPackets(line1); pkts != 12345 {
		t.Errorf("expected 12345, got %d", pkts)
	}

	// 2. Line with "REDIRECT" and "989"
	line2 := "Chain PREROUTING\n 9999 12345 REDIRECT tcp -- * * 0.0.0.0/0 0.0.0.0/0 to:989"
	if pkts := parseIptablesPackets(line2); pkts != 9999 {
		t.Errorf("expected 9999, got %d", pkts)
	}

	// 3. Malformed first field
	line3 := " not_a_number 12345 REDIRECT tcp -- redir ports 989"
	if pkts := parseIptablesPackets(line3); pkts != 0 {
		t.Errorf("expected 0 for malformed field, got %d", pkts)
	}

	// 4. 0 packets
	line4 := " 0 0 REDIRECT tcp -- * * 0.0.0.0/0 0.0.0.0/0 multiport dports 80,443 redir ports 989"
	if pkts := parseIptablesPackets(line4); pkts != 0 {
		t.Errorf("expected 0 packets, got %d", pkts)
	}

	// 5. Unrelated lines
	line5 := "Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)\n pkts bytes target prot opt in out source destination"
	if pkts := parseIptablesPackets(line5); pkts != 0 {
		t.Errorf("expected 0 for unrelated lines, got %d", pkts)
	}
}
