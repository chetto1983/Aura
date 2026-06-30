package agui

import (
	"encoding/json"
	"net/http"

	"github.com/chetto1983/aura/internal/documents"
)

func (s *Server) registerStorageOrphanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/storage/orphans", s.handleStorageOrphansDryRun)
	mux.HandleFunc("POST /api/storage/orphans/cleanup", s.handleStorageOrphansCleanup)
}

func (s *Server) handleStorageOrphansDryRun(w http.ResponseWriter, r *http.Request) {
	if !s.storageOrphansReady(w, r) {
		return
	}
	report, err := s.storageOrphans.DryRun(r.Context(), documents.StorageOrphanRequest{
		Bucket: r.URL.Query().Get("bucket"),
		Prefix: r.URL.Query().Get("prefix"),
	})
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, report)
}

func (s *Server) handleStorageOrphansCleanup(w http.ResponseWriter, r *http.Request) {
	if !s.storageOrphansReady(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var req documents.StorageOrphanCleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	report, err := s.storageOrphans.Cleanup(r.Context(), req)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, report)
}

func (s *Server) storageOrphansReady(w http.ResponseWriter, r *http.Request) bool {
	if s.storageOrphans == nil {
		http.Error(w, "storage orphan service unavailable", http.StatusServiceUnavailable)
		return false
	}
	if _, ok := principalIdentityID(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}
