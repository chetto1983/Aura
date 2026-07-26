package adaptive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrPayloadConflict reports reuse of an event ID with different immutable content.
	ErrPayloadConflict = errors.New("adaptive event id already exists with different immutable content")
	// ErrOwnerTombstoned prevents deleted identities from recreating adaptive facts.
	ErrOwnerTombstoned = errors.New("adaptive event owner was deleted and cannot be resurrected")
	// ErrLeaseLost prevents a worker from acknowledging another worker's lease.
	ErrLeaseLost = errors.New("adaptive outbox lease is not owned by this worker")
)

// StoreConfig configures adaptive-outbox delivery behavior.
type StoreConfig struct {
	MaxAttempts int
}

// Store owns the authoritative immutable adaptive ledger and delivery state.
type Store struct {
	pool        *pgxpool.Pool
	maxAttempts int
}

// OutboxRecord is one immutable fact plus its mutable projection-delivery state.
type OutboxRecord struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	AggregateID string
	Sequence    int64
	DecisionID  uuid.UUID
	Kind        EventKind
	Payload     []byte
	PayloadHash []byte
	Status      string
	Attempts    int32
	AvailableAt time.Time
	CreatedAt   time.Time
}

// ClaimOptions configures one bounded projector lease.
type ClaimOptions struct {
	WorkerID string
	Now      time.Time
	Lease    time.Duration
}

// NewStore binds the adaptive ledger to PostgreSQL.
func NewStore(pool *pgxpool.Pool, cfg StoreConfig) *Store {
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	return &Store{pool: pool, maxAttempts: maxAttempts}
}

func (s *Store) recordValidated(ctx context.Context, event Event) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("adaptive store requires a database pool")
	}
	var sequence int64
	err := db.WithIdentityTx(ctx, s.pool, event.OwnerID.String(), func(q *sqlc.Queries) error {
		var err error
		sequence, _, err = s.recordValidatedTx(ctx, q, event)
		return err
	})
	return sequence, err
}

func (s *Store) recordValidatedTx(
	ctx context.Context,
	q *sqlc.Queries,
	event Event,
) (sequence int64, duplicate bool, err error) {
	if err := lockAdaptiveOwnerTx(ctx, q, event.OwnerID); err != nil {
		return 0, false, err
	}
	return s.recordOwnerLockedTx(ctx, q, event)
}

// Every writer locks owner parent -> assignment child (delivery only) ->
// event advisory -> aggregate sequence/insert.
func lockAdaptiveOwnerTx(
	ctx context.Context,
	q *sqlc.Queries,
	ownerID uuid.UUID,
) error {
	_, err := q.LockAdaptiveOwner(ctx, dbUUID(ownerID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock adaptive owner %s: %w", ownerID, err)
	}
	tombstoned, tombstoneErr := q.AdaptiveIdentityTombstoned(ctx, dbUUID(ownerID))
	if tombstoneErr != nil {
		return fmt.Errorf("check adaptive owner tombstone: %w", tombstoneErr)
	}
	if tombstoned {
		return ErrOwnerTombstoned
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("adaptive owner %s not found: %w", ownerID, pgx.ErrNoRows)
	}
	return nil
}

func (s *Store) recordOwnerLockedTx(
	ctx context.Context,
	q *sqlc.Queries,
	event Event,
) (sequence int64, duplicate bool, err error) {
	if err := q.LockAdaptiveEvent(ctx, event.ID.String()); err != nil {
		return 0, false, fmt.Errorf("lock adaptive event: %w", err)
	}
	existing, err := q.GetAdaptiveOutboxByID(ctx, dbUUID(event.ID))
	switch {
	case err == nil:
		if !sameImmutableEvent(existing, event) {
			return 0, false, ErrPayloadConflict
		}
		return existing.Sequence, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, false, fmt.Errorf("read adaptive event: %w", err)
	}

	next, err := q.NextAdaptiveAggregateSequence(ctx, sqlc.NextAdaptiveAggregateSequenceParams{
		OwnerID:     dbUUID(event.OwnerID),
		AggregateID: event.AggregateID,
	})
	if err != nil {
		return 0, false, fmt.Errorf("allocate adaptive sequence: %w", err)
	}
	sequence = int64(next)
	rows, err := q.InsertAdaptiveOutbox(ctx, sqlc.InsertAdaptiveOutboxParams{
		ID:          dbUUID(event.ID),
		OwnerID:     dbUUID(event.OwnerID),
		AggregateID: event.AggregateID,
		Sequence:    sequence,
		DecisionID:  dbUUID(event.DecisionID),
		EventKind:   string(event.Kind),
		Payload:     event.Payload,
		PayloadHash: event.PayloadHash,
		AvailableAt: dbTime(event.CreatedAt),
		CreatedAt:   dbTime(event.CreatedAt),
	})
	if err != nil {
		return 0, false, fmt.Errorf("insert adaptive event: %w", err)
	}
	if rows != 1 {
		// The SQL guard remains a database backstop for writers outside Store.
		tombstoned, checkErr := q.AdaptiveIdentityTombstoned(ctx, dbUUID(event.OwnerID))
		if checkErr != nil {
			return 0, false, fmt.Errorf("classify guarded adaptive insert: %w", checkErr)
		}
		if tombstoned {
			return 0, false, ErrOwnerTombstoned
		}
		return 0, false, ErrPayloadConflict
	}
	return sequence, false, nil
}

// ListAggregate returns an owner's facts in aggregate-sequence order.
func (s *Store) ListAggregate(ctx context.Context, ownerID uuid.UUID, aggregateID string) ([]OutboxRecord, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("adaptive store requires a database pool")
	}
	var records []OutboxRecord
	err := db.WithIdentityTx(ctx, s.pool, ownerID.String(), func(q *sqlc.Queries) error {
		rows, err := q.ListAdaptiveAggregate(ctx, sqlc.ListAdaptiveAggregateParams{
			OwnerID:     dbUUID(ownerID),
			AggregateID: aggregateID,
		})
		if err != nil {
			return err
		}
		records = make([]OutboxRecord, 0, len(rows))
		for _, row := range rows {
			records = append(records, recordFromRow(row))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list adaptive aggregate %s: %w", aggregateID, err)
	}
	return records, nil
}

// Claim leases one projectable event. SKIP LOCKED permits multiple projectors,
// while the SQL head-of-line predicate prevents sequence N+1 from overtaking an
// unprojected sequence N in the same owner/aggregate stream.
func (s *Store) Claim(ctx context.Context, opts ClaimOptions) (*OutboxRecord, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("adaptive store requires a database pool")
	}
	workerID := strings.TrimSpace(opts.WorkerID)
	if workerID == "" {
		return nil, errors.New("adaptive claim worker_id is required")
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	lease := opts.Lease
	if lease <= 0 {
		lease = time.Minute
	}
	row, err := sqlc.New(s.pool).ClaimAdaptiveOutbox(ctx, sqlc.ClaimAdaptiveOutboxParams{
		WorkerID:       dbText(workerID),
		LeaseExpiresAt: dbTime(now.Add(lease)),
		ClaimedAt:      dbTime(now),
		MaxAttempts:    int32(s.maxAttempts),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim adaptive outbox: %w", err)
	}
	record := recordFromRow(row)
	return &record, nil
}

// MarkProjected completes a lease held by workerID.
func (s *Store) MarkProjected(ctx context.Context, eventID uuid.UUID, workerID string, projectedAt time.Time) error {
	if s == nil || s.pool == nil {
		return errors.New("adaptive store requires a database pool")
	}
	if projectedAt.IsZero() {
		projectedAt = time.Now().UTC()
	}
	rows, err := sqlc.New(s.pool).MarkAdaptiveProjected(ctx, sqlc.MarkAdaptiveProjectedParams{
		ProjectedAt: dbTime(projectedAt),
		EventID:     dbUUID(eventID),
		WorkerID:    dbText(strings.TrimSpace(workerID)),
	})
	if err != nil {
		return fmt.Errorf("mark adaptive event projected: %w", err)
	}
	if rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

// Retry records a failed projection and either reschedules or dead-letters it.
func (s *Store) Retry(ctx context.Context, eventID uuid.UUID, workerID, errorClass string, failedAt, availableAt time.Time) error {
	if s == nil || s.pool == nil {
		return errors.New("adaptive store requires a database pool")
	}
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	if availableAt.IsZero() {
		availableAt = failedAt
	}
	rows, err := sqlc.New(s.pool).RetryAdaptiveOutbox(ctx, sqlc.RetryAdaptiveOutboxParams{
		MaxAttempts: int32(s.maxAttempts),
		AvailableAt: dbTime(availableAt),
		FailedAt:    dbTime(failedAt),
		ErrorClass:  dbText(strings.TrimSpace(errorClass)),
		EventID:     dbUUID(eventID),
		WorkerID:    dbText(strings.TrimSpace(workerID)),
	})
	if err != nil {
		return fmt.Errorf("retry adaptive event: %w", err)
	}
	if rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

// IsOwnerTombstoned reports whether an identity is permanently fenced.
func (s *Store) IsOwnerTombstoned(ctx context.Context, ownerID uuid.UUID) (bool, error) {
	if s == nil || s.pool == nil {
		return false, errors.New("adaptive store requires a database pool")
	}
	tombstoned, err := sqlc.New(s.pool).AdaptiveIdentityTombstoned(ctx, dbUUID(ownerID))
	if err != nil {
		return false, fmt.Errorf("check adaptive owner tombstone: %w", err)
	}
	return tombstoned, nil
}

func sameImmutableEvent(row sqlc.AuraAdaptiveOutbox, event Event) bool {
	return uuid.UUID(row.OwnerID.Bytes) == event.OwnerID &&
		row.AggregateID == event.AggregateID &&
		uuid.UUID(row.DecisionID.Bytes) == event.DecisionID &&
		EventKind(row.EventKind) == event.Kind &&
		bytes.Equal(row.PayloadHash, event.PayloadHash)
}

func recordFromRow(row sqlc.AuraAdaptiveOutbox) OutboxRecord {
	return OutboxRecord{
		ID:          uuid.UUID(row.ID.Bytes),
		OwnerID:     uuid.UUID(row.OwnerID.Bytes),
		AggregateID: row.AggregateID,
		Sequence:    row.Sequence,
		DecisionID:  uuid.UUID(row.DecisionID.Bytes),
		Kind:        EventKind(row.EventKind),
		Payload:     append([]byte(nil), row.Payload...),
		PayloadHash: append([]byte(nil), row.PayloadHash...),
		Status:      row.Status,
		Attempts:    row.Attempts,
		AvailableAt: row.AvailableAt.Time,
		CreatedAt:   row.CreatedAt.Time,
	}
}

func dbUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func dbTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func dbText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
