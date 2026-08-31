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

func newTestCellularContext(t *testing.T) (*atengine.MockTransport, *atengine.Engine, *config.Manager, *telemetry.Poller) {
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

	return mock, eng, cfgMgr, poller
}

// 1. Cellular IMEI Handler Tests
func TestCellularIMEIHandler(t *testing.T) {
	tmpDir := t.TempDir()
	origBackupPath := imeiBackupPath
	origFlagPath := imeiRebootPendingFlag
	imeiBackupPath = filepath.Join(tmpDir, "imei_backup.json")
	imeiRebootPendingFlag = filepath.Join(tmpDir, "qm_imei_reboot_pending")
	t.Cleanup(func() {
		imeiBackupPath = origBackupPath
		imeiRebootPendingFlag = origFlagPath
	})

	mock, eng, cfgMgr, poller := newTestCellularContext(t)
	h := NewCellularImeiHandler(eng, poller, cfgMgr)

	// GET IMEI
	mock.SetResponse("AT+GSN", "860123456789010\r\nOK")
	req := httptest.NewRequest(http.MethodGet, "/api/cellular/imei", nil)
	w := httptest.NewRecorder()
	h.GetIMEI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetIMEI returned %d, want 200", w.Code)
	}

	var getResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if getResp["current_imei"] != "860123456789010" {
		t.Errorf("expected imei '860123456789010', got %v", getResp["current_imei"])
	}

	// POST Save IMEI - invalid body
	reqBad := httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBufferString(`invalid-json`))
	wBad := httptest.NewRecorder()
	h.SaveIMEI(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	// POST Save IMEI - invalid length
	bodyInvalidLen, _ := json.Marshal(map[string]string{"imei": "12345"})
	reqInvalidLen := httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodyInvalidLen))
	wInvalidLen := httptest.NewRecorder()
	h.SaveIMEI(wInvalidLen, reqInvalidLen)
	if wInvalidLen.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short IMEI, got %d", wInvalidLen.Code)
	}

	// POST Save IMEI - invalid Luhn check (15 digits)
	bodyBadLuhn, _ := json.Marshal(map[string]string{"imei": "860123456789012"})
	reqBadLuhn := httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodyBadLuhn))
	wBadLuhn := httptest.NewRecorder()
	h.SaveIMEI(wBadLuhn, reqBadLuhn)
	if wBadLuhn.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid Luhn checksum, got %d", wBadLuhn.Code)
	}

	// POST Save IMEI - valid 14-digit IMEI (auto-computes Luhn 15th digit)
	prefix := "86012345678901"
	checkDigit, _ := CalculateLuhnCheckDigit(prefix)
	expectedFullIMEI := prefix + string('0'+byte(checkDigit))

	mock.SetResponse("AT+EGMR=1,7,\""+expectedFullIMEI+"\"", "OK")
	body14, _ := json.Marshal(ImeiSavePayload{
		Action: "set_imei",
		Imei:   prefix,
	})
	req14 := httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(body14))
	w14 := httptest.NewRecorder()
	h.SaveIMEI(w14, req14)

	if w14.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid 14-digit IMEI, got %d: %s", w14.Code, w14.Body.String())
	}

	var resp14 map[string]interface{}
	_ = json.NewDecoder(w14.Body).Decode(&resp14)
	if resp14["imei"] != expectedFullIMEI {
		t.Errorf("expected applied IMEI %s, got %v", expectedFullIMEI, resp14["imei"])
	}
}

// 2. Cellular APN Handler Tests
func TestCellularAPNHandler(t *testing.T) {
	tmpDir := t.TempDir()
	origApnSetting := apnSettingPath
	origApnNames := apnNamesPath
	apnSettingPath = filepath.Join(tmpDir, "apn_setting.json")
	apnNamesPath = filepath.Join(tmpDir, "apn_names.json")
	t.Cleanup(func() {
		apnSettingPath = origApnSetting
		apnNamesPath = origApnNames
	})

	mock, eng, cfgMgr, _ := newTestCellularContext(t)
	h := NewCellularApnHandler(eng, cfgMgr)

	// Mock AT responses for APN listing
	mock.SetResponse("AT+CGDCONT?", `+CGDCONT: 1,"IPV4V6","internet","0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",0,0,0,0`+"\r\nOK")
	mock.SetResponse("AT+CGAUTH?", `+CGAUTH: 1,1,"user123","pass123"`+"\r\nOK")

	// GET APN Profiles
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cellular/apn", nil)
	wGet := httptest.NewRecorder()
	h.GetAPN(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetAPN returned %d, want 200", wGet.Code)
	}

	var getResp map[string]interface{}
	if err := json.NewDecoder(wGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode APN response: %v", err)
	}
	if getResp["success"] != true {
		t.Errorf("expected success=true, got %v", getResp["success"])
	}

	// POST Save APN - invalid JSON
	reqBad := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBufferString(`bad`))
	wBad := httptest.NewRecorder()
	h.SaveAPN(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	// POST Save APN - save action for Profile 1
	apnStr := "custom.apn"
	pdpStr := "ipv4v6"
	authStr := "pap"
	userStr := "testuser"
	passStr := "testpass"
	cidVal := 1

	mock.SetResponse(`AT+CGDCONT=1,"IPV4V6","custom.apn"`, "OK")
	mock.SetResponse(`AT+CGAUTH=1,1,"testuser","testpass"`, "OK")
	mock.SetResponse("AT+CGATT=0", "OK")
	mock.SetResponse("AT+CGATT=1", "OK")

	savePayload := ApnSavePayload{
		Action:   "save",
		Cid:      cidVal,
		Apn:      &apnStr,
		PdpType:  &pdpStr,
		AuthType: &authStr,
		Username: &userStr,
		Password: &passStr,
	}
	bodySave, _ := json.Marshal(savePayload)
	reqSave := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.SaveAPN(wSave, reqSave)

	if wSave.Code != http.StatusOK {
		t.Fatalf("SaveAPN returned %d, want 200: %s", wSave.Code, wSave.Body.String())
	}
}

// 3. Cellular MBN Handler Tests
func TestCellularMBNHandler(t *testing.T) {
	mock, eng, _, _ := newTestCellularContext(t)
	h := NewCellularMbnHandler(eng)

	// Mock MBN queries
	mock.SetResponse(`AT+QMBNCFG="autosel"`, `+QMBNCFG: "AutoSel",1`+"\r\nOK")
	mock.SetResponse(`AT+QMBNCFG="list"`, `+QMBNCFG: "List",0,1,1,"ROW_Commercial",0x08010801,202305091`+"\r\nOK")

	// GET MBN
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cellular/mbn", nil)
	wGet := httptest.NewRecorder()
	h.GetMBN(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetMBN returned %d, want 200", wGet.Code)
	}

	var mbnResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&mbnResp)
	if mbnResp["auto_sel"].(float64) != 1 {
		t.Errorf("expected auto_sel=1, got %v", mbnResp["auto_sel"])
	}

	// POST Save MBN - apply_profile action
	mock.SetResponse(`AT+QMBNCFG="select","ROW_Commercial"`, "OK")
	mock.SetResponse(`AT+QMBNCFG="autosel",0`, "OK")

	bodyApply, _ := json.Marshal(MbnSavePayload{
		Action:      "apply_profile",
		ProfileName: "ROW_Commercial",
	})
	reqApply := httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyApply))
	wApply := httptest.NewRecorder()
	h.SaveMBN(wApply, reqApply)

	if wApply.Code != http.StatusOK {
		t.Fatalf("expected 200 for apply_profile, got %d", wApply.Code)
	}

	// POST Save MBN - auto_sel action
	mock.SetResponse(`AT+QMBNCFG="autosel",1`, "OK")
	autoSelVal := 1
	bodyAuto, _ := json.Marshal(MbnSavePayload{
		Action:  "auto_sel",
		AutoSel: &autoSelVal,
	})
	reqAuto := httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyAuto))
	wAuto := httptest.NewRecorder()
	h.SaveMBN(wAuto, reqAuto)

	if wAuto.Code != http.StatusOK {
		t.Fatalf("expected 200 for auto_sel, got %d", wAuto.Code)
	}

	// POST Save MBN - unknown action
	bodyUnknown, _ := json.Marshal(MbnSavePayload{Action: "invalid"})
	reqUnknown := httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyUnknown))
	wUnknown := httptest.NewRecorder()
	h.SaveMBN(wUnknown, reqUnknown)
	if wUnknown.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d", wUnknown.Code)
	}
}

// 4. Cellular FPLMN Handler Tests
func TestCellularFPLMNHandler(t *testing.T) {
	mock, eng, _, _ := newTestCellularContext(t)
	h := NewCellularFplmnHandler(eng)

	// Mock CRSM read
	mock.SetResponse("AT+CRSM=192,28539,0,0,15", `+CRSM: 144,0,"0000000C"`+"\r\nOK")
	mock.SetResponse("AT+CRSM=176,28539,0,0,12", `+CRSM: 144,0,"05F51005F520FFFFFFFFFFFF"`+"\r\nOK")

	// GET FPLMN
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cellular/fplmn", nil)
	wGet := httptest.NewRecorder()
	h.GetFPLMN(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetFPLMN returned %d, want 200", wGet.Code)
	}

	var fplmnResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&fplmnResp)
	entries := fplmnResp["fplmns"].([]interface{})
	if len(entries) != 2 {
		t.Errorf("expected 2 FPLMN entries, got %d", len(entries))
	}

	// POST Clear FPLMN
	mock.SetResponse("AT+CRSM=192,28539,0,0,15", `+CRSM: 144,0,"0000000C"`+"\r\nOK")
	mock.SetResponse("AT+CRSM=214,28539,0,0,12,\"FFFFFFFFFFFFFFFFFFFFFFFF\"", `+CRSM: 144,0,""`+"\r\nOK")
	reqClear := httptest.NewRequest(http.MethodPost, "/api/cellular/fplmn/clear", nil)
	wClear := httptest.NewRecorder()
	h.ClearFPLMN(wClear, reqClear)

	if wClear.Code != http.StatusOK {
		t.Fatalf("ClearFPLMN returned %d, want 200: %s", wClear.Code, wClear.Body.String())
	}
}

// 5. Cellular Priority (RAT Acquisition Order) Handler Tests
func TestCellularPriorityHandler(t *testing.T) {
	mock, eng, _, _ := newTestCellularContext(t)
	h := NewNetworkPriorityHandler(eng)

	// GET Priority
	mock.SetResponse(`AT+QNWPREFCFG="rat_acq_order"`, `+QNWPREFCFG: "rat_acq_order",NR5G:LTE:WCDMA`+"\r\nOK")
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cellular/priority", nil)
	wGet := httptest.NewRecorder()
	h.GetPriority(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetPriority returned %d, want 200", wGet.Code)
	}

	var pResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&pResp)
	if pResp["rat_acq_order"] != "NR5G:LTE:WCDMA" {
		t.Errorf("expected 'NR5G:LTE:WCDMA', got %v", pResp["rat_acq_order"])
	}

	// POST Set Priority - valid
	mock.SetResponse(`AT+QNWPREFCFG="rat_acq_order",LTE:NR5G`, "OK")
	bodyValid, _ := json.Marshal(SetPriorityPayload{Order: "LTE:NR5G"})
	reqValid := httptest.NewRequest(http.MethodPost, "/api/cellular/priority", bytes.NewBuffer(bodyValid))
	wValid := httptest.NewRecorder()
	h.SetPriority(wValid, reqValid)

	if wValid.Code != http.StatusOK {
		t.Fatalf("SetPriority returned %d, want 200", wValid.Code)
	}

	// POST Set Priority - invalid RAT name
	bodyInvalid, _ := json.Marshal(SetPriorityPayload{Order: "LTE:INVALID_RAT"})
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/cellular/priority", bytes.NewBuffer(bodyInvalid))
	wInvalid := httptest.NewRecorder()
	h.SetPriority(wInvalid, reqInvalid)

	if wInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid RAT, got %d", wInvalid.Code)
	}
}
