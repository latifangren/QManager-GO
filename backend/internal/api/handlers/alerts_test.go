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

func TestAlertsPersistence_AndReboot(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerts_config.json")

	origAlertsPath := defaultAlertsConfigPath
	defaultAlertsConfigPath = cfgPath
	defer func() { defaultAlertsConfigPath = origAlertsPath }()

	h := NewAlertsHandler()

	// 1. Initial defaults when file doesn't exist
	reqGet := httptest.NewRequest(http.MethodGet, "/api/monitoring/alerts", nil)
	wGet := httptest.NewRecorder()
	h.HandleAlerts(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleAlerts GET returned %d", wGet.Code)
	}
	var getResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&getResp)
	channels := getResp["channels"].(map[string]interface{})
	smsChan := channels["sms"].(map[string]interface{})
	if smsChan["enabled"] != false || smsChan["recipient_phone"] != "" {
		t.Errorf("expected default SMS channel unconfigured, got: %+v", smsChan)
	}

	// 2. Update alert configuration via POST
	savePayload := map[string]interface{}{
		"sms": map[string]interface{}{
			"enabled":           true,
			"recipient_phone":   "+15551234567",
			"threshold_minutes": 10,
			"configured":        true,
		},
		"email": map[string]interface{}{
			"enabled":           true,
			"sender_email":      "modem@example.com",
			"recipient_email":   "admin@example.com",
			"app_password_set":  true,
			"threshold_minutes": 15,
			"configured":        true,
		},
		"discord": map[string]interface{}{
			"enabled":          true,
			"owner_discord_id": "123456789012345678",
			"token_set":        true,
			"configured":       true,
		},
		"routing": map[string]interface{}{
			"events": map[string]interface{}{
				"connection_lost":     map[string]bool{"sms": true, "email": true, "discord": true},
				"connection_restored": map[string]bool{"sms": true, "email": false, "discord": true},
				"reboot":              map[string]bool{"sms": true, "email": false, "discord": false},
			},
		},
	}
	bodySave, _ := json.Marshal(savePayload)
	reqSave := httptest.NewRequest(http.MethodPost, "/api/monitoring/alerts", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandleAlerts(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("HandleAlerts POST failed: %d", wSave.Code)
	}

	// 3. Verify file exists on disk and contains saved config
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read alerts config file: %v", err)
	}
	var rawDiskMap map[string]interface{}
	if err := json.Unmarshal(data, &rawDiskMap); err != nil {
		t.Fatalf("failed to parse json on disk: %v", err)
	}
	diskSMS := rawDiskMap["sms"].(map[string]interface{})
	if diskSMS["recipient_phone"] != "+15551234567" || diskSMS["enabled"] != true {
		t.Errorf("saved SMS config mismatch on disk: %+v", diskSMS)
	}

	// 4. Send test alert to generate log entries in RAM
	testPayload := map[string]interface{}{
		"action":  "test",
		"channel": "sms",
		"message": "Critical cell handoff failed",
	}
	bodyTest, _ := json.Marshal(testPayload)
	reqTest := httptest.NewRequest(http.MethodPost, "/api/monitoring/alerts", bytes.NewBuffer(bodyTest))
	wTest := httptest.NewRecorder()
	h.HandleAlerts(wTest, reqTest)
	if wTest.Code != http.StatusOK {
		t.Fatalf("test alert failed: %d", wTest.Code)
	}

	// Verify log entry exists in RAM
	reqLog := httptest.NewRequest(http.MethodGet, "/api/monitoring/alerts?action=get_log", nil)
	wLog := httptest.NewRecorder()
	h.HandleAlerts(wLog, reqLog)
	var logResp map[string]interface{}
	_ = json.NewDecoder(wLog.Body).Decode(&logResp)
	if logResp["total"].(float64) != 1 {
		t.Fatalf("expected 1 log entry in RAM, got: %+v", logResp)
	}

	// 5. Verify Zero Flash Wear: Logs are strictly volatile in RAM and NEVER written to flash
	dataAfterLog, _ := os.ReadFile(cfgPath)
	var diskMapAfterLog map[string]interface{}
	_ = json.Unmarshal(dataAfterLog, &diskMapAfterLog)
	if _, hasLogs := diskMapAfterLog["logs"]; hasLogs {
		t.Errorf("VIOLATION of Zero Flash Wear policy: logs found in persisted JSON file!")
	}

	// 6. Simulate modem reboot: NewAlertsHandler restores config from disk
	hReboot := NewAlertsHandler()

	wGetReboot := httptest.NewRecorder()
	hReboot.HandleAlerts(wGetReboot, reqGet)
	var getRespReboot map[string]interface{}
	_ = json.NewDecoder(wGetReboot.Body).Decode(&getRespReboot)

	rebootChannels := getRespReboot["channels"].(map[string]interface{})
	rebootSMS := rebootChannels["sms"].(map[string]interface{})
	if rebootSMS["recipient_phone"] != "+15551234567" || rebootSMS["enabled"] != true {
		t.Errorf("expected SMS config restored after reboot, got: %+v", rebootSMS)
	}
	rebootEmail := rebootChannels["email"].(map[string]interface{})
	if rebootEmail["sender_email"] != "modem@example.com" || rebootEmail["enabled"] != true {
		t.Errorf("expected Email config restored after reboot, got: %+v", rebootEmail)
	}

	// 7. Verify alert logs were volatile and are empty after reboot
	wLogReboot := httptest.NewRecorder()
	hReboot.HandleAlerts(wLogReboot, reqLog)
	var logRespReboot map[string]interface{}
	_ = json.NewDecoder(wLogReboot.Body).Decode(&logRespReboot)
	if logRespReboot["total"].(float64) != 0 {
		t.Errorf("expected 0 log entries after reboot, got %v", logRespReboot["total"])
	}
}

func TestAlertsHandler_SetStoragePath(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom_alerts.json")

	h := NewAlertsHandler()
	h.SetStoragePath(customPath)

	bodySave, _ := json.Marshal(map[string]interface{}{
		"sms": map[string]interface{}{
			"enabled":         true,
			"recipient_phone": "+999000",
		},
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/api/monitoring/alerts", bytes.NewBuffer(bodySave))
	wSave := httptest.NewRecorder()
	h.HandleAlerts(wSave, reqSave)

	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Fatalf("expected custom path file to be created, err: %v", err)
	}

	h2 := NewAlertsHandler()
	h2.SetConfigPath(customPath)
	_ = h2.LoadConfig()

	reqGet := httptest.NewRequest(http.MethodGet, "/api/monitoring/alerts", nil)
	wGet := httptest.NewRecorder()
	h2.HandleAlerts(wGet, reqGet)
	var getResp map[string]interface{}
	_ = json.NewDecoder(wGet.Body).Decode(&getResp)
	rebootSMS := getResp["channels"].(map[string]interface{})["sms"].(map[string]interface{})
	if rebootSMS["recipient_phone"] != "+999000" {
		t.Errorf("expected custom path to reload properly, got: %+v", rebootSMS)
	}
}
