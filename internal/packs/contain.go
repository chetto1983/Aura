package packs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// errOutsideRoot is the containment failure of Agent Plugins §4.1. Each caller applies the
// narrowest failure boundary the specification lists for the path it was resolving.
var errOutsideRoot = errors.New("resolves outside the plugin root")

// withinRoot resolves a plugin-relative path ("./x" or "x") against root and returns its
// filesystem-resolved absolute form. Every symlink that exists along the way is followed, so a
// link whose target leaves the root fails here even when the path's own spelling does not.
// A tail that does not exist yet is joined lexically; it cannot hide a link.
func withinRoot(root, rel string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve plugin root: %w", err)
	}
	joined := filepath.Join(realRoot, filepath.FromSlash(rel))
	if !insideDir(realRoot, joined) {
		return "", errOutsideRoot
	}
	resolved, err := resolveExisting(joined)
	if err != nil {
		return "", err
	}
	if !insideDir(realRoot, resolved) {
		return "", errOutsideRoot
	}
	return resolved, nil
}

// resolveExisting evaluates the longest existing prefix of p and joins the rest back on.
func resolveExisting(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p, nil
	}
	base, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(p)), nil
}

func insideDir(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// statContained is os.Stat of a plugin-relative path that must stay inside root. A missing
// path reports fs.ErrNotExist, which callers treat as an absent component (§6.2).
func statContained(root, rel string) (os.FileInfo, error) {
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		return nil, err
	}
	resolved, err := withinRoot(root, rel)
	if err != nil {
		return nil, err
	}
	return os.Stat(resolved)
}
