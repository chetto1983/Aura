package objectstore

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
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

func checkOffset(offset int64) error {
	if offset < 0 {
		return fmt.Errorf("objectstore: negative read offset %d", offset)
	}
	return nil
}

// isRangeNotSatisfiable reports the 416 an S3 store answers when a Range starts at or past the
// object's end (RFC 9110 §15.5.17). Measured 2026-09-15 on dxflrs/garage:v2.3.0: "bytes=10-" on
// a 10-byte object answers 416 InvalidRange, while a missing key stays 404 NoSuchKey.
func isRangeNotSatisfiable(err error) bool {
	var statusError interface{ HTTPStatusCode() int }
	return errors.As(err, &statusError) && statusError.HTTPStatusCode() == http.StatusRequestedRangeNotSatisfiable
}
