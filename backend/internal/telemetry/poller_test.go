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
