package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"qmanager/internal/atengine"
)

const (
	defaultScenariosDir        = "/etc/qmanager/scenarios"
	defaultActiveScenarioPath  = "/etc/qmanager/active_scenario"
)

// ScenarioDefinition represents a custom or built-in scenario configuration.
type ScenarioDefinition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ModePref    string   `json:"mode_pref"` // "AUTO", "LTE", "NR5G", "LTE:NR5G", etc.
	LTEBands    []string `json:"lte_bands,omitempty"`
	NRNSABands  []string `json:"nr_nsa_bands,omitempty"`
	NRSABands   []string `json:"nr_sa_bands,omitempty"`
	IsBuiltin   bool     `json:"is_builtin,omitempty"`
	CreatedAt   int64    `json:"created_at,omitempty"`
	UpdatedAt   int64    `json:"updated_at,omitempty"`
}

// ScenarioHandler handles connection scenarios CRUD, activation, and status.
type ScenarioHandler struct {
	engine             *atengine.Engine
	scenariosDir       string
	activeScenarioPath string
	mu                 sync.Mutex
}

// NewScenarioHandler creates a new ScenarioHandler.
func NewScenarioHandler(engine *atengine.Engine) *ScenarioHandler {
	return &ScenarioHandler{
		engine:             engine,
		scenariosDir:       defaultScenariosDir,
		activeScenarioPath: defaultActiveScenarioPath,
	}
}

// SetStoragePaths allows customizing paths for isolated unit tests.
func (h *ScenarioHandler) SetStoragePaths(scenariosDir, activePath string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scenariosDir = scenariosDir
	h.activeScenarioPath = activePath
}

func (h *ScenarioHandler) getActiveScenarioID() string {
	data, err := os.ReadFile(h.activeScenarioPath)
	if err != nil {
		return "balanced"
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "balanced"
	}
	return id
}

func (h *ScenarioHandler) setActiveScenarioID(id string) error {
	_ = os.MkdirAll(filepath.Dir(h.activeScenarioPath), 0755)
	if id == "" {
		id = "balanced"
	}
	return os.WriteFile(h.activeScenarioPath, []byte(id), 0644)
}

// Builtin scenarios
var builtinScenarios = map[string]ScenarioDefinition{
	"balanced": {
		ID:          "balanced",
		Name:        "Balanced",
		Description: "Automatic RAT selection with carrier aggregation and full band access",
		ModePref:    "AUTO",
		IsBuiltin:   true,
	},
	"gaming": {
		ID:          "gaming",
		Name:        "Low Latency Gaming",
		Description: "Prioritize 5G NR Standalone (SA) for lower latency and ping stability",
		ModePref:    "NR5G",
		IsBuiltin:   true,
	},
	"streaming": {
		ID:          "streaming",
		Name:        "High Throughput Streaming",
		Description: "Dual LTE + NR connectivity for maximum bandwidth and fast streaming",
		ModePref:    "LTE:NR5G",
		IsBuiltin:   true,
	},
}

// List handles GET /api/v1/cellular/scenarios and GET /cgi-bin/quecmanager/scenarios/list.sh
func (h *ScenarioHandler) List(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	activeID := h.getActiveScenarioID()
	var scenarios []ScenarioDefinition

	_ = os.MkdirAll(h.scenariosDir, 0755)
	entries, err := os.ReadDir(h.scenariosDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			path := filepath.Join(h.scenariosDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var s ScenarioDefinition
			if err := json.Unmarshal(data, &s); err != nil {
				continue
			}
			scenarios = append(scenarios, s)
		}
	}

	if scenarios == nil {
		scenarios = []ScenarioDefinition{}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"scenarios":          scenarios,
		"active_scenario_id": activeID,
	})
}

// Active handles GET /api/v1/cellular/scenarios/active and GET /cgi-bin/quecmanager/scenarios/active.sh
func (h *ScenarioHandler) Active(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	activeID := h.getActiveScenarioID()
	h.mu.Unlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"active_scenario_id": activeID,
	})
}

// Save handles POST /api/v1/cellular/scenarios and POST /cgi-bin/quecmanager/scenarios/save.sh
func (h *ScenarioHandler) Save(w http.ResponseWriter, r *http.Request) {
	var s ScenarioDefinition
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		Error(w, http.StatusBadRequest, "Invalid scenario payload")
		return
	}

	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		Error(w, http.StatusBadRequest, "Scenario name is required")
		return
	}

	s.ModePref = strings.ToUpper(strings.TrimSpace(s.ModePref))
	if s.ModePref == "" {
		s.ModePref = "AUTO"
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_ = os.MkdirAll(h.scenariosDir, 0755)

	isNew := s.ID == ""
	if isNew {
		s.ID = fmt.Sprintf("custom-%d", time.Now().Unix())
		s.CreatedAt = time.Now().Unix()
	} else if _, isBuiltin := builtinScenarios[s.ID]; isBuiltin {
		Error(w, http.StatusBadRequest, "Cannot overwrite built-in scenario")
		return
	}

	s.UpdatedAt = time.Now().Unix()
	if s.CreatedAt == 0 {
		s.CreatedAt = s.UpdatedAt
	}

	path := filepath.Join(h.scenariosDir, s.ID+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to encode scenario JSON")
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to write scenario file")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"id":       s.ID,
		"scenario": s,
		"message":  "Scenario saved successfully",
	})
}

// Delete handles DELETE /api/v1/cellular/scenarios/{id} and POST /cgi-bin/quecmanager/scenarios/delete.sh
func (h *ScenarioHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}
	if id == "" && r.Method == http.MethodPost {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	if id == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 && parts[len(parts)-1] != "scenarios" && !strings.HasSuffix(parts[len(parts)-1], ".sh") {
			id = parts[len(parts)-1]
		}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		Error(w, http.StatusBadRequest, "Missing scenario ID")
		return
	}

	if _, isBuiltin := builtinScenarios[id]; isBuiltin {
		Error(w, http.StatusBadRequest, "Cannot delete built-in scenario")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	path := filepath.Join(h.scenariosDir, id+".json")
	if _, err := os.Stat(path); err != nil {
		Error(w, http.StatusNotFound, "Scenario not found")
		return
	}

	_ = os.Remove(path)

	// If active scenario was deleted, reset to balanced
	if h.getActiveScenarioID() == id {
		_ = h.setActiveScenarioID("balanced")
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Scenario deleted",
	})
}

// Activate handles POST /api/v1/cellular/scenarios/activate and POST /cgi-bin/quecmanager/scenarios/activate.sh
func (h *ScenarioHandler) Activate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		ScenarioID string `json:"scenario_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	id := req.ID
	if id == "" {
		id = req.ScenarioID
	}
	id = strings.TrimSpace(id)
	if id == "" {
		Error(w, http.StatusBadRequest, "Missing scenario ID")
		return
	}

	var scn ScenarioDefinition
	if b, ok := builtinScenarios[id]; ok {
		scn = b
	} else {
		h.mu.Lock()
		path := filepath.Join(h.scenariosDir, id+".json")
		data, err := os.ReadFile(path)
		h.mu.Unlock()
		if err != nil {
			Error(w, http.StatusNotFound, "Scenario not found")
			return
		}
		if err := json.Unmarshal(data, &scn); err != nil {
			Error(w, http.StatusInternalServerError, "Malformed scenario definition")
			return
		}
	}

	// Apply mode preference: AT+QNWPREFCFG="mode_pref",<MODE>
	var modePrefCmd string
	if scn.ModePref != "" {
		modePrefCmd = fmt.Sprintf(`AT+QNWPREFCFG="mode_pref",%s`, scn.ModePref)
		if _, err := h.engine.Exec(modePrefCmd); err != nil {
			Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set mode preference: %v", err))
			return
		}
	}

	// Apply LTE bands if configured
	if len(scn.LTEBands) > 0 {
		lteMask := strings.Join(scn.LTEBands, ":")
		_, _ = h.engine.Exec(fmt.Sprintf(`AT+QNWPREFCFG="lte_band",%s`, lteMask))
	}

	// Apply NR NSA bands if configured
	if len(scn.NRNSABands) > 0 {
		nsaMask := strings.Join(scn.NRNSABands, ":")
		_, _ = h.engine.Exec(fmt.Sprintf(`AT+QNWPREFCFG="nsa_nr5g_band",%s`, nsaMask))
	}

	// Apply NR SA bands if configured
	if len(scn.NRSABands) > 0 {
		saMask := strings.Join(scn.NRSABands, ":")
		_, _ = h.engine.Exec(fmt.Sprintf(`AT+QNWPREFCFG="nr5g_band",%s`, saMask))
	}

	h.mu.Lock()
	_ = h.setActiveScenarioID(id)
	h.mu.Unlock()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"active_scenario_id": id,
		"applied": map[string]interface{}{
			"mode_pref":     scn.ModePref,
			"lte_bands":     scn.LTEBands,
			"nr_nsa_bands":  scn.NRNSABands,
			"nr_sa_bands":   scn.NRSABands,
		},
		"message": fmt.Sprintf("Scenario %s activated", scn.Name),
	})
}
