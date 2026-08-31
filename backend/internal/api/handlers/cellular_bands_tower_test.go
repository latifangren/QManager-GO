package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	mock.SetResponse(`AT+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`, `+QNWPREFCFG: "lte_band",1:3:7:8:20:28`+"\r\nOK")
	wGetBands := httptest.NewRecorder()
	h.GetBands(wGetBands, httptest.NewRequest(http.MethodGet, "/api/cellular/bands", nil))
	if wGetBands.Code != http.StatusOK {
		t.Fatalf("GetBands returned %d, want 200", wGetBands.Code)
	}
	var getResp struct {
		Success bool         `json:"success"`
		Current CurrentBands `json:"current"`
		Failover FailoverState `json:"failover"`
	}
	if err := json.NewDecoder(wGetBands.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed decoding GetBands response: %v", err)
	}
	if !getResp.Success || getResp.Current.LTEBands != "1:3:7:8:20:28" {
		t.Fatalf("unexpected GetBands response payload: %+v", getResp)
	}

	// 1b. Test fallback to legacy AT+QCFG="band"
	mock.SetResponse(`AT+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`, "ERROR\r\n")
	mock.SetResponse(`AT+QCFG="band"`, `+QCFG: "band",0x1e,0x5,0x0`+"\r\nOK")
	wGetBandsLegacy := httptest.NewRecorder()
	h.GetBands(wGetBandsLegacy, httptest.NewRequest(http.MethodGet, "/api/cellular/bands", nil))
	if wGetBandsLegacy.Code != http.StatusOK {
		t.Fatalf("GetBands legacy fallback returned %d, want 200", wGetBandsLegacy.Code)
	}
	var getLegacyResp struct {
		Success bool         `json:"success"`
		Current CurrentBands `json:"current"`
	}
	_ = json.NewDecoder(wGetBandsLegacy.Body).Decode(&getLegacyResp)
	if getLegacyResp.Current.LTEBands != "1:3" {
		t.Fatalf("expected LTE bands 1:3 from legacy mask 0x5, got %s", getLegacyResp.Current.LTEBands)
	}

	// 2. LockBands LTE and NR (String arrays)
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

	// 2b. LockBands with single band_type and colon string
	mock.SetResponse(`AT+QNWPREFCFG="lte_band",1:3:5:8:40`, "OK")
	bodyColon := `{"band_type":"lte","bands":"1:3:5:8:40","failover":true}`
	wLockColon := httptest.NewRecorder()
	h.LockBands(wLockColon, httptest.NewRequest(http.MethodPost, "/api/cellular/bands", strings.NewReader(bodyColon)))
	if wLockColon.Code != http.StatusOK {
		t.Fatalf("LockBands with colon string returned %d: %s", wLockColon.Code, wLockColon.Body.String())
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
