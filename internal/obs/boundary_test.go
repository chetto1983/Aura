package obs

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBoundaryRecordsSuccessErrorCancelAndPanicExactlyOnce(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		panicWith any
		outcome   string
		errClass  string
	}{
		{name: "success", outcome: "success", errClass: "none"},
		{name: "error", err: errors.New("secret raw failure"), outcome: "error", errClass: ValueOther},
		{name: "cancel", err: context.Canceled, outcome: "canceled", errClass: "canceled"},
		{name: "panic", panicWith: "secret panic payload", outcome: "error", errClass: "panic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			recorder := tracetest.NewSpanRecorder()
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			boundary, err := NewBoundary(provider.Meter("aura.test"), tracerProvider.Tracer("aura.test"), BoundaryConfig{
				Operation: "mcp_call_tool",
				ToolClass: ToolClassMCP,
				Transport: "stdio",
				Count:     MCPCallsID,
				Duration:  MCPDurationID,
			})
			if err != nil {
				t.Fatalf("NewBoundary: %v", err)
			}

			func() {
				defer func() { _ = recover() }()
				func() {
					_, end := boundary.Start(context.Background())
					defer end.PanicSafe(&tt.err)
					if tt.panicWith != nil {
						panic(tt.panicWith)
					}
					end.End(tt.err)
					end.End(errors.New("duplicate end must be ignored"))
				}()
			}()

			metrics := collectBoundaryMetrics(t, reader)
			assertMetricPoint(t, metrics, "aura.mcp.call", 1, tt.outcome, tt.errClass)
			assertMetricPoint(t, metrics, "aura.mcp.call.duration", 1, tt.outcome, "")
			assertMetricPoint(t, metrics, "aura.boundary.in_flight", 0, "", "")
			if spans := recorder.Ended(); len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			_ = provider.Shutdown(context.Background())
			_ = tracerProvider.Shutdown(context.Background())
		})
	}
}

func collectBoundaryMetrics(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var out []metricdata.Metrics
	for _, scope := range rm.ScopeMetrics {
		out = append(out, scope.Metrics...)
	}
	return out
}

func assertMetricPoint(t *testing.T, metrics []metricdata.Metrics, name string, want int64, outcome, errClass string) {
	t.Helper()
	for _, metric := range metrics {
		if metric.Name != name {
			continue
		}
		switch data := metric.Data.(type) {
		case metricdata.Sum[int64]:
			if len(data.DataPoints) != 1 || data.DataPoints[0].Value != want {
				t.Fatalf("%s points = %#v, want one value %d", name, data.DataPoints, want)
			}
			attrs := data.DataPoints[0].Attributes.ToSlice()
			if outcome != "" && !hasAttribute(attrs, AttributeOutcome, outcome) {
				t.Fatalf("%s attrs = %v, want outcome=%s", name, attrs, outcome)
			}
			if errClass != "" && !hasAttribute(attrs, AttributeErrorClass, errClass) {
				t.Fatalf("%s attrs = %v, want error_class=%s", name, attrs, errClass)
			}
		case metricdata.Histogram[float64]:
			if len(data.DataPoints) != 1 || data.DataPoints[0].Count != uint64(want) {
				t.Fatalf("%s points = %#v, want one count %d", name, data.DataPoints, want)
			}
		}
		return
	}
	t.Fatalf("metric %q not found", name)
}
