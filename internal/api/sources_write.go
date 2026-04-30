package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/source"
)

// IngestResponse is the JSON body returned by POST /sources/{id}/ingest.
// It mirrors the relevant fields of UploadResponse (slice 10c.1) so the
// frontend can use the same toast formatter for both.
type IngestResponse struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Filename   string   `json:"filename"`
	WikiPages  []string `json:"wiki_pages,omitempty"`
	IngestNote string   `json:"ingest_note,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// ReocrResponse is the JSON body returned by POST /sources/{id}/reocr. It
// covers both successful re-OCR and re-OCR-then-auto-ingest paths.
type ReocrResponse struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Filename   string   `json:"filename"`
	PageCount  int      `json:"page_count,omitempty"`
	WikiPages  []string `json:"wiki_pages,omitempty"`
	IngestNote string   `json:"ingest_note,omitempty"`
	OCRError   string   `json:"ocr_error,omitempty"`
	Note       string   `json:"note,omitempty"`
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
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "source not found")
				return
			}
			deps.Logger.Warn("api: ingest get source", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read source")
			return
		}
		if rec.Status != source.StatusOCRComplete && rec.Status != source.StatusIngested {
			writeError(w, deps.Logger, http.StatusConflict,
				"source not ready for ingest (status="+string(rec.Status)+"); run OCR first")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), uploadOCRTimeout)
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
			ID:         fresh.ID,
			Status:     string(fresh.Status),
			Filename:   fresh.Filename,
			WikiPages:  fresh.WikiPages,
			IngestNote: note,
			Note:       "ingested · " + note,
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
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "source not found")
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
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "original.pdf missing on disk")
				return
			}
			deps.Logger.Warn("api: reocr read pdf", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read original.pdf")
			return
		}

		ocrCtx, cancel := context.WithTimeout(r.Context(), uploadOCRTimeout)
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
			ID:        updated.ID,
			Status:    string(updated.Status),
			Filename:  updated.Filename,
			PageCount: updated.PageCount,
			WikiPages: updated.WikiPages,
		}

		if deps.Ingest != nil {
			ingCtx, ingCancel := context.WithTimeout(r.Context(), uploadOCRTimeout)
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
				}
				resp.Note = "re-OCR + ingested · " + note
			}
		} else {
			resp.Note = "re-OCR done · awaiting ingest"
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

// decodeJSONBody reads a small JSON body into v. Returns an error message
// suitable for writeError when parsing fails. Caps body at 64 KiB so a
// runaway client can't exhaust memory.
func decodeJSONBody(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
