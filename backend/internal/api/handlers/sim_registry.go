package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"qmanager/internal/telemetry"
)

var (
	SimRegistryPath = "/etc/qmanager/known_sims.json"
)

// KnownSIMEntry represents an entry in known_sims.json and sim_registry.sh spec.
type KnownSIMEntry struct {
	ICCID       string `json:"iccid"`
	Carrier     string `json:"carrier"`
	Label       string `json:"label,omitempty"`
	ProfileID   string `json:"profile_id,omitempty"`
	PhoneNumber string `json:"phone_number"`
	FirstSeen   string `json:"first_seen"`
	Dismissed   bool   `json:"dismissed"`
	Active      bool   `json:"active"`
	LastSeenTs  int64  `json:"last_seen_ts,omitempty"`
}

// SimRegistryHandler handles SIM registry queries and updates.
type SimRegistryHandler struct {
	mu     sync.Mutex
	path   string
	poller *telemetry.Poller
}

// NewSimRegistryHandler creates a new SimRegistryHandler.
func NewSimRegistryHandler(params ...interface{}) *SimRegistryHandler {
	path := SimRegistryPath
	var p *telemetry.Poller
	for _, arg := range params {
		if s, ok := arg.(string); ok && s != "" {
			path = s
		} else if pol, ok := arg.(*telemetry.Poller); ok {
			p = pol
		}
	}
	return &SimRegistryHandler{
		path:   path,
		poller: p,
	}
}

// SetPath sets custom registry file path for testing.
func (h *SimRegistryHandler) SetPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.path = path
}

// HandleRegistry handles GET/POST /cgi-bin/quecmanager/system/sim_registry.sh and /api/system/sim-registry
func (h *SimRegistryHandler) HandleRegistry(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r.Method == http.MethodGet {
		sims := h.loadLocked()

		// Auto-populate active SIM from poller if registry is empty
		if h.poller != nil {
			status := h.poller.GetStatus()
			if status != nil && status.ICCID != "" {
				activeICCID := status.ICCID
				carrier := status.Carrier
				phone := status.PhoneNumber

				found := false
				for i, s := range sims {
					if s.ICCID == activeICCID {
						sims[i].Active = true
						if sims[i].Carrier == "" {
							sims[i].Carrier = carrier
						}
						found = true
					} else {
						sims[i].Active = false
					}
				}
				if !found {
					newEntry := KnownSIMEntry{
						ICCID:       activeICCID,
						Carrier:     carrier,
						PhoneNumber: phone,
						FirstSeen:   time.Now().UTC().Format(time.RFC3339),
						Dismissed:   false,
						Active:      true,
						LastSeenTs:  time.Now().Unix(),
					}
					sims = append([]KnownSIMEntry{newEntry}, sims...)
					_ = h.saveLocked(sims)
				}
			}
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"sims":    sims,
		})
		return
	}

	if r.Method == http.MethodPost {
		var payload struct {
			Action string        `json:"action"`
			SIM    KnownSIMEntry `json:"sim"`
			ICCID  string        `json:"iccid"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		sims := h.loadLocked()
		switch payload.Action {
		case "dismiss":
			for i, s := range sims {
				if s.ICCID == payload.ICCID {
					sims[i].Dismissed = true
					break
				}
			}
			_ = h.saveLocked(sims)
			Success(w, map[string]interface{}{"success": true, "message": "SIM dismissed"})
		case "undismiss":
			for i, s := range sims {
				if s.ICCID == payload.ICCID {
					sims[i].Dismissed = false
					break
				}
			}
			_ = h.saveLocked(sims)
			Success(w, map[string]interface{}{"success": true, "message": "SIM restored"})
		case "clear", "clear_registry":
			_ = h.saveLocked([]KnownSIMEntry{})
			Success(w, map[string]interface{}{"success": true, "message": "SIM registry cleared", "registry_cleared": true})
		case "save", "update":
			found := false
			for i, s := range sims {
				if s.ICCID == payload.SIM.ICCID {
					sims[i] = payload.SIM
					found = true
					break
				}
			}
			if !found && payload.SIM.ICCID != "" {
				sims = append(sims, payload.SIM)
			}
			_ = h.saveLocked(sims)
			Success(w, map[string]interface{}{"success": true, "message": "SIM registry updated"})
		case "delete":
			var updated []KnownSIMEntry
			for _, s := range sims {
				if s.ICCID != payload.ICCID {
					updated = append(updated, s)
				}
			}
			_ = h.saveLocked(updated)
			Success(w, map[string]interface{}{"success": true, "message": "SIM entry removed"})
		default:
			Success(w, map[string]interface{}{"success": true})
		}
	}
}

func (h *SimRegistryHandler) loadLocked() []KnownSIMEntry {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return []KnownSIMEntry{}
	}
	var sims []KnownSIMEntry
	_ = json.Unmarshal(data, &sims)
	return sims
}

func (h *SimRegistryHandler) saveLocked(sims []KnownSIMEntry) error {
	dir := filepath.Dir(h.path)
	_ = os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(sims, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, data, 0644)
}
