//go:build windows

package retention

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func sampleDiskUsage(path string) (float64, error) {
	directory, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode filesystem path: %w", err)
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(directory, &available, &total, &free); err != nil {
		return 0, fmt.Errorf("get filesystem capacity: %w", err)
	}
	return ratioFromCapacity(total, available)
}
