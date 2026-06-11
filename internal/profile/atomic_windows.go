//go:build windows

package profile

import (
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceFile(src, dst string) error {
	srcp, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstp, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	r1, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(srcp)), // #nosec G103 -- MoveFileExW requires UTF-16 pointer conversion.
		uintptr(unsafe.Pointer(dstp)), // #nosec G103 -- MoveFileExW requires UTF-16 pointer conversion.
		uintptr(moveFileReplaceExisting|moveFileWriteThrough),
	)
	if r1 == 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.EINVAL
	}
	return nil
}
