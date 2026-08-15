package agui

import (
	"context"
	"net/http"
	"path"
)

// The write half of the SVAR File Manager REST contract, matching its official Go reference
// backend (svar-widgets/filemanager-backend-go, MIT) verb for verb and payload for payload:
//
//	POST   /files/{parent}  {name, type}                  create a file or folder
//	PUT    /files/{id}      {operation:"rename", name}    rename in place
//	PUT    /files           {operation:"move"|"copy", ids, target}
//	DELETE /files           {ids}
//
// Matching it exactly is what lets the cockpit drive all of this through the component's own
// RestDataProvider, with no request-building code of ours in between.

// FileOperations is the store-side of those verbs. It is the write counterpart of
// FileBrowser and carries the same obligation: resolve the OWNER's bucket first, so a key
// from somebody else's corpus cannot be reached even when a caller supplies one.
type FileOperations interface {
	Create(ctx context.Context, identityID, parent, name, kind string) (string, error)
	Rename(ctx context.Context, identityID, id, name string) (string, error)
	Move(ctx context.Context, identityID string, ids []string, target string) ([]string, error)
	Copy(ctx context.Context, identityID string, ids []string, target string) ([]string, error)
	Delete(ctx context.Context, identityID string, ids []string) error
}

// SetFileOperations wires create/rename/move/copy/delete.
func (s *Server) SetFileOperations(ops FileOperations) { s.fileOps = ops }

func (s *Server) registerFileWriteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+fileManagerBase+"/files/{path...}", s.handleFileCreate)
	mux.HandleFunc("PUT "+fileManagerBase+"/files/{path...}", s.handleFileRename)
	mux.HandleFunc("PUT "+fileManagerBase+"/files", s.handleFileBulk)
	mux.HandleFunc("DELETE "+fileManagerBase+"/files", s.handleFileDelete)
}

// fileResult is the component's success envelope. It reads result.id back to reconcile the
// row it already drew optimistically -- so when the store had to pick a different name, the
// UI follows the store rather than the two silently disagreeing.
type fileResult struct {
	Error  string      `json:"error,omitempty"`
	Result *fileRecord `json:"result,omitempty"`
}

type fileResults struct {
	Error  string       `json:"error,omitempty"`
	Result []fileRecord `json:"result,omitempty"`
}

type fileRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// fileMutation is every field the component sends on a write, mirroring the reference
// backend's FileUpdate. One struct for both PUT shapes because the decoder rejects unknown
// fields, so a payload naming something that is not here is a client bug worth surfacing.
type fileMutation struct {
	Operation string   `json:"operation"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Target    string   `json:"target"`
	IDs       []string `json:"ids"`
}

func (s *Server) fileWriteContext(w http.ResponseWriter, r *http.Request) (string, fileMutation, bool) {
	if s.fileOps == nil {
		http.Error(w, "file operations unavailable", http.StatusServiceUnavailable)
		return "", fileMutation{}, false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", fileMutation{}, false
	}
	// Gated like every other write on this server. The gate is the CSRF floor: a cross-origin
	// form can post text/plain or a urlencoded body with no preflight, but it cannot post
	// application/json, so refusing anything else is what makes the browser ask permission
	// before a page the operator did not open can rename or delete their files.
	//
	// It was dropped once, and the reason is worth naming so it is not traded away again:
	// RestDataProvider.send overrides Rest.sendRequest and loses its "application/json"
	// default, so the browser labelled every write text/plain and create, rename, move, copy
	// and delete all 400'd against the running cockpit while the listing kept working. The
	// label belongs on the request, not in a hole in the server — web/src/files/filesApi.ts
	// puts it back at the source.
	var body fileMutation
	if err := strictDecodeJSON(w, r, &body, decodeOpts{allowEmpty: true}); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return "", fileMutation{}, false
	}
	return identityID, body, true
}

func (s *Server) handleFileCreate(w http.ResponseWriter, r *http.Request) {
	identityID, body, ok := s.fileWriteContext(w, r)
	if !ok {
		return
	}
	id, err := s.fileOps.Create(r.Context(), identityID, r.PathValue("path"), body.Name, body.Type)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, fileResult{Error: sanitizeErr(err)})
		return
	}
	writeJSON(w, fileResult{Result: &fileRecord{ID: id, Name: path.Base(id)}})
}

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request) {
	identityID, body, ok := s.fileWriteContext(w, r)
	if !ok {
		return
	}
	if body.Operation != "rename" {
		http.Error(w, "operation must be rename", http.StatusBadRequest)
		return
	}
	id, err := s.fileOps.Rename(r.Context(), identityID, r.PathValue("path"), body.Name)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, fileResult{Error: sanitizeErr(err)})
		return
	}
	writeJSON(w, fileResult{Result: &fileRecord{ID: id, Name: path.Base(id)}})
}

// handleFileBulk is move and copy, which share a route and differ only by the operation
// field -- the component's own dialect, not a shape chosen here.
func (s *Server) handleFileBulk(w http.ResponseWriter, r *http.Request) {
	identityID, body, ok := s.fileWriteContext(w, r)
	if !ok {
		return
	}
	var (
		moved []string
		err   error
	)
	switch body.Operation {
	case "move":
		moved, err = s.fileOps.Move(r.Context(), identityID, body.IDs, body.Target)
	case "copy":
		moved, err = s.fileOps.Copy(r.Context(), identityID, body.IDs, body.Target)
	default:
		http.Error(w, "operation must be move or copy", http.StatusBadRequest)
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, fileResults{Error: sanitizeErr(err)})
		return
	}
	records := make([]fileRecord, 0, len(moved))
	for _, id := range moved {
		records = append(records, fileRecord{ID: id, Name: path.Base(id)})
	}
	writeJSON(w, fileResults{Result: records})
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	identityID, body, ok := s.fileWriteContext(w, r)
	if !ok {
		return
	}
	if len(body.IDs) == 0 {
		http.Error(w, "ids are required", http.StatusBadRequest)
		return
	}
	if err := s.fileOps.Delete(r.Context(), identityID, body.IDs); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, fileResults{Error: sanitizeErr(err)})
		return
	}
	writeJSON(w, fileResults{})
}

// inlineDisposition decides whether a download opens in the browser or lands on disk.
//
// The component asks for a download with ?download=true and an open without it, exactly as
// its reference backend defines. Serving inline is only safe because handleFileDirect sends
// Content-Security-Policy: sandbox with it — see there.
func inlineDisposition(r *http.Request) bool {
	return !r.URL.Query().Has("download")
}
