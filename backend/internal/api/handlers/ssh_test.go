package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qmanager/internal/config"
	"qmanager/internal/sshd"
)

func TestSSHHandler(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")
	keyDir := filepath.Join(tmpDir, "ssh_keys")

	cfgMgr, _ := config.NewManager(confPath)
	sshMgr := sshd.NewManager(cfgMgr, keyDir)
	h := NewSSHHandler(sshMgr)

	// 1. Get Status
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/ssh", nil)
	w := httptest.NewRecorder()
	h.GetStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetStatus returned %d", w.Code)
	}

	// 2. Save Settings
	payload := map[string]interface{}{
		"enabled":         true,
		"port":            2222,
		"authorized_keys": "ssh-ed25519 AAAAB3NzaC1yc2EAAAADAQABAAABAQC3 test@key",
	}
	body, _ := json.Marshal(payload)
	reqSave := httptest.NewRequest(http.MethodPost, "/api/v1/system/ssh", bytes.NewReader(body))
	wSave := httptest.NewRecorder()
	h.SaveSettings(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("SaveSettings returned %d: %s", wSave.Code, wSave.Body.String())
	}

	// 3. CGI Fallback
	reqCGI := httptest.NewRequest(http.MethodGet, "/cgi-bin/quecmanager/system/ssh.sh", nil)
	wCGI := httptest.NewRecorder()
	h.HandleCGI(wCGI, reqCGI)
	if wCGI.Code != http.StatusOK {
		t.Fatalf("HandleCGI GET returned %d", wCGI.Code)
	}

	// 4. CGI POST Fallback
	reqCGIPost := httptest.NewRequest(http.MethodPost, "/cgi-bin/quecmanager/system/ssh.sh", bytes.NewReader(body))
	wCGIPost := httptest.NewRecorder()
	h.HandleCGI(wCGIPost, reqCGIPost)
	if wCGIPost.Code != http.StatusOK {
		t.Fatalf("HandleCGI POST returned %d", wCGIPost.Code)
	}
}
