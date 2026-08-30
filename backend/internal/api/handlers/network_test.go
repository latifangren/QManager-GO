package handlers

import (
	"testing"
)

func TestValidateMAC(t *testing.T) {
	validMACs := []string{
		"00:1A:2B:3C:4D:5E",
		"00-1A-2B-3C-4D-5E",
		"aa:bb:cc:dd:ee:ff",
		"AA:BB:CC:DD:EE:FF",
	}
	invalidMACs := []string{
		"00:1A:2B:3C:4D",
		"00:1A:2B:3C:4D:5E:6F",
		"00:1G:2B:3C:4D:5E",
		"invalid",
		"",
	}

	for _, mac := range validMACs {
		if !validateMAC(mac) {
			t.Errorf("validateMAC(%q) = false; want true", mac)
		}
	}
	for _, mac := range invalidMACs {
		if validateMAC(mac) {
			t.Errorf("validateMAC(%q) = true; want false", mac)
		}
	}
}

func TestTrafficEngineHostlistParsing(t *testing.T) {
	domains := []string{"example.com", "test.org", "googlevideo.com"}
	p := VideoOptimizerSavePayload{
		Domains: domains,
	}
	if len(p.Domains) != 3 {
		t.Errorf("expected 3 domains, got %d", len(p.Domains))
	}
}
