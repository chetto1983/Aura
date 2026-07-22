package obs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestMeterRuntimeModes(t *testing.T) {
	tests := []struct {
		name        string
		prometheus  bool
		otlp        bool
		wantProm    int32
		wantOTLP    int32
		wantHandler bool
	}{
		{name: "disabled"},
		{name: "prometheus only", prometheus: true, wantProm: 1, wantHandler: true},
		{name: "otlp only", otlp: true, wantOTLP: 1},
		{name: "dual fanout", prometheus: true, otlp: true, wantProm: 1, wantOTLP: 1, wantHandler: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var promCalls, otlpCalls atomic.Int32
			factories := meterFactories{
				prometheus: func(*prometheus.Registry) (readerComponent, error) {
					promCalls.Add(1)
					return readerComponent{reader: sdkmetric.NewManualReader(), handler: http.NotFoundHandler()}, nil
				},
				otlp: func(context.Context, string) (readerComponent, error) {
					otlpCalls.Add(1)
					return readerComponent{reader: sdkmetric.NewManualReader()}, nil
				},
			}
			runtime, err := buildMeterRuntime(context.Background(), meterOptions{
				resource:         resource.Empty(),
				enablePrometheus: tt.prometheus,
				enableOTLP:       tt.otlp,
				registry:         prometheus.NewRegistry(),
				factories:        factories,
			})
			if err != nil {
				t.Fatalf("buildMeterRuntime: %v", err)
			}
			if got := promCalls.Load(); got != tt.wantProm {
				t.Errorf("prometheus factory calls = %d, want %d", got, tt.wantProm)
			}
			if got := otlpCalls.Load(); got != tt.wantOTLP {
				t.Errorf("OTLP factory calls = %d, want %d", got, tt.wantOTLP)
			}
			if got := runtime.handler != nil; got != tt.wantHandler {
				t.Errorf("handler present = %v, want %v", got, tt.wantHandler)
			}
			if err := runtime.shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
		})
	}
}

func TestMeterRuntimeDualReaderReceivesOneLogicalMeasurement(t *testing.T) {
	promReader := sdkmetric.NewManualReader()
	otlpReader := sdkmetric.NewManualReader()
	runtime, err := buildMeterRuntime(context.Background(), meterOptions{
		resource:         resource.Empty(),
		enablePrometheus: true,
		enableOTLP:       true,
		registry:         prometheus.NewRegistry(),
		factories: meterFactories{
			prometheus: func(*prometheus.Registry) (readerComponent, error) {
				return readerComponent{reader: promReader, handler: http.NotFoundHandler()}, nil
			},
			otlp: func(context.Context, string) (readerComponent, error) {
				return readerComponent{reader: otlpReader}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("buildMeterRuntime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.shutdown(context.Background()) })

	counter, err := runtime.provider.Meter("aura.test").Int64Counter("aura.test.dual")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	for name, reader := range map[string]*sdkmetric.ManualReader{"prometheus": promReader, "otlp": otlpReader} {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("%s Collect: %v", name, err)
		}
		if got := metricPointCount(rm, "aura.test.dual"); got != 1 {
			t.Errorf("%s point count = %d, want 1", name, got)
		}
	}
}

func TestMeterRuntimePartialExporterFailureCleansConstructedComponents(t *testing.T) {
	wantErr := errors.New("otlp exporter unavailable")
	var promCleanup atomic.Int32
	runtime, err := buildMeterRuntime(context.Background(), meterOptions{
		resource:         resource.Empty(),
		enablePrometheus: true,
		enableOTLP:       true,
		registry:         prometheus.NewRegistry(),
		factories: meterFactories{
			prometheus: func(*prometheus.Registry) (readerComponent, error) {
				return readerComponent{
					reader:  sdkmetric.NewManualReader(),
					cleanup: func(context.Context) error { promCleanup.Add(1); return nil },
				}, nil
			},
			otlp: func(context.Context, string) (readerComponent, error) {
				return readerComponent{}, wantErr
			},
		},
	})
	if runtime != nil {
		t.Fatal("partial failure returned a live runtime")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if got := promCleanup.Load(); got != 1 {
		t.Fatalf("prometheus cleanup calls = %d, want 1", got)
	}
}

func TestShutdownStackReverseOrderRepeatSafeAndDeadlineAware(t *testing.T) {
	var order []string
	stack := newShutdownStack(
		func(ctx context.Context) error { order = append(order, "trace"); return ctx.Err() },
		func(ctx context.Context) error { order = append(order, "meter"); return ctx.Err() },
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stack(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown error = %v, want context.Canceled", err)
	}
	if got := strings.Join(order, ","); got != "meter,trace" {
		t.Fatalf("shutdown order = %q, want meter,trace", got)
	}
	if err := stack(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeat shutdown error = %v, want cached context.Canceled", err)
	}
	if got := strings.Join(order, ","); got != "meter,trace" {
		t.Fatalf("repeat shutdown reran components: %q", got)
	}
}

func TestPrometheusReaderUsesDedicatedRegistryAndHandler(t *testing.T) {
	registry := prometheus.NewRegistry()
	component, err := newPrometheusComponent(registry)
	if err != nil {
		t.Fatalf("newPrometheusComponent: %v", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(component.reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	counter, err := provider.Meter("aura.test").Int64Counter("aura.test.private_counter")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	component.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aura_test_private_counter") {
		t.Fatalf("private scrape status/body = %d/%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "otel_scope_") {
		t.Fatalf("private scrape exposed unbounded OTel scope labels: %q", rec.Body.String())
	}
	defaultFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("default Gather: %v", err)
	}
	for _, family := range defaultFamilies {
		if strings.Contains(family.GetName(), "aura_test_private_counter") {
			t.Fatalf("OTel collector leaked into DefaultGatherer as %q", family.GetName())
		}
	}
}

func metricPointCount(rm metricdata.ResourceMetrics, name string) int {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			switch data := metric.Data.(type) {
			case metricdata.Sum[int64]:
				return len(data.DataPoints)
			case metricdata.Sum[float64]:
				return len(data.DataPoints)
			}
		}
	}
	return 0
}

func TestMeterRuntimeShutdownHonorsBoundedContext(t *testing.T) {
	deadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var sawDeadline bool
	stack := newShutdownStack(func(ctx context.Context) error {
		got, ok := ctx.Deadline()
		sawDeadline = ok && got.Equal(deadline)
		return nil
	})
	if err := stack(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !sawDeadline {
		t.Fatal("shutdown component did not receive caller deadline")
	}
}
