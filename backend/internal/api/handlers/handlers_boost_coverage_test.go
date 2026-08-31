package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/config"
)

// Tests utility parsers and helper branches in handlers to push package coverage > 85%
func TestHandlers_UtilityParsersAndTarExtractor(t *testing.T) {
	// 1. parseCID test
	if parseCID(nil, nil) != 0 {
		t.Errorf("parseCID(nil, nil) expected 0")
	}
	if parseCID(float64(3), nil) != 3 {
		t.Errorf("parseCID(3.0, nil) expected 3")
	}
	if parseCID("3", nil) != 3 {
		t.Errorf("parseCID('3', nil) expected 3")
	}
	if parseCID(nil, float64(4)) != 4 {
		t.Errorf("parseCID(nil, 4.0) expected 4")
	}
	if parseCID(nil, "5") != 5 {
		t.Errorf("parseCID(nil, '5') expected 5")
	}

	// 2. getCgactState
	lines := []string{
		"+CGACT: 1,1",
		"+CGACT: 2,0",
		"+CGACT: 3,1",
	}
	if getCgactState(lines, 1) != 1 || getCgactState(lines, 2) != 0 || getCgactState(lines, 4) != 0 {
		t.Errorf("getCgactState mismatch")
	}

	// 3. parseNegotiatedApnForCid
	contrdpLines := `+CGCONTRDP: 1,5,"internet.carrier.com","10.0.0.1"`
	if parseNegotiatedApnForCid(contrdpLines, 1) != "internet.carrier.com" {
		t.Errorf("parseNegotiatedApnForCid mismatch")
	}
	if parseNegotiatedApnForCid(contrdpLines, 2) != "" {
		t.Errorf("parseNegotiatedApnForCid for non-matching cid should be empty")
	}

	// 4. parseBoolFlexible
	if !parseBoolFlexible(true) || !parseBoolFlexible(1) || !parseBoolFlexible("1") || !parseBoolFlexible("true") || !parseBoolFlexible("TRUE") {
		t.Errorf("parseBoolFlexible true cases failed")
	}
	if parseBoolFlexible(false) || parseBoolFlexible(0) || parseBoolFlexible("0") || parseBoolFlexible("false") || parseBoolFlexible("other") {
		t.Errorf("parseBoolFlexible false cases failed")
	}

	// 5. extractBinaryFromTar with valid in-memory gzip tar
	tmpDir := t.TempDir()
	tarGzPath := filepath.Join(tmpDir, "test-qmanager.tar.gz")

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	binaryContent := []byte("#!/bin/sh\necho test")
	hdr := &tar.Header{
		Name: "qmanager",
		Mode: 0755,
		Size: int64(len(binaryContent)),
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write(binaryContent)
	_ = tw.Close()
	_ = gzw.Close()

	_ = os.WriteFile(tarGzPath, buf.Bytes(), 0644)

	destDir := filepath.Join(tmpDir, "extracted")
	extracted, err := extractBinaryFromTar(tarGzPath, destDir)
	if err != nil || extracted == "" {
		t.Fatalf("extractBinaryFromTar failed on valid archive: %v", err)
	}

	if fi, err := os.Stat(extracted); err != nil || fi.Size() != int64(len(binaryContent)) {
		t.Errorf("extracted binary mismatch: size=%d", fi.Size())
	}
}

func TestUpdateHandler_FullHTTPMock(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)

	// Mock server for GitHub release API & binary download
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/test/releases/latest" || r.URL.Path == "/repos/test/releases" {
			rel := GitHubRelease{
				TagName:     "v0.2.0",
				Name:        "Release 0.2.0",
				Body:        "New features and bugfixes",
				PublishedAt: time.Now().Format(time.RFC3339),
				Prerelease:  false,
				Assets: []GitHubAsset{
					{
						Name:               "qmanager-armv7.tar.gz",
						Size:               1024,
						BrowserDownloadURL: "http://" + r.Host + "/download/qmanager.tar.gz",
					},
				},
			}
			if r.URL.Path == "/repos/test/releases/latest" {
				_ = json.NewEncoder(w).Encode(rel)
			} else {
				_ = json.NewEncoder(w).Encode([]GitHubRelease{rel})
			}
			return
		}

		if r.URL.Path == "/download/qmanager.tar.gz" {
			// Write valid gzipped tar with dummy binary
			gzw := gzip.NewWriter(w)
			tw := tar.NewWriter(gzw)
			content := []byte("#!/bin/sh\necho updated\n")
			hdr := &tar.Header{
				Name: "qmanager",
				Mode: 0755,
				Size: int64(len(content)),
			}
			_ = tw.WriteHeader(hdr)
			_, _ = tw.Write(content)
			_ = tw.Close()
			_ = gzw.Close()
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	h := NewUpdateHandler(cfgMgr)
	h.updateDir = filepath.Join(tmpDir, "update")
	h.binaryPath = filepath.Join(tmpDir, "qmanager_bin")
	_ = os.MkdirAll(h.updateDir, 0755)

	// Override httpClient to talk to local test server
	h.httpClient = ts.Client()

	// 1. asyncDownload test with context
	ctx := context.Background()
	h.asyncDownload(ctx, "v0.2.0", ts.URL+"/download/qmanager.tar.gz")

	// Wait briefly for download goroutine
	time.Sleep(200 * time.Millisecond)

	h.downloadMu.Lock()
	state := *h.currentDown
	h.downloadMu.Unlock()

	if state.Status != "ready" && state.Status != "done" {
		t.Errorf("expected download state 'ready' or 'done', got %s", state.Status)
	}

	// 2. checkStagedDownload test
	stagedVer, stagedTar := h.checkStagedDownload()
	if stagedVer != "v0.2.0" || stagedTar == "" {
		t.Errorf("checkStagedDownload mismatch: ver=%s, path=%s", stagedVer, stagedTar)
	}

	// 3. handleApply with staged valid download
	wApply := httptest.NewRecorder()
	h.handleApply(wApply, "v0.2.0", "")
	if wApply.Code != http.StatusOK {
		t.Fatalf("handleApply returned %d, want 200: %s", wApply.Code, wApply.Body.String())
	}
}
