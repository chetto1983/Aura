package source

import (
	"context"
	"fmt"
)

func ExtractWithPyodide(ctx context.Context, runner PyodideExtractor, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	switch in.Source.Kind {
	case KindXLSX:
		if runner == nil {
			return ExtractResult{}, fmt.Errorf("source: xlsx extraction requires pyodide runner")
		}
		return runner.ExtractXLSX(ctx, in.Bytes)
	default:
		return ExtractResult{}, fmt.Errorf("source: no Pyodide extractor for kind %s", in.Source.Kind)
	}
}
