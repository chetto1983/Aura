package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// catalog_store_offline_test.go covers the half of the catalog a nil-pool test cannot reach:
// what happens AFTER validation passes and a transaction is attempted. It needs no database —
// the pool points at a closed port, the same unreachable-port instrument
// internal/mcpregistry/store_test.go uses — so every call fails at Begin.
//
// The contract it pins is the one an operator meets when Postgres is down: nothing panics,
// nothing invents a row, and every error names the read or write it came from instead of
// surfacing a bare dial failure. The round trip itself is the db_integration tier's job.

// offlinePool is a pool that can never connect. pgxpool.New does not dial, so the failure
// lands where the code under test puts it rather than in the constructor.
func offlinePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/nowhere?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCatalogReadsSurfaceAnUnreachableDatabase(t *testing.T) {
	store := NewCatalogStore(offlinePool(t))
	if store == nil {
		t.Fatal("NewCatalogStore over a real pool returned nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name string
		want string
		call func() error
	}{
		{"list owned", "skill catalog list", func() error {
			rows, err := store.ListOwned(ctx, catalogTestOwner)
			if len(rows) != 0 {
				t.Errorf("rows = %+v, want none when the read failed", rows)
			}
			return err
		}},
		{"always apply", "skill catalog always-apply", func() error {
			rows, err := store.ListAlwaysApply(ctx, catalogTestOwner)
			if len(rows) != 0 {
				t.Errorf("rows = %+v, want none when the read failed", rows)
			}
			return err
		}},
		{"by ids", "skill catalog by ids", func() error {
			rows, err := store.ListByIDs(ctx, catalogTestOwner, []string{catalogTestOwner})
			if len(rows) != 0 {
				t.Errorf("rows = %+v, want none when the read failed", rows)
			}
			return err
		}},
		{"resolve id", "skill catalog list", func() error {
			id, err := store.ResolveID(ctx, catalogTestOwner, "calc")
			if id != "" {
				t.Errorf("id = %q, want empty when the read failed", id)
			}
			// A failed read is NOT "you own no such skill": the CLI prints two different
			// sentences for those and must not report an outage as a typo.
			if errors.Is(err, ErrCatalogUnknownSkill) {
				t.Error("an unreachable database was reported as an unknown skill")
			}
			return err
		}},
	} {
		err := tc.call()
		if err == nil {
			t.Errorf("%s: succeeded against an unreachable database", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not name the operation (%q)", tc.name, err, tc.want)
		}
	}
}

// TestScopedWriteReachesTheLedgerAndFailsThere covers commitLedger's identity-scoped branch:
// a scoped Writer commits the audit row and the catalog row in ONE identity transaction, so a
// database that cannot be reached must fail the write rather than leave a skill visible on
// disk with no row behind it.
func TestScopedWriteReachesTheLedgerAndFailsThere(t *testing.T) {
	w, _ := newLayoutWriter(t)
	w.pool = offlinePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	fill := writeFilesInto(map[string][]byte{"SKILL.md": []byte("---\nname: ledger-skill\n---\nbody\n")})
	cat := scoped.catalogUpsertOp("ledger-skill", "d", false, "hash")
	if cat == nil {
		t.Fatal("a scoped writer produced no catalog operation")
	}
	err = scoped.writeActive(ctx, "ledger-skill", fill, AuditInsert{}, cat)
	if err == nil || !strings.Contains(err.Error(), "audit") {
		t.Fatalf("scoped write = %v, want a failure naming the audit transaction", err)
	}
	if scoped.ActiveExists("ledger-skill") {
		t.Fatal("the skill was promoted into the identity's root although the ledger write failed")
	}

	// The deployment Writer takes the OTHER branch of commitLedger (plain WithTx, no
	// app.current_identity) and must fail the same way — the pre-#214 statements, unchanged.
	if err := w.writeActive(ctx, "ledger-skill", fill, AuditInsert{}, nil); err == nil ||
		!strings.Contains(err.Error(), "audit") {
		t.Fatalf("deployment write = %v, want a failure naming the audit transaction", err)
	}
}

// TestCatalogRowsProjectionKeepsOrderAndEmptiness pins the list projection: an empty result is
// an empty slice rather than nil (the caller ranges over it either way, but a nil would render
// as "unknown" in a JSON body), and every row keeps its place.
func TestCatalogRowsProjectionKeepsOrderAndEmptiness(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse(catalogTestOwner)
	rows := catalogRowsFrom([]sqlc.AuraSkillCatalog{
		{ID: pgtype.UUID{Bytes: id, Valid: true}, Name: "alpha"},
		{ID: pgtype.UUID{Bytes: id, Valid: true}, Name: "beta", AlwaysApply: true},
	})
	if len(rows) != 2 || rows[0].Name != "alpha" || rows[1].Name != "beta" || !rows[1].AlwaysApply {
		t.Fatalf("catalogRowsFrom = %+v", rows)
	}
	if empty := catalogRowsFrom(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("catalogRowsFrom(nil) = %v, want an empty non-nil slice", empty)
	}
}

// TestCatalogOpsRunTheirStatementAndSurfaceTheFailure drives the two catalog operations the
// Writer folds into the audit transaction, through a real *sqlc.Queries over an unreachable
// pool. That is the only daemon-free way to execute the closures themselves: a nil Queries
// would panic instead of returning, and the closure is where the owner and the row shape are
// finally handed to Postgres.
func TestCatalogOpsRunTheirStatementAndSurfaceTheFailure(t *testing.T) {
	w, _ := newLayoutWriter(t)
	w.pool = offlinePool(t)
	q := sqlc.New(w.pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if err := scoped.catalogUpsertOp("calc", "adds", true, "hash")(ctx, q); err == nil ||
		!strings.Contains(err.Error(), "skill catalog upsert") {
		t.Errorf("upsert op = %v, want a failure naming the upsert", err)
	}
	if err := scoped.catalogDeleteOp("calc")(ctx, q); err == nil ||
		!strings.Contains(err.Error(), "skill catalog delete") {
		t.Errorf("delete op = %v, want a failure naming the delete", err)
	}

	// The owner is validated before the statement, so a row can never be attempted for an
	// identity aura.identities does not know.
	if _, err := UpsertCatalogTx(ctx, q, CatalogUpsert{OwnerID: "", Name: "calc"}); err == nil ||
		!strings.Contains(err.Error(), "owner identity") {
		t.Errorf("upsert with an empty owner = %v, want an owner-identity error", err)
	}
}

// TestInstallerForFollowsTheWriter proves the install transport lands a skill in the same root
// an authored one does: For derives from the Writer, so there is one answer to "whose library
// is this" rather than two that can disagree.
func TestInstallerForFollowsTheWriter(t *testing.T) {
	t.Parallel()
	w, layout := newLayoutWriter(t)
	installer := NewInstaller(InstallerConfig{Writer: w, BodyCapBytes: 32768})

	scoped, err := installer.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if scoped == installer {
		t.Fatal("For returned the deployment installer for a real identity — every install would land in the shared root")
	}
	if want := filepath.Join(layout.Identities, writerTestIdentity); scoped.writer.ActiveDir() != want {
		t.Errorf("scoped installer writes to %q, want %q", scoped.writer.ActiveDir(), want)
	}
	if installer.writer.ActiveDir() != layout.Global {
		t.Fatal("For mutated the receiver: an unscoped install would follow the last scoped one")
	}

	// The three no-op cases the Writer has, so an unscoped install and a deployment with
	// per-identity skills switched off run exactly the pre-#214 path.
	for _, identity := range []string{"", "   "} {
		got, ferr := installer.For(identity)
		if ferr != nil {
			t.Fatalf("For(%q): %v", identity, ferr)
		}
		if got != installer {
			t.Errorf("For(%q) derived an installer — an unscoped install must write the deployment library", identity)
		}
	}
	if _, ferr := installer.For("../escape"); ferr == nil {
		t.Error("For must refuse an identity that cannot name a directory")
	}
}

// TestLifecycleVerbsCarryTheCatalogToTheLedger drives the three verbs this slice gave a
// catalog leg — archive, restore and always — over a real skill tree and an unreachable pool.
// Each must do its filesystem work and then FAIL at the ledger, because the ledger and the
// catalog row are one transaction: a skill that changed on disk while its row did not is
// exactly the divergence the single transaction exists to prevent.
func TestLifecycleVerbsCarryTheCatalogToTheLedger(t *testing.T) {
	w, _ := newLayoutWriter(t)
	w.pool = offlinePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scoped, err := w.For(writerTestIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	seedOfflineSkill(t, scoped.ActiveDir(), "tuner")

	// always: the row has to learn the new value, so the write is the audit transaction and
	// an unreachable database fails it rather than leaving the flag changed with no row.
	if err := scoped.SetAlways(ctx, "tuner", true, AuditActor{}); err == nil ||
		!strings.Contains(err.Error(), "audit") {
		t.Fatalf("SetAlways = %v, want a failure naming the audit transaction", err)
	}
	if err := scoped.Archive(ctx, "tuner", ApprovalCLI, AuditActor{}); err == nil ||
		!strings.Contains(err.Error(), "audit") {
		t.Fatalf("Archive = %v, want a failure naming the audit transaction", err)
	}
	// Archive moved the tree before the ledger refused it, so restore has something to
	// promote back — and fails at the same place, one verb later.
	if err := scoped.Restore(ctx, "tuner", ApprovalCLI, AuditActor{}); err == nil ||
		!strings.Contains(err.Error(), "audit") {
		t.Fatalf("Restore = %v, want a failure naming the audit transaction", err)
	}
	if !scoped.ActiveExists("tuner") {
		t.Fatal("restore left the identity's root without the skill it promoted")
	}
}

// seedOfflineSkill writes a minimal valid active skill under root. It is deliberately not
// the db_integration tier's seedActiveSkill: that one is behind a build tag this file is
// not, and it roots at <root>/active rather than at the root itself.
func seedOfflineSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	md := "---\nname: " + name + "\ndescription: " + name + " description\ntype: instruction\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}
