package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
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
}

// 5. Custom DNS / Traffic Engine Handler Tests
func TestCustomDNSHandler(t *testing.T) {
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
