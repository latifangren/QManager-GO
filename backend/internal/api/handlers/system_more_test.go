package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func newTestSystemContext(t *testing.T) (*atengine.MockTransport, *atengine.Engine, *config.Manager, *telemetry.Poller, *telemetry.Watchdog) {
	t.Helper()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() {
		_ = eng.Close()
	})

	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 1*time.Second)
	prober := telemetry.NewPingProber("127.0.0.1:9", 50*time.Millisecond)
	watchdog := telemetry.NewWatchdog(eng, cfgMgr, prober)

	return mock, eng, cfgMgr, poller, watchdog
}

// 1. Auth Handler Tests
func TestAuthHandler(t *testing.T) {
	h := NewAuthHandler("test-secret-123")

	// Login - bad password
	bodyBad, _ := json.Marshal(LoginRequest{Password: "wrong"})
	reqBad := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(bodyBad))
	wBad := httptest.NewRecorder()
	h.Login(wBad, reqBad)
	if wBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", wBad.Code)
	}

	// Login - invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`bad`))
	wBadJSON := httptest.NewRecorder()
	h.Login(wBadJSON, reqBadJSON)
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}

	// Login - correct password
	bodyGood, _ := json.Marshal(LoginRequest{Password: "test-secret-123"})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(bodyGood))
	wGood := httptest.NewRecorder()
	h.Login(wGood, reqGood)
	if wGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for correct login, got %d", wGood.Code)
	}

	var loginResp Response
	_ = json.NewDecoder(wGood.Body).Decode(&loginResp)
	dataMap, ok := loginResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map in response, got %+v", loginResp.Data)
	}
	token, ok := dataMap["token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected non-empty token string in %+v", dataMap)
	}

	// Check Auth - valid token in Authorization header
	reqCheck := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	reqCheck.Header.Set("Authorization", "Bearer "+token)
	wCheck := httptest.NewRecorder()
	h.Check(wCheck, reqCheck)
	if wCheck.Code != http.StatusOK {
		t.Errorf("expected 200 for valid token check, got %d", wCheck.Code)
	}

	// Check Auth - invalid token
	reqInvalid := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	reqInvalid.Header.Set("Authorization", "Bearer invalid-token")
	wInvalid := httptest.NewRecorder()
	h.Check(wInvalid, reqInvalid)
	if wInvalid.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token check, got %d", wInvalid.Code)
	}

	// Logout
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.Header.Set("Authorization", "Bearer "+token)
	wLogout := httptest.NewRecorder()
	h.Logout(wLogout, reqLogout)
	if wLogout.Code != http.StatusOK {
		t.Errorf("expected 200 for logout, got %d", wLogout.Code)
	}

	// Token should now be invalidated
	wAfterLogout := httptest.NewRecorder()
	h.Check(wAfterLogout, reqCheck)
	if wAfterLogout.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", wAfterLogout.Code)
	}
}

// 2. Logs Handler Tests (Ring buffer export & diagnostic metrics)
func TestLogsHandler(t *testing.T) {
	h := NewLogsHandler()

	// Add test entries to memory ring buffer
	telemetry.GetGlobalLogger().Add(telemetry.LevelInfo, "test_facility", "Sample log entry 1")
	telemetry.GetGlobalLogger().Add(telemetry.LevelError, "test_facility", "Sample error message 2")

	reqGet := httptest.NewRequest(http.MethodGet, "/api/system/logs?lines=50&level=INFO", nil)
	wGet := httptest.NewRecorder()
	h.GetLogs(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetLogs returned %d, want 200", wGet.Code)
	}

	var logsResp map[string]interface{}
	if err := json.NewDecoder(wGet.Body).Decode(&logsResp); err != nil {
		t.Fatalf("failed to decode logs response: %v", err)
	}
	if logsResp["success"] != true {
		t.Errorf("expected success=true, got %v", logsResp["success"])
	}
}

// 3. History Handler Tests (Signal, Ping, Events)
func TestHistoryHandler(t *testing.T) {
	h := NewHistoryHandler()

	// Seed some history points
	lat := 12.5
	telemetry.GetGlobalHistory().RecordPing(telemetry.PingHistoryPoint{
		Timestamp: time.Now().Unix(),
		LatencyMs: &lat,
		LossPct:   0,
	})
	telemetry.GetGlobalHistory().RecordEvent(telemetry.NetworkEventItem{
		Timestamp: time.Now().Unix(),
		Type:      "band_change",
		Message:   "Switched to B3",
		Severity:  "info",
	})

	// Signal History
	reqSig := httptest.NewRequest(http.MethodGet, "/api/telemetry/history/signal?limit=10", nil)
	wSig := httptest.NewRecorder()
	h.FetchSignalHistory(wSig, reqSig)
	if wSig.Code != http.StatusOK {
		t.Errorf("FetchSignalHistory returned %d, want 200", wSig.Code)
	}

	// Ping History
	reqPing := httptest.NewRequest(http.MethodGet, "/api/telemetry/history/ping?limit=10", nil)
	wPing := httptest.NewRecorder()
	h.FetchPingHistory(wPing, reqPing)
	if wPing.Code != http.StatusOK {
		t.Errorf("FetchPingHistory returned %d, want 200", wPing.Code)
	}

	// Events History
	reqEvents := httptest.NewRequest(http.MethodGet, "/api/telemetry/events?limit=10", nil)
	wEvents := httptest.NewRecorder()
	h.FetchEvents(wEvents, reqEvents)
	if wEvents.Code != http.StatusOK {
		t.Errorf("FetchEvents returned %d, want 200", wEvents.Code)
	}
}

// 4. Public Handler Tests (Overview, Hostname, Units)
func TestPublicHandler(t *testing.T) {
	_, _, cfgMgr, poller, _ := newTestSystemContext(t)
	identity := platform.Identity{Model: "RG501Q-EU"}
	h := NewPublicHandler(poller, cfgMgr, identity)

	// Overview
	reqOverview := httptest.NewRequest(http.MethodGet, "/api/public/overview", nil)
	wOverview := httptest.NewRecorder()
	h.Overview(wOverview, reqOverview)
	if wOverview.Code != http.StatusOK {
		t.Errorf("Overview returned %d, want 200", wOverview.Code)
	}

	// Hostname
	reqHn := httptest.NewRequest(http.MethodGet, "/api/public/hostname", nil)
	wHn := httptest.NewRecorder()
	h.Hostname(wHn, reqHn)
	if wHn.Code != http.StatusOK {
		t.Errorf("Hostname returned %d, want 200", wHn.Code)
	}

	// Units
	reqUnits := httptest.NewRequest(http.MethodGet, "/api/public/units", nil)
	wUnits := httptest.NewRecorder()
	h.Units(wUnits, reqUnits)
	if wUnits.Code != http.StatusOK {
		t.Errorf("Units returned %d, want 200", wUnits.Code)
	}
}

// 5. Watchdog Handler Tests
func TestWatchdogHandler(t *testing.T) {
	_, _, cfgMgr, _, watchdog := newTestSystemContext(t)
	h := NewWatchdogHandler(cfgMgr, watchdog)

	// GET Watchdog status
	reqGet := httptest.NewRequest(http.MethodGet, "/api/monitoring/watchdog", nil)
	wGet := httptest.NewRecorder()
	h.HandleWatchdog(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleWatchdog GET returned %d, want 200", wGet.Code)
	}

	var wdResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&wdResp)
	if wdResp["success"] != true {
		t.Errorf("expected success=true, got %v", wdResp["success"])
	}

	// POST Save Watchdog Settings
	savePayload := map[string]interface{}{
		"action": "save_settings",
		"settings": config.WatchcatConfig{
			Enabled:           1,
			CheckInterval:     15,
			FailThreshold:     3,
			Cooldown:          30,
			Tier1Enabled:      1,
			Tier2Enabled:      1,
			Tier3Enabled:      0,
			Tier4Enabled:      1,
			MaxRebootsPerHour: 2,
		},
	}
	bodySave, _ := json.Marshal(savePayload)
	reqSave := httptest.NewRequest(http.MethodPost, "/api/monitoring/watchdog", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandleWatchdog(wSave, reqSave)

	if wSave.Code != http.StatusOK {
		t.Fatalf("HandleWatchdog POST returned %d, want 200", wSave.Code)
	}

	// Verify persistence in config
	cfg := cfgMgr.Get().Watchcat
	if cfg.Enabled != 1 || cfg.FailThreshold != 3 || cfg.MaxRebootsPerHour != 2 {
		t.Errorf("persisted config mismatch: %+v", cfg)
	}
}

// 6. Alerts Handler Tests
func TestAlertsHandler(t *testing.T) {
	h := NewAlertsHandler()

	// GET Alerts
	reqGet := httptest.NewRequest(http.MethodGet, "/api/monitoring/alerts", nil)
	wGet := httptest.NewRecorder()
	h.HandleAlerts(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleAlerts GET returned %d, want 200", wGet.Code)
	}

	// POST Alerts Configuration
	savePayload := map[string]interface{}{
		"sms": map[string]interface{}{
			"enabled":         true,
			"recipient_phone": "+628123456789",
		},
		"email": map[string]interface{}{
			"enabled":      true,
			"sender_email": "modem@example.com",
		},
	}
	bodySave, _ := json.Marshal(savePayload)
	reqSave := httptest.NewRequest(http.MethodPost, "/api/monitoring/alerts", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandleAlerts(wSave, reqSave)

	if wSave.Code != http.StatusOK {
		t.Fatalf("HandleAlerts POST returned %d, want 200", wSave.Code)
	}
}

// 7. Language Packs Handler Tests
func TestLanguagePacksHandler(t *testing.T) {
	tmpPacksDir := filepath.Join(t.TempDir(), "language-packs")
	h := &LanguagePacksHandler{
		packsDir: tmpPacksDir,
		installState: LanguagePackInstallState{
			State:    "idle",
			Progress: 0,
		},
	}

	// List - empty initially
	reqList := httptest.NewRequest(http.MethodGet, "/api/system/language-packs/list", nil)
	wList := httptest.NewRecorder()
	h.List(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("List returned %d, want 200", wList.Code)
	}

	// Install pack "id"
	bodyInstall, _ := json.Marshal(map[string]string{"code": "id", "url": "https://example.com/id.json"})
	reqInstall := httptest.NewRequest(http.MethodPost, "/api/system/language-packs/install", bytes.NewBuffer(bodyInstall))
	wInstall := httptest.NewRecorder()
	h.Install(wInstall, reqInstall)
	if wInstall.Code != http.StatusOK {
		t.Fatalf("Install returned %d, want 200", wInstall.Code)
	}

	// Install Status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/system/language-packs/install-status", nil)
	wStatus := httptest.NewRecorder()
	h.InstallStatus(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Errorf("InstallStatus returned %d, want 200", wStatus.Code)
	}

	// List - should now contain "id"
	wListAfter := httptest.NewRecorder()
	h.List(wListAfter, reqList)
	var listResp map[string]interface{}
	_ = json.NewDecoder(wListAfter.Body).Decode(&listResp)
	packs := listResp["packs"].([]interface{})
	if len(packs) != 1 {
		t.Fatalf("expected 1 installed pack, got %d", len(packs))
	}

	// Remove pack "id"
	bodyRemove, _ := json.Marshal(map[string]string{"code": "id"})
	reqRemove := httptest.NewRequest(http.MethodPost, "/api/system/language-packs/remove", bytes.NewBuffer(bodyRemove))
	wRemove := httptest.NewRecorder()
	h.Remove(wRemove, reqRemove)
	if wRemove.Code != http.StatusOK {
		t.Fatalf("Remove returned %d, want 200", wRemove.Code)
	}
}

// 8. Data Usage Handler Tests
func TestDataUsageHandler(t *testing.T) {
	h := NewDataUsageHandler()

	// GET Data Usage
	reqGet := httptest.NewRequest(http.MethodGet, "/api/network/data-usage", nil)
	wGet := httptest.NewRecorder()
	h.GetDataUsed(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetDataUsed returned %d, want 200", wGet.Code)
	}

	var dataResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&dataResp)
	if dataResp["success"] != true {
		t.Errorf("expected success=true, got %v", dataResp["success"])
	}

	// POST Reset Data Usage
	bodyReset, _ := json.Marshal(map[string]string{"action": "reset"})
	reqReset := httptest.NewRequest(http.MethodPost, "/api/network/data-usage/reset", bytes.NewBuffer(bodyReset))
	wReset := httptest.NewRecorder()
	h.ResetDataUsed(wReset, reqReset)

	if wReset.Code != http.StatusOK {
		t.Fatalf("ResetDataUsed returned %d, want 200", wReset.Code)
	}
}
