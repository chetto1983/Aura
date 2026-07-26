package adaptive

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrAssignmentNotFound hides absent and foreign persisted assignments identically.
	ErrAssignmentNotFound = errors.New("adaptive assignment not found")
	// ErrPersistedAssignmentInvalid rejects a stored assignment that fails the Go contract.
	ErrPersistedAssignmentInvalid = errors.New("persisted adaptive assignment is invalid")
)

// RecordAssignment validates and appends one schema-2 assignment.
func (s *Store) RecordAssignment(ctx context.Context, assignment Assignment) (int64, error) {
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return 0, fmt.Errorf("construct adaptive assignment event: %w", err)
	}
	sequence, err := s.recordValidated(ctx, event)
	if err != nil {
		return 0, fmt.Errorf("record adaptive assignment %s: %w", assignment.AssignmentID, err)
	}
	return sequence, nil
}

// RecordDelivery appends one delivery bound to its persisted assignment.
func (s *Store) RecordDelivery(
	ctx context.Context,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
	delivery Delivery,
) (int64, error) {
	if ownerID == uuid.Nil || assignmentID == uuid.Nil {
		return 0, fmt.Errorf(
			"load adaptive assignment %s for owner %s: %w",
			assignmentID,
			ownerID,
			ErrAssignmentNotFound,
		)
	}
	if s == nil || s.pool == nil {
		return 0, errors.New("adaptive store requires a database pool")
	}
	var sequence int64
	err := db.WithIdentityTx(ctx, s.pool, ownerID.String(), func(q *sqlc.Queries) error {
		if err := lockAdaptiveOwnerTx(ctx, q, ownerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf(
					"load adaptive assignment %s for owner %s: %w",
					assignmentID,
					ownerID,
					ErrAssignmentNotFound,
				)
			}
			return err
		}
		row, err := q.LockSchema2AdaptiveAssignment(
			ctx,
			sqlc.LockSchema2AdaptiveAssignmentParams{
				OwnerID:      dbUUID(ownerID),
				AssignmentID: dbUUID(assignmentID),
			},
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf(
				"load adaptive assignment %s for owner %s: %w",
				assignmentID,
				ownerID,
				ErrAssignmentNotFound,
			)
		}
		if err != nil {
			return fmt.Errorf("lock persisted adaptive assignment %s: %w", assignmentID, err)
		}
		assignment, err := assignmentFromPersistedRow(row, ownerID, assignmentID)
		if err != nil {
			return err
		}
		event, err := NewDeliveryEvent(assignment, delivery)
		if err != nil {
			return fmt.Errorf("construct adaptive delivery event: %w", err)
		}
		sequence, _, err = s.recordOwnerLockedTx(ctx, q, event)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("record adaptive delivery for assignment %s: %w", assignmentID, err)
	}
	return sequence, nil
}

func assignmentFromPersistedRow(
	row sqlc.AuraAdaptiveOutbox,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
) (Assignment, error) {
	assignment, err := DecodeAssignment(row.Payload)
	if err != nil {
		return Assignment{}, fmt.Errorf("%w: %w", ErrPersistedAssignmentInvalid, err)
	}
	reconstructed, err := NewAssignmentEvent(assignment)
	if err != nil {
		return Assignment{}, fmt.Errorf("%w: reconstruct event: %w", ErrPersistedAssignmentInvalid, err)
	}
	rowOwner := uuid.UUID(row.OwnerID.Bytes)
	rowDecision := uuid.UUID(row.DecisionID.Bytes)
	switch {
	case !row.OwnerID.Valid || rowOwner != ownerID || assignment.OwnerID != ownerID:
		return Assignment{}, fmt.Errorf(
			"%w: owner_id does not match caller and persisted row",
			ErrPersistedAssignmentInvalid,
		)
	case !row.DecisionID.Valid ||
		rowDecision != assignmentID ||
		assignment.AssignmentID != assignmentID:
		return Assignment{}, fmt.Errorf(
			"%w: assignment_id does not match caller and persisted row",
			ErrPersistedAssignmentInvalid,
		)
	case row.AggregateID != assignment.RequestID.String():
		return Assignment{}, fmt.Errorf(
			"%w: request_id does not match persisted aggregate",
			ErrPersistedAssignmentInvalid,
		)
	case !row.ID.Valid || reconstructed.ID != uuid.UUID(row.ID.Bytes):
		return Assignment{}, fmt.Errorf(
			"%w: event_id does not match persisted row",
			ErrPersistedAssignmentInvalid,
		)
	case reconstructed.Kind != EventKind(row.EventKind):
		return Assignment{}, fmt.Errorf(
			"%w: event kind does not match persisted row",
			ErrPersistedAssignmentInvalid,
		)
	case !bytes.Equal(reconstructed.PayloadHash, row.PayloadHash):
		return Assignment{}, fmt.Errorf(
			"%w: payload hash does not match persisted row",
			ErrPersistedAssignmentInvalid,
		)
	}
	return assignment, nil
}
