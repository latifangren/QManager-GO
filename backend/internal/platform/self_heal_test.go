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

	if p.Schema != 1 || p.Model != "RM520NGL_VC" || p.FormFactor != "M.2" || p.Tier != "reference" {
		t.Errorf("profile fields mismatch: %+v", p)
	}
}

func TestDeriveFormFactorAndTier(t *testing.T) {
	if DeriveFormFactor("RM520N-GL") != "M.2" {
		t.Errorf("RM520N-GL should be M.2")
	}
	if DeriveFormFactor("RG501Q-EU") != "LGA" {
		t.Errorf("RG501Q-EU should be LGA")
	}

	idRM := Identity{Model: "RM520NGL_VC", IsSDX65: true}
	if DeriveTier(idRM) != "reference" {
		t.Errorf("RM520N should be reference tier")
	}

	idRG := Identity{Model: "RG501QEU_VD", IsSDX55: true}
	if DeriveTier(idRG) != "community" {
		t.Errorf("RG501Q should be community tier")
	}
}
