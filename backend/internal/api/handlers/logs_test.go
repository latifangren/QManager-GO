package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qmanager/internal/config"
	"qmanager/internal/telemetry"
)

func setupLogsTest(t *testing.T) (string, *config.Manager) {
	t.Helper()
	dir := t.TempDir()

	origLog := DefaultLogFilePath
	origSubsys := DefaultSubsysDevice
	origCrash := CrashLogFile
	origRamdump := RamdumpDir

	DefaultLogFilePath = filepath.Join(dir, "qmanager.log")
	DefaultSubsysDevice = filepath.Join(dir, "subsys0")
	CrashLogFile = filepath.Join(dir, "modem_crashes.json")
	RamdumpDir = filepath.Join(dir, "ramdump_modem")

	t.Cleanup(func() {
		DefaultLogFilePath = origLog
		DefaultSubsysDevice = origSubsys
		CrashLogFile = origCrash
		RamdumpDir = origRamdump
	})

	_ = os.MkdirAll(DefaultSubsysDevice, 0755)
	_ = os.MkdirAll(RamdumpDir, 0755)

	confPath := filepath.Join(dir, "qmanager.conf")
	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	return dir, cfgMgr
}

func TestNewLogsHandler_Variants(t *testing.T) {
	_, cfgMgr := setupLogsTest(t)

	history := telemetry.NewTelemetryHistory(10, 10, 10)
	version := "1.0.0-test"

	// 1. NewLogsHandler(history, cfgMgr, version)
	hMulti := NewLogsHandler(history, cfgMgr, version)
	if hMulti == nil {
		t.Fatalf("expected non-nil LogsHandler")
	}
	if hMulti.cfgMgr != cfgMgr {
		t.Errorf("expected cfgMgr to be set")
	}

	// 2. NewLogsHandler(cfgMgr)
	hSingle := NewLogsHandler(cfgMgr)
	if hSingle == nil || hSingle.cfgMgr != cfgMgr {
		t.Errorf("expected hSingle to have cfgMgr")
	}

	// 3. NewLogsHandler()
	hEmpty := NewLogsHandler()
	if hEmpty == nil {
		t.Errorf("expected non-nil hEmpty")
	}
}

func TestLogsHandler_GetTzLocation(t *testing.T) {
	_, cfgMgr := setupLogsTest(t)

	// 1. With nil cfgMgr -> returns WIB (+0700)
	hNil := NewLogsHandler()
	locNil := hNil.getTzLocation()
	if locNil.String() != "WIB" {
		t.Errorf("expected WIB for nil cfgMgr, got %s", locNil.String())
	}

	// 2. Timezone "WIB-7" or zonename "Asia/Jakarta"
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Settings.Timezone = "WIB-7"
		c.Settings.Zonename = "Asia/Jakarta"
	})
	hWIB := NewLogsHandler(cfgMgr)
	locWIB := hWIB.getTzLocation()
	if !strings.Contains(locWIB.String(), "Jakarta") && locWIB.String() != "WIB" {
		t.Errorf("expected Jakarta/WIB, got %s", locWIB.String())
	}

	// 3. Timezone "WITA-8" or zonename "Asia/Makassar"
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Settings.Timezone = "WITA-8"
		c.Settings.Zonename = "Asia/Makassar"
	})
	hWITA := NewLogsHandler(cfgMgr)
	locWITA := hWITA.getTzLocation()
	if !strings.Contains(locWITA.String(), "Makassar") && locWITA.String() != "WITA" {
		t.Errorf("expected Makassar/WITA, got %s", locWITA.String())
	}

	// 4. Timezone "WIT-9" or zonename "Asia/Jayapura"
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Settings.Timezone = "WIT-9"
		c.Settings.Zonename = "Asia/Jayapura"
	})
	hWIT := NewLogsHandler(cfgMgr)
	locWIT := hWIT.getTzLocation()
	if !strings.Contains(locWIT.String(), "Jayapura") && locWIT.String() != "WIT" {
		t.Errorf("expected Jayapura/WIT, got %s", locWIT.String())
	}

	// 5. POSIX timezone "EST5EDT"
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Settings.Timezone = "EST5EDT"
		c.Settings.Zonename = ""
	})
	hEST := NewLogsHandler(cfgMgr)
	locEST := hEST.getTzLocation()
	if locEST == nil {
		t.Errorf("expected non-nil locEST for EST5EDT")
	}
}

func TestLogsHandler_ParseLogLine(t *testing.T) {
	h := NewLogsHandler()

	// 1. Empty string -> returns false
	if _, ok := h.parseLogLine(""); ok {
		t.Errorf("expected false for empty line")
	}
	if _, ok := h.parseLogLine("   "); ok {
		t.Errorf("expected false for whitespace line")
	}

	// 2. Legacy bracket format: "[2026-09-11 15:33:37] ERROR [qcmd:3127] Command returned ERROR: AT+QRSRQ"
	legacyLine := "[2026-09-11 15:33:37] ERROR [qcmd:3127] Command returned ERROR: AT+QRSRQ"
	entry1, ok1 := h.parseLogLine(legacyLine)
	if !ok1 {
		t.Fatalf("expected true for legacy bracket format")
	}
	if entry1.Level != "ERROR" || entry1.Component != "qcmd" || entry1.PID != "3127" {
		t.Errorf("unexpected parsed legacy entry: %+v", entry1)
	}

	// 3. Standard Linux /var/log/messages syslog format
	syslogLine := "Sep 11 15:33:37 sdxprairie kernel: [12345.67] some test message"
	entry2, ok2 := h.parseLogLine(syslogLine)
	if !ok2 {
		t.Fatalf("expected true for syslog format")
	}
	if entry2.Component != "kernel" {
		t.Errorf("expected component 'kernel', got %q", entry2.Component)
	}

	// 4. Plain unformatted line
	plainLine := "Just a plain unformatted line of text without timestamp"
	entry3, ok3 := h.parseLogLine(plainLine)
	if !ok3 {
		t.Fatalf("expected true for fallback unformatted line")
	}
	if entry3.Message != plainLine {
		t.Errorf("expected plain message %q, got %q", plainLine, entry3.Message)
	}
}

func TestLogsHandler_CountCrashes(t *testing.T) {
	_, cfgMgr := setupLogsTest(t)
	h := NewLogsHandler(cfgMgr)

	// Initially 0 crashes
	if count := h.countCrashes(); count != 0 {
		t.Errorf("expected 0 initial crashes, got %d", count)
	}

	// Create sample crash files in RamdumpDir and CrashLogFile
	_ = os.WriteFile(filepath.Join(h.ramdumpDir, "ramdump_modem_01.bin"), []byte("dump1"), 0644)
	_ = os.WriteFile(filepath.Join(h.ramdumpDir, "ramdump_modem_02.bin"), []byte("dump2"), 0644)
	_ = os.WriteFile(filepath.Join(h.ramdumpDir, "other_file.txt"), []byte("ignore"), 0644)

	crashJSON := `[{"timestamp": 1726050000, "reason": "subsys crash"}]`
	_ = os.WriteFile(CrashLogFile, []byte(crashJSON), 0644)

	if count := h.countCrashes(); count != 2 {
		t.Errorf("expected 2 crashes from ramdumpDir, got %d", count)
	}
}

func TestLogsHandler_QueryLiveSubsys(t *testing.T) {
	_, cfgMgr := setupLogsTest(t)
	h := NewLogsHandler(cfgMgr)

	// Create subsys0/name and subsys0/state in temp dir
	_ = os.WriteFile(filepath.Join(DefaultSubsysDevice, "name"), []byte("modem\n"), 0644)
	_ = os.WriteFile(filepath.Join(DefaultSubsysDevice, "state"), []byte("ONLINE\n"), 0644)

	subsys := h.queryLiveSubsys()
	if subsys == nil {
		t.Fatalf("expected non-nil ModemSubsysData")
	}
	if subsys.SubsysName != "modem" {
		t.Errorf("expected SubsysName 'modem', got %q", subsys.SubsysName)
	}
	if subsys.State != "online" {
		t.Errorf("expected State 'online', got %q", subsys.State)
	}
}

func TestLogsHandler_GetLogsAndDownloadLogs(t *testing.T) {
	dir, cfgMgr := setupLogsTest(t)
	h := NewLogsHandler(cfgMgr)

	// Populate log file with sample lines
	logContent := `[2026-09-11 15:30:00] INFO [qcmd:1000] Initialized
[2026-09-11 15:33:37] ERROR [qcmd:3127] Command returned ERROR: AT+QRSRQ
[2026-09-11 15:34:00] WARN [daemon:1001] Minor warning
`
	logPath := filepath.Join(dir, "qmanager.log")
	_ = os.WriteFile(logPath, []byte(logContent), 0644)
	h.logFilePath = logPath

	// 1. GetLogs with filters: level=ERROR, component=qcmd, lines=10
	req := httptest.NewRequest(http.MethodGet, "/api/system/logs?level=ERROR&component=qcmd&lines=10", nil)
	w := httptest.NewRecorder()
	h.GetLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetLogs returned %d, want 200", w.Code)
	}

	var resp LogsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode LogsResponse: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true in LogsResponse")
	}

	// 2. DownloadLogs
	reqDL := httptest.NewRequest(http.MethodGet, "/api/system/logs/download", nil)
	wDL := httptest.NewRecorder()
	h.DownloadLogs(wDL, reqDL)

	if wDL.Code != http.StatusOK {
		t.Fatalf("DownloadLogs returned %d, want 200", wDL.Code)
	}
	if wDL.Header().Get("Content-Disposition") != "attachment; filename=\"qmanager.log\"" {
		t.Errorf("unexpected Content-Disposition: %s", wDL.Header().Get("Content-Disposition"))
	}
	if !strings.Contains(wDL.Body.String(), "Command returned ERROR") {
		t.Errorf("expected downloaded logs to contain log content")
	}
}
