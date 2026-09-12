//go:build windows

package platform

// GetStorageStats provides mock fallback filesystem usage on non-POSIX/Windows development machines.
func GetStorageStats(mountPath string) *StorageStats {
	if mountPath == "" {
		mountPath = "C:\\"
	}
	return &StorageStats{
		Mount:       mountPath,
		TotalKB:     1024 * 1024,
		UsedKB:      512 * 1024,
		AvailableKB: 512 * 1024,
	}
}
