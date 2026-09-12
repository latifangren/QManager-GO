package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/telemetry/bandwidth"
)

const sampleNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
rmnet_data0: 100000000 10000 0 0 0 0 0 0 50000000 5000 0 0 0 0 0 0
`
const sampleNetDevDelta = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
rmnet_data0: 101000000 10100 0 0 0 0 0 0 50500000 5050 0 0 0 0 0 0
`

func TestBandwidthHandler_GetAndReset(t *testing.T) {
	tmpDir := t.TempDir()
	devPath := filepath.Join(tmpDir, "net_dev")
	storePath := filepath.Join(tmpDir, "bandwidth.json")

	_ = os.WriteFile(devPath, []byte(sampleNetDev), 0644)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	col := bandwidth.NewCollector(storePath, bandwidth.WithDevPath(devPath), bandwidth.WithTimeFunc(func() time.Time { return now }))
	col.Collect()

	now = now.Add(1 * time.Second)
	_ = os.WriteFile(devPath, []byte(sampleNetDevDelta), 0644)
	col.Collect()

	h := NewBandwidthHandler(col)

	// 1. GET all interfaces
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/monitoring/bandwidth", nil)
	wGet := httptest.NewRecorder()
	h.GetBandwidth(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for GetBandwidth, got %d", wGet.Code)
	}

	var getResp struct {
		Success bool                        `json:"success"`
		Data    bandwidth.BandwidthSnapshot `json:"data"`
	}
	if err := json.NewDecoder(wGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !getResp.Success {
		t.Errorf("expected success=true")
	}
	if len(getResp.Data.Interfaces) == 0 {
		t.Errorf("expected non-empty interfaces in snapshot")
	}

	// 2. GET specific interface filter
	reqFilter := httptest.NewRequest(http.MethodGet, "/api/v1/monitoring/bandwidth?interface=rmnet_data0", nil)
	wFilter := httptest.NewRecorder()
	h.GetBandwidth(wFilter, reqFilter)

	var filterResp struct {
		Success bool                        `json:"success"`
		Data    bandwidth.BandwidthSnapshot `json:"data"`
	}
	_ = json.NewDecoder(wFilter.Body).Decode(&filterResp)
	if filterResp.Data.DefaultInterface != "rmnet_data0" {
		t.Errorf("expected DefaultInterface = 'rmnet_data0', got %s", filterResp.Data.DefaultInterface)
	}

	// 3. POST Reset specific interface
	bodyReset := bytes.NewBufferString(`{"interface":"rmnet_data0"}`)
	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/monitoring/bandwidth/reset", bodyReset)
	wReset := httptest.NewRecorder()
	h.ResetBandwidth(wReset, reqReset)

	if wReset.Code != http.StatusOK {
		t.Fatalf("expected 200 for ResetBandwidth, got %d", wReset.Code)
	}

	// 4. Test Nil Collector
	hNil := NewBandwidthHandler(nil)
	wNilGet := httptest.NewRecorder()
	hNil.GetBandwidth(wNilGet, reqGet)
	if wNilGet.Code != http.StatusOK {
		t.Errorf("expected 200 for nil collector GET, got %d", wNilGet.Code)
	}

	wNilReset := httptest.NewRecorder()
	hNil.ResetBandwidth(wNilReset, reqReset)
	if wNilReset.Code != http.StatusOK {
		t.Errorf("expected 200 for nil collector reset, got %d", wNilReset.Code)
	}
}
