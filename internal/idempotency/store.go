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
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/chetto1983/aura/internal/db"
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
	params := sqlc.TryRecoverExpiredOperationParams{
		LeaseExpiresAt: timestamp(now.Add(s.leaseDuration)),
		RetryAfter:     timestamp(now.Add(s.retryAfter)),
		Now:            timestamp(now),
		IdentityID:     identityID,
		OperationScope: string(request.Operation.Scope),
		OperationKey:   request.Operation.Key,
		PayloadHash:    append([]byte(nil), request.Fingerprint[:]...),
	}
	var token ClaimToken
	var recovered bool
	err := s.withIdentity(ctx, request.Operation.IdentityID, func(q operationQueries) error {
		generation, qErr := q.TryRecoverExpiredOperation(ctx, params)
		if errors.Is(qErr, pgx.ErrNoRows) {
			return nil
		}
		if qErr != nil {
			return fmt.Errorf("recover expired idempotency operation: %w", qErr)
		}
		if generation <= 0 {
			return fmt.Errorf(
				"recover expired idempotency operation: invalid claim token",
			)
		}
		token, recovered = ClaimToken(generation), true
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return token, recovered, nil
}

// Store owns atomic operation acquisition, terminal transitions, replay, and
// bounded expiry over the generated idempotency registry queries.
//
// pool is the RLS carrier, not a second query handle: migration 0087 put a fail-closed
// floor on aura.idempotency_operations, so every statement below runs inside a
// transaction that first binds app.current_identity. See withIdentity.
type Store struct {
	pool          *pgxpool.Pool
	queries       operationQueries
	now           func() time.Time
	leaseDuration time.Duration
	retryAfter    time.Duration
	expiryBatch   int32
	telemetry     *idempotencyTelemetry
}

// New constructs a Store over a pool or transaction implementing sqlc.DBTX.
//
// A *pgxpool.Pool is ALSO kept as the pool, so the Store can open the identity-scoped
// transactions aura.idempotency_operations now requires. Every deployed caller
// (cmd/aura/chat_boot.go, cmd/aura/idempotency.go) already passes one — which is why the
// signature does not change and no call site moves. Any other DBTX is a handle the caller
// already owns and is responsible for scoping; it is used as-is.
func New(database sqlc.DBTX, cfg Config) *Store {
	store := newStore(sqlc.New(database), cfg)
	if pool, ok := database.(*pgxpool.Pool); ok {
		store.pool = pool
	}
	return store
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

// withIdentity runs fn with app.current_identity bound to identityID, so the fail-closed
// floor migration 0087 put on aura.idempotency_operations admits exactly that principal's
// registry rows. Without it every Begin on a deployed daemon fails 42501: the floor is AS
// RESTRICTIVE, so an unset session variable refuses the INSERT outright.
//
// The scope is the identity the REQUEST carries, never the ambient context. A registry
// entry belongs to the principal the operation was issued FOR, which is not necessarily
// whoever is executing it, and OperationKey.Validate has already rejected anything that is
// not a uuid before any caller reaches here. This is the conversations.Store.Create side of
// the choice db.WithCallerIdentityTx documents, not the askuser.Store side.
//
// A pool-less Store is the injected-DBTX construction the package's unit tests use
// (newStore over a fakeOperationQueries): it has no transaction to open, so it runs fn on
// the injected queries directly. Production always goes through New with a pool.
func (s *Store) withIdentity(ctx context.Context, identityID string, fn func(operationQueries) error) error {
	if s.pool == nil {
		return fn(s.queries)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		return fn(q)
	})
}

// transition runs one conditional terminal UPDATE as identityID and turns its affected-row
// count into the stale-transition contract. Complete, MarkIndeterminate and MarkRejected
// differ only in the statement they issue and the label they report, so the identity scope
// and the row-count rule live here once instead of three times.
func (s *Store) transition(
	ctx context.Context,
	identityID, label string,
	exec func(operationQueries) (int64, error),
) error {
	return s.withIdentity(ctx, identityID, func(q operationQueries) error {
		affected, err := exec(q)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return requireOneTransition(affected)
	})
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
	// The acquire and the losing racer's read of the durable decision share ONE
	// transaction: ON CONFLICT DO NOTHING yields zero rows without aborting it, so the
	// GetOperation that follows is a valid statement on the same snapshot. The error is
	// returned ALONGSIDE the decision rather than collapsing it, because DecisionConflict
	// travels with ErrConflict — dropping either half loses the caller's whole answer.
	err = s.withIdentity(ctx, request.Operation.IdentityID, func(q operationQueries) error {
		generation, qErr := q.TryStartOperation(ctx, params)
		if errors.Is(qErr, pgx.ErrNoRows) {
			decision, qErr = s.readExistingDecision(ctx, q, request, identityID, now)
			return qErr
		}
		if qErr != nil {
			return fmt.Errorf("begin idempotency operation: %w", qErr)
		}
		if generation <= 0 {
			return fmt.Errorf("begin idempotency operation: invalid claim token")
		}
		decision = BeginDecision{
			Decision: DecisionAcquired, ClaimToken: ClaimToken(generation),
		}
		return nil
	})
	return decision, err
}

func (s *Store) readExistingDecision(ctx context.Context, q operationQueries, request BeginRequest, identityID pgtype.UUID, now time.Time) (BeginDecision, error) {
	row, err := q.GetOperation(ctx, sqlc.GetOperationParams{
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
	params := sqlc.CompleteOperationParams{
		ReplayBody: append([]byte(nil), request.Result.Body...), ReplayPreview: optionalText(request.Result.Preview),
		ReplaySidecarRef: optionalText(request.Result.SidecarRef),
		ReplayStatusCode: optionalInt2(request.Result.StatusCode), ReplayHeaders: headers,
		ReplayBytes:     int64(len(request.Result.Body) + len(request.Result.Preview) + len(request.Result.SidecarRef) + len(headers)),
		ReplayExpiresAt: timestamp(request.Result.ExpiresAt.UTC()), Now: timestamp(now), IdentityID: identityID,
		OperationScope: string(request.Operation.Scope), OperationKey: request.Operation.Key,
		PayloadHash: append([]byte(nil), request.Fingerprint[:]...),
		ClaimToken:  int64(request.ClaimToken),
	}
	return s.transition(ctx, request.Operation.IdentityID, "complete idempotency operation",
		func(q operationQueries) (int64, error) { return q.CompleteOperation(ctx, params) })
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
	params := sqlc.MarkOperationIndeterminateParams{
		Now: timestamp(s.now().UTC()), IdentityID: identityID, OperationScope: string(operation.Scope),
		OperationKey: operation.Key, PayloadHash: append([]byte(nil), fingerprint[:]...),
		ClaimToken: int64(claimToken),
	}
	return s.transition(ctx, operation.IdentityID, "mark idempotency operation indeterminate",
		func(q operationQueries) (int64, error) { return q.MarkOperationIndeterminate(ctx, params) })
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
	params := sqlc.MarkOperationRejectedParams{
		ReplayBody:      append([]byte(nil), request.Result.Body...),
		ReplayPreview:   optionalText(request.Result.Preview),
		ReplayBytes:     int64(len(request.Result.Body) + len(request.Result.Preview)),
		ReplayExpiresAt: timestamp(request.Result.ExpiresAt.UTC()),
		Now:             timestamp(now), IdentityID: identityID,
		OperationScope: string(request.Operation.Scope), OperationKey: request.Operation.Key,
		PayloadHash: append([]byte(nil), request.Fingerprint[:]...),
		ClaimToken:  int64(request.ClaimToken),
	}
	return s.transition(ctx, request.Operation.IdentityID, "mark idempotency operation rejected",
		func(q operationQueries) (int64, error) { return q.MarkOperationRejected(ctx, params) })
}

// ExpireReplayBodies clears at most the configured number of replay payloads
// at or before the cutoff. Registry state and audit linkage are never deleted.
//
// It is the one genuinely cross-identity operation on the registry, and it cannot name a
// single principal — so it does what conversations.ScanOrphans does: enumerate
// aura.identities (which carries NO row-level security, being the login lookup) and run
// ONE identity-scoped transaction per owner. No RLS bypass, no service role. The per-owner
// passes debit a single global budget, so a sweep stays bounded exactly as it was before.
//
// On a failure the report is best-effort, as it always was. What changed is the shape of
// the residue: the owner being swept rolls back whole, so nothing is left half-cleared,
// while earlier owners' passes have committed and are counted.
func (s *Store) ExpireReplayBodies(ctx context.Context, before time.Time) (ExpiryReport, error) {
	if before.IsZero() {
		return ExpiryReport{}, fmt.Errorf("idempotency replay expiry cutoff is required")
	}
	before = before.UTC()
	report := ExpiryReport{}
	if s.pool == nil {
		_, err := s.expireVisibleReplays(ctx, s.queries, before, s.expiryBatch, &report)
		return report, err
	}
	owners, err := s.replayExpiryOwners(ctx)
	if err != nil {
		return report, err
	}
	budget := s.expiryBatch
	for _, owner := range owners {
		if budget <= 0 {
			break
		}
		var examined int32
		if err := s.withIdentity(ctx, owner, func(q operationQueries) error {
			var passErr error
			examined, passErr = s.expireVisibleReplays(ctx, q, before, budget, &report)
			return passErr
		}); err != nil {
			return report, err
		}
		budget -= examined
	}
	return report, nil
}

// expireVisibleReplays clears every expired replay payload q can see, up to budget rows,
// adding what it cleared to report. It returns how many rows it EXAMINED (not how many it
// cleared) so the cross-identity sweep can debit one global budget across its per-owner
// passes: a row a concurrent writer had already cleared still consumed a slot in the batch.
func (s *Store) expireVisibleReplays(
	ctx context.Context,
	q operationQueries,
	before time.Time,
	budget int32,
	report *ExpiryReport,
) (int32, error) {
	rows, err := q.ListExpiredReplayBodies(ctx, sqlc.ListExpiredReplayBodiesParams{
		Before: timestamp(before), BatchSize: budget,
	})
	if err != nil {
		return 0, fmt.Errorf("list expired idempotency replays: %w", err)
	}
	examined := int32(len(rows)) //nolint:gosec // bounded by budget, itself <= maxExpiryBatch.
	clearedAt := timestamp(s.now().UTC())
	for _, row := range rows {
		if row.ReplayBytes < 0 || math.MaxInt64-report.Bytes < row.ReplayBytes {
			return examined, fmt.Errorf("expire idempotency replays: invalid replay byte count")
		}
		affected, err := q.ClearExpiredReplayBody(ctx, sqlc.ClearExpiredReplayBodyParams{
			ClearedAt: clearedAt, IdentityID: row.IdentityID, OperationScope: row.OperationScope,
			OperationKey: row.OperationKey, Before: timestamp(before),
		})
		if err != nil {
			return examined, fmt.Errorf("clear expired idempotency replay: %w", err)
		}
		switch affected {
		case 0:
			continue
		case 1:
			report.Cleared++
			report.Bytes += row.ReplayBytes
		default:
			return examined, fmt.Errorf("clear expired idempotency replay: unexpected affected row count")
		}
	}
	return examined, nil
}

// replayExpiryOwners lists every identity the sweep must visit. aura.identities carries no
// policy of its own, so this read needs no principal — it is the same identity-less read
// conversations.listOwnerIdentities relies on, and it is what makes a cross-tenant sweep
// possible without widening anything.
func (s *Store) replayExpiryOwners(ctx context.Context) ([]string, error) {
	rows, err := sqlc.New(s.pool).ListIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list idempotency replay expiry owners: %w", err)
	}
	owners := make([]string, 0, len(rows))
	for _, row := range rows {
		owners = append(owners, uuid.UUID(row.ID.Bytes).String())
	}
	return owners, nil
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
