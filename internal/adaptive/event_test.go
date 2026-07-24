package adaptive

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestNewEventCanonicalizesPayloadHash(t *testing.T) {
	t.Parallel()
	owner := uuid.Must(uuid.NewV7())
	decision := uuid.Must(uuid.NewV7())

	first, err := NewEvent(EventParams{
		ID:          uuid.Must(uuid.NewV7()),
		OwnerID:     owner,
		AggregateID: "turn-42",
		DecisionID:  decision,
		Kind:        EventDecision,
		Payload:     json.RawMessage(`{"schema_version":"1.0","b":2,"a":1}`),
	})
	if err != nil {
		t.Fatalf("NewEvent(first): %v", err)
	}
	second, err := NewEvent(EventParams{
		ID:          first.ID,
		OwnerID:     owner,
		AggregateID: "turn-42",
		DecisionID:  decision,
		Kind:        EventDecision,
		Payload:     json.RawMessage("{\n \"a\": 1, \"schema_version\": \"1.0\", \"b\": 2\n}"),
	})
	if err != nil {
		t.Fatalf("NewEvent(second): %v", err)
	}
	if !bytes.Equal(first.Payload, second.Payload) {
		t.Fatalf("canonical payload mismatch:\nfirst: %s\nsecond: %s", first.Payload, second.Payload)
	}
	if !bytes.Equal(first.PayloadHash, second.PayloadHash) {
		t.Fatalf("semantic duplicate produced different payload hashes: %x != %x", first.PayloadHash, second.PayloadHash)
	}
}

func TestNewEventRejectsUnscopedOrUnversionedPayload(t *testing.T) {
	t.Parallel()
	valid := EventParams{
		ID:          uuid.Must(uuid.NewV7()),
		OwnerID:     uuid.Must(uuid.NewV7()),
		AggregateID: "turn-42",
		DecisionID:  uuid.Must(uuid.NewV7()),
		Kind:        EventDecision,
		Payload:     json.RawMessage(`{"schema_version":"1.0"}`),
	}
	tests := []struct {
		name   string
		mutate func(*EventParams)
	}{
		{name: "missing owner", mutate: func(p *EventParams) { p.OwnerID = uuid.Nil }},
		{name: "missing aggregate", mutate: func(p *EventParams) { p.AggregateID = "" }},
		{name: "missing decision", mutate: func(p *EventParams) { p.DecisionID = uuid.Nil }},
		{name: "unknown kind", mutate: func(p *EventParams) { p.Kind = EventKind("surprise") }},
		{name: "missing schema version", mutate: func(p *EventParams) { p.Payload = json.RawMessage(`{"value":1}`) }},
		{name: "invalid json", mutate: func(p *EventParams) { p.Payload = json.RawMessage(`{`) }},
		{name: "multiple json values", mutate: func(p *EventParams) {
			p.Payload = json.RawMessage(`{"schema_version":"1.0"} {"schema_version":"2.0"}`)
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := valid
			tt.mutate(&p)
			if _, err := NewEvent(p); err == nil {
				t.Fatal("NewEvent error = nil, want rejection")
			}
		})
	}
}

func TestNewEventKeepsSchema1ReadableButRejectsTypedAndFutureEvidence(t *testing.T) {
	t.Parallel()
	valid := EventParams{
		ID:          uuid.Must(uuid.NewV7()),
		OwnerID:     uuid.Must(uuid.NewV7()),
		AggregateID: "turn-42",
		DecisionID:  uuid.Must(uuid.NewV7()),
		Kind:        EventDecision,
		Payload:     json.RawMessage(`{"schema_version":"1.0"}`),
	}
	for _, kind := range []EventKind{EventDecision, EventOutcome, EventCorrection} {
		params := valid
		params.Kind = kind
		if _, err := NewEvent(params); err != nil {
			t.Fatalf("NewEvent(%s schema 1.0): %v", kind, err)
		}
		for _, version := range []string{"2.0", "3.0", "future"} {
			params = valid
			params.Kind = kind
			params.Payload = json.RawMessage(`{"schema_version":"` + version + `"}`)
			if _, err := NewEvent(params); err == nil {
				t.Fatalf("NewEvent accepted generic %s schema %q", kind, version)
			}
		}
	}
}
