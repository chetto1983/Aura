package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const maintenanceRegistryLockKey int64 = 0x4155524152455345 // "AURARESE"
const maintenanceRecoveryLease = 5 * time.Minute

type maintenanceLockKey struct {
	classID  int32
	objectID int32
}

type maintenanceOperationRow struct {
	payloadHash     []byte
	state           State
	replayBody      []byte
	replayExpiresAt *time.Time
	retryAfter      time.Time
	leaseExpiresAt  time.Time
}

// MaintenanceRegistry keeps destructive maintenance ownership outside the
// application schema that db reset destroys and recreates.
type MaintenanceRegistry struct {
	conn *pgx.Conn
	now  func() time.Time
}

// OpenMaintenanceRegistry opens and bootstraps the reset-safe public registry.
// The migration role owns this table; the runtime role receives no access.
func OpenMaintenanceRegistry(ctx context.Context, migrateURL string) (*MaintenanceRegistry, error) {
	if migrateURL == "" {
		return nil, errors.New("maintenance registry URL is empty")
	}
	conn, err := pgx.Connect(ctx, migrateURL)
	if err != nil {
		return nil, errors.New("connect maintenance registry")
	}
	registry := &MaintenanceRegistry{conn: conn, now: time.Now}
	if err := registry.ensure(ctx); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return registry, nil
}

func (r *MaintenanceRegistry) ensure(ctx context.Context) error {
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin maintenance registry bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, maintenanceRegistryLockKey); err != nil {
		return fmt.Errorf("lock maintenance registry bootstrap: %w", err)
	}
	if _, err := tx.Exec(ctx, `
CREATE TABLE IF NOT EXISTS public.aura_maintenance_operations (
    identity_id       text        NOT NULL,
    operation_scope   text        NOT NULL CHECK (operation_scope = 'cli.command'),
    operation_key     text        NOT NULL,
    payload_hash      bytea       NOT NULL CHECK (octet_length(payload_hash) = 32),
    state             text        NOT NULL CHECK (state IN ('in_progress', 'completed', 'indeterminate')),
    replay_body       jsonb,
    replay_expires_at timestamptz,
    retry_after       timestamptz NOT NULL,
    lease_expires_at  timestamptz NOT NULL,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    PRIMARY KEY (identity_id, operation_scope, operation_key)
)`); err != nil {
		return fmt.Errorf("create maintenance registry: %w", err)
	}
	if _, err := tx.Exec(ctx, `
ALTER TABLE public.aura_maintenance_operations
ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz`); err != nil {
		return fmt.Errorf("add maintenance recovery lease: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.aura_maintenance_operations
SET lease_expires_at = $1
WHERE lease_expires_at IS NULL`, r.now().UTC().Add(maintenanceRecoveryLease)); err != nil {
		return fmt.Errorf("backfill maintenance recovery lease: %w", err)
	}
	if _, err := tx.Exec(ctx, `
ALTER TABLE public.aura_maintenance_operations
ALTER COLUMN lease_expires_at SET NOT NULL`); err != nil {
		return fmt.Errorf("require maintenance recovery lease: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit maintenance registry bootstrap: %w", err)
	}
	return nil
}

// Close releases the migration-role connection and any live-owner session lock.
func (r *MaintenanceRegistry) Close() {
	if r != nil && r.conn != nil {
		_ = r.conn.Close(context.Background())
	}
}

// Begin acquires a reset operation or returns its durable terminal receipt.
func (r *MaintenanceRegistry) Begin(ctx context.Context, request BeginRequest) (BeginDecision, error) {
	if err := request.Validate(); err != nil {
		return BeginDecision{}, err
	}
	if request.Operation.Scope != ScopeCLICommand {
		return BeginDecision{}, errors.New("maintenance registry only accepts CLI operations")
	}
	now := r.now().UTC()
	lockKey := maintenanceOwnerLockKey(request.Operation)
	hasOwnerLock, err := r.tryOwnerLock(ctx, lockKey)
	if err != nil {
		return BeginDecision{}, err
	}
	releaseOwnerLock := hasOwnerLock
	defer func() {
		if !releaseOwnerLock {
			return
		}
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.unlockOwnerLock(unlockCtx, lockKey)
	}()

	if hasOwnerLock {
		tag, err := r.conn.Exec(ctx, `
INSERT INTO public.aura_maintenance_operations (
    identity_id, operation_scope, operation_key, payload_hash, state,
    retry_after, lease_expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, 'in_progress', $5, $6, $7, $7)
ON CONFLICT (identity_id, operation_scope, operation_key) DO NOTHING`,
			request.Operation.IdentityID, request.Operation.Scope, request.Operation.Key,
			request.Fingerprint[:], now.Add(defaultRetryAfter), now.Add(maintenanceRecoveryLease), now)
		if err != nil {
			return BeginDecision{}, fmt.Errorf("begin maintenance operation: %w", err)
		}
		if tag.RowsAffected() == 1 {
			// The owner keeps this session lock until Close, independently of the
			// client-facing retry hint and durable crash-recovery lease.
			releaseOwnerLock = false
			return BeginDecision{Decision: DecisionAcquired}, nil
		}
	}

	row, err := r.readOperation(ctx, request.Operation)
	if errors.Is(err, pgx.ErrNoRows) && !hasOwnerLock {
		// The live owner may still be between acquiring its session lock and
		// inserting the durable row. Fail closed and tell the duplicate to retry.
		return BeginDecision{Decision: DecisionInProgress, RetryAfter: defaultRetryAfter}, nil
	}
	if err != nil {
		return BeginDecision{}, fmt.Errorf("read maintenance operation: %w", err)
	}
	if !bytes.Equal(row.payloadHash, request.Fingerprint[:]) {
		return BeginDecision{Decision: DecisionConflict}, ErrConflict
	}

	// Only an owner-free operation whose independent recovery lease has expired
	// can be sealed indeterminate. This never reacquires or re-executes the reset.
	if hasOwnerLock && row.state == StateInProgress && !row.leaseExpiresAt.After(now) {
		tag, err := r.conn.Exec(ctx, `
UPDATE public.aura_maintenance_operations
SET state = 'indeterminate', updated_at = $1
WHERE identity_id = $2 AND operation_scope = $3 AND operation_key = $4
	  AND payload_hash = $5 AND state = 'in_progress' AND lease_expires_at <= $1`,
			now, request.Operation.IdentityID, request.Operation.Scope, request.Operation.Key, request.Fingerprint[:])
		if err != nil {
			return BeginDecision{}, fmt.Errorf("expire maintenance operation: %w", err)
		}
		if tag.RowsAffected() == 1 {
			row.state = StateIndeterminate
		} else {
			// Complete may have won the conditional transition. Re-read its receipt
			// rather than returning the stale in-progress snapshot.
			row, err = r.readOperation(ctx, request.Operation)
			if err != nil {
				return BeginDecision{}, fmt.Errorf("re-read maintenance operation: %w", err)
			}
		}
	}

	switch row.state {
	case StateCompleted:
		if row.replayExpiresAt == nil || !row.replayExpiresAt.After(now) || len(row.replayBody) == 0 {
			return BeginDecision{Decision: DecisionResultExpired}, nil
		}
		result := &ReplayResult{Body: row.replayBody, StatusCode: 200, ExpiresAt: row.replayExpiresAt.UTC()}
		if err := result.Validate(); err != nil {
			return BeginDecision{}, fmt.Errorf("read maintenance operation: %w", err)
		}
		return BeginDecision{Decision: DecisionReplay, Replay: result}, nil
	case StateInProgress:
		retry := row.retryAfter.Sub(now)
		if retry <= 0 {
			retry = defaultRetryAfter
		}
		if retry > MaxRetryAfter {
			retry = MaxRetryAfter
		}
		return BeginDecision{Decision: DecisionInProgress, RetryAfter: retry}, nil
	case StateIndeterminate:
		return BeginDecision{Decision: DecisionIndeterminate}, nil
	default:
		return BeginDecision{}, errors.New("read maintenance operation: invalid state")
	}
}

func maintenanceOwnerLockKey(operation OperationKey) maintenanceLockKey {
	hasher := sha256.New()
	var length [4]byte
	for _, value := range []string{operation.IdentityID, string(operation.Scope), operation.Key} {
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(value))
	}
	sum := hasher.Sum(nil)
	return maintenanceLockKey{
		classID:  int32(binary.BigEndian.Uint32(sum[:4])),
		objectID: int32(binary.BigEndian.Uint32(sum[4:8])),
	}
}

func (r *MaintenanceRegistry) tryOwnerLock(ctx context.Context, key maintenanceLockKey) (bool, error) {
	var acquired bool
	if err := r.conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1, $2)`, key.classID, key.objectID).Scan(&acquired); err != nil {
		return false, fmt.Errorf("lock maintenance operation owner: %w", err)
	}
	return acquired, nil
}

func (r *MaintenanceRegistry) unlockOwnerLock(ctx context.Context, key maintenanceLockKey) error {
	var released bool
	if err := r.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1, $2)`, key.classID, key.objectID).Scan(&released); err != nil {
		return fmt.Errorf("unlock maintenance operation owner: %w", err)
	}
	if !released {
		return errors.New("unlock maintenance operation owner: lock was not held")
	}
	return nil
}

func (r *MaintenanceRegistry) readOperation(ctx context.Context, operation OperationKey) (maintenanceOperationRow, error) {
	var row maintenanceOperationRow
	var state string
	err := r.conn.QueryRow(ctx, `
SELECT payload_hash, state, replay_body, replay_expires_at, retry_after, lease_expires_at
FROM public.aura_maintenance_operations
WHERE identity_id = $1 AND operation_scope = $2 AND operation_key = $3`,
		operation.IdentityID, operation.Scope, operation.Key,
	).Scan(&row.payloadHash, &state, &row.replayBody, &row.replayExpiresAt, &row.retryAfter, &row.leaseExpiresAt)
	row.state = State(state)
	return row, err
}

// Complete persists the reset receipt after the child has rebuilt the schema.
func (r *MaintenanceRegistry) Complete(ctx context.Context, request CompleteRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.Operation.Scope != ScopeCLICommand {
		return errors.New("maintenance registry only accepts CLI operations")
	}
	tag, err := r.conn.Exec(ctx, `
UPDATE public.aura_maintenance_operations
SET state = 'completed', replay_body = $1, replay_expires_at = $2, updated_at = $3
WHERE identity_id = $4 AND operation_scope = $5 AND operation_key = $6
  AND payload_hash = $7 AND state = 'in_progress'`,
		request.Result.Body, request.Result.ExpiresAt.UTC(), r.now().UTC(),
		request.Operation.IdentityID, request.Operation.Scope, request.Operation.Key, request.Fingerprint[:])
	if err != nil {
		return fmt.Errorf("complete maintenance operation: %w", err)
	}
	return requireOneTransition(tag.RowsAffected())
}

// MarkIndeterminate prevents automatic re-execution after an ambiguous reset.
func (r *MaintenanceRegistry) MarkIndeterminate(ctx context.Context, operation OperationKey, fingerprint [32]byte) error {
	if err := (BeginRequest{Operation: operation, Fingerprint: fingerprint}).Validate(); err != nil {
		return err
	}
	if operation.Scope != ScopeCLICommand {
		return errors.New("maintenance registry only accepts CLI operations")
	}
	tag, err := r.conn.Exec(ctx, `
UPDATE public.aura_maintenance_operations
SET state = 'indeterminate', updated_at = $1
WHERE identity_id = $2 AND operation_scope = $3 AND operation_key = $4
  AND payload_hash = $5 AND state = 'in_progress'`,
		r.now().UTC(), operation.IdentityID, operation.Scope, operation.Key, fingerprint[:])
	if err != nil {
		return fmt.Errorf("mark maintenance operation indeterminate: %w", err)
	}
	return requireOneTransition(tag.RowsAffected())
}
