package idempotency

import (
	"context"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const idempotencyMeterName = "github.com/chetto1983/aura/internal/idempotency"

type idempotencyTelemetry struct {
	operations metric.Int64Counter
}

func newIdempotencyTelemetry(meter metric.Meter) *idempotencyTelemetry {
	descriptor := obs.MustDescriptor(obs.IdempotencyOperationsID)
	operations, err := meter.Int64Counter(
		descriptor.Name,
		metric.WithDescription(descriptor.Description),
		metric.WithUnit(descriptor.Unit),
	)
	if err != nil {
		panic(err)
	}
	return &idempotencyTelemetry{operations: operations}
}

func (t *idempotencyTelemetry) recordBegin(ctx context.Context, decision BeginDecision) {
	if t == nil {
		return
	}
	switch decision.Decision {
	case DecisionAcquired:
		t.record(ctx, "idempotency_reserve", "in_progress", "success")
	case DecisionReplay:
		t.record(ctx, "idempotency_replay", "completed", "replayed")
	case DecisionResultExpired:
		t.record(ctx, "idempotency_replay", "expired", "skipped")
	case DecisionInProgress:
		t.record(ctx, "idempotency_reserve", "in_progress", "in_progress")
	case DecisionIndeterminate:
		t.record(ctx, "idempotency_reserve", "indeterminate", "indeterminate")
	case DecisionConflict:
		t.record(ctx, "idempotency_reserve", "failed", "conflict")
	default:
		t.record(ctx, "idempotency_reserve", "failed", "error")
	}
}

func (t *idempotencyTelemetry) recordCompletion(ctx context.Context, err error) {
	if err != nil {
		t.record(ctx, "idempotency_complete", "failed", "error")
		return
	}
	t.record(ctx, "idempotency_complete", "completed", "success")
}

func (t *idempotencyTelemetry) recordIndeterminate(ctx context.Context, err error) {
	if err != nil {
		t.record(ctx, "idempotency_complete", "failed", "error")
		return
	}
	t.record(ctx, "idempotency_complete", "indeterminate", "indeterminate")
}

func (t *idempotencyTelemetry) record(ctx context.Context, operation, state, outcome string) {
	if t == nil {
		return
	}
	t.operations.Add(ctx, 1, metric.WithAttributes(
		attribute.String(string(obs.AttributeOperation), obs.NormalizeAttribute(obs.AttributeOperation, operation)),
		attribute.String(string(obs.AttributeState), obs.NormalizeAttribute(obs.AttributeState, state)),
		attribute.String(string(obs.AttributeOutcome), obs.NormalizeAttribute(obs.AttributeOutcome, outcome)),
	))
}
