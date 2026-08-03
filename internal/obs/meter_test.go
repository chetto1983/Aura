package obs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestMeterRuntimeModes(t *testing.T) {
	tests := []struct {
		name        string
		prometheus  bool
		wantHandler bool
	}{
		{name: "disabled"},
		{name: "prometheus", prometheus: true, wantHandler: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := buildMeterRuntime(meterOptions{
				resource:         resource.Empty(),
				enablePrometheus: tt.prometheus,
				registry:         prometheus.NewRegistry(),
			})
			if err != nil {
				t.Fatalf("buildMeterRuntime: %v", err)
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
