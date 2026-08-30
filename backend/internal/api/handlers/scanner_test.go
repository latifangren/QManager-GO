package handlers

import (
	"testing"
)

func TestCalculateLTEFrequency(t *testing.T) {
	// Test Band 3 (EARFCN: 1675)
	// DLLow = 1805, Offset = 1200 -> 1805 + (1675 - 1200) * 0.1 = 1852.5 MHz
	// ULLow = 1710 -> 1710 + (1675 - 1200) * 0.1 = 1757.5 MHz
	res := CalculateLTEFrequency(1675)
	if !res.Valid {
		t.Fatalf("expected valid calculation for EARFCN 1675")
	}
	if res.DLFrequencyMHz != 1852.5 {
		t.Errorf("expected DL 1852.5, got %f", res.DLFrequencyMHz)
	}
	if res.ULFrequencyMHz == nil || *res.ULFrequencyMHz != 1757.5 {
		t.Errorf("expected UL 1757.5, got %v", res.ULFrequencyMHz)
	}
	if len(res.MatchingBands) == 0 || res.MatchingBands[0].Band != "B3" {
		t.Errorf("expected matching band B3, got %+v", res.MatchingBands)
	}

	// Test TDD Band 40 (EARFCN: 39150)
	// DLLow = 2300, Offset = 38650 -> 2300 + (39150 - 38650) * 0.1 = 2350.0 MHz
	resTDD := CalculateLTEFrequency(39150)
	if !resTDD.Valid {
		t.Fatalf("expected valid calculation for EARFCN 39150")
	}
	if resTDD.DLFrequencyMHz != 2350.0 {
		t.Errorf("expected DL 2350.0, got %f", resTDD.DLFrequencyMHz)
	}
	if resTDD.ULFrequencyMHz == nil || *resTDD.ULFrequencyMHz != 2350.0 {
		t.Errorf("expected UL 2350.0 for TDD, got %v", resTDD.ULFrequencyMHz)
	}
}

func TestCalculateNRFrequency(t *testing.T) {
	// Test n78 (NR-ARFCN: 633334)
	// 3000 MHz + 0.015 * (633334 - 600000) = 3000 + 0.015 * 33334 = 3500.01 MHz
	res := CalculateNRFrequency(633334)
	if !res.Valid {
		t.Fatalf("expected valid calculation for NR-ARFCN 633334")
	}
	if res.DLFrequencyMHz < 3500.0 || res.DLFrequencyMHz > 3500.02 {
		t.Errorf("expected ~3500.01 MHz DL, got %f", res.DLFrequencyMHz)
	}
	foundN78 := false
	for _, m := range res.MatchingBands {
		if m.Band == "n78" {
			foundN78 = true
			break
		}
	}
	if !foundN78 {
		t.Errorf("expected n78 in matching bands, got %+v", res.MatchingBands)
	}
}

func TestParseQScanOutput(t *testing.T) {
	raw := `
+QSCAN: "LTE",510,11,1675,218,-85,-9,1747802,42319,50,3
+QSCAN: "NR5G",510,11,633334,120,-82,-11,2938120,42319,100,78,30

OK
`
	items := ParseQScanOutput(raw)
	if len(items) != 2 {
		t.Fatalf("expected 2 parsed cells, got %d", len(items))
	}

	c0 := items[0]
	if c0.NetworkType != "LTE" || c0.MCC != 510 || c0.MNC != 11 || c0.EARFCN != 1675 || c0.PCI != 218 || c0.Band != 3 {
		t.Errorf("cell 0 parsed incorrectly: %+v", c0)
	}

	c1 := items[1]
	if c1.NetworkType != "NR5G" || c1.Band != 78 || c1.SCS == nil || *c1.SCS != 30 {
		t.Errorf("cell 1 parsed incorrectly: %+v", c1)
	}
}

func TestParseNeighbourCellOutput(t *testing.T) {
	raw := `
+QENG: "neighbourcell intra","LTE",1675,218,-85,-9,-62,0,18,0,-
+QENG: "neighbourcell inter","LTE",1675,219,-88,-11,-65,0,15,0,-

OK
`
	cells := ParseNeighbourCellOutput(raw)
	if len(cells) != 2 {
		t.Fatalf("expected 2 neighbour cells, got %d", len(cells))
	}

	if cells[0].PCI != 218 || cells[0].RSRP != -85 || cells[0].RSRQ == nil || *cells[0].RSRQ != -9 {
		t.Errorf("neighbour cell 0 parsed incorrectly: %+v", cells[0])
	}
}
