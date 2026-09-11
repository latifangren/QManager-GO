package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"qmanager/internal/atengine"
)

func TestValidateLuhnIMEI(t *testing.T) {
	tests := []struct {
		imei  string
		valid bool
	}{
		{"860123456789012", false}, // random non-luhn
		{"862959040123456", false},
		{"867012040123451", false},
		{"12345678901234", false},  // 14 digits
		{"1234567890123456", false}, // 16 digits
		{"86012345678901a", false}, // non-digit
		{"", false},
	}

	for _, tc := range tests {
		got := ValidateLuhnIMEI(tc.imei)
		if got != tc.valid {
			t.Errorf("ValidateLuhnIMEI(%q) = %v; want %v", tc.imei, got, tc.valid)
		}
	}

	// Test prefix check digit calculation + verification
	prefix := "86012345678901"
	checkDigit, ok := CalculateLuhnCheckDigit(prefix)
	if !ok {
		t.Fatalf("CalculateLuhnCheckDigit failed for %s", prefix)
	}
	validImei := prefix + string('0'+byte(checkDigit))
	if !ValidateLuhnIMEI(validImei) {
		t.Errorf("Constructed IMEI %s should pass Luhn validation", validImei)
	}
}

func TestParseMbnList(t *testing.T) {
	raw := `
+QMBNCFG: "List",0,1,1,"ROW_Commercial",0x08010801,202305091
+QMBNCFG: "List",1,0,0,"Commercial-TMO",0x08010101,202211041
+QMBNCFG: "List",2,0,0,"Telstra-Commercial",0x08010501,202301181

OK
`
	profiles := ParseMbnList(raw)
	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}

	p0 := profiles[0]
	if p0.Index != 0 || !p0.Selected || !p0.Activated || p0.Name != "ROW_Commercial" || p0.Version != "0x08010801" || p0.Date != "202305091" {
		t.Errorf("profile 0 parsed incorrectly: %+v", p0)
	}

	p1 := profiles[1]
	if p1.Index != 1 || p1.Selected || p1.Activated || p1.Name != "Commercial-TMO" {
		t.Errorf("profile 1 parsed incorrectly: %+v", p1)
	}
}

func TestParseFplmnHex(t *testing.T) {
	// Sample EF_FPLMN hex data:
	// PLMN 1: 505 01 (Telstra AU) -> 05 F5 10 (MCC: 505, MNC: 01)
	// Byte 1: 05 -> mcc2=0, mcc1=5 -> mcc1=5, mcc2=0
	// Byte 2: F5 -> mnc3=F, mcc3=5 -> mcc=505, mnc3=F
	// Byte 3: 10 -> mnc2=1, mnc1=0 -> mnc=01
	// PLMN 2: 505 02 (Optus AU) -> 05 F5 20
	// Rest empty: FFFFFFFFFFFF
	hex := "05F51005F520FFFFFFFFFFFF"
	entries := ParseFplmnHex(hex)
	if len(entries) != 2 {
		t.Fatalf("expected 2 FPLMN entries, got %d", len(entries))
	}

	if entries[0].MCC != "505" || entries[0].MNC != "01" || entries[0].PLMN != "50501" {
		t.Errorf("entry 0 parsed incorrectly: %+v", entries[0])
	}
	if entries[1].MCC != "505" || entries[1].MNC != "02" || entries[1].PLMN != "50502" {
		t.Errorf("entry 1 parsed incorrectly: %+v", entries[1])
	}
}

func TestPdpConversions(t *testing.T) {
	if pdpToFrontend("IP") != "ipv4" {
		t.Errorf("pdpToFrontend(IP) = %s; want ipv4", pdpToFrontend("IP"))
	}
	if pdpToFrontend("IPV6") != "ipv6" {
		t.Errorf("pdpToFrontend(IPV6) = %s; want ipv6", pdpToFrontend("IPV6"))
	}
	if pdpToFrontend("IPV4V6") != "ipv4v6" {
		t.Errorf("pdpToFrontend(IPV4V6) = %s; want ipv4v6", pdpToFrontend("IPV4V6"))
	}

	if pdpToAT("ipv4") != "IP" || pdpToAT("ipv6") != "IPV6" || pdpToAT("ipv4v6") != "IPV4V6" {
		t.Errorf("pdpToAT conversion error")
	}
}

func TestRatAcqOrderValidation(t *testing.T) {
	if !isValidRatAcqOrder("NR5G:LTE:WCDMA") {
		t.Errorf("NR5G:LTE:WCDMA should be valid")
	}
	if !isValidRatAcqOrder("LTE:NR5G") {
		t.Errorf("LTE:NR5G should be valid")
	}
	if isValidRatAcqOrder("INVALID:LTE") {
		t.Errorf("INVALID:LTE should be invalid")
	}
}

func TestLockTower_5G_CanonicalFormat(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularHandler(eng, nil)

	mock.SetResponse(`AT+QNWLOCK="common/5g",120,627264,30,78`, "OK")

	body, _ := json.Marshal(map[string]interface{}{
		"mode":   "5g",
		"pcid":   120,
		"earfcn": 627264,
		"scs":    30,
		"band":   78,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.LockTower(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("LockTower returned %d, want 200", w.Code)
	}

	history := mock.GetHistory()
	expectedCmd := `AT+QNWLOCK="common/5g",120,627264,30,78`
	found := false
	for _, cmd := range history {
		if cmd == expectedCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected command %q in history, got: %v", expectedCmd, history)
	}
}

func TestLockTower_5G_WithoutBand_Returns400(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularHandler(eng, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"mode":   "5g",
		"pcid":   120,
		"earfcn": 627264,
		"scs":    30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.LockTower(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("LockTower 5G without band returned %d, want 400", w.Code)
	}
}

func TestLockTower_5G_Aliases(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularHandler(eng, nil)

	mock.SetResponse(`AT+QNWLOCK="common/5g",120,627264,30,78`, "OK")

	body, _ := json.Marshal(map[string]interface{}{
		"mode":  "5g",
		"pci":   120,
		"arfcn": 627264,
		"band":  78,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.LockTower(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("LockTower with aliases returned %d, want 200", w.Code)
	}

	history := mock.GetHistory()
	expectedCmd := `AT+QNWLOCK="common/5g",120,627264,30,78`
	found := false
	for _, cmd := range history {
		if cmd == expectedCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected command %q in history, got: %v", expectedCmd, history)
	}
}

func TestHandleTowerLockCGI(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularHandler(eng, nil)

	// 1. LTE lock
	bodyLTELock, _ := json.Marshal(map[string]interface{}{
		"type":   "lte",
		"action": "lock",
		"cells": []map[string]int{
			{"earfcn": 1300, "pci": 123},
		},
	})
	mock.SetResponse(`AT+QNWLOCK="common/4g",1,1300,123`, "OK")
	reqLTELock := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/tower/lock.sh", bytes.NewBuffer(bodyLTELock))
	wLTELock := httptest.NewRecorder()
	h.HandleTowerLockCGI(wLTELock, reqLTELock)
	if wLTELock.Code != http.StatusOK {
		t.Fatalf("HandleTowerLockCGI LTE lock returned %d, want 200", wLTELock.Code)
	}

	// 2. LTE unlock
	bodyLTEUnlock, _ := json.Marshal(map[string]interface{}{
		"type":   "lte",
		"action": "unlock",
	})
	mock.SetResponse(`AT+QNWLOCK="common/4g",0`, "OK")
	reqLTEUnlock := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/tower/lock.sh", bytes.NewBuffer(bodyLTEUnlock))
	wLTEUnlock := httptest.NewRecorder()
	h.HandleTowerLockCGI(wLTEUnlock, reqLTEUnlock)
	if wLTEUnlock.Code != http.StatusOK {
		t.Fatalf("HandleTowerLockCGI LTE unlock returned %d, want 200", wLTEUnlock.Code)
	}

	// 3. NR-SA lock
	bodyNRLock, _ := json.Marshal(map[string]interface{}{
		"type":   "nr_sa",
		"action": "lock",
		"pci":    901,
		"arfcn":  504990,
		"scs":    30,
		"band":   41,
	})
	mock.SetResponse(`AT+QNWLOCK="common/5g",901,504990,30,41`, "OK")
	reqNRLock := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/tower/lock.sh", bytes.NewBuffer(bodyNRLock))
	wNRLock := httptest.NewRecorder()
	h.HandleTowerLockCGI(wNRLock, reqNRLock)
	if wNRLock.Code != http.StatusOK {
		t.Fatalf("HandleTowerLockCGI NR-SA lock returned %d, want 200", wNRLock.Code)
	}

	// 4. NR-SA unlock
	bodyNRUnlock, _ := json.Marshal(map[string]interface{}{
		"type":   "nr_sa",
		"action": "unlock",
	})
	mock.SetResponse(`AT+QNWLOCK="common/5g",0`, "OK")
	reqNRUnlock := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/tower/lock.sh", bytes.NewBuffer(bodyNRUnlock))
	wNRUnlock := httptest.NewRecorder()
	h.HandleTowerLockCGI(wNRUnlock, reqNRUnlock)
	if wNRUnlock.Code != http.StatusOK {
		t.Fatalf("HandleTowerLockCGI NR-SA unlock returned %d, want 200", wNRUnlock.Code)
	}

	// 5. NR-SA lock without band returns 400
	bodyNoBand, _ := json.Marshal(map[string]interface{}{
		"type":   "nr_sa",
		"action": "lock",
		"pci":    901,
		"arfcn":  504990,
	})
	reqNoBand := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/tower/lock.sh", bytes.NewBuffer(bodyNoBand))
	wNoBand := httptest.NewRecorder()
	h.HandleTowerLockCGI(wNoBand, reqNoBand)
	if wNoBand.Code != http.StatusBadRequest {
		t.Fatalf("HandleTowerLockCGI NR-SA lock without band returned %d, want 400", wNoBand.Code)
	}

	// 6. Invalid type returns 400
	bodyBadType, _ := json.Marshal(map[string]interface{}{
		"type":   "gsm",
		"action": "lock",
	})
	reqBadType := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/tower/lock.sh", bytes.NewBuffer(bodyBadType))
	wBadType := httptest.NewRecorder()
	h.HandleTowerLockCGI(wBadType, reqBadType)
	if wBadType.Code != http.StatusBadRequest {
		t.Fatalf("HandleTowerLockCGI invalid type returned %d, want 400", wBadType.Code)
	}
}
