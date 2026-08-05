package tools

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// send_file's delivery path (D-10, plan 37-07). The requested path is always a BOX path, and a box
// path cannot be fenced with filepath.Abs/EvalSymlinks — those resolve against the aura container's
// filesystem, a DIFFERENT volume mounted at the same names. So the fence runs in TWO steps:
// (1) a literal /workspace prefix-check on the box path BEFORE copy-out, rejecting an
// out-of-workspace box path without ever host-stat'ing it, then (2) CopyArtifactsOut stages the
// artifact to a host-readable dir and fenceWithinRoot + the size gate re-run over the STAGED copy,
// so the symlink-escape and size invariants still hold — on the one copy that IS a host file.

// deliverFromBox stages a box /workspace artifact out and delivers the vetted host-side copy.
func (s *SendFile) deliverFromBox(ctx context.Context, h usersandbox.BoxHandle, boxPath, caption string) (ToolResult, error) {
	// (1) Pre-copy fence: the box path must be under the literal /workspace root.
	if !withinBoxWorkspace(boxPath) {
		return outsideWorkspaceResult(boxPath, boxWorkspaceRoot), nil
	}
	rc, err := s.Router.CopyArtifactOut(ctx, h, boxPath)
	if err != nil {
		return sandboxUnavailableResult("send_file", err), nil
	}
	defer func() { _ = rc.Close() }()

	// (2a) Stage the tar stream out to a host-readable dir under the run dir.
	stageDir, staged, err := stageBoxArtifact(ctx, rc, filepath.Base(boxPath))
	if err != nil {
		return errorResult("file_unreadable", fmt.Sprintf("cannot stage %q from the sandbox: %v", boxPath, err)), nil
	}
	// (2b) Re-run the workspace fence + size gate over the STAGED copy (rooted at the staging dir,
	// requireWorkspace=true) before delivery — the staged copy is a real host file, so the symlink
	// and size invariants are enforceable on it.
	resolved, ok, ferr := fenceWithinRoot(stageDir, staged)
	if ferr != nil {
		return errorResult("file_unreadable", fmt.Sprintf("cannot resolve staged artifact %q: %v", staged, ferr)), nil
	}
	if !ok {
		return outsideWorkspaceResult(resolved, stageDir), nil
	}
	return s.emitDelivery(ctx, resolved, caption), nil
}

// stageBoxArtifact extracts the FIRST regular file from a CopyArtifactsOut tar stream into a fresh
// staging dir under the tool-call run dir, returning (stageDir, stagedFilePath). The extracted name
// is basename-only (no tar path traversal); a symlink/non-regular entry is skipped. The read is
// bounded to maxSendFileBytes+1 so a pathological artifact cannot exhaust memory (the caller's size
// gate then rejects an over-cap file).
//
// Both box→host tools stage through here — send_file, to deliver the vetted copy, and
// document_index, to hand the ingest pipeline bytes it can actually read — hence the neutral
// prefix. A FAILED extraction takes its staging dir with it: the caller gets no path back, so
// anything left behind would sit there until the 24h run-dir sweeper (or the OS) noticed.
func stageBoxArtifact(ctx context.Context, r io.Reader, fallbackName string) (string, string, error) {
	return stageBoxArtifactCapped(ctx, r, fallbackName, maxSendFileBytes)
}

func stageBoxArtifactCapped(ctx context.Context, r io.Reader, fallbackName string, maxBytes int64) (string, string, error) {
	root, err := stagingRoot(ctx)
	if err != nil {
		return "", "", err
	}
	stageDir, err := os.MkdirTemp(root, "aura-boxstage-*")
	if err != nil {
		return "", "", err
	}
	staged, err := extractFirstRegularFile(tar.NewReader(r), stageDir, fallbackName, maxBytes)
	if err != nil {
		_ = os.RemoveAll(stageDir)
		return "", "", err
	}
	return stageDir, staged, nil
}

// extractFirstRegularFile writes the first regular tar entry into stageDir and returns its path.
// The archive entry supplies bytes only: its untrusted name never reaches a filesystem operation.
func extractFirstRegularFile(tr *tar.Reader, stageDir, fallbackName string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		return "", fmt.Errorf("staged artifact size limit must be positive")
	}
	name := filepath.Clean(fallbackName)
	if !filepath.IsLocal(name) || name == "." || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid staged artifact name %q", fallbackName)
	}
	dest := filepath.Join(stageDir, name)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		_, cerr := io.Copy(f, io.LimitReader(tr, maxBytes+1))
		closeErr := f.Close()
		if cerr != nil {
			return "", cerr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return dest, nil
	}
	return "", fmt.Errorf("sandbox artifact stream contained no regular file")
}

// stagingRoot is $AURA_RUN_DIR/tmp — the one run-dir subtree an existing sweeper already reclaims
// (conversations.ScanOrphans → sweepTmp, at boot and on every sweeper tick, 24h TTL). An empty run
// dir falls back to os.MkdirTemp's OS temp default, which the OS reclaims.
//
// Staging at the run-dir ROOT, as this did while only the strict profile reached it, left a full
// copy of every delivered file (up to 50 MiB) there forever — nothing enumerates the run dir itself.
// With one execution path EVERY delivery stages, so the leak went from occasional to per-call. A
// defer-RemoveAll is not the alternative: the descriptor carries a PATH, and the channel opens it
// lazily when it renders the artifact event — after the turn returns — so deleting on return would
// deliver an empty file.
func stagingRoot(ctx context.Context) (string, error) {
	tc, ok := toolCallCtx(ctx)
	if !ok || tc.runDir == "" {
		return "", nil
	}
	root := filepath.Join(tc.runDir, "tmp")
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", err
	}
	return root, nil
}
