package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCellularHandler_SetFailoverHandler(t *testing.T) {
	h := NewCellularHandler(nil, nil)
	fo := NewBandFailoverHandler()
	h.SetFailoverHandler(fo)

	if h.failover != fo {
		t.Errorf("expected failover handler reference to be set")
	}
}

func TestCellularApnHandler_SetStoragePaths(t *testing.T) {
	tmpDir := t.TempDir()
	apnH := NewCellularApnHandler(nil, nil, tmpDir)

	settingPath := filepath.Join(tmpDir, "apn_setting.json")
	namesPath := filepath.Join(tmpDir, "apn_names.json")
	apnH.SetStoragePaths(settingPath, namesPath)

	testNames := map[string]string{
		"default": "internet.apn",
		"ims":     "ims.apn",
	}

	if err := apnH.writeApnNames(testNames); err != nil {
		t.Fatalf("writeApnNames failed: %v", err)
	}

	data, err := os.ReadFile(namesPath)
	if err != nil {
		t.Fatalf("failed to read namesPath file: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal apn names JSON: %v", err)
	}

	if parsed["default"] != "internet.apn" || parsed["ims"] != "ims.apn" {
		t.Errorf("unexpected apn names content: %+v", parsed)
	}

	readBack := apnH.readApnNames()
	if readBack["default"] != "internet.apn" {
		t.Errorf("readApnNames mismatch: %+v", readBack)
	}
}

func TestScenarioHandler_SetStoragePaths(t *testing.T) {
	tmpDir := t.TempDir()
	scnH := NewScenarioHandler(nil, tmpDir)

	scenariosDir := filepath.Join(tmpDir, "scenarios")
	activePath := filepath.Join(tmpDir, "active.json")
	scnH.SetStoragePaths(scenariosDir, activePath)

	if scnH.scenariosDir != scenariosDir {
		t.Errorf("expected scenariosDir %s, got %s", scenariosDir, scnH.scenariosDir)
	}
	if scnH.activeScenarioPath != activePath {
		t.Errorf("expected activeScenarioPath %s, got %s", activePath, scnH.activeScenarioPath)
	}
}

func TestSIMProfileHandler_SetStoragePaths(t *testing.T) {
	tmpDir := t.TempDir()
	simH := NewSIMProfileHandler(nil, tmpDir)

	profDir := filepath.Join(tmpDir, "profiles")
	activePath := filepath.Join(tmpDir, "active.json")
	statePath := filepath.Join(tmpDir, "state.json")
	simH.SetStoragePaths(profDir, activePath, statePath)

	if simH.profileDir != profDir {
		t.Errorf("expected profileDir %s, got %s", profDir, simH.profileDir)
	}
	if simH.activeProfilePath != activePath {
		t.Errorf("expected activeProfilePath %s, got %s", activePath, simH.activeProfilePath)
	}
	if simH.profileStatePath != statePath {
		t.Errorf("expected profileStatePath %s, got %s", statePath, simH.profileStatePath)
	}
}
