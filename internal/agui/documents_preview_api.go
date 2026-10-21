package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

type documentPreviewer interface {
	Preview(context.Context, string, string, documents.PreviewSource) ([]byte, error)
}
type documentIngestionReader interface {
	IngestState(context.Context, string) (*arcadedb.IngestState, error)
}

// SetDocumentPreviewer wires native previews; originals retain their existing full viewers.
func (s *Server) SetDocumentPreviewer(provider documentPreviewer) { s.documentPreviews = provider }

// SetDocumentIngestionReader wires the reconciler's existing tenant-scoped status read.
func (s *Server) SetDocumentIngestionReader(reader documentIngestionReader) {
	s.documentIngestion = reader
}

func (s *Server) registerDocumentPreviewRoutes(mux *http.ServeMux) {
	thumbnail := s.documentsRoute(s.handleDocumentThumbnail)
	mux.HandleFunc("GET "+documentsBase+"/{id}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		thumbnail(w, r)
	})
	mux.HandleFunc("GET "+documentsBase+"/ingestion", s.handleDocumentIngestion)
}

func (s *Server) handleDocumentThumbnail(w http.ResponseWriter, r *http.Request, store documentsStore, identityID string) {
	if s.documentPreviews == nil || s.fileObjects == nil {
		writePreviewError(w, documents.ErrPreviewUnavailable)
		return
	}
	detail, err := store.Document(r.Context(), identityID, r.PathValue("id"))
	if err != nil {
		writeDocumentsError(w, err)
		return
	}
	if detail.SourceKey == "" {
		writeDocumentsError(w, docfields.ErrNotFound)
		return
	}
	body, err := s.documentPreviews.Preview(r.Context(), identityID, detail.FileName, func(ctx context.Context) (io.ReadCloser, int64, error) {
		object, attrs, err := s.fileObjects.OpenSeekable(ctx, identityID, detail.SourceKey)
		if err != nil {
			return nil, 0, err
		}
		return object, attrs.SizeBytes, nil
	})
	if err != nil {
		writePreviewError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writePreviewError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "thumbnail_failed"
	switch {
	case errors.Is(err, documents.ErrPreviewUnavailable):
		status, code = http.StatusServiceUnavailable, "thumbnail_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusGatewayTimeout, "thumbnail_timeout"
	case errors.Is(err, context.Canceled):
		status, code = http.StatusRequestTimeout, "thumbnail_cancelled"
	case objectstore.IsNotFound(err):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, documents.ErrPreviewUnsupported):
		status, code = http.StatusUnsupportedMediaType, "thumbnail_unsupported"
	case errors.Is(err, documents.ErrPreviewTooLarge):
		status, code = http.StatusRequestEntityTooLarge, "thumbnail_too_large"
	case errors.Is(err, documents.ErrPreviewInvalid):
		status, code = http.StatusUnprocessableEntity, "thumbnail_invalid"
	}
	writeJSONStatus(w, status, map[string]string{"error": code})
}

func (s *Server) handleDocumentIngestion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.documentIngestion == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "documents_unavailable"})
		return
	}
	state, err := s.documentIngestion.IngestState(r.Context(), scopedIdentityID(r.Context()))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "ingestion_unavailable"})
		return
	}
	writeJSON(w, map[string]any{"indexing": state})
}
