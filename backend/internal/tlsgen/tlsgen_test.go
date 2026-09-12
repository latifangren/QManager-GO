package tlsgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCertificates(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "qmanager_test_tls")
	defer os.RemoveAll(tempDir)

	certPath, keyPath, err := EnsureCertificates(tempDir)
	if err != nil {
		t.Fatalf("EnsureCertificates failed: %v", err)
	}

	if !fileExistsAndNotEmpty(certPath) {
		t.Errorf("certPath file missing or empty: %s", certPath)
	}

	if !fileExistsAndNotEmpty(keyPath) {
		t.Errorf("keyPath file missing or empty: %s", keyPath)
	}

	// Test cached re-use path
	cPath2, kPath2, err2 := EnsureCertificates(tempDir)
	if err2 != nil || cPath2 != certPath || kPath2 != keyPath {
		t.Errorf("expected cached cert paths, got %s, %s (err: %v)", cPath2, kPath2, err2)
	}
}
