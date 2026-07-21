package obs

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type BoundaryConfig struct {
	Operation, ToolClass, Transport string
	Count, Duration                 InstrumentID
}
type Boundary struct{}
type BoundaryEnd struct{}

func NewBoundary(metric.Meter, trace.Tracer, BoundaryConfig) (*Boundary, error) {
	return nil, errors.New("boundary recorder not implemented")
}
func (*Boundary) Start(ctx context.Context) (context.Context, *BoundaryEnd) {
	return ctx, &BoundaryEnd{}
}
func (*BoundaryEnd) End(error)                                     {}
func (*BoundaryEnd) PanicSafe(*error)                              {}
func hasAttribute([]attribute.KeyValue, AttributeKey, string) bool { return false }
