package documents

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/obs"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestIngestionTelemetryRecordsJobOutcomesAndQueueDepth(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	telemetry := newIngestionTelemetry(provider.Meter("aura.documents.test"))

	telemetry.recordJobOutcome(context.Background(), ingestionJobStatusSucceeded)
	telemetry.recordJobOutcome(context.Background(), ingestionJobStatusDeadLetter)
	telemetry.recordJobOutcome(context.Background(), ingestionJobOutcomeRetryScheduled)
	telemetry.recordQueueDepth(context.Background(), 7)

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resources); err != nil {
		t.Fatal(err)
	}

	outcomes := map[string]int64{}
	var gaugeValue int64
	var gaugeSeen bool
	for _, scope := range resources.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case obs.MustDescriptor(obs.IngestionJobsID).Name:
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("jobs metric type = %T", m.Data)
				}
				for _, point := range sum.DataPoints {
					for _, attr := range point.Attributes.ToSlice() {
						if string(attr.Key) == "outcome" {
							outcomes[attr.Value.AsString()] += point.Value
						}
					}
				}
			case obs.MustDescriptor(obs.IngestionQueueDepthID).Name:
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("queue depth metric type = %T", m.Data)
				}
				for _, point := range gauge.DataPoints {
					gaugeValue = point.Value
					gaugeSeen = true
				}
			}
		}
	}
	if outcomes["succeeded"] != 1 || outcomes["dead_letter"] != 1 || outcomes["retry_scheduled"] != 1 {
		t.Fatalf("outcome counts = %#v", outcomes)
	}
	if !gaugeSeen || gaugeValue != 7 {
		t.Fatalf("gauge value = %d seen=%v", gaugeValue, gaugeSeen)
	}
}

func TestRecordIngestionHelpersDoNotPanicAgainstGlobalTelemetry(t *testing.T) {
	// The package-level helpers route through the process-global OTel provider
	// (noop unless obs.Init was called); this only guards against panics/nil
	// derefs in the wiring, mirroring how retention exercises its global var.
	recordIngestionJobOutcome(context.Background(), ingestionJobStatusSucceeded)
	recordIngestionQueueDepth(context.Background(), 0)
}
