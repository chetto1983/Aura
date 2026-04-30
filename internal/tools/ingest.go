package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/ingest"
)

// IngestSourceTool compiles a stored source into a wiki summary page via the
// ingest pipeline. Idempotent: a second call on an already-ingested source
// returns the existing slug.
//
// Slice 6 ships the deterministic auto-ingest path; richer LLM-driven
// extraction of entity/concept pages from the OCR markdown is left for a
// later slice.
type IngestSourceTool struct {
	pipeline *ingest.Pipeline
}

func NewIngestSourceTool(p *ingest.Pipeline) *IngestSourceTool {
	return &IngestSourceTool{pipeline: p}
}

func (t *IngestSourceTool) Name() string { return "ingest_source" }

func (t *IngestSourceTool) Description() string {
	return "Compile a stored source (status=ocr_complete) into a wiki summary page. Returns the wiki slug. Idempotent."
}

func (t *IngestSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "string",
				"description": "Source ID (e.g. src_<16hex>) with status ocr_complete.",
			},
		},
		"required": []string{"source_id"},
	}
}

func (t *IngestSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.pipeline == nil {
		return "", errors.New("ingest_source: pipeline unavailable")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}

	res, err := t.pipeline.Compile(ctx, id)
	if err != nil {
		return "", fmt.Errorf("ingest_source: %w", err)
	}

	verb := "Compiled"
	if !res.Created {
		verb = "Already compiled"
	}
	return fmt.Sprintf("%s source %s as [[%s]]", verb, id, res.Slug), nil
}
