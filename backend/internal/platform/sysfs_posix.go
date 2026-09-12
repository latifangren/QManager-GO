//go:build !windows

package platform

import "syscall"

// GetStorageStats reads filesystem usage for the specified mount path.
func GetStorageStats(mountPath string) *StorageStats {
	if mountPath == "" {
		mountPath = "/usrdata"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPath, &stat); err != nil {
		if err := syscall.Statfs("/", &stat); err != nil {
			return nil
		}
		mountPath = "/"
	}

	bsize := uint64(stat.Bsize)
	totalKB := (stat.Blocks * bsize) / 1024
	freeKB := (stat.Bfree * bsize) / 1024
	availKB := (stat.Bavail * bsize) / 1024
	usedKB := uint64(0)
	if totalKB > freeKB {
		usedKB = totalKB - freeKB
	}

	return &StorageStats{
		Mount:       mountPath,
		TotalKB:     totalKB,
		UsedKB:      usedKB,
		AvailableKB: availKB,
	}
}
