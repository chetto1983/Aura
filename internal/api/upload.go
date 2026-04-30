package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/source"
)

// uploadOCRTimeout matches the Telegram path's per-OCR cap. PDFs of 100
// pages take ~30-60s; the cap keeps a stalled call from holding the
// request goroutine forever.
const uploadOCRTimeout = 5 * time.Minute

// defaultMaxUploadMB is used when Deps.MaxUploadMB is zero. Mirrors the
// OCR_MAX_FILE_MB default in internal/config.
const defaultMaxUploadMB = 100

// UploadResponse is the JSON body returned by POST /sources/upload.
type UploadResponse struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Duplicate  bool     `json:"duplicate"`
	Filename   string   `json:"filename"`
	PageCount  int      `json:"page_count,omitempty"`
	WikiPages  []string `json:"wiki_pages,omitempty"`
	IngestNote string   `json:"ingest_note,omitempty"`
	OCRError   string   `json:"ocr_error,omitempty"`
	Note       string   `json:"note,omitempty"` // human-friendly summary line
}

// handleSourceUpload accepts a multipart PDF upload and runs the same
// pipeline as Telegram: store -> OCR -> auto-ingest. Bounded by the
// configured size cap; OCR/ingest are skipped if their dependencies are
// nil so the bot still works when MISTRAL_API_KEY is unset.
func handleSourceUpload(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sources == nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "source store unavailable")
			return
		}

		maxMB := deps.MaxUploadMB
		if maxMB <= 0 {
			maxMB = defaultMaxUploadMB
		}
		maxBytes := int64(maxMB) * 1024 * 1024

		// Cap the total request body so a malicious upload can't OOM the bot.
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024) // +1 MiB headroom for multipart overhead

		if err := r.ParseMultipartForm(maxBytes); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "parse upload: "+err.Error())
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "missing file field (use multipart 'file')")
			return
		}
		defer file.Close()

		filename := safeUploadName(header.Filename)
		if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
			writeError(w, deps.Logger, http.StatusBadRequest, "only PDF uploads are accepted")
			return
		}
		if header.Size > maxBytes {
			writeError(w, deps.Logger, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("file exceeds %d MB cap", maxMB))
			return
		}

		body, err := io.ReadAll(file)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "read upload: "+err.Error())
			return
		}
		if len(body) == 0 {
			writeError(w, deps.Logger, http.StatusBadRequest, "empty file")
			return
		}

		// Step 1 — store
		src, dup, err := deps.Sources.Put(r.Context(), source.PutInput{
			Kind:     source.KindPDF,
			Filename: filename,
			MimeType: "application/pdf",
			Bytes:    body,
		})
		if err != nil {
			deps.Logger.Warn("api: upload put failed", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "store failed: "+err.Error())
			return
		}

		resp := UploadResponse{
			ID:        src.ID,
			Status:    string(src.Status),
			Duplicate: dup,
			Filename:  src.Filename,
			PageCount: src.PageCount,
			WikiPages: src.WikiPages,
		}

		if dup {
			resp.Note = fmt.Sprintf("duplicate · already stored as %s (status %s)", src.ID, src.Status)
			writeJSON(w, deps.Logger, http.StatusOK, resp)
			return
		}

		// Step 2 — OCR (optional)
		if deps.OCR == nil {
			resp.Note = fmt.Sprintf("stored as %s (OCR disabled)", src.ID)
			writeJSON(w, deps.Logger, http.StatusOK, resp)
			return
		}

		ocrCtx, cancel := context.WithTimeout(r.Context(), uploadOCRTimeout)
		defer cancel()
		ocrRes, err := deps.OCR.Process(ocrCtx, ocr.ProcessInput{PDFBytes: body})
		if err != nil {
			_, _ = upsertSourceStatus(deps.Sources, src.ID, source.StatusFailed, err.Error())
			resp.Status = string(source.StatusFailed)
			resp.OCRError = err.Error()
			resp.Note = "OCR failed: " + err.Error()
			deps.Logger.Warn("api: upload ocr failed", "source_id", src.ID, "error", err)
			writeJSON(w, deps.Logger, http.StatusOK, resp)
			return
		}

		// Step 3 — write ocr.md / ocr.json next to the source.
		md := ocr.RenderMarkdown(ocr.RenderMeta{
			SourceID: src.ID,
			Filename: src.Filename,
			Model:    ocrRes.Response.Model,
		}, ocrRes.Response)
		if err := writeNextToSource(deps.Sources, src.ID, "ocr.md", []byte(md)); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "write ocr.md: "+err.Error())
			return
		}
		if err := writeNextToSource(deps.Sources, src.ID, "ocr.json", ocrRes.RawJSON); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "write ocr.json: "+err.Error())
			return
		}

		pageCount := len(ocrRes.Response.Pages)
		if ocrRes.Response.UsageInfo != nil && ocrRes.Response.UsageInfo.PagesProcessed > 0 {
			pageCount = ocrRes.Response.UsageInfo.PagesProcessed
		}

		// Step 4 — flip status, attach OCR metadata.
		updated, err := deps.Sources.Update(src.ID, func(s *source.Source) error {
			s.Status = source.StatusOCRComplete
			s.OCRModel = ocrRes.Response.Model
			s.PageCount = pageCount
			return nil
		})
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "status update: "+err.Error())
			return
		}
		resp.Status = string(updated.Status)
		resp.PageCount = updated.PageCount

		// Step 5 — auto-ingest (optional).
		if deps.Ingest != nil {
			ingCtx, ingCancel := context.WithTimeout(r.Context(), uploadOCRTimeout)
			note, err := deps.Ingest.AfterOCR(ingCtx, updated)
			ingCancel()
			if err != nil {
				deps.Logger.Warn("api: upload ingest failed", "source_id", src.ID, "error", err)
				resp.Note = fmt.Sprintf("OCR done · ingest failed: %v", err)
			} else {
				resp.IngestNote = note
				// Re-read so we surface the new ingested status + wiki_pages.
				if fresh, ferr := deps.Sources.Get(src.ID); ferr == nil {
					resp.Status = string(fresh.Status)
					resp.WikiPages = fresh.WikiPages
				}
				resp.Note = fmt.Sprintf("ingested · %d page(s) · %s", pageCount, note)
			}
		} else {
			resp.Note = fmt.Sprintf("OCR done · %d page(s) · ready for ingest", pageCount)
		}

		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

// safeUploadName mirrors the cleaning telegram/documents.go does: strip
// control characters and path separators, cap to 80 chars. The on-disk
// path is always original.pdf regardless; this only affects the display
// filename stored in source.json.
func safeUploadName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "(unnamed.pdf)"
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	if len(cleaned) > 80 {
		cleaned = cleaned[:80] + "…"
	}
	return cleaned
}

// upsertSourceStatus is a small helper that swallows the not-found case so
// the OCR-failure path doesn't crash if the source was just created — its
// only purpose is bookkeeping.
func upsertSourceStatus(store SourceStore, id string, status source.Status, errMsg string) (*source.Source, error) {
	mut, ok := store.(interface {
		Update(id string, mutator func(*source.Source) error) (*source.Source, error)
	})
	if !ok {
		return nil, errors.New("source store does not support Update")
	}
	return mut.Update(id, func(s *source.Source) error {
		s.Status = status
		s.Error = errMsg
		return nil
	})
}

// writeNextToSource mirrors the helper in internal/telegram/documents.go.
// Uses Store.Path so the join is containment-checked. Errors only surface
// when the path is rejected or the file system itself fails.
func writeNextToSource(store SourceStore, id, name string, data []byte) error {
	path := store.Path(id, name)
	if path == "" {
		return fmt.Errorf("invalid path for %s/%s", id, name)
	}
	return os.WriteFile(path, data, 0o644)
}
