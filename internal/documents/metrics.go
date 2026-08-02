package documents

import (
	"context"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const ingestionMeterName = "github.com/chetto1983/aura/internal/documents"

// ingestionTelemetry records the durable ingestion job outcomes and the
// queue-depth backlog. It mirrors internal/retention's telemetry shape
// (catalog-owned descriptors, a package-global instance backed by the process
// OTel provider, and a private constructor tests can build directly against a
// manual reader).
type ingestionTelemetry struct {
	jobs       metric.Int64Counter
	queueDepth metric.Int64Gauge
}

var globalIngestionTelemetry = newIngestionTelemetry(otel.Meter(ingestionMeterName))

func newIngestionTelemetry(meter metric.Meter) ingestionTelemetry {
	jobsDescriptor := obs.MustDescriptor(obs.IngestionJobsID)
	jobs, err := meter.Int64Counter(jobsDescriptor.Name,
		metric.WithDescription(jobsDescriptor.Description), metric.WithUnit(jobsDescriptor.Unit))
	if err != nil {
		panic(err)
	}
	depthDescriptor := obs.MustDescriptor(obs.IngestionQueueDepthID)
	queueDepth, err := meter.Int64Gauge(depthDescriptor.Name,
		metric.WithDescription(depthDescriptor.Description), metric.WithUnit(depthDescriptor.Unit))
	if err != nil {
		panic(err)
	}
	return ingestionTelemetry{jobs: jobs, queueDepth: queueDepth}
}

func (t ingestionTelemetry) recordJobOutcome(ctx context.Context, outcome string) {
	t.jobs.Add(ctx, 1, metric.WithAttributes(
		attribute.String(string(obs.AttributeOutcome), obs.NormalizeAttribute(obs.AttributeOutcome, outcome)),
	))
}

func (t ingestionTelemetry) recordQueueDepth(ctx context.Context, depth int64) {
	t.queueDepth.Record(ctx, depth)
}

func recordIngestionJobOutcome(ctx context.Context, outcome string) {
	globalIngestionTelemetry.recordJobOutcome(ctx, outcome)
}

func recordIngestionQueueDepth(ctx context.Context, depth int64) {
	globalIngestionTelemetry.recordQueueDepth(ctx, depth)
}
