package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/telemetry"
)

func TestPingProfileHandler(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "ping_profile.json")

	prober := telemetry.NewPingProber("1.1.1.1:53", 1*time.Second)
	h := NewPingProfileHandler(prober, filePath)

	// 1. GET default profile
	wGet := httptest.NewRecorder()
	h.Handle(wGet, httptest.NewRequest(http.MethodGet, "/cgi-bin/quecmanager/settings/ping_profile.sh", nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for Get, got %d", wGet.Code)
	}

	var resp PingProfileSettings
	if err := json.NewDecoder(wGet.Body).Decode(&resp); err != nil {
		t.Fatalf("failed decoding GET response: %v", err)
	}
	if resp.Profile != "regular" || resp.TargetIPv4 != "1.1.1.1" || resp.IntervalSec != 2 {
		t.Fatalf("unexpected default ping profile: %+v", resp)
	}

	// 2. POST update target
	bodyUpdate := `{"profile":"aggressive","target_ipv4":"8.8.8.8","target_ipv6":"2001:4860:4860::8888","interval_sec":1,"timeout_sec":1,"fail_count":2}`
	wPost := httptest.NewRecorder()
	h.Handle(wPost, httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/settings/ping_profile.sh", bytes.NewBufferString(bodyUpdate)))
	if wPost.Code != http.StatusOK {
		t.Fatalf("expected 200 for Save, got %d: %s", wPost.Code, wPost.Body.String())
	}

	var postResp PingProfileSettings
	_ = json.NewDecoder(wPost.Body).Decode(&postResp)
	if postResp.Profile != "aggressive" || postResp.TargetIPv4 != "8.8.8.8" {
		t.Fatalf("unexpected updated ping profile: %+v", postResp)
	}

	// 3. Persisted in new handler
	h2 := NewPingProfileHandler(prober, filePath)
	wGet2 := httptest.NewRecorder()
	h2.Get(wGet2, httptest.NewRequest(http.MethodGet, "/api/v1/settings/ping-profile", nil))
	if wGet2.Code != http.StatusOK {
		t.Fatalf("expected 200 for Get on h2, got %d", wGet2.Code)
	}
	var resp2 PingProfileSettings
	_ = json.NewDecoder(wGet2.Body).Decode(&resp2)
	if resp2.TargetIPv4 != "8.8.8.8" || resp2.Profile != "aggressive" {
		t.Fatalf("expected persisted 8.8.8.8, got %+v", resp2)
	}

	// 4. Bad JSON
	wBadJSON := httptest.NewRecorder()
	h2.Save(wBadJSON, httptest.NewRequest(http.MethodPost, "/api/v1/settings/ping-profile", bytes.NewBufferString("invalid")))
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}
}
