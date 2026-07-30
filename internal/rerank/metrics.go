package rerank

import (
	"context"
	"time"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const rerankMeterName = "github.com/chetto1983/aura/internal/rerank"

type rerankTelemetry struct {
	calls    metric.Int64Counter
	duration metric.Float64Histogram
}

var globalRerankTelemetry = newRerankTelemetry(otel.Meter(rerankMeterName))

func newRerankTelemetry(meter metric.Meter) rerankTelemetry {
	callsDescriptor := obs.MustDescriptor(obs.RerankCallsID)
	calls, err := meter.Int64Counter(
		callsDescriptor.Name,
		metric.WithDescription(callsDescriptor.Description),
		metric.WithUnit(callsDescriptor.Unit),
	)
	if err != nil {
		panic(err)
	}
	durationDescriptor := obs.MustDescriptor(obs.RerankDurationID)
	duration, err := meter.Float64Histogram(
		durationDescriptor.Name,
		metric.WithDescription(durationDescriptor.Description),
		metric.WithUnit(durationDescriptor.Unit),
	)
	if err != nil {
		panic(err)
	}
	return rerankTelemetry{calls: calls, duration: duration}
}

func (telemetry rerankTelemetry) record(
	ctx context.Context,
	elapsed time.Duration,
	outcome string,
	errorClass string,
) {
	outcome = obs.NormalizeAttribute(obs.AttributeOutcome, outcome)
	telemetry.calls.Add(ctx, 1, metric.WithAttributes(
		attribute.String(string(obs.AttributeOutcome), outcome),
		attribute.String(
			string(obs.AttributeErrorClass),
			obs.NormalizeAttribute(obs.AttributeErrorClass, errorClass),
		),
	))
	telemetry.duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attribute.String(string(obs.AttributeOutcome), outcome),
	))
}

func (c *RerankClient) activeTelemetry() rerankTelemetry {
	if c.telemetry != nil {
		return *c.telemetry
	}
	return globalRerankTelemetry
}

func rerankTransportErrorClass(err error) string {
	class := obs.ErrorClass(err)
	if class == obs.ValueOther || class == "internal" {
		return "unavailable"
	}
	return class
}
