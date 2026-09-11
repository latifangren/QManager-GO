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

var defaultQualityThresholdsPath = "/etc/qmanager/quality_thresholds.json"

// QualityThresholdPreset defines the threshold numerical bounds for a quality level.
type QualityThresholdPreset struct {
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
}

// LatencyThresholdConfig describes latency threshold settings.
type LatencyThresholdConfig struct {
	Preset     string `json:"preset"`
	WarningMs  int    `json:"warning_ms"`
	CriticalMs int    `json:"critical_ms"`
}

// LossThresholdConfig describes packet loss threshold settings.
type LossThresholdConfig struct {
	Preset      string `json:"preset"`
	WarningPct  int    `json:"warning_pct"`
	CriticalPct int    `json:"critical_pct"`
}

// QualityThresholdsResponse represents the GET/POST output envelope.
type QualityThresholdsResponse struct {
	Latency   LatencyThresholdConfig `json:"latency"`
	Loss      LossThresholdConfig    `json:"loss"`
	IsDefault bool                   `json:"is_default"`
}

// QualityThresholdsSettings represents the persisted config in JSON.
type QualityThresholdsSettings struct {
	Latency struct {
		Preset string `json:"preset"`
	} `json:"latency"`
	Loss struct {
		Preset string `json:"preset"`
	} `json:"loss"`
}

// QualityThresholdsHandler manages latency and packet loss thresholds.
type QualityThresholdsHandler struct {
	mu            sync.RWMutex
	filePath      string
	latencyPreset string
	lossPreset    string
}

// Preset mapping table
var (
	latencyPresets = map[string]struct {
		warning  int
		critical int
	}{
		"standard":      {warning: 80, critical: 150},
		"tolerant":      {warning: 150, critical: 300},
		"very-tolerant": {warning: 300, critical: 500},
	}

	lossPresets = map[string]struct {
		warning  int
		critical int
	}{
		"standard":      {warning: 2, critical: 5},
		"tolerant":      {warning: 5, critical: 15},
		"very-tolerant": {warning: 10, critical: 25},
	}
)

// NewQualityThresholdsHandler creates a new QualityThresholdsHandler.
func NewQualityThresholdsHandler(optionalPath ...string) *QualityThresholdsHandler {
	path := defaultQualityThresholdsPath
	if len(optionalPath) > 0 && optionalPath[0] != "" {
		path = optionalPath[0]
	}

	h := &QualityThresholdsHandler{
		filePath:      path,
		latencyPreset: "tolerant",
		lossPreset:    "tolerant",
	}

	_ = h.loadConfig()
	return h
}

// SetFilePath updates storage path and reloads settings.
func (h *QualityThresholdsHandler) SetFilePath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.filePath = path
	h.latencyPreset = "tolerant"
	h.lossPreset = "tolerant"
	_ = h.loadConfig()
}

func (h *QualityThresholdsHandler) loadConfig() error {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return err
	}

	var cfg QualityThresholdsSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.Latency.Preset != "" {
		if _, ok := latencyPresets[cfg.Latency.Preset]; ok {
			h.latencyPreset = cfg.Latency.Preset
		}
	}
	if cfg.Loss.Preset != "" {
		if _, ok := lossPresets[cfg.Loss.Preset]; ok {
			h.lossPreset = cfg.Loss.Preset
		}
	}
	return nil
}

func (h *QualityThresholdsHandler) saveConfig() error {
	dir := filepath.Dir(h.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed creating dir %s: %w", dir, err)
	}

	cfg := QualityThresholdsSettings{}
	cfg.Latency.Preset = h.latencyPreset
	cfg.Loss.Preset = h.lossPreset

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed marshalling quality thresholds: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", h.filePath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed opening temp file %s: %w", tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed writing data: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed fsync: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, h.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed renaming to %s: %w", h.filePath, err)
	}

	return nil
}

// Handle handles GET and POST for /api/v1/settings/quality-thresholds and /cgi-bin/quecmanager/settings/quality_thresholds.sh
func (h *QualityThresholdsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.Get(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.Save(w, r)
		return
	}
	Error(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// Get returns current thresholds and presets.
func (h *QualityThresholdsHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	latPreset := h.latencyPreset
	lossPreset := h.lossPreset
	h.mu.RUnlock()

	latCfg, ok := latencyPresets[latPreset]
	if !ok {
		latPreset = "tolerant"
		latCfg = latencyPresets[latPreset]
	}

	lossCfg, ok := lossPresets[lossPreset]
	if !ok {
		lossPreset = "tolerant"
		lossCfg = lossPresets[lossPreset]
	}

	isDefault := latPreset == "tolerant" && lossPreset == "tolerant"

	resp := QualityThresholdsResponse{
		Latency: LatencyThresholdConfig{
			Preset:     latPreset,
			WarningMs:  latCfg.warning,
			CriticalMs: latCfg.critical,
		},
		Loss: LossThresholdConfig{
			Preset:      lossPreset,
			WarningPct:  lossCfg.warning,
			CriticalPct: lossCfg.critical,
		},
		IsDefault: isDefault,
	}

	JSON(w, http.StatusOK, resp)
}

// Save receives updated presets and persists them.
func (h *QualityThresholdsHandler) Save(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Latency struct {
			Preset string `json:"preset"`
		} `json:"latency"`
		Loss struct {
			Preset string `json:"preset"`
		} `json:"loss"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if payload.Latency.Preset != "" {
		if _, ok := latencyPresets[payload.Latency.Preset]; !ok {
			Error(w, http.StatusBadRequest, fmt.Sprintf("Invalid latency preset: %s", payload.Latency.Preset))
			return
		}
		h.latencyPreset = payload.Latency.Preset
	}

	if payload.Loss.Preset != "" {
		if _, ok := lossPresets[payload.Loss.Preset]; !ok {
			Error(w, http.StatusBadRequest, fmt.Sprintf("Invalid loss preset: %s", payload.Loss.Preset))
			return
		}
		h.lossPreset = payload.Loss.Preset
	}

	if err := h.saveConfig(); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save quality thresholds: %v", err))
		return
	}

	latCfg := latencyPresets[h.latencyPreset]
	lossCfg := lossPresets[h.lossPreset]
	isDefault := h.latencyPreset == "tolerant" && h.lossPreset == "tolerant"

	resp := QualityThresholdsResponse{
		Latency: LatencyThresholdConfig{
			Preset:     h.latencyPreset,
			WarningMs:  latCfg.warning,
			CriticalMs: latCfg.critical,
		},
		Loss: LossThresholdConfig{
			Preset:      h.lossPreset,
			WarningPct:  lossCfg.warning,
			CriticalPct: lossCfg.critical,
		},
		IsDefault: isDefault,
	}

	JSON(w, http.StatusOK, resp)
}
