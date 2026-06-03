package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WorkspaceManager owns the per-conversation host workspace tree under
// <runDir>/conversations/<id>/. The container mounts <id>/workspace/ RW as
// /workspace (owner 65532); Aura also writes <tool_call_id>.result spillover in
// the same <id>/ dir (co-tenancy landmine #4). All walks are os.Root/openat2
// confined so an attacker-planted symlink in the sidecar-writable workspace can
// never redirect a host read or unlink outside the tree (D-13/D-14).
type WorkspaceManager struct {
	runDir   string
	maxBytes int64
}

// NewWorkspaceManager builds a manager rooted at runDir with the per-conversation
// quota maxBytes (AURA_SANDBOX_WORKSPACE_MAX_BYTES). A non-positive quota disables
// the size gate (CheckQuota becomes a no-op) — the caller (08-07) decides policy.
func NewWorkspaceManager(runDir string, maxBytes int) *WorkspaceManager {
	return &WorkspaceManager{runDir: runDir, maxBytes: int64(maxBytes)}
}

// EnsureDir creates <runDir>/conversations/<id>/workspace/ and returns the
// workspace path. The dir is owner-writable; the container's 65532 user writes
// into the bind-mounted /workspace, but the HOST dir is created by Aura (the host
// owner) so the os.Root walks anchor on an Aura-created root.
func (w *WorkspaceManager) EnsureDir(convID string) (string, error) {
	ws, err := w.workspacePath(convID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(ws, 0o770); err != nil {
		return "", fmt.Errorf("ensure workspace %s: %w", convID, err)
	}
	return ws, nil
}

// WalkSize sums regular-file bytes beneath the workspace via os.OpenRoot (openat2
// RESOLVE_BENEATH). Symlinks are detected by Lstat and NEVER followed/summed, so a
// symlinked dir pointing outside the workspace contributes nothing (D-13). A
// missing workspace is size 0, not an error.
func (w *WorkspaceManager) WalkSize(convID string) (int64, error) {
	ws, err := w.workspacePath(convID)
	if err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(ws)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("walk size %s: open root: %w", convID, err)
	}
	defer func() { _ = root.Close() }()
	return sizeBeneath(root, ".")
}

// CheckQuota returns ErrWorkspaceQuotaExceeded when the workspace exceeds maxBytes.
// A non-positive quota disables the gate.
func (w *WorkspaceManager) CheckQuota(convID string) error {
	if w.maxBytes <= 0 {
		return nil
	}
	total, err := w.WalkSize(convID)
	if err != nil {
		return err
	}
	if total > w.maxBytes {
		return fmt.Errorf("workspace %s is %d bytes (quota %d): %w", convID, total, w.maxBytes, ErrWorkspaceQuotaExceeded)
	}
	return nil
}

// PurgeConversationDir removes the WHOLE <id>/ dir (workspace/ AND .result
// spillover — landmine #4) with a manual post-order no-follow openat cascade. It
// NEVER uses os.RemoveAll on the attacker-controlled workspace subtree (D-14):
// os.Root has no RemoveAll (golang/go#67002) and os.RemoveAll is TOCTOU-symlink-
// susceptible (golang/go#52745). root.Remove unlinks the LINK, never the target
// (ROADMAP criterion 2). A missing dir is a no-op success.
func (w *WorkspaceManager) PurgeConversationDir(convID string) error {
	dir, err := w.conversationPath(convID)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("purge %s: open root: %w", convID, err)
	}
	if err := purgeBeneath(root, "."); err != nil {
		_ = root.Close()
		return fmt.Errorf("purge %s: %w", convID, err)
	}
	if err := root.Close(); err != nil {
		return fmt.Errorf("purge %s: close root: %w", convID, err)
	}
	// The root dir itself is now empty; remove it from the parent (validated path,
	// not attacker-influenceable beyond the id segment which workspacePath checks).
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("purge %s: remove dir: %w", convID, err)
	}
	return nil
}

// sizeBeneath recursively sums regular-file sizes under dir within root. It Lstats
// each entry (no symlink follow) and only adds Mode().IsRegular() files; symlinks
// and their targets are skipped entirely.
func sizeBeneath(root *os.Root, dir string) (int64, error) {
	names, err := readDirNames(root, dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, name := range names {
		p := path.Join(dir, name)
		info, err := root.Lstat(p)
		if err != nil {
			return 0, err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			continue // never follow/sum a symlink (D-13)
		case info.IsDir():
			sub, err := sizeBeneath(root, p)
			if err != nil {
				return 0, err
			}
			total += sub
		case info.Mode().IsRegular():
			total += info.Size()
		}
	}
	return total, nil
}

// purgeBeneath is the post-order no-follow cascade. It Lstats each entry, recurses
// into REAL subdirs (symlink-to-dir reports as ModeSymlink, so it is unlinked, not
// traversed), then root.Remove unlinks every entry (file/symlink/now-empty dir).
func purgeBeneath(root *os.Root, dir string) error {
	names, err := readDirNames(root, dir)
	if err != nil {
		return err
	}
	for _, name := range names {
		p := path.Join(dir, name)
		info, err := root.Lstat(p)
		if err != nil {
			return err
		}
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if err := purgeBeneath(root, p); err != nil {
				return err
			}
		}
		if err := root.Remove(p); err != nil {
			return err // unlinks the link itself, never the target
		}
	}
	return nil
}

// readDirNames lists the entry names of dir within root using an openat-confined
// *File. os.Root has no ReadDir, so we Open the dir handle and Readdirnames.
func readDirNames(root *os.Root, dir string) ([]string, error) {
	f, err := root.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return names, nil
}

// conversationPath returns <runDir>/conversations/<id>, rejecting an id that could
// escape the fixed prefix (mirrors conversations.validateID — sandbox must NOT
// import conversations, landmine #4, so the guard is duplicated locally).
func (w *WorkspaceManager) conversationPath(convID string) (string, error) {
	if err := validateConvID(convID); err != nil {
		return "", err
	}
	return filepath.Join(w.runDir, "conversations", convID), nil
}

func (w *WorkspaceManager) workspacePath(convID string) (string, error) {
	dir, err := w.conversationPath(convID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspace"), nil
}

// validateConvID rejects an id that could traverse out of the conversations prefix
// once joined: a `..` component or an embedded path separator. The only
// model/agent-supplied segment is the conversation_id (D-26).
func validateConvID(convID string) error {
	if convID == "" {
		return errors.New("conversation_id is empty")
	}
	if strings.Contains(convID, "..") {
		return fmt.Errorf("conversation_id %q contains %q", convID, "..")
	}
	for i := 0; i < len(convID); i++ {
		if convID[i] == '/' || convID[i] == '\\' || os.IsPathSeparator(convID[i]) {
			return fmt.Errorf("conversation_id %q contains a path separator", convID)
		}
	}
	return nil
}
