package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeMockNetDev(t *testing.T, path string, ifaces map[string][2]uint64) {
	t.Helper()
	header := "Inter-|   Receive                                                |  Transmit\n" +
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n"
	content := header
	for iface, bytes := range ifaces {
		line := fmt.Sprintf("%s: %d 10 0 0 0 0 0 0 %d 10 0 0 0 0 0 0\n", iface, bytes[0], bytes[1])
		content += line
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed writing mock /proc/net/dev: %v", err)
	}
}

func TestGetDataUsed_RmnetIpa0Priority_And_DeltaAccumulation(t *testing.T) {
	tmpDir := t.TempDir()
	netDevFile := filepath.Join(tmpDir, "net_dev")

	// 1. Initial counter state
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0":  {1000, 2000},
		"rmnet_data0": {300, 400},
		"eth0":        {50000, 60000},
	})

	h := NewDataUsageHandler(netDevFile)

	// First read initializes baseline
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/data-usage", nil)
	w1 := httptest.NewRecorder()
	h.GetDataUsed(w1, req)

	if w1.Code != http.StatusOK {
		t.Fatalf("GetDataUsed returned %d, want 200", w1.Code)
	}

	var resp1 map[string]interface{}
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}

	if resp1["selected_counter"] != "rmnet_ipa0" {
		t.Errorf("expected selected_counter=rmnet_ipa0, got %v", resp1["selected_counter"])
	}
	if resp1["accumulated_rx_bytes"].(float64) != 0 || resp1["accumulated_tx_bytes"].(float64) != 0 {
		t.Errorf("expected initial accumulated rx=0, tx=0, got rx=%v, tx=%v", resp1["accumulated_rx_bytes"], resp1["accumulated_tx_bytes"])
	}

	// 2. Traffic passes: rx +500, tx +800
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0":  {1500, 2800},
		"rmnet_data0": {300, 400},
		"eth0":        {90000, 90000},
	})

	w2 := httptest.NewRecorder()
	h.GetDataUsed(w2, req)
	var resp2 map[string]interface{}
	_ = json.NewDecoder(w2.Body).Decode(&resp2)

	if resp2["accumulated_rx_bytes"].(float64) != 500 || resp2["accumulated_tx_bytes"].(float64) != 800 {
		t.Errorf("expected accumulated delta rx=500, tx=800, got rx=%v, tx=%v", resp2["accumulated_rx_bytes"], resp2["accumulated_tx_bytes"])
	}
}

func TestGetDataUsed_FallbackRmnetData0(t *testing.T) {
	tmpDir := t.TempDir()
	netDevFile := filepath.Join(tmpDir, "net_dev")

	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_data0": {500, 600},
		"eth0":        {90000, 90000},
	})

	h := NewDataUsageHandler(netDevFile)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/data-usage", nil)
	w := httptest.NewRecorder()
	h.GetDataUsed(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetDataUsed returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["selected_counter"] != "rmnet_data0" {
		t.Errorf("expected selected_counter=rmnet_data0, got %v", resp["selected_counter"])
	}
}

func TestGetDataUsed_FallbackWwan0(t *testing.T) {
	tmpDir := t.TempDir()
	netDevFile := filepath.Join(tmpDir, "net_dev")

	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"wwan0": {700, 800},
		"eth0":  {90000, 90000},
	})

	h := NewDataUsageHandler(netDevFile)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/data-usage", nil)
	w := httptest.NewRecorder()
	h.GetDataUsed(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetDataUsed returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["selected_counter"] != "wwan0" {
		t.Errorf("expected selected_counter=wwan0, got %v", resp["selected_counter"])
	}
}

func TestGetDataUsed_Eth0NotCountedAsCellularWAN(t *testing.T) {
	tmpDir := t.TempDir()
	netDevFile := filepath.Join(tmpDir, "net_dev")

	// Only eth0 present
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"eth0": {500000, 600000},
	})

	h := NewDataUsageHandler(netDevFile)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/data-usage", nil)
	w := httptest.NewRecorder()
	h.GetDataUsed(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetDataUsed returned %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["selected_counter"] != "none" {
		t.Errorf("expected selected_counter=none when only eth0 present, got %v", resp["selected_counter"])
	}
	if resp["accumulated_rx_bytes"].(float64) != 0 || resp["accumulated_tx_bytes"].(float64) != 0 {
		t.Errorf("expected rx=0, tx=0, got rx=%v, tx=%v", resp["accumulated_rx_bytes"], resp["accumulated_tx_bytes"])
	}
}

func TestDataUsed_RolloverAndResetCount(t *testing.T) {
	tmpDir := t.TempDir()
	netDevFile := filepath.Join(tmpDir, "net_dev")

	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0": {1000, 2000},
	})

	h := NewDataUsageHandler(netDevFile)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/data-usage", nil)

	// Step 1: Initial read
	w1 := httptest.NewRecorder()
	h.GetDataUsed(w1, req)

	// Step 2: Accumulate 500 bytes
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0": {1500, 2500},
	})
	w2 := httptest.NewRecorder()
	h.GetDataUsed(w2, req)
	var resp2 map[string]interface{}
	_ = json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2["accumulated_rx_bytes"].(float64) != 500 || resp2["modem_reset_count"].(float64) != 0 {
		t.Fatalf("expected rx=500, reset_count=0, got %+v", resp2)
	}

	// Step 3: Interface resets / counter rollover (counter drops from 1500 to 200)
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0": {200, 300},
	})
	w3 := httptest.NewRecorder()
	h.GetDataUsed(w3, req)
	var resp3 map[string]interface{}
	_ = json.NewDecoder(w3.Body).Decode(&resp3)
	// Accumulated should be 500 + 200 = 700, and resetCount should be 1
	if resp3["accumulated_rx_bytes"].(float64) != 700 || resp3["modem_reset_count"].(float64) != 1 {
		t.Errorf("expected rx=700, reset_count=1 after rollover, got %+v", resp3)
	}
}

func TestResetDataUsed(t *testing.T) {
	tmpDir := t.TempDir()
	netDevFile := filepath.Join(tmpDir, "net_dev")

	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0": {1000, 2000},
	})

	h := NewDataUsageHandler(netDevFile)

	// 1. Initial read baseline
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/network/data-usage", nil)
	wGet1 := httptest.NewRecorder()
	h.GetDataUsed(wGet1, reqGet)

	// 2. Traffic accumulated
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0": {1300, 2400},
	})
	wGet2 := httptest.NewRecorder()
	h.GetDataUsed(wGet2, reqGet)
	var resp2 map[string]interface{}
	_ = json.NewDecoder(wGet2.Body).Decode(&resp2)
	if resp2["accumulated_rx_bytes"].(float64) != 300 {
		t.Errorf("expected rx=300 before reset, got %v", resp2["accumulated_rx_bytes"])
	}

	// 3. Reset
	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/network/data-usage/reset", nil)
	wReset := httptest.NewRecorder()
	h.ResetDataUsed(wReset, reqReset)
	if wReset.Code != http.StatusOK {
		t.Fatalf("ResetDataUsed returned %d, want 200", wReset.Code)
	}

	// 4. Immediately read after reset
	wGet3 := httptest.NewRecorder()
	h.GetDataUsed(wGet3, reqGet)
	var resp3 map[string]interface{}
	_ = json.NewDecoder(wGet3.Body).Decode(&resp3)
	if resp3["accumulated_rx_bytes"].(float64) != 0 || resp3["accumulated_tx_bytes"].(float64) != 0 {
		t.Errorf("expected rx=0, tx=0 immediately after reset, got rx=%v, tx=%v", resp3["accumulated_rx_bytes"], resp3["accumulated_tx_bytes"])
	}

	// 5. Update file with new traffic after reset
	writeMockNetDev(t, netDevFile, map[string][2]uint64{
		"rmnet_ipa0": {1500, 2800},
	})

	wGet4 := httptest.NewRecorder()
	h.GetDataUsed(wGet4, reqGet)
	var resp4 map[string]interface{}
	_ = json.NewDecoder(wGet4.Body).Decode(&resp4)
	if resp4["accumulated_rx_bytes"].(float64) != 200 || resp4["accumulated_tx_bytes"].(float64) != 400 {
		t.Errorf("expected delta rx=200, tx=400 after reset, got rx=%v, tx=%v", resp4["accumulated_rx_bytes"], resp4["accumulated_tx_bytes"])
	}
}
