package retention

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRetentionTelemetryEmitsBoundedItemSeries(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	telemetry := newRetentionTelemetry(provider.Meter("aura.retention.test"))

	telemetry.recordItems(context.Background(), "pending", "accepted", 3)
	telemetry.recordItems(context.Background(), "completed", "success", 2)
	telemetry.recordItems(context.Background(), "completed", "error", 1)

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resources); err != nil {
		t.Fatal(err)
	}
	points := int64(0)
	for _, scope := range resources.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != obs.MustDescriptor(obs.RetentionOperationsID).Name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("retention operation metric type = %T", metric.Data)
			}
			for _, point := range sum.DataPoints {
				attrs := point.Attributes.ToSlice()
				if !hasRetentionAttribute(attrs, "operation", "retention_delete") {
					t.Fatalf("retention point attrs = %v, want operation=retention_delete", attrs)
				}
				points += point.Value
			}
		}
	}
	if points != 6 {
		t.Fatalf("retention item total = %d, want 6", points)
	}
}

func hasRetentionAttribute(attrs []attribute.KeyValue, key, value string) bool {
	for _, attr := range attrs {
		if string(attr.Key) == key && attr.Value.AsString() == value {
			return true
		}
	}
	return false
}
