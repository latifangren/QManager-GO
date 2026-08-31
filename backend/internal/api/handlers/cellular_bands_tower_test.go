package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func TestCellular_LockTowerAndBands_Deep(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 100)
	h := NewCellularHandler(eng, poller)

	// 1. GetBands
	mock.SetResponse(`AT+QNWPREFCFG="gw_band";+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`, `+QNWPREFCFG: "lte_band",1:3:7:8:20:28`+"\r\nOK")
	wGetBands := httptest.NewRecorder()
	h.GetBands(wGetBands, httptest.NewRequest(http.MethodGet, "/api/cellular/bands", nil))
	if wGetBands.Code != http.StatusOK {
		t.Fatalf("GetBands returned %d, want 200", wGetBands.Code)
	}

	// 2. LockBands LTE and NR
	mock.SetResponse(`AT+QNWPREFCFG="lte_band",1:3:7`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="nr5g_band",77:78`, "OK")
	bodyBands, _ := json.Marshal(SetBandsRequest{
		LTEBands: []string{"1", "3", "7"},
		NRBands:  []string{"77", "78"},
	})
	wLockBands := httptest.NewRecorder()
	h.LockBands(wLockBands, httptest.NewRequest(http.MethodPost, "/api/cellular/bands", bytes.NewBuffer(bodyBands)))
	if wLockBands.Code != http.StatusOK {
		t.Fatalf("LockBands returned %d: %s", wLockBands.Code, wLockBands.Body.String())
	}

	// 3. LockTower 4G mode
	mock.SetResponse(`AT+QNWLOCK="common/4g",1,1850,120`, "OK")
	bodyLock4G, _ := json.Marshal(map[string]interface{}{
		"mode":   "4g",
		"earfcn": 1850,
		"pcid":   120,
	})
	wLock4G := httptest.NewRecorder()
	h.LockTower(wLock4G, httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(bodyLock4G)))
	if wLock4G.Code != http.StatusOK {
		t.Fatalf("LockTower 4g returned %d", wLock4G.Code)
	}

	// 4. LockTower 5G mode with SCS
	mock.SetResponse(`AT+QNWLOCK="common/5g",631000,200,30`, "OK")
	bodyLock5G, _ := json.Marshal(map[string]interface{}{
		"mode":   "5g",
		"earfcn": 631000,
		"pcid":   200,
		"scs":    30,
	})
	wLock5G := httptest.NewRecorder()
	h.LockTower(wLock5G, httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(bodyLock5G)))
	if wLock5G.Code != http.StatusOK {
		t.Fatalf("LockTower 5g returned %d", wLock5G.Code)
	}

	// 5. LockTower with AT execution failure
	mock.SetResponse(`AT+QNWLOCK="common/4g",1,9999,99`, "ERROR")
	bodyLockFail, _ := json.Marshal(map[string]interface{}{
		"mode":   "4g",
		"earfcn": 9999,
		"pcid":   99,
	})
	wLockFail := httptest.NewRecorder()
	h.LockTower(wLockFail, httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(bodyLockFail)))
	if wLockFail.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for AT error, got %d", wLockFail.Code)
	}

	// 6. LockTower invalid JSON
	wLockBad := httptest.NewRecorder()
	h.LockTower(wLockBad, httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBufferString("{invalid")))
	if wLockBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wLockBad.Code)
	}
}
