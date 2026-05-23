package api

import (
	"context"
	"net/http"
	"os"

	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/sources/store"
)

// IngestResponse is the JSON body returned by POST /sources/{id}/ingest.
// It mirrors the relevant fields of UploadResponse (slice 10c.1) so the
// frontend can use the same toast formatter for both.
type IngestResponse struct {
	ID                string   `json:"id"`
	Status            string   `json:"status"`
	Filename          string   `json:"filename"`
	WikiPages         []string `json:"wiki_pages,omitempty"`
	MaterializedPages []string `json:"materialized_pages,omitempty"`
	IngestNote        string   `json:"ingest_note,omitempty"`
	Note              string   `json:"note,omitempty"`
}

// ReocrResponse is the JSON body returned by POST /sources/{id}/reocr. It
// covers both successful re-OCR and re-OCR-then-auto-ingest paths.
type ReocrResponse struct {
	ID                string   `json:"id"`
	Status            string   `json:"status"`
	Filename          string   `json:"filename"`
	PageCount         int      `json:"page_count,omitempty"`
	WikiPages         []string `json:"wiki_pages,omitempty"`
	MaterializedPages []string `json:"materialized_pages,omitempty"`
	IngestNote        string   `json:"ingest_note,omitempty"`
	OCRError          string   `json:"ocr_error,omitempty"`
	Note              string   `json:"note,omitempty"`
}

// handleSourceIngest re-runs the ingest pipeline against a source whose
// OCR is already complete. Idempotent — Compile rewrites the same wiki
// page slug when called twice on the same source.
func handleSourceIngest(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		if deps.Ingest == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "ingest pipeline disabled (set MISTRAL_API_KEY)")
			return
		}
		rec, err := deps.Sources.Get(id)
		if err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			deps.Logger.Warn("api: ingest get source", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read source")
			return
		}
		if rec.Status != source.StatusOCRComplete && rec.Status != source.StatusExtractComplete && rec.Status != source.StatusIngested {
			writeError(w, deps.Logger, http.StatusConflict,
				"source not ready for ingest (status="+string(rec.Status)+"); run OCR or extraction first")
			return
		}

		// Detach from r.Context() — ingest can run long; browser drop
		// must not abort it. See internal/api/upload.go for the rationale.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), uploadOCRTimeout)
		defer cancel()
		note, err := deps.Ingest.AfterOCR(ctx, rec)
		if err != nil {
			deps.Logger.Warn("api: ingest failed", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "ingest failed: "+err.Error())
			return
		}

		fresh, ferr := deps.Sources.Get(id)
		if ferr != nil {
			fresh = rec
		}
		writeJSON(w, deps.Logger, http.StatusOK, IngestResponse{
			ID:                fresh.ID,
			Status:            string(fresh.Status),
			Filename:          fresh.Filename,
			WikiPages:         fresh.WikiPages,
			MaterializedPages: materializedPages(fresh.WikiPages),
			IngestNote:        note,
			Note:              "ingested · " + note,
		})
	}
}

// handleSourceReocr re-runs Mistral OCR over a stored PDF and, if an
// ingest pipeline is configured, follows up with auto-ingest. Use cases:
// the original OCR call failed, or the user re-uploaded after a model
// upgrade. Re-OCR is destructive in the sense that it overwrites
// ocr.md/ocr.json but the original.pdf is never touched.
func handleSourceReocr(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		if deps.OCR == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "OCR client disabled (set MISTRAL_API_KEY)")
			return
		}
		rec, err := deps.Sources.Get(id)
		if err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			deps.Logger.Warn("api: reocr get source", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read source")
			return
		}
		if rec.Kind != source.KindPDF {
			writeError(w, deps.Logger, http.StatusBadRequest, "reocr only supports PDF sources")
			return
		}

		path := deps.Sources.Path(id, "original.pdf")
		if path == "" {
			writeError(w, deps.Logger, http.StatusInternalServerError, "invalid source path")
			return
		}
		body, err := os.ReadFile(path)
		if err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			deps.Logger.Warn("api: reocr read pdf", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read original.pdf")
			return
		}

		// Detach from r.Context() — reocr is the same Mistral OCR call as
		// upload.go and must survive a browser drop. See upload.go rationale.
		ocrCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), uploadOCRTimeout)
		defer cancel()
		ocrRes, err := deps.OCR.Process(ocrCtx, ocr.ProcessInput{PDFBytes: body})
		if err != nil {
			_, _ = upsertSourceStatus(deps.Sources, id, source.StatusFailed, err.Error())
			deps.Logger.Warn("api: reocr failed", "id", id, "error", err)
			writeJSON(w, deps.Logger, http.StatusOK, ReocrResponse{
				ID:       id,
				Status:   string(source.StatusFailed),
				Filename: rec.Filename,
				OCRError: err.Error(),
				Note:     "OCR failed: " + err.Error(),
			})
			return
		}

		md := ocr.RenderMarkdown(ocr.RenderMeta{
			SourceID: id,
			Filename: rec.Filename,
			Model:    ocrRes.Response.Model,
		}, ocrRes.Response)
		if err := writeNextToSource(deps.Sources, id, "ocr.md", []byte(md)); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "write ocr.md: "+err.Error())
			return
		}
		if err := writeNextToSource(deps.Sources, id, "ocr.json", ocrRes.RawJSON); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "write ocr.json: "+err.Error())
			return
		}

		pageCount := len(ocrRes.Response.Pages)
		if ocrRes.Response.UsageInfo != nil && ocrRes.Response.UsageInfo.PagesProcessed > 0 {
			pageCount = ocrRes.Response.UsageInfo.PagesProcessed
		}

		updated, err := deps.Sources.Update(id, func(s *source.Source) error {
			s.Status = source.StatusOCRComplete
			s.OCRModel = ocrRes.Response.Model
			s.PageCount = pageCount
			s.Error = ""
			return nil
		})
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "status update: "+err.Error())
			return
		}

		resp := ReocrResponse{
			ID:                updated.ID,
			Status:            string(updated.Status),
			Filename:          updated.Filename,
			PageCount:         updated.PageCount,
			WikiPages:         updated.WikiPages,
			MaterializedPages: materializedPages(updated.WikiPages),
		}

		if deps.Ingest != nil {
			// Detach: ingest-after-reocr must survive browser drop too.
			ingCtx, ingCancel := context.WithTimeout(context.WithoutCancel(r.Context()), uploadOCRTimeout)
			note, err := deps.Ingest.AfterOCR(ingCtx, updated)
			ingCancel()
			if err != nil {
				deps.Logger.Warn("api: reocr ingest failed", "id", id, "error", err)
				resp.Note = "OCR done · ingest failed: " + err.Error()
			} else {
				resp.IngestNote = note
				if fresh, ferr := deps.Sources.Get(id); ferr == nil {
					resp.Status = string(fresh.Status)
					resp.WikiPages = fresh.WikiPages
					resp.MaterializedPages = materializedPages(fresh.WikiPages)
				}
				resp.Note = "re-OCR + ingested · " + note
			}
		} else {
			resp.Note = "re-OCR done · awaiting ingest"
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

// SourcePurger drops compact memoryindex rows (and their Qdrant mirror) for
// a given source_id. The router-side Deps struct accepts any impl that
// satisfies this interface so tests can swap fakes; production wires
// memoryindex.Store.PurgeSource.
type SourcePurger interface {
	PurgeSource(ctx context.Context, sourceID string) error
}

// wikiSourceRefScanner is satisfied by *wiki.Store and exposes the
// linear scan that finds wiki pages referencing a source ID via their
// frontmatter sources: list. Used by handleSourceDelete to surface
// orphan references the cascade did not delete, and by GET
// /sources/{id}/derived for the frontend confirmation preview.
type wikiSourceRefScanner interface {
	PagesReferencingSource(ctx context.Context, sourceID string) ([]string, error)
}

// DeleteResponse is the JSON body returned by DELETE /sources/{id}.
type DeleteResponse struct {
	ID                  string   `json:"id"`
	Status              string   `json:"status"`
	MemoryPurged        bool     `json:"memory_purged"`
	MemoryPurgeWarning  string   `json:"memory_purge_warning,omitempty"`
	WikiPagesDeleted    []string `json:"wiki_pages_deleted,omitempty"`
	WikiPagesFailed     []string `json:"wiki_pages_failed,omitempty"`
	OrphanReferences    []string `json:"orphan_references,omitempty"`
	OrphanScanError     string   `json:"orphan_scan_error,omitempty"`
}

// DerivedResponse is the JSON body returned by GET /sources/{id}/derived.
// It powers the frontend cascade-preview modal so the operator sees
// what the impending delete will touch before confirming.
type DerivedResponse struct {
	ID               string   `json:"id"`
	Filename         string   `json:"filename,omitempty"`
	TrackedPages     []string `json:"tracked_pages"`
	ReferencingPages []string `json:"referencing_pages"`
}

// handleSourceDelete removes the raw directory of a stored source,
// purges its rows from the compact memoryindex (and Qdrant mirror,
// via the memoryindex's vector cascade), and cascades the wiki pages
// the source materialised. Pages outside source.WikiPages that still
// reference the source ID are surfaced as 'orphan_references' so the
// operator can decide whether to clean them up — they may be shared
// with other sources.
//
// Idempotent for "not found": returns 404 so the frontend can show a
// "source already gone" toast and refresh the list.
func handleSourceDelete(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		if deps.Sources == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "source store unavailable")
			return
		}

		// Read source.json BEFORE delete so we know which wiki pages
		// to cascade. Delete() removes source.json itself.
		var trackedPages []string
		if rec, err := deps.Sources.Get(id); err == nil {
			trackedPages = append([]string(nil), rec.WikiPages...)
		} else if code, _ := errorStatus(err); code == http.StatusNotFound {
			writeError(w, deps.Logger, http.StatusNotFound, "source not found")
			return
		} else {
			deps.Logger.Warn("api: read source before delete", "id", id, "error", err)
		}

		if err := deps.Sources.Delete(r.Context(), id); err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			deps.Logger.Warn("api: delete source failed", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "delete failed")
			return
		}

		resp := DeleteResponse{ID: id, Status: "deleted"}
		if deps.SourcePurger != nil {
			if err := deps.SourcePurger.PurgeSource(r.Context(), id); err != nil {
				// Files are gone; index is now stale. Surface the warning
				// so the frontend can prompt for a rebuild.
				deps.Logger.Warn("api: source memoryindex purge failed", "id", id, "error", err)
				resp.MemoryPurgeWarning = err.Error()
			} else {
				resp.MemoryPurged = true
			}
		} else {
			resp.MemoryPurgeWarning = "memoryindex purger not configured; rebuild required"
		}

		// Cascade-delete the wiki pages this source materialised. Each
		// DeletePage call hits FTS5, GraphIndex, Qdrant, git, and TOC —
		// the same surface as a dashboard wiki delete.
		if deps.Wiki != nil {
			for _, slug := range trackedPages {
				if err := deps.Wiki.DeletePage(r.Context(), slug); err != nil {
					deps.Logger.Warn("api: cascade delete wiki page failed", "id", id, "slug", slug, "error", err)
					resp.WikiPagesFailed = append(resp.WikiPagesFailed, slug)
					continue
				}
				resp.WikiPagesDeleted = append(resp.WikiPagesDeleted, slug)
			}
		}

		// Linear scan for pages still referencing the deleted source.
		// These are either concept pages co-authored from this source
		// or stale links; the operator decides whether to prune them.
		if scanner, ok := deps.Wiki.(wikiSourceRefScanner); ok {
			refs, err := scanner.PagesReferencingSource(r.Context(), id)
			if err != nil {
				deps.Logger.Warn("api: orphan-ref scan failed", "id", id, "error", err)
				resp.OrphanScanError = err.Error()
			} else {
				resp.OrphanReferences = refs
			}
		}

		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

// handleSourceDerived returns a preview of what DELETE /sources/{id}
// will touch. The frontend uses this to render a confirmation modal
// listing tracked pages (cascade-deleted) and referencing pages (left
// in place but surfaced as orphans).
func handleSourceDerived(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		if deps.Sources == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "source store unavailable")
			return
		}
		rec, err := deps.Sources.Get(id)
		if err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			deps.Logger.Warn("api: derived get source", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read source")
			return
		}

		resp := DerivedResponse{
			ID:           rec.ID,
			Filename:     rec.Filename,
			TrackedPages: append([]string{}, rec.WikiPages...),
		}
		if scanner, ok := deps.Wiki.(wikiSourceRefScanner); ok {
			refs, err := scanner.PagesReferencingSource(r.Context(), id)
			if err != nil {
				deps.Logger.Warn("api: derived scan failed", "id", id, "error", err)
			} else {
				resp.ReferencingPages = refs
			}
		}
		if resp.ReferencingPages == nil {
			resp.ReferencingPages = []string{}
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}
