package objectstore

import (
	"errors"
	"io/fs"
)

// IsNotFound distinguishes verified absence from transport, authorization, and
// server failures returned by filesystem and S3-compatible stores.
func IsNotFound(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var statusError interface{ HTTPStatusCode() int }
	if errors.As(err, &statusError) && statusError.HTTPStatusCode() == 404 {
		return true
	}
	var codeError interface{ ErrorCode() string }
	if !errors.As(err, &codeError) {
		return false
	}
	switch codeError.ErrorCode() {
	case "NotFound", "NoSuchKey", "NoSuchObject":
		return true
	default:
		return false
	}
}
