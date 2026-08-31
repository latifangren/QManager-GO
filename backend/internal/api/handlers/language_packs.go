package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	DefaultLanguagePacksDir = "/usrdata/qmanager/language-packs"
)

// InstalledPack represents a downloaded/installed language pack.
type InstalledPack struct {
	Code         string   `json:"code"`
	Version      string   `json:"version"`
	NativeName   string   `json:"native_name"`
	EnglishName  string   `json:"english_name"`
	Completeness float64  `json:"completeness"`
	Namespaces   []string `json:"namespaces"`
}

// LanguagePackInstallState represents the progress/status of an installation.
type LanguagePackInstallState struct {
	State     string  `json:"state"`
	Code      string  `json:"code,omitempty"`
	Progress  int     `json:"progress"`
	Step      string  `json:"step,omitempty"`
	Message   string  `json:"message,omitempty"`
	UpdatedAt int64   `json:"updated_at,omitempty"`
}

// LanguagePacksHandler manages language pack listing, installation, and removal.
type LanguagePacksHandler struct {
	packsDir     string
	mu           sync.Mutex
	installState LanguagePackInstallState
}

// NewLanguagePacksHandler creates a new LanguagePacksHandler.
func NewLanguagePacksHandler() *LanguagePacksHandler {
	return &LanguagePacksHandler{
		packsDir: DefaultLanguagePacksDir,
		installState: LanguagePackInstallState{
			State:    "idle",
			Progress: 0,
		},
	}
}

// List handles GET /cgi-bin/quecmanager/system/language-packs/list.sh and /api/system/language-packs/list
func (h *LanguagePacksHandler) List(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var packs []InstalledPack
	entries, err := os.ReadDir(h.packsDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				packMetaPath := filepath.Join(h.packsDir, e.Name(), "_pack.json")
				if data, err := os.ReadFile(packMetaPath); err == nil {
					var p InstalledPack
					if err := json.Unmarshal(data, &p); err == nil {
						packs = append(packs, p)
					}
				}
			}
		}
	}

	if packs == nil {
		packs = []InstalledPack{}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"packs":   packs,
	})
}

// Install handles POST /cgi-bin/quecmanager/system/language-packs/install.sh and /api/system/language-packs/install
func (h *LanguagePacksHandler) Install(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Code == "" {
		Error(w, http.StatusBadRequest, "Invalid JSON payload or missing 'code'")
		return
	}

	h.installState = LanguagePackInstallState{
		State:     "installing",
		Code:      payload.Code,
		Progress:  50,
		Step:      "installing",
		Message:   fmt.Sprintf("Installing language pack %s", payload.Code),
		UpdatedAt: time.Now().Unix(),
	}

	// Create directory and simulate local install completion
	packDir := filepath.Join(h.packsDir, payload.Code)
	_ = os.MkdirAll(packDir, 0755)

	meta := InstalledPack{
		Code:         payload.Code,
		Version:      time.Now().Format("2006.01.02"),
		NativeName:   payload.Code,
		EnglishName:  payload.Code,
		Completeness: 1.0,
		Namespaces:   []string{"common", "sidebar", "dashboard", "cellular", "system-settings"},
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(packDir, "_pack.json"), data, 0644)

	h.installState = LanguagePackInstallState{
		State:     "done",
		Code:      payload.Code,
		Progress:  100,
		Step:      "done",
		Message:   "Language pack installed successfully",
		UpdatedAt: time.Now().Unix(),
	}

	Success(w, map[string]interface{}{
		"success": true,
		"message": "Language pack installation completed",
	})
}

// InstallStatus handles GET /cgi-bin/quecmanager/system/language-packs/install_status.sh and /api/system/language-packs/install-status
func (h *LanguagePacksHandler) InstallStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	JSON(w, http.StatusOK, h.installState)
}

// InstallCancel handles POST /cgi-bin/quecmanager/system/language-packs/install_cancel.sh and cancels an in-progress install.
func (h *LanguagePacksHandler) InstallCancel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.installState = LanguagePackInstallState{
		State:     "idle",
		Progress:  0,
		Message:   "Installation canceled",
		UpdatedAt: time.Now().Unix(),
	}

	Success(w, map[string]interface{}{
		"success": true,
		"message": "Language pack installation canceled",
	})
}

// Remove handles POST /cgi-bin/quecmanager/system/language-packs/remove.sh and /api/system/language-packs/remove
func (h *LanguagePacksHandler) Remove(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Code string `json:"code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Code == "" {
		Error(w, http.StatusBadRequest, "Invalid JSON payload or missing 'code'")
		return
	}

	packDir := filepath.Join(h.packsDir, payload.Code)
	_ = os.RemoveAll(packDir)

	Success(w, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Language pack %s removed", payload.Code),
	})
}
