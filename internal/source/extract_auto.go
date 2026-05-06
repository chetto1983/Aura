package source

import (
	"context"
	"fmt"
)

type PyodideExtractor interface {
	ExtractXLSX(context.Context, []byte) (ExtractResult, error)
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
	case KindXLSX:
		if runner == nil {
			return ExtractResult{}, fmt.Errorf("source: xlsx extraction requires pyodide runner")
		}
		return ExtractWithPyodide(ctx, runner, in)
	default:
		return ExtractResult{}, fmt.Errorf("source: no extractor for kind %s", in.Source.Kind)
	}
}
