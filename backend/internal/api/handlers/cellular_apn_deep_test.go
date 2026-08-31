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

func TestCellularAPN_DeepBranches(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)

	apnH := NewCellularApnHandler(eng, cfgMgr)

	// 1. Save with quotes in APN (should fail)
	badAPN := `test"apn`
	bodyQuote, _ := json.Marshal(ApnSavePayload{
		Action: "save",
		Apn:    &badAPN,
	})
	reqQuote := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodyQuote))
	wQuote := httptest.NewRecorder()
	apnH.SaveAPN(wQuote, reqQuote)
	if wQuote.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for APN with double-quotes, got %d", wQuote.Code)
	}

	// 2. Save with invalid PDP type
	goodAPN := "internet.custom"
	badPDP := "invalid_pdp"
	bodyBadPDP, _ := json.Marshal(ApnSavePayload{
		Action:  "save",
		Apn:     &goodAPN,
		PdpType: &badPDP,
	})
	reqBadPDP := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodyBadPDP))
	wBadPDP := httptest.NewRecorder()
	apnH.SaveAPN(wBadPDP, reqBadPDP)
	if wBadPDP.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid PDP type, got %d", wBadPDP.Code)
	}

	// 3. Save with CHAP auth
	chapAuth := "chap"
	user := "chapuser"
	pass := "chappass"
	pdpValid := "ipv4v6"
	mock.SetResponse(`AT+CGDCONT=1,"IPV4V6","internet.custom"`, "OK")
	mock.SetResponse(`AT+CGAUTH=1,2,"chapuser","chappass"`, "OK")
	mock.SetResponse("AT+CGATT=0", "OK")
	mock.SetResponse("AT+CGATT=1", "OK")

	bodyChap, _ := json.Marshal(ApnSavePayload{
		Action:   "save",
		Apn:      &goodAPN,
		PdpType:  &pdpValid,
		AuthType: &chapAuth,
		Username: &user,
		Password: &pass,
	})
	reqChap := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodyChap))
	wChap := httptest.NewRecorder()
	apnH.SaveAPN(wChap, reqChap)
	if wChap.Code != http.StatusOK {
		t.Fatalf("Save APN with CHAP returned %d, want 200: %s", wChap.Code, wChap.Body.String())
	}

	// 4. Save with profile name sidecar
	profName := "Office APN"
	bodyName, _ := json.Marshal(ApnSavePayload{
		Action: "save",
		Cid:    2,
		Apn:    &goodAPN,
		Name:   &profName,
	})
	mock.SetResponse(`AT+CGDCONT=2,"IPV4V6","internet.custom"`, "OK")
	mock.SetResponse("AT+CGAUTH=2,0,\"\",\"", "OK")
	mock.SetResponse("AT+CGATT=0", "OK")
	mock.SetResponse("AT+CGATT=1", "OK")

	reqName := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodyName))
	wName := httptest.NewRecorder()
	apnH.SaveAPN(wName, reqName)
	if wName.Code != http.StatusOK {
		t.Fatalf("Save APN with Name returned %d, want 200", wName.Code)
	}

	// 5. APN classifier tests
	if classifyApnType("ims") != "ims" {
		t.Errorf("expected classifyApnType('ims') == 'ims'")
	}
	if classifyApnType("sos") != "emergency" {
		t.Errorf("expected classifyApnType('sos') == 'emergency'")
	}
	if classifyApnType("general") != "" {
		t.Errorf("expected classifyApnType('general') == ''")
	}

	// 6. parseActiveCidFromCGCONTRDP
	contrdpRaw := `+CGCONTRDP: 1,5,"internet","10.0.0.1","10.0.0.2"`
	cid := parseActiveCidFromCGCONTRDP(contrdpRaw)
	if cid != 1 {
		t.Errorf("expected parseActiveCidFromCGCONTRDP == 1, got %d", cid)
	}
}
