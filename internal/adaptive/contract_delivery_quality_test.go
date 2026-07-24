package adaptive

import (
	"bytes"
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestNewDeliveryEventAcceptsOnlyAssignmentBoundInput(t *testing.T) {
	t.Parallel()
	constructor, ok := any(NewDeliveryEvent).(func(Assignment, Delivery) (Event, error))
	if !ok {
		t.Fatal("NewDeliveryEvent still accepts independently supplied owner/request facts")
	}
	assignment := validAssignment()
	delivery := assignmentBoundDelivery(assignment)

	event, err := constructor(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	if event.OwnerID != assignment.OwnerID || event.AggregateID != assignment.RequestID.String() {
		t.Fatalf("delivery event scope = (%s, %q), want assignment scope",
			event.OwnerID, event.AggregateID)
	}
}

func TestNewDeliveryEventRejectsMismatchedAssignmentFacts(t *testing.T) {
	t.Parallel()
	constructor, ok := any(NewDeliveryEvent).(func(Assignment, Delivery) (Event, error))
	if !ok {
		t.Fatal("NewDeliveryEvent does not bind a validated assignment")
	}
	tests := []struct {
		name   string
		mutate func(*Assignment, *Delivery)
	}{
		{name: "nonnil wrong owner", mutate: func(a *Assignment, _ *Delivery) {
			a.OwnerID = uuid.MustParse("08e7ad81-0982-4c85-899d-f83d329544f7")
		}},
		{name: "nonnil wrong request", mutate: func(a *Assignment, _ *Delivery) {
			a.RequestID = uuid.MustParse("a662fba9-7d4e-47ac-b42a-02aee56cb655")
		}},
		{name: "assignment ID", mutate: func(_ *Assignment, d *Delivery) {
			d.AssignmentID = uuid.MustParse("796561b8-3819-49c1-9d69-791be8dcc9f6")
		}},
		{name: "intended action", mutate: func(_ *Assignment, d *Delivery) {
			d.IntendedActionID = "candidate"
		}},
		{name: "actual action catalog", mutate: func(_ *Assignment, d *Delivery) {
			d.ActualActionID = "syntactically-safe-secret"
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assignment := validAssignment()
			delivery := assignmentBoundDelivery(assignment)
			tt.mutate(&assignment, &delivery)
			if _, err := constructor(assignment, delivery); err == nil {
				t.Fatal("NewDeliveryEvent error = nil, want assignment fact rejection")
			}
		})
	}
}

func TestNewDeliveryEventValidatesSuccessfulExposureAgainstAssignment(t *testing.T) {
	t.Parallel()
	constructor, ok := any(NewDeliveryEvent).(func(Assignment, Delivery) (Event, error))
	if !ok {
		t.Fatal("NewDeliveryEvent does not bind a validated assignment")
	}
	assignment := validAssignment()
	makeCanary(&assignment)
	delivery := validDelivery(assignment.AssignmentID)
	delivery.IntendedActionID = assignment.IntendedActionID
	delivery.ActualActionID = assignment.IntendedActionID
	delivery.Status = DeliverySuccess
	delivery.FallbackReason = ""
	delivery.ExposureKnown = true
	mismatch := 0.4
	delivery.ExposureProbability = &mismatch

	if _, err := constructor(assignment, delivery); err == nil {
		t.Fatal("NewDeliveryEvent accepted exposure that differs from the logged marginal")
	}
	marginal := 0.5
	delivery.ExposureProbability = &marginal
	if _, err := constructor(assignment, delivery); err != nil {
		t.Fatalf("NewDeliveryEvent matching exposure: %v", err)
	}
}

func TestAssignmentEventCanonicalizesNilMapsAndSignedZero(t *testing.T) {
	t.Parallel()
	nilAndNegativeZero := validAssignment()
	nilAndNegativeZero.Features = nil
	nilAndNegativeZero.ActionProbabilities[0].Probability = math.Copysign(0, -1)
	emptyAndPositiveZero := validAssignment()
	emptyAndPositiveZero.Features = map[FeatureKey]float64{}
	emptyAndPositiveZero.ActionProbabilities[0].Probability = 0

	first, err := NewAssignmentEvent(nilAndNegativeZero)
	if err != nil {
		t.Fatalf("NewAssignmentEvent(nil/-0): %v", err)
	}
	second, err := NewAssignmentEvent(emptyAndPositiveZero)
	if err != nil {
		t.Fatalf("NewAssignmentEvent(empty/+0): %v", err)
	}
	assertCanonicalEventsEqual(t, first, second)
	if !bytes.Contains(first.Payload, []byte(`"features":{}`)) {
		t.Fatalf("assignment payload does not contain canonical empty features: %s", first.Payload)
	}
}

func TestDeliveryEventCanonicalizesNilAndEmptyCollections(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	nilCollections := assignmentBoundDelivery(assignment)
	nilCollections.ResultIDs = nil
	nilCollections.Revisions = RevisionSet{}
	nilCollections.EffectiveLimits = nil
	emptyCollections := assignmentBoundDelivery(assignment)
	emptyCollections.ResultIDs = []ResultID{}
	emptyCollections.Revisions = mustRevisionSet()
	emptyCollections.EffectiveLimits = map[string]int{}

	first, err := NewDeliveryEvent(assignment, nilCollections)
	if err != nil {
		t.Fatalf("NewDeliveryEvent(nil): %v", err)
	}
	second, err := NewDeliveryEvent(assignment, emptyCollections)
	if err != nil {
		t.Fatalf("NewDeliveryEvent(empty): %v", err)
	}
	assertCanonicalEventsEqual(t, first, second)
	for _, field := range [][]byte{
		[]byte(`"result_ids":[]`),
		[]byte(`"revisions":{}`),
		[]byte(`"effective_limits":{}`),
	} {
		if !bytes.Contains(first.Payload, field) {
			t.Fatalf("delivery payload does not contain %s: %s", field, first.Payload)
		}
	}
}

func assertCanonicalEventsEqual(t *testing.T, first, second Event) {
	t.Helper()
	if first.ID != second.ID {
		t.Fatalf("event IDs differ: %s != %s", first.ID, second.ID)
	}
	if !bytes.Equal(first.Payload, second.Payload) {
		t.Fatalf("canonical payload mismatch:\nfirst: %s\nsecond: %s", first.Payload, second.Payload)
	}
	if !bytes.Equal(first.PayloadHash, second.PayloadHash) {
		t.Fatalf("canonical payload hashes differ: %x != %x", first.PayloadHash, second.PayloadHash)
	}
}

func assignmentBoundDelivery(assignment Assignment) Delivery {
	delivery := validDelivery(assignment.AssignmentID)
	delivery.IntendedActionID = assignment.IntendedActionID
	delivery.ActualActionID = "candidate"
	return delivery
}
