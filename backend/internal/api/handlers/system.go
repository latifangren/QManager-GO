package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

	"qmanager/internal/config"
	"qmanager/internal/platform"
)

// SystemHandler provides system metrics, hardware profile, and reboot controls.
type SystemHandler struct {
	identity platform.Identity
	cfgMgr   *config.Manager
}

// NewSystemHandler creates a SystemHandler.
func NewSystemHandler(id platform.Identity, cfgMgr *config.Manager) *SystemHandler {
	return &SystemHandler{
		identity: id,
		cfgMgr:   cfgMgr,
	}
}

// Info returns device platform info & identity.
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
	metrics := platform.GetSystemMetrics()
	Success(w, map[string]interface{}{
		"identity": h.identity,
		"metrics":  metrics,
	})
}

// GetConfig returns complete active config.
func (h *SystemHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	Success(w, cfg)
}

// SaveConfig updates and persists config.
func (h *SystemHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		Error(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}

	err := h.cfgMgr.Update(func(c *config.Config) {
		*c = newCfg
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	Success(w, map[string]string{"message": "Configuration saved"})
}

// Reboot safely reboots modem.
func (h *SystemHandler) Reboot(w http.ResponseWriter, r *http.Request) {
	Success(w, map[string]string{"message": "Modem reboot initiated"})

	// Run async reboot after response is flushed
	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("reboot").Run()
	}()
}
