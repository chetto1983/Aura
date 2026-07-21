package idempotency

import (
	"context"
	"errors"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// This test-only shim keeps the RED contract buildable until store.go is added.
// It is deleted in the GREEN commit.
type operationQueries interface {
	TryStartOperation(context.Context, sqlc.TryStartOperationParams) (int64, error)
	GetOperation(context.Context, sqlc.GetOperationParams) (sqlc.AuraIdempotencyOperations, error)
	CompleteOperation(context.Context, sqlc.CompleteOperationParams) (int64, error)
	MarkOperationIndeterminate(context.Context, sqlc.MarkOperationIndeterminateParams) (int64, error)
	ListExpiredReplayBodies(context.Context, sqlc.ListExpiredReplayBodiesParams) ([]sqlc.ListExpiredReplayBodiesRow, error)
	ClearExpiredReplayBody(context.Context, sqlc.ClearExpiredReplayBodyParams) (int64, error)
}

type Config struct {
	Now           func() time.Time
	LeaseDuration time.Duration
	RetryAfter    time.Duration
	ExpiryBatch   int
}

type ExpiryReport struct {
	Cleared int
	Bytes   int64
}

type Store struct{}

func newStore(operationQueries, Config) *Store { return &Store{} }

func (s *Store) Begin(context.Context, BeginRequest) (BeginDecision, error) {
	return BeginDecision{}, errors.New("store not implemented")
}

func (s *Store) Complete(context.Context, CompleteRequest) error {
	return errors.New("store not implemented")
}

func (s *Store) MarkIndeterminate(context.Context, OperationKey, [32]byte) error {
	return errors.New("store not implemented")
}

func (s *Store) ExpireReplayBodies(context.Context, time.Time) (ExpiryReport, error) {
	return ExpiryReport{}, errors.New("store not implemented")
}
