// Package workspace bounds Aura's filesystem-facing tools to one directory.
//
// Containment model — two layers, defense-in-depth:
//
//  1. Kernel layer: every filesystem operation goes through *os.Root
//     (Go 1.24+). The kernel uses openat(AT_BENEATH) under the hood,
//     which atomically refuses to follow any path component that would
//     escape the root — including symlinks pointed at /etc/passwd or
//     ../ tricks inside a symlink target. This defeats symlink-escape
//     and TOCTOU races that pure userspace path-validation cannot.
//
//  2. Application layer (Aura-specific): the kernel only knows where the
//     workspace ENDS, not which paths inside it are sensitive. Even
//     though /workspace/.env or /workspace/data/aura.db live "inside"
//     the root, an LLM under prompt injection must not be able to read
//     them. isSensitivePath enforces a denylist that the kernel can't.
//     Same logic blocks writes to binary/executable extensions.
//
// Picobot uses (1) only; pure VM-isolated coding agents can afford that
// because every file in the workspace is fair game. Aura's workspace is
// shared with the user (wiki, AGENT.md, sources, sometimes data/) so
// (2) is load-bearing for the prompt-injection threat model.
package workspace

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
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
// Internally backed by *os.Root for kernel-enforced containment.
type Root struct {
	abs  string // absolute path used at OpenRoot time (returned by Path)
	root *os.Root
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

// New opens an os.Root anchored at the supplied directory. Caller is
// expected to call Close() at shutdown to release the FD.
func New(root string) (*Root, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("workspace: root required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve root: %w", err)
	}
	abs = filepath.Clean(abs)
	osRoot, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: open root: %w", err)
	}
	return &Root{abs: abs, root: osRoot}, nil
}

// Close releases the underlying *os.Root file descriptor. After Close,
// every method returns an error.
func (r *Root) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.Close()
}

// Path returns the absolute directory the Root was opened at.
func (r *Root) Path() string {
	if r == nil {
		return ""
	}
	return r.abs
}

// Resolve validates rel and returns the corresponding ABSOLUTE path
// on disk for callers that need it. Inside the package we always
// prefer resolveRel which returns just the cleanRel — passing a
// cleanRel to *os.Root methods keeps every syscall kernel-anchored.
// Resolve stays exported for callers and tests that need the absolute
// form.
func (r *Root) Resolve(rel string) (string, error) {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.abs, filepath.FromSlash(cleanRel)), nil
}

// resolveRel validates rel and returns a clean relative path safe to
// pass to *os.Root methods. The kernel will refuse anything that
// escapes the root anyway; we only enforce the Aura-specific layer
// (sensitive-path denylist) here.
func (r *Root) resolveRel(rel string) (string, error) {
	if r == nil || r.root == nil {
		return "", fmt.Errorf("workspace: root unavailable")
	}
	cleanRel, err := cleanRelative(rel)
	if err != nil {
		return "", err
	}
	if isSensitivePath(cleanRel) {
		return "", fmt.Errorf("%w: %s", ErrDeniedPath, cleanRel)
	}
	return cleanRel, nil
}

// Read returns the file's bytes, capped at maxBytes. Errors on directory
// targets, missing files, sensitive paths, or oversize binary files.
// Use ReadBest for the permissive variant that handles all three.
func (r *Root) Read(rel string, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultReadLimit
	}
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return nil, err
	}
	info, err := r.root.Stat(cleanRel)
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
	f, err := r.root.Open(cleanRel)
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

// ReadResult is what tool-facing reads return. It encodes the three
// cases — file fits, file truncated, target is a directory — so
// callers never need to interpret a stat error.
type ReadResult struct {
	Path        string     `json:"path"`
	Type        string     `json:"type"` // "file" | "directory"
	Bytes       int        `json:"bytes,omitempty"`
	TotalBytes  int64      `json:"total_bytes,omitempty"`
	Truncated   bool       `json:"truncated,omitempty"`
	Encoding    string     `json:"encoding,omitempty"`
	Content     string     `json:"content,omitempty"`
	Entries     []FileInfo `json:"entries,omitempty"`
	EntriesHint string     `json:"hint,omitempty"`
}

// ReadBest is the permissive read used by the read_file tool. It never
// errors on oversize (truncates) or on directories (lists). It only
// errors on missing paths, permission boundaries, or sensitive paths.
//
// Sibling to Read: Read stays strict for callers that must reject
// either case (hashing, atomic patch). ReadBest gives the LLM a single
// tool that always returns something useful and lets it decide next
// steps.
func (r *Root) ReadBest(rel string, maxBytes int) (ReadResult, error) {
	if maxBytes <= 0 {
		maxBytes = defaultReadLimit
	}
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return ReadResult{}, err
	}
	info, err := r.root.Stat(cleanRel)
	if err != nil {
		return ReadResult{}, fmt.Errorf("workspace: stat %s: %w", cleanRel, err)
	}
	if info.IsDir() {
		entries, err := r.List(cleanRel, 200)
		if err != nil {
			return ReadResult{}, fmt.Errorf("workspace: list %s: %w", cleanRel, err)
		}
		return ReadResult{
			Path:        cleanRel,
			Type:        "directory",
			Entries:     entries,
			EntriesHint: "Use list_files for paging or different limits; pass an entry's path back to read_file to read it.",
		}, nil
	}
	if isBinaryExtension(cleanRel) && info.Size() > smallBinaryReadLimit {
		return ReadResult{}, fmt.Errorf("%w: binary file too large for read-only access", ErrDeniedPath)
	}
	f, err := r.root.Open(cleanRel)
	if err != nil {
		return ReadResult{}, fmt.Errorf("workspace: open %s: %w", cleanRel, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return ReadResult{}, fmt.Errorf("workspace: read %s: %w", cleanRel, err)
	}
	truncated := len(data) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	result := ReadResult{
		Path:       cleanRel,
		Type:       "file",
		Bytes:      len(data),
		TotalBytes: info.Size(),
		Truncated:  truncated,
	}
	if utf8.Valid(data) {
		result.Encoding = "utf-8"
		result.Content = string(data)
	} else {
		result.Encoding = "base64"
		result.Content = base64.StdEncoding.EncodeToString(data)
	}
	return result, nil
}

// WriteAtomic writes content via a sibling temp file + rename. The temp
// file lives in the SAME directory as the target so the rename is
// atomic on the same filesystem. Both operations go through *os.Root,
// so the kernel refuses any traversal out of the root.
func (r *Root) WriteAtomic(rel string, content []byte) error {
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return err
	}
	if isBinaryExtension(cleanRel) {
		return fmt.Errorf("%w: binary or executable writes are disabled", ErrDeniedPath)
	}
	parent := path.Dir(cleanRel)
	if parent != "" && parent != "." {
		if err := r.root.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", parent, err)
		}
	}
	tmpRel, err := r.tempPath(parent)
	if err != nil {
		return fmt.Errorf("workspace: temp name: %w", err)
	}
	tmp, err := r.root.OpenFile(tmpRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("workspace: temp file %s: %w", cleanRel, err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = r.root.Remove(tmpRel)
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
	if err := r.root.Rename(tmpRel, cleanRel); err != nil {
		return fmt.Errorf("workspace: rename %s: %w", cleanRel, err)
	}
	removeTmp = false
	return nil
}

// tempPath returns a sibling temp path with a random suffix, scoped to
// the same directory as the target so the eventual rename is atomic.
func (r *Root) tempPath(parent string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	name := ".aura-tmp-" + hex.EncodeToString(buf[:])
	if parent == "" || parent == "." {
		return name, nil
	}
	return path.Join(parent, name), nil
}

// Search performs a case-insensitive substring scan over text files
// inside the root. Sensitive paths, binary extensions, and files
// larger than searchFileLimit are skipped. The walk uses *os.Root's
// fs.FS so each open is kernel-anchored — no symlink escape risk.
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
	rootFS := r.root.FS()
	err := fs.WalkDir(rootFS, ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		// fs.WalkDir uses forward slashes already; clean for consistency.
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
		if isBinaryExtension(rel) || !matchesAnyGlob(rel, globs) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > searchFileLimit {
			return nil
		}
		data, err := r.root.ReadFile(rel)
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

// List returns the entries under rel (default: root). Sensitive paths
// are filtered. Output is sorted by Path for stable LLM-facing output.
func (r *Root) List(rel string, limit int) ([]FileInfo, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	cleanRel, err := r.resolveRel(rel)
	if err != nil {
		return nil, err
	}
	dir, err := r.root.Open(cleanRel)
	if err != nil {
		return nil, fmt.Errorf("workspace: open dir %s: %w", cleanRel, err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("workspace: list %s: %w", cleanRel, err)
	}
	out := make([]FileInfo, 0, min(len(entries), limit))
	for _, entry := range entries {
		var child string
		if cleanRel == "." {
			child = entry.Name()
		} else {
			child = path.Join(cleanRel, entry.Name())
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

// cleanRelative normalizes user-supplied rel and rejects shapes that
// could traverse out of root in pure-string analysis. The *os.Root
// layer also refuses anything outside, so this is mostly a clean-input
// gate that produces predictable cleanRel strings for downstream use.
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

// isSensitivePath is the Aura-specific denylist layer on top of kernel
// containment. The kernel only knows where the workspace ENDS; this
// function knows which paths inside it must never reach the LLM.
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

// isBinaryExtension blocks reads/writes of binary or executable file
// types. Reads above smallBinaryReadLimit are refused; writes are
// always refused. Aura is a text agent; an LLM dropping a .exe into
// the workspace via a prompt injection is a credible threat.
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
