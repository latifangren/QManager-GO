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

func TestScenariosHandler_Deep(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	scenDir := filepath.Join(tmpDir, "scenarios")
	actPath := filepath.Join(tmpDir, "active_scenario")
	h := NewScenarioHandler(eng)
	h.SetStoragePaths(scenDir, actPath)

	// 1. List scenarios - initially empty custom scenarios, active is default (balanced)
	reqList := httptest.NewRequest(http.MethodGet, "/api/cellular/scenarios", nil)
	wList := httptest.NewRecorder()
	h.List(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("List returned %d, want 200", wList.Code)
	}

	// 2. Save invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios", bytes.NewBufferString(`bad`))
	wBadJSON := httptest.NewRecorder()
	h.Save(wBadJSON, reqBadJSON)
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}

	// 3. Save missing name
	bodyNoName, _ := json.Marshal(ScenarioDefinition{Name: ""})
	reqNoName := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios", bytes.NewBuffer(bodyNoName))
	wNoName := httptest.NewRecorder()
	h.Save(wNoName, reqNoName)
	if wNoName.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", wNoName.Code)
	}

	// 4. Save attempt to overwrite built-in scenario (e.g. "balanced")
	bodyOverwrite, _ := json.Marshal(ScenarioDefinition{ID: "balanced", Name: "Hacked Balanced"})
	reqOverwrite := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios", bytes.NewBuffer(bodyOverwrite))
	wOverwrite := httptest.NewRecorder()
	h.Save(wOverwrite, reqOverwrite)
	if wOverwrite.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for overwriting builtin scenario, got %d", wOverwrite.Code)
	}

	// 5. Save new custom scenario with bands and auto-generated ID
	bodyNew, _ := json.Marshal(ScenarioDefinition{
		Name:       "Full 5G High Band",
		ModePref:   "NR5G",
		LTEBands:   []string{"1", "3"},
		NRNSABands: []string{"78"},
		NRSABands:  []string{"77", "78"},
	})
	reqNew := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios", bytes.NewBuffer(bodyNew))
	wNew := httptest.NewRecorder()
	h.Save(wNew, reqNew)
	if wNew.Code != http.StatusOK {
		t.Fatalf("Save new scenario returned %d, want 200: %s", wNew.Code, wNew.Body.String())
	}

	var saveResp map[string]interface{}
	_ = json.NewDecoder(wNew.Body).Decode(&saveResp)
	createdID := saveResp["id"].(string)

	// 6. Save update to existing custom scenario
	bodyUpdate, _ := json.Marshal(ScenarioDefinition{
		ID:          createdID,
		Name:        "Renamed 5G High Band",
		ModePref:    "NR5G",
		Description: "Updated description",
		LTEBands:    []string{"1"},
	})
	reqUpdate := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios", bytes.NewBuffer(bodyUpdate))
	wUpdate := httptest.NewRecorder()
	h.Save(wUpdate, reqUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("Save update scenario returned %d, want 200", wUpdate.Code)
	}

	// 7. Active endpoint
	reqActive := httptest.NewRequest(http.MethodGet, "/api/cellular/scenarios/active", nil)
	wActive := httptest.NewRecorder()
	h.Active(wActive, reqActive)
	if wActive.Code != http.StatusOK {
		t.Fatalf("Active endpoint returned %d, want 200", wActive.Code)
	}

	// 8. Activate built-in scenario (streaming)
	mock.SetResponse(`AT+QNWPREFCFG="mode_pref",LTE:NR5G`, "OK")
	bodyActBuiltin, _ := json.Marshal(map[string]string{"id": "streaming"})
	reqActBuiltin := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios/activate", bytes.NewBuffer(bodyActBuiltin))
	wActBuiltin := httptest.NewRecorder()
	h.Activate(wActBuiltin, reqActBuiltin)
	if wActBuiltin.Code != http.StatusOK {
		t.Fatalf("Activate builtin returned %d, want 200", wActBuiltin.Code)
	}

	// 9. Activate custom scenario with bands
	mock.SetResponse(`AT+QNWPREFCFG="mode_pref",NR5G`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="lte_band",1`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="nsa_nr5g_band",78`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="nr5g_band",77:78`, "OK")

	bodyActCustom, _ := json.Marshal(map[string]string{"id": createdID})
	reqActCustom := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios/activate", bytes.NewBuffer(bodyActCustom))
	wActCustom := httptest.NewRecorder()
	h.Activate(wActCustom, reqActCustom)
	if wActCustom.Code != http.StatusOK {
		t.Fatalf("Activate custom returned %d, want 200", wActCustom.Code)
	}

	// 10. Activate non-existent scenario
	bodyActMissing, _ := json.Marshal(map[string]string{"id": "nonexistent_id"})
	reqActMissing := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios/activate", bytes.NewBuffer(bodyActMissing))
	wActMissing := httptest.NewRecorder()
	h.Activate(wActMissing, reqActMissing)
	if wActMissing.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing scenario activation, got %d", wActMissing.Code)
	}

	// 11. Delete built-in scenario rejection
	reqDelBuiltin := httptest.NewRequest(http.MethodDelete, "/api/cellular/scenarios/balanced", nil)
	wDelBuiltin := httptest.NewRecorder()
	h.Delete(wDelBuiltin, reqDelBuiltin)
	if wDelBuiltin.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for deleting builtin scenario, got %d", wDelBuiltin.Code)
	}

	// 12. Delete custom scenario
	bodyDel, _ := json.Marshal(map[string]string{"id": createdID})
	reqDel := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios/delete.sh", bytes.NewBuffer(bodyDel))
	wDel := httptest.NewRecorder()
	h.Delete(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("Delete custom scenario returned %d, want 200", wDel.Code)
	}

	// 13. Delete non-existent scenario
	bodyDel404, _ := json.Marshal(map[string]string{"id": "missing_again"})
	reqDel404 := httptest.NewRequest(http.MethodPost, "/api/cellular/scenarios/delete.sh", bytes.NewBuffer(bodyDel404))
	wDel404 := httptest.NewRecorder()
	h.Delete(wDel404, reqDel404)
	if wDel404.Code != http.StatusNotFound {
		t.Errorf("expected 404 for deleting non-existent scenario, got %d", wDel404.Code)
	}

	// 14. Corrupt scenario JSON in directory handling
	corruptFile := filepath.Join(scenDir, "corrupt.json")
	_ = os.WriteFile(corruptFile, []byte("invalid json content"), 0644)
	wListCorrupt := httptest.NewRecorder()
	h.List(wListCorrupt, reqList)
	if wListCorrupt.Code != http.StatusOK {
		t.Errorf("List should gracefully skip corrupt files, got %d", wListCorrupt.Code)
	}
}
