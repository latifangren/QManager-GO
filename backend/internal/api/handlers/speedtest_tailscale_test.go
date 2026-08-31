package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpeedtestAndTailscale_Deep(t *testing.T) {
	// 1. Speedtest Handler
	stH := NewSpeedtestHandler()

	// Check available
	wCheck := httptest.NewRecorder()
	stH.CheckAvailable(wCheck, httptest.NewRequest(http.MethodGet, "/api/diagnostics/speedtest/check", nil))
	if wCheck.Code != http.StatusOK {
		t.Fatalf("CheckAvailable returned %d", wCheck.Code)
	}

	// List servers
	wList := httptest.NewRecorder()
	stH.ListServers(wList, httptest.NewRequest(http.MethodGet, "/api/diagnostics/speedtest/servers", nil))
	if wList.Code != http.StatusNotFound && wList.Code != http.StatusOK && wList.Code != http.StatusInternalServerError {
		t.Errorf("ListServers unexpected code %d", wList.Code)
	}

	// Stop test when not running
	wStop := httptest.NewRecorder()
	stH.StopTest(wStop, httptest.NewRequest(http.MethodPost, "/api/diagnostics/speedtest/stop", nil))
	if wStop.Code != http.StatusOK {
		t.Fatalf("StopTest returned %d", wStop.Code)
	}

	// Get Status
	wProg := httptest.NewRecorder()
	stH.GetStatus(wProg, httptest.NewRequest(http.MethodGet, "/api/diagnostics/speedtest/status", nil))
	if wProg.Code != http.StatusOK {
		t.Fatalf("GetStatus returned %d", wProg.Code)
	}

	// 2. Tailscale Handler
	tsH := NewTailscaleHandler()

	// GET status
	wTsGet := httptest.NewRecorder()
	tsH.HandleTailscale(wTsGet, httptest.NewRequest(http.MethodGet, "/api/vpn/tailscale", nil))
	if wTsGet.Code != http.StatusOK {
		t.Fatalf("HandleTailscale GET returned %d", wTsGet.Code)
	}

	// POST actions: up, down, logout, default
	actions := []string{"up", "down", "logout"}
	for _, act := range actions {
		body, _ := json.Marshal(map[string]interface{}{
			"action":     act,
			"auth_key":   "tskey-auth-123456",
			"hostname":   "modem-gw",
			"enable_ssh": true,
		})
		wTsPost := httptest.NewRecorder()
		tsH.HandleTailscale(wTsPost, httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBuffer(body)))
		if wTsPost.Code != http.StatusOK {
			t.Errorf("HandleTailscale POST action %s returned %d, want 200", act, wTsPost.Code)
		}
	}

	// POST invalid JSON
	wTsBad := httptest.NewRecorder()
	tsH.HandleTailscale(wTsBad, httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBufferString("{invalid")))
	if wTsBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wTsBad.Code)
	}
}
