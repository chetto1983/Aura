package source

import (
	"context"
	"fmt"
)

// SandboxExtractor is the runtime contract for parsing non-text uploads
// (XLSX/DOCX) through the sandbox process runner.
type SandboxExtractor interface {
	ExtractXLSX(context.Context, []byte) (ExtractResult, error)
	ExtractDOCX(context.Context, []byte) (ExtractResult, error)
}

func ExtractUploadedSource(ctx context.Context, runner SandboxExtractor, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	switch in.Source.Kind {
	case KindText, KindMarkdown, KindJSON, KindCSV:
		return ExtractGo(ctx, in)
	case KindXLSX:
		if runner == nil {
			return ExtractResult{}, fmt.Errorf("source: xlsx extraction requires sandbox runner")
		}
		return runner.ExtractXLSX(ctx, in.Bytes)
	case KindDOCX:
		if runner == nil {
			return ExtractResult{}, fmt.Errorf("source: docx extraction requires sandbox runner")
		}
		return runner.ExtractDOCX(ctx, in.Bytes)
	default:
		return ExtractResult{}, fmt.Errorf("source: no extractor for kind %s", in.Source.Kind)
	}
}
