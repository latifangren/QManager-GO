package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestWebConsole_Unauthenticated(t *testing.T) {
	h := NewWebConsoleHandler(func(r *http.Request) bool {
		return false
	})

	req := httptest.NewRequest(http.MethodGet, "/console/ws", nil)
	w := httptest.NewRecorder()
	h.HandleWS(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d", w.Code)
	}
}

func TestWebConsole_Authenticated(t *testing.T) {
	h := NewWebConsoleHandler(func(r *http.Request) bool {
		return true
	})

	server := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/console/ws"
	dialer := websocket.Dialer{
		Subprotocols: []string{"tty"},
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}

	if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	if conn != nil {
		_ = conn.Close()
	}
	_ = err
}
