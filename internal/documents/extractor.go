package documents

import "context"

// Extractor converts a source file into normalized document chunks.
type Extractor interface {
	ExtractFile(ctx context.Context, path string, req IngestRequest) (*ExtractorResponse, error)
}
