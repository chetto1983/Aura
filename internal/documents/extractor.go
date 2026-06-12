package documents

import "context"

type Extractor interface {
	ExtractFile(ctx context.Context, path string, req IngestRequest) (*ExtractorResponse, error)
}
