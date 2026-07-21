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

func recordRetentionBytes(ctx context.Context, bytes int64) {
	if bytes <= 0 {
		return
	}
	descriptor := obs.MustDescriptor(obs.RetentionBytesID)
	counter, err := otel.Meter("aura/retention").Int64Counter(
		descriptor.Name, metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit),
	)
	if err != nil {
		return
	}
	counter.Add(ctx, bytes, metric.WithAttributes(
		attribute.String(string(obs.AttributeOperation), "retention_delete"),
		attribute.String(string(obs.AttributeOutcome), "success"),
	))
}
