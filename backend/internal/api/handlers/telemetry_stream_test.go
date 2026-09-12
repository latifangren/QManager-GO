package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

type nonFlusherWriter struct {
	http.ResponseWriter
}

func TestTelemetryStreamHandler_NonFlusher(t *testing.T) {
	h := NewTelemetryStreamHandler(nil)
	w := httptest.NewRecorder()
	wrapped := nonFlusherWriter{w}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil)
	h.StreamStatus(wrapped, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for non-flusher writer, got %d", w.Code)
	}
}

func TestTelemetryStreamHandler_StreamFlow(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	id := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, id, 1*time.Second)

	h := NewTelemetryStreamHandler(poller)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.StreamStatus(w, req)
		close(done)
	}()

	// Allow initial status flush
	time.Sleep(50 * time.Millisecond)

	// Broadcast update from poller
	updatedStatus := poller.GetStatus()
	updatedStatus.Band = "B3"
	updatedStatus.Carrier = "TestCarrier"
	// Broadcast to active subscriber
	subCh := poller.Subscribe()
	defer poller.Unsubscribe(subCh)

	// Cancel context to stop stream cleanly
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamStatus did not exit after context cancellation")
	}

	res := w.Body.String()
	if !strings.Contains(res, "data: {") {
		t.Errorf("expected SSE data chunk, got: %s", res)
	}

	headers := w.Header()
	if headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", headers.Get("Content-Type"))
	}
	if headers.Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %s", headers.Get("Cache-Control"))
	}
}

func TestTelemetryStreamHandler_NilPoller(t *testing.T) {
	h := NewTelemetryStreamHandler(nil)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	cancel() // cancel immediately
	h.StreamStatus(w, req)

	headers := w.Header()
	if headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream header, got %s", headers.Get("Content-Type"))
	}
}
