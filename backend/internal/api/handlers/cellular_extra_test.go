package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

// 1. Cellular Status & Control Tests (cellular.go)
func TestCellularHandler_Full(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() { _ = eng.Close() })

	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 1*time.Second)
	h := NewCellularHandler(eng, poller)

	// Status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/status", nil)
	wStatus := httptest.NewRecorder()
	h.Status(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("Status returned %d, want 200", wStatus.Code)
	}

	// SendCommand - valid AT command
	mock.SetResponse("AT+CPIN?", "+CPIN: READY\r\nOK")
	bodyCmd, _ := json.Marshal(SendATRequest{Command: "AT+CPIN?"})
	reqCmd := httptest.NewRequest(http.MethodPost, "/api/cellular/command", bytes.NewBuffer(bodyCmd))
	wCmd := httptest.NewRecorder()
	h.SendCommand(wCmd, reqCmd)
	if wCmd.Code != http.StatusOK {
		t.Fatalf("SendCommand returned %d, want 200", wCmd.Code)
	}

	// SendCommand - bad JSON
	reqBadCmd := httptest.NewRequest(http.MethodPost, "/api/cellular/command", bytes.NewBufferString(`invalid`))
	wBadCmd := httptest.NewRecorder()
	h.SendCommand(wBadCmd, reqBadCmd)
	if wBadCmd.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad command payload, got %d", wBadCmd.Code)
	}

	// GetBands
	mock.SetResponse(`AT+QNWPREFCFG="gw_band";+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`, `+QNWPREFCFG: "lte_band",1:3:5:7:8:20:28:38:40:41`+"\r\nOK")
	reqGetBands := httptest.NewRequest(http.MethodGet, "/api/cellular/bands", nil)
	wGetBands := httptest.NewRecorder()
	h.GetBands(wGetBands, reqGetBands)
	if wGetBands.Code != http.StatusOK {
		t.Fatalf("GetBands returned %d, want 200", wGetBands.Code)
	}

	// LockBands
	mock.SetResponse(`AT+QNWPREFCFG="lte_band",1:3`, "OK")
	mock.SetResponse(`AT+QNWPREFCFG="nr5g_band",78`, "OK")
	bodyLock, _ := json.Marshal(SetBandsRequest{
		LTEBands: []string{"1", "3"},
		NRBands:  []string{"78"},
	})
	reqLock := httptest.NewRequest(http.MethodPost, "/api/cellular/bands", bytes.NewBuffer(bodyLock))
	wLock := httptest.NewRecorder()
	h.LockBands(wLock, reqLock)
	if wLock.Code != http.StatusOK {
		t.Fatalf("LockBands returned %d, want 200", wLock.Code)
	}

	// LockTower - 4G
	mock.SetResponse(`AT+QNWLOCK="common/4g",1,1675,218`, "OK")
	bodyTower4G, _ := json.Marshal(map[string]interface{}{
		"mode":   "4g",
		"earfcn": 1675,
		"pcid":   218,
	})
	reqTower4G := httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(bodyTower4G))
	wTower4G := httptest.NewRecorder()
	h.LockTower(wTower4G, reqTower4G)
	if wTower4G.Code != http.StatusOK {
		t.Fatalf("LockTower 4G returned %d, want 200", wTower4G.Code)
	}

	// LockTower - 5G
	mock.SetResponse(`AT+QNWLOCK="common/5g",1,627392,320,30`, "OK")
	bodyTower5G, _ := json.Marshal(map[string]interface{}{
		"mode":   "5g",
		"earfcn": 627392,
		"pcid":   320,
		"scs":    30,
	})
	reqTower5G := httptest.NewRequest(http.MethodPost, "/api/cellular/lock-tower", bytes.NewBuffer(bodyTower5G))
	wTower5G := httptest.NewRecorder()
	h.LockTower(wTower5G, reqTower5G)
	if wTower5G.Code != http.StatusOK {
		t.Fatalf("LockTower 5G returned %d, want 200", wTower5G.Code)
	}

	// UnlockTower
	mock.SetResponse(`AT+QNWLOCK="common/4g",0`, "OK")
	mock.SetResponse(`AT+QNWLOCK="common/5g",0`, "OK")
	mock.SetResponse(`AT+QNWLOCK="save_ctrl",0`, "OK")
	reqUnlock := httptest.NewRequest(http.MethodPost, "/api/cellular/unlock-tower", nil)
	wUnlock := httptest.NewRecorder()
	h.UnlockTower(wUnlock, reqUnlock)
	if wUnlock.Code != http.StatusOK {
		t.Fatalf("UnlockTower returned %d, want 200", wUnlock.Code)
	}
}

// 2. Cell Scanner & Neighbour Scanner Tests
func TestCellAndNeighbourScannerHandlers(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() { _ = eng.Close() })

	// Cell Scanner
	mock.SetResponse(`AT+QSCAN="all"`, `+QSCAN: "LTE",510,11,1675,218,-85,-9,1739580,39503,20,3`+"\r\nOK")
	cellScanH := NewCellScannerHandler(eng)

	reqStart := httptest.NewRequest(http.MethodPost, "/api/cellular/scanner/start", nil)
	wStart := httptest.NewRecorder()
	cellScanH.StartScan(wStart, reqStart)
	if wStart.Code != http.StatusOK {
		t.Fatalf("Cell scan start returned %d, want 200", wStart.Code)
	}

	time.Sleep(100 * time.Millisecond)

	reqStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/scanner/status", nil)
	wStatus := httptest.NewRecorder()
	cellScanH.ScanStatus(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("Cell scan status returned %d, want 200", wStatus.Code)
	}

	// Neighbour Scanner
	mock.SetResponse(`AT+QENG="neighbourcell"`, `+QENG: "neighbourcell intra","LTE",1675,218,-85,-9,-62,0,18,0,-`+"\r\nOK")
	neighbourH := NewNeighbourScannerHandler(eng)

	reqNeighbourStart := httptest.NewRequest(http.MethodPost, "/api/cellular/neighbour/start", nil)
	wNeighbourStart := httptest.NewRecorder()
	neighbourH.StartScan(wNeighbourStart, reqNeighbourStart)
	if wNeighbourStart.Code != http.StatusOK {
		t.Fatalf("Neighbour scan start returned %d, want 200", wNeighbourStart.Code)
	}

	time.Sleep(100 * time.Millisecond)

	reqNeighbourStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/neighbour/status", nil)
	wNeighbourStatus := httptest.NewRecorder()
	neighbourH.ScanStatus(wNeighbourStatus, reqNeighbourStatus)
	if wNeighbourStatus.Code != http.StatusOK {
		t.Fatalf("Neighbour scan status returned %d, want 200", wNeighbourStatus.Code)
	}
}
