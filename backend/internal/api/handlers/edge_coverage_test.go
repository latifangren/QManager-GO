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
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func TestHandlers_EdgeCoverage(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)
	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 100)

	// 1. Auth ValidateToken
	authH := NewAuthHandler("secret")
	if authH.ValidateToken("invalid_tok") {
		t.Errorf("expected ValidateToken=false")
	}

	// 2. Cellular IMEI handleSaveBackup & writeImeiBackupConfig
	imeiH := NewCellularImeiHandler(eng, poller, cfgMgr)
	backupEnabled := true
	bodyBackup, _ := json.Marshal(ImeiSavePayload{
		Action:     "save_backup",
		Enabled:    &backupEnabled,
		BackupImei: "860123456789014", // Valid 15-digit Luhn
	})
	reqBackup := httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodyBackup))
	wBackup := httptest.NewRecorder()
	imeiH.SaveIMEI(wBackup, reqBackup)
	if wBackup.Code != http.StatusOK {
		t.Errorf("handleSaveBackup returned %d, want 200: %s", wBackup.Code, wBackup.Body.String())
	}

	// 2b. Cellular IMEI handleReboot
	bodyImeiReboot, _ := json.Marshal(ImeiSavePayload{Action: "reboot"})
	reqImeiReboot := httptest.NewRequest(http.MethodPost, "/api/cellular/imei", bytes.NewBuffer(bodyImeiReboot))
	wImeiReboot := httptest.NewRecorder()
	imeiH.SaveIMEI(wImeiReboot, reqImeiReboot)
	if wImeiReboot.Code != http.StatusOK {
		t.Errorf("IMEI handleReboot returned %d, want 200", wImeiReboot.Code)
	}

	// 3. Cellular MBN handleReboot
	mbnH := NewCellularMbnHandler(eng)
	bodyMbnReboot, _ := json.Marshal(MbnSavePayload{Action: "reboot"})
	reqMbnReboot := httptest.NewRequest(http.MethodPost, "/api/cellular/mbn", bytes.NewBuffer(bodyMbnReboot))
	wMbnReboot := httptest.NewRecorder()
	mbnH.SaveMBN(wMbnReboot, reqMbnReboot)
	if wMbnReboot.Code != http.StatusOK {
		t.Errorf("MBN handleReboot returned %d, want 200", wMbnReboot.Code)
	}

	// 4. Ethernet NewEthernetHandler constructor
	ethH := NewEthernetHandler()
	if ethH == nil {
		t.Errorf("NewEthernetHandler returned nil")
	}

	// 5. LanguagePacks NewLanguagePacksHandler constructor
	lp := NewLanguagePacksHandler()
	if lp == nil {
		t.Errorf("NewLanguagePacksHandler returned nil")
	}

	// 6. HealthCheck Download & Clear
	healthH := NewHealthCheckHandler(eng, poller, identity)
	// Run first so h.current is populated
	mock.SetResponse("AT", "OK")
	mock.SetResponse("AT+CPIN?", "+CPIN: READY\r\nOK")
	wRun := httptest.NewRecorder()
	healthH.Run(wRun, httptest.NewRequest(http.MethodPost, "/api/system/health-check/run", nil))

	reqDownload := httptest.NewRequest(http.MethodGet, "/api/diagnostics/health/download", nil)
	wDownload := httptest.NewRecorder()
	healthH.Download(wDownload, reqDownload)
	if wDownload.Code != http.StatusOK {
		t.Errorf("HealthCheck Download returned %d, want 200", wDownload.Code)
	}

	reqClear := httptest.NewRequest(http.MethodPost, "/api/diagnostics/health/clear", nil)
	wClear := httptest.NewRecorder()
	healthH.Clear(wClear, reqClear)
	if wClear.Code != http.StatusOK {
		t.Errorf("HealthCheck Clear returned %d, want 200", wClear.Code)
	}

	// 7. SMS ListSMS & DeleteSMS
	smsH := NewSMSHandler(eng)
	reqListSMS := httptest.NewRequest(http.MethodGet, "/api/cellular/sms/list", nil)
	wListSMS := httptest.NewRecorder()
	smsH.ListSMS(wListSMS, reqListSMS)
	if wListSMS.Code != http.StatusOK {
		t.Errorf("ListSMS returned %d, want 200", wListSMS.Code)
	}

	mock.SetResponse("AT+CMGD=1", "OK")
	reqDelSMS := httptest.NewRequest(http.MethodDelete, "/api/cellular/sms?index=1", nil)
	wDelSMS := httptest.NewRecorder()
	smsH.DeleteSMS(wDelSMS, reqDelSMS)
	if wDelSMS.Code != http.StatusOK {
		t.Errorf("DeleteSMS returned %d, want 200: %s", wDelSMS.Code, wDelSMS.Body.String())
	}
}
