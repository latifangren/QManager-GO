package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func TestSIMProfileHandler_Deactivate(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	simH := NewSIMProfileHandler(eng, tmpDir)
	simH.SetStoragePaths(
		filepath.Join(tmpDir, "profiles"),
		filepath.Join(tmpDir, "active_profile.txt"),
		filepath.Join(tmpDir, "profile_state.json"),
	)

	req := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/profiles/deactivate.sh", nil)
	w := httptest.NewRecorder()
	simH.Deactivate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Deactivate returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success=true in deactivate response, got %+v", resp)
	}
}

func TestCellularApnHandler_HandleDeactivate(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	confPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	apnH := NewCellularApnHandler(eng, cfgMgr, tmpDir)
	apnH.SetStoragePaths(
		filepath.Join(tmpDir, "apn_setting.json"),
		filepath.Join(tmpDir, "apn_names.json"),
	)

	w := httptest.NewRecorder()
	apnH.handleDeactivate(w)

	if w.Code != http.StatusOK {
		t.Fatalf("handleDeactivate returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success=true in deactivate response, got %+v", resp)
	}
}

func TestLanguagePacksHandler_InstallCancel(t *testing.T) {
	h := NewLanguagePacksHandler()

	req := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/system/language-packs/install_cancel.sh", nil)
	w := httptest.NewRecorder()
	h.InstallCancel(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("InstallCancel returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success=true, got %+v", resp)
	}
}

func TestTailscaleHandler_InstallStatusAndHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	origJSON := tailscaleInstallJSON
	origLog := tailscaleInstallLog
	tailscaleInstallJSON = filepath.Join(tmpDir, "tailscale_install.json")
	tailscaleInstallLog = filepath.Join(tmpDir, "tailscale_install.log")
	t.Cleanup(func() {
		tailscaleInstallJSON = origJSON
		tailscaleInstallLog = origLog
	})

	h := NewTailscaleHandler()

	// 1. Missing install status file returns idle
	wIdle := httptest.NewRecorder()
	h.handleInstallStatus(wIdle)
	if wIdle.Code != http.StatusOK {
		t.Fatalf("handleInstallStatus returned %d, want 200", wIdle.Code)
	}
	var idleResp map[string]interface{}
	_ = json.NewDecoder(wIdle.Body).Decode(&idleResp)
	if idleResp["status"] != "idle" || idleResp["success"] != true {
		t.Errorf("expected idle status for missing file, got %+v", idleResp)
	}

	// 2. Valid install status file
	writeInstallProgress("complete", "Installation succeeded", "Installed v1.92.5")
	wValid := httptest.NewRecorder()
	h.handleInstallStatus(wValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("handleInstallStatus returned %d, want 200", wValid.Code)
	}
	var validResp map[string]interface{}
	_ = json.NewDecoder(wValid.Body).Decode(&validResp)
	if validResp["status"] != "complete" || validResp["message"] != "Installation succeeded" {
		t.Errorf("unexpected valid install status: %+v", validResp)
	}

	// 3. Helper functions isTailscaleBootEnabled and isSSHEnabled (graceful non-fatal execution)
	_ = isTailscaleBootEnabled()
	_ = isSSHEnabled()

	// 4. POST actions on TailscaleHandler
	reqBad := httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBufferString("{bad-json"))
	wBad := httptest.NewRecorder()
	h.HandleTailscale(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	reqPost := httptest.NewRequest(http.MethodPost, "/api/vpn/tailscale", bytes.NewBufferString(`{"action":"install_status"}`))
	wPost := httptest.NewRecorder()
	h.HandleTailscale(wPost, reqPost)
	if wPost.Code != http.StatusOK {
		t.Errorf("expected 200 for action=install_status, got %d", wPost.Code)
	}
}
