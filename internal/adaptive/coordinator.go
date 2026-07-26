package adaptive

import (
	"context"
	"errors"
)

// Exposure holds a learned result until its exact delivery is durable.
type Exposure[T any] struct {
	Value    T
	Delivery Delivery
}

// DecideAndDeliver exposes a learned result only after its assignment and delivery
// events commit. A failure at any learned-path boundary discards that result and
// executes the static path.
func DecideAndDeliver[T any](
	ctx context.Context,
	assignment Assignment,
	assign func(context.Context, Event) error,
	learned func(context.Context, Assignment) (Exposure[T], error),
	deliver func(context.Context, Event) error,
	static func(context.Context) (T, error),
) (T, error) {
	fallback := func() (T, error) {
		if static == nil {
			var zero T
			return zero, errors.New("adaptive coordinator requires a static executor")
		}
		value, err := static(ctx)
		if err != nil {
			var zero T
			return zero, err
		}
		return value, nil
	}
	if assign == nil || learned == nil || deliver == nil {
		return fallback()
	}
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		return fallback()
	}
	if err := assign(ctx, assignmentEvent); err != nil {
		return fallback()
	}
	exposure, err := learned(ctx, assignment)
	if err != nil {
		return fallback()
	}
	deliveryEvent, err := NewDeliveryEvent(assignment, exposure.Delivery)
	if err != nil {
		return fallback()
	}
	if err := deliver(ctx, deliveryEvent); err != nil {
		return fallback()
	}
	return exposure.Value, nil
}
