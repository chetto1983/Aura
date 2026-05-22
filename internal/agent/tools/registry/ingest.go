package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/storage/sources/ingest"
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
