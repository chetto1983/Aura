package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/sources/store"
)

// OCRSourceTool runs Mistral OCR over a stored PDF source. Mirrors the
// pipeline in internal/telegram/documents.go but is callable by the LLM —
// useful when an upload was queued before OCR was enabled, or to retry a
// failed source.
type OCRSourceTool struct {
	store source.Repository
	ocr   *ocr.Client
}

func NewOCRSourceTool(store source.Repository, client *ocr.Client) *OCRSourceTool {
	return &OCRSourceTool{store: store, ocr: client}
}

func (t *OCRSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("ocr_source: source store unavailable")
	}
	if t.ocr == nil {
		return "", errors.New("ocr_source: OCR is disabled (set MISTRAL_API_KEY to enable)")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}
	src, err := t.store.Get(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("ocr_source: source %s not found", id)
		}
		return "", fmt.Errorf("ocr_source: %w", err)
	}
	if src.Kind != source.KindPDF {
		return "", fmt.Errorf("ocr_source: only PDF sources can be OCRed (got %s)", src.Kind)
	}

	pdfPath := t.store.Path(id, "original.pdf")
	if pdfPath == "" {
		return "", fmt.Errorf("ocr_source: invalid source path for %s", id)
	}
	pdfBytes, err := readBoundedFile(pdfPath, maxOCRPDFBytes)
	if err != nil {
		return "", fmt.Errorf("ocr_source: read pdf: %w", err)
	}

	start := time.Now()
	res, err := t.ocr.Process(ctx, ocr.ProcessInput{PDFBytes: pdfBytes})
	if err != nil {
		_, _ = t.store.Update(id, func(s *source.Source) error {
			s.Status = source.StatusFailed
			s.Error = err.Error()
			return nil
		})
		return "", fmt.Errorf("ocr_source: %w", err)
	}
	elapsed := time.Since(start)

	mdPath := t.store.Path(id, "ocr.md")
	jsonPath := t.store.Path(id, "ocr.json")
	if mdPath == "" || jsonPath == "" {
		return "", fmt.Errorf("ocr_source: invalid output path for %s", id)
	}

	md := ocr.RenderMarkdown(ocr.RenderMeta{
		SourceID: id,
		Filename: src.Filename,
		Model:    res.Response.Model,
	}, res.Response)

	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("ocr_source: write ocr.md: %w", err)
	}
	if err := os.WriteFile(jsonPath, res.RawJSON, 0o644); err != nil {
		return "", fmt.Errorf("ocr_source: write ocr.json: %w", err)
	}

	pageCount := len(res.Response.Pages)
	if res.Response.UsageInfo != nil && res.Response.UsageInfo.PagesProcessed > 0 {
		pageCount = res.Response.UsageInfo.PagesProcessed
	}

	if _, err := t.store.Update(id, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.OCRModel = res.Response.Model
		s.PageCount = pageCount
		s.Error = ""
		return nil
	}); err != nil {
		// Roll back the artifact writes so the on-disk view matches the
		// status field — otherwise list_sources shows status=stored while
		// ocr.md exists, confusing the LLM about which sources still need OCR.
		_ = os.Remove(mdPath)
		_ = os.Remove(jsonPath)
		return "", fmt.Errorf("ocr_source: update metadata: %w (ocr files rolled back)", err)
	}

	return fmt.Sprintf("OCR complete · %s · %d page(s) · %s · model=%s",
		id, pageCount, formatToolDuration(elapsed), res.Response.Model), nil
}
