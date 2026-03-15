package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func checkDiskSpace(backupDir string, pathsToBackup map[string]string) error {
	var totalSize int64

	for _, path := range pathsToBackup {
		size, err := dirSize(path)
		if err != nil {
			return fmt.Errorf("calculate size of %s: %w", path, err)
		}
		totalSize += size
	}

	stat := syscall.Statfs_t{}
	if err := syscall.Statfs(backupDir, &stat); err != nil {
		return fmt.Errorf("get disk stats: %w", err)
	}

	available := int64(stat.Bavail) * int64(stat.Bsize)
	needed := totalSize + (totalSize / 10)

	if available < needed {
		return fmt.Errorf("%w: need %d bytes, have %d bytes", ErrInsufficientSpace, needed, available)
	}

	return nil
}

func dirSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}
