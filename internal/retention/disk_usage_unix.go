//go:build !windows

package retention

import (
	"fmt"
	"syscall"
)

func sampleDiskUsage(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("stat filesystem: %w", err)
	}
	total := float64(stat.Blocks) * float64(stat.Bsize)
	available := float64(stat.Bavail) * float64(stat.Bsize)
	if total <= 0 {
		return 0, fmt.Errorf("filesystem capacity is zero")
	}
	if available > total {
		available = total
	}
	return 1 - available/total, nil
}
