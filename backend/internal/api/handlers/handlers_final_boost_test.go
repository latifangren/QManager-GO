package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func TestHandlers_FinalTargetedCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)
	_ = cfgMgr
	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 100)
	_ = poller

	// 1. sms.go: hasSmsTool positive and negative branches
	smsHWithoutTool := NewSMSHandler(eng)
	smsHWithoutTool.smsToolPath = "/nonexistent/path/sms_tool"
	if smsHWithoutTool.hasSmsTool() {
		t.Errorf("expected hasSmsTool=false for non-existent binary")
	}

	fakeSmsTool := filepath.Join(tmpDir, "sms_tool")
	_ = os.WriteFile(fakeSmsTool, []byte("#!/bin/sh\nexit 0\n"), 0755)
	smsHWithTool := NewSMSHandler(eng)
	smsHWithTool.smsToolPath = fakeSmsTool
	if !smsHWithTool.hasSmsTool() {
		t.Errorf("expected hasSmsTool=true for existing binary")
	}

	// 1b. sms.go: handleSend empty recipient & empty message
	wSendEmptyPhone := httptest.NewRecorder()
	smsHWithoutTool.handleSend(wSendEmptyPhone, nil, "", "Hello")
	var errPhone map[string]interface{}
	_ = json.NewDecoder(wSendEmptyPhone.Body).Decode(&errPhone)
	if errPhone["error"] != "missing_phone" {
		t.Errorf("expected missing_phone error, got %v", errPhone)
	}

	wSendEmptyMsg := httptest.NewRecorder()
	smsHWithoutTool.handleSend(wSendEmptyMsg, nil, "+628123456789", "   ")
	var errMsg map[string]interface{}
	_ = json.NewDecoder(wSendEmptyMsg.Body).Decode(&errMsg)
	if errMsg["error"] != "missing_message" {
		t.Errorf("expected missing_message error, got %v", errMsg)
	}

	// 1c. sms.go: handleDeleteAll empty storage
	mock.SetResponse("AT+CPMS=\"ME\",\"ME\",\"ME\"", "OK")
	mock.SetResponse("AT+CMGD=1,4", "OK")
	mock.SetResponse("AT+CPMS=\"SM\",\"SM\",\"SM\"", "OK")
	wDelAll := httptest.NewRecorder()
	smsHWithoutTool.handleDeleteAll(wDelAll, context.Background())
	if wDelAll.Code != http.StatusOK {
		t.Errorf("expected 200 for handleDeleteAll, got %d", wDelAll.Code)
	}

	// 2. cellular_priority.go: GetPriority and SetPriority
	prioH := NewNetworkPriorityHandler(eng)
	mock.SetResponse(`AT+QNWPREFCFG="rat_acq_order"`, `+QNWPREFCFG: "rat_acq_order",AUTO`+"\r\nOK")
	wPrioGet := httptest.NewRecorder()
	prioH.GetPriority(wPrioGet, httptest.NewRequest(http.MethodGet, "/api/cellular/priority", nil))
	if wPrioGet.Code != http.StatusOK {
		t.Fatalf("GetPriority returned %d, want 200", wPrioGet.Code)
	}

	mock.SetResponse(`AT+QNWPREFCFG="rat_acq_order",NR5G:LTE:WCDMA:GSM`, "OK")
	bodyPrioValid, _ := json.Marshal(SetPriorityPayload{Order: "NR5G:LTE:WCDMA:GSM"})
	wPrioSet := httptest.NewRecorder()
	prioH.SetPriority(wPrioSet, httptest.NewRequest(http.MethodPost, "/api/cellular/priority", bytes.NewBuffer(bodyPrioValid)))
	if wPrioSet.Code != http.StatusOK {
		t.Fatalf("SetPriority valid returned %d, want 200", wPrioSet.Code)
	}

	bodyPrioBad, _ := json.Marshal(SetPriorityPayload{Order: "INVALID_RAT_ORDER"})
	wPrioBad := httptest.NewRecorder()
	prioH.SetPriority(wPrioBad, httptest.NewRequest(http.MethodPost, "/api/cellular/priority", bytes.NewBuffer(bodyPrioBad)))
	if wPrioBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid RAT priority order, got %d", wPrioBad.Code)
	}

	// 3. data_usage.go: GetDataUsed with sysfs mock network counters
	dataH := NewDataUsageHandler()
	wDataGet := httptest.NewRecorder()
	dataH.GetDataUsed(wDataGet, httptest.NewRequest(http.MethodGet, "/api/network/data-usage", nil))
	if wDataGet.Code != http.StatusOK {
		t.Fatalf("GetDataUsed returned %d, want 200", wDataGet.Code)
	}

	// 4. ip_passthrough.go: Status and mode transitions
	origIppt := ipptConfigPath
	ipptConfigPath = filepath.Join(tmpDir, "ippt_config.json")
	t.Cleanup(func() {
		ipptConfigPath = origIppt
	})
	ipptH := NewIPPassthroughHandler(eng)
	mock.SetResponse(`AT+QMAP="MPDN_rule";+QMAP="IPPT_NAT";+QCFG="usbnet";+QMAP="DHCPV4DNS"`, `+QMAP: "MPDN_rule",0,1,0,3,1,"11:22:33:44:55:66"`+"\r\nOK")
	wIpptStatus := httptest.NewRecorder()
	ipptH.Status(wIpptStatus, httptest.NewRequest(http.MethodGet, "/api/network/passthrough", nil))
	if wIpptStatus.Code != http.StatusOK {
		t.Fatalf("IPPT Status returned %d, want 200", wIpptStatus.Code)
	}

	// 5. language_packs.go: NewLanguagePacksHandler, list, install & remove
	lpH := NewLanguagePacksHandler()
	lpH.packsDir = filepath.Join(tmpDir, "lang_packs")
	_ = os.MkdirAll(lpH.packsDir, 0755)

	wLpList := httptest.NewRecorder()
	lpH.List(wLpList, httptest.NewRequest(http.MethodGet, "/api/system/language-packs/list", nil))
	if wLpList.Code != http.StatusOK {
		t.Fatalf("LanguagePacks List returned %d, want 200", wLpList.Code)
	}

	bodyInstallLp, _ := json.Marshal(map[string]string{"code": "zh-CN", "url": "https://example.com/zh.json"})
	wLpInstall := httptest.NewRecorder()
	lpH.Install(wLpInstall, httptest.NewRequest(http.MethodPost, "/api/system/language-packs/install", bytes.NewBuffer(bodyInstallLp)))
	if wLpInstall.Code != http.StatusOK {
		t.Fatalf("LanguagePacks Install returned %d, want 200", wLpInstall.Code)
	}

	bodyRemoveLp, _ := json.Marshal(map[string]string{"code": "zh-CN"})
	wLpRemove := httptest.NewRecorder()
	lpH.Remove(wLpRemove, httptest.NewRequest(http.MethodPost, "/api/system/language-packs/remove", bytes.NewBuffer(bodyRemoveLp)))
	if wLpRemove.Code != http.StatusOK {
		t.Fatalf("LanguagePacks Remove returned %d, want 200", wLpRemove.Code)
	}

	// 6. ethernet.go: NewEthernetHandler, query port stats
	ethH := NewEthernetHandler()
	ethSysDir := filepath.Join(tmpDir, "eth0_sysfs")
	_ = os.MkdirAll(ethSysDir, 0755)
	_ = os.WriteFile(filepath.Join(ethSysDir, "operstate"), []byte("up\n"), 0644)
	_ = os.WriteFile(filepath.Join(ethSysDir, "speed"), []byte("2500\n"), 0644)
	_ = os.WriteFile(filepath.Join(ethSysDir, "duplex"), []byte("full\n"), 0644)
	_ = os.WriteFile(filepath.Join(ethSysDir, "mtu"), []byte("1500\n"), 0644)
	ethH.ifacePath = ethSysDir

	wEth := httptest.NewRecorder()
	ethH.HandleEthernet(wEth, httptest.NewRequest(http.MethodGet, "/api/network/ethernet", nil))
	if wEth.Code != http.StatusOK {
		t.Fatalf("HandleEthernet returned %d, want 200", wEth.Code)
	}
	var ethResp map[string]interface{}
	_ = json.NewDecoder(wEth.Body).Decode(&ethResp)
	if ethResp["speed_mbps"].(float64) != 2500 || ethResp["link_up"] != true {
		t.Errorf("HandleEthernet mismatch: %+v", ethResp)
	}
}
