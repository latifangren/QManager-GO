package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestQualityThresholdsHandler(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "quality_thresholds.json")

	h := NewQualityThresholdsHandler(filePath)

	// 1. Initial GET (defaults to tolerant)
	wGet := httptest.NewRecorder()
	h.Get(wGet, httptest.NewRequest(http.MethodGet, "/api/v1/settings/quality-thresholds", nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for Get, got %d", wGet.Code)
	}

	var resp QualityThresholdsResponse
	if err := json.NewDecoder(wGet.Body).Decode(&resp); err != nil {
		t.Fatalf("failed decoding GET response: %v", err)
	}
	if !resp.IsDefault || resp.Latency.Preset != "tolerant" || resp.Loss.Preset != "tolerant" {
		t.Fatalf("unexpected default thresholds: %+v", resp)
	}
	if resp.Latency.WarningMs != 150 || resp.Latency.CriticalMs != 300 {
		t.Errorf("expected latency warning 150 critical 300, got %+v", resp.Latency)
	}
	if resp.Loss.WarningPct != 5 || resp.Loss.CriticalPct != 15 {
		t.Errorf("expected loss warning 5 critical 15, got %+v", resp.Loss)
	}

	// 2. POST update to standard
	bodyUpdate := `{"latency":{"preset":"standard"},"loss":{"preset":"standard"}}`
	wPost := httptest.NewRecorder()
	h.Save(wPost, httptest.NewRequest(http.MethodPost, "/api/v1/settings/quality-thresholds", bytes.NewBufferString(bodyUpdate)))
	if wPost.Code != http.StatusOK {
		t.Fatalf("expected 200 for Save, got %d: %s", wPost.Code, wPost.Body.String())
	}

	var postResp QualityThresholdsResponse
	_ = json.NewDecoder(wPost.Body).Decode(&postResp)
	if postResp.IsDefault || postResp.Latency.Preset != "standard" || postResp.Latency.WarningMs != 80 {
		t.Fatalf("unexpected updated thresholds: %+v", postResp)
	}

	// 3. New handler instance loads persisted configuration
	h2 := NewQualityThresholdsHandler(filePath)
	wGet2 := httptest.NewRecorder()
	h2.Handle(wGet2, httptest.NewRequest(http.MethodGet, "/cgi-bin/quecmanager/settings/quality_thresholds.sh", nil))
	if wGet2.Code != http.StatusOK {
		t.Fatalf("expected 200 for Handle GET, got %d", wGet2.Code)
	}
	var resp2 QualityThresholdsResponse
	_ = json.NewDecoder(wGet2.Body).Decode(&resp2)
	if resp2.Latency.Preset != "standard" || resp2.Loss.Preset != "standard" {
		t.Fatalf("expected persisted standard preset in new handler instance, got %+v", resp2)
	}

	// 4. Invalid preset rejection
	bodyInvalid := `{"latency":{"preset":"invalid-preset"}}`
	wInvalid := httptest.NewRecorder()
	h2.Handle(wInvalid, httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/settings/quality_thresholds.sh", bytes.NewBufferString(bodyInvalid)))
	if wInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid preset, got %d", wInvalid.Code)
	}

	// 5. Invalid JSON
	wBadJSON := httptest.NewRecorder()
	h2.Save(wBadJSON, httptest.NewRequest(http.MethodPost, "/api/v1/settings/quality-thresholds", bytes.NewBufferString("not-json")))
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}
}
