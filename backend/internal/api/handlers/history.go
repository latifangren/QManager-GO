package handlers

import (
	"net/http"
	"strconv"

	"qmanager/internal/telemetry"
)

// HistoryHandler handles GET queries for historical signal, ping, and network events.
type HistoryHandler struct {
	history *telemetry.TelemetryHistory
}

// NewHistoryHandler creates a new HistoryHandler.
func NewHistoryHandler() *HistoryHandler {
	return &HistoryHandler{
		history: telemetry.GetGlobalHistory(),
	}
}

// FetchSignalHistory handles GET /cgi-bin/quecmanager/at_cmd/fetch_signal_history.sh and /api/telemetry/history/signal
func (h *HistoryHandler) FetchSignalHistory(w http.ResponseWriter, r *http.Request) {
	limit := 180
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}

	pts := h.history.GetSignalHistory(limit)
	JSON(w, http.StatusOK, pts)
}

// FetchPingHistory handles GET /cgi-bin/quecmanager/at_cmd/fetch_ping_history.sh and /api/telemetry/history/ping
func (h *HistoryHandler) FetchPingHistory(w http.ResponseWriter, r *http.Request) {
	limit := 180
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}

	pts := h.history.GetPingHistory(limit)
	JSON(w, http.StatusOK, pts)
}

// FetchEvents handles GET /cgi-bin/quecmanager/at_cmd/fetch_events.sh and /api/telemetry/events
func (h *HistoryHandler) FetchEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}

	events := h.history.GetEvents(limit)
	JSON(w, http.StatusOK, events)
}
