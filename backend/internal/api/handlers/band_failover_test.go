package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
