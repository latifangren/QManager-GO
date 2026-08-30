package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
)

// AlertsHandler handles GET/POST /cgi-bin/quecmanager/monitoring/alerts.sh
type AlertsHandler struct {
	mu       sync.Mutex
	sms      map[string]interface{}
	email    map[string]interface{}
	discord  map[string]interface{}
	routing  map[string]interface{}
}

// NewAlertsHandler creates a new AlertsHandler.
func NewAlertsHandler() *AlertsHandler {
	return &AlertsHandler{
		sms: map[string]interface{}{
			"enabled":           false,
			"recipient_phone":   "",
			"threshold_minutes": 5,
			"configured":        false,
		},
		email: map[string]interface{}{
			"enabled":           false,
			"sender_email":      "",
			"recipient_email":   "",
			"app_password_set":  false,
			"threshold_minutes": 5,
			"msmtp_installed":   true,
			"configured":        false,
		},
		discord: map[string]interface{}{
			"enabled":           false,
			"owner_discord_id":  "",
			"token_set":         false,
			"threshold_minutes": 5,
			"connected":         false,
			"configured":        false,
		},
		routing: map[string]interface{}{
			"events": map[string]interface{}{
				"connection_lost":     map[string]bool{"sms": true, "email": false, "discord": false},
				"connection_restored": map[string]bool{"sms": true, "email": false, "discord": false},
				"reboot":              map[string]bool{"sms": false, "email": false, "discord": false},
			},
		},
	}
}

// HandleAlerts handles GET/POST /cgi-bin/quecmanager/monitoring/alerts.sh
func (h *AlertsHandler) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r.Method == http.MethodGet {
		JSON(w, http.StatusOK, map[string]interface{}{
			"channels": map[string]interface{}{
				"sms":     h.sms,
				"email":   h.email,
				"discord": h.discord,
			},
			"routing": h.routing,
			"capabilities": map[string]interface{}{
				"connection_lost": map[string]interface{}{
					"sms":            true,
					"email":          false,
					"discord":        false,
					"email_reason":   "requires_internet",
					"discord_reason": "requires_internet",
				},
				"connection_restored": map[string]interface{}{
					"sms":     true,
					"email":   true,
					"discord": true,
				},
				"reboot": map[string]interface{}{
					"sms":     true,
					"email":   true,
					"discord": true,
				},
			},
			"reboots": []interface{}{},
		})
		return
	}

	if r.Method == http.MethodPost {
		var payload struct {
			Action  string                 `json:"action"`
			SMS     map[string]interface{} `json:"sms"`
			Email   map[string]interface{} `json:"email"`
			Discord map[string]interface{} `json:"discord"`
			Routing map[string]interface{} `json:"routing"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		if payload.SMS != nil {
			for k, v := range payload.SMS {
				h.sms[k] = v
			}
		}
		if payload.Email != nil {
			for k, v := range payload.Email {
				h.email[k] = v
			}
		}
		if payload.Discord != nil {
			for k, v := range payload.Discord {
				h.discord[k] = v
			}
		}
		if payload.Routing != nil {
			h.routing = payload.Routing
		}

		Success(w, map[string]interface{}{"success": true, "message": "Alerts configuration saved"})
	}
}
