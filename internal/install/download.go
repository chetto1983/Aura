package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultMaxAttempts caps the retry loop for transient network/server
	// failures. 5 attempts with exponential backoff (1m → 2m → 4m → 8m → 16m)
	// total worst-case wait ≈ 31 min — enough to ride out a typical HF
	// outage without burning CPU on retry storms.
	DefaultMaxAttempts = 5

	// DefaultRetryBaseDelay is the initial backoff before retry attempt #2.
	DefaultRetryBaseDelay = time.Minute

	// readBufSize balances syscall overhead against memory pressure on the
	// init container (distroless, no swap).
	readBufSize = 256 * 1024
)

// Default HTTP client. Times out the whole transfer at 30 min so a stuck
// connection eventually fails instead of hanging the boot indefinitely.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Minute}

// VerifyFileSHA256 checks an on-disk file against the expected lowercase-hex
// SHA-256 digest. Returns nil on match, an error otherwise (including when
// the file does not exist).
func VerifyFileSHA256(path, wantSHA256 string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("install: hash %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantSHA256 {
		return fmt.Errorf("install: sha256 mismatch %s: want=%s got=%s", path, wantSHA256, got)
	}
	return nil
}

// DownloadOpts controls a single Download invocation.
type DownloadOpts struct {
	// URL is the HTTP(S) source. Required.
	URL string
	// Dest is the final on-disk path; the function writes to <Dest>.partial
	// first and atomically renames on successful hash verification.
	Dest string
	// SHA256 is the lowercase-hex expected digest of the full file content.
	// Verified streaming during the download; on mismatch the partial file
	// is deleted and an error is returned. Required.
	SHA256 string
	// Progress receives byte counts. May be nil.
	Progress ProgressFn
	// HTTPClient overrides the default (30-min timeout). Tests inject here.
	HTTPClient *http.Client
	// MaxAttempts caps retries on transient errors. Defaults to
	// DefaultMaxAttempts when zero.
	MaxAttempts int
	// RetryBaseDelay is the first backoff window. Defaults to
	// DefaultRetryBaseDelay when zero.
	RetryBaseDelay time.Duration
}

// Download fetches URL into Dest with SHA-256 verification, resume support,
// and bounded retry. Re-running with the destination already present + valid
// is a fast no-op (single hash pass, no HTTP). The caller is responsible for
// ensuring Dest's parent directory exists.
func Download(ctx context.Context, opts DownloadOpts) error {
	if opts.URL == "" {
		return errors.New("install: URL required")
	}
	if opts.Dest == "" {
		return errors.New("install: Dest required")
	}
	if opts.SHA256 == "" {
		return errors.New("install: SHA256 required")
	}

	// Fast path: already-installed file with matching hash.
	if err := VerifyFileSHA256(opts.Dest, opts.SHA256); err == nil {
		if opts.Progress != nil {
			// Synthesize a final 100% tick so callers (the UI in particular)
			// don't need a special "cached" branch.
			if info, statErr := os.Stat(opts.Dest); statErr == nil {
				opts.Progress(info.Size(), info.Size())
			}
		}
		return nil
	}

	client := opts.HTTPClient
	if client == nil {
		client = defaultHTTPClient
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	base := opts.RetryBaseDelay
	if base <= 0 {
		base = DefaultRetryBaseDelay
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := downloadOnce(ctx, client, opts)
		if err == nil {
			return nil
		}
		lastErr = err
		// SHA mismatch is non-recoverable — don't burn retries on a
		// permanently-bad upstream file. The function deletes the partial
		// inside downloadOnce so the next manual retry starts clean.
		var shaErr *shaMismatchError
		if errors.As(err, &shaErr) {
			return err
		}
		if attempt == maxAttempts {
			break
		}
		// Exponential backoff: 1m, 2m, 4m, 8m, 16m.
		wait := base << (attempt - 1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return fmt.Errorf("install: download failed after %d attempts: %w", maxAttempts, lastErr)
}

// shaMismatchError signals a non-recoverable upstream content corruption.
// Distinct type so Download can short-circuit the retry loop via errors.As.
type shaMismatchError struct {
	want, got string
}

func (e *shaMismatchError) Error() string {
	return fmt.Sprintf("install: sha256 mismatch want=%s got=%s", e.want, e.got)
}

// downloadOnce performs a single attempt: resume from <Dest>.partial if it
// exists, stream into the partial while hashing, verify, and atomically
// rename. Cleans up the partial on SHA mismatch so the next attempt starts
// fresh; preserves it on transport errors so the next attempt can resume.
func downloadOnce(ctx context.Context, client *http.Client, opts DownloadOpts) error {
	partial := opts.Dest + ".partial"

	// Re-hash any pre-existing partial so the streaming hash covers the
	// full final file, not just the new bytes appended this attempt.
	h := sha256.New()
	var startAt int64
	if info, err := os.Stat(partial); err == nil && info.Size() > 0 {
		if rehashErr := rehashInto(partial, h); rehashErr != nil {
			// Partial unreadable — start fresh.
			_ = os.Remove(partial)
			h = sha256.New()
			startAt = 0
		} else {
			startAt = info.Size()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return fmt.Errorf("install: build request: %w", err)
	}
	if startAt > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startAt))
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("install: GET %s: %w", opts.URL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// Server ignored our Range header — discard prior partial and
		// restart from byte 0. Hash already reset above when we read no
		// existing partial; if we did read one, reset it now.
		if startAt > 0 {
			_ = os.Remove(partial)
			h = sha256.New()
			startAt = 0
		}
	case http.StatusPartialContent:
		// Server honored Range — continue from startAt.
	default:
		return fmt.Errorf("install: HTTP %d from %s", resp.StatusCode, opts.URL)
	}

	totalBytes := startAt + resp.ContentLength

	f, err := os.OpenFile(partial, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("install: open partial: %w", err)
	}
	// Use a buffered loop instead of io.Copy so we can fire ProgressFn
	// per-chunk and honor ctx cancellation between chunks.
	buf := make([]byte, readBufSize)
	written := startAt
	closed := false
	closeOnce := func() {
		if !closed {
			_ = f.Close()
			closed = true
		}
	}
	defer closeOnce()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fmt.Errorf("install: write partial: %w", werr)
			}
			h.Write(buf[:n])
			written += int64(n)
			if opts.Progress != nil {
				opts.Progress(written, totalBytes)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("install: read body: %w", readErr)
		}
	}
	closeOnce()

	got := hex.EncodeToString(h.Sum(nil))
	if got != opts.SHA256 {
		_ = os.Remove(partial)
		return &shaMismatchError{want: opts.SHA256, got: got}
	}
	if err := os.Rename(partial, opts.Dest); err != nil {
		return fmt.Errorf("install: rename %s → %s: %w", partial, opts.Dest, err)
	}
	return nil
}

func rehashInto(path string, h io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(h, f)
	return err
}

// EnsureParentDir creates the directory containing path if it does not exist.
// Convenience for callers that may run before runtimebootstrap has touched
// the layout (e.g. the init container).
func EnsureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
