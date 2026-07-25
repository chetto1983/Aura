package adaptive

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeAssignmentRoundTripsCanonicalPayload(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}

	decoded, err := DecodeAssignment(event.Payload)
	if err != nil {
		t.Fatalf("DecodeAssignment: %v", err)
	}
	roundTrip, err := NewAssignmentEvent(decoded)
	if err != nil {
		t.Fatalf("NewAssignmentEvent(decoded): %v", err)
	}
	if !bytes.Equal(roundTrip.Payload, event.Payload) {
		t.Fatalf("roundtrip payload mismatch:\nwant: %s\ngot:  %s", event.Payload, roundTrip.Payload)
	}
}

func TestDecodeAssignmentRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	t.Parallel()
	event, err := NewAssignmentEvent(validAssignment())
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	valid := string(event.Payload)
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "unknown field",
			payload: strings.Replace(valid, "{", `{"raw_content":"private",`, 1),
		},
		{name: "trailing JSON", payload: valid + ` {}`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeAssignment([]byte(tt.payload)); err == nil {
				t.Fatal("DecodeAssignment error = nil, want strict wire rejection")
			}
		})
	}
}

func TestDecodeAssignmentRevalidatesPersistedContract(t *testing.T) {
	t.Parallel()
	event, err := NewAssignmentEvent(validAssignment())
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	payload := bytes.Replace(
		event.Payload,
		[]byte(`"schema_version":"2.0"`),
		[]byte(`"schema_version":"1.0"`),
		1,
	)

	if _, err := DecodeAssignment(payload); err == nil {
		t.Fatal("DecodeAssignment error = nil, want contract rejection")
	}
}

func TestDecodeAssignmentReturnsOwnedCollectionsAndPointers(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	makeCanary(&assignment)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatalf("NewAssignmentEvent: %v", err)
	}
	first, err := DecodeAssignment(event.Payload)
	if err != nil {
		t.Fatalf("DecodeAssignment(first): %v", err)
	}
	second, err := DecodeAssignment(event.Payload)
	if err != nil {
		t.Fatalf("DecodeAssignment(second): %v", err)
	}

	first.EligibleActions[0] = "mutated"
	first.ActionProbabilities[0].Probability = 1
	first.Features[FeatureCandidateCount] = 99
	*first.CohortID = uuid.Nil
	*first.ArmProbability = 1

	if second.EligibleActions[0] != "candidate" ||
		second.ActionProbabilities[0].Probability != 0.5 ||
		second.Features[FeatureCandidateCount] != 2 ||
		*second.CohortID == uuid.Nil ||
		*second.ArmProbability != 0.5 {
		t.Fatalf("separate DecodeAssignment results share mutable state: %+v", second)
	}
}
