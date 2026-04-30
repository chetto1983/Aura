package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/aura/aura/internal/source"
)

func handleSourceList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := source.ListFilter{
			Kind:   source.Kind(r.URL.Query().Get("kind")),
			Status: source.Status(r.URL.Query().Get("status")),
		}
		if filter.Kind != "" && !validKind(filter.Kind) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid kind")
			return
		}
		if filter.Status != "" && !validStatus(filter.Status) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid status")
			return
		}
		records, err := deps.Sources.List(filter)
		if err != nil {
			deps.Logger.Warn("api: list sources", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list sources")
			return
		}
		out := make([]SourceSummary, 0, len(records))
		for _, rec := range records {
			out = append(out, SourceSummary{
				ID:        rec.ID,
				Kind:      string(rec.Kind),
				Filename:  rec.Filename,
				Status:    string(rec.Status),
				CreatedAt: rec.CreatedAt.UTC(),
				PageCount: rec.PageCount,
				WikiPages: rec.WikiPages,
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleSourceGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		rec, err := deps.Sources.Get(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "source not found")
				return
			}
			deps.Logger.Warn("api: get source", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read source")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, SourceDetail{
			ID:        rec.ID,
			Kind:      string(rec.Kind),
			Filename:  rec.Filename,
			MimeType:  rec.MimeType,
			SHA256:    rec.SHA256,
			SizeBytes: rec.SizeBytes,
			CreatedAt: rec.CreatedAt.UTC(),
			Status:    string(rec.Status),
			OCRModel:  rec.OCRModel,
			PageCount: rec.PageCount,
			WikiPages: rec.WikiPages,
			Error:     rec.Error,
		})
	}
}

func handleSourceOCR(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		path := deps.Sources.Path(id, "ocr.md")
		if path == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source path")
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "ocr.md not found for this source")
				return
			}
			deps.Logger.Warn("api: read ocr.md", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read ocr.md")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, SourceOCR{Markdown: string(data)})
	}
}

func handleSourceRaw(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !sourceIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source id")
			return
		}
		rec, err := deps.Sources.Get(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "source not found")
				return
			}
			deps.Logger.Warn("api: get source for raw", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read source")
			return
		}
		if rec.Kind != source.KindPDF {
			writeError(w, deps.Logger, http.StatusNotFound, "raw endpoint only supports PDF sources")
			return
		}
		path := deps.Sources.Path(id, "original.pdf")
		if path == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source path")
			return
		}
		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "original.pdf not found")
				return
			}
			deps.Logger.Warn("api: open original.pdf", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read original.pdf")
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			deps.Logger.Warn("api: stat original.pdf", "id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to stat original.pdf")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `inline; filename="`+rec.Filename+`"`)
		http.ServeContent(w, r, rec.Filename, stat.ModTime(), f)
	}
}

func validKind(k source.Kind) bool {
	switch k {
	case source.KindPDF, source.KindText, source.KindURL:
		return true
	}
	return false
}

func validStatus(s source.Status) bool {
	switch s {
	case source.StatusStored, source.StatusOCRComplete, source.StatusIngested, source.StatusFailed:
		return true
	}
	return false
}
