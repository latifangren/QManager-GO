package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSimRegistryHandler_Deep(t *testing.T) {
	tmpDir := t.TempDir()
	regFile := filepath.Join(tmpDir, "known_sims.json")

	h := &SimRegistryHandler{
		path: regFile,
	}

	// 1. Initial load on empty/non-existent file
	reqGet := httptest.NewRequest(http.MethodGet, "/api/system/sim-registry", nil)
	wGet := httptest.NewRecorder()
	h.HandleRegistry(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("HandleRegistry GET returned %d, want 200", wGet.Code)
	}

	// 2. Bad JSON payload
	reqBad := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBufferString(`bad`))
	wBad := httptest.NewRecorder()
	h.HandleRegistry(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBad.Code)
	}

	// 3. Save action missing ICCID
	bodyNoICCID, _ := json.Marshal(map[string]interface{}{"action": "save", "sim": KnownSIMEntry{Label: "No ICCID"}})
	reqNoICCID := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodyNoICCID))
	wNoICCID := httptest.NewRecorder()
	h.HandleRegistry(wNoICCID, reqNoICCID)
	if wNoICCID.Code != http.StatusOK {
		t.Errorf("expected 200 for empty ICCID no-op save, got %d", wNoICCID.Code)
	}

	// 4. Valid Save 1
	bodyUpsert1, _ := json.Marshal(map[string]interface{}{
		"action": "save",
		"sim": KnownSIMEntry{
			ICCID:      "8986000000000000001",
			Carrier:    "Carrier A",
			Label:      "Primary SIM",
			ProfileID:  "prof-1",
			LastSeenTs: 1000,
		},
	})
	reqUpsert1 := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodyUpsert1))
	wUpsert1 := httptest.NewRecorder()
	h.HandleRegistry(wUpsert1, reqUpsert1)
	if wUpsert1.Code != http.StatusOK {
		t.Fatalf("Save 1 returned %d, want 200", wUpsert1.Code)
	}

	// 5. Valid Save 2 (different ICCID)
	bodyUpsert2, _ := json.Marshal(map[string]interface{}{
		"action": "save",
		"sim": KnownSIMEntry{
			ICCID:      "8986000000000000002",
			Carrier:    "Carrier B",
			Label:      "Backup SIM",
			ProfileID:  "prof-2",
			LastSeenTs: 2000,
		},
	})
	reqUpsert2 := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodyUpsert2))
	wUpsert2 := httptest.NewRecorder()
	h.HandleRegistry(wUpsert2, reqUpsert2)
	if wUpsert2.Code != http.StatusOK {
		t.Fatalf("Save 2 returned %d, want 200", wUpsert2.Code)
	}

	// 6. Update existing ICCID 1 (label change)
	bodyUpdate1, _ := json.Marshal(map[string]interface{}{
		"action": "update",
		"sim": KnownSIMEntry{
			ICCID:      "8986000000000000001",
			Carrier:    "Carrier A New",
			Label:      "Updated Primary SIM",
			ProfileID:  "prof-1",
			LastSeenTs: 3000,
		},
	})
	reqUpdate1 := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodyUpdate1))
	wUpdate1 := httptest.NewRecorder()
	h.HandleRegistry(wUpdate1, reqUpdate1)
	if wUpdate1.Code != http.StatusOK {
		t.Fatalf("Update 1 returned %d, want 200", wUpdate1.Code)
	}

	// 7. GET to verify 2 entries with updated data
	wGetAfter := httptest.NewRecorder()
	h.HandleRegistry(wGetAfter, reqGet)
	var respGet map[string]interface{}
	_ = json.NewDecoder(wGetAfter.Body).Decode(&respGet)
	sims := respGet["sims"].([]interface{})
	if len(sims) != 2 {
		t.Fatalf("expected 2 sims in registry, got %d", len(sims))
	}

	// 8. Delete ICCID 2
	bodyDel2, _ := json.Marshal(map[string]string{"action": "delete", "iccid": "8986000000000000002"})
	reqDel2 := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodyDel2))
	wDel2 := httptest.NewRecorder()
	h.HandleRegistry(wDel2, reqDel2)
	if wDel2.Code != http.StatusOK {
		t.Fatalf("Delete 2 returned %d, want 200", wDel2.Code)
	}

	// 9. Unknown action default success envelope
	bodyUnknown, _ := json.Marshal(map[string]string{"action": "invalid_action"})
	reqUnknown := httptest.NewRequest(http.MethodPost, "/api/system/sim-registry", bytes.NewBuffer(bodyUnknown))
	wUnknown := httptest.NewRecorder()
	h.HandleRegistry(wUnknown, reqUnknown)
	if wUnknown.Code != http.StatusOK {
		t.Errorf("expected 200 for fallback action, got %d", wUnknown.Code)
	}
}
