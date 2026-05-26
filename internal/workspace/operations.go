package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type PathInfo struct {
	Path     string `json:"path"`
	Name     string `json:"name,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Ext      string `json:"ext,omitempty"`
	Type     string `json:"type"` // file | dir | missing
	Exists   bool   `json:"exists"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
}

type TreeNode struct {
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	Type      string     `json:"type"` // file | dir
	Size      int64      `json:"size,omitempty"`
	Children  []TreeNode `json:"children,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
}

// WriteValidator lets outer layers enforce content-specific rules (wiki page
// schema, skill frontmatter, server-managed files) before workspace copy/move
// operations publish bytes at a destination path.
type WriteValidator func(rel string, content []byte) error

func (r *Root) Info(rel string) (PathInfo, error) {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return PathInfo{}, err
	}
	info := PathInfo{
		Path: cleanRel,
		Name: path.Base(cleanRel),
		Dir:  path.Dir(cleanRel),
		Ext:  path.Ext(cleanRel),
		Type: "missing",
	}
	if cleanRel == "." {
		info.Name = "."
		info.Dir = "."
	}
	stat, err := r.root.Stat(cleanRel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return info, nil
		}
		return PathInfo{}, fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	info.Exists = true
	info.Size = stat.Size()
	info.Modified = stat.ModTime().UTC().Format(time.RFC3339)
	if stat.IsDir() {
		info.Type = "dir"
	} else {
		info.Type = "file"
	}
	return info, nil
}

func (r *Root) Mkdir(rel string) (FileInfo, error) {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return FileInfo{}, err
	}
	if cleanRel == "." {
		return FileInfo{Path: cleanRel, Type: "dir"}, nil
	}
	if err := r.root.MkdirAll(cleanRel, 0o755); err != nil {
		return FileInfo{}, fmt.Errorf("workspace: mkdir %s: %w", cleanRel, err)
	}
	return FileInfo{Path: cleanRel, Type: "dir"}, nil
}

func (r *Root) RemoveFile(rel string) error {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return err
	}
	info, err := r.root.Stat(cleanRel)
	if err != nil {
		return fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	if info.IsDir() {
		return fmt.Errorf("workspace: %s is a directory", cleanRel)
	}
	if err := r.root.Remove(cleanRel); err != nil {
		return fmt.Errorf("workspace: remove file %s: %w", cleanRel, err)
	}
	return nil
}

func (r *Root) RemoveDir(rel string) error {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return err
	}
	if cleanRel == "." {
		return fmt.Errorf("workspace: refusing to remove workspace root")
	}
	info, err := r.root.Stat(cleanRel)
	if err != nil {
		return fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace: %s is not a directory", cleanRel)
	}
	if err := r.root.Remove(cleanRel); err != nil {
		return fmt.Errorf("workspace: remove directory %s: %w", cleanRel, err)
	}
	return nil
}

func (r *Root) Move(srcRel, dstRel string, validate WriteValidator) (FileInfo, error) {
	src, dst, info, err := r.resolveTransfer(srcRel, dstRel)
	if err != nil {
		return FileInfo{}, err
	}
	if err := validateTransferSource(r, src, dst, info.IsDir(), validate); err != nil {
		return FileInfo{}, err
	}
	if err := r.ensureParent(dst); err != nil {
		return FileInfo{}, err
	}
	if err := r.root.Rename(src, dst); err != nil {
		return FileInfo{}, fmt.Errorf("workspace: move %s to %s: %w", src, dst, err)
	}
	return fileInfoForPath(dst, info), nil
}

func (r *Root) Copy(srcRel, dstRel string, validate WriteValidator) (FileInfo, error) {
	src, dst, info, err := r.resolveTransfer(srcRel, dstRel)
	if err != nil {
		return FileInfo{}, err
	}
	if err := validateTransferSource(r, src, dst, info.IsDir(), validate); err != nil {
		return FileInfo{}, err
	}
	if info.IsDir() {
		if err := r.copyDir(src, dst, validate); err != nil {
			return FileInfo{}, err
		}
		return FileInfo{Path: dst, Type: "dir"}, nil
	}
	data, err := r.Read(src, searchFileLimit)
	if err != nil {
		return FileInfo{}, err
	}
	if validate != nil {
		if err := validate(dst, data); err != nil {
			return FileInfo{}, err
		}
	}
	if err := r.WriteAtomic(dst, data); err != nil {
		return FileInfo{}, fmt.Errorf("workspace: copy %s to %s: %w", src, dst, err)
	}
	return FileInfo{Path: dst, Type: "file", Size: int64(len(data))}, nil
}

func (r *Root) Walk(rel string, limit int) (TreeNode, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return TreeNode{}, err
	}
	remaining := limit
	return r.walkNode(cleanRel, &remaining)
}

func (r *Root) Grep(rel, searchText string, caseInsensitive bool, limit int) ([]SearchMatch, error) {
	searchText = strings.TrimSpace(searchText)
	if searchText == "" {
		return nil, fmt.Errorf("workspace: search_text required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return nil, err
	}
	data, err := r.Read(cleanRel, searchFileLimit)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("workspace: %s is not UTF-8 text", cleanRel)
	}
	needle := searchText
	if caseInsensitive {
		needle = strings.ToLower(needle)
	}
	lines := strings.Split(string(data), "\n")
	matches := make([]SearchMatch, 0)
	for i, line := range lines {
		haystack := line
		if caseInsensitive {
			haystack = strings.ToLower(haystack)
		}
		col := strings.Index(haystack, needle)
		if col < 0 {
			continue
		}
		matches = append(matches, SearchMatch{
			Path:   cleanRel,
			Line:   i + 1,
			Column: col + 1,
			Text:   strings.TrimRight(line, "\r"),
		})
		if len(matches) >= limit {
			break
		}
	}
	return matches, nil
}

func (r *Root) resolveTransfer(srcRel, dstRel string) (string, string, fs.FileInfo, error) {
	src, err := r.resolveRel(srcRel)
	if err != nil {
		return "", "", nil, err
	}
	dst, err := r.resolveRel(dstRel)
	if err != nil {
		return "", "", nil, err
	}
	if src == "." {
		return "", "", nil, fmt.Errorf("workspace: refusing to transfer workspace root")
	}
	info, err := r.root.Stat(src)
	if err != nil {
		return "", "", nil, fmt.Errorf("workspace: stat source %s: %w", src, err)
	}
	if existing, err := r.root.Stat(dst); err == nil && existing.IsDir() {
		dst = path.Join(dst, path.Base(src))
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", "", nil, fmt.Errorf("workspace: stat destination %s: %w", dst, err)
	}
	if dst == "." || dst == src || strings.HasPrefix(dst, src+"/") {
		return "", "", nil, fmt.Errorf("workspace: invalid destination %s for source %s", dst, src)
	}
	if _, err := r.root.Stat(dst); err == nil {
		return "", "", nil, fmt.Errorf("workspace: destination already exists: %s", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", nil, fmt.Errorf("workspace: stat destination %s: %w", dst, err)
	}
	return src, dst, info, nil
}

func validateTransferSource(r *Root, src, dst string, isDir bool, validate WriteValidator) error {
	if validate == nil {
		return nil
	}
	if !isDir {
		data, err := r.Read(src, searchFileLimit)
		if err != nil {
			return err
		}
		return validate(dst, data)
	}
	return fs.WalkDir(r.root.FS(), src, func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel = path.Clean(rel)
		if isSensitivePath(rel) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		childDst := path.Join(dst, strings.TrimPrefix(rel, src+"/"))
		data, err := r.Read(rel, searchFileLimit)
		if err != nil {
			return err
		}
		return validate(childDst, data)
	})
}

func (r *Root) copyDir(src, dst string, validate WriteValidator) error {
	if err := r.root.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("workspace: mkdir %s: %w", dst, err)
	}
	return fs.WalkDir(r.root.FS(), src, func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel = path.Clean(rel)
		if isSensitivePath(rel) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if rel == src {
			return nil
		}
		dstRel := path.Join(dst, strings.TrimPrefix(rel, src+"/"))
		if entry.IsDir() {
			return r.root.MkdirAll(dstRel, 0o755)
		}
		data, err := r.Read(rel, searchFileLimit)
		if err != nil {
			return err
		}
		if validate != nil {
			if err := validate(dstRel, data); err != nil {
				return err
			}
		}
		return r.WriteAtomic(dstRel, data)
	})
}

func (r *Root) ensureParent(rel string) error {
	parent := path.Dir(rel)
	if parent == "" || parent == "." {
		return nil
	}
	if err := r.root.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("workspace: mkdir %s: %w", parent, err)
	}
	return nil
}

func fileInfoForPath(rel string, info fs.FileInfo) FileInfo {
	kind := "file"
	if info.IsDir() {
		kind = "dir"
	}
	return FileInfo{Path: rel, Type: kind, Size: info.Size()}
}

func (r *Root) walkNode(rel string, remaining *int) (TreeNode, error) {
	info, err := r.root.Stat(rel)
	if err != nil {
		return TreeNode{}, fmt.Errorf("workspace: stat %s: %w", rel, err)
	}
	node := TreeNode{
		Path: rel,
		Name: path.Base(rel),
		Type: "file",
		Size: info.Size(),
	}
	if rel == "." {
		node.Name = "."
	}
	if !info.IsDir() {
		return node, nil
	}
	node.Type = "dir"
	node.Size = 0
	dir, err := r.root.Open(rel)
	if err != nil {
		return TreeNode{}, fmt.Errorf("workspace: open dir %s: %w", rel, err)
	}
	defer func() {
		_ = dir.Close()
	}()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return TreeNode{}, fmt.Errorf("workspace: list %s: %w", rel, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if *remaining <= 0 {
			node.Truncated = true
			break
		}
		child := path.Join(rel, entry.Name())
		if rel == "." {
			child = entry.Name()
		}
		child = path.Clean(child)
		if isSensitivePath(child) {
			continue
		}
		*remaining = *remaining - 1
		childNode, err := r.walkNode(child, remaining)
		if err != nil {
			continue
		}
		node.Children = append(node.Children, childNode)
	}
	return node, nil
}
