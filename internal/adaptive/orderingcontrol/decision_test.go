package orderingcontrol

import (
	"context"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

// recordingToolEvents is the shared in-memory EventRecorder that every
// ordering-domain test records against.
type recordingToolEvents struct {
	order         *[]string
	assignments   map[uuid.UUID]adaptive.Assignment
	deliveries    map[uuid.UUID]adaptive.Delivery
	assignmentErr error
	deliveryErr   error
}

func (recorder *recordingToolEvents) RecordAssignment(
	_ context.Context,
	assignment adaptive.Assignment,
) (int64, error) {
	if recorder.order != nil {
		*recorder.order = append(*recorder.order, "assignment")
	}
	if recorder.assignmentErr != nil {
		return 0, recorder.assignmentErr
	}
	if recorder.assignments == nil {
		recorder.assignments = make(map[uuid.UUID]adaptive.Assignment)
	}
	recorder.assignments[assignment.AssignmentID] = assignment
	return 1, nil
}

func (recorder *recordingToolEvents) RecordDelivery(
	_ context.Context,
	_ uuid.UUID,
	assignmentID uuid.UUID,
	delivery adaptive.Delivery,
) (int64, error) {
	if recorder.order != nil {
		*recorder.order = append(*recorder.order, "delivery")
	}
	if recorder.deliveryErr != nil {
		return 0, recorder.deliveryErr
	}
	if recorder.deliveries == nil {
		recorder.deliveries = make(map[uuid.UUID]adaptive.Delivery)
	}
	recorder.deliveries[assignmentID] = delivery
	return 2, nil
}
