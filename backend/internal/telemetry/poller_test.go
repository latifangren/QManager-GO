package telemetry

import (
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/platform"
)

func TestPoller_LifecycleAndSnapshot(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`)
	mock.SetResponse(`AT+QCAINFO`, "+QCAINFO: \"PCC\",1675,100,\"LTE BAND 3\",1,218,-85,-9,-62,18\n+QCAINFO: \"SCC\",300,50,\"LTE BAND 1\",1,120,-90,-11,-68,14\nOK")
	mock.SetResponse(`AT+CSQ`, `+CSQ: 25,99`)

	eng := atengine.NewEngine(mock)
	id := platform.Identity{
		Model:    "RG501QEU_VD",
		Revision: "RG501QEUAAR12A08M4G",
		SoC:      "SDX55",
		Serial:   "b7e3d6f1",
		IsSDX55:  true,
	}

	poller := NewPoller(eng, id, 50*time.Millisecond)

	// Initial default status before start
	initStatus := poller.GetStatus()
	if initStatus.DeviceModel != "RG501QEU_VD" {
		t.Errorf("expected DeviceModel RG501QEU_VD, got %s", initStatus.DeviceModel)
	}

	// Start polling loop
	poller.Start()
	poller.Start() // Idempotent start

	time.Sleep(150 * time.Millisecond)

	status := poller.GetStatus()
	if !status.Online {
		t.Errorf("expected Online=true after servingcell parsed")
	}
	if status.Band != "B3" {
		t.Errorf("expected Band=B3, got %s", status.Band)
	}
	if status.Network.CACount != 2 || !status.Network.CAActive {
		t.Errorf("expected 2 CA components with CAActive=true, got count=%d", status.Network.CACount)
	}
	if status.Signal.RSRP != -85 || status.Signal.SINR != 18 {
		t.Errorf("unexpected signal values: %+v", status.Signal)
	}
	if status.Cell.PCID != 218 || status.Cell.EARFCN != 1675 {
		t.Errorf("unexpected cell info: %+v", status.Cell)
	}

	poller.Stop()
	poller.Stop() // Idempotent stop
}

func TestPoller_CSQFallback(t *testing.T) {
	mock := atengine.NewMockTransport()
	// Serving cell fails
	mock.SetResponse(`AT+QENG="servingcell"`, "ERROR")
	mock.SetResponse(`AT+QCAINFO`, "ERROR")
	// CSQ returns valid signal
	mock.SetResponse(`AT+CSQ`, `+CSQ: 20,99`)

	eng := atengine.NewEngine(mock)
	id := platform.Identity{
		Model: "RM520NGL_VC",
	}

	poller := NewPoller(eng, id, 50*time.Millisecond)
	poller.poll()

	status := poller.GetStatus()
	// CSQ rssi=20 converts to -73 dBm (-113 + 20*2)
	if status.RSSI != -73 {
		t.Errorf("expected CSQ fallback RSSI=-73, got %d", status.RSSI)
	}
}

func TestPoller_TransitionStates(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	id := platform.Identity{Model: "RG501Q-EU"}
	poller := NewPoller(eng, id, 50*time.Millisecond)

	// 1. SEARCH state
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","SEARCH"`+"\r\nOK")
	poller.poll()
	sSearch := poller.GetStatus()
	if sSearch.Online {
		t.Errorf("expected Online=false for SEARCH state")
	}
	if sSearch.Network.ServiceStatus != "searching" {
		t.Errorf("expected ServiceStatus='searching', got %q", sSearch.Network.ServiceStatus)
	}

	// 2. LIMSRV state
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","LIMSRV"`+"\r\nOK")
	poller.poll()
	sLimsrv := poller.GetStatus()
	if sLimsrv.Online {
		t.Errorf("expected Online=false for LIMSRV state")
	}
	if sLimsrv.Network.ServiceStatus != "limited_service" {
		t.Errorf("expected ServiceStatus='limited_service', got %q", sLimsrv.Network.ServiceStatus)
	}

	// 3. Empty / Malformed servingcell
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell"`+"\r\nOK")
	poller.poll()
	sEmpty := poller.GetStatus()
	if sEmpty.Online {
		t.Errorf("expected Online=false for empty servingcell")
	}
	if sEmpty.Network.ServiceStatus != "no_service" {
		t.Errorf("expected ServiceStatus='no_service', got %q", sEmpty.Network.ServiceStatus)
	}

	// 4. NR5G-SA connected with hex cellID
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","NOCONN","NR5G-SA","TDD",510,11,1A2B3C,120,500,627264,78,100,-85,-10,15`+"\r\nOK")
	poller.poll()
	sNR := poller.GetStatus()
	if !sNR.Online {
		t.Errorf("expected Online=true for NR5G-SA")
	}
	if sNR.NR.Band != "n78" {
		t.Errorf("expected NR band n78, got %s", sNR.NR.Band)
	}
	// In 5G NR: gNodeB = cid >> 14 (0x1A2B3C >> 14 = 0x68), sector = cid & 0x3FFF (0x1A2B3C & 0x3FFF = 0x2B3C)
	if sNR.Cell.ENodeBID != 0x68 || sNR.Cell.SectorID != 0x2B3C {
		t.Errorf("expected 5G cell id 1A2B3C -> gnodeb=0x68, sector=0x2B3C, got enodeb=%X, sector=%X", sNR.Cell.ENodeBID, sNR.Cell.SectorID)
	}
	if sNR.Network.ServiceStatus != "excellent" {
		t.Errorf("expected ServiceStatus='excellent', got %q", sNR.Network.ServiceStatus)
	}
}

func TestPoller_36BitNRCellID_NoOverflow(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	id := platform.Identity{Model: "RG501Q-EU"}
	poller := NewPoller(eng, id, 50*time.Millisecond)

	// 36-bit NR Cell ID: 0x1A2B3C4D5E (decimal 112394784094, exceeds 32-bit int)
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","NOCONN","NR5G-SA","TDD",510,11,1A2B3C4D5E,120,500,627264,78,100,-85,-10,15`+"\r\nOK")
	poller.poll()
	sNR := poller.GetStatus()

	if !sNR.Online {
		t.Fatalf("expected Online=true")
	}
	if sNR.NR.CellID == nil || *sNR.NR.CellID != int64(0x1A2B3C4D5E) {
		t.Errorf("expected 36-bit CellID=0x1A2B3C4D5E, got %v", sNR.NR.CellID)
	}

	expectedGNodeB := int64(0x1A2B3C4D5E >> 14)
	expectedSector := int64(0x1A2B3C4D5E & 0x3FFF)
	if sNR.NR.GNodeBID == nil || *sNR.NR.GNodeBID != expectedGNodeB {
		t.Errorf("expected GNodeBID=%X, got %v", expectedGNodeB, sNR.NR.GNodeBID)
	}
	if sNR.NR.SectorID == nil || *sNR.NR.SectorID != expectedSector {
		t.Errorf("expected SectorID=%X, got %v", expectedSector, sNR.NR.SectorID)
	}
}
