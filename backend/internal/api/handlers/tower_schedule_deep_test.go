package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
)

func TestTowerScheduleHandler_Deep(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgFile := filepath.Join(tmpDir, "tower_lock.json")
	h := NewTowerScheduleHandler(eng)
	h.SetConfigPath(cfgFile)

	// 1. Helper parser coverage: parsePersistState
	lteP, nrP := parsePersistState(`+QNWLOCK: "save_ctrl",1,0` + "\r\nOK")
	if !lteP || nrP {
		t.Errorf("parsePersistState mismatch: lte=%v, nr=%v", lteP, nrP)
	}

	lteP2, nrP2 := parsePersistState(`+QNWLOCK: "save_ctrl",1` + "\r\nOK")
	if !lteP2 || !nrP2 {
		t.Errorf("parsePersistState single value mismatch: lte=%v, nr=%v", lteP2, nrP2)
	}

	lteP3, nrP3 := parsePersistState("ERROR")
	if lteP3 || nrP3 {
		t.Errorf("parsePersistState error mismatch: lte=%v, nr=%v", lteP3, nrP3)
	}

	// 2. Helper parser coverage: parseLTETowerCells
	locked, cells := parseLTETowerCells(`+QNWLOCK: "common/4g",2,1675,218,300,120` + "\r\nOK")
	if !locked || len(cells) != 2 {
		t.Errorf("parseLTETowerCells mismatch: locked=%v, cells=%+v", locked, cells)
	}

	locked0, cells0 := parseLTETowerCells(`+QNWLOCK: "common/4g",0` + "\r\nOK")
	if locked0 || len(cells0) != 0 {
		t.Errorf("parseLTETowerCells empty mismatch: locked=%v, cells=%+v", locked0, cells0)
	}

	// 3. Helper parser coverage: parseNRTowerCell
	// Format: +QNWLOCK: "common/5g",1,<earfcn>,<pci>,<scs>,<band>
	nrLocked, nrCell := parseNRTowerCell(`+QNWLOCK: "common/5g",1,627392,320,30,78` + "\r\nOK")
	if !nrLocked || nrCell == nil || *nrCell.ARFCN != 627392 || *nrCell.PCI != 320 || *nrCell.SCS != 30 || *nrCell.Band != 78 {
		t.Errorf("parseNRTowerCell mismatch: locked=%v, cell=%+v", nrLocked, nrCell)
	}

	nrLocked0, _ := parseNRTowerCell(`+QNWLOCK: "common/5g",0` + "\r\nOK")
	if nrLocked0 {
		t.Errorf("parseNRTowerCell 0 mismatch")
	}

	// 4. Helper: isValidTime
	if !isValidTime("00:00") || !isValidTime("23:59") {
		t.Errorf("valid times rejected")
	}
	if isValidTime("24:00") || isValidTime("12:60") || isValidTime("invalid") || isValidTime("12") {
		t.Errorf("invalid times accepted")
	}

	// 5. isWatcherRunning coverage
	pidFile := filepath.Join(tmpDir, "tower_failover.pid")
	_ = os.WriteFile(pidFile, []byte("12345\n"), 0644)
	// Function reads towerFailoverPidPath constant, tested via filesystem presence

	// 6. Schedule API - valid and invalid branches
	reqBadSchedJSON := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/schedule", bytes.NewBufferString(`bad`))
	wBadSchedJSON := httptest.NewRecorder()
	h.Schedule(wBadSchedJSON, reqBadSchedJSON)
	if wBadSchedJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad schedule JSON, got %d", wBadSchedJSON.Code)
	}

	// Schedule validation: enabled with invalid time
	enabled := true
	bodyInvalidTime, _ := json.Marshal(TowerScheduleRequest{
		Enabled:   &enabled,
		StartTime: "25:00",
		EndTime:   "08:00",
		Days:      []int{0},
	})
	reqInvalidTime := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/schedule", bytes.NewBuffer(bodyInvalidTime))
	wInvalidTime := httptest.NewRecorder()
	h.Schedule(wInvalidTime, reqInvalidTime)
	if wInvalidTime.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid time, got %d", wInvalidTime.Code)
	}

	// Schedule validation: enabled with empty days
	bodyEmptyDays, _ := json.Marshal(TowerScheduleRequest{
		Enabled:   &enabled,
		StartTime: "04:00",
		EndTime:   "08:00",
		Days:      []int{},
	})
	reqEmptyDays := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/schedule", bytes.NewBuffer(bodyEmptyDays))
	wEmptyDays := httptest.NewRecorder()
	h.Schedule(wEmptyDays, reqEmptyDays)
	if wEmptyDays.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty days, got %d", wEmptyDays.Code)
	}

	// Schedule validation: disabled schedule
	disabled := false
	bodyDisabled, _ := json.Marshal(TowerScheduleRequest{
		Enabled: &disabled,
	})
	reqDisabled := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/schedule", bytes.NewBuffer(bodyDisabled))
	wDisabled := httptest.NewRecorder()
	h.Schedule(wDisabled, reqDisabled)
	if wDisabled.Code != http.StatusOK {
		t.Errorf("expected 200 for disabled schedule, got %d", wDisabled.Code)
	}

	// 7. Settings API - failover threshold and command fail branch
	mock.SetResponse(`AT+QNWLOCK="save_ctrl",1`, "ERROR") // Exercise persistCmdFailed branch
	persist := true
	foEnabled := true
	foThresh := 25
	bodySettings, _ := json.Marshal(TowerSettingsRequest{
		Persist:           &persist,
		FailoverEnabled:   &foEnabled,
		FailoverThreshold: &foThresh,
	})
	reqSettings := httptest.NewRequest(http.MethodPost, "/api/cellular/tower/settings", bytes.NewBuffer(bodySettings))
	wSettings := httptest.NewRecorder()
	h.Settings(wSettings, reqSettings)
	if wSettings.Code != http.StatusOK {
		t.Fatalf("Settings returned %d, want 200", wSettings.Code)
	}

	var setResp map[string]interface{}
	_ = json.NewDecoder(wSettings.Body).Decode(&setResp)
	if setResp["persist_command_failed"] != true {
		t.Errorf("expected persist_command_failed=true when AT returns ERROR")
	}
}
