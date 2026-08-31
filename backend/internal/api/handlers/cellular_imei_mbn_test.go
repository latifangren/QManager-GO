package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func TestCellularImeiAndMbn_DeepCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)

	// 1. Cellular IMEI Handler
	imeiH := NewCellularImeiHandler(eng, nil, cfgMgr)

	// 1a. GET IMEI
	mock.SetResponse("AT+GSN", "862104041234567\r\nOK")
	wImeiGet := httptest.NewRecorder()
	imeiH.GetIMEI(wImeiGet, httptest.NewRequest(http.MethodGet, "/api/cellular/imei", nil))
	if wImeiGet.Code != http.StatusOK {
		t.Fatalf("GetIMEI returned %d, want 200", wImeiGet.Code)
	}

	// 1b. POST set_imei with 14 digits (auto calculates 15th Luhn digit)
	valid14 := "86210404123456"
	cd, _ := CalculateLuhnCheckDigit(valid14)
	expected15 := valid14 + string('0'+byte(cd))
	mock.SetResponse(`AT+EGMR=1,7,"`+expected15+`"`, "OK")

	bodySet14, _ := json.Marshal(ImeiSavePayload{
		Action: "set_imei",
		Imei:   valid14,
	})
	wSet14 := httptest.NewRecorder()
	imeiH.SaveIMEI(wSet14, httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodySet14)))
	if wSet14.Code != http.StatusOK {
		t.Fatalf("SaveIMEI set_imei 14 digits returned %d: %s", wSet14.Code, wSet14.Body.String())
	}

	// 1c. POST set_imei invalid checksum
	bodySetBad, _ := json.Marshal(ImeiSavePayload{
		Action: "set_imei",
		Imei:   "111111111111111",
	})
	wSetBad := httptest.NewRecorder()
	imeiH.SaveIMEI(wSetBad, httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodySetBad)))
	if wSetBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid Luhn IMEI, got %d", wSetBad.Code)
	}

	// 1d. POST save_backup
	enabledTrue := true
	bodyBackup, _ := json.Marshal(ImeiSavePayload{
		Action:     "save_backup",
		Enabled:    &enabledTrue,
		BackupImei: expected15,
	})
	wBackup := httptest.NewRecorder()
	imeiH.SaveIMEI(wBackup, httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodyBackup)))
	if wBackup.Code != http.StatusOK {
		t.Fatalf("SaveIMEI save_backup returned %d: %s", wBackup.Code, wBackup.Body.String())
	}

	// 1e. POST unknown action & invalid JSON
	bodyUnknown, _ := json.Marshal(ImeiSavePayload{Action: "unknown_act"})
	wUnknown := httptest.NewRecorder()
	imeiH.SaveIMEI(wUnknown, httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodyUnknown)))
	if wUnknown.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d", wUnknown.Code)
	}

	wBadJSON := httptest.NewRecorder()
	imeiH.SaveIMEI(wBadJSON, httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBufferString("{invalid")))
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}

	// 2. Cellular MBN Handler
	mbnH := NewCellularMbnHandler(eng)

	// 2a. GET MBN
	mock.SetResponse(`AT+QMBNCFG="autosel"`, `+QMBNCFG: "autosel",1`+"\r\nOK")
	mock.SetResponse(`AT+QMBNCFG="list"`, `+QMBNCFG: "list",0,1,1,"ROW_Commercial","0x08010801",20201010
+QMBNCFG: "list",1,0,0,"Telstra_Commercial","0x08010802",20201011
OK`)
	wMbnGet := httptest.NewRecorder()
	mbnH.GetMBN(wMbnGet, httptest.NewRequest(http.MethodGet, "/api/cellular/mbn", nil))
	if wMbnGet.Code != http.StatusOK {
		t.Fatalf("GetMBN returned %d, want 200", wMbnGet.Code)
	}

	// 2b. POST apply_profile
	mock.SetResponse(`AT+QMBNCFG="select","ROW_Commercial"`, "OK")
	mock.SetResponse("AT+CFUN=1,1", "OK")
	bodyApplyProfile, _ := json.Marshal(MbnSavePayload{
		Action:      "apply_profile",
		ProfileName: "ROW_Commercial",
	})
	wApplyProfile := httptest.NewRecorder()
	mbnH.SaveMBN(wApplyProfile, httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyApplyProfile)))
	if wApplyProfile.Code != http.StatusOK {
		t.Fatalf("SaveMBN apply_profile returned %d: %s", wApplyProfile.Code, wApplyProfile.Body.String())
	}

	// 2c. POST auto_sel
	mock.SetResponse(`AT+QMBNCFG="autosel",1`, "OK")
	autoSelVal := 1
	bodyAutoSel, _ := json.Marshal(MbnSavePayload{
		Action:  "auto_sel",
		AutoSel: &autoSelVal,
	})
	wAutoSel := httptest.NewRecorder()
	mbnH.SaveMBN(wAutoSel, httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyAutoSel)))
	if wAutoSel.Code != http.StatusOK {
		t.Fatalf("SaveMBN auto_sel returned %d: %s", wAutoSel.Code, wAutoSel.Body.String())
	}

	// 2d. POST unknown action & missing params
	bodyMbnBad, _ := json.Marshal(MbnSavePayload{Action: "apply_profile", ProfileName: ""})
	wMbnBad := httptest.NewRecorder()
	mbnH.SaveMBN(wMbnBad, httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyMbnBad)))
	if wMbnBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty profile name, got %d", wMbnBad.Code)
	}
}
