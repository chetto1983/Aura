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

// rawAsset maps a source kind to its on-disk filename and download
// content-type. PDFs render inline (browsers handle them); xlsx forces
// an attachment because no browser previews .xlsx natively. Kept as a
// table so adding KindDOCX / KindPDFGen in slices 15b/15c is one row
// each — no router change.
type rawAsset struct {
	filename    string // original.<ext>
	contentType string
	disposition string // "inline" or "attachment"
}

var rawAssets = map[source.Kind]rawAsset{
	source.KindPDF: {
		filename:    "original.pdf",
		contentType: "application/pdf",
		disposition: "inline",
	},
	source.KindXLSX: {
		filename:    "original.xlsx",
		contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		disposition: "attachment",
	},
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
		asset, ok := rawAssets[rec.Kind]
		if !ok {
			writeError(w, deps.Logger, http.StatusNotFound, "raw endpoint not supported for this source kind")
			return
		}
		path := deps.Sources.Path(id, asset.filename)
		if path == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source path")
			return
		}
		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, asset.filename+" not found")
				return
			}
			deps.Logger.Warn("api: open raw file", "id", id, "file", asset.filename, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read "+asset.filename)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			deps.Logger.Warn("api: stat raw file", "id", id, "file", asset.filename, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to stat "+asset.filename)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		w.Header().Set("Content-Disposition", asset.disposition+`; filename="`+rec.Filename+`"`)
		http.ServeContent(w, r, rec.Filename, stat.ModTime(), f)
	}
}

func validKind(k source.Kind) bool {
	switch k {
	case source.KindPDF, source.KindText, source.KindURL, source.KindXLSX:
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
