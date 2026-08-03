package obs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestBoundaryRejectsInvalidDescriptorsAndCoversGlobalLifecycle(t *testing.T) {
	meter := metricnoop.NewMeterProvider().Meter("test")
	tracer := tracenoop.NewTracerProvider().Tracer("test")
	for name, test := range map[string]func() error{
		"nil meter": func() error {
			_, err := NewBoundary(nil, tracer, BoundaryConfig{Count: MCPCallsID})
			return err
		},
		"nil tracer": func() error {
			_, err := NewBoundary(meter, nil, BoundaryConfig{Count: MCPCallsID})
			return err
		},
		"missing count": func() error {
			_, err := NewBoundary(meter, tracer, BoundaryConfig{Count: InstrumentID("missing")})
			return err
		},
		"non-counter count": func() error {
			_, err := NewBoundary(meter, tracer, BoundaryConfig{Count: MCPDurationID})
			return err
		},
		"missing duration": func() error {
			_, err := NewBoundary(meter, tracer, BoundaryConfig{Count: MCPCallsID, Duration: InstrumentID("missing")})
			return err
		},
		"non-histogram duration": func() error {
			_, err := NewBoundary(meter, tracer, BoundaryConfig{Count: MCPCallsID, Duration: MCPCallsID})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := test(); err == nil {
				t.Fatal("invalid boundary configuration succeeded")
			}
		})
	}

	oldMeter, oldTracer := otel.GetMeterProvider(), otel.GetTracerProvider()
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(traceProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		otel.SetTracerProvider(oldTracer)
		_ = meterProvider.Shutdown(context.Background())
		_ = traceProvider.Shutdown(context.Background())
	})

	boundary := NewGlobalBoundary("aura.test.global", BoundaryConfig{
		Operation: "retention_apply", Count: RetentionOperationsID, Duration: RetentionDurationID,
	})
	_, panicked := boundary.Start(nil) //nolint:staticcheck // Intentional nil-context normalization probe.
	panicked.EndPanic()
	_, succeeded := boundary.Start(nil) //nolint:staticcheck // Intentional nil-context normalization probe.
	succeeded.PanicSafe(nil)
	_, timedOut := boundary.Start(context.Background())
	timedOut.End(context.DeadlineExceeded)
	if got := len(recorder.Ended()); got != 3 {
		t.Fatalf("global boundary spans = %d, want 3", got)
	}

	var nilEnd *BoundaryEnd
	nilEnd.End(nil)
	(&BoundaryEnd{}).End(nil)
	if hasAttribute([]attribute.KeyValue{attribute.String("state", "ready")}, AttributeState, "missing") {
		t.Fatal("hasAttribute matched a missing value")
	}
}

func TestCatalogValidationRejectsEveryInvariantViolation(t *testing.T) {
	original := descriptors
	t.Cleanup(func() { descriptors = original })
	histogramIndex := -1
	for i := range original {
		if original[i].Kind == KindHistogram {
			histogramIndex = i
			break
		}
	}
	if histogramIndex < 0 {
		t.Fatal("catalog has no histogram fixture")
	}
	tests := map[string]func([]Descriptor){
		"incomplete":    func(candidate []Descriptor) { candidate[0].Description = "" },
		"duplicate":     func(candidate []Descriptor) { candidate[1].ID = candidate[0].ID },
		"non aura name": func(candidate []Descriptor) { candidate[0].Name = "foreign.metric" },
		"unbounded attribute": func(candidate []Descriptor) {
			candidate[0].Attributes = []AttributeKey{"tenant_id"}
		},
		"histogram without buckets": func(candidate []Descriptor) { candidate[histogramIndex].Buckets = nil },
		"unsorted histogram": func(candidate []Descriptor) {
			candidate[histogramIndex].Buckets = []float64{2, 1}
		},
		"duplicate histogram boundary": func(candidate []Descriptor) {
			candidate[histogramIndex].Buckets = []float64{1, 1}
		},
		"non histogram with buckets": func(candidate []Descriptor) { candidate[0].Buckets = []float64{1} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := append([]Descriptor(nil), original...)
			mutate(candidate)
			descriptors = candidate
			defer func() { descriptors = original }()
			if err := ValidateCatalog(); err == nil {
				t.Fatal("invalid catalog succeeded")
			}
		})
	}

	if _, ok := DescriptorFor(InstrumentID("missing")); ok {
		t.Fatal("missing descriptor resolved")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustDescriptor did not panic for a missing descriptor")
		}
	}()
	MustDescriptor(InstrumentID("missing"))
}

func TestCatalogClassifiersCoverFiniteVocabulary(t *testing.T) {
	tools := map[string]string{
		ToolClassWeb: ToolClassWeb, "approval_confirm": ToolClassInteraction,
		"browser_open": ToolClassWeb, "exec_command": ToolClassShell,
		"file_read": ToolClassFilesystem, "mcp_server": ToolClassMCP,
		"scheduler_tick": ToolClassScheduler, "postgres_query": ToolClassData,
		"calculator": ToolClassBuiltin, "unknown raw tool": ValueOther,
	}
	for input, want := range tools {
		if got := ClassifyTool(input); got != want {
			t.Errorf("ClassifyTool(%q)=%q, want %q", input, got, want)
		}
	}
	errorsByClass := map[string]string{
		"": "none", "none": "none", "operation canceled": "canceled",
		"deadline exceeded": "timeout", "breaker open": "unavailable",
		"invalid argument": "invalid", "write conflict": "conflict",
		"permission denied": "permission", "panic recovered": "panic",
		"internal stream failure": "internal", "opaque failure": ValueOther,
	}
	for input, want := range errorsByClass {
		if got := ClassifyError(input); got != want {
			t.Errorf("ClassifyError(%q)=%q, want %q", input, got, want)
		}
	}
	if ErrorClass(nil) != "none" || ErrorClass(errors.New("request timeout")) != "timeout" {
		t.Fatal("ErrorClass did not preserve finite nil/timeout classes")
	}
}

func TestTraceAndMeterConcreteFactories(t *testing.T) {
	for _, mode := range []string{exporterNone, exporterStdout, exporterOTLP} {
		t.Run(mode, func(t *testing.T) {
			provider, err := newTraceProvider(context.Background(), resource.Empty(), mode, "127.0.0.1:1")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := provider.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
	if provider, err := newTraceProvider(context.Background(), resource.Empty(), "unsupported", ""); err == nil || provider != nil {
		t.Fatalf("unsupported trace provider = %v, %v", provider, err)
	}
	if _, err := newPrometheusComponent(nil); err == nil {
		t.Fatal("nil Prometheus registry succeeded")
	}
}

func TestMeterConstructionFailuresCleanUpAndRuntimeAccessorsAreNilSafe(t *testing.T) {
	buildErr := errors.New("build failed")
	_, err := buildMeterRuntime(meterOptions{
		enablePrometheus: true,
		factories: meterFactories{prometheus: func(*prometheus.Registry) (readerComponent, error) {
			return readerComponent{}, buildErr
		}},
	})
	if !errors.Is(err, buildErr) {
		t.Fatalf("Prometheus build error = %v", err)
	}
	if err := newShutdownStack(nil)(context.Background()); err != nil {
		t.Fatal(err)
	}

	var nilRuntime *Runtime
	if nilRuntime.MetricsHandler() != nil || nilRuntime.Shutdown(context.Background()) != nil {
		t.Fatal("nil runtime accessors were not nil-safe")
	}
	handler := http.NotFoundHandler()
	runtime := &Runtime{handler: handler}
	if runtime.MetricsHandler() == nil || runtime.Shutdown(context.Background()) != nil {
		t.Fatal("runtime accessors lost handler or nil shutdown semantics")
	}
	if shutdown, err := Init(context.Background(), Config{OtelExporter: "unsupported"}); err == nil || shutdown != nil {
		t.Fatalf("unsupported Init = %T, %v", shutdown, err)
	}
	privateRuntime, err := InitRuntime(context.Background(), Config{OtelExporter: exporterNone, EnablePrometheus: true})
	if err != nil {
		t.Fatal(err)
	}
	if privateRuntime.MetricsHandler() == nil {
		t.Fatal("Prometheus runtime has no private handler")
	}
	if err := privateRuntime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	type benignValue struct{ Ready bool }
	if got := redactValue(slog.AnyValue(benignValue{Ready: true})); got.Any() != (benignValue{Ready: true}) {
		t.Fatalf("non-sensitive any value changed: %v", got)
	}
	group := redactValue(slog.GroupValue(slog.Bool("ready", true)))
	if group.Kind() != slog.KindGroup || len(group.Group()) != 1 || !group.Group()[0].Value.Bool() {
		t.Fatalf("non-sensitive group changed: %v", group)
	}
}
