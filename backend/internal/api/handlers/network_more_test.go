package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func newTestNetworkEngine(t *testing.T) (*atengine.MockTransport, *atengine.Engine) {
	t.Helper()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() {
		_ = eng.Close()
	})
	return mock, eng
}

// 1. Network MTU Handler Tests
func TestNetworkMTUHandler(t *testing.T) {
	tmpDir := t.TempDir()
	origMtu := mtuFirewallFile
	mtuFirewallFile = filepath.Join(tmpDir, "firewall.user.mtu")
	t.Cleanup(func() {
		mtuFirewallFile = origMtu
	})

	h := NewNetworkMTUHandler()

	// GET MTU
	reqGet := httptest.NewRequest(http.MethodGet, "/api/network/mtu", nil)
	wGet := httptest.NewRecorder()
	h.GetMTU(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GetMTU returned %d, want 200", wGet.Code)
	}

	var getResp map[string]interface{}
	if err := json.NewDecoder(wGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode MTU response: %v", err)
	}
	if getResp["success"] != true {
		t.Errorf("expected success=true, got %v", getResp["success"])
	}

	// POST Set MTU - invalid JSON
	reqBad := httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBufferString(`invalid`))
	wBad := httptest.NewRecorder()
	h.SetMTU(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	// POST Set MTU - out of range
	bodyLow, _ := json.Marshal(MTUSavePayload{MTU: 500})
	reqLow := httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBuffer(bodyLow))
	wLow := httptest.NewRecorder()
	h.SetMTU(wLow, reqLow)
	if wLow.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for MTU < 576, got %d", wLow.Code)
	}

	// POST Set MTU - disable
	bodyDisable, _ := json.Marshal(MTUSavePayload{MTU: "disable"})
	reqDisable := httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBuffer(bodyDisable))
	wDisable := httptest.NewRecorder()
	h.SetMTU(wDisable, reqDisable)
	if wDisable.Code != http.StatusOK {
		t.Errorf("expected 200 for disable, got %d", wDisable.Code)
	}

	// POST Set MTU - valid 1420
	bodyValid, _ := json.Marshal(MTUSavePayload{MTU: 1420})
	reqValid := httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBuffer(bodyValid))
	wValid := httptest.NewRecorder()
	h.SetMTU(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Errorf("expected 200 for MTU 1420, got %d", wValid.Code)
	}
}

// 2. IP Passthrough Handler Tests
func TestIPPassthroughHandler(t *testing.T) {
	tmpDir := t.TempDir()
	origIppt := ipptConfigPath
	ipptConfigPath = filepath.Join(tmpDir, "ippt_config.json")
	t.Cleanup(func() {
		ipptConfigPath = origIppt
	})

	mock, eng := newTestNetworkEngine(t)
	h := NewIPPassthroughHandler(eng)

	// Mock AT responses for IPPT status
	mock.SetResponse(`AT+QMAP="MPDN_rule";+QMAP="IPPT_NAT";+QCFG="usbnet";+QMAP="DHCPV4DNS"`, `+QMAP: "MPDN_rule",0,1,0,1,1,"AA:BB:CC:DD:EE:FF"`+"\r\nOK")

	// GET Status
	reqGet := httptest.NewRequest(http.MethodGet, "/api/network/passthrough", nil)
	wGet := httptest.NewRecorder()
	h.Status(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("Status returned %d, want 200", wGet.Code)
	}

	var statusResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&statusResp)
	if statusResp["success"] != true {
		t.Errorf("expected success=true, got %v", statusResp["success"])
	}

	// POST Apply - invalid mode
	bodyInvalidMode, _ := json.Marshal(IPPTSavePayload{Mode: "invalid"})
	reqInvalidMode := httptest.NewRequest(http.MethodPost, "/api/network/passthrough", bytes.NewBuffer(bodyInvalidMode))
	wInvalidMode := httptest.NewRecorder()
	h.Apply(wInvalidMode, reqInvalidMode)
	if wInvalidMode.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid mode, got %d", wInvalidMode.Code)
	}

	// POST Apply - eth mode invalid MAC
	bodyBadMAC, _ := json.Marshal(IPPTSavePayload{Mode: "eth", MAC: "bad-mac"})
	reqBadMAC := httptest.NewRequest(http.MethodPost, "/api/network/passthrough", bytes.NewBuffer(bodyBadMAC))
	wBadMAC := httptest.NewRecorder()
	h.Apply(wBadMAC, reqBadMAC)
	if wBadMAC.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad MAC, got %d", wBadMAC.Code)
	}

	// POST Apply - valid disabled mode
	bodyDisabled, _ := json.Marshal(IPPTSavePayload{Mode: "disabled"})
	reqDisabled := httptest.NewRequest(http.MethodPost, "/api/network/passthrough", bytes.NewBuffer(bodyDisabled))
	wDisabled := httptest.NewRecorder()
	h.Apply(wDisabled, reqDisabled)
	if wDisabled.Code != http.StatusOK {
		t.Errorf("expected 200 for disabled mode, got %d", wDisabled.Code)
	}
}

// 3. Tailscale Handler Tests
func TestTailscaleHandler(t *testing.T) {
	h := NewTailscaleHandler()

	// GET Tailscale Status
	reqGet := httptest.NewRequest(http.MethodGet, "/api/vpn/tailscale", nil)
	wGet := httptest.NewRecorder()
	h.HandleTailscale(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleTailscale GET returned %d, want 200", wGet.Code)
	}

	// POST Tailscale - invalid JSON
	reqBad := httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBufferString(`invalid`))
	wBad := httptest.NewRecorder()
	h.HandleTailscale(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	// POST Tailscale - down action
	bodyDown, _ := json.Marshal(map[string]string{"action": "down"})
	reqDown := httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBuffer(bodyDown))
	wDown := httptest.NewRecorder()
	h.HandleTailscale(wDown, reqDown)
	if wDown.Code != http.StatusOK {
		t.Errorf("expected 200 for down action, got %d", wDown.Code)
	}

	// POST Tailscale - up action with auth_key and hostname
	bodyUp, _ := json.Marshal(map[string]interface{}{
		"action":     "up",
		"auth_key":   "tskey-auth-123456",
		"hostname":   "qmanager-modem",
		"enable_ssh": true,
	})
	reqUp := httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBuffer(bodyUp))
	wUp := httptest.NewRecorder()
	h.HandleTailscale(wUp, reqUp)
	if wUp.Code != http.StatusOK {
		t.Errorf("expected 200 for up action, got %d", wUp.Code)
	}
}

// 4. Ethernet Handler Tests
func TestEthernetHandler(t *testing.T) {
	tmpDir := t.TempDir()
	ethDir := filepath.Join(tmpDir, "eth0")
	_ = os.MkdirAll(ethDir, 0755)

	_ = os.WriteFile(filepath.Join(ethDir, "operstate"), []byte("up\n"), 0644)
	_ = os.WriteFile(filepath.Join(ethDir, "speed"), []byte("1000\n"), 0644)
	_ = os.WriteFile(filepath.Join(ethDir, "duplex"), []byte("full\n"), 0644)
	_ = os.WriteFile(filepath.Join(ethDir, "mtu"), []byte("1500\n"), 0644)

	h := &EthernetHandler{ifacePath: ethDir}

	req := httptest.NewRequest(http.MethodGet, "/api/network/ethernet", nil)
	w := httptest.NewRecorder()
	h.HandleEthernet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HandleEthernet returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["link_up"] != true {
		t.Errorf("expected link_up=true, got %v", resp["link_up"])
	}
	if resp["speed_mbps"].(float64) != 1000 {
		t.Errorf("expected speed_mbps=1000, got %v", resp["speed_mbps"])
	}
	if resp["duplex"] != "full" {
		t.Errorf("expected duplex='full', got %v", resp["duplex"])
	}

	// POST speed_limit as string
	bodyStr := bytes.NewBufferString(`{"speed_limit":"2500"}`)
	reqPostStr := httptest.NewRequest(http.MethodPost, "/api/network/ethernet", bodyStr)
	wPostStr := httptest.NewRecorder()
	h.HandleEthernet(wPostStr, reqPostStr)
	if wPostStr.Code != http.StatusOK {
		t.Fatalf("HandleEthernet POST string returned %d, want 200", wPostStr.Code)
	}
	var postRespStr map[string]interface{}
	_ = json.NewDecoder(wPostStr.Body).Decode(&postRespStr)
	if postRespStr["success"] != true || postRespStr["speed_limit"] != "2500" || postRespStr["disconnect_window_seconds"].(float64) != 8 {
		t.Errorf("unexpected post response: %+v", postRespStr)
	}

	// POST speed_limit as int
	bodyInt := bytes.NewBufferString(`{"speed_limit":100}`)
	reqPostInt := httptest.NewRequest(http.MethodPost, "/api/network/ethernet", bodyInt)
	wPostInt := httptest.NewRecorder()
	h.HandleEthernet(wPostInt, reqPostInt)
	if wPostInt.Code != http.StatusOK {
		t.Fatalf("HandleEthernet POST int returned %d, want 200", wPostInt.Code)
	}
	var postRespInt map[string]interface{}
	_ = json.NewDecoder(wPostInt.Body).Decode(&postRespInt)
	if postRespInt["success"] != true || postRespInt["speed_limit"] != "100" {
		t.Errorf("unexpected post response for int: %+v", postRespInt)
	}

	// POST speed_limit as 0 (auto)
	bodyZero := bytes.NewBufferString(`{"speed_limit":0}`)
	reqPostZero := httptest.NewRequest(http.MethodPost, "/api/network/ethernet", bodyZero)
	wPostZero := httptest.NewRecorder()
	h.HandleEthernet(wPostZero, reqPostZero)
	if wPostZero.Code != http.StatusOK {
		t.Fatalf("HandleEthernet POST zero returned %d, want 200", wPostZero.Code)
	}
	var postRespZero map[string]interface{}
	_ = json.NewDecoder(wPostZero.Body).Decode(&postRespZero)
	if postRespZero["speed_limit"] != "auto" {
		t.Errorf("expected speed_limit='auto', got %v", postRespZero["speed_limit"])
	}

	// Invalid speed_limit POST
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/network/ethernet", bytes.NewBufferString(`{"speed_limit":"5000"}`))
	wInvalid := httptest.NewRecorder()
	h.HandleEthernet(wInvalid, reqInvalid)
	if wInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid speed_limit, got %d", wInvalid.Code)
	}

	// Bad JSON POST
	reqBad := httptest.NewRequest(http.MethodPost, "/api/network/ethernet", bytes.NewBufferString(`{bad`))
	wBad := httptest.NewRecorder()
	h.HandleEthernet(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}
}

// 5. Custom DNS / Traffic Engine Handler Tests
func TestCustomDNSHandler(t *testing.T) {
	tmpDir := t.TempDir()
	origDnsmasq := dnsmasqConfPath
	origCustomDNS := customDNSConfig
	dnsmasqConfPath = filepath.Join(tmpDir, "dnsmasq.conf")
	customDNSConfig = filepath.Join(tmpDir, "custom_dns.json")
	t.Cleanup(func() {
		dnsmasqConfPath = origDnsmasq
		customDNSConfig = origCustomDNS
	})

	h := NewCustomDNSHandler()

	// GET DNS
	reqGet := httptest.NewRequest(http.MethodGet, "/api/network/dns", nil)
	wGet := httptest.NewRecorder()
	h.HandleGet(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleGet returned %d, want 200", wGet.Code)
	}

	// POST DNS - invalid JSON
	reqBad := httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBufferString(`bad`))
	wBad := httptest.NewRecorder()
	h.HandlePost(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	// POST DNS - enabled but no servers
	enabled := true
	bodyNoServers, _ := json.Marshal(CustomDNSSavePayload{Enabled: &enabled, Servers: []string{}})
	reqNoServers := httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyNoServers))
	wNoServers := httptest.NewRecorder()
	h.HandlePost(wNoServers, reqNoServers)
	if wNoServers.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty servers when enabled, got %d", wNoServers.Code)
	}

	// POST DNS - invalid IP format
	bodyInvalidIP, _ := json.Marshal(CustomDNSSavePayload{Enabled: &enabled, Servers: []string{"invalid.ip.address"}})
	reqInvalidIP := httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyInvalidIP))
	wInvalidIP := httptest.NewRecorder()
	h.HandlePost(wInvalidIP, reqInvalidIP)
	if wInvalidIP.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid IP, got %d", wInvalidIP.Code)
	}

	// POST DNS - valid servers
	bodyValid, _ := json.Marshal(CustomDNSSavePayload{
		Enabled: &enabled,
		Servers: []string{"1.1.1.1", "8.8.8.8"},
	})
	reqValid := httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyValid))
	wValid := httptest.NewRecorder()
	h.HandlePost(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid DNS, got %d", wValid.Code)
	}

	// POST DNS - clear action
	bodyClear, _ := json.Marshal(CustomDNSSavePayload{Action: "clear"})
	reqClear := httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyClear))
	wClear := httptest.NewRecorder()
	h.HandlePost(wClear, reqClear)
	if wClear.Code != http.StatusOK {
		t.Errorf("expected 200 for clear action, got %d", wClear.Code)
	}
}

func TestVideoOptimizer_RestoreHostlist(t *testing.T) {
	tmpDir := t.TempDir()
	origHostlist := dpiHostlistFile
	dpiHostlistFile = filepath.Join(tmpDir, "dpi_hostlist.txt")
	t.Cleanup(func() {
		dpiHostlistFile = origHostlist
	})

	h := NewVideoOptimizerHandler()

	// Write custom hostlist first
	_ = os.WriteFile(dpiHostlistFile, []byte("custom.example.com\n"), 0644)

	// Call restore_hostlist
	body, _ := json.Marshal(VideoOptimizerSavePayload{Action: "restore_hostlist"})
	req := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	h.HandlePost(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for restore_hostlist, got %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != true || resp["message"] != "Hostlist restored to default" {
		t.Errorf("unexpected restore_hostlist response: %+v", resp)
	}

	// Verify restored file contents
	content, err := os.ReadFile(dpiHostlistFile)
	if err != nil {
		t.Fatalf("failed to read restored hostlist: %v", err)
	}
	strContent := string(content)
	if !strings.Contains(strContent, "googlevideo.com") || !strings.Contains(strContent, "youtube.com") {
		t.Errorf("hostlist did not contain default domains: %s", strContent)
	}
}

func TestUpdateHandler_RebootAck(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	h := NewUpdateHandler(cfgMgr)

	// Test 1: POST /system/update?action=reboot_ack with empty body
	reqPostEmpty := httptest.NewRequest(http.MethodPost, "/api/system/update?action=reboot_ack", nil)
	wPostEmpty := httptest.NewRecorder()
	h.HandleUpdateAction(wPostEmpty, reqPostEmpty)
	if wPostEmpty.Code != http.StatusOK {
		t.Fatalf("expected 200 for POST empty body reboot_ack, got %d", wPostEmpty.Code)
	}
	var respPostEmpty map[string]interface{}
	_ = json.NewDecoder(wPostEmpty.Body).Decode(&respPostEmpty)
	dataEmpty, _ := respPostEmpty["data"].(map[string]interface{})
	if respPostEmpty["success"] != true || dataEmpty["message"] != "Reboot acknowledged" {
		t.Errorf("unexpected reboot_ack response: %+v", respPostEmpty)
	}

	// Test 2: POST /system/update with json {"action":"reboot_ack"}
	bodyJSON := bytes.NewBufferString(`{"action":"reboot_ack"}`)
	reqPostJSON := httptest.NewRequest(http.MethodPost, "/api/system/update", bodyJSON)
	wPostJSON := httptest.NewRecorder()
	h.HandleUpdateAction(wPostJSON, reqPostJSON)
	if wPostJSON.Code != http.StatusOK {
		t.Fatalf("expected 200 for POST JSON reboot_ack, got %d", wPostJSON.Code)
	}
	var respPostJSON map[string]interface{}
	_ = json.NewDecoder(wPostJSON.Body).Decode(&respPostJSON)
	dataJSON, _ := respPostJSON["data"].(map[string]interface{})
	if respPostJSON["success"] != true || dataJSON["message"] != "Reboot acknowledged" {
		t.Errorf("unexpected reboot_ack response: %+v", respPostJSON)
	}

	// Test 3: GET /system/update.sh?action=reboot_ack
	reqGet := httptest.NewRequest(http.MethodGet, "/cgi-bin/quecmanager/system/update.sh?action=reboot_ack", nil)
	wGet := httptest.NewRecorder()
	h.CheckUpdate(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET reboot_ack, got %d", wGet.Code)
	}
	var respGet map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&respGet)
	dataGet, _ := respGet["data"].(map[string]interface{})
	if respGet["success"] != true || dataGet["message"] != "Reboot acknowledged" {
		t.Errorf("unexpected GET reboot_ack response: %+v", respGet)
	}
}
