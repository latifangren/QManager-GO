package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
)

func TestPoller_Basic(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`)
	mock.SetResponse(`AT+QCAINFO`, `+QCAINFO: "PCC",1675,100,"LTE BAND 3",1,218,-85,-9,-62,18`)

	eng := atengine.NewEngine(mock)
	id := platform.Identity{
		Model:   "RG501QEU_VD",
		SoC:     "SDX55",
		IsSDX55: true,
	}

	poller := NewPoller(eng, id, 100*time.Millisecond)
	poller.Start()
	time.Sleep(250 * time.Millisecond)
	poller.Stop()

	status := poller.GetStatus()
	if status == nil {
		t.Fatalf("expected non-nil status")
	}
	if status.Band != "B3" {
		t.Errorf("expected Band=B3, got %s", status.Band)
	}
	if status.DeviceModel != "RG501QEU_VD" {
		t.Errorf("expected DeviceModel=RG501QEU_VD, got %s", status.DeviceModel)
	}
	if len(status.CA) != 1 {
		t.Errorf("expected 1 CA component, got %d", len(status.CA))
	}
}

func TestPingProber(t *testing.T) {
	prober := NewPingProber("127.0.0.1:9", 100*time.Millisecond)
	sample := prober.ProbeOnce()

	// Should record a sample (even if connection refused/failed)
	if sample.Timestamp == 0 {
		t.Errorf("expected non-zero timestamp")
	}

	stats := prober.GetStats()
	if len(stats.RecentPoints) == 0 {
		t.Errorf("expected recent points in stats")
	}
}

func TestWatchdog(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CFUN=0", "OK")
	mock.SetResponse("AT+CFUN=1", "OK")

	eng := atengine.NewEngine(mock)
	cfgMgr, _ := config.NewManager(t.TempDir() + "/qmanager.conf")
	prober := NewPingProber("127.0.0.1:9", 50*time.Millisecond)

	watchdog := NewWatchdog(eng, cfgMgr, prober)
	watchdog.Start()
	time.Sleep(100 * time.Millisecond)
	watchdog.Stop()
}

func TestAlertDispatcher(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CMGF=1", "OK")
	eng := atengine.NewEngine(mock)

	dispatcher := NewAlertDispatcher(eng)
	dispatcher.SetSMSConfig(SMSAlertConfig{
		Enabled:     true,
		PhoneNumber: "+628123456789",
	})

	alert := AlertPayload{
		Level:     "WARNING",
		Title:     "High Temp",
		Message:   "Modem temperature reached 75C",
		Timestamp: time.Now(),
	}

	dispatcher.Dispatch(context.Background(), alert)
	time.Sleep(100 * time.Millisecond)

	history := dispatcher.GetHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 alert in history, got %d", len(history))
	}
	if history[0].Title != "High Temp" {
		t.Errorf("expected Title='High Temp', got %s", history[0].Title)
	}
}

func TestSMSForwarder_Cycle(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+CPMS="ME","ME","ME"`, "OK")
	mock.SetResponse(`AT+CPMS="SM","SM","SM"`, "OK")
	mock.SetResponse("AT+CPMS?", `+CPMS: "ME",1,255,"ME",1,255,"SM",0,40`)
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CREG?", "+CREG: 0,1\nOK")
	mock.SetResponse(`AT+CMGL="ALL"`, `+CMGL: 1,"REC READ","+123456789",,"08/24/26,10:00:00+00"
Secret OTP 8899

OK`)
	mock.SetResponse(`AT+CMGS="+62899999999"`+"\rFrom +123456789: Secret OTP 8899\x1A", "OK")

	eng := atengine.NewEngine(mock)
	tmpDir := t.TempDir()
	cfgMgr, _ := config.NewManager(tmpDir + "/qmanager.conf")

	cfgPath := tmpDir + "/sms_forwarding.json"
	failPath := tmpDir + "/sms_forward_failures.json"
	seenPath := tmpDir + "/sms_forward_seen"

	cfg := SMSForwardingSettings{
		Enabled:     true,
		TargetPhone: "+62899999999",
	}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, data, 0644)

	t.Setenv("SMS_FORWARD_CONFIG_PATH", cfgPath)
	t.Setenv("SMS_FORWARD_FAILURES_PATH", failPath)
	t.Setenv("SMS_FORWARD_SEEN_PATH", seenPath)

	forwarder := NewSMSForwarder(eng, cfgMgr)
	forwarder.runCycle(false)

	// Verify fingerprint was marked seen
	seenData, err := os.ReadFile(seenPath)
	if err != nil || len(seenData) == 0 {
		t.Errorf("expected seen file to contain message fingerprint")
	}
}
