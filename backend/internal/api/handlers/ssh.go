package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"qmanager/internal/sshd"
)

// SSHHandler exposes endpoints to query and update native SSH server configuration.
type SSHHandler struct {
	mgr *sshd.Manager
}

// NewSSHHandler returns a new SSHHandler instance.
func NewSSHHandler(mgr *sshd.Manager) *SSHHandler {
	return &SSHHandler{mgr: mgr}
}

// GetStatus returns the current SSH server configuration and running status.
func (h *SSHHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if h.mgr == nil {
		Error(w, http.StatusServiceUnavailable, "SSH manager not available")
		return
	}
	status := h.mgr.GetStatus()
	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    status,
	})
}

// SaveSettings applies SSH enable/disable, port change, and authorized keys.
func (h *SSHHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	if h.mgr == nil {
		Error(w, http.StatusServiceUnavailable, "SSH manager not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var payload struct {
		Enabled        bool   `json:"enabled"`
		Port           int    `json:"port"`
		AuthorizedKeys string `json:"authorized_keys"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if payload.Port <= 0 || payload.Port > 65535 {
		payload.Port = 22
	}

	if err := h.mgr.ApplySettings(payload.Enabled, payload.Port, payload.AuthorizedKeys); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to apply SSH settings: "+err.Error())
		return
	}

	status := h.mgr.GetStatus()
	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "SSH configuration updated successfully",
		"data":    status,
	})
}

// HandleCGI provides CGI fallback for /cgi-bin/quecmanager/system/ssh.sh
func (h *SSHHandler) HandleCGI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.GetStatus(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.SaveSettings(w, r)
		return
	}
	Error(w, http.StatusMethodNotAllowed, "Method not allowed")
}
