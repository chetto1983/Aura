package retention

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestBacklogObserverGlobalRegistrationAndNilSafety(t *testing.T) {
	oldProvider := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldProvider)
		_ = provider.Shutdown(context.Background())
	})
	observer, err := NewBacklogObserver(&fakeBacklogSource{count: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertBacklogGauge(t, reader, 2)
	observer.Stop()
	observer.Stop()
	var nilObserver *BacklogObserver
	nilObserver.Stop()
	if _, err := NewBacklogObserver(nil); err == nil {
		t.Fatal("nil backlog source succeeded")
	}
}

func TestBacklogObserverExportsCurrentDurableCount(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	source := &fakeBacklogSource{count: 7}
	observer, err := newBacklogObserver(provider.Meter(backlogObserverMeterName), source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Stop)

	assertBacklogGauge(t, reader, 7)
	source.count = 0
	assertBacklogGauge(t, reader, 0)
}

func TestBacklogObserverFailsSoftWithoutPublishingStaleData(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	source := &fakeBacklogSource{err: errors.New("query failed")}
	observer, err := newBacklogObserver(provider.Meter(backlogObserverMeterName), source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Stop)

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &resources); err != nil {
		t.Fatal(err)
	}
	for _, scope := range resources.ScopeMetrics {
		for _, collected := range scope.Metrics {
			if collected.Name == "aura.retention.backlog" {
				t.Fatalf("failed observation published stale backlog data: %#v", collected.Data)
			}
		}
	}
}

type fakeBacklogSource struct {
	count int64
	err   error
}

func (f *fakeBacklogSource) PendingCount(context.Context) (int64, error) {
	return f.count, f.err
}

func assertBacklogGauge(t *testing.T, reader *sdkmetric.ManualReader, want int64) {
	t.Helper()
	var resources metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &resources); err != nil {
		t.Fatal(err)
	}
	for _, scope := range resources.ScopeMetrics {
		for _, collected := range scope.Metrics {
			if collected.Name != "aura.retention.backlog" {
				continue
			}
			gauge, ok := collected.Data.(metricdata.Gauge[int64])
			if !ok || len(gauge.DataPoints) != 1 || gauge.DataPoints[0].Value != want {
				t.Fatalf("backlog metric = %#v, want one point %d", collected.Data, want)
			}
			if attrs := gauge.DataPoints[0].Attributes.ToSlice(); len(attrs) != 0 {
				t.Fatalf("backlog attributes = %v, want none", attrs)
			}
			return
		}
	}
	t.Fatalf("aura.retention.backlog metric missing; want %d", want)
}
