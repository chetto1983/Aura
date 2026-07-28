//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"fmt"
	"maps"
)

// ImageDocumentProcessor makes an uploaded image both describable inline AND searchable.
// It runs the vision summary (Vision) and the searchable document ingest (Document —
// markitdown OCR -> chunks -> embed -> Neo4j) over the SAME image, then merges
// the results. It is fail-soft:
//   - both succeed  -> StatusSearchable, DocumentID set, vision Summary kept for display;
//   - no text/OCR   -> StatusComplete, vision Summary only (normal for non-document images);
//   - vision down   -> still searchable via the index, with the index summary as fallback;
//   - both fail     -> error (the service then marks the asset failed).
//
// Spike 075 proved the documents pipeline already OCRs+chunks+embeds+indexes images, so the
// asset layer only had to route an image through it WITHOUT dropping the existing vision
// summary — hence this composite over the Image slot rather than a modality re-route.
//
// Vision and Document run sequentially on purpose: both call the GPU OCR/vision sidecar,
// and serializing the two calls avoids contending for VRAM on the 4 GB card.
type ImageDocumentProcessor struct {
	Vision   Processor // vision "describe this image" summary (*ImageProcessor)
	Document Processor // markitdown OCR -> chunks -> Neo4j (*DocumentProcessor)
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
		// Not indexable (e.g. an image with no text -> empty OCR -> no chunks). Normal for
		// non-document images; the vision summary still makes the asset useful in chat.
		merged.Metadata["document_indexed"] = false
		merged.Metadata["document_index_skipped_reason"] = docErr.Error()
	}
	return merged, nil
}
