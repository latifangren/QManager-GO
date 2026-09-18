package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestTTLConfig_PersistenceAndReboot(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "ttl_config.json")

	origTTLPath := defaultTTLConfigPath
	defaultTTLConfigPath = cfgPath
	defer func() { defaultTTLConfigPath = origTTLPath }()

	var executedCmds []string
	mockRunner := func(name string, arg ...string) error {
		executedCmds = append(executedCmds, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		return nil
	}

	h := NewNetworkHandler(nil, mockRunner)

	// 1. Initial state without config file
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/network/ttl", nil)
	wGet := httptest.NewRecorder()
	h.GetTTL(wGet, reqGet)
	var getResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&getResp)
	if getResp["autostart"] != false || getResp["is_enabled"] != true || getResp["ttl"].(float64) != 64 {
		t.Errorf("expected default unpersisted ttl response, got: %+v", getResp)
	}

	// 2. Set TTL to 65
	bodySet, _ := json.Marshal(map[string]interface{}{
		"ttl": 65,
		"hl":  65,
	})
	reqSet := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodySet))
	wSet := httptest.NewRecorder()
	h.SetTTL(wSet, reqSet)
	if wSet.Code != http.StatusOK {
		t.Fatalf("SetTTL failed with %d", wSet.Code)
	}

	// 3. Verify config file written to disk via platform.AtomicWriteFile
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read persisted ttl config file: %v", err)
	}
	var savedCfg TTLConfig
	if err := json.Unmarshal(data, &savedCfg); err != nil {
		t.Fatalf("failed to parse persisted ttl json: %v", err)
	}
	if savedCfg.TTL != 65 || savedCfg.HL != 65 || !savedCfg.AutoStart {
		t.Errorf("unexpected saved config: %+v", savedCfg)
	}

	// 4. Verify GetTTL returns autostart: true, is_enabled: true
	wGet2 := httptest.NewRecorder()
	h.GetTTL(wGet2, reqGet)
	var getResp2 map[string]interface{}
	_ = json.NewDecoder(wGet2.Body).Decode(&getResp2)
	if getResp2["autostart"] != true || getResp2["is_enabled"] != true || getResp2["ttl"].(float64) != 65 {
		t.Errorf("expected persisted ttl response with autostart=true, got: %+v", getResp2)
	}

	// 5. Simulate modem reboot: NewNetworkHandler loads persisted config and reapplies iptables rules
	var rebootCmds []string
	rebootRunner := func(name string, arg ...string) error {
		rebootCmds = append(rebootCmds, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		return nil
	}
	hReboot := NewNetworkHandler(nil, rebootRunner)

	wGetReboot := httptest.NewRecorder()
	hReboot.GetTTL(wGetReboot, reqGet)
	var getRespReboot map[string]interface{}
	_ = json.NewDecoder(wGetReboot.Body).Decode(&getRespReboot)
	if getRespReboot["ttl"].(float64) != 65 || getRespReboot["hl"].(float64) != 65 || getRespReboot["autostart"] != true || getRespReboot["is_enabled"] != true {
		t.Errorf("expected rebooted handler to restore config, got: %+v", getRespReboot)
	}

	// Verify iptables & ip6tables mangle rules re-applied automatically on boot
	if len(rebootCmds) < 2 {
		t.Fatalf("expected at least 2 iptables commands on reboot, got %d: %v", len(rebootCmds), rebootCmds)
	}
	if !strings.Contains(rebootCmds[0], "iptables -t mangle -A POSTROUTING -o rmnet+ -j TTL --ttl-set 65") {
		t.Errorf("expected iptables rule re-applied, got: %s", rebootCmds[0])
	}
	if !strings.Contains(rebootCmds[1], "ip6tables -t mangle -A POSTROUTING -o rmnet+ -j HL --hl-set 65") {
		t.Errorf("expected ip6tables rule re-applied, got: %s", rebootCmds[1])
	}

	// 6. Disable TTL: SetTTL with ttl=0, hl=0
	bodyDisable, _ := json.Marshal(map[string]interface{}{
		"ttl": 0,
		"hl":  0,
	})
	reqDisable := httptest.NewRequest(http.MethodPost, "/api/v1/network/ttl", bytes.NewBuffer(bodyDisable))
	wDisable := httptest.NewRecorder()
	hReboot.SetTTL(wDisable, reqDisable)
	if wDisable.Code != http.StatusOK {
		t.Fatalf("SetTTL disable failed with %d", wDisable.Code)
	}

	dataDisabled, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read persisted ttl config file after disable: %v", err)
	}
	var disabledCfg TTLConfig
	_ = json.Unmarshal(dataDisabled, &disabledCfg)
	if disabledCfg.TTL != 0 || disabledCfg.HL != 0 || disabledCfg.AutoStart {
		t.Errorf("expected disabled config on disk, got: %+v", disabledCfg)
	}

	// 7. Simulate another reboot with disabled config: no iptables commands executed
	var rebootCmds2 []string
	rebootRunner2 := func(name string, arg ...string) error {
		rebootCmds2 = append(rebootCmds2, fmt.Sprintf("%s %s", name, strings.Join(arg, " ")))
		return nil
	}
	hReboot2 := NewNetworkHandler(nil, rebootRunner2)
	if len(rebootCmds2) != 0 {
		t.Errorf("expected 0 iptables commands on reboot when disabled, got %d: %v", len(rebootCmds2), rebootCmds2)
	}

	wGetReboot2 := httptest.NewRecorder()
	hReboot2.GetTTL(wGetReboot2, reqGet)
	var getRespReboot2 map[string]interface{}
	_ = json.NewDecoder(wGetReboot2.Body).Decode(&getRespReboot2)
	if getRespReboot2["ttl"].(float64) != 0 || getRespReboot2["is_enabled"] != false || getRespReboot2["autostart"] != false {
		t.Errorf("expected disabled state in GetTTL after reboot, got: %+v", getRespReboot2)
	}
}
