//go:build windows

package atengine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	var out bytes.Buffer
	buf := make([]byte, 1024)

	for {
		select {
		case <-ctx.Done():
			return out.String(), ErrTimeout
		default:
		}

		n, err := f.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if isTerminator(out.Bytes()) {
				resp := out.String()
				if strings.Contains(resp, "\r\nERROR\r\n") || strings.Contains(resp, "\nERROR\n") || strings.Contains(resp, "+CME ERROR:") || strings.Contains(resp, "+CMS ERROR:") {
					return resp, ErrATCommand
				}
				if strings.Contains(resp, "\r\nBUSY\r\n") {
					return resp, ErrBusy
				}
				return resp, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				resp := out.String()
				if strings.Contains(resp, "\r\nERROR\r\n") || strings.Contains(resp, "\nERROR\n") || strings.Contains(resp, "+CME ERROR:") || strings.Contains(resp, "+CMS ERROR:") {
					return resp, ErrATCommand
				}
				if strings.Contains(resp, "\r\nBUSY\r\n") {
					return resp, ErrBusy
				}
				return resp, nil
			}
			return out.String(), err
		}
	}
}
