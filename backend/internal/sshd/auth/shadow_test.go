package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCryptMD5(t *testing.T) {
	pw := "Error65@"
	salt := "bBqizhjU"
	expected := "$1$bBqizhjU$2H3lbSKbZz5/UNtdaY4ow."

	got := CryptMD5(pw, salt)
	if got != expected {
		t.Fatalf("CryptMD5 mismatch: got %q, expected %q", got, expected)
	}

	if !CheckPasswordHash(pw, expected) {
		t.Errorf("CheckPasswordHash failed for valid password")
	}

	if CheckPasswordHash("WrongPass", expected) {
		t.Errorf("CheckPasswordHash should return false for invalid password")
	}
}

func TestVerifyShadowPassword(t *testing.T) {
	tmpDir := t.TempDir()
	shadowFile := filepath.Join(tmpDir, "shadow")

	content := "root:$1$bBqizhjU$2H3lbSKbZz5/UNtdaY4ow.:20708:0:99999:7:::\ndaemon:*:20418:0:99999:7:::\n"
	if err := os.WriteFile(shadowFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write mock shadow: %v", err)
	}

	// 1. Success root
	ok, err := VerifyShadowPassword("root", "Error65@", shadowFile)
	if err != nil || !ok {
		t.Errorf("expected root auth success, got ok=%v, err=%v", ok, err)
	}

	// 2. Fail root wrong pass
	ok, err = VerifyShadowPassword("root", "WrongPass", shadowFile)
	if err != nil || ok {
		t.Errorf("expected root auth fail, got ok=%v, err=%v", ok, err)
	}

	// 3. Locked daemon user
	ok, err = VerifyShadowPassword("daemon", "any", shadowFile)
	if err != nil || ok {
		t.Errorf("expected locked daemon auth fail, got ok=%v, err=%v", ok, err)
	}

	// 4. Unknown user
	ok, err = VerifyShadowPassword("nonexistent", "pass", shadowFile)
	if err == nil {
		t.Errorf("expected error for nonexistent user, got nil")
	}
}
