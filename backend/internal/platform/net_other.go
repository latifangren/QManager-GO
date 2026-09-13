//go:build !linux

package platform

import "fmt"

// SetInterfaceMTU is a dummy implementation for non-Linux OS.
func SetInterfaceMTU(ifaceName string, mtu int) error {
	return fmt.Errorf("SetInterfaceMTU only supported on Linux")
}
