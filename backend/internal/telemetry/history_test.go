package telemetry

import (
	"testing"
)

func TestTelemetryHistory_SignalRing(t *testing.T) {
	hist := NewTelemetryHistory(3, 3, 3)

	val1 := 10
	val2 := 20
	val3 := 30
	val4 := 40

	hist.RecordSignal(SignalHistoryPoint{TS: 100, LteRsrp: []*int{&val1}})
	hist.RecordSignal(SignalHistoryPoint{TS: 101, LteRsrp: []*int{&val2}})
	hist.RecordSignal(SignalHistoryPoint{TS: 102, LteRsrp: []*int{&val3}})

	pts := hist.GetSignalHistory(10)
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if *pts[0].LteRsrp[0] != 10 || *pts[2].LteRsrp[0] != 30 {
		t.Errorf("signal points mismatch: %+v", pts)
	}

	// Wrap around
	hist.RecordSignal(SignalHistoryPoint{TS: 103, LteRsrp: []*int{&val4}})
	ptsWrapped := hist.GetSignalHistory(10)
	if len(ptsWrapped) != 3 {
		t.Fatalf("expected 3 points after wrap, got %d", len(ptsWrapped))
	}
	if *ptsWrapped[0].LteRsrp[0] != 20 || *ptsWrapped[2].LteRsrp[0] != 40 {
		t.Errorf("wrapped signal points mismatch: %+v", ptsWrapped)
	}
}

func TestTelemetryHistory_PingRing(t *testing.T) {
	hist := NewTelemetryHistory(3, 3, 3)

	lat1 := 15.5
	lat2 := 18.2

	hist.RecordPing(PingHistoryPoint{Timestamp: 200, LatencyMs: &lat1, LossPct: 0})
	hist.RecordPing(PingHistoryPoint{Timestamp: 201, LatencyMs: &lat2, LossPct: 5})

	pts := hist.GetPingHistory(10)
	if len(pts) != 2 {
		t.Fatalf("expected 2 ping points, got %d", len(pts))
	}
	if *pts[0].LatencyMs != 15.5 || pts[1].LossPct != 5 {
		t.Errorf("ping points mismatch: %+v", pts)
	}
}

func TestTelemetryHistory_EventRing(t *testing.T) {
	hist := NewTelemetryHistory(3, 3, 3)

	hist.RecordEvent(NetworkEventItem{Timestamp: 300, Type: "link_up", Message: "Modem connected", Severity: "info"})
	hist.RecordEvent(NetworkEventItem{Timestamp: 301, Type: "band_change", Message: "Band switched to B3", Severity: "info"})

	events := hist.GetEvents(10)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "link_up" || events[1].Type != "band_change" {
		t.Errorf("events mismatch: %+v", events)
	}
}
