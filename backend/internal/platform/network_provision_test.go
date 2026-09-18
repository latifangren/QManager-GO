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

	// Default config has AutoProvisionLAN=0 -> Start() should skip cleanly without starting loop
	if cfgMgr.Get().Network.AutoProvisionLAN != 0 {
		t.Fatalf("expected default AutoProvisionLAN=0, got %d", cfgMgr.Get().Network.AutoProvisionLAN)
	}
	np.Start()
	if np.running {
		t.Errorf("expected np.running=false when AutoProvisionLAN=0, got true")
	}
	np.ProvisionOnce() // Should return immediately without touching anything
	np.Stop()

	// Now set AutoProvisionLAN=1 -> Start() should initialize loop and running=true
	err = cfgMgr.Update(func(c *config.Config) {
		c.Network.AutoProvisionLAN = 1
	})
	if err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	np.Start()
	if !np.running {
		t.Errorf("expected np.running=true when AutoProvisionLAN=1, got false")
	}
	np.Stop()
	if np.running {
		t.Errorf("expected np.running=false after Stop(), got true")
	}
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
