package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

const (
	defaultLeaseDuration = 5 * time.Minute
	defaultRetryAfter    = time.Second
	defaultExpiryBatch   = 100
	maxExpiryBatch       = 1000
)

// Config controls operation leases, retry guidance, expiry batch size, and
// the clock seam used by deterministic tests. Zero values select safe defaults.
type Config struct {
	Now           func() time.Time
	LeaseDuration time.Duration
	RetryAfter    time.Duration
	ExpiryBatch   int
}

// ExpiryReport is the bounded amount of replay material cleared by one sweep.
type ExpiryReport struct {
	Cleared int
	Bytes   int64
}

type operationQueries interface {
	TryStartOperation(context.Context, sqlc.TryStartOperationParams) (int64, error)
	TryRecoverExpiredOperation(context.Context, sqlc.TryRecoverExpiredOperationParams) (int64, error)
	GetOperation(context.Context, sqlc.GetOperationParams) (sqlc.GetOperationRow, error)
	CompleteOperation(context.Context, sqlc.CompleteOperationParams) (int64, error)
	MarkOperationIndeterminate(context.Context, sqlc.MarkOperationIndeterminateParams) (int64, error)
	MarkOperationRejected(context.Context, sqlc.MarkOperationRejectedParams) (int64, error)
	ListExpiredReplayBodies(context.Context, sqlc.ListExpiredReplayBodiesParams) ([]sqlc.ListExpiredReplayBodiesRow, error)
	ClearExpiredReplayBody(context.Context, sqlc.ClearExpiredReplayBodyParams) (int64, error)
}

// RecoverExpired atomically renews only the exact expired in-progress operation.
func (s *Store) RecoverExpired(
	ctx context.Context,
	request BeginRequest,
) (ClaimToken, bool, error) {
	if err := request.Validate(); err != nil {
		return 0, false, err
	}
	now := s.now().UTC()
	identityID, _ := operationUUID(request.Operation.IdentityID)
	generation, err := s.queries.TryRecoverExpiredOperation(
		ctx,
		sqlc.TryRecoverExpiredOperationParams{
			LeaseExpiresAt: timestamp(now.Add(s.leaseDuration)),
			RetryAfter:     timestamp(now.Add(s.retryAfter)),
			Now:            timestamp(now),
			IdentityID:     identityID,
			OperationScope: string(request.Operation.Scope),
			OperationKey:   request.Operation.Key,
			PayloadHash:    append([]byte(nil), request.Fingerprint[:]...),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("recover expired idempotency operation: %w", err)
	}
	if generation <= 0 {
		return 0, false, fmt.Errorf(
			"recover expired idempotency operation: invalid claim token",
		)
	}
	return ClaimToken(generation), true, nil
}

// Store owns atomic operation acquisition, terminal transitions, replay, and
// bounded expiry over the generated idempotency registry queries.
type Store struct {
	queries       operationQueries
	now           func() time.Time
	leaseDuration time.Duration
	retryAfter    time.Duration
	expiryBatch   int32
	telemetry     *idempotencyTelemetry
}

// New constructs a Store over a pool or transaction implementing sqlc.DBTX.
func New(database sqlc.DBTX, cfg Config) *Store {
	return newStore(sqlc.New(database), cfg)
}

func newStore(queries operationQueries, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.RetryAfter <= 0 || cfg.RetryAfter > MaxRetryAfter {
		cfg.RetryAfter = defaultRetryAfter
	}
	if cfg.ExpiryBatch <= 0 {
		cfg.ExpiryBatch = defaultExpiryBatch
	}
	if cfg.ExpiryBatch > maxExpiryBatch {
		cfg.ExpiryBatch = maxExpiryBatch
	}
	return &Store{
		queries: queries, now: cfg.Now, leaseDuration: cfg.LeaseDuration,
		retryAfter: cfg.RetryAfter, expiryBatch: int32(cfg.ExpiryBatch),
		telemetry: newIdempotencyTelemetry(otel.Meter(idempotencyMeterName)),
	}
}

// Begin atomically acquires a new operation or returns the durable decision
// for an existing identity/scope/key. Only DecisionAcquired permits an effect.
func (s *Store) Begin(ctx context.Context, request BeginRequest) (decision BeginDecision, err error) {
	defer func() { s.telemetry.recordBegin(ctx, decision) }()
	if err := request.Validate(); err != nil {
		return BeginDecision{}, err
	}
	now := s.now().UTC()
	identityID, _ := operationUUID(request.Operation.IdentityID)
	params := sqlc.TryStartOperationParams{
		IdentityID: identityID, OperationScope: string(request.Operation.Scope), OperationKey: request.Operation.Key,
		PayloadHash:    append([]byte(nil), request.Fingerprint[:]...),
		LeaseExpiresAt: timestamp(now.Add(s.leaseDuration)), RetryAfter: timestamp(now.Add(s.retryAfter)), Now: timestamp(now),
	}
	if request.Audit != nil {
		params.AuditConversationID, _ = operationUUID(request.Audit.ConversationID)
		params.AuditRequestID, _ = operationUUID(request.Audit.RequestID)
		params.AuditToolCallID = optionalText(request.Audit.ToolCallID)
	}
	generation, err := s.queries.TryStartOperation(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.readExistingDecision(ctx, request, identityID, now)
	}
	if err != nil {
		return BeginDecision{}, fmt.Errorf("begin idempotency operation: %w", err)
	}
	if generation <= 0 {
		return BeginDecision{}, fmt.Errorf("begin idempotency operation: invalid claim token")
	}
	return BeginDecision{
		Decision: DecisionAcquired, ClaimToken: ClaimToken(generation),
	}, nil
}

func (s *Store) readExistingDecision(ctx context.Context, request BeginRequest, identityID pgtype.UUID, now time.Time) (BeginDecision, error) {
	row, err := s.queries.GetOperation(ctx, sqlc.GetOperationParams{
		IdentityID: identityID, OperationScope: string(request.Operation.Scope), OperationKey: request.Operation.Key,
	})
	if err != nil {
		return BeginDecision{}, fmt.Errorf("read existing idempotency operation: %w", err)
	}
	if !bytes.Equal(row.PayloadHash, request.Fingerprint[:]) {
		return BeginDecision{Decision: DecisionConflict}, ErrConflict
	}
	state, err := ParseState(row.State)
	if err != nil {
		return BeginDecision{}, fmt.Errorf("read existing idempotency operation: %w", err)
	}
	switch state {
	case StateCompleted, StateRejected:
		if !row.ReplayExpiresAt.Valid {
			return BeginDecision{}, fmt.Errorf("read existing idempotency operation: terminal replay expiry is missing")
		}
		rejected := state == StateRejected
		// Expiry is an authorization boundary, independent of physical GC. Never
		// emit retained HTTP/tool/CLI bytes at or after the durable deadline.
		if !row.ReplayExpiresAt.Time.After(now) {
			if rejected {
				return BeginDecision{Decision: DecisionRejected}, nil
			}
			return BeginDecision{Decision: DecisionResultExpired}, nil
		}
		if row.ReplayClearedAt.Valid || (len(row.ReplayBody) == 0 && !row.ReplayPreview.Valid && !row.ReplaySidecarRef.Valid && !row.ReplayStatusCode.Valid && len(row.ReplayHeaders) == 0) {
			if rejected {
				return BeginDecision{Decision: DecisionRejected}, nil
			}
			return BeginDecision{Decision: DecisionResultExpired}, nil
		}
		headers := map[string]string(nil)
		if len(row.ReplayHeaders) != 0 {
			if err := json.Unmarshal(row.ReplayHeaders, &headers); err != nil {
				return BeginDecision{}, fmt.Errorf("read existing idempotency operation: invalid replay headers")
			}
		}
		decision := BeginDecision{Decision: DecisionReplay, Replay: &ReplayResult{
			Body: append([]byte(nil), row.ReplayBody...), Preview: textValue(row.ReplayPreview),
			SidecarRef: textValue(row.ReplaySidecarRef), StatusCode: int(row.ReplayStatusCode.Int16),
			Headers: headers, ExpiresAt: row.ReplayExpiresAt.Time,
		}}
		if rejected {
			decision.Decision = DecisionRejected
		}
		if err := decision.Validate(); err != nil {
			return BeginDecision{}, fmt.Errorf("read existing idempotency operation: %w", err)
		}
		return decision, nil
	case StateInProgress:
		retry := s.retryAfter
		if row.RetryAfter.Valid && row.RetryAfter.Time.After(now) {
			retry = row.RetryAfter.Time.Sub(now)
		}
		if retry > MaxRetryAfter {
			retry = MaxRetryAfter
		}
		return BeginDecision{Decision: DecisionInProgress, RetryAfter: retry}, nil
	case StateIndeterminate:
		return BeginDecision{Decision: DecisionIndeterminate}, nil
	default:
		return BeginDecision{}, fmt.Errorf("read existing idempotency operation: invalid state")
	}
}

// Complete changes an owned in-progress operation to completed and attaches a
// bounded replay result. A zero-row conditional update is a stale transition.
func (s *Store) Complete(ctx context.Context, request CompleteRequest) (err error) {
	defer func() { s.telemetry.recordCompletion(ctx, err) }()
	if err := request.Validate(); err != nil {
		return err
	}
	now := s.now().UTC()
	identityID, _ := operationUUID(request.Operation.IdentityID)
	headers, err := replayHeadersJSON(request.Result.Headers)
	if err != nil {
		return err
	}
	affected, err := s.queries.CompleteOperation(ctx, sqlc.CompleteOperationParams{
		ReplayBody: append([]byte(nil), request.Result.Body...), ReplayPreview: optionalText(request.Result.Preview),
		ReplaySidecarRef: optionalText(request.Result.SidecarRef),
		ReplayStatusCode: optionalInt2(request.Result.StatusCode), ReplayHeaders: headers,
		ReplayBytes:     int64(len(request.Result.Body) + len(request.Result.Preview) + len(request.Result.SidecarRef) + len(headers)),
		ReplayExpiresAt: timestamp(request.Result.ExpiresAt.UTC()), Now: timestamp(now), IdentityID: identityID,
		OperationScope: string(request.Operation.Scope), OperationKey: request.Operation.Key,
		PayloadHash: append([]byte(nil), request.Fingerprint[:]...),
		ClaimToken:  int64(request.ClaimToken),
	})
	if err != nil {
		return fmt.Errorf("complete idempotency operation: %w", err)
	}
	return requireOneTransition(affected)
}

// MarkIndeterminate makes ambiguous work terminal without permitting replay or
// reacquisition. The fingerprint and in-progress state must both still match.
func (s *Store) MarkIndeterminate(
	ctx context.Context,
	operation OperationKey,
	fingerprint [32]byte,
	claimToken ClaimToken,
) (err error) {
	defer func() { s.telemetry.recordIndeterminate(ctx, err) }()
	if err := (BeginRequest{Operation: operation, Fingerprint: fingerprint}).Validate(); err != nil {
		return err
	}
	if claimToken <= 0 {
		return fmt.Errorf("idempotency claim token is required")
	}
	identityID, _ := operationUUID(operation.IdentityID)
	affected, err := s.queries.MarkOperationIndeterminate(ctx, sqlc.MarkOperationIndeterminateParams{
		Now: timestamp(s.now().UTC()), IdentityID: identityID, OperationScope: string(operation.Scope),
		OperationKey: operation.Key, PayloadHash: append([]byte(nil), fingerprint[:]...),
		ClaimToken: int64(claimToken),
	})
	if err != nil {
		return fmt.Errorf("mark idempotency operation indeterminate: %w", err)
	}
	return requireOneTransition(affected)
}

// MarkRejected makes a deterministic no-effect domain failure terminal and
// retains its bounded error envelope for subsequent callers.
func (s *Store) MarkRejected(ctx context.Context, request RejectRequest) (err error) {
	defer func() { s.telemetry.recordRejected(ctx, err) }()
	if err := request.Validate(); err != nil {
		return err
	}
	now := s.now().UTC()
	identityID, _ := operationUUID(request.Operation.IdentityID)
	affected, err := s.queries.MarkOperationRejected(ctx, sqlc.MarkOperationRejectedParams{
		ReplayBody:      append([]byte(nil), request.Result.Body...),
		ReplayPreview:   optionalText(request.Result.Preview),
		ReplayBytes:     int64(len(request.Result.Body) + len(request.Result.Preview)),
		ReplayExpiresAt: timestamp(request.Result.ExpiresAt.UTC()),
		Now:             timestamp(now), IdentityID: identityID,
		OperationScope: string(request.Operation.Scope), OperationKey: request.Operation.Key,
		PayloadHash: append([]byte(nil), request.Fingerprint[:]...),
		ClaimToken:  int64(request.ClaimToken),
	})
	if err != nil {
		return fmt.Errorf("mark idempotency operation rejected: %w", err)
	}
	return requireOneTransition(affected)
}

// ExpireReplayBodies clears at most the configured number of replay payloads
// at or before the cutoff. Registry state and audit linkage are never deleted.
func (s *Store) ExpireReplayBodies(ctx context.Context, before time.Time) (ExpiryReport, error) {
	if before.IsZero() {
		return ExpiryReport{}, fmt.Errorf("idempotency replay expiry cutoff is required")
	}
	before = before.UTC()
	rows, err := s.queries.ListExpiredReplayBodies(ctx, sqlc.ListExpiredReplayBodiesParams{
		Before: timestamp(before), BatchSize: s.expiryBatch,
	})
	if err != nil {
		return ExpiryReport{}, fmt.Errorf("list expired idempotency replays: %w", err)
	}
	report := ExpiryReport{}
	clearedAt := timestamp(s.now().UTC())
	for _, row := range rows {
		if row.ReplayBytes < 0 || math.MaxInt64-report.Bytes < row.ReplayBytes {
			return report, fmt.Errorf("expire idempotency replays: invalid replay byte count")
		}
		affected, err := s.queries.ClearExpiredReplayBody(ctx, sqlc.ClearExpiredReplayBodyParams{
			ClearedAt: clearedAt, IdentityID: row.IdentityID, OperationScope: row.OperationScope,
			OperationKey: row.OperationKey, Before: timestamp(before),
		})
		if err != nil {
			return report, fmt.Errorf("clear expired idempotency replay: %w", err)
		}
		switch affected {
		case 0:
			continue
		case 1:
			report.Cleared++
			report.Bytes += row.ReplayBytes
		default:
			return report, fmt.Errorf("clear expired idempotency replay: unexpected affected row count")
		}
	}
	return report, nil
}

func requireOneTransition(affected int64) error {
	if affected == 1 {
		return nil
	}
	if affected == 0 {
		return ErrStaleTransition
	}
	return fmt.Errorf("idempotency transition: unexpected affected row count")
}

func operationUUID(raw string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func optionalInt2(value int) pgtype.Int2 {
	return pgtype.Int2{Int16: int16(value), Valid: value != 0}
}

func replayHeadersJSON(headers map[string]string) ([]byte, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("marshal idempotency replay headers: %w", err)
	}
	return encoded, nil
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
