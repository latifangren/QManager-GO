//go:build !windows

package atengine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
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

func fdSetBit(set *syscall.FdSet, fd int) {
	bitsPerWord := int(unsafe.Sizeof(set.Bits[0])) * 8
	idx := fd / bitsPerWord
	set.Bits[idx] |= 1 << (uint(fd) % uint(bitsPerWord))
}

func readDeviceRawResponse(ctx context.Context, fd int) (string, error) {
	var out bytes.Buffer
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return out.String(), ErrTimeout
		default:
		}

		rdfs := &syscall.FdSet{}
		fdSetBit(rdfs, fd)
		tv := syscall.NsecToTimeval(100 * time.Millisecond.Nanoseconds())

		n, err := syscall.Select(fd+1, rdfs, nil, nil, &tv)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return out.String(), err
		}
		if n == 0 {
			// select timed out on this slice
			continue
		}

		nr, rerr := syscall.Read(fd, buf)
		if nr > 0 {
			out.Write(buf[:nr])
			if terminated, termErr := evaluateResponseTerminator(out.String()); terminated {
				return out.String(), termErr
			}
		} else if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				resp := out.String()
				if terminated, termErr := evaluateResponseTerminator(resp); terminated {
					return resp, termErr
				}
				return resp, nil
			}
			if !isEAGAIN(rerr) && !errors.Is(rerr, syscall.EINTR) {
				return out.String(), rerr
			}
		}
	}
}
