package swarm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// transcript_api.go is SWARM-10's read surface over dumpTranscript's (report.go)
// shipped append-only output: ListChildTranscripts discovers which children have a
// transcript for a conversation, ReadTranscript returns the bytes written so far
// (never blocking, never waiting for EOF) plus a monotonic resume offset. Both are
// package-level functions taking runDir explicitly, mirroring dumpTranscript's own
// shape — this file owns reading, report.go owns writing (the package's existing
// four-way concern split: swarm.go/brief.go/swarm_depth.go/report.go). report.go
// calls transcriptDir/transcriptPath from here so the two files share one hardened
// path-construction rule (T-51-28).

// maxTranscriptReadBytes caps a single ReadTranscript call (T-51-31 DoS mitigation),
// mirroring the maxRunBodyBytes convention (internal/agui/server.go): a huge
// transcript is paged over repeated offset-based calls, never read in one shot.
const maxTranscriptReadBytes = 1 << 20 // 1 MiB

// validatePathSegment rejects a path segment before it reaches any filesystem call
// (T-51-28): empty, a traversal segment ("." or ".."), a path separator (either
// direction, so a Windows host is covered too), or a NUL byte. kind names the
// segment in the returned error (conversation id vs child id) for diagnosability.
func validatePathSegment(kind, s string) error {
	if s == "" {
		return fmt.Errorf("swarm: %s must not be empty", kind)
	}
	if s == "." || s == ".." {
		return fmt.Errorf("swarm: %s %q is a traversal segment", kind, s)
	}
	if strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("swarm: %s %q contains a path separator", kind, s)
	}
	if strings.Contains(s, "\x00") {
		return fmt.Errorf("swarm: %s %q contains a NUL byte", kind, s)
	}
	return nil
}

// transcriptDir returns <runDir>/<conv>/swarm after validating conv. It is the
// mkdir target dumpTranscript uses and the base ListChildTranscripts walks.
func transcriptDir(runDir, conv string) (string, error) {
	if err := validatePathSegment("conversation id", conv); err != nil {
		return "", err
	}
	return filepath.Join(runDir, conv, "swarm"), nil
}

// transcriptPath returns the hardened transcript file path for (runDir, conv,
// childID). Both conv and childID are validated before any filepath.Join, and
// neither os.Open nor os.OpenFile is reached on a rejected input (T-51-28's
// hostile-input test table asserts zero filesystem access).
func transcriptPath(runDir, conv, childID string) (string, error) {
	dir, err := transcriptDir(runDir, conv)
	if err != nil {
		return "", err
	}
	if err := validatePathSegment("child id", childID); err != nil {
		return "", err
	}
	return filepath.Join(dir, childID+".jsonl"), nil
}

// ReadTranscript returns the complete-line bytes written to (conv, childID)'s
// transcript starting at fromOffset, plus the offset the next read should resume
// from. It never blocks and never waits for EOF: a trailing partial line (the
// writer mid-flush) is withheld until a later call observes its closing '\n', so
// two sequential reads never duplicate or skip a line. A transcript that does not
// exist yet (the worker has not flushed its first event) degrades to an empty read
// at the same offset, mirroring dumpTranscript's own best-effort posture — never an
// error. The read is capped at maxTranscriptReadBytes per call (T-51-31).
func ReadTranscript(ctx context.Context, runDir, conv, childID string, fromOffset int64) ([]byte, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, fromOffset, err
	}
	path, err := transcriptPath(runDir, conv, childID)
	if err != nil {
		return nil, fromOffset, err
	}
	if fromOffset < 0 {
		fromOffset = 0
	}
	f, err := os.Open(path) //nolint:gosec // G304: path is built from validatePathSegment-hardened conv/childID, never model-traversable.
	if err != nil {
		if os.IsNotExist(err) {
			return []byte{}, fromOffset, nil
		}
		return nil, fromOffset, fmt.Errorf("swarm: read transcript: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(fromOffset, io.SeekStart); err != nil {
		return nil, fromOffset, fmt.Errorf("swarm: read transcript seek: %w", err)
	}
	buf, err := io.ReadAll(io.LimitReader(f, maxTranscriptReadBytes))
	if err != nil {
		return nil, fromOffset, fmt.Errorf("swarm: read transcript: %w", err)
	}
	lastNL := bytes.LastIndexByte(buf, '\n')
	if lastNL < 0 {
		// No complete line in this window yet (either nothing new, or a partial
		// trailing line the writer has not closed) — report nothing new.
		return []byte{}, fromOffset, nil
	}
	complete := buf[:lastNL+1]
	return complete, fromOffset + int64(len(complete)), nil
}

// ListChildTranscripts returns the child ids that have a transcript file under
// conv's swarm directory (an empty slice, never an error, when the directory does
// not exist yet — before any child has flushed a first event).
func ListChildTranscripts(ctx context.Context, runDir, conv string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := transcriptDir(runDir, conv)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("swarm: list child transcripts: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if id, ok := strings.CutSuffix(name, ".jsonl"); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
