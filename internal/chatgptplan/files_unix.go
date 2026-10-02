//go:build !windows

package chatgptplan

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func privatePath(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}

func tryFileLock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func unlockFile(f *os.File) error  { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
func lockBusy(err error) bool      { return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) }
