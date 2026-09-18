package platform

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AtomicWriteFile writes data to filePath atomically using a temporary file in the same directory,
// syncing buffers to disk (UBIFS NAND flash protection), and renaming it over the target file.
// Parent directories are created if they do not exist.
func AtomicWriteFile(filePath string, data []byte, perm os.FileMode) error {
	if filePath == "" {
		return fmt.Errorf("empty file path")
	}

	dir := filepath.Dir(filePath)

	// Refuse if parent directory doesn't exist or is not a directory
	dirInfo, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
				return fmt.Errorf("failed to create dir %s: %w", dir, mkErr)
			}
		} else {
			return fmt.Errorf("failed to stat dir %s: %w", dir, err)
		}
	} else if !dirInfo.IsDir() {
		return fmt.Errorf("parent path %s is not a directory", dir)
	}

	// Refuse to write through symlinks or existing directory destinations
	if fi, err := os.Lstat(filePath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write: %s is a symlink", filePath)
		}
		if fi.IsDir() {
			return fmt.Errorf("refusing to write: %s is a directory", filePath)
		}
	}

	// Generate random suffix to guarantee uniqueness even under rapid concurrent calls
	randBuf := make([]byte, 4)
	_, _ = rand.Read(randBuf)
	randHex := hex.EncodeToString(randBuf)

	// Unique temporary file in same directory
	base := filepath.Base(filePath)
	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%d_%s", base, time.Now().UnixNano(), randHex))

	// Symlink check on tmp path
	if fi, err := os.Lstat(tmpPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("temp path is a symlink: %s", tmpPath)
		}
		_ = os.Remove(tmpPath)
	}

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to open tmp file %s: %w", tmpPath, err)
	}
	_ = f.Chmod(perm)

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write tmp file: %w", err)
	}

	// Flush to disk blocks
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync tmp file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close tmp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace file %s: %w", filePath, err)
	}

	return nil
}
