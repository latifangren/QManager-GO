//go:build windows

package platform

// Magic numbers for Linux filesystem types
const (
	TMPFS_MAGIC uint64 = 0x01021994
	RAMFS_MAGIC uint64 = 0x858458f6
	UBIFS_MAGIC uint64 = 0x24051905
)

// IsTmpfsOrRamfs returns mock status for non-POSIX/Windows development machines.
func IsTmpfsOrRamfs(dirPath string) (bool, uint64, error) {
	return true, TMPFS_MAGIC, nil
}

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
