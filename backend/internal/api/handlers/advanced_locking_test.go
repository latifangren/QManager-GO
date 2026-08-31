package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
)

func TestParseFrequencyStatus(t *testing.T) {
	raw := `
+QNWCFG: "lte_earfcn_lock",2,1675:1800
+QNWCFG: "nr5g_earfcn_lock",1,630000:30
OK
`
	state := ParseFrequencyStatus(raw)
	if !state.LTELocked {
		t.Errorf("expected LTE locked to be true")
	}
	if len(state.LTEEntries) != 2 || state.LTEEntries[0].EARFCN != 1675 || state.LTEEntries[1].EARFCN != 1800 {
		t.Errorf("unexpected LTE entries: %+v", state.LTEEntries)
	}
	if !state.NRLocked {
		t.Errorf("expected NR locked to be true")
	}
	if len(state.NREntries) != 1 || state.NREntries[0].ARFCN != 630000 || state.NREntries[0].SCS != 30 {
		t.Errorf("unexpected NR entries: %+v", state.NREntries)
	}

	// Test unlocked state
	rawUnlocked := `
+QNWCFG: "lte_earfcn_lock",0
+QNWCFG: "nr5g_earfcn_lock",0
OK
`
	stateUnlocked := ParseFrequencyStatus(rawUnlocked)
	if stateUnlocked.LTELocked || len(stateUnlocked.LTEEntries) != 0 {
		t.Errorf("expected LTE unlocked, got: %+v", stateUnlocked.LTEEntries)
	}
	if stateUnlocked.NRLocked || len(stateUnlocked.NREntries) != 0 {
		t.Errorf("expected NR unlocked, got: %+v", stateUnlocked.NREntries)
	}
}

func TestFrequencyLockHandler_Flow(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock";+QNWCFG="nr5g_earfcn_lock"`, `+QNWCFG: "lte_earfcn_lock",0`+"\r\n"+`+QNWCFG: "nr5g_earfcn_lock",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock",1,1675`, "OK")
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock",0`, "OK")

	eng := atengine.NewEngine(mock)
	h := NewFrequencyLockHandler(eng)

	// 1. Check Status
	req := httptest.NewRequest("GET", "/api/v1/cellular/frequency", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from Status, got %d", w.Code)
	}
	var resp FrequencyStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil || !resp.Success {
		t.Fatalf("unexpected response decoding: %+v", resp)
	}

	// 2. Lock LTE
	lockBody := `{"rat":"lte","action":"lock","lte_entries":[{"earfcn":1675}]}`
	reqLock := httptest.NewRequest("POST", "/api/v1/cellular/frequency/lock", bytes.NewReader([]byte(lockBody)))
	reqLock.Header.Set("Content-Type", "application/json")
	wLock := httptest.NewRecorder()
	h.Lock(wLock, reqLock)

	if wLock.Code != http.StatusOK {
		t.Fatalf("expected 200 from Lock, got %d", wLock.Code)
	}

	// 3. Unlock LTE
	unlockBody := `{"rat":"lte","action":"unlock"}`
	reqUnlock := httptest.NewRequest("POST", "/api/v1/cellular/frequency/lock", bytes.NewReader([]byte(unlockBody)))
	reqUnlock.Header.Set("Content-Type", "application/json")
	wUnlock := httptest.NewRecorder()
	h.Lock(wUnlock, reqUnlock)

	if wUnlock.Code != http.StatusOK {
		t.Fatalf("expected 200 from Unlock, got %d", wUnlock.Code)
	}
}

func TestTowerScheduleHandler_Flow(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",1,1675,218`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",1,630000,500,30,78`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="save_ctrl"`, `+QNWLOCK: "save_ctrl",1,1`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="save_ctrl",1`, "OK")

	eng := atengine.NewEngine(mock)
	h := NewTowerScheduleHandler(eng)

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "tower_lock.json")
	h.SetConfigPath(cfgPath)

	// 1. Status
	req := httptest.NewRequest("GET", "/api/v1/cellular/tower/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from Status, got %d", w.Code)
	}

	// 2. Settings update
	setBody := `{"persist":true,"failover_enabled":false,"failover_threshold":25}`
	reqSet := httptest.NewRequest("POST", "/api/v1/cellular/tower/settings", bytes.NewReader([]byte(setBody)))
	wSet := httptest.NewRecorder()
	h.Settings(wSet, reqSet)

	if wSet.Code != http.StatusOK {
		t.Fatalf("expected 200 from Settings, got %d", wSet.Code)
	}

	// 3. Schedule update
	schedBody := `{"enabled":true,"start_time":"09:00","end_time":"18:00","days":[1,2,3,4,5]}`
	reqSched := httptest.NewRequest("POST", "/api/v1/cellular/tower/schedule", bytes.NewReader([]byte(schedBody)))
	wSched := httptest.NewRecorder()
	h.Schedule(wSched, reqSched)

	if wSched.Code != http.StatusOK {
		t.Fatalf("expected 200 from Schedule, got %d", wSched.Code)
	}

	// 4. Failover status
	reqFo := httptest.NewRequest("GET", "/api/v1/cellular/tower/failover-status", nil)
	wFo := httptest.NewRecorder()
	h.FailoverStatus(wFo, reqFo)

	if wFo.Code != http.StatusOK {
		t.Fatalf("expected 200 from FailoverStatus, got %d", wFo.Code)
	}
}

func TestSIMProfileHandler_CRUD(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+CGDCONT=1,"IPV4V6","internet"`, "OK")
	mock.SetResponse(`AT+CGDCONT?;+CGSN;+QCCID;+CGPADDR;+QMAP="WWAN";+QSPN`, `
+CGDCONT: 1,"IPV4V6","internet",,0,0,0,0
860123456789012
+QCCID: 89014103211118510720F
+QSPN: "T-Mobile","TMO","Mint",0,"310260"
+QMAP: "WWAN",1,1,1,"192.168.225.20"
OK
`)

	eng := atengine.NewEngine(mock)
	h := NewSIMProfileHandler(eng)

	tmpDir := t.TempDir()
	profDir := filepath.Join(tmpDir, "profiles")
	actPath := filepath.Join(tmpDir, "active_profile")
	stPath := filepath.Join(tmpDir, "state.json")
	h.SetStoragePaths(profDir, actPath, stPath)

	// 1. Current Settings
	reqCS := httptest.NewRequest("GET", "/api/v1/cellular/profiles/current-settings", nil)
	wCS := httptest.NewRecorder()
	h.CurrentSettings(wCS, reqCS)
	if wCS.Code != http.StatusOK {
		t.Fatalf("expected 200 from CurrentSettings, got %d", wCS.Code)
	}
	var cs CurrentModemSettings
	_ = json.NewDecoder(wCS.Body).Decode(&cs)
	if cs.IMEI != "860123456789012" || cs.ICCID != "89014103211118510720" || cs.MCC != "310" || cs.MNC != "260" {
		t.Errorf("unexpected CurrentSettings: %+v", cs)
	}

	// 2. Save profile
	pData := `{"name":"T-Mobile Plan","settings":{"apn":"fast.t-mobile.com","pdp_type":"IPV4V6","cid":1,"auto_connect":true,"roaming":false}}`
	reqSave := httptest.NewRequest("POST", "/api/v1/cellular/profiles", bytes.NewReader([]byte(pData)))
	wSave := httptest.NewRecorder()
	h.Save(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("expected 200 from Save, got %d: %s", wSave.Code, wSave.Body.String())
	}
	var saveResp struct {
		Success bool       `json:"success"`
		ID      string     `json:"id"`
		Profile SIMProfile `json:"profile"`
	}
	_ = json.NewDecoder(wSave.Body).Decode(&saveResp)
	if !saveResp.Success || saveResp.ID == "" {
		t.Fatalf("expected profile ID, got empty")
	}

	// 3. List profiles
	reqList := httptest.NewRequest("GET", "/api/v1/cellular/profiles", nil)
	wList := httptest.NewRecorder()
	h.List(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 from List, got %d", wList.Code)
	}

	// 4. Apply profile
	applyBody := fmt.Sprintf(`{"profile_id":"%s"}`, saveResp.ID)
	reqApply := httptest.NewRequest("POST", "/api/v1/cellular/profiles/apply", bytes.NewReader([]byte(applyBody)))
	wApply := httptest.NewRecorder()
	h.Apply(wApply, reqApply)
	if wApply.Code != http.StatusOK {
		t.Fatalf("expected 200 from Apply, got %d", wApply.Code)
	}

	// 5. Check Apply status
	reqSt := httptest.NewRequest("GET", "/api/v1/cellular/profiles/apply-status", nil)
	wSt := httptest.NewRecorder()
	h.ApplyStatus(wSt, reqSt)
	if wSt.Code != http.StatusOK {
		t.Fatalf("expected 200 from ApplyStatus, got %d", wSt.Code)
	}

	// 6. Delete profile
	h.WaitForAsync()
	reqDel := httptest.NewRequest("DELETE", "/api/v1/cellular/profiles/"+saveResp.ID, nil)
	wDel := httptest.NewRecorder()
	h.Delete(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 from Delete, got %d", wDel.Code)
	}
	h.WaitForAsync()
}

func TestScenarioHandler_CRUD(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+QNWPREFCFG="mode_pref",NR5G`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="nr5g_band",78:77`, "OK")

	eng := atengine.NewEngine(mock)
	h := NewScenarioHandler(eng)

	tmpDir := t.TempDir()
	scnDir := filepath.Join(tmpDir, "scenarios")
	actPath := filepath.Join(tmpDir, "active_scenario")
	h.SetStoragePaths(scnDir, actPath)

	// 1. List
	reqList := httptest.NewRequest("GET", "/api/v1/cellular/scenarios", nil)
	wList := httptest.NewRecorder()
	h.List(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 from List, got %d", wList.Code)
	}

	// 2. Save Custom Scenario
	customData := `{"name":"5G Ultra","mode_pref":"NR5G","nr_sa_bands":["78","77"]}`
	reqSave := httptest.NewRequest("POST", "/api/v1/cellular/scenarios", bytes.NewReader([]byte(customData)))
	wSave := httptest.NewRecorder()
	h.Save(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("expected 200 from Save, got %d: %s", wSave.Code, wSave.Body.String())
	}
	var saveResp struct {
		Success  bool               `json:"success"`
		ID       string             `json:"id"`
		Scenario ScenarioDefinition `json:"scenario"`
	}
	_ = json.NewDecoder(wSave.Body).Decode(&saveResp)

	// 3. Activate Scenario
	actBody := fmt.Sprintf(`{"scenario_id":"%s"}`, saveResp.ID)
	reqAct := httptest.NewRequest("POST", "/api/v1/cellular/scenarios/activate", bytes.NewReader([]byte(actBody)))
	wAct := httptest.NewRecorder()
	h.Activate(wAct, reqAct)
	if wAct.Code != http.StatusOK {
		t.Fatalf("expected 200 from Activate, got %d", wAct.Code)
	}

	// 4. Check Active
	reqActCheck := httptest.NewRequest("GET", "/api/v1/cellular/scenarios/active", nil)
	wActCheck := httptest.NewRecorder()
	h.Active(wActCheck, reqActCheck)
	if wActCheck.Code != http.StatusOK {
		t.Fatalf("expected 200 from Active, got %d", wActCheck.Code)
	}

	// 5. Delete Scenario
	reqDel := httptest.NewRequest("DELETE", "/api/v1/cellular/scenarios/"+saveResp.ID, nil)
	wDel := httptest.NewRecorder()
	h.Delete(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 from Delete, got %d", wDel.Code)
	}

	// Verify active reset to balanced
	if h.getActiveScenarioID() != "balanced" {
		t.Errorf("expected active scenario to reset to balanced, got %s", h.getActiveScenarioID())
	}
}
