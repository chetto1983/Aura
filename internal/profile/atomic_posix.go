//go:build !windows

package profile

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
