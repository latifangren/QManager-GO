package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncPlatformJSON(t *testing.T) {
	tempDir := t.TempDir()
	destPath := filepath.Join(tempDir, "platform.json")

	id := Identity{
		Model:    "RM520NGL_VC",
		Revision: "RM520NGLAAR03A03M4G",
		SoC:      "SDX6X",
		IsSDX65:  true,
		Serial:   "61368cd2",
	}

	if !NeedsRegen(destPath, id.Revision) {
		t.Fatalf("expected NeedsRegen to be true for non-existent file")
	}

	err := SyncPlatformJSON(id, destPath)
	if err != nil {
		t.Fatalf("SyncPlatformJSON failed: %v", err)
	}

	if NeedsRegen(destPath, id.Revision) {
		t.Fatalf("expected NeedsRegen to be false after sync")
	}

	// Verify content
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var p PlatformProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("invalid json generated: %v", err)
	}

	if p.Schema != 1 || p.Model != "RM520NGL_VC" || p.FormFactor != "M.2" || p.Tier != "reference" || p.FWFingerprint != "RM520NGLAAR03A03M4G" {
		t.Errorf("profile fields mismatch: %+v", p)
	}
}

func TestSyncPlatformJSON_FallbackRevision(t *testing.T) {
	tempDir := t.TempDir()
	destPath := filepath.Join(tempDir, "platform_fallback.json")

	id := Identity{
		Model:    "RG501QEU_VD",
		Revision: "",
		SoC:      "SDX55",
		IsSDX55:  true,
	}

	err := SyncPlatformJSON(id, destPath)
	if err != nil {
		t.Fatalf("SyncPlatformJSON failed: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var p PlatformProfile
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if p.FWFingerprint != "RG501QEU_VD" {
		t.Errorf("expected FWFingerprint fallback to model RG501QEU_VD, got %s", p.FWFingerprint)
	}
}

func TestSyncPlatformJSON_RefuseSymlinkAndDirectory(t *testing.T) {
	tempDir := t.TempDir()

	id := Identity{
		Model: "RM520NGL_VC",
		SoC:   "SDX6X",
	}

	// 1. Refuse directory destination
	dirDest := filepath.Join(tempDir, "profile_dir")
	if err := os.MkdirAll(dirDest, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	err := SyncPlatformJSON(id, dirDest)
	if err == nil {
		t.Errorf("expected error when destination is a directory, got nil")
	}

	// 2. Refuse symlink destination
	realFile := filepath.Join(tempDir, "real_platform.json")
	if err := os.WriteFile(realFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create real file: %v", err)
	}
	symlinkPath := filepath.Join(tempDir, "symlink_platform.json")
	if err := os.Symlink(realFile, symlinkPath); err == nil {
		err = SyncPlatformJSON(id, symlinkPath)
		if err == nil {
			t.Errorf("expected error when destination is a symlink, got nil")
		}
	}
}

func TestDeriveFormFactorAndTier(t *testing.T) {
	// Form Factor tests
	if DeriveFormFactor("RM520N-GL") != "M.2" {
		t.Errorf("RM520N-GL should be M.2")
	}
	if DeriveFormFactor("rm500q") != "M.2" {
		t.Errorf("rm500q should be M.2")
	}
	if DeriveFormFactor("RG501Q-EU") != "LGA" {
		t.Errorf("RG501Q-EU should be LGA")
	}
	if DeriveFormFactor("EM120R-GL") != "LGA" {
		t.Errorf("EM120R-GL should be LGA")
	}
	if DeriveFormFactor("UNKNOWN_MODEL") != "unknown" {
		t.Errorf("UNKNOWN_MODEL should be unknown")
	}

	// Tier tests
	idRM := Identity{Model: "RM520NGL_VC", IsSDX65: true}
	if DeriveTier(idRM) != "reference" {
		t.Errorf("RM520N should be reference tier")
	}

	idRMByModel := Identity{Model: "RM520N-GL", IsSDX65: false}
	if DeriveTier(idRMByModel) != "reference" {
		t.Errorf("RM520N by model name should be reference tier")
	}

	idRG := Identity{Model: "RG501QEU_VD", IsSDX55: true}
	if DeriveTier(idRG) != "community" {
		t.Errorf("RG501Q should be community tier")
	}
}

func TestNeedsRegen(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Missing file -> needs regen
	missingPath := filepath.Join(tempDir, "missing.json")
	if !NeedsRegen(missingPath, "REV123") {
		t.Errorf("expected true for missing file")
	}

	// 2. Corrupted JSON -> needs regen
	corruptedPath := filepath.Join(tempDir, "corrupted.json")
	if err := os.WriteFile(corruptedPath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}
	if !NeedsRegen(corruptedPath, "REV123") {
		t.Errorf("expected true for corrupted json")
	}

	// 3. Schema version mismatch -> needs regen
	schemaMismatchPath := filepath.Join(tempDir, "schema_mismatch.json")
	schemaMismatchContent := `{"schema": 999, "fw_fingerprint": "REV123"}`
	if err := os.WriteFile(schemaMismatchPath, []byte(schemaMismatchContent), 0644); err != nil {
		t.Fatalf("failed to write schema mismatch file: %v", err)
	}
	if !NeedsRegen(schemaMismatchPath, "REV123") {
		t.Errorf("expected true for schema mismatch")
	}

	// 4. Firmware fingerprint mismatch -> needs regen
	fwMismatchPath := filepath.Join(tempDir, "fw_mismatch.json")
	fwMismatchContent := `{"schema": 1, "fw_fingerprint": "OLD_REV"}`
	if err := os.WriteFile(fwMismatchPath, []byte(fwMismatchContent), 0644); err != nil {
		t.Fatalf("failed to write fw mismatch file: %v", err)
	}
	if !NeedsRegen(fwMismatchPath, "NEW_REV") {
		t.Errorf("expected true for firmware fingerprint mismatch")
	}

	// 5. Valid file and matching fingerprint -> does not need regen
	validPath := filepath.Join(tempDir, "valid.json")
	validContent := `{"schema": 1, "fw_fingerprint": "MATCH_REV"}`
	if err := os.WriteFile(validPath, []byte(validContent), 0644); err != nil {
		t.Fatalf("failed to write valid file: %v", err)
	}
	if NeedsRegen(validPath, "MATCH_REV") {
		t.Errorf("expected false for matching firmware fingerprint")
	}

	// 6. Valid file with empty or unknown current fingerprint -> does not need regen
	if NeedsRegen(validPath, "") {
		t.Errorf("expected false when current fingerprint is empty")
	}
	if NeedsRegen(validPath, "unknown") {
		t.Errorf("expected false when current fingerprint is unknown")
	}
}
