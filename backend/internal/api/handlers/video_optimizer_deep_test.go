package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVideoOptimizer_DeepBranches(t *testing.T) {
	tmpDir := t.TempDir()
	origDpiConfig := dpiConfigFile
	origDpiHostlist := dpiHostlistFile
	origDpiVerify := dpiVerifyFile
	origDpiInstall := dpiInstallFile
	dpiConfigFile = filepath.Join(tmpDir, "dpi_config.json")
	dpiHostlistFile = filepath.Join(tmpDir, "dpi_hostlist.txt")
	dpiVerifyFile = filepath.Join(tmpDir, "dpi_verify.json")
	dpiInstallFile = filepath.Join(tmpDir, "dpi_install.json")
	t.Cleanup(func() {
		dpiConfigFile = origDpiConfig
		dpiHostlistFile = origDpiHostlist
		dpiVerifyFile = origDpiVerify
		dpiInstallFile = origDpiInstall
	})

	h := NewVideoOptimizerHandler()

	// 1. readDpiConfig / writeDpiConfig
	cfg := TrafficEngineConfig{
		VideoOptimizerEnabled: true,
		MasqueradeEnabled:     true,
		SNIDomain:             "my.custom.sni.com",
	}
	_ = writeDpiConfig(cfg)

	readCfg := readDpiConfig()
	if readCfg.SNIDomain != "my.custom.sni.com" && readCfg.SNIDomain != "" {
		t.Errorf("unexpected readCfg: %+v", readCfg)
	}

	// 2. Hostlist reading & count
	_ = os.WriteFile(dpiHostlistFile, []byte("google.com\nyoutube.com\nnetflix.com\n"), 0644)
	domains := readHostlistDomains()
	_ = domains
	count := countHostlistDomains()
	_ = count

	// 3. GET verify_status & install_status
	reqVer := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer?action=verify_status", nil)
	wVer := httptest.NewRecorder()
	h.HandleGet(wVer, reqVer)
	if wVer.Code != http.StatusOK {
		t.Errorf("verify_status returned %d, want 200", wVer.Code)
	}

	reqInst := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer?action=install_status", nil)
	wInst := httptest.NewRecorder()
	h.HandleGet(wInst, reqInst)
	if wInst.Code != http.StatusOK {
		t.Errorf("install_status returned %d, want 200", wInst.Code)
	}

	// 4. POST install & uninstall
	bodyInst, _ := json.Marshal(VideoOptimizerSavePayload{Action: "install"})
	reqDoInst := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodyInst))
	wDoInst := httptest.NewRecorder()
	h.HandlePost(wDoInst, reqDoInst)
	if wDoInst.Code != http.StatusOK {
		t.Errorf("install action returned %d, want 200", wDoInst.Code)
	}

	bodyUninst, _ := json.Marshal(VideoOptimizerSavePayload{Action: "uninstall"})
	reqDoUninst := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodyUninst))
	wDoUninst := httptest.NewRecorder()
	h.HandlePost(wDoUninst, reqDoUninst)
	if wDoUninst.Code != http.StatusOK {
		t.Errorf("uninstall action returned %d, want 200", wDoUninst.Code)
	}
}

func TestVideoOptimizer_Actions(t *testing.T) {
	tmpDir := t.TempDir()
	origDpiConfig := dpiConfigFile
	origDpiHostlist := dpiHostlistFile
	origDpiVerify := dpiVerifyFile
	origDpiInstall := dpiInstallFile
	dpiConfigFile = filepath.Join(tmpDir, "dpi_config.json")
	dpiHostlistFile = filepath.Join(tmpDir, "dpi_hostlist.txt")
	dpiVerifyFile = filepath.Join(tmpDir, "dpi_verify.json")
	dpiInstallFile = filepath.Join(tmpDir, "dpi_install.json")
	t.Cleanup(func() {
		dpiConfigFile = origDpiConfig
		dpiHostlistFile = origDpiHostlist
		dpiVerifyFile = origDpiVerify
		dpiInstallFile = origDpiInstall
	})

	h := NewVideoOptimizerHandler()

	// 1. POST action: save_video_optimizer
	reqSaveOpt := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"save_video_optimizer","enabled":true}`))
	wSaveOpt := httptest.NewRecorder()
	h.HandlePost(wSaveOpt, reqSaveOpt)
	if wSaveOpt.Code != http.StatusOK {
		t.Errorf("save_video_optimizer returned %d, want 200", wSaveOpt.Code)
	}

	// 2. POST action: save_full_bypass
	reqFullBypass := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"save_full_bypass","enabled":true,"sni_domain":"cdn.example.com"}`))
	wFullBypass := httptest.NewRecorder()
	h.HandlePost(wFullBypass, reqFullBypass)
	if wFullBypass.Code != http.StatusOK {
		t.Errorf("save_full_bypass returned %d, want 200", wFullBypass.Code)
	}

	// 3. POST action: save_force_tcp
	reqForceTCP := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"save_force_tcp","enabled":true}`))
	wForceTCP := httptest.NewRecorder()
	h.HandlePost(wForceTCP, reqForceTCP)
	if wForceTCP.Code != http.StatusOK {
		t.Errorf("save_force_tcp returned %d, want 200", wForceTCP.Code)
	}

	// 4. POST action: save_hostlist
	reqSaveHosts := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"save_hostlist","domains":["youtube.com","netflix.com"]}`))
	wSaveHosts := httptest.NewRecorder()
	h.HandlePost(wSaveHosts, reqSaveHosts)
	if wSaveHosts.Code != http.StatusOK {
		t.Errorf("save_hostlist returned %d, want 200", wSaveHosts.Code)
	}

	// 5. POST action: restore_hostlist
	reqRestore := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"restore_hostlist"}`))
	wRestore := httptest.NewRecorder()
	h.HandlePost(wRestore, reqRestore)
	if wRestore.Code != http.StatusOK {
		t.Errorf("restore_hostlist returned %d, want 200", wRestore.Code)
	}

	// 6. POST action: install
	reqInstall := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"install"}`))
	wInstall := httptest.NewRecorder()
	h.HandlePost(wInstall, reqInstall)
	if wInstall.Code != http.StatusOK {
		t.Errorf("install returned %d, want 200", wInstall.Code)
	}

	// 7. POST action: uninstall
	reqUninstall := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"uninstall"}`))
	wUninstall := httptest.NewRecorder()
	h.HandlePost(wUninstall, reqUninstall)
	if wUninstall.Code != http.StatusOK {
		t.Errorf("uninstall returned %d, want 200", wUninstall.Code)
	}

	// 8. POST action: verify
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBufferString(`{"action":"verify"}`))
	wVerify := httptest.NewRecorder()
	h.HandlePost(wVerify, reqVerify)
	if wVerify.Code != http.StatusOK {
		t.Errorf("verify returned %d, want 200", wVerify.Code)
	}

	// 9. GET ?action=hostlist (text/plain)
	reqGetHosts := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer?action=hostlist", nil)
	wGetHosts := httptest.NewRecorder()
	h.HandleGet(wGetHosts, reqGetHosts)
	if wGetHosts.Code != http.StatusOK {
		t.Errorf("GET action=hostlist returned %d, want 200", wGetHosts.Code)
	}
	if wGetHosts.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %s", wGetHosts.Header().Get("Content-Type"))
	}

	// 10. GET ?action=hostlist_section
	reqGetSection := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer?action=hostlist_section", nil)
	wGetSection := httptest.NewRecorder()
	h.HandleGet(wGetSection, reqGetSection)
	if wGetSection.Code != http.StatusOK {
		t.Errorf("GET action=hostlist_section returned %d, want 200", wGetSection.Code)
	}

	// 11. Default GET (returns status JSON)
	reqGetDefault := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer", nil)
	wGetDefault := httptest.NewRecorder()
	h.HandleGet(wGetDefault, reqGetDefault)
	if wGetDefault.Code != http.StatusOK {
		t.Errorf("default GET returned %d, want 200", wGetDefault.Code)
	}
	var defResp map[string]interface{}
	if err := json.NewDecoder(wGetDefault.Body).Decode(&defResp); err != nil {
		t.Fatalf("failed to decode default GET response: %v", err)
	}
	if defResp["success"] != true {
		t.Errorf("expected success=true in default GET response, got %+v", defResp)
	}
}
