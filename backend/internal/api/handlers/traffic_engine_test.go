package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCustomDNSHandler_Deep(t *testing.T) {
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

	// 1. GET DNS
	wGet := httptest.NewRecorder()
	h.HandleGet(wGet, httptest.NewRequest(http.MethodGet, "/api/network/dns", nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleGet returned %d, want 200", wGet.Code)
	}

	// 2. POST save valid DNS
	enabledTrue := true
	bodySave, _ := json.Marshal(CustomDNSSavePayload{
		Action:  "save",
		Enabled: &enabledTrue,
		Servers: []string{"1.1.1.1", "8.8.8.8"},
	})
	wSave := httptest.NewRecorder()
	h.HandlePost(wSave, httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodySave)))
	if wSave.Code != http.StatusOK {
		t.Fatalf("HandlePost save returned %d: %s", wSave.Code, wSave.Body.String())
	}

	// 3. POST clear DNS
	bodyClear, _ := json.Marshal(CustomDNSSavePayload{
		Action: "clear",
	})
	wClear := httptest.NewRecorder()
	h.HandlePost(wClear, httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyClear)))
	if wClear.Code != http.StatusOK {
		t.Fatalf("HandlePost clear returned %d", wClear.Code)
	}

	// 4. Validation errors: invalid IP, too many servers, empty servers when enabled
	bodyEmpty, _ := json.Marshal(CustomDNSSavePayload{
		Action:  "save",
		Enabled: &enabledTrue,
		Servers: []string{},
	})
	wEmpty := httptest.NewRecorder()
	h.HandlePost(wEmpty, httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyEmpty)))
	if wEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty servers, got %d", wEmpty.Code)
	}

	bodyBadIP, _ := json.Marshal(CustomDNSSavePayload{
		Action:  "save",
		Enabled: &enabledTrue,
		Servers: []string{"invalid.ip.address"},
	})
	wBadIP := httptest.NewRecorder()
	h.HandlePost(wBadIP, httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyBadIP)))
	if wBadIP.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad IP, got %d", wBadIP.Code)
	}

	bodyTooMany, _ := json.Marshal(CustomDNSSavePayload{
		Action:  "save",
		Enabled: &enabledTrue,
		Servers: []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9"},
	})
	wTooMany := httptest.NewRecorder()
	h.HandlePost(wTooMany, httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyTooMany)))
	if wTooMany.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for >4 servers, got %d", wTooMany.Code)
	}
}
