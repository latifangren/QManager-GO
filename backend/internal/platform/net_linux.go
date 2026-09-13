//go:build linux

package platform

import (
	"syscall"
	"unsafe"
)

// SetInterfaceMTU sets the MTU of a network interface using ioctl(SIOCSIFMTU) kernel syscall.
func SetInterfaceMTU(ifaceName string, mtu int) error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	var ifr struct {
		Name [16]byte
		MTU  int32
		Pad  [20]byte
	}
	copy(ifr.Name[:], ifaceName)
	ifr.MTU = int32(mtu)

	// SIOCSIFMTU = 0x8922 on Linux
	const SIOCSIFMTU = 0x8922
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(SIOCSIFMTU), uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		return errno
	}
	return nil
}
