package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateMAC(t *testing.T) {
	validMACs := []string{
		"00:1A:2B:3C:4D:5E",
		"00-1A-2B-3C-4D-5E",
		"aa:bb:cc:dd:ee:ff",
		"AA:BB:CC:DD:EE:FF",
	}
	invalidMACs := []string{
		"00:1A:2B:3C:4D",
		"00:1A:2B:3C:4D:5E:6F",
		"00:1G:2B:3C:4D:5E",
		"invalid",
		"",
	}

	for _, mac := range validMACs {
		if !validateMAC(mac) {
			t.Errorf("validateMAC(%q) = false; want true", mac)
		}
	}
	for _, mac := range invalidMACs {
		if validateMAC(mac) {
			t.Errorf("validateMAC(%q) = true; want false", mac)
		}
	}
}

func TestTrafficEngineHostlistParsing(t *testing.T) {
	domains := []string{"example.com", "test.org", "googlevideo.com"}
	p := VideoOptimizerSavePayload{
		Domains: domains,
	}
	if len(p.Domains) != 3 {
		t.Errorf("expected 3 domains, got %d", len(p.Domains))
	}
}

func TestGetTTL_And_SetTTL(t *testing.T) {
	executedCmds := []string{}
	mockRunner := func(name string, arg ...string) error {
		executedCmds = append(executedCmds, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		return nil
	}

	h := NewNetworkHandler(nil, mockRunner)

	// 1. Initial GetTTL returns defaults
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/network/ttl", nil)
	wGet := httptest.NewRecorder()
	h.GetTTL(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetTTL returned %d, want 200", wGet.Code)
	}

	var getResp map[string]interface{}
	if err := json.NewDecoder(wGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode GetTTL response: %v", err)
	}
	if getResp["success"] != true || getResp["ttl"].(float64) != 64 || getResp["hl"].(float64) != 64 {
		t.Errorf("expected default ttl=64, hl=64, got %+v", getResp)
	}

	// 2. SetTTL with both ttl and hl
	bodyBoth, _ := json.Marshal(map[string]interface{}{
		"ttl": 128,
		"hl":  128,
	})
	reqBoth := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyBoth))
	wBoth := httptest.NewRecorder()
	h.SetTTL(wBoth, reqBoth)

	if wBoth.Code != http.StatusOK {
		t.Fatalf("SetTTL returned %d, want 200", wBoth.Code)
	}

	// 3. SetTTL with only ttl (hl should default to ttl)
	bodyTTLOnly, _ := json.Marshal(map[string]interface{}{
		"ttl": 65,
	})
	reqTTLOnly := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyTTLOnly))
	wTTLOnly := httptest.NewRecorder()
	h.SetTTL(wTTLOnly, reqTTLOnly)

	if wTTLOnly.Code != http.StatusOK {
		t.Fatalf("SetTTL with ttl only returned %d, want 200", wTTLOnly.Code)
	}

	// 4. Verify GetTTL returns updated values
	wGetUpdated := httptest.NewRecorder()
	h.GetTTL(wGetUpdated, reqGet)
	var getRespUpdated map[string]interface{}
	_ = json.NewDecoder(wGetUpdated.Body).Decode(&getRespUpdated)
	if getRespUpdated["ttl"].(float64) != 65 || getRespUpdated["hl"].(float64) != 65 {
		t.Errorf("expected ttl=65, hl=65, got %+v", getRespUpdated)
	}

	// 5. Disable TTL with ttl=0, hl=0
	bodyDisable, _ := json.Marshal(map[string]interface{}{
		"ttl": 0,
		"hl":  0,
	})
	reqDisable := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyDisable))
	wDisable := httptest.NewRecorder()
	h.SetTTL(wDisable, reqDisable)
	if wDisable.Code != http.StatusOK {
		t.Fatalf("SetTTL with ttl=0, hl=0 returned %d, want 200", wDisable.Code)
	}
	var disableResp map[string]interface{}
	_ = json.NewDecoder(wDisable.Body).Decode(&disableResp)
	if disableResp["is_enabled"] != false || disableResp["ttl"].(float64) != 0 || disableResp["hl"].(float64) != 0 {
		t.Errorf("expected is_enabled=false, ttl=0, hl=0 on disable, got %+v", disableResp)
	}

	// Verify GetTTL after disable reports is_enabled=false
	wGetDisabled := httptest.NewRecorder()
	h.GetTTL(wGetDisabled, reqGet)
	var getDisabledResp map[string]interface{}
	_ = json.NewDecoder(wGetDisabled.Body).Decode(&getDisabledResp)
	if getDisabledResp["is_enabled"] != false || getDisabledResp["ttl"].(float64) != 0 || getDisabledResp["hl"].(float64) != 0 {
		t.Errorf("expected GetTTL to report is_enabled=false, ttl=0, hl=0, got %+v", getDisabledResp)
	}

	// 6. Invalid TTL returns 400
	bodyInvalid, _ := json.Marshal(map[string]interface{}{
		"ttl": 300,
	})
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyInvalid))
	wInvalid := httptest.NewRecorder()
	h.SetTTL(wInvalid, reqInvalid)
	if wInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for ttl=300, got %d", wInvalid.Code)
	}

	// 7. Negative TTL returns 400
	bodyNegative, _ := json.Marshal(map[string]interface{}{
		"ttl": -5,
	})
	reqNegative := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyNegative))
	wNegative := httptest.NewRecorder()
	h.SetTTL(wNegative, reqNegative)
	if wNegative.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for ttl=-5, got %d", wNegative.Code)
	}

	// 8. Runner failure returns 500
	failRunner := func(name string, arg ...string) error {
		return fmt.Errorf("permission denied")
	}
	hFail := NewNetworkHandler(nil, failRunner)
	reqFail := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyTTLOnly))
	wFail := httptest.NewRecorder()
	hFail.SetTTL(wFail, reqFail)
	if wFail.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when runner fails, got %d", wFail.Code)
	}
}
