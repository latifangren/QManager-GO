package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qmanager/internal/atengine"
)

func TestCellularSettings_GetSettings_DefaultAndAMBR(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularSettingsHandler(eng)

	compoundCmd := `AT+QUIMSLOT?;+CFUN?;+QNWPREFCFG="mode_pref";+QNWPREFCFG="nr5g_disable_mode";+QNWPREFCFG="roam_pref";+QSIMDET?;+QNWCFG="lte_ambr";+QNWCFG="nr5g_ambr"`
	compoundResp := `+QUIMSLOT: 2
+CFUN: 1
+QNWPREFCFG: "mode_pref",LTE:NR5G
+QNWPREFCFG: "nr5g_disable_mode",2
+QNWPREFCFG: "roam_pref",1
+QSIMDET: 1,1
+QNWCFG: "lte_ambr","internet",100000,50000
+QNWCFG: "nr5g_ambr","ims",2,50000,1,25000
OK`
	mock.SetResponse(compoundCmd, compoundResp)
	mock.SetResponse(`AT+QSIMCFG="dual_slot_status"`, "ERROR")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cellular/settings", nil)
	w := httptest.NewRecorder()
	h.GetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetSettings returned %d, want 200", w.Code)
	}

	var resp CellularSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true")
	}
	if resp.Settings.SimSlot != 2 {
		t.Errorf("expected sim_slot=2, got %d", resp.Settings.SimSlot)
	}
	if resp.Settings.Cfun != 1 {
		t.Errorf("expected cfun=1, got %d", resp.Settings.Cfun)
	}
	if resp.Settings.ModePref != "LTE:NR5G" {
		t.Errorf("expected mode_pref='LTE:NR5G', got %q", resp.Settings.ModePref)
	}
	if resp.Settings.Nr5gMode != 2 {
		t.Errorf("expected nr5g_mode=2, got %d", resp.Settings.Nr5gMode)
	}
	if resp.Settings.RoamPref != 1 {
		t.Errorf("expected roam_pref=1, got %d", resp.Settings.RoamPref)
	}
	if resp.Settings.SimDetect != 1 || resp.Settings.SimDetectLevel != 1 {
		t.Errorf("expected sim_detect=1, sim_detect_level=1, got %d, %d", resp.Settings.SimDetect, resp.Settings.SimDetectLevel)
	}
	if len(resp.Ambr.LTE) != 1 || resp.Ambr.LTE[0].APN != "internet" || resp.Ambr.LTE[0].DLKbps != 100000 {
		t.Errorf("unexpected LTE AMBR: %+v", resp.Ambr.LTE)
	}
	if len(resp.Ambr.NR5G) != 1 || resp.Ambr.NR5G[0].DNN != "ims" || resp.Ambr.NR5G[0].DLKbps != 100000 {
		t.Errorf("unexpected NR5G AMBR: %+v", resp.Ambr.NR5G)
	}
	if resp.DualSlot != nil {
		t.Errorf("expected dual_slot to be null/nil on error, got %+v", resp.DualSlot)
	}
}

func TestCellularSettings_GetSettings_DualSlot(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularSettingsHandler(eng)

	mock.SetResponse(`AT+QUIMSLOT?;+CFUN?;+QNWPREFCFG="mode_pref";+QNWPREFCFG="nr5g_disable_mode";+QNWPREFCFG="roam_pref";+QSIMDET?;+QNWCFG="lte_ambr";+QNWCFG="nr5g_ambr"`, `+QUIMSLOT: 1`+"\r\n"+`+CFUN: 1`+"\r\nOK")
	mock.SetResponse(`AT+QSIMCFG="dual_slot_status"`, `+QSIMCFG: "dual_slot_status",1,1,3,3B9F96803FC6,5,"89860401102290123456",1,0,3,3B9F96803FC6,5,"89860401102290654321"`+"\r\nOK")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cellular/settings", nil)
	w := httptest.NewRecorder()
	h.GetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetSettings returned %d, want 200", w.Code)
	}

	var resp CellularSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.DualSlot == nil || len(*resp.DualSlot) != 2 {
		t.Fatalf("expected 2 dual slot entries, got %+v", resp.DualSlot)
	}
	slots := *resp.DualSlot
	if slots[0].Slot != 1 || !slots[0].Active || slots[0].ICCID != "89860401102290123456" {
		t.Errorf("slot 1 mismatch: %+v", slots[0])
	}
	if slots[1].Slot != 2 || slots[1].Active || slots[1].ICCID != "89860401102290654321" {
		t.Errorf("slot 2 mismatch: %+v", slots[1])
	}
}

func TestCellularSettings_GetSettings_MissingCriticalLinesReturnsReadFailed(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularSettingsHandler(eng)

	// Compound response missing +QUIMSLOT: and +CFUN:
	mock.SetResponse(`AT+QUIMSLOT?;+CFUN?;+QNWPREFCFG="mode_pref";+QNWPREFCFG="nr5g_disable_mode";+QNWPREFCFG="roam_pref";+QSIMDET?;+QNWCFG="lte_ambr";+QNWCFG="nr5g_ambr"`, "OK")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cellular/settings", nil)
	w := httptest.NewRecorder()
	h.GetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetSettings returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != false || resp["error"] != "read_failed" {
		t.Errorf("expected success=false, error=read_failed for missing critical lines, got %+v", resp)
	}
}

func TestCellularSettings_ApplySettings(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularSettingsHandler(eng)
	h.SetDelays(0, 0)
	mock.SetResponse("AT+QUIMSLOT?", `+QUIMSLOT: 2`+"\r\nOK")

	slot := 2
	cfun := 1
	modePref := "LTE:NR5G"
	nr5gMode := 0
	roamPref := 255
	simDetect := 1
	simDetectLevel := 1

	payload := CellularSettingsApplyReq{
		SimSlot:        &slot,
		Cfun:           &cfun,
		ModePref:       &modePref,
		Nr5gMode:       &nr5gMode,
		RoamPref:       &roamPref,
		SimDetect:      &simDetect,
		SimDetectLevel: &simDetectLevel,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cellular/settings", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.ApplySettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ApplySettings returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode apply response: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success=true, got %+v", resp)
	}

	history := mock.GetHistory()
	expectedSequence := []string{
		"AT+CFUN=0",
		"AT+QUIMSLOT=2",
		"AT+CFUN=1",
		"AT+QUIMSLOT?",
		"AT+CFUN=1",
		`AT+QNWPREFCFG="mode_pref",LTE:NR5G`,
		`AT+QNWPREFCFG="nr5g_disable_mode",0`,
		`AT+QNWPREFCFG="roam_pref",255`,
		"AT+QSIMDET=1,1",
	}

	for _, exp := range expectedSequence {
		found := false
		for _, act := range history {
			if act == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected command %q in history, history=%v", exp, history)
		}
	}
}

func TestCellularSettings_ApplySimSlot_VerificationFailure(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularSettingsHandler(eng)
	h.SetDelays(0, 0)
	// Modem remains on slot 1 even though we requested slot 2
	mock.SetResponse("AT+QUIMSLOT?", `+QUIMSLOT: 1`+"\r\nOK")

	err := h.applySimSlot(2)
	if err == nil {
		t.Fatalf("expected error when slot switch verification fails, got nil")
	}
	if !strings.Contains(err.Error(), "slot switch verification failed: modem remained on slot 1") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCellularSettings_ApplySettings_BadJSON(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	h := NewCellularSettingsHandler(eng)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cellular/settings", bytes.NewBufferString("invalid json"))
	w := httptest.NewRecorder()
	h.ApplySettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", w.Code)
	}
}
