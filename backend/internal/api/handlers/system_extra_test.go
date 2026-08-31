package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/config"
	"qmanager/internal/platform"
)

// 1. System Handler Tests (system.go)
func TestSystemHandler_Full(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	identity := platform.Identity{
		Model:      "RG501Q-EU",
		Revision:   "RG501QEUAAR12A08M4G",
		SoC:        "SDX55",
		CustomName: "STD",
		Serial:     "12345678",
		IsSDX55:    true,
	}

	h := NewSystemHandler(identity, cfgMgr)

	// Info
	reqInfo := httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
	wInfo := httptest.NewRecorder()
	h.Info(wInfo, reqInfo)
	if wInfo.Code != http.StatusOK {
		t.Fatalf("Info returned %d, want 200", wInfo.Code)
	}

	// GetConfig
	reqGetCfg := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	wGetCfg := httptest.NewRecorder()
	h.GetConfig(wGetCfg, reqGetCfg)
	if wGetCfg.Code != http.StatusOK {
		t.Fatalf("GetConfig returned %d, want 200", wGetCfg.Code)
	}

	// SaveConfig - bad JSON
	reqBadCfg := httptest.NewRequest(http.MethodPost, "/api/system/config", bytes.NewBufferString(`bad`))
	wBadCfg := httptest.NewRecorder()
	h.SaveConfig(wBadCfg, reqBadCfg)
	if wBadCfg.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad config payload, got %d", wBadCfg.Code)
	}

	// SaveConfig - valid
	newCfg := cfgMgr.Get()
	newCfg.Settings.Hostname = "CustomQManager"
	newCfg.Settings.Timezone = "WIB-7"
	bodyCfg, _ := json.Marshal(newCfg)
	reqSaveCfg := httptest.NewRequest(http.MethodPost, "/api/system/config", bytes.NewBuffer(bodyCfg))
	wSaveCfg := httptest.NewRecorder()
	h.SaveConfig(wSaveCfg, reqSaveCfg)
	if wSaveCfg.Code != http.StatusOK {
		t.Fatalf("SaveConfig returned %d, want 200", wSaveCfg.Code)
	}

	if cfgMgr.Get().Settings.Hostname != "CustomQManager" {
		t.Errorf("expected updated hostname 'CustomQManager', got %s", cfgMgr.Get().Settings.Hostname)
	}

	// Reboot
	reqReboot := httptest.NewRequest(http.MethodPost, "/api/system/reboot", nil)
	wReboot := httptest.NewRecorder()
	h.Reboot(wReboot, reqReboot)
	if wReboot.Code != http.StatusOK {
		t.Fatalf("Reboot returned %d, want 200", wReboot.Code)
	}
}

// 2. Update Handler Tests (update.go)
func TestUpdateHandler_Full(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	h := NewUpdateHandler(cfgMgr)

	// CheckUpdate - status action
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/system/update?action=status", nil)
	wStatus := httptest.NewRecorder()
	h.CheckUpdate(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("CheckUpdate status returned %d, want 200", wStatus.Code)
	}

	// CheckUpdate - full check
	reqCheck := httptest.NewRequest(http.MethodGet, "/api/system/update", nil)
	wCheck := httptest.NewRecorder()
	h.CheckUpdate(wCheck, reqCheck)
	if wCheck.Code != http.StatusOK {
		t.Fatalf("CheckUpdate returned %d, want 200", wCheck.Code)
	}

	// HandleUpdateAction - save_settings
	autoUpdate := true
	includePre := false
	bodySettings, _ := json.Marshal(map[string]interface{}{
		"action":              "save_settings",
		"auto_update_enabled": &autoUpdate,
		"auto_update_time":    "03:30",
		"include_prerelease":  &includePre,
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/api/system/update", bytes.NewBuffer(bodySettings))
	wSave := httptest.NewRecorder()
	h.HandleUpdateAction(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("HandleUpdateAction save_settings returned %d, want 200: %s", wSave.Code, wSave.Body.String())
	}

	if cfgMgr.Get().Update.AutoUpdateEnabled != 1 || cfgMgr.Get().Update.AutoUpdateTime != "03:30" {
		t.Errorf("saved update settings mismatch: %+v", cfgMgr.Get().Update)
	}

	// HandleUpdateAction - cancel_download
	bodyCancel, _ := json.Marshal(map[string]string{"action": "cancel_download"})
	reqCancel := httptest.NewRequest(http.MethodPost, "/api/system/update", bytes.NewBuffer(bodyCancel))
	wCancel := httptest.NewRecorder()
	h.HandleUpdateAction(wCancel, reqCancel)
	if wCancel.Code != http.StatusOK {
		t.Fatalf("HandleUpdateAction cancel_download returned %d, want 200", wCancel.Code)
	}

	// HandleUpdateAction - reboot_ack
	bodyRebootAck, _ := json.Marshal(map[string]string{"action": "reboot_ack"})
	reqRebootAck := httptest.NewRequest(http.MethodPost, "/api/system/update", bytes.NewBuffer(bodyRebootAck))
	wRebootAck := httptest.NewRecorder()
	h.HandleUpdateAction(wRebootAck, reqRebootAck)
	if wRebootAck.Code != http.StatusOK {
		t.Fatalf("HandleUpdateAction reboot_ack returned %d, want 200", wRebootAck.Code)
	}
}
