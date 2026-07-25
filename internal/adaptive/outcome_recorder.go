package adaptive

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrOutcomeRecorderUnavailable reports a missing store, catalog, or recorder.
var ErrOutcomeRecorderUnavailable = errors.New("adaptive outcome recorder is unavailable")

// OutcomeRecorder is the only production persistence path for eligible schema-2 outcomes.
type OutcomeRecorder struct {
	store   *Store
	catalog EvaluatorCatalog
}

// NewOutcomeRecorder binds typed evidence to a store and frozen evaluator catalog.
func NewOutcomeRecorder(store *Store, catalog EvaluatorCatalog) (*OutcomeRecorder, error) {
	if store == nil || store.pool == nil {
		return nil, fmt.Errorf("%w: database store", ErrOutcomeRecorderUnavailable)
	}
	if len(catalog.registrations) == 0 {
		return nil, fmt.Errorf("%w: evaluator catalog", ErrOutcomeRecorderUnavailable)
	}
	return &OutcomeRecorder{store: store, catalog: catalog}, nil
}

// RecordOutcome appends one terminal evaluator observation after locking its assignment.
func (recorder *OutcomeRecorder) RecordOutcome(
	ctx context.Context,
	observation OutcomeObservation,
) (int64, error) {
	if err := recorder.available(); err != nil {
		return 0, err
	}
	observation = canonicalOutcomeObservation(observation)
	if err := validateOutcomeObservation(observation, recorder.catalog); err != nil {
		return 0, err
	}
	var sequence int64
	err := db.WithIdentityTx(
		ctx,
		recorder.store.pool,
		observation.OwnerID.String(),
		func(queries *sqlc.Queries) error {
			assignment, err := recorder.lockAssignment(
				ctx,
				queries,
				observation.OwnerID,
				observation.AssignmentID,
			)
			if err != nil {
				return err
			}
			event, err := NewOutcomeEvent(assignment, observation, recorder.catalog)
			if err != nil {
				return err
			}
			sequence, _, err = recorder.store.recordOwnerLockedTx(ctx, queries, event)
			return err
		},
	)
	if err != nil {
		return 0, fmt.Errorf(
			"record adaptive outcome for assignment %s: %w",
			observation.AssignmentID,
			err,
		)
	}
	return sequence, nil
}

// RecordCorrection appends one correction to the current effective outcome leaf.
func (recorder *OutcomeRecorder) RecordCorrection(
	ctx context.Context,
	correction CorrectionObservation,
) (int64, error) {
	if err := recorder.available(); err != nil {
		return 0, err
	}
	correction = canonicalCorrectionObservation(correction)
	if err := validateCorrectionObservation(correction, recorder.catalog); err != nil {
		return 0, err
	}
	var sequence int64
	err := db.WithIdentityTx(
		ctx,
		recorder.store.pool,
		correction.OwnerID.String(),
		func(queries *sqlc.Queries) error {
			assignment, err := recorder.lockAssignment(
				ctx,
				queries,
				correction.OwnerID,
				correction.AssignmentID,
			)
			if err != nil {
				return err
			}
			event, err := NewCorrectionEvent(assignment, correction, recorder.catalog)
			if err != nil {
				return err
			}
			if duplicateSequence, duplicate, err := lockAndClassifyDuplicate(
				ctx,
				queries,
				event,
			); err != nil {
				return err
			} else if duplicate {
				sequence = duplicateSequence
				return nil
			}
			rows, err := queries.LockSchema2AdaptiveOutcomeChain(
				ctx,
				sqlc.LockSchema2AdaptiveOutcomeChainParams{
					OwnerID:      dbUUID(correction.OwnerID),
					AssignmentID: dbUUID(correction.AssignmentID),
				},
			)
			if err != nil {
				return fmt.Errorf("lock adaptive outcome chain: %w", err)
			}
			facts, err := recorder.outcomeChainFacts(assignment, rows)
			if err != nil {
				return err
			}
			if err := validateCorrectionAppend(facts, event.ID, correction); err != nil {
				return err
			}
			sequence, _, err = recorder.store.recordOwnerLockedTx(ctx, queries, event)
			return err
		},
	)
	if err != nil {
		return 0, fmt.Errorf(
			"record adaptive correction for assignment %s: %w",
			correction.AssignmentID,
			err,
		)
	}
	return sequence, nil
}

func (recorder *OutcomeRecorder) available() error {
	if recorder == nil || recorder.store == nil || recorder.store.pool == nil ||
		len(recorder.catalog.registrations) == 0 {
		return ErrOutcomeRecorderUnavailable
	}
	return nil
}

func (recorder *OutcomeRecorder) lockAssignment(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
) (Assignment, error) {
	if err := lockAdaptiveOwnerTx(ctx, queries, ownerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Assignment{}, ErrAssignmentNotFound
		}
		return Assignment{}, err
	}
	row, err := queries.LockSchema2AdaptiveAssignment(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID:      dbUUID(ownerID),
			AssignmentID: dbUUID(assignmentID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Assignment{}, ErrAssignmentNotFound
	}
	if err != nil {
		return Assignment{}, fmt.Errorf("lock persisted adaptive assignment: %w", err)
	}
	return assignmentFromPersistedRow(row, ownerID, assignmentID)
}

func lockAndClassifyDuplicate(
	ctx context.Context,
	queries *sqlc.Queries,
	event Event,
) (int64, bool, error) {
	if err := queries.LockAdaptiveEvent(ctx, event.ID.String()); err != nil {
		return 0, false, fmt.Errorf("lock adaptive event: %w", err)
	}
	existing, err := queries.GetAdaptiveOutboxByID(ctx, dbUUID(event.ID))
	switch {
	case err == nil && sameImmutableEvent(existing, event):
		return existing.Sequence, true, nil
	case err == nil:
		return 0, false, ErrPayloadConflict
	case errors.Is(err, pgx.ErrNoRows):
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("read adaptive event: %w", err)
	}
}

func (recorder *OutcomeRecorder) outcomeChainFacts(
	assignment Assignment,
	rows []sqlc.AuraAdaptiveOutbox,
) ([]outcomeChainFact, error) {
	facts := make([]outcomeChainFact, 0, len(rows))
	for _, row := range rows {
		event := eventFromOutboxRow(row)
		switch event.Kind {
		case EventOutcome:
			observation, err := DecodeOutcome(row.Payload, assignment, recorder.catalog)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrCorrectionChainInvalid, err)
			}
			expected, err := NewOutcomeEvent(assignment, observation, recorder.catalog)
			if err != nil || !sameImmutableEvent(row, expected) {
				return nil, ErrCorrectionChainInvalid
			}
			facts = append(facts, outcomeChainFact{
				id: event.ID, kind: event.Kind, evaluator: observation.Evaluator,
			})
		case EventCorrection:
			correction, err := DecodeCorrection(row.Payload, assignment, recorder.catalog)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrCorrectionChainInvalid, err)
			}
			expected, err := NewCorrectionEvent(assignment, correction, recorder.catalog)
			if err != nil || !sameImmutableEvent(row, expected) {
				return nil, ErrCorrectionChainInvalid
			}
			facts = append(facts, outcomeChainFact{
				id:         event.ID,
				kind:       event.Kind,
				evaluator:  correction.Evaluator,
				supersedes: correction.SupersedesEventID,
			})
		default:
			return nil, ErrCorrectionChainInvalid
		}
	}
	return facts, nil
}

func eventFromOutboxRow(row sqlc.AuraAdaptiveOutbox) Event {
	return Event{
		ID:          uuid.UUID(row.ID.Bytes),
		OwnerID:     uuid.UUID(row.OwnerID.Bytes),
		AggregateID: row.AggregateID,
		DecisionID:  uuid.UUID(row.DecisionID.Bytes),
		Kind:        EventKind(row.EventKind),
		Payload:     row.Payload,
		PayloadHash: row.PayloadHash,
		CreatedAt:   row.CreatedAt.Time,
	}
}

// BenchmarkOutcomeEvaluator routes a trusted benchmark evaluator through OutcomeRecorder.
type BenchmarkOutcomeEvaluator struct {
	recorder     *OutcomeRecorder
	registration EvaluatorRegistration
}

// NewDeterministicBenchmarkEvaluator binds a deterministic benchmark registration.
func NewDeterministicBenchmarkEvaluator(
	recorder *OutcomeRecorder,
	registration EvaluatorRegistration,
) (*BenchmarkOutcomeEvaluator, error) {
	return newBenchmarkOutcomeEvaluator(recorder, registration, EvaluatorDeterministic)
}

// NewCalibratedJudgeBenchmarkEvaluator binds a calibrated judge registration.
func NewCalibratedJudgeBenchmarkEvaluator(
	recorder *OutcomeRecorder,
	registration EvaluatorRegistration,
) (*BenchmarkOutcomeEvaluator, error) {
	return newBenchmarkOutcomeEvaluator(recorder, registration, EvaluatorCalibratedJudge)
}

func newBenchmarkOutcomeEvaluator(
	recorder *OutcomeRecorder,
	registration EvaluatorRegistration,
	expected EvaluatorKind,
) (*BenchmarkOutcomeEvaluator, error) {
	if recorder == nil {
		return nil, ErrOutcomeRecorderUnavailable
	}
	if registration.Kind != expected {
		return nil, ErrEvaluatorRegistrationInvalid
	}
	if err := validateEvaluatorRegistration(registration); err != nil {
		return nil, err
	}
	return &BenchmarkOutcomeEvaluator{recorder: recorder, registration: registration}, nil
}

// Record overwrites caller-supplied provenance with the adapter's trusted registration.
func (evaluator *BenchmarkOutcomeEvaluator) Record(
	ctx context.Context,
	observation OutcomeObservation,
) (int64, error) {
	if evaluator == nil || evaluator.recorder == nil {
		return 0, ErrOutcomeRecorderUnavailable
	}
	observation.Evaluator = evaluator.registration.Provenance()
	return evaluator.recorder.RecordOutcome(ctx, observation)
}
