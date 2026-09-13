package sshd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"qmanager/internal/sshd/auth"
)

func TestSSHServerExec(t *testing.T) {
	tmpDir := t.TempDir()
	shadowFile := filepath.Join(tmpDir, "shadow")

	// Create shadow with root user and password "TestSecret123"
	salt := "test1234"
	hash := auth.CryptMD5("TestSecret123", salt)
	content := fmt.Sprintf("root:%s:20708:0:99999:7:::\n", hash)
	if err := os.WriteFile(shadowFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write shadow: %v", err)
	}

	cfg := Config{
		ListenAddr: "127.0.0.1:0", // random ephemeral port
		KeyDir:     filepath.Join(tmpDir, "ssh_keys"),
		ShadowPath: shadowFile,
		Shell:      "/bin/sh",
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer srv.Close()

	addr := srv.listener.Addr().String()

	// Test SSH Client Connection
	clientConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password("TestSecret123"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		t.Fatalf("ssh.Dial failed: %v", err)
	}
	defer client.Close()

	// Run exec command
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout
	if err := session.Run("echo 'HELLO_QMANAGER_SSHD'"); err != nil {
		t.Fatalf("session.Run failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "HELLO_QMANAGER_SSHD") {
		t.Errorf("expected HELLO_QMANAGER_SSHD in stdout, got %q", stdout.String())
	}
}

type netTCPAddr interface {
	Network() string
	String() string
}
