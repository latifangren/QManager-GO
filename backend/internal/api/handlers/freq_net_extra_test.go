package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/telemetry"
)

// NetworkHandler & FrequencyLockHandler Tests
func TestNetworkAndFrequencyLockHandlers(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() { _ = eng.Close() })

	// 1. NetworkHandler Tests
	prober := telemetry.NewPingProber("127.0.0.1:9", 50*time.Millisecond)
	netH := NewNetworkHandler(prober)

	// PingStats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/network/ping-stats", nil)
	wStats := httptest.NewRecorder()
	netH.PingStats(wStats, reqStats)
	if wStats.Code != http.StatusOK {
		t.Fatalf("PingStats returned %d, want 200", wStats.Code)
	}

	// SetTTL - invalid TTL
	bodyBadTTL, _ := json.Marshal(SetTTLRequest{TTL: 300})
	reqBadTTL := httptest.NewRequest(http.MethodPost, "/api/network/ttl", bytes.NewBuffer(bodyBadTTL))
	wBadTTL := httptest.NewRecorder()
	netH.SetTTL(wBadTTL, reqBadTTL)
	if wBadTTL.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for TTL > 255, got %d", wBadTTL.Code)
	}

	// SetDNS
	bodyDNS, _ := json.Marshal(SetDNSRequest{Primary: "1.1.1.1", Secondary: "8.8.8.8"})
	reqDNS := httptest.NewRequest(http.MethodPost, "/api/network/dns", bytes.NewBuffer(bodyDNS))
	wDNS := httptest.NewRecorder()
	netH.SetDNS(wDNS, reqDNS)
	if wDNS.Code != http.StatusOK {
		t.Fatalf("SetDNS returned %d, want 200", wDNS.Code)
	}

	// 2. FrequencyLockHandler Tests
	freqH := NewFrequencyLockHandler(eng)

	// Status
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock";+QNWCFG="nr5g_earfcn_lock"`, `+QNWCFG: "lte_earfcn_lock",1,1675`+"\r\n"+`+QNWCFG: "nr5g_earfcn_lock",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWLOCK="common/5g"`, `+QNWLOCK: "common/5g",0`+"\r\nOK")

	reqFreqStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/frequency", nil)
	wFreqStatus := httptest.NewRecorder()
	freqH.Status(wFreqStatus, reqFreqStatus)
	if wFreqStatus.Code != http.StatusOK {
		t.Fatalf("Frequency Status returned %d, want 200", wFreqStatus.Code)
	}

	// Lock LTE - Unlock action
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock",0`, "OK")
	bodyUnlock, _ := json.Marshal(FrequencyLockRequest{
		Action: "unlock",
		RAT:    "lte",
	})
	reqUnlock := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyUnlock))
	wUnlock := httptest.NewRecorder()
	freqH.Lock(wUnlock, reqUnlock)
	if wUnlock.Code != http.StatusOK {
		t.Fatalf("Lock LTE unlock returned %d, want 200", wUnlock.Code)
	}

	// Lock NR5G - Unlock action
	mock.SetResponse(`AT+QNWCFG="nr5g_earfcn_lock",0`, "OK")
	bodyUnlockNR, _ := json.Marshal(FrequencyLockRequest{
		Action: "unlock",
		RAT:    "nr5g",
	})
	reqUnlockNR := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyUnlockNR))
	wUnlockNR := httptest.NewRecorder()
	freqH.Lock(wUnlockNR, reqUnlockNR)
	if wUnlockNR.Code != http.StatusOK {
		t.Fatalf("Lock NR unlock returned %d, want 200", wUnlockNR.Code)
	}

	// Lock LTE - Apply Lock action
	mock.SetResponse(`AT+QNWLOCK="common/4g"`, `+QNWLOCK: "common/4g",0`+"\r\nOK")
	mock.SetResponse(`AT+QNWCFG="lte_earfcn_lock",1,1675`, "OK")
	bodyLockLTE, _ := json.Marshal(FrequencyLockRequest{
		Action:     "lock",
		RAT:        "lte",
		LTEEntries: []LTEFreqEntry{{EARFCN: 1675}},
	})
	reqLockLTE := httptest.NewRequest(http.MethodPost, "/api/cellular/frequency/lock", bytes.NewBuffer(bodyLockLTE))
	wLockLTE := httptest.NewRecorder()
	freqH.Lock(wLockLTE, reqLockLTE)
	if wLockLTE.Code != http.StatusOK {
		t.Fatalf("Lock LTE apply returned %d, want 200", wLockLTE.Code)
	}
}
