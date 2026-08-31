package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectIdentity_RG501Q_EU(t *testing.T) {
	tmpDir := t.TempDir()
	verFile := filepath.Join(tmpDir, "quectel-project-version")
	cmdFile := filepath.Join(tmpDir, "cmdline")

	verContent := `Project Name: RG501QEU_VD
Project Rev : RG501QEUAAR12A08M4G_01.001.01.001
Branch  Name: SDX55
Custom  Name: STD
Package Time: 2024-05-10,14:20
`
	cmdContent := `console=ttyMSM0,115200 root=/dev/ubiblock0_0 ro rootfstype=squashfs androidboot.serialno=b7e3d6f1`

	if err := os.WriteFile(verFile, []byte(verContent), 0644); err != nil {
		t.Fatalf("failed to write version file: %v", err)
	}
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0644); err != nil {
		t.Fatalf("failed to write cmdline file: %v", err)
	}

	id := DetectIdentity(verFile, cmdFile)

	if id.Model != "RG501QEU_VD" {
		t.Errorf("expected Model=RG501QEU_VD, got %s", id.Model)
	}
	if id.Revision != "RG501QEUAAR12A08M4G_01.001.01.001" {
		t.Errorf("expected Revision=RG501QEUAAR12A08M4G_01.001.01.001, got %s", id.Revision)
	}
	if id.SoC != "SDX55" {
		t.Errorf("expected SoC=SDX55, got %s", id.SoC)
	}
	if id.CustomName != "STD" {
		t.Errorf("expected CustomName=STD, got %s", id.CustomName)
	}
	if id.Serial != "b7e3d6f1" {
		t.Errorf("expected Serial=b7e3d6f1, got %s", id.Serial)
	}
	if !id.IsSDX55 {
		t.Errorf("expected IsSDX55=true, got false")
	}
	if id.IsSDX65 {
		t.Errorf("expected IsSDX65=false, got true")
	}
}

func TestDetectIdentity_RM520N_GL(t *testing.T) {
	tmpDir := t.TempDir()
	verFile := filepath.Join(tmpDir, "quectel-project-version")
	cmdFile := filepath.Join(tmpDir, "cmdline")

	verContent := `Project Name: RM520NGL_VC
Project Rev : RM520NGLAAR03A03M4G_01.001.01.001
Branch  Name: SDX6X
Custom  Name: STD
Package Time: 2024-06-15,10:30
`
	cmdContent := `console=ttyMSM0,115200 androidboot.serialno=61368cd2 init=/sbin/init`

	if err := os.WriteFile(verFile, []byte(verContent), 0644); err != nil {
		t.Fatalf("failed to write version file: %v", err)
	}
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0644); err != nil {
		t.Fatalf("failed to write cmdline file: %v", err)
	}

	id := DetectIdentity(verFile, cmdFile)

	if id.Model != "RM520NGL_VC" {
		t.Errorf("expected Model=RM520NGL_VC, got %s", id.Model)
	}
	if id.Revision != "RM520NGLAAR03A03M4G_01.001.01.001" {
		t.Errorf("expected Revision=RM520NGLAAR03A03M4G_01.001.01.001, got %s", id.Revision)
	}
	if id.SoC != "SDX6X" {
		t.Errorf("expected SoC=SDX6X, got %s", id.SoC)
	}
	if id.CustomName != "STD" {
		t.Errorf("expected CustomName=STD, got %s", id.CustomName)
	}
	if id.Serial != "61368cd2" {
		t.Errorf("expected Serial=61368cd2, got %s", id.Serial)
	}
	if id.IsSDX55 {
		t.Errorf("expected IsSDX55=false, got true")
	}
	if !id.IsSDX65 {
		t.Errorf("expected IsSDX65=true, got false")
	}
}

func TestDetectIdentity_UnknownFallback(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentVer := filepath.Join(tmpDir, "missing_ver")
	nonExistentCmd := filepath.Join(tmpDir, "missing_cmd")

	id := DetectIdentity(nonExistentVer, nonExistentCmd)

	if id.Model != "unknown" {
		t.Errorf("expected Model=unknown, got %s", id.Model)
	}
	if id.Revision != "unknown" {
		t.Errorf("expected Revision=unknown, got %s", id.Revision)
	}
	if id.SoC != "unknown" {
		t.Errorf("expected SoC=unknown, got %s", id.SoC)
	}
	if id.CustomName != "unknown" {
		t.Errorf("expected CustomName=unknown, got %s", id.CustomName)
	}
	if id.Serial != "unknown" {
		t.Errorf("expected Serial=unknown, got %s", id.Serial)
	}
	if id.IsSDX55 || id.IsSDX65 {
		t.Errorf("expected IsSDX55 and IsSDX65 to be false for unknown platform")
	}
}

func TestDetectIdentity_MalformedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	verFile := filepath.Join(tmpDir, "quectel-project-version")
	cmdFile := filepath.Join(tmpDir, "cmdline")

	verContent := `Invalid line format
Random Header
Some   Weird  :  Value
`
	cmdContent := `console=ttyMSM0,115200 without_serialno`

	if err := os.WriteFile(verFile, []byte(verContent), 0644); err != nil {
		t.Fatalf("failed to write malformed version file: %v", err)
	}
	if err := os.WriteFile(cmdFile, []byte(cmdContent), 0644); err != nil {
		t.Fatalf("failed to write malformed cmdline file: %v", err)
	}

	id := DetectIdentity(verFile, cmdFile)
	if id.Model != "unknown" || id.Revision != "unknown" || id.Serial != "unknown" {
		t.Errorf("expected unknown fields for malformed files, got %+v", id)
	}

	// Test default path call
	_ = DetectIdentity("", "")
}
