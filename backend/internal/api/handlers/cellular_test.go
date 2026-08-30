package handlers

import (
	"testing"
)

func TestValidateLuhnIMEI(t *testing.T) {
	tests := []struct {
		imei  string
		valid bool
	}{
		{"860123456789012", false}, // random non-luhn
		{"862959040123456", false},
		{"867012040123451", false},
		{"12345678901234", false},  // 14 digits
		{"1234567890123456", false}, // 16 digits
		{"86012345678901a", false}, // non-digit
		{"", false},
	}

	for _, tc := range tests {
		got := ValidateLuhnIMEI(tc.imei)
		if got != tc.valid {
			t.Errorf("ValidateLuhnIMEI(%q) = %v; want %v", tc.imei, got, tc.valid)
		}
	}

	// Test prefix check digit calculation + verification
	prefix := "86012345678901"
	checkDigit, ok := CalculateLuhnCheckDigit(prefix)
	if !ok {
		t.Fatalf("CalculateLuhnCheckDigit failed for %s", prefix)
	}
	validImei := prefix + string('0'+byte(checkDigit))
	if !ValidateLuhnIMEI(validImei) {
		t.Errorf("Constructed IMEI %s should pass Luhn validation", validImei)
	}
}

func TestParseMbnList(t *testing.T) {
	raw := `
+QMBNCFG: "List",0,1,1,"ROW_Commercial",0x08010801,202305091
+QMBNCFG: "List",1,0,0,"Commercial-TMO",0x08010101,202211041
+QMBNCFG: "List",2,0,0,"Telstra-Commercial",0x08010501,202301181

OK
`
	profiles := ParseMbnList(raw)
	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}

	p0 := profiles[0]
	if p0.Index != 0 || !p0.Selected || !p0.Activated || p0.Name != "ROW_Commercial" || p0.Version != "0x08010801" || p0.Date != "202305091" {
		t.Errorf("profile 0 parsed incorrectly: %+v", p0)
	}

	p1 := profiles[1]
	if p1.Index != 1 || p1.Selected || p1.Activated || p1.Name != "Commercial-TMO" {
		t.Errorf("profile 1 parsed incorrectly: %+v", p1)
	}
}

func TestParseFplmnHex(t *testing.T) {
	// Sample EF_FPLMN hex data:
	// PLMN 1: 505 01 (Telstra AU) -> 05 F5 10 (MCC: 505, MNC: 01)
	// Byte 1: 05 -> mcc2=0, mcc1=5 -> mcc1=5, mcc2=0
	// Byte 2: F5 -> mnc3=F, mcc3=5 -> mcc=505, mnc3=F
	// Byte 3: 10 -> mnc2=1, mnc1=0 -> mnc=01
	// PLMN 2: 505 02 (Optus AU) -> 05 F5 20
	// Rest empty: FFFFFFFFFFFF
	hex := "05F51005F520FFFFFFFFFFFF"
	entries := ParseFplmnHex(hex)
	if len(entries) != 2 {
		t.Fatalf("expected 2 FPLMN entries, got %d", len(entries))
	}

	if entries[0].MCC != "505" || entries[0].MNC != "01" || entries[0].PLMN != "50501" {
		t.Errorf("entry 0 parsed incorrectly: %+v", entries[0])
	}
	if entries[1].MCC != "505" || entries[1].MNC != "02" || entries[1].PLMN != "50502" {
		t.Errorf("entry 1 parsed incorrectly: %+v", entries[1])
	}
}

func TestPdpConversions(t *testing.T) {
	if pdpToFrontend("IP") != "ipv4" {
		t.Errorf("pdpToFrontend(IP) = %s; want ipv4", pdpToFrontend("IP"))
	}
	if pdpToFrontend("IPV6") != "ipv6" {
		t.Errorf("pdpToFrontend(IPV6) = %s; want ipv6", pdpToFrontend("IPV6"))
	}
	if pdpToFrontend("IPV4V6") != "ipv4v6" {
		t.Errorf("pdpToFrontend(IPV4V6) = %s; want ipv4v6", pdpToFrontend("IPV4V6"))
	}

	if pdpToAT("ipv4") != "IP" || pdpToAT("ipv6") != "IPV6" || pdpToAT("ipv4v6") != "IPV4V6" {
		t.Errorf("pdpToAT conversion error")
	}
}

func TestRatAcqOrderValidation(t *testing.T) {
	if !isValidRatAcqOrder("NR5G:LTE:WCDMA") {
		t.Errorf("NR5G:LTE:WCDMA should be valid")
	}
	if !isValidRatAcqOrder("LTE:NR5G") {
		t.Errorf("LTE:NR5G should be valid")
	}
	if isValidRatAcqOrder("INVALID:LTE") {
		t.Errorf("INVALID:LTE should be invalid")
	}
}
