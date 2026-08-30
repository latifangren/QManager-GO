package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
)

// TailscaleHandler manages Tailscale VPN daemon interactions.
type TailscaleHandler struct{}

// NewTailscaleHandler creates a new TailscaleHandler.
func NewTailscaleHandler() *TailscaleHandler {
	return &TailscaleHandler{}
}

// HandleTailscale handles GET/POST /cgi-bin/quecmanager/vpn/tailscale.sh and /api/vpn/tailscale
func (h *TailscaleHandler) HandleTailscale(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		out, err := exec.Command("tailscale", "status", "--json").Output()
		if err != nil {
			JSON(w, http.StatusOK, map[string]interface{}{
				"installed": false,
				"running":   false,
				"backend_state": "NoState",
			})
			return
		}

		var status map[string]interface{}
		if err := json.Unmarshal(out, &status); err == nil {
			status["installed"] = true
			JSON(w, http.StatusOK, status)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"installed": true,
			"running":   true,
			"raw":       string(out),
		})
		return
	}

	if r.Method == http.MethodPost {
		var payload struct {
			Action    string `json:"action"`
			AuthKey   string `json:"auth_key"`
			Hostname  string `json:"hostname"`
			EnableSSH bool   `json:"enable_ssh"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		switch payload.Action {
		case "up":
			args := []string{"up"}
			if payload.AuthKey != "" {
				args = append(args, "--authkey="+payload.AuthKey)
			}
			if payload.Hostname != "" {
				args = append(args, "--hostname="+payload.Hostname)
			}
			if payload.EnableSSH {
				args = append(args, "--ssh")
			}
			_ = exec.Command("tailscale", args...).Run()
			Success(w, map[string]interface{}{"success": true, "message": "Tailscale connecting"})
		case "down":
			_ = exec.Command("tailscale", "down").Run()
			Success(w, map[string]interface{}{"success": true, "message": "Tailscale disconnected"})
		default:
			Success(w, map[string]interface{}{"success": true})
		}
	}
}
