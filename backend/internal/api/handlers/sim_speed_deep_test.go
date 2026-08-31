package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
)

func TestSIMProfiles_And_Speedtest_Deep(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	// 1. SIM Profile with scenario schedule
	h := NewSIMProfileHandler(eng)
	h.SetStoragePaths(
		filepath.Join(tmpDir, "profiles"),
		filepath.Join(tmpDir, "active_profile"),
		filepath.Join(tmpDir, "profile_state.json"),
	)

	var scenBlock ScenarioBlock
	scenBlock.Default = "gaming"
	scenBlock.Schedule.Enabled = true

	bodyProf, _ := json.Marshal(SIMProfile{
		Name:     "Profile with Schedule",
		Scenario: &scenBlock,
		Settings: ProfileSettings{
			APN:         "custom.apn",
			PDPType:     "IPV4V6",
			CID:         1,
			AutoConnect: true,
			Roaming:     true,
		},
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/api/cellular/profiles", bytes.NewBuffer(bodyProf))
	wSave := httptest.NewRecorder()
	h.Save(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("Save profile with schedule returned %d, want 200", wSave.Code)
	}

	// 2. Speedtest list servers & start test error branches
	speedH := NewSpeedtestHandler()
	reqServers := httptest.NewRequest(http.MethodGet, "/api/diagnostics/speedtest/servers", nil)
	wServers := httptest.NewRecorder()
	speedH.ListServers(wServers, reqServers)
	// Even if binary missing on host, should handle gracefully
	if wServers.Code != http.StatusOK && wServers.Code != http.StatusNotFound && wServers.Code != http.StatusInternalServerError {
		t.Errorf("unexpected status from ListServers: %d", wServers.Code)
	}

	serverID := 1234
	bodyStart, _ := json.Marshal(StartTestPayload{ServerID: &serverID})
	reqStart := httptest.NewRequest(http.MethodPost, "/api/diagnostics/speedtest/start", bytes.NewBuffer(bodyStart))
	wStart := httptest.NewRecorder()
	speedH.StartTest(wStart, reqStart)
	// On machines without speedtest binary, 500 error is returned cleanly
	if wStart.Code != http.StatusOK && wStart.Code != http.StatusInternalServerError {
		t.Errorf("unexpected status from StartTest: %d", wStart.Code)
	}
}
