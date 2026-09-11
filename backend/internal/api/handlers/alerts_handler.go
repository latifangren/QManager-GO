package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// AlertsHandler handles GET/POST /cgi-bin/quecmanager/monitoring/alerts.sh
type AlertsHandler struct {
	mu      sync.Mutex
	sms     map[string]interface{}
	email   map[string]interface{}
	discord map[string]interface{}
	routing map[string]interface{}
	logs    []map[string]interface{}
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
		logs: make([]map[string]interface{}, 0),
	}
}

// HandleAlerts handles GET/POST /cgi-bin/quecmanager/monitoring/alerts.sh
func (h *AlertsHandler) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r.Method == http.MethodGet {
		action := r.URL.Query().Get("action")
		if action == "get_log" {
			entries := h.logs
			if entries == nil {
				entries = make([]map[string]interface{}, 0)
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"success":         true,
				"entries":         entries,
				"total":           len(entries),
				"total_events":    len(entries),
				"filtered_events": len(entries),
			})
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
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
			Channel string                 `json:"channel,omitempty"`
			Message string                 `json:"message,omitempty"`
			SMS     map[string]interface{} `json:"sms"`
			Email   map[string]interface{} `json:"email"`
			Discord map[string]interface{} `json:"discord"`
			Routing map[string]interface{} `json:"routing"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		action := payload.Action
		if action == "" {
			action = r.URL.Query().Get("action")
		}

		switch action {
		case "get_log":
			entries := h.logs
			if entries == nil {
				entries = make([]map[string]interface{}, 0)
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"success":         true,
				"entries":         entries,
				"total":           len(entries),
				"total_events":    len(entries),
				"filtered_events": len(entries),
			})
			return

		case "clear_log":
			h.logs = make([]map[string]interface{}, 0)
			JSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "Alert log cleared",
			})
			return

		case "test":
			channel := payload.Channel
			if channel == "" {
				channel = "all"
			}
			msg := payload.Message
			if msg == "" {
				msg = "Test alert sent"
			}
			entry := map[string]interface{}{
				"timestamp": time.Now().Format("2006-01-02 15:04:05"),
				"trigger":   "test",
				"channel":   channel,
				"recipient": "admin",
				"message":   msg,
				"status":    "sent",
			}
			h.logs = append(h.logs, entry)
			if len(h.logs) > 500 {
				h.logs = h.logs[len(h.logs)-500:]
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "Test alert sent",
			})
			return

		default:
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
			return
		}
	}
}
