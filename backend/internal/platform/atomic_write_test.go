package platform

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicWriteFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_atomic.txt")

	data1 := []byte("first content line\nsecond content line\n")
	if err := AtomicWriteFile(filePath, data1, 0644); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}

	read1, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if !bytes.Equal(read1, data1) {
		t.Errorf("read content mismatch: got %q, want %q", string(read1), string(data1))
	}

	// Overwrite atomically
	data2 := []byte("overwritten new content\n")
	if err := AtomicWriteFile(filePath, data2, 0644); err != nil {
		t.Fatalf("AtomicWriteFile overwrite failed: %v", err)
	}

	read2, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read overwritten file: %v", err)
	}
	if !bytes.Equal(read2, data2) {
		t.Errorf("overwritten content mismatch: got %q, want %q", string(read2), string(data2))
	}
}

func TestAtomicWriteFile_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "secret_key")

	data := []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...\n")
	if err := AtomicWriteFile(filePath, data, 0600); err != nil {
		t.Fatalf("AtomicWriteFile with 0600 failed: %v", err)
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	// Check file mode permissions (ignoring non-permission bits)
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %#o", fi.Mode().Perm())
	}
}

func TestAtomicWriteFile_ParentDirCreation(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "sub", "dir", "config.json")

	data := []byte(`{"status":"ok"}`)
	if err := AtomicWriteFile(nestedPath, data, 0644); err != nil {
		t.Fatalf("expected parent dirs to be created, got: %v", err)
	}

	read, err := os.ReadFile(nestedPath)
	if err != nil {
		t.Fatalf("failed to read from nested path: %v", err)
	}
	if !bytes.Equal(read, data) {
		t.Errorf("content mismatch in nested dir: got %q, want %q", string(read), string(data))
	}
}

func TestAtomicWriteFile_ParentIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	fileAsParent := filepath.Join(tmpDir, "regular_file.txt")
	if err := os.WriteFile(fileAsParent, []byte("file data"), 0644); err != nil {
		t.Fatalf("failed to write dummy parent file: %v", err)
	}

	badTarget := filepath.Join(fileAsParent, "cannot_be_child.json")
	if err := AtomicWriteFile(badTarget, []byte("payload"), 0644); err == nil {
		t.Errorf("expected error when parent is a regular file, got nil")
	}
}

func TestAtomicWriteFile_TargetIsDir(t *testing.T) {
	tmpDir := t.TempDir()
	dirTarget := filepath.Join(tmpDir, "some_directory")
	if err := os.MkdirAll(dirTarget, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	if err := AtomicWriteFile(dirTarget, []byte("payload"), 0644); err == nil {
		t.Errorf("expected error when target is a directory, got nil")
	}
}

func TestAtomicWriteFile_TargetIsSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	realFile := filepath.Join(tmpDir, "real_target.txt")
	if err := os.WriteFile(realFile, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to create real file: %v", err)
	}

	symlinkPath := filepath.Join(tmpDir, "symlink_file.txt")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	if err := AtomicWriteFile(symlinkPath, []byte("new data"), 0644); err == nil {
		t.Errorf("expected error when writing to a symlink, got nil")
	}

	// Ensure real target was untouched
	content, _ := os.ReadFile(realFile)
	if string(content) != "original" {
		t.Errorf("expected real file to remain untouched, got: %q", string(content))
	}
}

func TestAtomicWriteFile_EmptyPath(t *testing.T) {
	if err := AtomicWriteFile("", []byte("data"), 0644); err == nil {
		t.Errorf("expected error for empty file path, got nil")
	}
}

func TestAtomicWriteFile_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	var wg sync.WaitGroup
	const numGoroutines = 20

	// Concurrent writes to distinct files
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := filepath.Join(tmpDir, fmt.Sprintf("file_%d.txt", idx))
			data := []byte(fmt.Sprintf("content_%d", idx))
			if err := AtomicWriteFile(path, data, 0644); err != nil {
				t.Errorf("concurrent write to %s failed: %v", path, err)
			}
			read, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(read, data) {
				t.Errorf("concurrent read mismatch for %s: %v", path, err)
			}
		}(i)
	}

	// Concurrent writes to same file
	sharedPath := filepath.Join(tmpDir, "shared_atomic.txt")
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := []byte(fmt.Sprintf("iteration_%d\n", idx))
			_ = AtomicWriteFile(sharedPath, data, 0644)
		}(i)
	}

	wg.Wait()

	// Ensure shared file exists and is valid readable content
	data, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("failed to read shared file: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("shared file is empty")
	}
}
