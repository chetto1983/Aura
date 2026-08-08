package agui

import (
	"context"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/assets"
)

// fileManagerBase is the mount point of the SVAR File Manager REST contract.
//
// The three routes below are that component's own dialect, taken from its official Go
// reference backend (svar-widgets/filemanager-backend-go, MIT) and confirmed against the
// live demo on 2026-08-08: it asks for GET /files, GET /files/<percent-encoded id> and
// GET /direct?id=. Speaking the component's dialect is the point — it means the cockpit
// mounts the real widget with no adapter of ours in between.
const fileManagerBase = "/api/filemanager"

// FileBrowser lists one identity's stored objects as folders and files.
type FileBrowser interface {
	List(ctx context.Context, identityID, prefix string, limit int) (assets.BrowseResult, error)
}

// FileObjectOpener streams one object out of one identity's own bucket. The implementation
// MUST resolve the owner's own store before reading: this handler relies on that as its
// ownership gate, exactly as document_open does.
type FileObjectOpener interface {
	OpenObject(ctx context.Context, identityID, key string) (io.ReadCloser, error)
}

// SetFileBrowser wires the listing the file manager reads.
func (s *Server) SetFileBrowser(browser FileBrowser) { s.files = browser }

// SetFileOpener wires the byte stream behind a file manager download.
func (s *Server) SetFileOpener(opener FileObjectOpener) { s.fileObjects = opener }

func (s *Server) registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+fileManagerBase+"/files", s.handleFileList)
	mux.HandleFunc("GET "+fileManagerBase+"/files/{path...}", s.handleFileList)
	mux.HandleFunc("GET "+fileManagerBase+"/direct", s.handleFileDirect)
}

// fileEntry is one row in the component's vocabulary.
//
// id is an absolute path and carries the whole hierarchy: the widget splits it on the last
// slash to derive the parent and the display name, so no separate name field is sent. A
// folder reports neither size nor date because a grouped S3 prefix is not an object and has
// neither — reporting a zero would be inventing one.
type fileEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
	Date string `json:"date,omitempty"`
	Lazy bool   `json:"lazy,omitempty"`
}

// handleFileList returns one folder of the caller's own bucket, root included.
//
// The identity comes from the authenticated principal and is never a parameter: a browser
// that took an identity from the query string would be one typo away from listing someone
// else's corpus, and the resolver behind the browser scopes the read by the owner's own
// credential rather than by a prefix this handler remembered to apply.
//
// It replaces GET /api/documents for the file manager. That route listed the catalog, which
// only ever had rows for uploaded documents — so a document reconciled from the bucket was
// invisible to the UI no matter that it was fully indexed and answerable.
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "file browser unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	result, err := s.files.List(r.Context(), identityID, r.PathValue("path"), 0)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, fileEntries(result))
}

// fileEntries turns one browse page into the component's entity list.
//
// Never nil: an empty folder must encode as [] rather than null, which the widget would
// treat as a load failure instead of as an empty folder.
func fileEntries(result assets.BrowseResult) []fileEntry {
	entries := make([]fileEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		id := "/" + result.Prefix + strings.TrimSuffix(entry.Key, "/")
		if entry.Folder {
			// Always lazy. A grouped prefix only exists because at least one object lives
			// under it, so a folder here is never empty and the widget's expand always has
			// something to show. The reference backend counts children to decide this; over
			// S3 that would be one extra round trip per row to learn what the delimiter
			// already told us.
			entries = append(entries, fileEntry{ID: id, Type: "folder", Lazy: true})
			continue
		}
		entries = append(entries, fileEntry{
			ID: id, Type: "file", Size: entry.SizeBytes, Date: browseDate(entry.ModifiedAt),
		})
	}
	return entries
}

// browseDate formats a timestamp for the widget's date parser.
//
// RFC 3339 rather than the reference backend's "2006-01-02 15:04:05": that layout carries no
// zone, so new Date() reads it as local time and every timestamp silently shifts by the
// viewer's offset. The widget parses either.
func browseDate(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// handleFileDirect streams one object of the caller's own bucket.
//
// ALWAYS an attachment, never inline. The reference backend serves inline unless asked to
// download, and the widget's "open" action relies on that — but these are user-supplied
// bytes served from the cockpit's own origin, so rendering them in-origin is the stored-XSS
// hazard WEBART-03/D-10 exists to prevent. The same three guards as the asset download
// apply: a neutral content type plus nosniff regardless of what the object claims to be, the
// file name carried through the injection-safe Content-Disposition encoder, and a read
// scoped to the request context so a client disconnect cancels it.
func (s *Server) handleFileDirect(w http.ResponseWriter, r *http.Request) {
	if s.fileObjects == nil {
		http.Error(w, "file browser unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	key := strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("id")), "/")
	if key == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	body, err := s.fileObjects.OpenObject(r.Context(), identityID, key)
	if err != nil {
		// Not-found and not-owned collapse to the same 404 so the response cannot be used to
		// probe which keys exist in someone else's bucket (D-12 existence hiding).
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	defer func() { _ = body.Close() }()

	header := w.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Disposition", contentDisposition(path.Base(key)))
	_, _ = io.Copy(w, body)
}
