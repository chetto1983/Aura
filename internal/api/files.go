// files.go — multi-root file manager API.
//
// The dashboard exposes four named roots so the operator can browse and edit
// Aura's persistent state without shell access. Each root maps to one
// configured directory:
//
//   wiki       → cfg.WikiPath            (LLM-curated markdown pages)
//   sources    → cfg.WikiPath/raw        (immutable ingested PDFs/text)
//   workspace  → cfg.WorkspaceRoot       (LLM-writable scratch space)
//   skills     → cfg.SkillsPath          (installed skill bundles)
//
// Write/delete on the wiki and sources roots cascades to Aura's vector index
// so the LLM cannot retrieve stale embeddings of a renamed or deleted file.
// The other two roots are pure CRUD — the operator explicitly opted out of
// indexing them.
//
// Path safety: every operation goes through resolveFileRoot, which uses the
// workspace.Root pattern (clean → join → re-Abs → insideRoot). Sensitive
// paths in wiki/.env-style files are rejected the same way the LLM-facing
// workspace_files tools reject them.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// fileEntry is one row in a directory listing.
type fileEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "file" | "dir"
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
}

// fileReadResponse is the body returned by GET /files/{root}/file.
type fileReadResponse struct {
	Root     string `json:"root"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Encoding string `json:"encoding"` // "utf-8" | "base64"
	Content  string `json:"content"`
}

// fileWriteRequest is the body accepted by PUT /files/{root}/file.
type fileWriteRequest struct {
	Encoding string `json:"encoding"` // "utf-8" (default) or "base64"
	Content  string `json:"content"`
}

// fileMoveRequest is the body accepted by POST /files/{root}/rename.
type fileMoveRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// fileBulkDeleteRequest is the body accepted by POST /files/{root}/delete-many.
// Paths are root-relative (no leading slash); the handler resolves each via
// resolveFileRoot, exactly like the single-file delete. wiki .md files go
// through wiki.Store.DeletePage so the FTS5 + GraphIndex + Qdrant + git +
// TOC cascade fires on every page — equivalent to N single deletes but
// without 2N HTTP roundtrips.
type fileBulkDeleteRequest struct {
	Paths []string `json:"paths"`
}

// fileBulkDeleteResponse reports per-path success/failure so the frontend
// can render a "X of N deleted" toast and surface the failed paths.
type fileBulkDeleteResponse struct {
	Root    string                 `json:"root"`
	Deleted []string               `json:"deleted"`
	Failed  []fileBulkDeleteFailed `json:"failed,omitempty"`
}

type fileBulkDeleteFailed struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// fileBulkDeleteMax caps how many paths a single bulk-delete request may
// reference. Set high enough to cover the 876-row-pages cleanup case but
// low enough that a runaway client can't tie up the server with a
// pathological payload.
const fileBulkDeleteMax = 2000

// fileMaxBytes caps the response body for GET /files/{root}/file. Larger
// files are truncated and the dashboard surfaces a "truncated" warning.
const fileMaxBytes = 1 << 20 // 1 MiB

// WikiReindexer is the optional hook called when a wiki file is written or
// deleted. Production wires search.Repository.ReindexWikiPage so the LLM's
// vector index follows operator edits in real time. When nil, the index
// stays stale until the next bot restart.
type WikiReindexer interface {
	ReindexWikiPage(ctx context.Context, slug string) error
}

// fileRootResolver looks up the on-disk base directory for a root name. It
// is a method on Deps so handlers can stay package-level functions.
func (d Deps) fileRootResolver(name string) (string, error) {
	switch strings.TrimSpace(name) {
	case "wiki":
		if d.WikiDir == "" {
			return "", errors.New("wiki root not configured")
		}
		return d.WikiDir, nil
	case "sources":
		if d.SourcesDir != "" {
			return d.SourcesDir, nil // Phase-FS-LAYOUT: split from wiki/raw
		}
		if d.WikiDir == "" {
			return "", errors.New("sources root not configured")
		}
		return filepath.Join(d.WikiDir, "raw"), nil // legacy fallback
	case "workspace":
		if d.WorkspaceDir == "" {
			return "", errors.New("workspace root not configured")
		}
		return d.WorkspaceDir, nil
	case "skills":
		if d.SkillsDir == "" {
			return "", errors.New("skills root not configured")
		}
		return d.SkillsDir, nil
	default:
		return "", fmt.Errorf("unknown root %q", name)
	}
}

// resolveFileRoot validates a (root, path) pair and returns an absolute,
// containment-checked filesystem path. Path traversal attempts (.., absolute
// paths, symlink escape) return an error and never touch disk.
func resolveFileRoot(deps Deps, root, rel string) (absDir, absPath string, err error) {
	absDir, err = deps.fileRootResolver(root)
	if err != nil {
		return "", "", err
	}
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." {
		return absDir, absDir, nil
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return "", "", errors.New("path must be relative and stay inside the root")
	}
	abs := filepath.Join(absDir, filepath.FromSlash(rel))
	resolved, absErr := filepath.Abs(abs)
	if absErr != nil {
		return "", "", fmt.Errorf("resolve path: %w", absErr)
	}
	dirAbs, dirErr := filepath.Abs(absDir)
	if dirErr != nil {
		return "", "", fmt.Errorf("resolve root: %w", dirErr)
	}
	if !strings.HasPrefix(resolved+string(filepath.Separator), dirAbs+string(filepath.Separator)) && resolved != dirAbs {
		return "", "", errors.New("path escapes the root")
	}
	return dirAbs, resolved, nil
}

// fileTreeMaxEntries caps recursive walks so a runaway root (think wiki
// with 5000 pages) can't blow up the dashboard.
const fileTreeMaxEntries = 5000

// handleFilesList lists one directory entry-by-entry. Files come back with
// size + mtime; sub-directories carry no size.
//
// When ?recursive=true is set the response is a flat list of every entry
// under the requested path, with the "name" field carrying a forward-slash
// path relative to the root (e.g. "wiki/notes/idea.md"). svar's React
// FileManager consumes this shape directly.
func handleFilesList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		rootAbs, abs, err := resolveFileRoot(deps, root, r.URL.Query().Get("path"))
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		info, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "path not found")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		if !info.IsDir() {
			writeError(w, deps.Logger, http.StatusBadRequest, "path is a file; use GET /files/{root}/file")
			return
		}
		recursive := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("recursive")), "true")
		if recursive {
			out, err := walkFileTree(rootAbs, abs, fileTreeMaxEntries)
			if err != nil {
				writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, deps.Logger, http.StatusOK, out)
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]fileEntry, 0, len(entries))
		for _, e := range entries {
			name := e.Name()
			if isHiddenFileEntry(name) {
				continue
			}
			row := fileEntry{Name: name, Type: "file"}
			if e.IsDir() {
				row.Type = "dir"
				out = append(out, row)
				continue
			}
			fi, statErr := e.Info()
			if statErr == nil {
				row.Size = fi.Size()
				row.Modified = fi.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
			out = append(out, row)
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Type != out[j].Type {
				return out[i].Type == "dir"
			}
			return out[i].Name < out[j].Name
		})
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

// walkFileTree returns a flat list of entries under base. "name" carries the
// forward-slash path relative to rootAbs so the dashboard can use it as an
// id. Hidden entries are skipped at the entry level; entire hidden subtrees
// are pruned so their contents never appear either.
func walkFileTree(rootAbs, base string, maxEntries int) ([]fileEntry, error) {
	out := make([]fileEntry, 0, 64)
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == base {
			return nil
		}
		name := d.Name()
		if isHiddenFileEntry(name) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		row := fileEntry{Name: filepath.ToSlash(rel), Type: "file"}
		if d.IsDir() {
			row.Type = "dir"
		} else {
			info, statErr := d.Info()
			if statErr == nil {
				row.Size = info.Size()
				row.Modified = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
		}
		out = append(out, row)
		if len(out) >= maxEntries {
			return io.EOF // signal a stop without returning an error to caller
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// handleFilesRead returns a single file's content. utf8.Valid bodies come
// back as UTF-8; non-text bodies as base64. Files larger than fileMaxBytes
// are truncated.
func handleFilesRead(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		rel := r.URL.Query().Get("path")
		_, abs, err := resolveFileRoot(deps, root, rel)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		info, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "file not found")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		if info.IsDir() {
			writeError(w, deps.Logger, http.StatusBadRequest, "path is a directory; use GET /files/{root}/tree")
			return
		}
		f, err := os.Open(abs)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, fileMaxBytes+1))
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		if int64(len(data)) > fileMaxBytes {
			data = data[:fileMaxBytes]
		}
		resp := fileReadResponse{
			Root:     root,
			Path:     filepath.ToSlash(rel),
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		}
		if utf8.Valid(data) {
			resp.Encoding = "utf-8"
			resp.Content = string(data)
		} else {
			resp.Encoding = "base64"
			resp.Content = base64.StdEncoding.EncodeToString(data)
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

// handleFilesWrite creates or replaces a file. wiki writes trigger a
// Qdrant reindex of the affected slug.
func handleFilesWrite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		rel := r.URL.Query().Get("path")
		_, abs, err := resolveFileRoot(deps, root, rel)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		var body fileWriteRequest
		if err := decodeJSONBody(r, &body); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		bytes, err := decodeFileContent(body)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.WriteFile(abs, bytes, 0o644); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		reindexWikiIfApplicable(r.Context(), deps, root, rel)
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"root": root, "path": filepath.ToSlash(rel), "size": len(bytes)})
	}
}

// handleFilesDelete removes a file or empty directory. Wiki .md deletes
// cascade through Wiki.DeletePage; sources hold immutable PDFs that must
// go through DELETE /sources/{id} (memoryindex cascade); everything else
// is plain os.Remove[All]. See deleteOneFile for the full routing.
func handleFilesDelete(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		rel := r.URL.Query().Get("path")
		if err := deleteOneFile(r.Context(), deps, root, rel); err != nil {
			msg := err.Error()
			status := http.StatusInternalServerError
			switch {
			case strings.HasPrefix(msg, "path not found"):
				status = http.StatusNotFound
			case strings.HasPrefix(msg, "refusing to delete"),
				strings.HasPrefix(msg, "delete a source via DELETE"),
				strings.Contains(msg, "invalid root"):
				status = http.StatusBadRequest
			}
			writeError(w, deps.Logger, status, msg)
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{
			"root":    root,
			"path":    filepath.ToSlash(rel),
			"deleted": true,
		})
	}
}

// deleteOneFile is the per-path delete used by both DELETE /files/{root}/file
// (single) and POST /files/{root}/delete-many (bulk). Routes wiki .md pages
// through Wiki.DeletePage to keep FTS5 / GraphIndex / Qdrant / git / TOC
// in sync. Other paths use os.Remove[All]. Returns a non-nil error on any
// failure; the caller decides whether to abort or continue.
func deleteOneFile(ctx context.Context, deps Deps, root, rel string) error {
	dirAbs, abs, err := resolveFileRoot(deps, root, rel)
	if err != nil {
		return err
	}
	if abs == dirAbs {
		return fmt.Errorf("refusing to delete the root itself")
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("path not found: %s", rel)
		}
		return err
	}
	if info.IsDir() && root == "sources" {
		return fmt.Errorf("delete a source via DELETE /sources/{id} to keep the memoryindex consistent")
	}
	if !info.IsDir() && root == "wiki" && deps.Wiki != nil && strings.HasSuffix(rel, ".md") && !strings.Contains(filepath.ToSlash(rel), "/") {
		slug := strings.TrimSuffix(rel, ".md")
		return deps.Wiki.DeletePage(ctx, slug)
	}
	if info.IsDir() {
		if err := os.RemoveAll(abs); err != nil {
			return err
		}
	} else {
		if err := os.Remove(abs); err != nil {
			return err
		}
	}
	reindexWikiIfApplicable(ctx, deps, root, rel)
	return nil
}

// handleFilesBulkDelete accepts a list of root-relative paths and deletes
// each via deleteOneFile. Best-effort: a single failure does NOT abort
// the rest; the response separates successes from failures so the frontend
// can render "X / N deleted; Y failed: ..." accurately. Capped at
// fileBulkDeleteMax paths to bound work per request.
func handleFilesBulkDelete(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		var body fileBulkDeleteRequest
		if err := decodeJSONBody(r, &body); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		if len(body.Paths) == 0 {
			writeError(w, deps.Logger, http.StatusBadRequest, "paths must contain at least one entry")
			return
		}
		if len(body.Paths) > fileBulkDeleteMax {
			writeError(w, deps.Logger, http.StatusBadRequest,
				fmt.Sprintf("too many paths: %d (max %d)", len(body.Paths), fileBulkDeleteMax))
			return
		}
		resp := fileBulkDeleteResponse{Root: root, Deleted: []string{}}
		for _, rel := range body.Paths {
			if err := deleteOneFile(r.Context(), deps, root, rel); err != nil {
				deps.Logger.Warn("files: bulk delete failed", "root", root, "path", rel, "error", err)
				resp.Failed = append(resp.Failed, fileBulkDeleteFailed{
					Path:  filepath.ToSlash(rel),
					Error: err.Error(),
				})
				continue
			}
			resp.Deleted = append(resp.Deleted, filepath.ToSlash(rel))
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

// handleFilesMkdir creates a directory tree (mkdir -p).
func handleFilesMkdir(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		rel := r.URL.Query().Get("path")
		_, abs, err := resolveFileRoot(deps, root, rel)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"root": root, "path": filepath.ToSlash(rel), "created": true})
	}
}

// handleFilesRename moves a file/dir within one root. Cross-root moves are
// rejected — the operator can read+write across roots if they really need to.
func handleFilesRename(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		root := r.PathValue("root")
		var body fileMoveRequest
		if err := decodeJSONBody(r, &body); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		_, fromAbs, err := resolveFileRoot(deps, root, body.From)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "from: "+err.Error())
			return
		}
		_, toAbs, err := resolveFileRoot(deps, root, body.To)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "to: "+err.Error())
			return
		}
		if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.Rename(fromAbs, toAbs); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		reindexWikiIfApplicable(r.Context(), deps, root, body.From)
		reindexWikiIfApplicable(r.Context(), deps, root, body.To)
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"root": root, "from": body.From, "to": body.To})
	}
}

func decodeFileContent(req fileWriteRequest) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(req.Encoding)) {
	case "", "utf-8", "utf8":
		return []byte(req.Content), nil
	case "base64":
		out, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 content: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown encoding %q", req.Encoding)
	}
}

// reindexWikiIfApplicable pokes the wiki search engine for a slug change.
// Only wiki root + ".md" files trigger the reindex; everything else is a
// no-op. Failures are logged, not surfaced — the file write already
// succeeded, and ReindexWikiPage is best-effort.
func reindexWikiIfApplicable(ctx context.Context, deps Deps, root, rel string) {
	if deps.WikiSearch == nil || root != "wiki" {
		return
	}
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || !strings.HasSuffix(rel, ".md") {
		return
	}
	// Top-level page only; nested wiki paths (e.g. raw/*) are handled by
	// other code paths (source ingest, etc.) and shouldn't loop through
	// here.
	if strings.Contains(rel, "/") {
		return
	}
	slug := strings.TrimSuffix(rel, ".md")
	if slug == "" {
		return
	}
	if err := deps.WikiSearch.ReindexWikiPage(ctx, slug); err != nil {
		deps.Logger.Warn("files: wiki reindex failed", "slug", slug, "error", err)
	}
}

func isHiddenFileEntry(name string) bool {
	// Hide UNIX dotfiles + a few Aura-internal artifacts that the dashboard
	// shouldn't expose. The LLM workspace tools already enforce the same
	// list via internal/workspace/sensitive.go for write access; we just
	// mirror the read-side view here.
	switch name {
	case ".env", ".git", ".gitignore", ".aura":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// no-op import shadow to keep the JSON import even when handlers are
// edge-cased; removing this drops the dependency cleanly.
var _ = json.Unmarshal
