//go:build db_integration

// Integration tests for internal/skills audit store (migration 0010 + AuditStore).
// Requires a running Postgres with the migrations applied through 0010:
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/skills -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset, so a
// skipped tier can never pass as green in the pipeline. The package goleak gate
// (main_test.go TestMain) fails the package on any leaked pgx pool goroutine.
package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// envOrSkip mirrors internal/identity: skip locally, fail-loud under CI so a
// missing DSN never reports a falsely-green integration job.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapURL composes the superuser DSN EnsureRoles needs from the password.
func bootstrapURL(t *testing.T, pwd string) string {
	t.Helper()
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
}

// migratedPool ensures roles + migrations (through 0010) are applied, then returns
// an aura_app pool ready for AuditStore use. Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	if err := db.EnsureRoles(ctx, bootstrapURL(t, pwd), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestInstallAuditRow proves SC#1: a coherent INSERT round-trips (the pending
// matrix row: NULL approval_source / NULL token / gate_recommended / !gate_taken)
// and reads back with its content_hash and tuple intact.
func TestInstallAuditRow(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	ctx := context.Background()

	name := "audit-rt-" + uuid.Must(uuid.NewV7()).String()[:8]
	got, err := store.InsertAudit(ctx, AuditInsert{
		ActorID:         "model",
		SkillName:       name,
		Action:          AuditCreate,
		ContentHash:     "sha256:deadbeef",
		GateRecommended: true,
		GateTaken:       false,
	})
	if err != nil {
		t.Fatalf("InsertAudit (pending row): %v", err)
	}
	if got.ID == "" || got.CreatedAt.IsZero() {
		t.Fatalf("InsertAudit: want id+created_at populated, got %+v", got)
	}
	if got.IdentityID != "local" {
		t.Errorf("identity_id default: want local, got %q", got.IdentityID)
	}
	if got.ApprovalSource != ApprovalNone || got.PausedStateToken != "" {
		t.Errorf("pending tuple: want NULL source+token, got %q / %q", got.ApprovalSource, got.PausedStateToken)
	}
	if got.ContentHash != "sha256:deadbeef" {
		t.Errorf("content_hash round-trip: want sha256:deadbeef, got %q", got.ContentHash)
	}

	rows, err := store.List(ctx, AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != got.ID {
		t.Fatalf("List by name: want 1 row matching insert, got %d", len(rows))
	}
}

// TestAuditCoherence proves the D-29 CHECK still admits EXACTLY its four coherent tuples
// and rejects an incoherent one, even though Aura now writes only two of the four.
//
// The 'ask_user' arms are the reason this test does not shrink with the code: those rows
// exist in every pre-#97 deployment's ledger, the table is append-only, so the constraint
// must keep accepting them. Since AuditInsert no longer carries a pause token (#97), the
// coherent ask_user row is inserted with a raw Exec — 0018 dropped the token's FK, so any
// UUID satisfies the CHECK — while the INCOHERENT arm still goes through the store,
// which is the path that must reject it.
func TestAuditCoherence(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	ctx := context.Background()
	base := "coh-" + uuid.Must(uuid.NewV7()).String()[:8]

	// Incoherent: ask_user approval with a NULL paused_state_token — D-29 forbids it.
	_, err := store.InsertAudit(ctx, AuditInsert{
		ActorID: "model", SkillName: base, Action: AuditActivate,
		ContentHash: "h", ApprovalSource: ApprovalSource("ask_user"),
		GateRecommended: true, GateTaken: true, // token intentionally absent
	})
	if !errors.Is(err, ErrAuditIncoherent) {
		t.Fatalf("incoherent ask_user+NULL-token: want ErrAuditIncoherent, got %v", err)
	}

	// The three shapes the store can still express.
	coherent := []AuditInsert{
		{ActorID: "model", SkillName: base, Action: AuditCreate, ContentHash: "h", GateRecommended: true, GateTaken: false},
		{ActorID: "cli", SkillName: base, Action: AuditActivate, ContentHash: "h", ApprovalSource: ApprovalCLI, GateRecommended: true, GateTaken: true},
		{ActorID: "system", SkillName: base, Action: AuditAutoArchive, ContentHash: "h", ApprovalSource: ApprovalAuto, GateRecommended: false, GateTaken: true},
	}
	for i, in := range coherent {
		if _, err := store.InsertAudit(ctx, in); err != nil {
			t.Fatalf("coherent row %d (%s): %v", i, in.ApprovalSource, err)
		}
	}

	// The fourth: a historical ask_user row with its token, written raw.
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.skill_audit
		   (actor_id, identity_id, skill_name, action, content_hash,
		    approval_source, paused_state_token, gate_recommended, gate_taken)
		 VALUES ('user', 'local', $1, 'activate', 'h', 'ask_user', $2, true, true)`,
		base, uuid.Must(uuid.NewV7()).String(),
	); err != nil {
		t.Fatalf("historical ask_user row must still satisfy the coherence CHECK: %v", err)
	}
}

// TestAuditImmutable proves SC#2: aura_app cannot UPDATE, DELETE, or TRUNCATE the
// audit table. The triggers raise (42501) AND aura_app lacks the grant — either
// path classifies as ErrAuditImmutable / a privilege error.
func TestAuditImmutable(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	ctx := context.Background()

	name := "immut-" + uuid.Must(uuid.NewV7()).String()[:8]
	row, err := store.InsertAudit(ctx, AuditInsert{
		ActorID: "model", SkillName: name, Action: AuditCreate,
		ContentHash: "h", GateRecommended: true, GateTaken: false,
	})
	if err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	if _, err := pool.Exec(ctx, "UPDATE aura.skill_audit SET content_hash = 'tampered' WHERE id = $1", row.ID); err == nil {
		t.Error("UPDATE as aura_app: want denied (trigger or role), got nil error")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.skill_audit WHERE id = $1", row.ID); err == nil {
		t.Error("DELETE as aura_app: want denied (trigger or role), got nil error")
	}
	if _, err := pool.Exec(ctx, "TRUNCATE aura.skill_audit"); err == nil {
		t.Error("TRUNCATE as aura_app: want denied (statement trigger or role), got nil error")
	}

	// The row survived every attempt.
	rows, err := store.List(ctx, AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List after mutation attempts: %v", err)
	}
	if len(rows) != 1 || rows[0].ContentHash != "h" {
		t.Fatalf("audit row tampered or removed: want 1 row with content_hash 'h', got %d", len(rows))
	}
}

// TestMigration0010_SchemaRoundTrip migrates back to the 0009 floor, then up to
// head again and asserts the table is gone after down and present after re-up,
// proving the migration is reversible+idempotent even as later migrations are
// added above 0010.
func TestMigration0010_SchemaRoundTrip(t *testing.T) {
	pool := migratedPool(t) // ensures roles + migrations to head first
	ctx := context.Background()

	// Down to 0009: the audit table must disappear and the kind CHECK must no
	// longer admit skill_ttl_sweep. The step count is computed from the current
	// head so this test does not rot when 0011+ migrations are added.
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	if err := migrateToVersion(t, ctx, migrateURL, 9); err != nil {
		t.Fatalf("migrate down to 0009: %v", err)
	}
	if tableExists(t, pool, "skill_audit") {
		t.Error("after down to 0009: aura.skill_audit still exists")
	}

	// Up to head: the table returns.
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !tableExists(t, pool, "skill_audit") {
		t.Error("after re-up: aura.skill_audit missing")
	}
}

func migrateToVersion(t *testing.T, ctx context.Context, migrateURL string, target int64) error {
	t.Helper()
	migratePool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		return fmt.Errorf("open migrate pool: %w", err)
	}
	defer migratePool.Close()

	rows, err := db.Status(ctx, migratePool)
	if err != nil {
		return fmt.Errorf("migration status: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("migration status: no applied migrations")
	}
	current := rows[len(rows)-1].Version
	// `target - current` counts VERSIONS; MigrateSteps counts MIGRATIONS, and the
	// embedded sequence has gaps since the adaptive plane's twenty-six migrations were
	// removed. The difference showed up as golang-migrate's opaque "limit N short".
	steps, err := db.MigrationStepDelta(current, target)
	if err != nil {
		return fmt.Errorf("migration step delta %d -> %d: %w", current, target, err)
	}
	if steps == 0 {
		return nil
	}
	return db.MigrateSteps(ctx, migrateURL, steps)
}

// TestWriterActiveAuditRow proves WriteMutation lands the skill where it takes effect —
// the active root AND the export dir the sandbox mounts — and records EXACTLY ONE audit
// row carrying the actor tuple ('cli' source, NULL token, gate_recommended=true,
// gate_taken=true) inside db.WithTx, with the content_hash recorded (D-23).
//
// gate_recommended stays true although nothing is gated: the 0010 coherence CHECK admits
// no ('cli',NULL,false,true) tuple, so this assertion is what stops a well-meaning
// "cleanup" from turning that constant into a runtime 23514 in production.
func TestWriterActiveAuditRow(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         pool,
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})

	name := "wr-" + uuid.Must(uuid.NewV7()).String()[:8]
	fm := Frontmatter{Name: name, Description: "a write-boundary skill", Type: TypeInstruction}
	status, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "Do the thing.", AuditActor{ActorID: "cli"})
	if err != nil {
		t.Fatalf("WriteMutation: %v", err)
	}
	if status != StatusActive {
		t.Errorf("status: want %s, got %s", StatusActive, status)
	}
	for _, dir := range []string{"active", "export"} {
		if _, serr := os.Stat(filepath.Join(root, dir, name, "SKILL.md")); serr != nil {
			t.Errorf("%s SKILL.md not written: %v", dir, serr)
		}
	}
	// The staging dir is swept on success — a committed write leaves no residue behind.
	if entries, _ := os.ReadDir(filepath.Join(root, "active", stagingDirName)); len(entries) != 0 {
		t.Errorf("staging dir must be empty after a committed write, got %v", entries)
	}

	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(rows))
	}
	r := rows[0]
	if r.ApprovalSource != ApprovalCLI || r.PausedStateToken != "" || !r.GateRecommended || !r.GateTaken {
		t.Errorf("actor tuple mismatch: src=%q token=%q gateRec=%v gateTaken=%v",
			r.ApprovalSource, r.PausedStateToken, r.GateRecommended, r.GateTaken)
	}
	if r.Action != AuditCreate || r.ContentHash == "" {
		t.Errorf("audit row: want action=create + non-empty content_hash, got action=%q hash=%q", r.Action, r.ContentHash)
	}
}

// tableExists reports whether aura.<name> exists.
func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='aura' AND table_name=$1)",
		name,
	).Scan(&exists); err != nil {
		t.Fatalf("tableExists %q: %v", name, err)
	}
	return exists
}
