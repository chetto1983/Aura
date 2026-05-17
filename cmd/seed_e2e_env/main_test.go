package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/api/auth"
)

func TestResolveDefaultSeedUserSkipsE2EBootstrap(t *testing.T) {
	db := newSeedTestDB(t)
	ctx := context.Background()
	mustExecSeedTest(t, db, `
INSERT INTO allowed_users (user_id, source, created_at) VALUES
  ('1000001', ?, '2026-05-17T08:00:00Z'),
  ('owner', ?, '2026-05-17T09:00:00Z')
`, auth.SourceE2EBootstrap, auth.SourceTelegramBootstrap)

	userID, source, err := resolveDefaultSeedUser(ctx, db)
	if err != nil {
		t.Fatalf("resolveDefaultSeedUser: %v", err)
	}
	if userID != "owner" || source != auth.SourceTelegramBootstrap {
		t.Fatalf("resolved (%q, %q), want owner telegram bootstrap", userID, source)
	}
}

func TestResolveDefaultSeedUserReturnsNoRowsWhenOnlyE2EBootstrapExists(t *testing.T) {
	db := newSeedTestDB(t)
	ctx := context.Background()
	mustExecSeedTest(t, db, `
INSERT INTO allowed_users (user_id, source, created_at) VALUES
  ('1000001', ?, '2026-05-17T08:00:00Z')
`, auth.SourceE2EBootstrap)

	_, _, err := resolveDefaultSeedUser(ctx, db)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("resolveDefaultSeedUser err = %v, want sql.ErrNoRows", err)
	}
}

func TestLookupAllowedUserSource(t *testing.T) {
	db := newSeedTestDB(t)
	ctx := context.Background()
	mustExecSeedTest(t, db, `
INSERT INTO allowed_users (user_id, source, created_at) VALUES
  ('approved', ?, '2026-05-17T08:00:00Z')
`, auth.SourceDashboardApprove)

	source, err := lookupAllowedUserSource(ctx, db, "approved")
	if err != nil {
		t.Fatalf("lookupAllowedUserSource: %v", err)
	}
	if source != auth.SourceDashboardApprove {
		t.Fatalf("source = %q, want %q", source, auth.SourceDashboardApprove)
	}
}

func TestPrintEnvShell(t *testing.T) {
	var out bytes.Buffer
	if err := printEnv(&out, "sh", "tok_123", "456"); err != nil {
		t.Fatalf("printEnv: %v", err)
	}
	want := "export AURA_E2E_TOKEN=tok_123\nexport AURA_E2E_CHAT_ID=456\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

func TestPrintEnvPowerShellEscapesSingleQuotes(t *testing.T) {
	var out bytes.Buffer
	if err := printEnv(&out, "powershell", "tok'en", "chat'id"); err != nil {
		t.Fatalf("printEnv: %v", err)
	}
	want := "$env:AURA_E2E_TOKEN = 'tok''en'\n$env:AURA_E2E_CHAT_ID = 'chat''id'\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

func TestPrintEnvRejectsUnknownShell(t *testing.T) {
	var out bytes.Buffer
	err := printEnv(&out, "fish", "tok", "chat")
	if err == nil {
		t.Fatal("printEnv err = nil, want unsupported shell error")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("err = %v, want unsupported shell", err)
	}
}

func newSeedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mustExecSeedTest(t, db, `
CREATE TABLE allowed_users (
  user_id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  created_at TEXT NOT NULL
)
`)
	return db
}

func mustExecSeedTest(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec seed fixture: %v", err)
	}
}
