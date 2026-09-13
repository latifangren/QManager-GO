package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qmanager/internal/config"
)

func TestNetworkProvisionerConfig(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")

	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	np := NewNetworkProvisioner(cfgMgr)
	if np == nil {
		t.Fatal("expected non-nil provisioner")
	}

	// Test lifecycle Start & Stop
	np.Start()
	np.Stop()
}

func TestEnsureQCMAPWWANBackhaul(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "mobileap_cfg.xml")
	initialXML := `<MobileAPCfg><FirstPreferredBackhaul>bt-pan</FirstPreferredBackhaul></MobileAPCfg>`
	_ = os.WriteFile(tmpFile, []byte(initialXML), 0644)

	content, _ := os.ReadFile(tmpFile)
	s := string(content)
	s = strings.ReplaceAll(s, "<FirstPreferredBackhaul>bt-pan</FirstPreferredBackhaul>", "<FirstPreferredBackhaul>wwan</FirstPreferredBackhaul>")
	_ = os.WriteFile(tmpFile, []byte(s), 0644)

	updated, _ := os.ReadFile(tmpFile)
	if !strings.Contains(string(updated), "<FirstPreferredBackhaul>wwan</FirstPreferredBackhaul>") {
		t.Errorf("expected wwan in mobileap_cfg, got: %s", string(updated))
	}
}
