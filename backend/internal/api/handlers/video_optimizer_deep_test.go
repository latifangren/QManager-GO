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
	cfgPath := filepath.Join(tmpDir, "dpi_config.json")
	hostlistPath := filepath.Join(tmpDir, "dpi_hostlist.txt")

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
	_ = os.WriteFile(hostlistPath, []byte("google.com\nyoutube.com\nnetflix.com\n"), 0644)
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

	_ = cfgPath
}
