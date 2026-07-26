package orderingcontrol

import (
	"context"
	"errors"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
)

type orderingDecisionFunc func(context.Context) (toolDecision, error)
type orderingAssignmentFunc func(toolDecision) (adaptive.Assignment, error)
type orderingDeliveryFunc func(
	adaptive.Assignment,
	[]string,
) (adaptive.Delivery, error)

func orderFromControl[Input any](
	ctx context.Context,
	input Input,
	execute func(context.Context, string) ([]string, error),
	decide func(context.Context, Input) (toolDecision, error),
	recorder toolEventRecorder,
	newAssignment func(
		Input,
		toolDecision,
	) (adaptive.Assignment, error),
	newDelivery func(
		Input,
		adaptive.Assignment,
		[]string,
	) (adaptive.Delivery, error),
) ([]string, error) {
	var boundDecision orderingDecisionFunc
	if decide != nil {
		boundDecision = func(ctx context.Context) (toolDecision, error) {
			return decide(ctx, input)
		}
	}
	return orderAdaptive(
		ctx,
		execute,
		boundDecision,
		recorder,
		func(decision toolDecision) (adaptive.Assignment, error) {
			return newAssignment(input, decision)
		},
		func(
			assignment adaptive.Assignment,
			ids []string,
		) (adaptive.Delivery, error) {
			return newDelivery(input, assignment, ids)
		},
	)
}

func orderAdaptive(
	ctx context.Context,
	execute func(context.Context, string) ([]string, error),
	decide orderingDecisionFunc,
	recorder toolEventRecorder,
	newAssignment orderingAssignmentFunc,
	newDelivery orderingDeliveryFunc,
) ([]string, error) {
	static := func(context.Context) ([]string, error) {
		if execute == nil {
			return nil, errors.New(
				"adaptive ordering requires a static strategy",
			)
		}
		return execute(ctx, adaptive.StaticActionID)
	}
	if decide == nil || recorder == nil ||
		newAssignment == nil || newDelivery == nil ||
		execute == nil {
		return static(ctx)
	}
	decision, err := decide(ctx)
	if err != nil || decision.Policy.Mode == adaptive.PolicyOff ||
		decision.Policy.Mode == adaptive.PolicyRollback {
		return static(ctx)
	}
	assignment, err := newAssignment(decision)
	if err != nil {
		return static(ctx)
	}
	var delivery adaptive.Delivery
	return adaptive.DecideAndDeliver(
		ctx,
		assignment,
		func(ctx context.Context, _ adaptive.Event) error {
			_, err := recorder.RecordAssignment(ctx, assignment)
			return err
		},
		func(
			ctx context.Context,
			assignment adaptive.Assignment,
		) (adaptive.Exposure[[]string], error) {
			ids, err := execute(ctx, assignment.IntendedActionID)
			if err != nil {
				return adaptive.Exposure[[]string]{}, err
			}
			delivery, err = newDelivery(assignment, ids)
			if err != nil {
				return adaptive.Exposure[[]string]{}, err
			}
			return adaptive.Exposure[[]string]{
				Value:    slices.Clone(ids),
				Delivery: delivery,
			}, nil
		},
		func(ctx context.Context, _ adaptive.Event) error {
			_, err := recorder.RecordDelivery(
				ctx,
				assignment.OwnerID,
				assignment.AssignmentID,
				delivery,
			)
			return err
		},
		static,
	)
}
