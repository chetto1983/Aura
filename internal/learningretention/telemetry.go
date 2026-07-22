package learningretention

import (
	"context"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const learningMeterName = "github.com/chetto1983/aura/internal/learningretention"

type telemetry struct {
	operations metric.Int64Counter
	size       metric.Int64Gauge
	oldestAge  metric.Float64Gauge
}

var globalTelemetry = newTelemetry(otel.Meter(learningMeterName))

// RecordMetric emits one Plan-04-catalog event through the process-global OTel
// provider. Every dimension is normalized again at this final export boundary.
func RecordMetric(event Metric) { globalTelemetry.record(event) }

func newTelemetry(meter metric.Meter) telemetry {
	return telemetry{
		operations: mustCounter(meter, obs.LearningOperationsID),
		size:       mustIntGauge(meter, obs.LearningSizeID),
		oldestAge:  mustFloatGauge(meter, obs.LearningOldestAgeID),
	}
}

func (t telemetry) record(event Metric) {
	ctx := context.Background()
	toolClass := obs.NormalizeAttribute(obs.AttributeToolClass, event.ToolClass)
	state := obs.NormalizeAttribute(obs.AttributeState, event.State)
	t.operations.Add(ctx, 1, metric.WithAttributes(
		attribute.String(string(obs.AttributeOperation), obs.NormalizeAttribute(obs.AttributeOperation, event.Operation)),
		attribute.String(string(obs.AttributeToolClass), toolClass),
		attribute.String(string(obs.AttributeOutcome), obs.NormalizeAttribute(obs.AttributeOutcome, event.Outcome)),
		attribute.String(string(obs.AttributeErrorClass), obs.NormalizeAttribute(obs.AttributeErrorClass, event.ErrorClass)),
	))
	if event.Size >= 0 {
		t.size.Record(ctx, int64(event.Size), metric.WithAttributes(
			attribute.String(string(obs.AttributeToolClass), toolClass),
			attribute.String(string(obs.AttributeState), state),
		))
	}
	t.oldestAge.Record(ctx, event.OldestAge.Seconds(), metric.WithAttributes(
		attribute.String(string(obs.AttributeToolClass), toolClass),
		attribute.String(string(obs.AttributeState), state),
	))
}

func mustCounter(meter metric.Meter, id obs.InstrumentID) metric.Int64Counter {
	descriptor, ok := obs.DescriptorFor(id)
	if !ok {
		panic("missing learning counter descriptor")
	}
	instrument, err := meter.Int64Counter(descriptor.Name, metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return instrument
}

func mustIntGauge(meter metric.Meter, id obs.InstrumentID) metric.Int64Gauge {
	descriptor, ok := obs.DescriptorFor(id)
	if !ok {
		panic("missing learning size descriptor")
	}
	instrument, err := meter.Int64Gauge(descriptor.Name, metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return instrument
}

func mustFloatGauge(meter metric.Meter, id obs.InstrumentID) metric.Float64Gauge {
	descriptor, ok := obs.DescriptorFor(id)
	if !ok {
		panic("missing learning age descriptor")
	}
	instrument, err := meter.Float64Gauge(descriptor.Name, metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return instrument
}
