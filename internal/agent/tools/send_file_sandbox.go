package tools

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// send_file's ROUTED branch (D-10, plan 37-07): under a strict profile the requested path is a BOX
// path, which the host checkWorkspace fence CANNOT validate (filepath.Abs/EvalSymlinks over a box
// path is meaningless). The routed branch therefore fences in TWO steps: (1) a literal /workspace
// prefix-check on the box path BEFORE copy-out (rejecting an out-of-workspace box path — never
// host-stating it), then (2) CopyArtifactsOut stages the artifact to a host-readable dir and the
// host fence + size-gate re-run over the STAGED copy AFTER copy-out, so the symlink-escape / size
// invariants still hold on the vetted host-side copy.

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
	// (2b) Re-run the host workspace fence + size gate over the STAGED copy (rooted at the staging
	// dir, requireWorkspace=true) before delivery — the same symlink/size invariants as a host file.
	resolved, ok, ferr := fenceWithinRoot(stageDir, true, staged)
	if ferr != nil {
		return errorResult("file_unreadable", fmt.Sprintf("cannot resolve staged artifact %q: %v", staged, ferr)), nil
	}
	if !ok {
		return outsideWorkspaceResult(resolved, stageDir), nil
	}
	return emitDelivery(resolved, caption), nil
}

// stageBoxArtifact extracts the FIRST regular file from a CopyArtifactsOut tar stream into a fresh
// staging dir under the tool-call run dir, returning (stageDir, stagedFilePath). The extracted name
// is basename-only (no tar path traversal); a symlink/non-regular entry is skipped. The read is
// bounded to maxSendFileBytes+1 so a pathological artifact cannot exhaust memory (the size gate
// then rejects an over-cap file).
func stageBoxArtifact(ctx context.Context, r io.Reader, fallbackName string) (string, string, error) {
	stageDir, err := os.MkdirTemp(runDirFromCtx(ctx), "aura-sendfile-*")
	if err != nil {
		return "", "", err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Zipslip guard: filepath.Base strips every directory component, and a residual "."/".."/
		// separator entry falls back to the (safe) basename of the requested box path — so the
		// extracted name can never carry traversal.
		name := filepath.Base(hdr.Name)
		if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
			name = fallbackName
		}
		dest := filepath.Join(stageDir, name)
		// Defence-in-depth: refuse any entry whose joined path escapes the staging dir (the tar
		// stream is daemon-produced, but the containment invariant must hold on the sink itself).
		if dest != stageDir && !strings.HasPrefix(dest, filepath.Clean(stageDir)+string(os.PathSeparator)) {
			continue
		}
		f, err := os.Create(dest)
		if err != nil {
			return "", "", err
		}
		_, cerr := io.Copy(f, io.LimitReader(tr, maxSendFileBytes+1))
		closeErr := f.Close()
		if cerr != nil {
			return "", "", cerr
		}
		if closeErr != nil {
			return "", "", closeErr
		}
		return stageDir, dest, nil
	}
	return "", "", fmt.Errorf("sandbox artifact stream contained no regular file")
}

// runDirFromCtx returns the tool-call run dir for staging ("" → the OS temp dir default of
// os.MkdirTemp), so a staged artifact lands under the conversation's own run tree.
func runDirFromCtx(ctx context.Context) string {
	if tc, ok := toolCallCtx(ctx); ok {
		return tc.runDir
	}
	return ""
}
