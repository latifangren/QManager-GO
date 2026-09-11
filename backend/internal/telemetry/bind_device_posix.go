//go:build !windows

package telemetry

import "syscall"

func bindSocketToDevice(fd uintptr, iface string) {
	if iface != "" {
		_ = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, 25, iface) // 25 = SO_BINDTODEVICE
	}
}
