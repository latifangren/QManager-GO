package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"qmanager/internal/telemetry"
)

// TelemetryStreamHandler streams live ModemStatus updates via Server-Sent Events (SSE).
type TelemetryStreamHandler struct {
	poller *telemetry.Poller
}

// NewTelemetryStreamHandler creates a new TelemetryStreamHandler.
func NewTelemetryStreamHandler(poller *telemetry.Poller) *TelemetryStreamHandler {
	return &TelemetryStreamHandler{
		poller: poller,
	}
}

// StreamStatus handles SSE telemetry streaming over HTTP.
func (h *TelemetryStreamHandler) StreamStatus(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 1. Send initial in-memory status immediately
	if h.poller != nil {
		initial := h.poller.GetStatus()
		if initial != nil {
			if data, err := json.Marshal(initial); err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}

	// 2. Subscribe to live updates from poller
	var ch chan *telemetry.ModemStatus
	if h.poller != nil {
		ch = h.poller.Subscribe()
		defer h.poller.Unsubscribe(ch)
	}

	// 3. Heartbeat ticker (15s) to maintain active connection through reverse proxies
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case status, ok := <-ch:
			if !ok {
				return
			}
			if status != nil {
				if data, err := json.Marshal(status); err == nil {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				}
			}

		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
