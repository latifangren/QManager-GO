package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
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
		Success   bool           `json:"success"`
		Current   CurrentBands   `json:"current"`
		Supported SupportedBands `json:"supported"`
		Failover  FailoverState  `json:"failover"`
	}
	if err := json.NewDecoder(wGetBands.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed decoding GetBands response: %v", err)
	}
	if !getResp.Success || getResp.Current.LTEBands != "1:3:7:8:20:28" {
		t.Fatalf("unexpected GetBands response payload: %+v", getResp)
	}
	if len(getResp.Supported.LTEBands) == 0 {
		t.Fatalf("expected supported LTE bands to be populated")
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
		t.Fatalf("expected LTE bands 1:3 from legacy mask 0x5, got %v", getLegacyResp.Current.LTEBands)
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
	mock.SetResponse(`AT+QNWLOCK="common/5g",200,631000,30,78`, "OK")
	bodyLock5G, _ := json.Marshal(map[string]interface{}{
		"mode":   "5g",
		"earfcn": 631000,
		"pcid":   200,
		"scs":    30,
		"band":   78,
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

func TestSetBandsRequest_FlexibleParsing(t *testing.T) {
	// Case 1: Array of strings
	raw1 := `{"lte_bands":["1","3","7"],"nr_bands":["78"]}`
	var req1 SetBandsRequest
	if err := json.Unmarshal([]byte(raw1), &req1); err != nil {
		t.Fatalf("failed to unmarshal req1: %v", err)
	}
	if strings.Join(req1.LTEBands, ":") != "1:3:7" || strings.Join(req1.NRBands, ":") != "78" {
		t.Errorf("req1 parsed incorrectly: %+v", req1)
	}

	// Case 2: Array of ints
	raw2 := `{"lte_bands":[1, 3, 7],"nr_bands":[78]}`
	var req2 SetBandsRequest
	if err := json.Unmarshal([]byte(raw2), &req2); err != nil {
		t.Fatalf("failed to unmarshal req2: %v", err)
	}
	if strings.Join(req2.LTEBands, ":") != "1:3:7" || strings.Join(req2.NRBands, ":") != "78" {
		t.Errorf("req2 parsed incorrectly: %+v", req2)
	}

	// Case 3: Colon-delimited string
	raw3 := `{"lte_bands":"1:3:7","nr_bands":"78"}`
	var req3 SetBandsRequest
	if err := json.Unmarshal([]byte(raw3), &req3); err != nil {
		t.Fatalf("failed to unmarshal req3: %v", err)
	}
	if strings.Join(req3.LTEBands, ":") != "1:3:7" || strings.Join(req3.NRBands, ":") != "78" {
		t.Errorf("req3 parsed incorrectly: %+v", req3)
	}

	// Case 4: Space-delimited string
	raw4 := `{"lte_bands":"1 3 7","nr_bands":"78"}`
	var req4 SetBandsRequest
	if err := json.Unmarshal([]byte(raw4), &req4); err != nil {
		t.Fatalf("failed to unmarshal req4: %v", err)
	}
	if strings.Join(req4.LTEBands, ":") != "1:3:7" || strings.Join(req4.NRBands, ":") != "78" {
		t.Errorf("req4 parsed incorrectly: %+v", req4)
	}
}

func TestGetAPN_BooleanFields(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	apnH := NewCellularApnHandler(eng, cfgMgr, tmpDir)

	mock.SetResponse("AT+CGDCONT?", `+CGDCONT: 1,"IPV4V6","internet","0.0.0.0",0,0,0,0`+"\r\nOK")
	mock.SetResponse("AT+CGACT?", `+CGACT: 1,1`+"\r\nOK")
	mock.SetResponse("AT+CGCONTRDP", `+CGCONTRDP: 1,5,"internet"`+"\r\nOK")

	w := httptest.NewRecorder()
	apnH.GetAPN(w, httptest.NewRequest(http.MethodGet, "/api/cellular/apn", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAPN returned %d, want 200", w.Code)
	}

	var resp struct {
		Success     bool         `json:"success"`
		Active      int          `json:"active"`
		MaxProfiles int          `json:"max_profiles"`
		DataSource  string       `json:"data_source"`
		Profiles    []ApnProfile `json:"profiles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode APN response: %v", err)
	}

	if !resp.Success || resp.MaxProfiles != 6 || resp.DataSource != "at" {
		t.Errorf("unexpected APN envelope: %+v", resp)
	}

	if len(resp.Profiles) != 6 {
		t.Fatalf("expected 6 profiles, got %d", len(resp.Profiles))
	}

	// Profile 1 should be active and enabled
	p1 := resp.Profiles[0]
	if !p1.Enabled || !p1.IsActive || !p1.Active || p1.HasPw {
		t.Errorf("profile 1 booleans incorrect: Enabled=%v, IsActive=%v, Active=%v, HasPw=%v", p1.Enabled, p1.IsActive, p1.Active, p1.HasPw)
	}
}
