package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"qmanager/internal/atengine"
)

func TestFrequencyLockHandler_Deep(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewFrequencyLockHandler(eng)

	// 1. Status with active tower lock (mutual exclusion flags)
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock";+QNWCFG="nr5g_earfcn_lock"`, `+QNWCFG: "lte_earfcn_lock",2,1675:1700`+"\r\n"+`+QNWCFG: "nr5g_earfcn_lock",1,627392:30`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",1,1675,218`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",627392,320,30,78`+"\r\nOK")

	reqStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/frequency", nil)
	wStatus := httptest.NewRecorder()
	h.Status(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("Status returned %d, want 200", wStatus.Code)
	}

	var statusResp FrequencyStatusResponse
	_ = json.NewDecoder(wStatus.Body).Decode(&statusResp)
	if !statusResp.ModemState.LTELocked || len(statusResp.ModemState.LTEEntries) != 2 {
		t.Errorf("expected 2 LTE entries locked, got %+v", statusResp.ModemState.LTEEntries)
	}
	if !statusResp.ModemState.NRLocked || len(statusResp.ModemState.NREntries) != 1 {
		t.Errorf("expected 1 NR entry locked, got %+v", statusResp.ModemState.NREntries)
	}
	if statusResp.ModemState.TowerLockLTE == nil || !*statusResp.ModemState.TowerLockLTE {
		t.Errorf("expected TowerLockLTE=true")
	}

	// 2. Lock - Bad JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBufferString(`bad`))
	wBadJSON := httptest.NewRecorder()
	h.Lock(wBadJSON, reqBadJSON)
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}

	// 3. Lock - Invalid RAT / Action
	bodyBadRAT, _ := json.Marshal(FrequencyLockRequest{RAT: "gsm", Action: "lock"})
	reqBadRAT := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyBadRAT))
	wBadRAT := httptest.NewRecorder()
	h.Lock(wBadRAT, reqBadRAT)
	if wBadRAT.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid RAT 'gsm', got %d", wBadRAT.Code)
	}

	bodyBadAct, _ := json.Marshal(FrequencyLockRequest{RAT: "lte", Action: "toggle"})
	reqBadAct := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyBadAct))
	wBadAct := httptest.NewRecorder()
	h.Lock(wBadAct, reqBadAct)
	if wBadAct.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid Action 'toggle', got %d", wBadAct.Code)
	}

	// 4. Lock LTE - Blocked by Active LTE Tower Lock
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",1,1675,218`+"\r\nOK")
	bodyLTETowerBlocked, _ := json.Marshal(FrequencyLockRequest{
		Action:     "lock",
		RAT:        "lte",
		LTEEntries: []LTEFreqEntry{{EARFCN: 1675}},
	})
	reqLTETowerBlocked := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyLTETowerBlocked))
	wLTETowerBlocked := httptest.NewRecorder()
	h.Lock(wLTETowerBlocked, reqLTETowerBlocked)
	// Handlers in frequency_lock respond with Error(w, http.StatusOK, msg) for UI toasts
	var blockResp map[string]interface{}
	_ = json.NewDecoder(wLTETowerBlocked.Body).Decode(&blockResp)
	if blockResp["success"] != false {
		t.Errorf("expected success=false when LTE tower lock is active, got %+v", blockResp)
	}

	// 5. Lock LTE - Invalid EARFCN (empty entries or out of range)
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",0`+"\r\nOK")

	bodyLTEEmpty, _ := json.Marshal(FrequencyLockRequest{
		Action:     "lock",
		RAT:        "lte",
		LTEEntries: []LTEFreqEntry{},
	})
	reqLTEEmpty := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyLTEEmpty))
	wLTEEmpty := httptest.NewRecorder()
	h.Lock(wLTEEmpty, reqLTEEmpty)
	if wLTEEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty LTE entries, got %d", wLTEEmpty.Code)
	}

	bodyLTEOutOfRange, _ := json.Marshal(FrequencyLockRequest{
		Action:     "lock",
		RAT:        "lte",
		LTEEntries: []LTEFreqEntry{{EARFCN: 300000}},
	})
	reqLTEOutOfRange := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyLTEOutOfRange))
	wLTEOutOfRange := httptest.NewRecorder()
	h.Lock(wLTEOutOfRange, reqLTEOutOfRange)
	if wLTEOutOfRange.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for out of range EARFCN, got %d", wLTEOutOfRange.Code)
	}

	// 6. Lock NR5G - Blocked by Active NR Tower Lock
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",627392,320,30,78`+"\r\nOK")
	bodyNRTowerBlocked, _ := json.Marshal(FrequencyLockRequest{
		Action:    "lock",
		RAT:       "nr5g",
		NREntries: []NRFreqEntry{{ARFCN: 627392, SCS: 30}},
	})
	reqNRTowerBlocked := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyNRTowerBlocked))
	wNRTowerBlocked := httptest.NewRecorder()
	h.Lock(wNRTowerBlocked, reqNRTowerBlocked)
	var nrBlockResp map[string]interface{}
	_ = json.NewDecoder(wNRTowerBlocked.Body).Decode(&nrBlockResp)
	if nrBlockResp["success"] != false {
		t.Errorf("expected success=false when NR tower lock is active, got %+v", nrBlockResp)
	}

	// 7. Lock NR5G - Success
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWCFG="nr5g_earfcn_lock",1,627392:30`, "OK")

	bodyNRSuccess, _ := json.Marshal(FrequencyLockRequest{
		Action:    "lock",
		RAT:       "nr5g",
		NREntries: []NRFreqEntry{{ARFCN: 627392, SCS: 30}},
	})
	reqNRSuccess := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyNRSuccess))
	wNRSuccess := httptest.NewRecorder()
	h.Lock(wNRSuccess, reqNRSuccess)
	if wNRSuccess.Code != http.StatusOK {
		t.Fatalf("NR lock returned %d, want 200", wNRSuccess.Code)
	}

	var nrSuccessResp map[string]interface{}
	_ = json.NewDecoder(wNRSuccess.Body).Decode(&nrSuccessResp)
	if nrSuccessResp["success"] != true || nrSuccessResp["count"].(float64) != 1 {
		t.Errorf("unexpected NR lock response: %+v", nrSuccessResp)
	}
}
