package adaptive

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAssignmentIDForPointRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	constructor, ok := any(AssignmentIDForPoint).(func(
		uuid.UUID,
		uuid.UUID,
		DecisionPoint,
		uint32,
	) (uuid.UUID, error))
	if !ok {
		t.Fatal("AssignmentIDForPoint does not expose validation errors")
	}
	owner := uuid.MustParse("17fc3d72-985a-4aa7-a80f-bc8e0f27d88c")
	request := uuid.MustParse("690d460e-6375-4637-ab93-59e07b2d81d6")
	tests := []struct {
		name    string
		owner   uuid.UUID
		request uuid.UUID
		point   DecisionPoint
	}{
		{name: "nil owner", owner: uuid.Nil, request: request, point: PointReasoning},
		{name: "nil request", owner: owner, request: uuid.Nil, point: PointReasoning},
		{name: "invalid point", owner: owner, request: request, point: DecisionPoint("reasoning ")},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := constructor(tt.owner, tt.request, tt.point, 1); err == nil {
				t.Fatal("AssignmentIDForPoint error = nil, want invalid scope rejection")
			}
		})
	}
}

func TestEventIDForSourceRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	constructor, ok := any(EventIDForSource).(func(
		uuid.UUID,
		EventKind,
		string,
	) (uuid.UUID, error))
	if !ok {
		t.Fatal("EventIDForSource does not expose validation errors")
	}
	assignmentID := uuid.MustParse("278b27e8-e53c-4fbc-9e40-ddb359cb3b7f")
	tests := []struct {
		name         string
		assignmentID uuid.UUID
		kind         EventKind
		source       string
	}{
		{name: "nil assignment", kind: EventDecision, source: "assignment"},
		{name: "invalid kind", assignmentID: assignmentID, kind: EventKind("decision "), source: "assignment"},
		{name: "blank source", assignmentID: assignmentID, kind: EventDecision},
		{name: "leading whitespace", assignmentID: assignmentID, kind: EventDecision, source: " assignment"},
		{name: "embedded whitespace", assignmentID: assignmentID, kind: EventDecision, source: "assign ment"},
		{name: "overlong source", assignmentID: assignmentID, kind: EventDecision, source: strings.Repeat("a", 129)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := constructor(tt.assignmentID, tt.kind, tt.source); err == nil {
				t.Fatal("EventIDForSource error = nil, want invalid scope rejection")
			}
		})
	}
}

func TestValidatedIDConstructorsKeepFieldBoundariesDistinct(t *testing.T) {
	t.Parallel()
	owner := uuid.MustParse("17fc3d72-985a-4aa7-a80f-bc8e0f27d88c")
	request := uuid.MustParse("690d460e-6375-4637-ab93-59e07b2d81d6")
	assignmentID := mustAssignmentID(owner, request, PointReasoning, 1)

	if assignmentID == mustAssignmentID(request, owner, PointReasoning, 1) {
		t.Fatal("owner/request boundary collision")
	}
	if assignmentID == mustAssignmentID(owner, request, PointReasoning, 256) {
		t.Fatal("point/ordinal boundary collision")
	}
	if mustEventID(assignmentID, EventDecision, "retry-2") ==
		mustEventID(assignmentID, EventDecision, "retry-20") {
		t.Fatal("source identity boundary collision")
	}
	if mustEventID(assignmentID, EventDecision, "assignment") ==
		mustEventID(assignmentID, EventDelivery, "assignment") {
		t.Fatal("event kind boundary collision")
	}
}

func mustAssignmentID(
	ownerID uuid.UUID,
	requestID uuid.UUID,
	point DecisionPoint,
	ordinal uint32,
) uuid.UUID {
	constructor, ok := any(AssignmentIDForPoint).(func(
		uuid.UUID,
		uuid.UUID,
		DecisionPoint,
		uint32,
	) (uuid.UUID, error))
	if !ok {
		panic("AssignmentIDForPoint does not expose validation errors")
	}
	id, err := constructor(ownerID, requestID, point, ordinal)
	if err != nil {
		panic(err)
	}
	return id
}

func mustEventID(assignmentID uuid.UUID, kind EventKind, sourceIdentity string) uuid.UUID {
	constructor, ok := any(EventIDForSource).(func(
		uuid.UUID,
		EventKind,
		string,
	) (uuid.UUID, error))
	if !ok {
		panic("EventIDForSource does not expose validation errors")
	}
	id, err := constructor(assignmentID, kind, sourceIdentity)
	if err != nil {
		panic(err)
	}
	return id
}
