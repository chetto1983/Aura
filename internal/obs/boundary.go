package obs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// BoundaryConfig binds one finite operation to its count and optional duration
// descriptors. ToolClass and Transport are normalized before use.
type BoundaryConfig struct {
	Operation string
	ToolClass string
	Transport string
	State     string
	Count     InstrumentID
	Duration  InstrumentID
}

// Boundary records one runtime boundary through metrics and a same-context span.
type Boundary struct {
	base      map[AttributeKey]string
	count     metric.Int64Counter
	duration  metric.Float64Histogram
	inFlight  metric.Int64UpDownCounter
	tracer    trace.Tracer
	scope     string
	spanName  string
	countDesc Descriptor
	durDesc   Descriptor
}

// BoundaryEnd owns one started boundary and makes every terminal path exactly once.
type BoundaryEnd struct {
	boundary *Boundary
	ctx      context.Context
	span     trace.Span
	started  time.Time
	once     sync.Once
}

// NewBoundary constructs a recorder from catalog-owned descriptors.
func NewBoundary(meter metric.Meter, tracer trace.Tracer, cfg BoundaryConfig) (*Boundary, error) {
	if meter == nil || tracer == nil {
		return nil, errors.New("boundary recorder requires meter and tracer")
	}
	countDesc, ok := DescriptorFor(cfg.Count)
	if !ok || countDesc.Kind != KindCounter {
		return nil, fmt.Errorf("boundary count descriptor %q is not a counter", cfg.Count)
	}
	var duration metric.Float64Histogram
	var durDesc Descriptor
	if cfg.Duration != "" {
		durDesc, ok = DescriptorFor(cfg.Duration)
		if !ok || durDesc.Kind != KindHistogram {
			return nil, fmt.Errorf("boundary duration descriptor %q is not a histogram", cfg.Duration)
		}
		var err error
		duration, err = meter.Float64Histogram(durDesc.Name,
			metric.WithDescription(durDesc.Description), metric.WithUnit(durDesc.Unit))
		if err != nil {
			return nil, err
		}
	}
	count, err := meter.Int64Counter(countDesc.Name,
		metric.WithDescription(countDesc.Description), metric.WithUnit(countDesc.Unit))
	if err != nil {
		return nil, err
	}
	inFlightDesc := MustDescriptor(BoundaryInFlightID)
	inFlight, err := meter.Int64UpDownCounter(inFlightDesc.Name,
		metric.WithDescription(inFlightDesc.Description), metric.WithUnit(inFlightDesc.Unit))
	if err != nil {
		return nil, err
	}
	operation := NormalizeAttribute(AttributeOperation, cfg.Operation)
	return &Boundary{
		base: map[AttributeKey]string{
			AttributeOperation: operation,
			AttributeToolClass: NormalizeAttribute(AttributeToolClass, cfg.ToolClass),
			AttributeTransport: NormalizeAttribute(AttributeTransport, cfg.Transport),
			AttributeState:     NormalizeAttribute(AttributeState, cfg.State),
		},
		count: count, duration: duration, inFlight: inFlight, tracer: tracer,
		spanName: operation, countDesc: countDesc, durDesc: durDesc,
	}, nil
}

// NewGlobalBoundary constructs a boundary from the process-global OTel providers.
// Catalog/configuration errors are programmer bugs and panic during package init.
func NewGlobalBoundary(scope string, cfg BoundaryConfig) *Boundary {
	boundary, err := NewBoundary(otel.Meter(scope), otel.Tracer(scope), cfg)
	if err != nil {
		panic(err)
	}
	boundary.scope = scope
	return boundary
}

// Start increments the in-flight series and returns a same-context span/end handle.
func (b *Boundary) Start(ctx context.Context) (context.Context, *BoundaryEnd) {
	if ctx == nil {
		ctx = context.Background()
	}
	baseAttrs := b.attributes(MustDescriptor(BoundaryInFlightID), nil)
	b.inFlight.Add(ctx, 1, metric.WithAttributes(baseAttrs...))
	tracer := b.tracer
	if b.scope != "" {
		tracer = otel.Tracer(b.scope)
	}
	spanCtx, span := tracer.Start(ctx, b.spanName, trace.WithAttributes(baseAttrs...))
	return spanCtx, &BoundaryEnd{boundary: b, ctx: spanCtx, span: span, started: time.Now()}
}

// End records a normal terminal result. Repeated calls are ignored.
func (e *BoundaryEnd) End(err error) {
	e.finish(err, false)
}

// EndPanic records a panic terminal class when a caller recovers outside the
// immediate boundary scope. Repeated calls, including a later End, are ignored.
func (e *BoundaryEnd) EndPanic() {
	e.finish(nil, true)
}

// PanicSafe is intended for defer. It records a panic class without persisting
// the recovered value, then re-panics so application semantics are unchanged.
func (e *BoundaryEnd) PanicSafe(err *error) {
	if recovered := recover(); recovered != nil {
		e.finish(nil, true)
		panic(recovered)
	}
	if err == nil {
		e.finish(nil, false)
		return
	}
	e.finish(*err, false)
}

func (e *BoundaryEnd) finish(err error, panicked bool) {
	if e == nil || e.boundary == nil {
		return
	}
	e.once.Do(func() {
		outcome, errorClass := terminalClasses(err, panicked)
		terminal := map[AttributeKey]string{AttributeOutcome: outcome, AttributeErrorClass: errorClass}
		countAttrs := e.boundary.attributes(e.boundary.countDesc, terminal)
		e.boundary.count.Add(e.ctx, 1, metric.WithAttributes(countAttrs...))
		if e.boundary.duration != nil {
			durationAttrs := e.boundary.attributes(e.boundary.durDesc, terminal)
			e.boundary.duration.Record(e.ctx, time.Since(e.started).Seconds(), metric.WithAttributes(durationAttrs...))
		}
		baseAttrs := e.boundary.attributes(MustDescriptor(BoundaryInFlightID), nil)
		e.boundary.inFlight.Add(e.ctx, -1, metric.WithAttributes(baseAttrs...))
		e.span.SetAttributes(countAttrs...)
		if outcome == "success" {
			e.span.SetStatus(codes.Ok, "")
		} else {
			e.span.SetStatus(codes.Error, errorClass)
		}
		e.span.End()
	})
}

func (b *Boundary) attributes(descriptor Descriptor, terminal map[AttributeKey]string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(descriptor.Attributes))
	for _, key := range descriptor.Attributes {
		value := b.base[key]
		if terminalValue, ok := terminal[key]; ok {
			value = terminalValue
		}
		attrs = append(attrs, attribute.String(string(key), NormalizeAttribute(key, value)))
	}
	return attrs
}

func terminalClasses(err error, panicked bool) (string, string) {
	if panicked {
		return "error", "panic"
	}
	switch {
	case err == nil:
		return "success", "none"
	case errors.Is(err, context.Canceled):
		return "canceled", "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", "timeout"
	default:
		return "error", ErrorClass(err)
	}
}

func hasAttribute(attrs []attribute.KeyValue, key AttributeKey, value string) bool {
	for _, attr := range attrs {
		if string(attr.Key) == string(key) && attr.Value.AsString() == value {
			return true
		}
	}
	return false
}
