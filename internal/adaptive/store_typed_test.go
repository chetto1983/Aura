package adaptive

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

func TestStoreGenericRecordAPIsRejectSchema2EventLiterals(t *testing.T) {
	t.Parallel()
	store := &Store{}
	for _, kind := range []EventKind{
		EventDecision,
		EventDelivery,
		EventOutcome,
		EventCorrection,
		EventPromotion,
		EventRollback,
	} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			event := Event{
				ID:          uuid.Must(uuid.NewV7()),
				OwnerID:     uuid.Must(uuid.NewV7()),
				AggregateID: "schema-2-bypass",
				DecisionID:  uuid.Must(uuid.NewV7()),
				Kind:        kind,
				Payload:     []byte(`{"schema_version":"2.0"}`),
			}
			if _, err := store.Record(context.Background(), event); !errors.Is(err, ErrTypedStoreRequired) {
				t.Fatalf("Record error = %v, want ErrTypedStoreRequired", err)
			}
			if _, _, err := store.RecordTx(
				context.Background(),
				nil,
				event,
			); !errors.Is(err, ErrTypedStoreRequired) {
				t.Fatalf("RecordTx error = %v, want ErrTypedStoreRequired", err)
			}
		})
	}
}

func TestStoreRecordAssignmentUsesTypedConstructor(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	assignment.SchemaVersion = SchemaVersion1

	_, err := (&Store{}).RecordAssignment(context.Background(), assignment)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("RecordAssignment error = %v, want typed constructor rejection", err)
	}
}

func TestStoreRecordDeliveryRejectsMissingAssignmentIdentity(t *testing.T) {
	t.Parallel()
	_, err := (&Store{}).RecordDelivery(
		context.Background(),
		uuid.Must(uuid.NewV7()),
		uuid.Nil,
		Delivery{},
	)
	if !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("RecordDelivery error = %v, want ErrAssignmentNotFound", err)
	}
}

func TestAssignmentFromPersistedRowRejectsBindingAndPayloadMismatches(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	valid := sqlc.AuraAdaptiveOutbox{
		ID:          dbUUID(event.ID),
		OwnerID:     dbUUID(event.OwnerID),
		AggregateID: event.AggregateID,
		DecisionID:  dbUUID(event.DecisionID),
		EventKind:   string(event.Kind),
		Payload:     append([]byte(nil), event.Payload...),
		PayloadHash: append([]byte(nil), event.PayloadHash...),
	}
	if _, err := assignmentFromPersistedRow(
		valid,
		assignment.OwnerID,
		assignment.AssignmentID,
	); err != nil {
		t.Fatalf("assignmentFromPersistedRow(valid): %v", err)
	}

	tests := []struct {
		name         string
		mutate       func(*sqlc.AuraAdaptiveOutbox)
		ownerID      uuid.UUID
		assignmentID uuid.UUID
	}{
		{
			name:    "caller owner",
			ownerID: uuid.Must(uuid.NewV7()),
		},
		{
			name:         "caller assignment",
			assignmentID: uuid.Must(uuid.NewV7()),
		},
		{
			name: "row owner",
			mutate: func(row *sqlc.AuraAdaptiveOutbox) {
				row.OwnerID = dbUUID(uuid.Must(uuid.NewV7()))
			},
		},
		{
			name: "row assignment",
			mutate: func(row *sqlc.AuraAdaptiveOutbox) {
				row.DecisionID = dbUUID(uuid.Must(uuid.NewV7()))
			},
		},
		{
			name: "row aggregate",
			mutate: func(row *sqlc.AuraAdaptiveOutbox) {
				row.AggregateID = uuid.Must(uuid.NewV7()).String()
			},
		},
		{
			name: "row event id",
			mutate: func(row *sqlc.AuraAdaptiveOutbox) {
				row.ID = dbUUID(uuid.Must(uuid.NewV7()))
			},
		},
		{
			name: "row event kind",
			mutate: func(row *sqlc.AuraAdaptiveOutbox) {
				row.EventKind = string(EventOutcome)
			},
		},
		{
			name: "row payload hash",
			mutate: func(row *sqlc.AuraAdaptiveOutbox) {
				row.PayloadHash = []byte("mismatch")
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := valid
			if tt.mutate != nil {
				tt.mutate(&row)
			}
			ownerID := tt.ownerID
			if ownerID == uuid.Nil {
				ownerID = assignment.OwnerID
			}
			assignmentID := tt.assignmentID
			if assignmentID == uuid.Nil {
				assignmentID = assignment.AssignmentID
			}
			if _, err := assignmentFromPersistedRow(
				row,
				ownerID,
				assignmentID,
			); !errors.Is(err, ErrPersistedAssignmentInvalid) {
				t.Fatalf(
					"assignmentFromPersistedRow error = %v, want ErrPersistedAssignmentInvalid",
					err,
				)
			}
		})
	}
}
