package platform

import (
	"runtime"
	"testing"
)

func TestInitFirewallRules(t *testing.T) {
	err := InitFirewallRules()
	if err != nil {
		t.Fatalf("InitFirewallRules returned unexpected error: %v", err)
	}

	if runtime.GOOS != "linux" {
		t.Log("Skipping execution test on non-linux host")
	}
}
