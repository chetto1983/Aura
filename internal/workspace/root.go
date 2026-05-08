package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultReadLimit     = 64 * 1024
	smallBinaryReadLimit = 64 * 1024
	searchFileLimit      = 1024 * 1024
)

var (
	ErrOutsideRoot = errors.New("workspace: path outside root")
	ErrDeniedPath  = errors.New("workspace: path denied")
)

// Root is a workspace-bounded filesystem boundary for LLM tools.
type Root struct {
	root string
}

type FileInfo struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

type SearchMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

func New(root string) (*Root, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("workspace: root required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve root: %w", err)
	}
	return &Root{root: filepath.Clean(abs)}, nil
}

func (r *Root) Path() string {
	if r == nil {
		return ""
	}
	return r.root
}

func (r *Root) Resolve(rel string) (string, error) {
	if r == nil || strings.TrimSpace(r.root) == "" {
		return "", fmt.Errorf("workspace: root unavailable")
	}
	cleanRel, err := cleanRelative(rel)
	if err != nil {
		return "", err
	}
	if isSensitivePath(cleanRel) {
		return "", fmt.Errorf("%w: %s", ErrDeniedPath, cleanRel)
	}
	abs := filepath.Join(r.root, filepath.FromSlash(cleanRel))
	resolved, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve %q: %w", rel, err)
	}
	inside, err := insideRoot(r.root, resolved)
	if err != nil {
		return "", err
	}
	if !inside {
		return "", ErrOutsideRoot
	}
	return resolved, nil
}

func (r *Root) Read(rel string, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultReadLimit
	}
	cleanRel, err := cleanRelative(rel)
	if err != nil {
		return nil, err
	}
	abs, err := r.Resolve(cleanRel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("workspace: %s is a directory", cleanRel)
	}
	if isBinaryExtension(cleanRel) && info.Size() > smallBinaryReadLimit {
		return nil, fmt.Errorf("%w: binary file too large for read-only access", ErrDeniedPath)
	}
	if info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("workspace: %s is %d bytes, above max_bytes %d", cleanRel, info.Size(), maxBytes)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: open %s: %w", cleanRel, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("workspace: read %s: %w", cleanRel, err)
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("workspace: %s exceeded max_bytes %d", cleanRel, maxBytes)
	}
	return data, nil
}

func (r *Root) WriteAtomic(rel string, content []byte) error {
	cleanRel, err := cleanRelative(rel)
	if err != nil {
		return err
	}
	if isBinaryExtension(cleanRel) {
		return fmt.Errorf("%w: binary or executable writes are disabled", ErrDeniedPath)
	}
	abs, err := r.Resolve(cleanRel)
	if err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("workspace: mkdir %s: %w", cleanRel, err)
	}
	tmp, err := os.CreateTemp(parent, ".aura-*")
	if err != nil {
		return fmt.Errorf("workspace: temp file %s: %w", cleanRel, err)
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: write temp %s: %w", cleanRel, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: sync temp %s: %w", cleanRel, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workspace: close temp %s: %w", cleanRel, err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("workspace: rename %s: %w", cleanRel, err)
	}
	removeTmp = false
	return nil
}

func (r *Root) Search(pattern string, globs []string, limit int) ([]SearchMatch, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("workspace: search pattern required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	needle := strings.ToLower(pattern)
	matches := make([]SearchMatch, 0)
	err := filepath.WalkDir(r.root, func(abs string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if abs == r.root {
			return nil
		}
		rel, err := r.relSlash(abs)
		if err != nil {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if isSensitivePath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if isBinaryExtension(rel) || !matchesAnyGlob(rel, globs) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > searchFileLimit {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			col := strings.Index(strings.ToLower(line), needle)
			if col < 0 {
				continue
			}
			matches = append(matches, SearchMatch{
				Path:   rel,
				Line:   i + 1,
				Column: col + 1,
				Text:   strings.TrimRight(line, "\r"),
			})
			if len(matches) >= limit {
				return errStopWalk
			}
		}
		return nil
	})
	if errors.Is(err, errStopWalk) {
		err = nil
	}
	return matches, err
}

func (r *Root) List(rel string, limit int) ([]FileInfo, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	cleanRel, err := cleanRelative(rel)
	if err != nil {
		return nil, err
	}
	abs, err := r.Resolve(cleanRel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: list %s: %w", cleanRel, err)
	}
	out := make([]FileInfo, 0, min(len(entries), limit))
	for _, entry := range entries {
		child := path.Join(cleanRel, entry.Name())
		if cleanRel == "." {
			child = entry.Name()
		}
		child = path.Clean(strings.ReplaceAll(child, "\\", "/"))
		if isSensitivePath(child) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		out = append(out, FileInfo{Path: child, Type: kind, Size: info.Size()})
		if len(out) >= limit {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

var errStopWalk = errors.New("workspace: stop walk")

func cleanRelative(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		rel = "."
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if len(rel) >= 2 && rel[1] == ':' {
		return "", ErrOutsideRoot
	}
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", ErrOutsideRoot
	}
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrOutsideRoot
	}
	return clean, nil
}

func insideRoot(root, abs string) (bool, error) {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false, fmt.Errorf("workspace: containment check: %w", err)
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func (r *Root) relSlash(abs string) (string, error) {
	rel, err := filepath.Rel(r.root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", ErrOutsideRoot
	}
	return filepath.ToSlash(rel), nil
}

func isSensitivePath(rel string) bool {
	rel = strings.ToLower(path.Clean(strings.ReplaceAll(rel, "\\", "/")))
	switch rel {
	case ".env", "data/aura.db", "data/aura.db-wal", "data/aura.db-shm":
		return true
	}
	return rel == ".git" || strings.HasPrefix(rel, ".git/") ||
		rel == "wiki/raw" || strings.HasPrefix(rel, "wiki/raw/") ||
		rel == "data/secrets" || strings.HasPrefix(rel, "data/secrets/") ||
		rel == "docker/secrets" || strings.HasPrefix(rel, "docker/secrets/")
}

func isBinaryExtension(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".exe", ".dll", ".so", ".dylib", ".bin", ".dat",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico",
		".pdf", ".zip", ".tar", ".gz", ".7z", ".rar",
		".db", ".sqlite", ".sqlite3", ".wal", ".shm",
		".woff", ".woff2", ".ttf":
		return true
	default:
		return false
	}
}

func matchesAnyGlob(rel string, globs []string) bool {
	clean := cleanGlobs(globs)
	if len(clean) == 0 {
		return true
	}
	for _, glob := range clean {
		if matchOneGlob(rel, glob) {
			return true
		}
	}
	return false
}

func cleanGlobs(globs []string) []string {
	out := make([]string, 0, len(globs))
	for _, glob := range globs {
		glob = path.Clean(strings.ReplaceAll(strings.TrimSpace(glob), "\\", "/"))
		if glob != "." && glob != "" {
			out = append(out, glob)
		}
	}
	return out
}

func matchOneGlob(rel, glob string) bool {
	if ok, _ := path.Match(glob, rel); ok {
		return true
	}
	if !strings.Contains(glob, "/") {
		ok, _ := path.Match(glob, path.Base(rel))
		return ok
	}
	if strings.HasPrefix(glob, "**/") {
		return matchOneGlob(rel, strings.TrimPrefix(glob, "**/"))
	}
	if idx := strings.Index(glob, "/**/"); idx >= 0 {
		prefix := glob[:idx]
		suffix := glob[idx+4:]
		return strings.HasPrefix(rel, prefix+"/") && matchOneGlob(strings.TrimPrefix(rel, prefix+"/"), strings.TrimPrefix(suffix, "/"))
	}
	return false
}
