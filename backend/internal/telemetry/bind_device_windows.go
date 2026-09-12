//go:build windows

package telemetry

func bindSocketToDevice(fd uintptr, iface string) {
	// SO_BINDTODEVICE is not supported on Windows
}
