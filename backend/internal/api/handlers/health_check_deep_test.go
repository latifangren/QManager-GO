package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func TestHealthCheck_Deep(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT", "OK")
	mock.SetResponse("AT+CPIN?", "+CPIN: READY\r\nOK")
	mock.SetResponse("AT+CREG?", "+CREG: 0,1\r\nOK")
	mock.SetResponse("AT+CGREG?", "+CGREG: 0,1\r\nOK")
	mock.SetResponse("AT+QENG=\"servingcell\"", `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,120,1850,3,3,3,-95,-8,-65,15,-`+"\r\nOK")
	mock.SetResponse("AT+QNWINFO", `+QNWINFO: "FDD LTE","51011","LTE BAND 3",1850`+"\r\nOK")

	eng := atengine.NewEngine(mock)
	defer eng.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)
	_ = cfgMgr
	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 100)

	h := NewHealthCheckHandler(eng, poller, identity)

	// 1. POST Run diagnostics
	wStart := httptest.NewRecorder()
	h.Run(wStart, httptest.NewRequest(http.MethodPost, "/api/system/health/run", nil))
	if wStart.Code != http.StatusOK {
		t.Fatalf("HealthCheck Run returned %d, want 200", wStart.Code)
	}

	// 2. Poll Status
	time.Sleep(100 * time.Millisecond)
	wStatus := httptest.NewRecorder()
	h.Status(wStatus, httptest.NewRequest(http.MethodGet, "/api/system/health/status", nil))
	if wStatus.Code != http.StatusOK {
		t.Fatalf("HealthCheck Status returned %d, want 200", wStatus.Code)
	}
}
