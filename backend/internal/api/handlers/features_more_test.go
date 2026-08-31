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

func setupTestRouterEngine(t *testing.T) (*atengine.MockTransport, *atengine.Engine, *config.Manager) {
	t.Helper()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() { _ = eng.Close() })

	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	return mock, eng, cfgMgr
}

// 1. Speedtest Handler Tests
func TestSpeedtestHandler(t *testing.T) {
	h := NewSpeedtestHandler()

	// CheckAvailable
	reqCheck := httptest.NewRequest(http.MethodGet, "/api/diagnostics/speedtest/check", nil)
	wCheck := httptest.NewRecorder()
	h.CheckAvailable(wCheck, reqCheck)
	if wCheck.Code != http.StatusOK {
		t.Fatalf("CheckAvailable returned %d, want 200", wCheck.Code)
	}

	// Status when idle
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/diagnostics/speedtest/status", nil)
	wStatus := httptest.NewRecorder()
	h.GetStatus(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("GetStatus returned %d, want 200", wStatus.Code)
	}

	// Stop when idle
	reqStop := httptest.NewRequest(http.MethodPost, "/api/diagnostics/speedtest/stop", nil)
	wStop := httptest.NewRecorder()
	h.StopTest(wStop, reqStop)
	if wStop.Code != http.StatusOK {
		t.Fatalf("StopTest returned %d, want 200", wStop.Code)
	}
}

// 2. Video Optimizer / DPI Handler Tests
func TestVideoOptimizerHandler(t *testing.T) {
	h := NewVideoOptimizerHandler()

	// GET status
	reqGet := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer", nil)
	wGet := httptest.NewRecorder()
	h.HandleGet(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleGet returned %d, want 200", wGet.Code)
	}

	// GET verify status
	reqVerify := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer?action=verify_status", nil)
	wVerify := httptest.NewRecorder()
	h.HandleGet(wVerify, reqVerify)
	if wVerify.Code != http.StatusOK {
		t.Fatalf("HandleGet verify_status returned %d, want 200", wVerify.Code)
	}

	// POST bad action
	bodyBad, _ := json.Marshal(map[string]string{"action": "unknown"})
	reqBad := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodyBad))
	wBad := httptest.NewRecorder()
	h.HandlePost(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d", wBad.Code)
	}

	// POST save
	enabled := true
	bodySave, _ := json.Marshal(VideoOptimizerSavePayload{
		Action:  "save",
		Enabled: &enabled,
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandlePost(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("HandlePost save returned %d, want 200", wSave.Code)
	}

	// POST save_masquerade
	bodyMasq, _ := json.Marshal(VideoOptimizerSavePayload{
		Action:    "save_masquerade",
		Enabled:   &enabled,
		SNIDomain: "speedtest.net",
	})
	reqMasq := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodyMasq))
	wMasq := httptest.NewRecorder()
	h.HandlePost(wMasq, reqMasq)
	if wMasq.Code != http.StatusOK {
		t.Fatalf("HandlePost save_masquerade returned %d, want 200", wMasq.Code)
	}
}

// 3. Frequency Calculator Handler Tests
func TestFrequencyCalculatorHandler(t *testing.T) {
	h := NewFrequencyCalculatorHandler()

	// LTE calculation (EARFCN 1675 -> Band 3, ~1842.5 MHz)
	reqLTE := httptest.NewRequest(http.MethodGet, "/api/cellular/frequency/calculate?tech=LTE&channel=1675", nil)
	wLTE := httptest.NewRecorder()
	h.Calculate(wLTE, reqLTE)
	if wLTE.Code != http.StatusOK {
		t.Fatalf("Calculate LTE returned %d, want 200", wLTE.Code)
	}
	var resLTE FrequencyCalcResult
	_ = json.NewDecoder(wLTE.Body).Decode(&resLTE)
	if len(resLTE.MatchingBands) == 0 || resLTE.MatchingBands[0].Band != "B3" {
		t.Errorf("expected Band B3 in matching bands, got %+v", resLTE.MatchingBands)
	}

	// NR 5G calculation (ARFCN 627392 -> n77/n78, ~3410.88 MHz)
	reqNR := httptest.NewRequest(http.MethodGet, "/api/cellular/frequency/calculate?tech=NR5G&arfcn=627392", nil)
	wNR := httptest.NewRecorder()
	h.Calculate(wNR, reqNR)
	if wNR.Code != http.StatusOK {
		t.Fatalf("Calculate NR returned %d, want 200", wNR.Code)
	}
	var resNR FrequencyCalcResult
	_ = json.NewDecoder(wNR.Body).Decode(&resNR)
	foundN78 := false
	for _, m := range resNR.MatchingBands {
		if m.Band == "n78" {
			foundN78 = true
			break
		}
	}
	if !foundN78 {
		t.Errorf("expected Band n78 in matching bands, got %+v", resNR.MatchingBands)
	}

	// Invalid ARFCN
	reqBad := httptest.NewRequest(http.MethodGet, "/api/cellular/frequency/calculate?channel=abc", nil)
	wBad := httptest.NewRecorder()
	h.Calculate(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid channel, got %d", wBad.Code)
	}
}

// 4. Health Check Handler Tests
func TestHealthCheckHandler(t *testing.T) {
	mock, eng, _ := setupTestRouterEngine(t)
	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 1*time.Second)

	h := NewHealthCheckHandler(eng, poller, identity)

	// Run health check
	mock.SetResponse("AT", "OK")
	mock.SetResponse("AT+CPIN?", "+CPIN: READY\r\nOK")
	reqRun := httptest.NewRequest(http.MethodPost, "/api/diagnostics/health/run", nil)
	wRun := httptest.NewRecorder()
	h.Run(wRun, reqRun)
	if wRun.Code != http.StatusOK {
		t.Fatalf("Run returned %d, want 200", wRun.Code)
	}

	time.Sleep(100 * time.Millisecond)

	// Status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/diagnostics/health/status", nil)
	wStatus := httptest.NewRecorder()
	h.Status(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("Status returned %d, want 200", wStatus.Code)
	}
}

// 5. Tower Schedule Handler Tests
func TestTowerScheduleHandler(t *testing.T) {
	mock, eng, _ := setupTestRouterEngine(t)
	h := NewTowerScheduleHandler(eng)

	// Status
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="save_ctrl"`, `+QNWLOCK: "save_ctrl",0,0`+"\r\nOK")
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/tower/status", nil)
	wStatus := httptest.NewRecorder()
	h.Status(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("Status returned %d, want 200", wStatus.Code)
	}

	// FailoverStatus
	reqFO := httptest.NewRequest(http.MethodGet, "/api/cellular/tower/failover-status", nil)
	wFO := httptest.NewRecorder()
	h.FailoverStatus(wFO, reqFO)
	if wFO.Code != http.StatusOK {
		t.Fatalf("FailoverStatus returned %d, want 200", wFO.Code)
	}

	// Settings
	persist := true
	bodySettings, _ := json.Marshal(TowerSettingsRequest{
		Persist: &persist,
	})
	mock.SetResponse(`AT+QNWLOCK="save_ctrl",1`, "OK")
	reqSettings := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/settings", bytes.NewBuffer(bodySettings))
	wSettings := httptest.NewRecorder()
	h.Settings(wSettings, reqSettings)
	if wSettings.Code != http.StatusOK {
		t.Fatalf("Settings returned %d, want 200", wSettings.Code)
	}

	// Schedule
	enabled := true
	bodySched, _ := json.Marshal(TowerScheduleRequest{
		Enabled:   &enabled,
		StartTime: "08:00",
		EndTime:   "22:00",
		Days:      []int{1, 2, 3, 4, 5},
	})
	reqSched := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/schedule", bytes.NewBuffer(bodySched))
	wSched := httptest.NewRecorder()
	h.Schedule(wSched, reqSched)
	if wSched.Code != http.StatusOK {
		t.Fatalf("Schedule returned %d, want 200", wSched.Code)
	}
}

// 6. SIM Registry Handler Tests
func TestSimRegistryHandler(t *testing.T) {
	h := NewSimRegistryHandler()

	// GET Registry
	reqGet := httptest.NewRequest(http.MethodGet, "/api/system/sim-registry", nil)
	wGet := httptest.NewRecorder()
	h.HandleRegistry(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleRegistry GET returned %d, want 200", wGet.Code)
	}

	// POST Save / Upsert SIM
	bodySave, _ := json.Marshal(map[string]interface{}{
		"action":     "upsert",
		"iccid":      "8986000000000000001",
		"carrier":    "Telkomsel",
		"label":      "Primary SIM",
		"profile_id": "prof-1",
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandleRegistry(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("HandleRegistry POST returned %d, want 200", wSave.Code)
	}
}

// 7. Scenarios Handler Tests
func TestScenariosHandler(t *testing.T) {
	tmpDir := t.TempDir()
	mock, eng, _ := setupTestRouterEngine(t)

	h := NewScenarioHandler(eng)
	h.SetStoragePaths(filepath.Join(tmpDir, "scenarios"), filepath.Join(tmpDir, "active_scenario"))

	// 1. List Scenarios
	reqList := httptest.NewRequest(http.MethodGet, "/api/cellular/scenarios", nil)
	wList := httptest.NewRecorder()
	h.List(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("List returned %d, want 200", wList.Code)
	}

	// 2. Save Scenario
	newScen := ScenarioDefinition{
		Name:     "Gaming Low Latency",
		ModePref: "NR5G",
		NRSABands: []string{"78"},
	}
	bodySave, _ := json.Marshal(newScen)
	reqSave := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.Save(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("Save returned %d, want 200: %s", wSave.Code, wSave.Body.String())
	}

	var saveResp map[string]interface{}
	_ = json.NewDecoder(wSave.Body).Decode(&saveResp)
	scenID := saveResp["id"].(string)

	// 3. Active Scenario
	reqActive := httptest.NewRequest(http.MethodGet, "/api/cellular/scenarios/active", nil)
	wActive := httptest.NewRecorder()
	h.Active(wActive, reqActive)
	if wActive.Code != http.StatusOK {
		t.Fatalf("Active returned %d, want 200", wActive.Code)
	}

	// 4. Activate Scenario
	mock.SetResponse(`AT+QNWPREFCFG="mode_pref",NR5G`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="nr5g_band",78`, "OK")

	bodyAct, _ := json.Marshal(map[string]string{"id": scenID})
	reqAct := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios/activate", bytes.NewBuffer(bodyAct))
	wAct := httptest.NewRecorder()
	h.Activate(wAct, reqAct)
	if wAct.Code != http.StatusOK {
		t.Fatalf("Activate returned %d, want 200: %s", wAct.Code, wAct.Body.String())
	}

	// 5. Delete Scenario
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/cellular/scenarios/"+scenID, nil)
	wDel := httptest.NewRecorder()
	h.Delete(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("Delete returned %d, want 200", wDel.Code)
	}
}

// 8. Band Failover Handler Tests
func TestBandFailoverHandler(t *testing.T) {
	h := NewBandFailoverHandler()

	// Status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/bands/failover", nil)
	wStatus := httptest.NewRecorder()
	h.Status(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("Status returned %d, want 200", wStatus.Code)
	}

	// Toggle
	bodyToggle, _ := json.Marshal(map[string]interface{}{"enabled": true})
	reqToggle := httptest.NewRequest(http.MethodPost, "/api/cellular/bands/failover", bytes.NewBuffer(bodyToggle))
	wToggle := httptest.NewRecorder()
	h.Toggle(wToggle, reqToggle)
	if wToggle.Code != http.StatusOK {
		t.Fatalf("Toggle returned %d, want 200", wToggle.Code)
	}
}

// 9. SMS Forwarding Handler Tests
func TestSMSForwardingHandler(t *testing.T) {
	mock, eng, cfgMgr := setupTestRouterEngine(t)
	h := NewSMSForwardingHandler(eng, cfgMgr)

	// GetSettings
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cellular/sms/forwarding", nil)
	wGet := httptest.NewRecorder()
	h.GetSettings(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GetSettings returned %d, want 200", wGet.Code)
	}

	// HandleAction - save
	bodySave, _ := json.Marshal(map[string]interface{}{
		"action":       "save",
		"enabled":      true,
		"target_phone": "+628123456789",
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/api/cellular/sms/forwarding", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandleAction(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("HandleAction save returned %d, want 200", wSave.Code)
	}

	// HandleAction - clear_failures
	bodyClear, _ := json.Marshal(map[string]string{"action": "clear_failures"})
	reqClear := httptest.NewRequest(http.MethodPost, "/api/cellular/sms/forwarding", bytes.NewBuffer(bodyClear))
	wClear := httptest.NewRecorder()
	h.HandleAction(wClear, reqClear)
	if wClear.Code != http.StatusOK {
		t.Fatalf("HandleAction clear_failures returned %d, want 200", wClear.Code)
	}
	_ = mock
}
