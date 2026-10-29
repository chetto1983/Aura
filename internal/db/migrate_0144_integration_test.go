//go:build db_integration

package db

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const retireLegacyLocalRows = "aura.retire_legacy_local_rows(uuid)"

// 0144 puts the Telegram links under row security, with the shape 0089 gave shared_links, and
// adds the definer function the legacy local retirement moves its rows through
// (docs/superpowers/specs/2026-10-27-telegram-account-links-design.md, D1 and D3).
func TestMigrate0144IsolatesTelegramLinks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, appURL := fresh0093Database(t, ctx, "aura_migrate0144_telegram")
	migrateToVersion(t, ctx, migrateURL, admin, 143)
	assert0144Absent(t, ctx, admin)

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0144 up: %v", err)
	}
	assert0144Schema(t, ctx, admin)

	app, err := Open(ctx, &Config{URL: appURL})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(app.Close)
	idA, idB := seed0144User(t, ctx, admin, "a"), seed0144User(t, ctx, admin, "b")
	const linkA, linkB = int64(9_000_060_001), int64(9_000_060_002)
	seed0144Link(t, ctx, admin, linkA, idA)
	seed0144Link(t, ctx, admin, linkB, idB)
	assert0144LinkIsolation(t, ctx, admin, app, idA, idB, linkA, linkB)
	assert0144Retirement(t, ctx, admin, app, idA, idB)

	// The cascade passes row security: aura_app with no identity cannot see B's link, and
	// deleting B still removes it (spec acceptance 4).
	if _, err := app.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, idB); err != nil {
		t.Fatalf("delete identity B as aura_app: %v", err)
	}
	if n := count0144Links(t, ctx, admin, `identity_id = $1`, idB); n != 0 {
		t.Fatalf("identity B's delete left %d links", n)
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0144 down: %v", err)
	}
	assert0144Absent(t, ctx, admin)
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0144 up again: %v", err)
	}
	assert0144Schema(t, ctx, admin)
}

func assert0144LinkIsolation(t *testing.T, ctx context.Context, admin, app *pgxpool.Pool, idA, idB string, linkA, linkB int64) {
	t.Helper()
	inApp0144Tx(t, ctx, app, idA, func(tx pgx.Tx) {
		if got := linkIDs0144(t, ctx, tx); !slices.Equal(got, []int64{linkA}) {
			t.Errorf("identity A sees links %v, want only its own %d", got, linkA)
		}
		if n := exec0144(t, ctx, tx, `DELETE FROM aura.telegram_accounts WHERE telegram_user_id = $1`, linkB); n != 0 {
			t.Errorf("identity A deleted %d of identity B's links", n)
		}
		if n := exec0144(t, ctx, tx, `UPDATE aura.telegram_accounts SET last_seen_at = now() WHERE telegram_user_id = $1`, linkA); n != 1 {
			t.Errorf("identity A touched %d of its own links, want 1", n)
		}
	})
	if n := count0144Links(t, ctx, admin, `telegram_user_id = $1`, linkB); n != 1 {
		t.Fatalf("identity B's link is gone after identity A's delete")
	}
	expect0144Refused(t, ctx, app, idA, idB)

	inApp0144Tx(t, ctx, app, "", func(tx pgx.Tx) {
		if got := linkIDs0144(t, ctx, tx); !slices.Equal(got, []int64{linkA, linkB}) {
			t.Errorf("a session with no identity sees links %v, want both %v", got, []int64{linkA, linkB})
		}
		if n := exec0144(t, ctx, tx, `UPDATE aura.telegram_accounts SET last_seen_at = now()`); n != 0 {
			t.Errorf("a session with no identity updated %d links, want 0", n)
		}
		if n := exec0144(t, ctx, tx, `DELETE FROM aura.telegram_accounts`); n != 0 {
			t.Errorf("a session with no identity deleted %d links, want 0", n)
		}
	})
	expect0144Refused(t, ctx, app, "", idA)
}

// expect0144Refused inserts a link for owner while bound to identity and wants the row
// security refusal: a failed WITH CHECK on an INSERT raises an error (PostgreSQL manual,
// CREATE POLICY), unlike an UPDATE or DELETE whose rows are filtered out.
func expect0144Refused(t *testing.T, ctx context.Context, app *pgxpool.Pool, identity, owner string) {
	t.Helper()
	inApp0144Tx(t, ctx, app, identity, func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, `INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id) VALUES (9000060999, $1)`, owner)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("a link for %s inserted while bound to %q: err = %v, want SQLSTATE 42501", owner, identity, err)
		}
	})
}

// The function moves local's rows, and only local's, and only to a user identity. It runs on
// the runtime pool with no identity bound, as the retirement does.
func assert0144Retirement(t *testing.T, ctx context.Context, admin, app *pgxpool.Pool, idA, idB string) {
	t.Helper()
	var localID, serviceID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities WHERE name = 'local' AND kind = 'system'`).Scan(&localID); err != nil {
		t.Fatalf("read the seeded local identity: %v", err)
	}
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities WHERE kind = 'service' LIMIT 1`).Scan(&serviceID); err != nil {
		t.Fatalf("read a seeded service identity: %v", err)
	}
	const linkLocal = int64(9_000_060_003)
	seed0144Link(t, ctx, admin, linkLocal, localID)
	var convLocal, convB string
	for owner, conv := range map[string]*string{localID: &convLocal, idB: &convB} {
		if err := admin.QueryRow(ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES (gen_random_uuid(), $1) RETURNING id::text`, owner).Scan(conv); err != nil {
			t.Fatalf("seed a conversation for %s: %v", owner, err)
		}
	}

	for name, target := range map[string]string{"a service identity": serviceID, "an unknown identity": uuid.NewString()} {
		_, err := app.Exec(ctx, `SELECT aura.retire_legacy_local_rows($1)`, target)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "22023" {
			t.Errorf("retirement onto %s: err = %v, want SQLSTATE 22023", name, err)
		}
	}
	if n := count0144Links(t, ctx, admin, `identity_id = $1`, localID); n != 1 {
		t.Fatalf("a refused retirement moved local's link: %d left", n)
	}

	if _, err := app.Exec(ctx, `SELECT aura.retire_legacy_local_rows($1)`, idA); err != nil {
		t.Fatalf("retire local onto A as aura_app: %v", err)
	}
	const linkOwner = `SELECT identity_id::text FROM aura.telegram_accounts WHERE telegram_user_id = $1`
	const conversationOwner = `SELECT identity_id::text FROM aura.conversations WHERE id = $1`
	for _, c := range []struct {
		name, sql string
		key       any
		owner     string
	}{
		{"local's link", linkOwner, linkLocal, idA},
		{"B's link", linkOwner, int64(9_000_060_002), idB},
		{"local's conversation", conversationOwner, convLocal, idA},
		{"B's conversation", conversationOwner, convB, idB},
	} {
		var owner string
		if err := admin.QueryRow(ctx, c.sql, c.key).Scan(&owner); err != nil || owner != c.owner {
			t.Errorf("%s belongs to %q (%v), want %s", c.name, owner, err, c.owner)
		}
	}

	if _, err := admin.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, localID); err != nil {
		t.Fatalf("delete local: %v", err)
	}
	if _, err := app.Exec(ctx, `SELECT aura.retire_legacy_local_rows($1)`, idA); err != nil {
		t.Fatalf("retirement with local already gone: %v", err)
	}
	if n := count0144Links(t, ctx, admin, `identity_id = $1`, idB); n != 1 {
		t.Fatalf("a retirement with no local moved B's link: %d left", n)
	}
}

func assert0144Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if !telegramLinksRowSecurity(t, ctx, admin) {
		t.Fatal("aura.telegram_accounts has no row-level security")
	}
	rows, err := admin.Query(ctx, `
SELECT policyname || ' ' || permissive || ' ' || array_to_string(roles, ',') || ' ' || cmd
FROM pg_policies WHERE schemaname = 'aura' AND tablename = 'telegram_accounts' ORDER BY policyname`)
	if err != nil {
		t.Fatalf("read the links' policies: %v", err)
	}
	policies, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scan the links' policies: %v", err)
	}
	want := []string{
		"telegram_accounts_owner_isolation PERMISSIVE public ALL",
		"telegram_accounts_owner_or_pre_identity RESTRICTIVE aura_app ALL",
		"telegram_accounts_pre_identity_read PERMISSIVE public SELECT",
	}
	if !slices.Equal(policies, want) {
		t.Fatalf("policies = %q, want %q", policies, want)
	}

	var definer, appCanRun, publicCanRun bool
	var config []string
	var owner string
	if err := admin.QueryRow(ctx, `
SELECT p.prosecdef, coalesce(p.proconfig, '{}'), r.rolname,
       has_function_privilege('aura_app', p.oid, 'EXECUTE'),
       has_function_privilege('public', p.oid, 'EXECUTE')
FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner
WHERE p.oid = to_regprocedure($1)`, retireLegacyLocalRows).Scan(&definer, &config, &owner, &appCanRun, &publicCanRun); err != nil {
		t.Fatalf("read %s: %v", retireLegacyLocalRows, err)
	}
	if !definer || owner != "aura_migrate" || !slices.Contains(config, "search_path=pg_catalog") {
		t.Fatalf("%s: definer=%t owner=%s config=%v, want a definer owned by aura_migrate with search_path=pg_catalog", retireLegacyLocalRows, definer, owner, config)
	}
	if !appCanRun || publicCanRun {
		t.Fatalf("%s: aura_app may run it=%t, PUBLIC may run it=%t; want only aura_app", retireLegacyLocalRows, appCanRun, publicCanRun)
	}
}

func assert0144Absent(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if telegramLinksRowSecurity(t, ctx, admin) {
		t.Fatal("aura.telegram_accounts has row-level security without 0144")
	}
	var policies int
	var function *string
	if err := admin.QueryRow(ctx, `
SELECT (SELECT count(*) FROM pg_policies WHERE schemaname = 'aura' AND tablename = 'telegram_accounts'),
       to_regprocedure($1)::text`, retireLegacyLocalRows).Scan(&policies, &function); err != nil {
		t.Fatalf("read the links' policies and function: %v", err)
	}
	if policies != 0 || function != nil {
		t.Fatalf("without 0144: %d policies on the links, function %v; want none", policies, function)
	}
}

func telegramLinksRowSecurity(t *testing.T, ctx context.Context, admin *pgxpool.Pool) bool {
	t.Helper()
	var on bool
	if err := admin.QueryRow(ctx, `SELECT relrowsecurity FROM pg_class WHERE oid = 'aura.telegram_accounts'::regclass`).Scan(&on); err != nil {
		t.Fatalf("read the links' row security: %v", err)
	}
	return on
}

func seed0144User(t *testing.T, ctx context.Context, admin *pgxpool.Pool, name string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := admin.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, "tg0144-"+name+"-"+id[:8]); err != nil {
		t.Fatalf("seed identity %s: %v", name, err)
	}
	return id
}

func seed0144Link(t *testing.T, ctx context.Context, admin *pgxpool.Pool, telegramUserID int64, identityID string) {
	t.Helper()
	if _, err := admin.Exec(ctx, `INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id) VALUES ($1, $2)`, telegramUserID, identityID); err != nil {
		t.Fatalf("seed link %d: %v", telegramUserID, err)
	}
}

func count0144Links(t *testing.T, ctx context.Context, admin *pgxpool.Pool, where string, arg any) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM aura.telegram_accounts WHERE `+where, arg).Scan(&n); err != nil {
		t.Fatalf("count links where %s: %v", where, err)
	}
	return n
}

// inApp0144Tx runs fn as aura_app, bound to identity when it is not empty, and rolls back.
func inApp0144Tx(t *testing.T, ctx context.Context, app *pgxpool.Pool, identity string, fn func(pgx.Tx)) {
	t.Helper()
	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if identity != "" {
		if err := SetTxIdentity(ctx, tx, identity); err != nil {
			t.Fatalf("bind %s: %v", identity, err)
		}
	}
	fn(tx)
}

func linkIDs0144(t *testing.T, ctx context.Context, tx pgx.Tx) []int64 {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT telegram_user_id FROM aura.telegram_accounts ORDER BY telegram_user_id`)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatalf("scan links: %v", err)
	}
	return ids
}

func exec0144(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) int64 {
	t.Helper()
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return tag.RowsAffected()
}
