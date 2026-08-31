//go:build !windows

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
	"time"
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
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func releaseFileLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

func isEBUSY(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EBUSY)
}

func isEAGAIN(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

func readDeviceResponse(ctx context.Context, f *os.File) (string, error) {
	_ = syscall.SetNonblock(int(f.Fd()), true)
	defer func() {
		_ = syscall.SetNonblock(int(f.Fd()), false)
	}()

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
		} else if err != nil {
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
			if !isEAGAIN(err) {
				return out.String(), err
			}
		}

		select {
		case <-ctx.Done():
			return out.String(), ErrTimeout
		case <-time.After(10 * time.Millisecond):
		}
	}
}
