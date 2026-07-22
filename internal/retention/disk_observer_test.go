package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/goleak"
)

func TestDiskCapacityRatioAndPlatformSampler(t *testing.T) {
	if _, err := ratioFromCapacity(0, 0); err == nil {
		t.Fatal("ratioFromCapacity with zero capacity succeeded")
	}
	for _, tt := range []struct {
		total, available uint64
		want             float64
	}{
		{total: 100, available: 50, want: 0.5},
		{total: 100, available: 100, want: 0},
		{total: 100, available: 101, want: 0},
	} {
		got, err := ratioFromCapacity(tt.total, tt.available)
		if err != nil || got != tt.want {
			t.Fatalf("ratioFromCapacity(%d, %d)=%v, %v; want %v", tt.total, tt.available, got, err, tt.want)
		}
	}
	ratio, err := sampleDiskUsage(t.TempDir())
	if err != nil || ratio < 0 || ratio > 1 {
		t.Fatalf("sampleDiskUsage=%v, %v; want bounded ratio", ratio, err)
	}
}

func TestDiskObserverValidationAndIdempotentLifecycle(t *testing.T) {
	thresholds := DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85}
	for _, cfg := range []DiskObserverConfig{
		{RunDir: "relative", Thresholds: thresholds},
		{RunDir: t.TempDir(), Thresholds: DiskThresholds{}},
		{RunDir: t.TempDir(), Thresholds: thresholds, Interval: -time.Second},
	} {
		if _, err := NewDiskObserver(cfg); err == nil {
			t.Fatalf("NewDiskObserver(%+v) succeeded", cfg)
		}
	}

	observer, err := NewDiskObserver(DiskObserverConfig{RunDir: t.TempDir(), Thresholds: thresholds})
	if err != nil {
		t.Fatal(err)
	}
	observer.Stop()
	observer.Stop()
	observer.Start(t.Context())
	var nilObserver *DiskObserver
	nilObserver.Start(t.Context())
	nilObserver.Stop()
}

func TestDiskObserverRejectsInvalidSamplesWithoutChangingState(t *testing.T) {
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var logs bytes.Buffer
	values := []float64{math.NaN(), math.Inf(1), -0.01, 1.01}
	index := 0
	observer, err := newDiskObserver(provider.Meter(diskObserverMeterName), DiskObserverConfig{
		RunDir: t.TempDir(), Thresholds: DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		SampleUsage: func(string) (float64, error) {
			value := values[index]
			index++
			return value, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Stop)
	for range values {
		observer.sample()
	}
	if observer.current.Load() != nil {
		t.Fatal("invalid sample replaced current observation")
	}
	if got := strings.Count(logs.String(), "invalid used ratio"); got != len(values) {
		t.Fatalf("invalid ratio warnings=%d, want %d", got, len(values))
	}
}

func TestDiskObserverClassifiesExactPressureBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
		state string
	}{
		{name: "below warn", ratio: 0.6999, state: "ready"},
		{name: "warn boundary", ratio: 0.70, state: "degraded"},
		{name: "below urgent", ratio: 0.7999, state: "degraded"},
		{name: "urgent boundary", ratio: 0.80, state: "draining"},
		{name: "below stop optional", ratio: 0.8499, state: "draining"},
		{name: "stop optional boundary", ratio: 0.85, state: "stopped"},
		{name: "full", ratio: 1, state: "stopped"},
	}
	thresholds := DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diskMetricState(tt.ratio, thresholds); got != tt.state {
				t.Fatalf("diskMetricState(%v) = %q, want %q", tt.ratio, got, tt.state)
			}
		})
	}
}

func TestDiskObserverEmitsOnlyCurrentStateAndControlsOptionalTraces(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var (
		mu         sync.Mutex
		ratio      = 0.70
		suppressed []bool
	)
	observer, err := newDiskObserver(provider.Meter(diskObserverMeterName), DiskObserverConfig{
		RunDir:     t.TempDir(),
		Thresholds: DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85},
		Interval:   time.Hour,
		SampleUsage: func(string) (float64, error) {
			mu.Lock()
			defer mu.Unlock()
			return ratio, nil
		},
		SetOptionalTracesSuppressed: func(value bool) {
			mu.Lock()
			defer mu.Unlock()
			suppressed = append(suppressed, value)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observer.Start(t.Context())
	t.Cleanup(observer.Stop)

	assertDiskGauge(t, reader, 0.70, "degraded")
	mu.Lock()
	ratio = 0.85
	mu.Unlock()
	observer.sample()
	assertDiskGauge(t, reader, 0.85, "stopped")
	mu.Lock()
	ratio = 0.84
	mu.Unlock()
	observer.sample()
	assertDiskGauge(t, reader, 0.84, "draining")

	mu.Lock()
	gotSuppressed := append([]bool(nil), suppressed...)
	mu.Unlock()
	if len(gotSuppressed) != 3 || gotSuppressed[0] || !gotSuppressed[1] || gotSuppressed[2] {
		t.Fatalf("optional trace suppression transitions = %v, want [false true false]", gotSuppressed)
	}
}

func TestDiskObserverSamplingFailureIsVisibleAndFailSoft(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	failing := false
	suppressed := false
	observer, err := newDiskObserver(provider.Meter(diskObserverMeterName), DiskObserverConfig{
		RunDir:     t.TempDir(),
		Thresholds: DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85},
		Interval:   time.Hour,
		Logger:     logger,
		SampleUsage: func(string) (float64, error) {
			if failing {
				return 0, errors.New("disk sampler sentinel")
			}
			return 0.85, nil
		},
		SetOptionalTracesSuppressed: func(value bool) { suppressed = value },
	})
	if err != nil {
		t.Fatal(err)
	}
	observer.Start(t.Context())
	t.Cleanup(observer.Stop)
	if !suppressed {
		t.Fatal("stop-optional sample did not suppress optional traces")
	}

	failing = true
	observer.sample()
	if !suppressed {
		t.Fatal("sampling failure changed the last safe suppression decision")
	}
	assertDiskGauge(t, reader, 0.85, "stopped")
	if got := logs.String(); !strings.Contains(got, "disk sampler sentinel") || !strings.Contains(got, "retention disk observation failed") {
		t.Fatalf("sampling failure log = %q, want visible fail-soft warning", got)
	}
}

func TestDiskObserverPeriodicLifecycleStopsAndJoins(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var samples atomic.Int64
	observer, err := newDiskObserver(provider.Meter(diskObserverMeterName), DiskObserverConfig{
		RunDir:     t.TempDir(),
		Thresholds: DiskThresholds{Warn: 70, Urgent: 80, StopOptional: 85},
		Interval:   time.Millisecond,
		SampleUsage: func(string) (float64, error) {
			samples.Add(1)
			return 0.10, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observer.Start(t.Context())
	deadline := time.Now().Add(time.Second)
	for samples.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if samples.Load() < 2 {
		t.Fatalf("periodic observer samples = %d, want immediate plus tick", samples.Load())
	}
	observer.Stop()
	stoppedAt := samples.Load()
	time.Sleep(10 * time.Millisecond)
	if got := samples.Load(); got != stoppedAt {
		t.Fatalf("observer sampled after Stop: before=%d after=%d", stoppedAt, got)
	}
}

func assertDiskGauge(t *testing.T, reader *sdkmetric.ManualReader, wantRatio float64, wantState string) {
	t.Helper()
	var resources metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &resources); err != nil {
		t.Fatal(err)
	}
	descriptor := obs.MustDescriptor(obs.RetentionDiskUtilizationID)
	points := 0
	for _, scope := range resources.ScopeMetrics {
		for _, collected := range scope.Metrics {
			if collected.Name != descriptor.Name {
				continue
			}
			gauge, ok := collected.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("disk utilization metric type = %T, want float64 gauge", collected.Data)
			}
			points += len(gauge.DataPoints)
			for _, point := range gauge.DataPoints {
				if point.Value != wantRatio {
					t.Fatalf("disk utilization = %v, want %v", point.Value, wantRatio)
				}
				if !hasDiskAttribute(point.Attributes.ToSlice(), string(obs.AttributeState), wantState) {
					t.Fatalf("disk utilization attrs = %v, want state=%s", point.Attributes.ToSlice(), wantState)
				}
			}
		}
	}
	if points != 1 {
		t.Fatalf("disk utilization points = %d, want exactly one current-state series", points)
	}
}

func hasDiskAttribute(attrs []attribute.KeyValue, key, value string) bool {
	for _, attr := range attrs {
		if string(attr.Key) == key && attr.Value.AsString() == value {
			return true
		}
	}
	return false
}
