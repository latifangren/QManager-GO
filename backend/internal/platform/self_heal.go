package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	PlatformSchemaVersion = 1
	DefaultPlatformJSON   = "/etc/qmanager/platform.json"
)

// PlatformProfile is the serialized JSON structure of /etc/qmanager/platform.json
type PlatformProfile struct {
	Schema        int                    `json:"schema"`
	Model         string                 `json:"model"`
	SoC           string                 `json:"soc"`
	FormFactor    string                 `json:"form_factor"`
	Tier          string                 `json:"tier"`
	FWFingerprint string                 `json:"fw_fingerprint"`
	Caps          map[string]interface{} `json:"caps"`
	UpdatedAt     string                 `json:"updated_at,omitempty"`
}

// DeriveFormFactor determines M.2 vs LGA from model string.
func DeriveFormFactor(model string) string {
	m := strings.ToUpper(model)
	if strings.HasPrefix(m, "RM") {
		return "M.2"
	}
	if strings.HasPrefix(m, "RG") || strings.HasPrefix(m, "EM") {
		return "LGA"
	}
	return "unknown"
}

// DeriveTier determines reference vs community tier.
func DeriveTier(id Identity) string {
	if id.IsSDX65 || strings.Contains(strings.ToUpper(id.Model), "RM520N") {
		return "reference"
	}
	return "community"
}

// SyncPlatformJSON writes or updates the hardware profile at destPath.
// Protects against symlink hijacking by refusing non-regular destinations and using atomic tmp+rename.
func SyncPlatformJSON(id Identity, destPath string) error {
	if destPath == "" {
		destPath = DefaultPlatformJSON
	}

	// 1. Guard against directory or symlink target
	if fi, err := os.Lstat(destPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write platform profile: %s is a symlink", destPath)
		}
		if fi.IsDir() {
			return fmt.Errorf("refusing to write platform profile: %s is a directory", destPath)
		}
	}

	formFactor := DeriveFormFactor(id.Model)
	tier := DeriveTier(id)
	fwFingerprint := id.Revision
	if fwFingerprint == "" || fwFingerprint == "unknown" {
		fwFingerprint = id.Model
	}

	profile := PlatformProfile{
		Schema:        PlatformSchemaVersion,
		Model:         id.Model,
		SoC:           id.SoC,
		FormFactor:    formFactor,
		Tier:          tier,
		FWFingerprint: fwFingerprint,
		Caps:          map[string]interface{}{},
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal platform profile: %w", err)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", destPath, time.Now().UnixNano())
	// Symlink check on tmp path
	if fi, err := os.Lstat(tmpPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("temp path is a symlink: %s", tmpPath)
		}
		_ = os.Remove(tmpPath)
	}

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp profile: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp profile to %s: %w", destPath, err)
	}

	return nil
}

// NeedsRegen checks if platform.json is missing or outdated.
func NeedsRegen(destPath string, currentFingerprint string) bool {
	data, err := os.ReadFile(destPath)
	if err != nil {
		return true
	}

	var p PlatformProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return true
	}

	if p.Schema != PlatformSchemaVersion {
		return true
	}

	if currentFingerprint != "" && currentFingerprint != "unknown" && p.FWFingerprint != currentFingerprint {
		return true
	}

	return false
}
