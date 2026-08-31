//go:build windows

package atengine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func acquireFileLock(lockPath string) (*os.File, error) {
	if lockPath == "" {
		return nil, nil
	}
	_ = os.MkdirAll(filepath.Dir(lockPath), 0755)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func releaseFileLock(f *os.File) {
	if f == nil {
		return
	}
	_ = f.Close()
}

func isEBUSY(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EBUSY)
}

func readDeviceResponse(ctx context.Context, f *os.File) (string, error) {
	return readResponse(ctx, f)
}
