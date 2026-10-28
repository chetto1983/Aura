//go:build db_integration

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
)

func TestLocalIdentityRetirementRequiresBrowserCredentialPurge(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := t.Context()
	localID := legacyLocalIdentity(t, pool)
	target := retirementTarget(t, pool)
	failure := errors.New("credential volume could not be removed")
	for _, purge := range []func(context.Context, string) error{nil, func(_ context.Context, id string) error {
		if id != localID {
			t.Fatalf("purged wrong principal %q", id)
		}
		return failure
	}} {
		if err := retireLegacyLocalIdentity(ctx, pool, target, purge); err == nil {
			t.Fatal("retirement succeeded without purging browser credentials")
		}
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aura.identities WHERE id=$1)`, localID).Scan(&exists); err != nil || !exists {
			t.Fatalf("failed purge deleted the identity: %t, %v", exists, err)
		}
	}
	called := false
	if err := retireLegacyLocalIdentity(ctx, pool, target, func(_ context.Context, id string) error {
		if id != localID {
			t.Fatalf("purged wrong principal %q", id)
		}
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aura.identities WHERE id=$1)`, localID).Scan(&exists); err != nil || exists || !called {
		t.Fatalf("successful purge did not retire identity: exists=%t called=%t err=%v", exists, called, err)
	}
}

// The retirement runs on the runtime pool, so every row it moves sits behind the row security
// of its table. Deleting local cascades whatever it did not move.
func TestLocalIdentityRetirementKeepsLocalsRows(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := t.Context()
	localID := legacyLocalIdentity(t, pool)
	target := retirementTarget(t, pool)

	conversationID, assetID, deletedAssetID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	const telegramUserID = int64(9_000_040_001)
	authulaUserID := "retire-" + uuid.NewString()
	if err := db.WithIdentityTxRaw(ctx, pool, localID, func(tx pgx.Tx) error {
		for _, seed := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)`, []any{conversationID, localID}},
			{`INSERT INTO aura.assets (id, identity_id, source_kind, scope, modality, status, file_name, object_bucket, object_key)
			  VALUES ($1, $2, 'web', 'library', 'document', 'complete', 'kept.pdf', 'aura', 'kept.pdf')`, []any{assetID, localID}},
			{`INSERT INTO aura.assets (id, identity_id, source_kind, scope, modality, status, file_name, object_bucket, object_key)
			  VALUES ($1, $2, 'web', 'library', 'document', 'deleted', 'gone.pdf', 'aura', $3)`, []any{deletedAssetID, localID, "identities/" + localID + "/gone.pdf"}},
			{`INSERT INTO aura.ingestion_jobs (id, job_type, status, idempotency_key, identity_id, asset_id)
			  VALUES ($1, 'document', 'succeeded', $2, $3, $4)`, []any{jobID, "retire-" + jobID, localID, assetID}},
			{`INSERT INTO aura.ingestion_events (entity_type, entity_id, job_id, event_type, identity_id)
			  VALUES ('job', $1, $1, 'created', $2)`, []any{jobID, localID}},
			{`INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id) VALUES ($1, $2)`, []any{telegramUserID, localID}},
			{`INSERT INTO aura.identity_auth_links (identity_id, authula_user_id) VALUES ($1, $2)`, []any{localID, authulaUserID}},
		} {
			if _, err := tx.Exec(ctx, seed.sql, seed.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed local's rows: %v", err)
	}

	if err := retireLegacyLocalIdentity(ctx, pool, target, func(context.Context, string) error { return nil }); err != nil {
		t.Fatalf("retire local: %v", err)
	}

	owners := map[string]string{}
	var deletedKey string
	if err := db.WithIdentityTxRaw(ctx, pool, target, func(tx pgx.Tx) error {
		for name, q := range map[string]struct {
			sql string
			arg any
		}{
			"conversation":    {`SELECT identity_id::text FROM aura.conversations WHERE id = $1`, conversationID},
			"asset":           {`SELECT identity_id::text FROM aura.assets WHERE id = $1`, assetID},
			"ingestion job":   {`SELECT identity_id::text FROM aura.ingestion_jobs WHERE id = $1`, jobID},
			"ingestion event": {`SELECT identity_id::text FROM aura.ingestion_events WHERE job_id = $1`, jobID},
			"telegram link":   {`SELECT identity_id::text FROM aura.telegram_accounts WHERE telegram_user_id = $1`, telegramUserID},
			"auth link":       {`SELECT identity_id::text FROM aura.identity_auth_links WHERE authula_user_id = $1`, authulaUserID},
		} {
			var owner string
			if err := tx.QueryRow(ctx, q.sql, q.arg).Scan(&owner); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			owners[name] = owner
		}
		return tx.QueryRow(ctx, `SELECT object_key FROM aura.assets WHERE id = $1`, deletedAssetID).Scan(&deletedKey)
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("read the target's rows: %v", err)
	}
	for name, owner := range owners {
		if owner != target {
			t.Errorf("%s belongs to %q after the retirement, want the target %s", name, owner, target)
		}
	}
	if strings.Contains(deletedKey, localID) || !strings.Contains(deletedKey, target) {
		t.Errorf("deleted asset key = %q, want local's id replaced by the target's", deletedKey)
	}
}

// legacyLocalIdentity makes sure local exists as migration 0004 seeds it. A retirement deletes
// it, and later tests on the same database seed rows under it or check its grants, so the
// cleanup puts it back with the grants it had. Since 0121 those are exact capability names:
// restoring the retired `*` left local with no capability that HasCapability matches.
func legacyLocalIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := t.Context()
	var localID string
	if err := pool.QueryRow(ctx, `INSERT INTO aura.identities (id,name,kind) VALUES ('00000000-0000-0000-0000-000000000001','local','system') ON CONFLICT (name) DO UPDATE SET kind='system' RETURNING id::text`).Scan(&localID); err != nil {
		t.Fatal(err)
	}
	var grants []string
	if err := db.WithIdentityTxRaw(ctx, pool, localID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT capability FROM aura.capability_grants WHERE identity_id = $1::uuid`, localID)
		if err != nil {
			return err
		}
		grants, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatalf("read the local identity's grants: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id,name,kind) VALUES ($1,'local','system') ON CONFLICT DO NOTHING`, localID); err != nil {
			t.Errorf("restore the local identity: %v", err)
			return
		}
		if err := db.WithIdentityTxRaw(ctx, pool, localID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO aura.capability_grants (identity_id,capability) SELECT $1::uuid, unnest($2::text[]) ON CONFLICT DO NOTHING`, localID, grants)
			return err
		}); err != nil {
			t.Errorf("restore the local identity's grants: %v", err)
		}
	})
	return localID
}

func retirementTarget(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	target := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `INSERT INTO aura.identities (id,name,kind) VALUES ($1,$2,'user')`, target, "retire-"+target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, target) })
	return target
}
