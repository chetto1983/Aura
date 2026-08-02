//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"fmt"
	"maps"
)

// ImageDocumentProcessor makes an uploaded image both describable inline AND
// retrievable. It runs the vision summary (Vision) and the document registration
// (Document) over the SAME image, then merges the results. It is fail-soft:
//   - both succeed  -> StatusSearchable, DocumentID set, vision Summary kept for display;
//   - ingest fails  -> StatusComplete, vision Summary only;
//   - vision down   -> still registered, with the Document leg's summary as fallback;
//   - both fail     -> error (the service then marks the asset failed).
//
// The Document leg REGISTERS, it does not extract. Image extensions are on the
// documents ingest allowlist, so an image gets the catalog row document_search
// ranks and document_open resolves — and nothing more. No OCR, no chunking, no
// embedding runs on this path, so the only text describing an image is the vision
// Summary, until the agent opens the file and writes a digest. That also means the
// Document leg does not fail on "this image has no text": a non-nil docErr is a
// real ingest failure (object read, size ceiling, unsupported extension, catalog
// write), never a verdict about the content.
type ImageDocumentProcessor struct {
	Vision   Processor // vision "describe this image" summary (*ImageProcessor)
	Document Processor // document-catalog registration (*DocumentProcessor)
}

func (p *ImageDocumentProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	if p == nil || p.Vision == nil || p.Document == nil {
		return Result{}, fmt.Errorf("image-document processor is not configured")
	}
	visionRes, visionErr := p.Vision.ProcessAsset(ctx, asset)
	docRes, docErr := p.Document.ProcessAsset(ctx, asset)

	if visionErr != nil && docErr != nil {
		return Result{}, fmt.Errorf("image processing failed (vision: %v; index: %w)", visionErr, docErr)
	}

	merged := Result{Status: StatusComplete, Metadata: map[string]any{}}
	if visionErr == nil {
		merged.Summary = visionRes.Summary
		maps.Copy(merged.Metadata, visionRes.Metadata)
	}
	if docErr == nil {
		merged.Status = docRes.Status // searchable
		merged.DocumentID = docRes.DocumentID
		maps.Copy(merged.Metadata, docRes.Metadata)
		merged.Metadata["document_indexed"] = true
		if merged.Summary == "" { // vision was down — fall back to the index summary
			merged.Summary = docRes.Summary
		}
	} else {
		// Ingest failed, so the image has no catalog row and document_open cannot reach
		// it. The vision summary still makes the asset useful in chat.
		merged.Metadata["document_indexed"] = false
		merged.Metadata["document_index_skipped_reason"] = docErr.Error()
	}
	return merged, nil
}
