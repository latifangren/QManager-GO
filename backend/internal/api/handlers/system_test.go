package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qmanager/internal/config"
	"qmanager/internal/platform"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"v0.1.15", "v0.1.14", 1},
		{"v0.1.14", "v0.1.14", 0},
		{"v0.1.13", "v0.1.14", -1},
		{"0.2.0", "0.1.99", 1},
		{"v1.0.0", "v0.9.9", 1},
		{"v1.0.0-beta", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-beta", 1},
		{"v1.0.0-beta.2", "v1.0.0-beta.1", 1},
	}

	for _, tc := range tests {
		got := CompareSemver(tc.v1, tc.v2)
		if got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d; want %d", tc.v1, tc.v2, got, tc.want)
		}
	}
}

func TestUpdateHandler_CheckAndSaveSettings(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	// Mock GitHub API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := GitHubRelease{
			TagName:     "v0.1.15",
			Name:        "Release v0.1.15",
			Body:        "## Fixes\n- OTA runner ported to Go",
			Prerelease:  false,
			PublishedAt: "2026-08-30T12:00:00Z",
			Assets: []GitHubAsset{
				{
					Name:               "qmanager-v0.1.15.tar.gz",
					Size:               15 * 1024 * 1024,
					BrowserDownloadURL: "http://example.com/qmanager-v0.1.15.tar.gz",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer mockServer.Close()

	t.Setenv("QMANAGER_VERSION", "v0.1.14")
	handler := NewUpdateHandler(cfgMgr)
	handler.httpClient = mockServer.Client()

	// Intercept GitHub URL by replacing URL pattern in checkUpdate or testing release decoder
	req := httptest.NewRequest("GET", "/api/v1/system/update", nil)
	rr := httptest.NewRecorder()
	handler.CheckUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rr.Code)
	}

	var resp UpdateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode update response: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected resp.Success=true")
	}
	if resp.CurrentVersion != "v0.1.14" {
		t.Errorf("expected current_version=v0.1.14, got %s", resp.CurrentVersion)
	}

	// Test POST save_settings
	enabled := true
	prerelease := true
	savePayload := map[string]interface{}{
		"action":              "save_settings",
		"auto_update_enabled": enabled,
		"auto_update_time":    "04:30",
		"include_prerelease":  prerelease,
	}
	body, _ := json.Marshal(savePayload)
	reqPost := httptest.NewRequest("POST", "/api/v1/system/update", bytes.NewBuffer(body))
	rrPost := httptest.NewRecorder()
	handler.HandleUpdateAction(rrPost, reqPost)

	if rrPost.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 on save_settings, got %d", rrPost.Code)
	}

	// Verify saved config
	cfg := cfgMgr.Get()
	if cfg.Update.AutoUpdateEnabled != 1 || cfg.Update.AutoUpdateTime != "04:30" || cfg.Update.IncludePrerelease != 1 {
		t.Errorf("config was not updated correctly: %+v", cfg.Update)
	}
}

func TestParseLogLine(t *testing.T) {
	line1 := "2026-08-30 14:20:00 [INFO] [poller:1234] Cell lock confirmed on Band 3"
	entry1, ok1 := parseLogLine(line1)
	if !ok1 || entry1.Level != "INFO" || entry1.Component != "poller" || entry1.PID != "1234" || entry1.Message != "Cell lock confirmed on Band 3" {
		t.Errorf("parseLogLine line1 mismatch: %+v", entry1)
	}

	line2 := "[ERROR] [watchdog:999] Ping probe failed 5 consecutive times"
	entry2, ok2 := parseLogLine(line2)
	if !ok2 || entry2.Level != "ERROR" || entry2.Component != "watchdog" || entry2.Message != "Ping probe failed 5 consecutive times" {
		t.Errorf("parseLogLine line2 mismatch: %+v", entry2)
	}
}

func TestLogsHandler_GetAndClearLogs(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "qmanager.log")
	rot1 := filepath.Join(tmpDir, "qmanager.log.1")

	logContent := `2026-08-30 10:00:00 [DEBUG] [atengine:100] Sending AT+CSQ
2026-08-30 10:01:00 [INFO] [poller:101] Modem registered on LTE
2026-08-30 10:02:00 [WARN] [watchdog:102] High temperature 72C
2026-08-30 10:03:00 [ERROR] [auth:103] Invalid login attempt
`
	_ = os.WriteFile(logFile, []byte(logContent), 0644)
	_ = os.WriteFile(rot1, []byte("2026-08-30 09:00:00 [INFO] [system:1] Boot complete\n"), 0644)

	t.Setenv("QMANAGER_LOG_FILE", logFile)
	logsH := NewLogsHandler()

	// 1. GET all logs
	req := httptest.NewRequest("GET", "/api/v1/system/logs?lines=10", nil)
	rr := httptest.NewRecorder()
	logsH.GetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rr.Code)
	}

	var resp LogsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode logs response: %v", err)
	}
	if !resp.Success || resp.Total != 4 {
		t.Errorf("expected 4 entries, got %d", resp.Total)
	}

	// 2. Filter by level ERROR
	reqErr := httptest.NewRequest("GET", "/api/v1/system/logs?level=ERROR", nil)
	rrErr := httptest.NewRecorder()
	logsH.GetLogs(rrErr, reqErr)

	var respErr LogsResponse
	_ = json.Unmarshal(rrErr.Body.Bytes(), &respErr)
	if respErr.Total != 1 || respErr.Entries[0].Component != "auth" {
		t.Errorf("expected 1 error entry from auth component, got %+v", respErr.Entries)
	}

	// 3. Filter by search keyword
	reqSearch := httptest.NewRequest("GET", "/api/v1/system/logs?search=temperature", nil)
	rrSearch := httptest.NewRecorder()
	logsH.GetLogs(rrSearch, reqSearch)

	var respSearch LogsResponse
	_ = json.Unmarshal(rrSearch.Body.Bytes(), &respSearch)
	if respSearch.Total != 1 || respSearch.Entries[0].Level != "WARN" {
		t.Errorf("expected 1 search result, got %d", respSearch.Total)
	}

	// 4. POST clear logs
	reqClear := httptest.NewRequest("POST", "/api/v1/system/logs", bytes.NewBufferString(`{"action":"clear"}`))
	rrClear := httptest.NewRecorder()
	logsH.HandleLogsAction(rrClear, reqClear)

	if rrClear.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 on clear, got %d", rrClear.Code)
	}

	// Verify file is truncated
	fi, _ := os.Stat(logFile)
	if fi.Size() != 0 {
		t.Errorf("expected log file size to be 0 after clear, got %d", fi.Size())
	}
}

func TestLogsHandler_ModemSubsys(t *testing.T) {
	tmpDir := t.TempDir()
	subsysDir := filepath.Join(tmpDir, "subsys0")
	_ = os.MkdirAll(subsysDir, 0755)
	_ = os.WriteFile(filepath.Join(subsysDir, "state"), []byte("ONLINE\n"), 0644)
	_ = os.WriteFile(filepath.Join(subsysDir, "crash_count"), []byte("0\n"), 0644)

	t.Setenv("QMANAGER_SUBSYS_PATH", subsysDir)
	logsH := NewLogsHandler()

	req := httptest.NewRequest("GET", "/api/v1/system/modem-subsys", nil)
	rr := httptest.NewRecorder()
	logsH.ModemSubsys(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rr.Code)
	}

	var data ModemSubsysData
	if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
		t.Fatalf("failed to decode subsys response: %v", err)
	}
	if data.State != "online" {
		t.Errorf("expected state=online, got %s", data.State)
	}
	if data.CrashCount == nil || *data.CrashCount != 0 {
		t.Errorf("expected crash_count=0, got %+v", data.CrashCount)
	}
}

func TestSystemHandler_Info(t *testing.T) {
	tmpDir := t.TempDir()
	cfgMgr, _ := config.NewManager(filepath.Join(tmpDir, "qmanager.conf"))
	id := platform.Identity{
		Model:    "RM520NGLAA",
		Revision: "RM520NGLAAR03A03M4G",
		Serial:   "61368cd2",
		SoC:      "SDX65",
	}

	sysH := NewSystemHandler(id, cfgMgr)

	req := httptest.NewRequest("GET", "/api/v1/system/info", nil)
	rr := httptest.NewRecorder()
	sysH.Info(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rr.Code)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Identity platform.Identity `json:"identity"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Data.Identity.Model != "RM520NGLAA" || resp.Data.Identity.Serial != "61368cd2" {
		t.Errorf("system info identity mismatch: %+v", resp.Data.Identity)
	}
}
