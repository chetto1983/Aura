//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestLockOperationReceiptBlocksConcurrentCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, _ := schema2MigrationDatabase(
		t,
		ctx,
		"aura_evidence_receipt_lock",
		shippedMigrationSteps(t),
	)
	identityID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		identityID,
		"evidence-receipt-lock-"+identityID.String(),
	); err != nil {
		t.Fatalf("seed receipt identity: %v", err)
	}
	fingerprint := bytes.Repeat([]byte{0xa5}, 32)
	now := time.Now().UTC().Truncate(time.Microsecond)
	params := sqlc.LockOperationReceiptParams{
		IdentityID:     dbUUID(identityID),
		OperationScope: "cli.command",
		OperationKey:   "adaptive-receipt-lock",
	}
	queries := sqlc.New(pool)
	started, err := queries.TryStartOperation(
		ctx,
		sqlc.TryStartOperationParams{
			IdentityID: params.IdentityID, OperationScope: params.OperationScope,
			OperationKey: params.OperationKey, PayloadHash: fingerprint,
			LeaseExpiresAt: pgtype.Timestamptz{
				Time: now.Add(time.Minute), Valid: true,
			},
			RetryAfter: pgtype.Timestamptz{
				Time: now.Add(time.Second), Valid: true,
			},
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		},
	)
	if err != nil || started != 1 {
		t.Fatalf("start operation = %d, %v", started, err)
	}

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin receipt lock transaction: %v", err)
	}
	t.Cleanup(func() { rollbackTypedTestTx(t, lockTx) })
	receipt, err := sqlc.New(lockTx).LockOperationReceipt(ctx, params)
	if err != nil {
		t.Fatalf("lock operation receipt: %v", err)
	}
	if !receipt.IdentityID.Valid ||
		uuid.UUID(receipt.IdentityID.Bytes) != identityID ||
		receipt.OperationScope != params.OperationScope ||
		receipt.OperationKey != params.OperationKey ||
		!bytes.Equal(receipt.PayloadHash, fingerprint) ||
		receipt.State != "in_progress" ||
		receipt.Version != started ||
		!receipt.CreatedAt.Valid ||
		!receipt.CreatedAt.Time.Equal(now) {
		t.Fatalf("locked receipt = %#v", receipt)
	}

	complete := sqlc.CompleteOperationParams{
		Now:              pgtype.Timestamptz{Time: now.Add(time.Second), Valid: true},
		IdentityID:       params.IdentityID,
		OperationScope:   params.OperationScope,
		OperationKey:     params.OperationKey,
		PayloadHash:      fingerprint,
		ClaimToken:       started,
		ReplayExpiresAt:  pgtype.Timestamptz{},
		ReplayStatusCode: pgtype.Int2{},
	}
	blockedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocked completion transaction: %v", err)
	}
	t.Cleanup(func() { rollbackTypedTestTx(t, blockedTx) })
	if _, err := blockedTx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		t.Fatalf("set completion lock timeout: %v", err)
	}
	if _, err := sqlc.New(blockedTx).CompleteOperation(ctx, complete); err == nil {
		t.Fatal("completion bypassed the locked operation receipt")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "55P03" {
			t.Fatalf("blocked completion error = %v, want SQLSTATE 55P03", err)
		}
	}
	if err := blockedTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback blocked completion: %v", err)
	}
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release operation receipt lock: %v", err)
	}
	affected, err := queries.CompleteOperation(ctx, complete)
	if err != nil || affected != 1 {
		t.Fatalf("completion after lock release = %d, %v, want 1, nil", affected, err)
	}
}
