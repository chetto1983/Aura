package agui

import (
	"context"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
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

// FileAttrs is what the store knows about an object beyond its bytes.
type FileAttrs struct {
	MIMEType  string
	SizeBytes int64
}

// FileObjectOpener streams one object out of one identity's own bucket. The implementation
// MUST resolve the owner's own store before reading: this handler relies on that as its
// ownership gate, exactly as document_open does.
type FileObjectOpener interface {
	ReadObject(ctx context.Context, identityID, key string) (io.ReadCloser, FileAttrs, error)
}

// FileObjectWriter stores one object in one identity's own bucket, with the same ownership
// obligation as the opener above.
type FileObjectWriter interface {
	PutObject(ctx context.Context, identityID, key, mimeType string, size int64, body io.Reader) error
}

// SetFileBrowser wires the listing the file manager reads.
func (s *Server) SetFileBrowser(browser FileBrowser) { s.files = browser }

// SetFileOpener wires the byte stream behind a file manager download.
func (s *Server) SetFileOpener(opener FileObjectOpener) { s.fileObjects = opener }

// SetFileWriter wires the destination of a file manager upload.
func (s *Server) SetFileWriter(writer FileObjectWriter) { s.fileWrites = writer }

func (s *Server) registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+fileManagerBase+"/files", s.handleFileList)
	mux.HandleFunc("GET "+fileManagerBase+"/files/{path...}", s.handleFileList)
	mux.HandleFunc("GET "+fileManagerBase+"/direct", s.handleFileDirect)
	mux.HandleFunc("POST "+fileManagerBase+"/upload", s.handleFileUpload)
	s.registerFileWriteRoutes(mux)
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

// handleFileDirect streams one object of the caller's own bucket, inline for an open and as
// an attachment for a download — the component's own distinction.
//
// Inline is the interesting one, because these are user-supplied bytes on the cockpit's own
// origin: an uploaded .html rendered here would run with the operator's session. What makes
// it safe is Content-Security-Policy: sandbox, which drops the response into an opaque
// origin with no scripts, no forms and no same-origin access — so the document renders and
// can reach nothing. That is the same control GitHub applies to raw user content, and it is
// why the answer is a header rather than refusing to open files at all.
//
// nosniff stays regardless, so a mislabelled type is never upgraded by the browser's guess,
// and the read is scoped to the request context so a disconnect cancels it.
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
	// Aura's own storage is not addressable here. It is hidden from listings, and hiding
	// alone would leave it reachable by anyone who typed the path.
	if assets.IsReservedPath(key) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, attrs, err := s.fileObjects.ReadObject(r.Context(), identityID, key)
	if err != nil {
		// Not-found and not-owned collapse to the same 404 so the response cannot be used to
		// probe which keys exist in someone else's bucket (D-12 existence hiding).
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	defer func() { _ = body.Close() }()

	header := w.Header()
	header.Set("X-Content-Type-Options", "nosniff")
	if inlineDisposition(r) && inlineSafeMIME(attrs.MIMEType) {
		header.Set("Content-Type", attrs.MIMEType)
		header.Set("Content-Disposition", inlineContentDisposition(path.Base(key)))
	} else {
		header.Set("Content-Type", "application/octet-stream")
		header.Set("Content-Disposition", contentDisposition(path.Base(key)))
	}
	_, _ = io.Copy(w, body)
}

// handleFileUpload stores one uploaded file in the folder the caller is viewing.
//
// The bytes land at the browsed prefix and NOTHING else happens here -- no queue row, no
// pipeline kick. Ingestion is reconciliation-driven now: the sidecar watches the bucket, so
// a file that appears in it gets extracted, carded and indexed on its own. That is why this
// writes to the folder the user chose rather than to the asset service's own key space,
// which would have stored the file somewhere other than where the UI just showed it.
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if s.fileWrites == nil {
		http.Error(w, "file upload unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// The cap is enforced on the REQUEST, before anything is read, so an oversized body is
	// refused rather than streamed into the bucket and deleted afterwards.
	r.Body = http.MaxBytesReader(w, r.Body, config.DefaultAssetMaxDocumentBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "a multipart file field is required", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	name := uploadFileName(header.Filename)
	if name == "" {
		http.Error(w, "the file needs a usable name", http.StatusBadRequest)
		return
	}
	key := strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("id")), "/")
	if key != "" && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	key += name
	if assets.IsReservedPath(key) {
		http.Error(w, "that folder belongs to Aura's internal storage", http.StatusBadRequest)
		return
	}

	// The MIME type is the browser's claim about somebody's file. It is recorded so a
	// download can be honest about what was uploaded, and is never trusted on the way out --
	// handleFileDirect serves octet-stream regardless.
	if err := s.fileWrites.PutObject(
		r.Context(), identityID, key, header.Header.Get("Content-Type"), header.Size, file,
	); err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadGateway)
		return
	}
	writeJSON(w, fileEntry{ID: "/" + key, Type: "file", Size: header.Size})
}

// uploadFileName reduces a caller-supplied name to a safe single path component.
//
// The name becomes part of an object key, so a directory in it would let an upload land
// outside the folder the user is looking at. Returns "" when nothing usable survives, which
// the caller refuses rather than papering over with a generated name -- the user picked this
// file and deserves to be told the name is the problem.
func uploadFileName(raw string) string {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/"))
	if name == "." || name == ".." || name == "/" || strings.HasPrefix(name, ".") {
		return ""
	}
	return name
}

// inlineSafeMIME is the allowlist of types the browser may RENDER from this origin.
//
// It replaces `Content-Security-Policy: sandbox`, which was the wrong tool and was MEASURED
// to be: Chrome will not run its PDF viewer inside a sandboxed opaque origin, so opening a
// PDF answered "Download is starting" and left a blank tab. A control that makes the feature
// not work is not a control.
//
// The rule is now about what the bytes can DO, not where they run. A PDF, an image, plain
// text, audio and video are handled by the browser's own viewers and execute no script from
// this origin, so they open natively. HTML, SVG and XML do execute -- SVG in particular is a
// script carrier wearing an image's clothes -- so they are served as attachments, which is
// the stored-XSS case WEBART-03/D-10 is actually about. That is a handful of types nobody
// opens to read, against every type somebody does.
//
// nosniff rides both paths, so a mislabelled type is never upgraded by a guess.
func inlineSafeMIME(declared string) bool {
	mime := strings.ToLower(strings.TrimSpace(declared))
	if index := strings.IndexByte(mime, ';'); index >= 0 {
		mime = strings.TrimSpace(mime[:index])
	}
	if mime == "application/pdf" || mime == "text/plain" {
		return true
	}
	if mime == "image/svg+xml" {
		return false
	}
	for _, family := range []string{"image/", "audio/", "video/"} {
		if strings.HasPrefix(mime, family) {
			return true
		}
	}
	return false
}
