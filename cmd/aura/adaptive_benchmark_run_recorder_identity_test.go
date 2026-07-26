package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
)

func TestAdaptiveBenchmarkTeeRecorderSelectsTerminalAssignmentByDeliverySequence(
	t *testing.T,
) {
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	base := adaptiveBenchmarkTestAssignment(
		t,
		adaptive.DomainReasoning,
	)
	first := adaptiveBenchmarkTestAssignmentAtOrdinal(t, base, 20)
	second := adaptiveBenchmarkTestAssignmentAtOrdinal(t, base, 2)
	for _, assignment := range []adaptive.Assignment{first, second} {
		if _, err := recorder.RecordAssignment(
			t.Context(),
			assignment,
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, assignment := range []adaptive.Assignment{second, first} {
		if _, err := recorder.RecordDelivery(
			t.Context(),
			assignment.OwnerID,
			assignment.AssignmentID,
			adaptiveBenchmarkTestDelivery(t, assignment),
		); err != nil {
			t.Fatal(err)
		}
	}

	assignmentID, err := recorder.TerminalEffectiveAssignmentID(
		base.OwnerID,
		base.RequestID,
		base.Domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	if assignmentID != first.AssignmentID {
		t.Fatalf(
			"terminal assignment = %s, want last delivered %s",
			assignmentID,
			first.AssignmentID,
		)
	}
}

func TestAdaptiveBenchmarkTeeRecorderRereadsExactAssignmentIdentity(
	t *testing.T,
) {
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	first := adaptiveBenchmarkTestAssignment(
		t,
		adaptive.DomainReasoning,
	)
	second := first
	second.PointOrdinal = 2
	second.AssignmentID, err = adaptive.AssignmentIDForPoint(
		second.OwnerID,
		second.RequestID,
		second.Point,
		second.PointOrdinal,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []adaptive.Assignment{first, second} {
		if _, err := recorder.RecordAssignment(
			t.Context(),
			assignment,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := recorder.RecordDelivery(
			t.Context(),
			assignment.OwnerID,
			assignment.AssignmentID,
			adaptiveBenchmarkTestDelivery(t, assignment),
		); err != nil {
			t.Fatal(err)
		}
	}

	for _, assignment := range []adaptive.Assignment{first, second} {
		facts, err := recorder.PersistedFactsForAssignment(
			t.Context(),
			assignment.OwnerID,
			assignment.RequestID,
			assignment.Domain,
			assignment.AssignmentID,
		)
		if err != nil {
			t.Fatal(err)
		}
		if facts.Assignment.AssignmentID != assignment.AssignmentID ||
			facts.Delivery.AssignmentID != assignment.AssignmentID {
			t.Fatalf(
				"exact facts = %s/%s, want %s",
				facts.Assignment.AssignmentID,
				facts.Delivery.AssignmentID,
				assignment.AssignmentID,
			)
		}
	}
}

func adaptiveBenchmarkTestAssignmentAtOrdinal(
	t *testing.T,
	base adaptive.Assignment,
	ordinal uint32,
) adaptive.Assignment {
	t.Helper()
	assignment := base
	assignment.PointOrdinal = ordinal
	var err error
	assignment.AssignmentID, err = adaptive.AssignmentIDForPoint(
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Point,
		assignment.PointOrdinal,
	)
	if err != nil {
		t.Fatal(err)
	}
	return assignment
}
