package atengine

import (
	"testing"
)

func TestParseQENGServingCell_LTE(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`
	info := ParseQENGServingCell(raw)
	if info == nil {
		t.Fatalf("expected non-nil CellInfo")
	}
	if info.Mode != "LTE" {
		t.Errorf("expected Mode=LTE, got %s", info.Mode)
	}
	if info.Band != "B3" {
		t.Errorf("expected Band=B3, got %s", info.Band)
	}
	if info.PCID != 218 {
		t.Errorf("expected PCID=218, got %d", info.PCID)
	}
	if info.RSRP != -85 || info.RSRQ != -9 || info.SINR != 18 {
		t.Errorf("signal mismatch: %+v", info)
	}
}

func TestParseQENGServingCell_NR5G(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN","NR5G-SA","TDD",510,11,402100,320,1234,627392,78,100,-78,-10,25`
	info := ParseQENGServingCell(raw)
	if info == nil {
		t.Fatalf("expected non-nil CellInfo")
	}
	if info.Mode != "NR5G-SA" {
		t.Errorf("expected Mode=NR5G-SA, got %s", info.Mode)
	}
	if info.Band != "n78" {
		t.Errorf("expected Band=n78, got %s", info.Band)
	}
	if info.PCID != 320 {
		t.Errorf("expected PCID=320, got %d", info.PCID)
	}
}

func TestParseQCAINFO(t *testing.T) {
	raw := `+QCAINFO: "PCC",1675,100,"LTE BAND 3",1,218,-85,-9,-62,18
+QCAINFO: "SCC",300,50,"LTE BAND 1",1,120,-90,-11,-68,14`

	ca := ParseQCAINFO(raw)
	if len(ca) != 2 {
		t.Fatalf("expected 2 CA components, got %d", len(ca))
	}
	if ca[0].Role != "PCC" || ca[0].Band != "B3" {
		t.Errorf("unexpected PCC: %+v", ca[0])
	}
	if ca[1].Role != "SCC" || ca[1].Band != "B1" {
		t.Errorf("unexpected SCC: %+v", ca[1])
	}
	if ca[0].BandwidthMHz != 20 || ca[1].BandwidthMHz != 10 {
		t.Errorf("unexpected bandwidths: PCC=%d SCC=%d", ca[0].BandwidthMHz, ca[1].BandwidthMHz)
	}
}

func TestParseCSQ(t *testing.T) {
	raw := `+CSQ: 24,99`
	csq := ParseCSQ(raw)
	if csq == nil {
		t.Fatalf("expected non-nil CSQ")
	}
	if csq.RSSI != 24 {
		t.Errorf("expected RSSI=24, got %d", csq.RSSI)
	}
	if csq.RSRPDbm != -65 {
		t.Errorf("expected RSRPDbm=-65, got %d", csq.RSRPDbm)
	}
}
