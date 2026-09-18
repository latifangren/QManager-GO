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

func TestBandFailoverHandler_StateAccessors(t *testing.T) {
	h := NewBandFailoverHandler()

	// Initial state
	enabled, activated, watcherRunning := h.GetState()
	if enabled || activated || watcherRunning {
		t.Errorf("expected initial state all false, got enabled=%v, activated=%v, watcherRunning=%v", enabled, activated, watcherRunning)
	}
	if h.IsEnabled() {
		t.Errorf("expected IsEnabled() = false")
	}

	// SetEnabled
	h.SetEnabled(true)
	if !h.IsEnabled() {
		t.Errorf("expected IsEnabled() = true after SetEnabled(true)")
	}
	enabled, _, _ = h.GetState()
	if !enabled {
		t.Errorf("expected enabled = true in GetState()")
	}

	// SetActivated
	h.SetActivated(true)
	_, activated, _ = h.GetState()
	if !activated {
		t.Errorf("expected activated = true in GetState()")
	}

	// SetWatcherRunning
	h.SetWatcherRunning(true)
	_, _, watcherRunning = h.GetState()
	if !watcherRunning {
		t.Errorf("expected watcherRunning = true in GetState()")
	}

	// SetState bulk update
	h.SetState(false, false, false)
	enabled, activated, watcherRunning = h.GetState()
	if enabled || activated || watcherRunning {
		t.Errorf("expected all false after SetState(false, false, false)")
	}
	if h.IsEnabled() {
		t.Errorf("expected IsEnabled() = false")
	}
}

func TestBandFailoverHandler_ServeHTTP(t *testing.T) {
	h := NewBandFailoverHandler()

	// 1. GET returns status
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cellular/bands/failover/status", nil)
	wGet := httptest.NewRecorder()
	h.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d", wGet.Code)
	}

	var statusResp map[string]interface{}
	if err := json.NewDecoder(wGet.Body).Decode(&statusResp); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}
	if statusResp["enabled"] != false {
		t.Errorf("expected enabled=false in GET response, got %v", statusResp["enabled"])
	}

	// 2. POST toggle with explicit payload: enabled = true
	bodyTrue, _ := json.Marshal(map[string]interface{}{"enabled": true})
	reqPost1 := httptest.NewRequest(http.MethodPost, "/api/cellular/bands/failover/toggle", bytes.NewBuffer(bodyTrue))
	wPost1 := httptest.NewRecorder()
	h.ServeHTTP(wPost1, reqPost1)

	if wPost1.Code != http.StatusOK {
		t.Fatalf("expected POST status 200, got %d", wPost1.Code)
	}
	if !h.IsEnabled() {
		t.Errorf("expected handler enabled = true after POST")
	}

	// 3. POST toggle with empty payload inverts enabled
	reqPost2 := httptest.NewRequest(http.MethodPost, "/api/cellular/bands/failover/toggle", bytes.NewBufferString("{}"))
	wPost2 := httptest.NewRecorder()
	h.ServeHTTP(wPost2, reqPost2)

	if wPost2.Code != http.StatusOK {
		t.Fatalf("expected POST status 200, got %d", wPost2.Code)
	}
	if h.IsEnabled() {
		t.Errorf("expected handler enabled = false after toggle without payload")
	}

	// 4. Unsupported method
	reqDelete := httptest.NewRequest(http.MethodDelete, "/api/cellular/bands/failover", nil)
	wDelete := httptest.NewRecorder()
	h.ServeHTTP(wDelete, reqDelete)
	if wDelete.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for DELETE, got %d", wDelete.Code)
	}
}

func TestBandFailover_PersistenceAndReboot(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "band_failover.json")

	origFailoverPath := defaultBandFailoverConfigPath
	defaultBandFailoverConfigPath = cfgPath
	defer func() { defaultBandFailoverConfigPath = origFailoverPath }()

	h := NewBandFailoverHandler()

	// 1. Initial state: unpersisted defaults
	if h.IsEnabled() {
		t.Errorf("expected failover to be initially disabled")
	}

	// 2. Toggle to enabled via HTTP POST
	bodyTrue, _ := json.Marshal(map[string]interface{}{"enabled": true})
	reqToggle := httptest.NewRequest(http.MethodPost, "/api/cellular/bands/failover/toggle", bytes.NewBuffer(bodyTrue))
	wToggle := httptest.NewRecorder()
	h.Toggle(wToggle, reqToggle)
	if wToggle.Code != http.StatusOK {
		t.Fatalf("Toggle returned %d", wToggle.Code)
	}

	// 3. Verify band_failover.json created via platform.AtomicWriteFile
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read band failover config file: %v", err)
	}
	var savedCfg BandFailoverConfig
	if err := json.Unmarshal(data, &savedCfg); err != nil {
		t.Fatalf("failed to parse band failover JSON: %v", err)
	}
	if !savedCfg.Enabled {
		t.Errorf("expected enabled=true on disk, got false")
	}
	if len(savedCfg.FailoverBands) == 0 {
		t.Errorf("expected failover bands on disk, got empty")
	}

	// 4. Set runtime volatile states in RAM
	h.SetActivated(true)
	h.SetWatcherRunning(true)
	enabled, activated, watcherRunning := h.GetState()
	if !enabled || !activated || !watcherRunning {
		t.Fatalf("expected all true in RAM state: enabled=%v, activated=%v, watcherRunning=%v", enabled, activated, watcherRunning)
	}

	// Verify volatile runtime states are NEVER written to flash
	dataAfterRun, _ := os.ReadFile(cfgPath)
	var diskMap map[string]interface{}
	_ = json.Unmarshal(dataAfterRun, &diskMap)
	if _, hasAct := diskMap["activated"]; hasAct {
		t.Errorf("VIOLATION of Zero Flash Wear policy: activated found in persisted JSON!")
	}
	if _, hasWatch := diskMap["watcher_running"]; hasWatch {
		t.Errorf("VIOLATION of Zero Flash Wear policy: watcher_running found in persisted JSON!")
	}

	// 5. Simulate modem reboot: NewBandFailoverHandler reads config from disk
	hReboot := NewBandFailoverHandler()
	rebootEnabled, rebootActivated, rebootWatcher := hReboot.GetState()
	if !rebootEnabled {
		t.Errorf("expected enabled=true restored after reboot")
	}
	// Volatile states must have reverted to false
	if rebootActivated {
		t.Errorf("expected activated=false after reboot, got true")
	}
	if rebootWatcher {
		t.Errorf("expected watcherRunning=false after reboot, got true")
	}

	// 6. Toggle failover to false via HTTP POST
	reqToggleOff := httptest.NewRequest(http.MethodPost, "/api/cellular/bands/failover/toggle", bytes.NewBufferString("{}"))
	wToggleOff := httptest.NewRecorder()
	hReboot.Toggle(wToggleOff, reqToggleOff)
	if wToggleOff.Code != http.StatusOK {
		t.Fatalf("Toggle off returned %d", wToggleOff.Code)
	}
	if hReboot.IsEnabled() {
		t.Errorf("expected failover disabled after toggle")
	}

	// Verify persisted file updated to enabled=false
	dataDisabled, _ := os.ReadFile(cfgPath)
	var disabledCfg BandFailoverConfig
	_ = json.Unmarshal(dataDisabled, &disabledCfg)
	if disabledCfg.Enabled {
		t.Errorf("expected enabled=false on disk after toggle, got true")
	}

	// 7. Simulate reboot after disable
	hReboot2 := NewBandFailoverHandler()
	if hReboot2.IsEnabled() {
		t.Errorf("expected enabled=false after reboot with disabled config")
	}
}

func TestBandFailover_SetStoragePath(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom_failover.json")

	h := NewBandFailoverHandler()
	h.SetStoragePath(customPath)

	h.SetEnabled(true)

	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Fatalf("expected custom path file to be created, err: %v", err)
	}

	h2 := NewBandFailoverHandler()
	h2.SetConfigPath(customPath)
	_ = h2.LoadConfig()

	if !h2.IsEnabled() {
		t.Errorf("expected custom path config loaded into h2 with enabled=true")
	}
}
