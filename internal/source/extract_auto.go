package source

import (
	"context"
	"fmt"
)

type PyodideExtractor interface {
	ExtractXLSX(context.Context, []byte) (ExtractResult, error)
	ExtractDOCX(context.Context, []byte) (ExtractResult, error)
}

type PyodideRunner interface {
	PyodideExtractor
}

func ExtractUploadedSource(ctx context.Context, runner PyodideExtractor, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	switch in.Source.Kind {
	case KindText, KindMarkdown, KindJSON, KindCSV:
		return ExtractGo(ctx, in)
	case KindXLSX, KindDOCX:
		if runner == nil {
			return ExtractResult{}, fmt.Errorf("source: %s extraction requires pyodide runner", in.Source.Kind)
		}
		return ExtractWithPyodide(ctx, runner, in)
	default:
		return ExtractResult{}, fmt.Errorf("source: no extractor for kind %s", in.Source.Kind)
	}
}
