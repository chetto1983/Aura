package obs

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// aura_agent_span_export_failures_total was declared in the catalog and recorded nowhere until
// 2026-10-26, so a dashboard on it read a permanent zero while traces were being lost. Each
// failed export call counts once; a successful one does not count.
func TestSpanExportFailuresAreCounted(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("aura.test")
	inner := &scriptedExporter{errs: []error{nil, errors.New("collector down"), errors.New("collector down")}}
	exporter, err := countSpanExportFailures(inner, meter)
	if err != nil {
		t.Fatalf("countSpanExportFailures: %v", err)
	}
	spans := tracetest.SpanStubs{{Name: "turn"}}.Snapshots()
	for range inner.errs {
		_ = exporter.ExportSpans(context.Background(), spans)
	}
	if inner.calls != 3 {
		t.Fatalf("inner exporter calls = %d, want every export forwarded", inner.calls)
	}
	assertMetricPoint(t, collectBoundaryMetrics(t, reader), MustDescriptor(AgentSpanExportFailuresID).Name, 2, "", "")
}

type scriptedExporter struct {
	errs  []error
	calls int
}

func (e *scriptedExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	err := e.errs[e.calls]
	e.calls++
	return err
}

func (*scriptedExporter) Shutdown(context.Context) error { return nil }
