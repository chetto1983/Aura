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
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
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

// TestAuditCoherence proves the D-29 CHECK rejects an incoherent tuple (approved
// with no token: ask_user + NULL token must violate the constraint) while the four
// coherent shapes all insert.
func TestAuditCoherence(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	ctx := context.Background()
	base := "coh-" + uuid.Must(uuid.NewV7()).String()[:8]

	token := uuid.Must(uuid.NewV7()).String()
	seedPausedState(t, pool, token)

	// Incoherent: ask_user approval with a NULL paused_state_token — D-29 forbids it.
	_, err := store.InsertAudit(ctx, AuditInsert{
		ActorID: "model", SkillName: base, Action: AuditActivate,
		ContentHash: "h", ApprovalSource: ApprovalAskUser,
		GateRecommended: true, GateTaken: true, // token intentionally empty
	})
	if !errors.Is(err, ErrAuditIncoherent) {
		t.Fatalf("incoherent ask_user+NULL-token: want ErrAuditIncoherent, got %v", err)
	}

	// The four coherent shapes.
	coherent := []AuditInsert{
		{ActorID: "model", SkillName: base, Action: AuditCreate, ContentHash: "h", GateRecommended: true, GateTaken: false},
		{ActorID: "user", SkillName: base, Action: AuditActivate, ContentHash: "h", ApprovalSource: ApprovalAskUser, PausedStateToken: token, GateRecommended: true, GateTaken: true},
		{ActorID: "cli", SkillName: base, Action: AuditActivate, ContentHash: "h", ApprovalSource: ApprovalCLI, GateRecommended: true, GateTaken: true},
		{ActorID: "system", SkillName: base, Action: AuditAutoArchive, ContentHash: "h", ApprovalSource: ApprovalAuto, GateRecommended: false, GateTaken: true},
	}
	for i, in := range coherent {
		if _, err := store.InsertAudit(ctx, in); err != nil {
			t.Fatalf("coherent row %d (%s): %v", i, in.ApprovalSource, err)
		}
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

// TestMigration0010_SchemaRoundTrip applies the 0010 down then up and asserts the
// table is gone after down and present after re-up, proving the migration is
// reversible+idempotent. It uses a migrate instance steered directly so it can
// step down one and back up.
func TestMigration0010_SchemaRoundTrip(t *testing.T) {
	pool := migratedPool(t) // ensures roles + migrations to head first
	ctx := context.Background()

	// Down one (0010 → 0009): the audit table must disappear and the kind CHECK
	// must no longer admit skill_ttl_sweep.
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := db.MigrateSteps(ctx, migrateURL, -2); err != nil {
		t.Fatalf("migrate down -2: %v", err)
	}
	if tableExists(t, pool, "skill_audit") {
		t.Error("after down -2: aura.skill_audit still exists")
	}

	// Up one (0009 → 0010): the table returns.
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !tableExists(t, pool, "skill_audit") {
		t.Error("after re-up: aura.skill_audit missing")
	}
}

// TestWriterPendingAuditRow proves the writer's WriteMutation lands a pending skill
// on disk AND records the D-29 pending tuple (NULL approval_source, NULL token,
// gate_recommended=true, gate_taken=false) inside db.WithTx — the (NULL,NULL,
// true,false) matrix row — with the content_hash recorded (D-23).
func TestWriterPendingAuditRow(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         pool,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})

	name := "wr-" + uuid.Must(uuid.NewV7()).String()[:8]
	fm := Frontmatter{Name: name, Description: "a write-boundary skill", Type: TypeInstruction}
	status, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "Do the thing.", AuditActor{ActorID: "model"})
	if err != nil {
		t.Fatalf("WriteMutation: %v", err)
	}
	if status != StatusPendingApproval {
		t.Errorf("status: want %s, got %s", StatusPendingApproval, status)
	}
	if _, serr := os.Stat(filepath.Join(root, "pending", name, "SKILL.md")); serr != nil {
		t.Errorf("pending SKILL.md not written: %v", serr)
	}

	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 audit row, got %d", len(rows))
	}
	r := rows[0]
	if r.ApprovalSource != ApprovalNone || r.PausedStateToken != "" || !r.GateRecommended || r.GateTaken {
		t.Errorf("D-29 pending tuple mismatch: src=%q token=%q gateRec=%v gateTaken=%v",
			r.ApprovalSource, r.PausedStateToken, r.GateRecommended, r.GateTaken)
	}
	if r.Action != AuditCreate || r.ContentHash == "" {
		t.Errorf("audit row: want action=create + non-empty content_hash, got action=%q hash=%q", r.Action, r.ContentHash)
	}
}

// TestWriterActivateAuditRow proves Activate moves pending→active, materializes into
// the export dir, and records the D-29 ask_user approved tuple (token NOT NULL).
func TestWriterActivateAuditRow(t *testing.T) {
	pool := migratedPool(t)
	store := NewAuditStore(pool)
	root := t.TempDir()
	exportDir := filepath.Join(root, "export")
	w := NewWriter(WriterConfig{
		Pool:       pool,
		PendingDir: filepath.Join(root, "pending"),
		ActiveDir:  filepath.Join(root, "active"),
		ExportDir:  exportDir,
		ArchiveDir: filepath.Join(root, "archived"),
		Blocklist:  []string{"<|im_start|>"},
	})

	name := "act-" + uuid.Must(uuid.NewV7()).String()[:8]
	fm := Frontmatter{Name: name, Description: "d", Type: TypeInstruction}
	if _, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "body", AuditActor{ActorID: "model"}); err != nil {
		t.Fatalf("WriteMutation: %v", err)
	}

	token := uuid.Must(uuid.NewV7()).String()
	seedPausedState(t, pool, token)
	if err := w.Activate(t.Context(), name, ApprovalAskUser, token, AuditActor{ActorID: "user"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// Materialized into the export dir (the /skills mount source).
	if _, serr := os.Stat(filepath.Join(exportDir, name, "SKILL.md")); serr != nil {
		t.Errorf("active skill not materialized into export dir: %v", serr)
	}
	// The activate audit row carries the approved tuple.
	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var sawActivate bool
	for _, r := range rows {
		if r.Action == AuditActivate {
			sawActivate = true
			if r.ApprovalSource != ApprovalAskUser || r.PausedStateToken != token || !r.GateTaken {
				t.Errorf("activate tuple: src=%q token=%q gateTaken=%v", r.ApprovalSource, r.PausedStateToken, r.GateTaken)
			}
		}
	}
	if !sawActivate {
		t.Error("no activate audit row recorded")
	}
}

// seedPausedState inserts a minimal paused_states row so an ask_user audit row's
// FK (paused_state_token) resolves. Cleaned up via t.Cleanup (cascade-free; the
// audit rows referencing it are append-only, so leave both — a fresh token per run
// keeps the suite isolated).
func seedPausedState(t *testing.T, pool *pgxpool.Pool, token string) {
	t.Helper()
	const seededLocalConv = "00000000-0000-0000-0000-000000000001"
	convID := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'test-model', 'active')",
		convID, seededLocalConv,
	); err != nil {
		t.Fatalf("seed conversation for paused_state: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.paused_states (token, conversation_id, kind, question, tool_call_id)
		 VALUES ($1, $2, 'approval', 'approve?', 'tc-1')`,
		token, convID,
	); err != nil {
		t.Fatalf("seed paused_state: %v", err)
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
