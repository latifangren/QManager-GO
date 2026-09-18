//go:build !windows

package platform

import "syscall"

// Magic numbers for Linux filesystem types
const (
	TMPFS_MAGIC uint64 = 0x01021994
	RAMFS_MAGIC uint64 = 0x858458f6
	UBIFS_MAGIC uint64 = 0x24051905
)

// IsTmpfsOrRamfs checks if path is mounted on tmpfs or ramfs.
func IsTmpfsOrRamfs(dirPath string) (bool, uint64, error) {
	if dirPath == "" {
		dirPath = "/tmp"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dirPath, &stat); err != nil {
		return false, 0, err
	}
	fsType := uint64(stat.Type)
	isRam := fsType == TMPFS_MAGIC || fsType == RAMFS_MAGIC
	return isRam, fsType, nil
}

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
