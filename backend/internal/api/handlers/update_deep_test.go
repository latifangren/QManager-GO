package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qmanager/internal/config"
)

func TestUpdateHandler_DeepBranches(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)

	h := NewUpdateHandler(cfgMgr)
	h.updateDir = filepath.Join(tmpDir, "update")
	_ = os.MkdirAll(h.updateDir, 0755)

	// 1. Download - bad download URL / version
	bodyDownload, _ := json.Marshal(map[string]string{
		"action":  "download",
		"version": "v1.0.0",
		"url":     "http://127.0.0.1:1/nonexistent.tar.gz",
	})
	reqDownload := httptest.NewRequest(http.MethodPost, "/api/system/update", bytes.NewBuffer(bodyDownload))
	wDownload := httptest.NewRecorder()
	h.HandleUpdateAction(wDownload, reqDownload)
	if wDownload.Code != http.StatusOK {
		t.Fatalf("HandleUpdateAction download returned %d, want 200", wDownload.Code)
	}

	// 2. Apply - when not downloaded or file missing
	bodyApply, _ := json.Marshal(map[string]string{
		"action":  "apply",
		"version": "v1.0.0",
	})
	reqApply := httptest.NewRequest(http.MethodPost, "/api/system/update", bytes.NewBuffer(bodyApply))
	wApply := httptest.NewRecorder()
	h.HandleUpdateAction(wApply, reqApply)
	if wApply.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when applying without staged file, got %d", wApply.Code)
	}

	// 3. Staged archive verification branch in CheckUpdate
	stagedTar := filepath.Join(h.updateDir, "qmanager-v1.0.0.tar.gz")
	_ = os.WriteFile(stagedTar, []byte("dummy archive content"), 0644)
	wCheckStaged := httptest.NewRecorder()
	h.CheckUpdate(wCheckStaged, httptest.NewRequest(http.MethodGet, "/api/system/update", nil))
	if wCheckStaged.Code != http.StatusOK {
		t.Fatalf("CheckUpdate with staged file returned %d, want 200", wCheckStaged.Code)
	}

	// 4. Helper compareSemver edge cases
	if CompareSemver("1.0.0", "1.0.0") != 0 {
		t.Errorf("CompareSemver 1.0.0 == 1.0.0 failed")
	}
	if CompareSemver("2.0.0", "1.9.9") <= 0 {
		t.Errorf("CompareSemver 2.0.0 > 1.9.9 failed")
	}
	if CompareSemver("1.0.0-rc1", "1.0.0") >= 0 {
		t.Errorf("CompareSemver pre-release comparison failed")
	}

	// 5. extractBinaryFromTar error handling
	corruptTar := filepath.Join(tmpDir, "corrupt.tar.gz")
	_ = os.WriteFile(corruptTar, []byte("not gzip"), 0644)
	_, err := extractBinaryFromTar(corruptTar, filepath.Join(tmpDir, "extracted"))
	if err == nil {
		t.Errorf("expected error extracting corrupt gzip archive")
	}
}
