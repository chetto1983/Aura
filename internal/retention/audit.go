package retention

import (
	"context"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AuditSummary intentionally contains no identifiers, paths, content, or errors.
type AuditSummary struct {
	Mode           string
	PolicyVersion  string
	Planned        int
	PlannedBytes   int64
	Completed      int
	CompletedBytes int64
	Retryable      int
	Failed         int
	FailureClasses map[string]int
}

// AuditSink records content-free retention summaries.
type AuditSink interface {
	Record(context.Context, AuditSummary)
}

type retentionMetrics interface {
	recordItems(context.Context, string, string, int64)
	recordBytes(context.Context, int64)
}

type retentionTelemetry struct {
	operations metric.Int64Counter
	bytes      metric.Int64Counter
}

var globalRetentionTelemetry retentionMetrics = newRetentionTelemetry(otel.Meter("aura/retention"))

func newRetentionTelemetry(meter metric.Meter) retentionMetrics {
	operationsDescriptor := obs.MustDescriptor(obs.RetentionOperationsID)
	operations, err := meter.Int64Counter(
		operationsDescriptor.Name,
		metric.WithDescription(operationsDescriptor.Description),
		metric.WithUnit(operationsDescriptor.Unit),
	)
	if err != nil {
		panic(err)
	}
	bytesDescriptor := obs.MustDescriptor(obs.RetentionBytesID)
	bytesCounter, err := meter.Int64Counter(
		bytesDescriptor.Name,
		metric.WithDescription(bytesDescriptor.Description),
		metric.WithUnit(bytesDescriptor.Unit),
	)
	if err != nil {
		panic(err)
	}
	return &retentionTelemetry{operations: operations, bytes: bytesCounter}
}

func (t *retentionTelemetry) recordItems(ctx context.Context, state, outcome string, count int64) {
	if t == nil || count <= 0 {
		return
	}
	t.operations.Add(ctx, count, metric.WithAttributes(
		attribute.String(string(obs.AttributeOperation), "retention_delete"),
		attribute.String(string(obs.AttributeState), obs.NormalizeAttribute(obs.AttributeState, state)),
		attribute.String(string(obs.AttributeOutcome), obs.NormalizeAttribute(obs.AttributeOutcome, outcome)),
		attribute.String(string(obs.AttributeErrorClass), func() string {
			if outcome == "error" {
				return "internal"
			}
			return "none"
		}()),
	))
}

func (t *retentionTelemetry) recordBytes(ctx context.Context, bytes int64) {
	if t == nil || bytes <= 0 {
		return
	}
	t.bytes.Add(ctx, bytes, metric.WithAttributes(
		attribute.String(string(obs.AttributeOperation), "retention_delete"),
		attribute.String(string(obs.AttributeOutcome), "success"),
	))
}

func recordRetentionBytes(ctx context.Context, bytes int64) {
	globalRetentionTelemetry.recordBytes(ctx, bytes)
}
