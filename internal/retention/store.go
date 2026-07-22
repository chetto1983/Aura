package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLeaseLost means a conditional item transition no longer owns the claim.
var ErrLeaseLost = errors.New("retention item lease lost")

// Status is the durable operation/item lifecycle vocabulary.
type Status string

// Durable retention lifecycle states.
const (
	StatusPlanned   Status = "planned"
	StatusDeleting  Status = "deleting"
	StatusRetryable Status = "retryable"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Operation is the aggregate durable projection of one plan.
type Operation struct {
	ID             string
	Token          string
	PolicyVersion  string
	Status         Status
	CandidateCount int
	PlannedBytes   int64
	CompletedCount int
	CompletedBytes int64
	FailureCount   int
	CreatedAt      time.Time
}

// Item is one claimed mark-remove-finalize unit.
type Item struct {
	ID             string
	OperationID    string
	Candidate      Candidate
	Status         Status
	ArtifactResult string
	RemovedBytes   int64
	FailureClass   string
	ClaimOwner     string
	LeaseExpiresAt time.Time
	AttemptCount   int
}

// OperationStore is the durable seam consumed by Engine and scheduler/CLI adapters.
type OperationStore interface {
	SavePlan(context.Context, Plan, time.Time) (Operation, error)
	GetByToken(context.Context, string) (Operation, error)
	Authorize(context.Context, string, string, string, time.Time) (bool, error)
	Claim(context.Context, string, string, int, time.Time, time.Duration) ([]Item, error)
	RecordArtifact(context.Context, string, string, string, int64, string, time.Time) error
	FinalizeItem(context.Context, string, string, time.Time) error
	RetryItem(context.Context, string, string, string, time.Time) error
	FailItem(context.Context, string, string, string, time.Time) error
	FinalizeOperation(context.Context, string, time.Time) (Operation, error)
}

// Store persists retention plans and conditional lease-owned transitions.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewStore constructs the PostgreSQL retention store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// SavePlan atomically persists an immutable plan and all its items.
func (s *Store) SavePlan(ctx context.Context, plan Plan, now time.Time) (Operation, error) {
	if s == nil || s.pool == nil {
		return Operation{}, fmt.Errorf("retention store is not configured")
	}
	if err := plan.RequireToken(plan.Token); err != nil {
		return Operation{}, err
	}
	var operation Operation
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		row, err := q.CreateRetentionOperation(ctx, sqlc.CreateRetentionOperationParams{
			ID: retentionUUID(), Token: plan.Token, PolicyVersion: plan.PolicyVersion,
			CandidateCount: int32(len(plan.Candidates)), PlannedBytes: plan.TotalBytes, Now: retentionTime(now),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err = q.RefreshPlannedRetentionOperation(ctx, sqlc.RefreshPlannedRetentionOperationParams{
				Now: retentionTime(now), Token: plan.Token,
			}); err != nil {
				return fmt.Errorf("refresh planned retention operation: %w", err)
			}
			row, err = q.GetRetentionOperationByToken(ctx, plan.Token)
		}
		if err != nil {
			return fmt.Errorf("create retention operation: %w", err)
		}
		operation = operationFromRow(row)
		operationID := row.ID
		for _, candidate := range plan.Candidates {
			_, err = q.CreateRetentionItem(ctx, sqlc.CreateRetentionItemParams{
				ID: retentionUUID(), OperationID: operationID, ItemKey: candidate.ItemKey(),
				IdentityID: candidate.IdentityID, ConversationID: retentionText(candidate.ConversationID),
				ArtifactID: retentionText(candidate.ArtifactID), Class: string(candidate.Class),
				Action: string(candidate.Action), ExpectedVersion: candidate.Version,
				ExpectedBytes: candidate.Bytes, Now: retentionTime(now),
			})
			if err != nil {
				return fmt.Errorf("create retention item: %w", err)
			}
		}
		return nil
	})
	return operation, err
}

// GetByToken resolves an already persisted plan without changing it.
func (s *Store) GetByToken(ctx context.Context, token string) (Operation, error) {
	if s == nil || s.q == nil || token == "" {
		return Operation{}, ErrTokenMismatch
	}
	row, err := s.q.GetRetentionOperationByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Operation{}, ErrTokenMismatch
		}
		return Operation{}, fmt.Errorf("get retention operation: %w", err)
	}
	return operationFromRow(row), nil
}

// Authorize atomically transitions a still-planned operation into deleting after
// Engine has rebuilt and matched the current policy/candidate token. False means
// another process won the transition or the immutable authorization no longer matches.
func (s *Store) Authorize(ctx context.Context, operationID, token, policyVersion string, now time.Time) (bool, error) {
	id, err := retentionParseUUID(operationID)
	if err != nil {
		return false, err
	}
	if token == "" || policyVersion == "" {
		return false, ErrTokenMismatch
	}
	rows, err := s.q.AuthorizeRetentionOperation(ctx, sqlc.AuthorizeRetentionOperationParams{
		Now: retentionTime(now), ID: id, Token: token, PolicyVersion: policyVersion,
	})
	if err != nil {
		return false, fmt.Errorf("authorize retention operation: %w", err)
	}
	return rows == 1, nil
}

// Claim marks at most batchSize disjoint items deleting under a bounded lease.
func (s *Store) Claim(ctx context.Context, operationID, owner string, batchSize int, now time.Time, lease time.Duration) ([]Item, error) {
	id, err := retentionParseUUID(operationID)
	if err != nil {
		return nil, err
	}
	if owner == "" || batchSize <= 0 || lease <= 0 {
		return nil, fmt.Errorf("invalid retention claim")
	}
	rows, err := s.q.ClaimRetentionItems(ctx, sqlc.ClaimRetentionItemsParams{
		ClaimOwner: retentionText(owner), LeaseExpiresAt: retentionTime(now.Add(lease)),
		Now: retentionTime(now), OperationID: id, BatchSize: int32(batchSize),
	})
	if err != nil {
		return nil, fmt.Errorf("claim retention items: %w", err)
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, itemFromRow(row))
	}
	return items, nil
}

// RecordArtifact durably records the external removal result before finalization.
func (s *Store) RecordArtifact(ctx context.Context, itemID, owner, result string, bytes int64, failure string, now time.Time) error {
	id, err := retentionParseUUID(itemID)
	if err != nil {
		return err
	}
	n, err := s.q.RecordRetentionArtifactResult(ctx, sqlc.RecordRetentionArtifactResultParams{
		ArtifactResult: result, RemovedBytes: bytes, FailureClass: retentionText(failure),
		Now: retentionTime(now), ID: id, ClaimOwner: retentionText(owner),
	})
	return retentionRows("record artifact result", n, err)
}

// FinalizeItem completes metadata only after removed/absent external state.
func (s *Store) FinalizeItem(ctx context.Context, itemID, owner string, now time.Time) error {
	id, err := retentionParseUUID(itemID)
	if err != nil {
		return err
	}
	n, err := s.q.FinalizeRetentionItem(ctx, sqlc.FinalizeRetentionItemParams{Now: retentionTime(now), ID: id, ClaimOwner: retentionText(owner)})
	return retentionRows("finalize item", n, err)
}

// RetryItem releases a failed claim for a later bounded attempt.
func (s *Store) RetryItem(ctx context.Context, itemID, owner, failure string, now time.Time) error {
	id, err := retentionParseUUID(itemID)
	if err != nil {
		return err
	}
	n, err := s.q.RetryRetentionItem(ctx, sqlc.RetryRetentionItemParams{
		FailureClass: retentionText(failure), Now: retentionTime(now), ID: id, ClaimOwner: retentionText(owner),
	})
	return retentionRows("retry item", n, err)
}

// FailItem records a terminal classified failure.
func (s *Store) FailItem(ctx context.Context, itemID, owner, failure string, now time.Time) error {
	id, err := retentionParseUUID(itemID)
	if err != nil {
		return err
	}
	n, err := s.q.FailRetentionItem(ctx, sqlc.FailRetentionItemParams{
		FailureClass: retentionText(failure), Now: retentionTime(now), ID: id, ClaimOwner: retentionText(owner),
	})
	return retentionRows("fail item", n, err)
}

// FinalizeOperation refreshes aggregate counts and terminal/retryable state.
func (s *Store) FinalizeOperation(ctx context.Context, operationID string, now time.Time) (Operation, error) {
	id, err := retentionParseUUID(operationID)
	if err != nil {
		return Operation{}, err
	}
	row, err := s.q.FinalizeRetentionOperation(ctx, sqlc.FinalizeRetentionOperationParams{Now: retentionTime(now), ID: id})
	if err != nil {
		return Operation{}, fmt.Errorf("finalize retention operation: %w", err)
	}
	return operationFromRow(row), nil
}

func retentionRows(action string, rows int64, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: %w", action, ErrLeaseLost)
	}
	return nil
}

func operationFromRow(row sqlc.AuraRetentionOperations) Operation {
	return Operation{
		ID: retentionUUIDString(row.ID), Token: row.Token, PolicyVersion: row.PolicyVersion,
		Status: Status(row.Status), CandidateCount: int(row.CandidateCount), PlannedBytes: row.PlannedBytes,
		CompletedCount: int(row.CompletedCount), CompletedBytes: row.CompletedBytes, FailureCount: int(row.FailureCount),
		CreatedAt: row.CreatedAt.Time,
	}
}

func itemFromRow(row sqlc.AuraRetentionOperationItems) Item {
	return Item{
		ID: retentionUUIDString(row.ID), OperationID: retentionUUIDString(row.OperationID),
		Candidate: Candidate{IdentityID: row.IdentityID, ConversationID: row.ConversationID.String,
			ArtifactID: row.ArtifactID.String, Version: row.ExpectedVersion, Action: Action(row.Action),
			Class: Class(row.Class), Bytes: row.ExpectedBytes},
		Status: Status(row.Status), ArtifactResult: row.ArtifactResult, RemovedBytes: row.RemovedBytes,
		FailureClass: row.FailureClass.String, ClaimOwner: row.ClaimOwner.String,
		LeaseExpiresAt: row.LeaseExpiresAt.Time, AttemptCount: int(row.AttemptCount),
	}
}

func retentionUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
}

func retentionParseUUID(value string) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid retention id: %w", err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func retentionUUIDString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

func retentionTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func retentionText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
