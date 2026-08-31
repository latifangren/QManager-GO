package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
)

func TestFplmnNeighbourAndMtu_Deep(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	// 1. Cellular FPLMN Handler
	fplmnH := NewCellularFplmnHandler(eng)

	// 1a. GET FPLMN
	mock.SetResponse("AT+CRSM=192,28539,0,0,15", "+CRSM: 144,0,12\r\nOK")
	mock.SetResponse("AT+CRSM=176,28539,0,0,12", "+CRSM: 144,0,12,\"51010FFFFFFFFFFFFFFFFFFF\"\r\nOK")
	wFplmnGet := httptest.NewRecorder()
	fplmnH.GetFPLMN(wFplmnGet, httptest.NewRequest(http.MethodGet, "/api/cellular/fplmn", nil))
	if wFplmnGet.Code != http.StatusOK {
		t.Fatalf("GetFPLMN returned %d, want 200", wFplmnGet.Code)
	}

	// 1b. POST Clear FPLMN
	mock.SetResponse("AT+CRSM=214,28539,0,0,12,\"FFFFFFFFFFFFFFFFFFFFFFFF\"", "+CRSM: 144,0\r\nOK")
	wClear := httptest.NewRecorder()
	fplmnH.ClearFPLMN(wClear, httptest.NewRequest(http.MethodPost, "/api/cellular/fplmn/clear", nil))
	if wClear.Code != http.StatusOK {
		t.Fatalf("ClearFPLMN clear returned %d", wClear.Code)
	}

	// 2. Neighbour Scanner Handler
	neighH := NewNeighbourScannerHandler(eng)

	// 2a. Start scan and check status
	mock.SetResponse("AT+QENG=\"neighbourcell\"", `+QENG: "neighbourcell intra","LTE",1825,120,-8,-95,-65,-
+QENG: "neighbourcell inter","LTE",1850,125,-10,-98,-68,-
+QENG: "neighbourcell","NR5G-NSA",631000,200,-9,-90,15
OK`)
	wScanStart := httptest.NewRecorder()
	neighH.StartScan(wScanStart, httptest.NewRequest(http.MethodPost, "/api/cellular/neighbour/start", nil))
	if wScanStart.Code != http.StatusOK {
		t.Fatalf("StartScan returned %d", wScanStart.Code)
	}

	wScanStatus := httptest.NewRecorder()
	neighH.ScanStatus(wScanStatus, httptest.NewRequest(http.MethodGet, "/api/cellular/neighbour/status", nil))
	if wScanStatus.Code != http.StatusOK {
		t.Fatalf("ScanStatus returned %d", wScanStatus.Code)
	}

	// 3. Network MTU Handler
	origMtu := mtuFirewallFile
	mtuFirewallFile = filepath.Join(tmpDir, "firewall.user.mtu")
	t.Cleanup(func() {
		mtuFirewallFile = origMtu
	})
	mtuH := NewNetworkMTUHandler()

	// 3a. GET MTU
	wMtuGet := httptest.NewRecorder()
	mtuH.GetMTU(wMtuGet, httptest.NewRequest(http.MethodGet, "/api/network/mtu", nil))
	if wMtuGet.Code != http.StatusOK {
		t.Fatalf("GetMTU returned %d", wMtuGet.Code)
	}

	// 3b. POST MTU enable & disable
	bodyMtuEnable, _ := json.Marshal(MTUSavePayload{
		MTU: 1420,
	})
	wMtuSet := httptest.NewRecorder()
	mtuH.SetMTU(wMtuSet, httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBuffer(bodyMtuEnable)))
	if wMtuSet.Code != http.StatusOK && wMtuSet.Code != http.StatusInternalServerError {
		t.Errorf("SetMTU unexpected code %d", wMtuSet.Code)
	}

	bodyMtuDisable, _ := json.Marshal(MTUSavePayload{
		MTU: "disable",
	})
	wMtuDisable := httptest.NewRecorder()
	mtuH.SetMTU(wMtuDisable, httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBuffer(bodyMtuDisable)))
	if wMtuDisable.Code != http.StatusOK && wMtuDisable.Code != http.StatusInternalServerError {
		t.Errorf("SetMTU disable unexpected code %d", wMtuDisable.Code)
	}

	// 3c. Invalid MTU values
	bodyMtuBad, _ := json.Marshal(MTUSavePayload{
		MTU: 400, // Below 576
	})
	wMtuBad := httptest.NewRecorder()
	mtuH.SetMTU(wMtuBad, httptest.NewRequest(http.MethodPost, "/api/network/mtu", bytes.NewBuffer(bodyMtuBad)))
	if wMtuBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for MTU < 576, got %d", wMtuBad.Code)
	}
}
