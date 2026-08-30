package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const (
	SimRegistryPath = "/etc/qmanager/known_sims.json"
)

// KnownSIMEntry represents an entry in known_sims.json.
type KnownSIMEntry struct {
	ICCID       string `json:"iccid"`
	Carrier     string `json:"carrier"`
	Label       string `json:"label"`
	ProfileID   string `json:"profile_id,omitempty"`
	LastSeenTs  int64  `json:"last_seen_ts"`
}

// SimRegistryHandler handles SIM registry queries and updates.
type SimRegistryHandler struct {
	mu   sync.Mutex
	path string
}

// NewSimRegistryHandler creates a new SimRegistryHandler.
func NewSimRegistryHandler() *SimRegistryHandler {
	return &SimRegistryHandler{
		path: SimRegistryPath,
	}
}

// HandleRegistry handles GET/POST /cgi-bin/quecmanager/system/sim_registry.sh and /api/system/sim-registry
func (h *SimRegistryHandler) HandleRegistry(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r.Method == http.MethodGet {
		sims := h.loadLocked()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"sims":    sims,
		})
		return
	}

	if r.Method == http.MethodPost {
		var payload struct {
			Action string          `json:"action"`
			SIM    KnownSIMEntry   `json:"sim"`
			ICCID  string          `json:"iccid"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			Error(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		sims := h.loadLocked()
		switch payload.Action {
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
