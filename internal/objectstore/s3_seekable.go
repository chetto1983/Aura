package objectstore

import (
	"io"
	"os"
)

// seekableBody returns body unchanged when it can already seek; otherwise it spools
// the stream to a temp file and hands back a seekable reader. The AWS SDK signs the
// SigV4 payload hash off a seekable body, so a streamed source (a Telegram getFile
// download piped straight into ingest) fails PutObject with "failed to compute
// payload hash: failed to seek body to start" (measured 2026-08-30, amendment #198).
// The caller must invoke cleanup after the upload.
func seekableBody(body io.Reader) (io.Reader, func(), error) {
	if _, ok := body.(io.ReadSeeker); ok {
		return body, func() {}, nil
	}
	tmp, err := os.CreateTemp("", "aura-s3-put-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	if _, err := io.Copy(tmp, body); err != nil {
		cleanup()
		return nil, nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, nil, err
	}
	return tmp, cleanup, nil
}
