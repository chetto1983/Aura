//go:build db_integration

// Live proofs for the catalog half of amendment #214: a scoped Writer records ownership in
// the SAME transaction as the audit row, a delete collects it, and the always-apply column
// tracks what the frontmatter says.
//
// Requires a Postgres with the migrations applied through 0118 and AURA_DB_URL set (the
// aura_app DSN). No-skip-as-green: catalogEnvOrSkip t.Fatals under $CI when the DSN is unset.
//
//	go test -tags db_integration -race -run TestCatalog ./internal/skills -count=1
package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
)

func catalogEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("skills catalog integration requires %s under CI — a skipped tier is never a silent pass", key)
		}
		t.Skipf("skills catalog integration requires %s", key)
	}
	return value
}

func catalogPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: catalogEnvOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func catalogIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, "skills-catalog-"+id); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

// catalogWriter builds a live, layout-carrying Writer over a temp root and the real pool.
func catalogWriter(t *testing.T, pool *pgxpool.Pool) *Writer {
	t.Helper()
	root := t.TempDir()
	layout := Layout{
		Global:     filepath.Join(root, "active"),
		Identities: filepath.Join(root, "identities"),
		Export:     filepath.Join(root, "export"),
	}
	return NewWriter(WriterConfig{
		Pool:         pool,
		ActiveDir:    layout.Global,
		ExportDir:    layout.Export,
		ArchiveDir:   filepath.Join(layout.Global, StageArchived),
		Layout:       layout,
		BodyCapBytes: 32768,
	})
}

// TestCatalogWriteRecordsOwnership walks the write path a person's cockpit install takes: the
// skill lands under their root AND the catalog learns they own it, in one transaction.
func TestCatalogWriteRecordsOwnership(t *testing.T) {
	pool := catalogPool(t)
	ctx := context.Background()
	owner := catalogIdentity(t, pool)

	scoped, err := catalogWriter(t, pool).For(owner)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if _, err := scoped.WriteMutationByName(ctx, "create", "catalog-owned", "a personal skill", "body",
		true, AuditActor{ActorID: "cli", IdentityID: owner}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(scoped.ActiveDir(), "catalog-owned", "SKILL.md")); err != nil {
		t.Fatalf("the skill did not land under the owner's root: %v", err)
	}

	store := NewCatalogStore(pool)
	rows, err := store.ListOwned(ctx, owner)
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "catalog-owned" {
		t.Fatalf("ListOwned = %+v, want the one skill just written", rows)
	}
	if !rows[0].AlwaysApply || rows[0].Description != "a personal skill" || rows[0].ContentHash == "" {
		t.Fatalf("the catalog row does not describe what was written: %+v", rows[0])
	}

	// The always-apply lookup is the read the always-on block runs on every turn.
	always, err := store.ListAlwaysApply(ctx, owner)
	if err != nil {
		t.Fatalf("ListAlwaysApply: %v", err)
	}
	if len(always) != 1 || always[0].ID != rows[0].ID {
		t.Fatalf("ListAlwaysApply = %+v, want the always:true skill", always)
	}

	// SetAlways flips the column the index is built on, so the always-on lookup stops
	// returning a skill the frontmatter no longer marks.
	if err := scoped.SetAlways(ctx, "catalog-owned", false, AuditActor{ActorID: "cli", IdentityID: owner}); err != nil {
		t.Fatalf("SetAlways: %v", err)
	}
	if always, err = store.ListAlwaysApply(ctx, owner); err != nil || len(always) != 0 {
		t.Fatalf("after always=off: (%+v, %v), want no rows", always, err)
	}

	// ResolveID is the name→id step the share CLI runs.
	id, err := store.ResolveID(ctx, owner, "catalog-owned")
	if err != nil || id != rows[0].ID {
		t.Fatalf("ResolveID = (%q, %v), want %q", id, err, rows[0].ID)
	}
}

// TestCatalogDeleteCollectsTheRow proves delete is the verb that takes ownership with it, and
// that a name nobody owns resolves to the sentinel rather than to another person's skill.
func TestCatalogDeleteCollectsTheRow(t *testing.T) {
	pool := catalogPool(t)
	ctx := context.Background()
	owner := catalogIdentity(t, pool)
	scoped, err := catalogWriter(t, pool).For(owner)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	actor := AuditActor{ActorID: "cli", IdentityID: owner}
	if _, err := scoped.WriteMutationByName(ctx, "create", "catalog-doomed", "d", "body", false, actor); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := scoped.Delete(ctx, "catalog-doomed", actor); err != nil {
		t.Fatalf("delete: %v", err)
	}

	store := NewCatalogStore(pool)
	rows, err := store.ListOwned(ctx, owner)
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the catalog still owns a deleted skill: %+v", rows)
	}
	if _, err := store.ResolveID(ctx, owner, "catalog-doomed"); err == nil {
		t.Fatal("ResolveID answered for a deleted skill")
	}
}

// TestCatalogIsInvisibleToAnotherIdentity is the read-side boundary at the database: B asking
// for their own catalog never sees A's row, and B asking for A's row by id — the shape a
// forged share would take — gets nothing back, because RLS admits a foreign row only with a
// grant.
func TestCatalogIsInvisibleToAnotherIdentity(t *testing.T) {
	pool := catalogPool(t)
	ctx := context.Background()
	alice := catalogIdentity(t, pool)
	bob := catalogIdentity(t, pool)

	scoped, err := catalogWriter(t, pool).For(alice)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if _, err := scoped.WriteMutationByName(ctx, "create", "catalog-private", "d", "body", false,
		AuditActor{ActorID: "cli", IdentityID: alice}); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := NewCatalogStore(pool)
	mine, err := store.ListOwned(ctx, alice)
	if err != nil || len(mine) != 1 {
		t.Fatalf("alice's own listing = (%+v, %v), want one row", mine, err)
	}
	theirs, err := store.ListOwned(ctx, bob)
	if err != nil {
		t.Fatalf("bob's listing: %v", err)
	}
	if len(theirs) != 0 {
		t.Fatalf("bob's listing contains %+v — RLS is not scoping the catalog", theirs)
	}
	byID, err := store.ListByIDs(ctx, bob, []string{mine[0].ID})
	if err != nil {
		t.Fatalf("bob reading alice's id: %v", err)
	}
	if len(byID) != 0 {
		t.Fatalf("bob resolved alice's ungranted skill by id: %+v", byID)
	}
}
