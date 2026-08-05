//go:build db_integration

package documents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPipelineWritesSerializeBeforeDocumentDelete(t *testing.T) {
	pool := pipelineDisposablePool(t)
	tests := []struct {
		name  string
		write func(context.Context, *sqlc.Queries, pipelineStoreFixture) error
		check func(context.Context, *testing.T, *pgxpool.Pool, pipelineStoreFixture)
	}{
		{
			name: "candidate",
			write: func(ctx context.Context, queries *sqlc.Queries, fixture pipelineStoreFixture) error {
				prepared, err := prepareCandidate(pipelineCandidateForVersion(t, fixture, fixture.v2, 2))
				if err != nil {
					return err
				}
				if _, err = queries.GetPipelineCandidateVersion(ctx, prepared.version); err != nil {
					return err
				}
				return errors.New("candidate eligibility unexpectedly survived delete")
			},
			check: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture pipelineStoreFixture) {
				t.Helper()
				assertPipelineRowCount(t, ctx, pool, fixture, "aura.document_chunks", 0)
			},
		},
		{
			name: "derived artifact",
			write: func(ctx context.Context, queries *sqlc.Queries, fixture pipelineStoreFixture) error {
				params, err := derivedArtifactParams(DerivedArtifactRequest{
					IdentityID: fixture.identityID, DocumentID: fixture.documentID,
					VersionID: fixture.v2.ID, PipelineGeneration: fixture.v2.PipelineGeneration,
					Bucket: "documents", ObjectKey: fixture.prefix + "/late-artifact.json",
					Kind: "chunks", SHA256: fixture.v2.SHA256, SizeBytes: 42,
					ContentType: "application/json", RetentionClass: "derived",
				})
				if err != nil {
					return err
				}
				_, err = queries.RecordPipelineDerivedArtifact(ctx, params)
				return err
			},
			check: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture pipelineStoreFixture) {
				t.Helper()
				assertPipelineRowCount(t, ctx, pool, fixture, "aura.storage_objects", 2)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			store := NewPostgresPipelineStore(pool)
			fixture := newPipelineStoreFixture(t, ctx, pool, store)
			lockTx := beginPipelineIdentityTx(t, ctx, pool, fixture.identityID)
			defer func() { _ = lockTx.Rollback(context.Background()) }()
			if _, err := lockTx.Exec(ctx,
				"SELECT id FROM aura.documents WHERE id = $1 FOR UPDATE", fixture.documentID); err != nil {
				t.Fatalf("lock document: %v", err)
			}
			writeTx := beginPipelineIdentityTx(t, ctx, pool, fixture.identityID)
			defer func() { _ = writeTx.Rollback(context.Background()) }()
			var backendPID int32
			if err := writeTx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
				t.Fatalf("write backend pid: %v", err)
			}
			result := make(chan error, 1)
			go func() { result <- test.write(ctx, sqlc.New(writeTx), fixture) }()
			waitForPipelineLock(t, ctx, pool, backendPID)
			if _, err := lockTx.Exec(ctx,
				"UPDATE aura.documents SET status = 'deleting' WHERE id = $1", fixture.documentID); err != nil {
				t.Fatalf("mark deleting: %v", err)
			}
			if err := lockTx.Commit(ctx); err != nil {
				t.Fatalf("commit delete: %v", err)
			}
			if err := <-result; !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("late write error = %v, want pgx.ErrNoRows", err)
			}
			if err := writeTx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Fatalf("rollback write transaction: %v", err)
			}
			test.check(ctx, t, pool, fixture)
		})
	}
}

func beginPipelineIdentityTx(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID string,
) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if err := db.SetTxIdentity(ctx, tx, identityID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("set transaction identity: %v", err)
	}
	return tx
}

func waitForPipelineLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, backendPID int32) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waitType string
		err := pool.QueryRow(ctx,
			"SELECT COALESCE(wait_event_type, '') FROM pg_stat_activity WHERE pid = $1", backendPID,
		).Scan(&waitType)
		if err != nil {
			t.Fatalf("read backend wait state: %v", err)
		}
		if waitType == "Lock" {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("write transaction never waited on document lock: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertPipelineRowCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
	table string,
	want int,
) {
	t.Helper()
	err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		var count int
		query := fmt.Sprintf("SELECT count(*) FROM %s WHERE document_id = $1", table) //nolint:gosec // table is test-owned.
		if err := tx.QueryRow(ctx, query, fixture.documentID).Scan(&count); err != nil {
			return err
		}
		if count != want {
			return fmt.Errorf("%s count = %d, want %d", table, count, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
