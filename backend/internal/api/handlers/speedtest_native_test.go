package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNativeSpeedtestLifecycle(t *testing.T) {
	h := NewSpeedtestHandler()

	// 1. Check Available
	wCheck := httptest.NewRecorder()
	h.CheckAvailable(wCheck, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/speedtest/check", nil))
	if wCheck.Code != http.StatusOK {
		t.Fatalf("CheckAvailable code %d != 200", wCheck.Code)
	}
	var checkResp map[string]bool
	_ = json.Unmarshal(wCheck.Body.Bytes(), &checkResp)
	if !checkResp["available"] {
		t.Fatalf("expected available == true, got false")
	}

	// 2. Initial Status
	wStatus := httptest.NewRecorder()
	h.GetStatus(wStatus, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/speedtest/status", nil))
	var statusResp map[string]interface{}
	_ = json.Unmarshal(wStatus.Body.Bytes(), &statusResp)
	if statusResp["status"] != "idle" {
		t.Fatalf("expected status == idle, got %v", statusResp["status"])
	}

	// 3. Stop when idle
	wStop := httptest.NewRecorder()
	h.StopTest(wStop, httptest.NewRequest(http.MethodPost, "/api/v1/diagnostics/speedtest/stop", nil))
	if wStop.Code != http.StatusOK {
		t.Fatalf("StopTest code %d != 200", wStop.Code)
	}

	// 4. Cancel during running
	h.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	h.running = true
	h.status = "running"
	h.phase = "ping"
	h.cancel = cancel
	h.mu.Unlock()

	wStop2 := httptest.NewRecorder()
	h.StopTest(wStop2, httptest.NewRequest(http.MethodPost, "/api/v1/diagnostics/speedtest/stop", nil))
	if wStop2.Code != http.StatusOK {
		t.Fatalf("StopTest running code %d != 200", wStop2.Code)
	}
	if ctx.Err() == nil {
		t.Fatalf("expected context to be cancelled")
	}

	// 5. GetStatus when complete
	h.mu.Lock()
	h.status = "complete"
	h.result = map[string]interface{}{"type": "result", "timestamp": time.Now().Format(time.RFC3339)}
	h.mu.Unlock()

	wStatusComplete := httptest.NewRecorder()
	h.GetStatus(wStatusComplete, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/speedtest/status", nil))
	var completeResp map[string]interface{}
	_ = json.Unmarshal(wStatusComplete.Body.Bytes(), &completeResp)
	if completeResp["status"] != "complete" || completeResp["result"] == nil {
		t.Fatalf("expected complete status and non-nil result")
	}
}
