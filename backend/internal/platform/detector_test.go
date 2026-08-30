package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectIdentity(t *testing.T) {
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
	if id.SoC != "SDX55" {
		t.Errorf("expected SoC=SDX55, got %s", id.SoC)
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
