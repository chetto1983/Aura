package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const (
	treeDefaultDepth   = 4
	treeMaxDepth       = 16
	treeDefaultEntries = 200
	treeMaxEntries     = 2000
	findDefaultLimit   = 50
	findMaxLimit       = 500
	copyMaxBytes       = 4 * 1024 * 1024
)

// TreeNode is the recursive shape used by file(action="tree"). A node
// is either a file (Type="file", no Children) or a directory (Type="dir",
// Children may be nil if depth/entries caps were hit). Truncated indicates
// a directory whose listing was clipped by limits.
type TreeNode struct {
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Size      int64      `json:"size,omitempty"`
	Children  []TreeNode `json:"children,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
}

// PathInfo is the stat-shaped result returned by file(action="info").
type PathInfo struct {
	Path    string `json:"path"`
	Type    string `json:"type"` // "file" | "dir" | "missing"
	Exists  bool   `json:"exists"`
	Size    int64  `json:"size,omitempty"`
	Mode    string `json:"mode,omitempty"`
	ModTime string `json:"mod_time,omitempty"`
}

// Remove deletes a file inside the root. Directories are rejected — a
// recursive delete is too easy to weaponise via prompt injection, so the
// only way to remove a directory is to empty it first (one Remove per
// file) and then call this on the directory with allowDir=true. We keep
// the dir branch behind a separate flag so the common case stays narrow.
func (r *Root) Remove(rel string, allowDir bool) error {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return err
	}
	if cleanRel == "." || cleanRel == "" {
		return fmt.Errorf("%w: refuse to remove workspace root", ErrDeniedPath)
	}
	info, err := r.root.Stat(cleanRel)
	if err != nil {
		return fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	if info.IsDir() {
		if !allowDir {
			return fmt.Errorf("workspace: %s is a directory (pass allowDir to remove an empty directory)", cleanRel)
		}
		dir, err := r.root.Open(cleanRel)
		if err != nil {
			return fmt.Errorf("workspace: open dir %s: %w", cleanRel, err)
		}
		entries, listErr := dir.ReadDir(1)
		_ = dir.Close()
		// ReadDir(1) returns io.EOF when the directory is empty — treat that
		// as success here. Any other error is surfaced.
		if listErr != nil && !errors.Is(listErr, io.EOF) {
			return fmt.Errorf("workspace: list %s: %w", cleanRel, listErr)
		}
		if len(entries) > 0 {
			return fmt.Errorf("workspace: directory %s is not empty", cleanRel)
		}
	}
	if err := r.root.Remove(cleanRel); err != nil {
		return fmt.Errorf("workspace: remove %s: %w", cleanRel, err)
	}
	return nil
}

// Move renames src to dst. Both paths go through resolveRel (kernel and
// denylist checks). The dst is additionally screened for binary
// extensions so an injection can't shuffle a text file into a forbidden
// shape (e.g. note.md → note.exe). Cross-directory renames are allowed;
// the kernel handles same-filesystem atomicity.
func (r *Root) Move(src, dst string) error {
	srcRel, err := r.resolveRel(src)
	if err != nil {
		return fmt.Errorf("workspace: move src: %w", err)
	}
	dstRel, err := r.resolveRel(dst)
	if err != nil {
		return fmt.Errorf("workspace: move dst: %w", err)
	}
	if srcRel == "." || dstRel == "." {
		return fmt.Errorf("%w: refuse to move workspace root", ErrDeniedPath)
	}
	if srcRel == dstRel {
		return fmt.Errorf("workspace: move src and dst are identical")
	}
	if isBinaryExtension(dstRel) {
		return fmt.Errorf("%w: dst has a binary/executable extension", ErrDeniedPath)
	}
	parent := path.Dir(dstRel)
	if parent != "" && parent != "." {
		if err := r.root.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", parent, err)
		}
	}
	if err := r.root.Rename(srcRel, dstRel); err != nil {
		return fmt.Errorf("workspace: rename %s -> %s: %w", srcRel, dstRel, err)
	}
	return nil
}

// Copy duplicates a file (only — no recursive directory copy). The src is
// read through the standard Read pipeline so sensitive-path and oversize
// gates apply uniformly. The dst is written via WriteAtomic for the same
// containment guarantees as file(action="write").
func (r *Root) Copy(src, dst string) error {
	srcRel, err := r.resolveRel(src)
	if err != nil {
		return fmt.Errorf("workspace: copy src: %w", err)
	}
	dstRel, err := r.resolveRel(dst)
	if err != nil {
		return fmt.Errorf("workspace: copy dst: %w", err)
	}
	if srcRel == "." || dstRel == "." {
		return fmt.Errorf("%w: refuse to copy workspace root", ErrDeniedPath)
	}
	if srcRel == dstRel {
		return fmt.Errorf("workspace: copy src and dst are identical")
	}
	info, err := r.root.Stat(srcRel)
	if err != nil {
		return fmt.Errorf("workspace: stat %s: %w", srcRel, err)
	}
	if info.IsDir() {
		return fmt.Errorf("workspace: %s is a directory (recursive copy is not supported)", srcRel)
	}
	data, err := r.Read(srcRel, copyMaxBytes)
	if err != nil {
		return fmt.Errorf("workspace: copy read: %w", err)
	}
	if err := r.WriteAtomic(dstRel, data); err != nil {
		return fmt.Errorf("workspace: copy write: %w", err)
	}
	return nil
}

// Tree returns a nested representation of the directory rooted at rel.
// Depth and entry caps protect against pathological structures (a deeply
// nested or wide tree would otherwise blow the LLM's tool-result budget).
// Sensitive paths are filtered, matching the rest of the workspace API.
func (r *Root) Tree(rel string, maxDepth, maxEntries int) (TreeNode, error) {
	if maxDepth <= 0 {
		maxDepth = treeDefaultDepth
	}
	if maxDepth > treeMaxDepth {
		maxDepth = treeMaxDepth
	}
	if maxEntries <= 0 {
		maxEntries = treeDefaultEntries
	}
	if maxEntries > treeMaxEntries {
		maxEntries = treeMaxEntries
	}
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return TreeNode{}, err
	}
	info, err := r.root.Stat(cleanRel)
	if err != nil {
		return TreeNode{}, fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	visited := 0
	node, err := r.treeWalk(cleanRel, info, maxDepth, maxEntries, &visited)
	if err != nil {
		return TreeNode{}, err
	}
	return node, nil
}

func (r *Root) treeWalk(rel string, info fs.FileInfo, depthRemaining, maxEntries int, visited *int) (TreeNode, error) {
	name := path.Base(rel)
	if rel == "." {
		name = "."
	}
	node := TreeNode{Path: rel, Name: name}
	if !info.IsDir() {
		node.Type = "file"
		node.Size = info.Size()
		return node, nil
	}
	node.Type = "dir"
	if depthRemaining <= 0 {
		node.Truncated = true
		return node, nil
	}
	dir, err := r.root.Open(rel)
	if err != nil {
		return node, fmt.Errorf("workspace: open dir %s: %w", rel, err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return node, fmt.Errorf("workspace: list %s: %w", rel, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	children := make([]TreeNode, 0, len(entries))
	for _, entry := range entries {
		var child string
		if rel == "." {
			child = entry.Name()
		} else {
			child = path.Join(rel, entry.Name())
		}
		child = path.Clean(strings.ReplaceAll(child, "\\", "/"))
		if isSensitivePath(child) {
			continue
		}
		if *visited >= maxEntries {
			node.Truncated = true
			break
		}
		*visited++
		childInfo, err := entry.Info()
		if err != nil {
			continue
		}
		sub, err := r.treeWalk(child, childInfo, depthRemaining-1, maxEntries, visited)
		if err != nil {
			return node, err
		}
		children = append(children, sub)
	}
	node.Children = children
	return node, nil
}

// Info returns lightweight metadata for rel. Missing paths return
// Exists=false rather than an error so the LLM can probe paths without
// branching on errno strings.
func (r *Root) Info(rel string) (PathInfo, error) {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return PathInfo{}, err
	}
	info, err := r.root.Stat(cleanRel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return PathInfo{Path: cleanRel, Type: "missing", Exists: false}, nil
		}
		return PathInfo{}, fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	kind := "file"
	if info.IsDir() {
		kind = "dir"
	}
	return PathInfo{
		Path:    cleanRel,
		Type:    kind,
		Exists:  true,
		Size:    info.Size(),
		Mode:    info.Mode().String(),
		ModTime: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
	}, nil
}

// Find walks the tree and returns paths whose basename equals name
// (case-insensitive). Sensitive directories are pruned, binary
// extensions are skipped — matching Search semantics so an LLM that
// resolved a path with Find can immediately Read it without surprises.
func (r *Root) Find(name string, limit int) ([]FileInfo, error) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return nil, fmt.Errorf("workspace: find name required")
	}
	if limit <= 0 {
		limit = findDefaultLimit
	}
	if limit > findMaxLimit {
		limit = findMaxLimit
	}
	matches := make([]FileInfo, 0)
	rootFS := r.root.FS()
	err := fs.WalkDir(rootFS, ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		rel = path.Clean(rel)
		if isSensitivePath(rel) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if strings.ToLower(path.Base(rel)) != needle {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		matches = append(matches, FileInfo{Path: rel, Type: kind, Size: info.Size()})
		if len(matches) >= limit {
			return errStopWalk
		}
		return nil
	})
	if errors.Is(err, errStopWalk) {
		err = nil
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Path < matches[j].Path })
	return matches, err
}
