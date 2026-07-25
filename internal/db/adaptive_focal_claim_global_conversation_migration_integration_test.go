//go:build db_integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAdaptiveFocalClaimGlobalConversationMigrationLiveContract(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(
		ctx,
		bootstrapURL(t),
		envOrSkip(t, "POSTGRES_PASSWORD"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatal(err)
	}
	pool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	var unique bool
	var definition string
	err = pool.QueryRow(ctx, `SELECT i.indisunique, pg_get_indexdef(i.indexrelid)
		FROM pg_index AS i
		JOIN pg_class AS c ON c.oid=i.indexrelid
		JOIN pg_namespace AS n ON n.oid=c.relnamespace
		WHERE n.nspname='aura'
		  AND c.relname='adaptive_focal_cohort_claims_owner_conversation_uidx'`).Scan(
		&unique,
		&definition,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !unique ||
		!strings.Contains(
			definition,
			"(owner_id, evaluation_conversation_id)",
		) {
		t.Fatalf(
			"owner/conversation index unique=%t definition=%q",
			unique,
			definition,
		)
	}
}
