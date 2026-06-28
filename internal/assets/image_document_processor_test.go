package assets

import (
	"context"
	"errors"
	"testing"
)

type cannedProcessor struct {
	res Result
	err error
}

func (c cannedProcessor) ProcessAsset(context.Context, Asset) (Result, error) { return c.res, c.err }

func TestImageDocumentProcessor(t *testing.T) {
	asset := Asset{ID: "a1", Modality: ModalityImage, FileName: "shot.png"}
	vision := Result{Status: StatusComplete, Summary: "a control panel with gauges", Metadata: map[string]any{"vision_model": "glm-ocr"}}
	doc := Result{Status: StatusSearchable, DocumentID: "doc-9", Summary: "shot.png indexed with 1 searchable chunks.", Metadata: map[string]any{"sparse_chunks": 1}}
	noChunks := errors.New("document extractor returned no chunks")

	t.Run("both succeed -> searchable, vision summary kept, metadata merged", func(t *testing.T) {
		p := &ImageDocumentProcessor{Vision: cannedProcessor{res: vision}, Document: cannedProcessor{res: doc}}
		got, err := p.ProcessAsset(context.Background(), asset)
		if err != nil {
			t.Fatalf("ProcessAsset: %v", err)
		}
		if got.Status != StatusSearchable || got.DocumentID != "doc-9" {
			t.Fatalf("result = %+v, want searchable doc-9", got)
		}
		if got.Summary != vision.Summary {
			t.Errorf("Summary = %q, want the vision summary", got.Summary)
		}
		if got.Metadata["vision_model"] != "glm-ocr" || got.Metadata["sparse_chunks"] != 1 || got.Metadata["document_indexed"] != true {
			t.Errorf("metadata not merged: %#v", got.Metadata)
		}
	})

	t.Run("no extractable text -> vision-only complete", func(t *testing.T) {
		p := &ImageDocumentProcessor{Vision: cannedProcessor{res: vision}, Document: cannedProcessor{err: noChunks}}
		got, err := p.ProcessAsset(context.Background(), asset)
		if err != nil {
			t.Fatalf("ProcessAsset: %v", err)
		}
		if got.Status != StatusComplete || got.DocumentID != "" {
			t.Fatalf("result = %+v, want complete with no document id", got)
		}
		if got.Summary != vision.Summary {
			t.Errorf("Summary = %q, want vision summary", got.Summary)
		}
		if got.Metadata["document_indexed"] != false || got.Metadata["document_index_skipped_reason"] != noChunks.Error() {
			t.Errorf("metadata = %#v, want indexed=false + reason", got.Metadata)
		}
	})

	t.Run("vision down but indexing ok -> searchable with index summary fallback", func(t *testing.T) {
		p := &ImageDocumentProcessor{Vision: cannedProcessor{err: errors.New("vision sidecar 503")}, Document: cannedProcessor{res: doc}}
		got, err := p.ProcessAsset(context.Background(), asset)
		if err != nil {
			t.Fatalf("ProcessAsset: %v", err)
		}
		if got.Status != StatusSearchable || got.DocumentID != "doc-9" {
			t.Fatalf("result = %+v, want searchable doc-9", got)
		}
		if got.Summary != doc.Summary {
			t.Errorf("Summary = %q, want index summary fallback", got.Summary)
		}
	})

	t.Run("both fail -> error", func(t *testing.T) {
		p := &ImageDocumentProcessor{Vision: cannedProcessor{err: errors.New("vision down")}, Document: cannedProcessor{err: errors.New("neo4j down")}}
		if _, err := p.ProcessAsset(context.Background(), asset); err == nil {
			t.Fatal("want error when both vision and index fail")
		}
	})

	t.Run("nil/unconfigured -> error", func(t *testing.T) {
		var p *ImageDocumentProcessor
		if _, err := p.ProcessAsset(context.Background(), asset); err == nil {
			t.Fatal("want error for nil processor")
		}
		if _, err := (&ImageDocumentProcessor{}).ProcessAsset(context.Background(), asset); err == nil {
			t.Fatal("want error for unconfigured processor")
		}
	})
}
