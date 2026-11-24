//go:build db_integration

package pimsplit

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

// The tests share one database and Upgrade reads every row in it, so none runs in parallel.

var oldTool = mcptools.NamespacedName("calendar", "calendar")

func splitEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("pim split integration requires %s", key)
		}
		t.Skipf("pim split integration requires %s", key)
	}
	return value
}

type harness struct {
	t    *testing.T
	ctx  context.Context
	conn *pgx.Conn // the migrate role, as dbMigrate runs Upgrade
	app  *pgxpool.Pool
	logs *bytes.Buffer
	log  *slog.Logger
}

// newHarness migrates the database, settles whatever old-shape rows other tests left, and re-arms
// the upgrade, so the test's first Upgrade runs for real over the rows it seeds.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	migrateURL := dbtest.MigrateURL(t, splitEnv(t, "AURA_DB_MIGRATE_URL"))
	appURL := splitEnv(t, "AURA_DB_URL")
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	conn, err := pgx.Connect(ctx, migrateURL)
	if err != nil {
		t.Fatalf("connect as the migrate role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	app, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("open the app pool: %v", err)
	}
	t.Cleanup(app.Close)
	logs := &bytes.Buffer{}
	h := &harness{t: t, ctx: ctx, conn: conn, app: app, logs: logs, log: slog.New(slog.NewTextHandler(logs, nil))}
	h.rearm()
	if _, err := Upgrade(ctx, conn, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("settle the database: %v", err)
	}
	h.rearm()
	return h
}

func (h *harness) rearm() {
	h.t.Helper()
	h.exec(`DELETE FROM aura.data_upgrades WHERE name = $1`, upgradeName)
}

func (h *harness) exec(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.conn.Exec(h.ctx, sql, args...); err != nil {
		h.t.Fatalf("%s: %v", strings.Fields(sql)[0], err)
	}
}

func (h *harness) upgrade() Report {
	h.t.Helper()
	report, err := Upgrade(h.ctx, h.conn, h.log)
	if err != nil {
		h.t.Fatalf("Upgrade: %v", err)
	}
	return report
}

func (h *harness) identity() string {
	h.t.Helper()
	id := uuid.NewString()
	h.exec(`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, "pim-split-"+id[:8])
	h.t.Cleanup(func() {
		_, _ = h.conn.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE identity_id = $1`, id)
		_, _ = h.conn.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

func (h *harness) server(name, source string) {
	h.t.Helper()
	h.exec(`INSERT INTO aura.mcp_server (name, source) VALUES ($1, $2)`, name, source)
	h.t.Cleanup(func() { _, _ = h.conn.Exec(context.Background(), `DELETE FROM aura.mcp_server WHERE name = $1`, name) })
}

func (h *harness) grant(identity, tool, action string) {
	h.t.Helper()
	h.exec(`INSERT INTO aura.gateway_approval_grants (identity_id, tool, action, granted_by) VALUES ($1, $2, $3, 'alice')`,
		identity, tool, action)
}

func (h *harness) policy(identity, tool, action, policy string) {
	h.t.Helper()
	h.exec(`INSERT INTO aura.gateway_tool_policies (identity_id, tool, action, policy, set_by) VALUES ($1, $2, $3, $4, 'bob')`,
		identity, tool, action, policy)
}

// grants lists an identity's grants as "tool/action by granted_by".
func (h *harness) grants(identity string) []string {
	return h.strings(`SELECT tool || '/' || action || ' by ' || coalesce(granted_by, '')
FROM aura.gateway_approval_grants WHERE identity_id = $1 ORDER BY tool, action`, identity)
}

// policies lists an identity's policies as "tool/action=policy by set_by".
func (h *harness) policies(identity string) []string {
	return h.strings(`SELECT tool || '/' || action || '=' || policy || ' by ' || coalesce(set_by, '')
FROM aura.gateway_tool_policies WHERE identity_id = $1 ORDER BY tool, action`, identity)
}

func (h *harness) strings(sql string, args ...any) []string {
	h.t.Helper()
	rows, err := h.conn.Query(h.ctx, sql, args...)
	if err != nil {
		h.t.Fatalf("query: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		h.t.Fatalf("collect: %v", err)
	}
	return out
}

// snapshot is everything an identity has that the upgrade may touch, as one comparable string.
func (h *harness) snapshot(identity string) string {
	h.t.Helper()
	var out string
	err := h.conn.QueryRow(h.ctx, `SELECT concat_ws(E'\n',
  (SELECT json_agg(g ORDER BY tool, action)::text FROM aura.gateway_approval_grants g WHERE identity_id = $1),
  (SELECT json_agg(p ORDER BY tool, action)::text FROM aura.gateway_tool_policies p WHERE identity_id = $1),
  (SELECT json_agg(d ORDER BY id)::text FROM aura.message_drafts d WHERE identity_id = $1))`, identity).Scan(&out)
	if err != nil {
		h.t.Fatalf("snapshot: %v", err)
	}
	return out
}

func TestUpgradeTwiceChangesNothingTheSecondTime(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.grant(id, oldTool, "send_email")
	h.policy(id, oldTool, "", "deny")
	h.grant(id, oldTool, "")
	accounted := h.legacyDraft(id, legacyDraft{status: "pending", original: legacyArgs(t, sendArgs)})
	h.legacyDraft(id, legacyDraft{status: "pending", original: `{"to":["a@example.com"],"action":"send_email","subject":"Hi"}`})

	first := h.upgrade()
	if first != (Report{Rewritten: 1, FannedOutDenies: 1, Dropped: 1, DraftsRewritten: 1, DraftsExpired: 1}) {
		t.Fatalf("first Report = %+v", first)
	}
	before := h.snapshot(id)
	h.logs.Reset()
	if second := h.upgrade(); second != (Report{}) {
		t.Fatalf("second Report = %+v, want zero", second)
	}
	if after := h.snapshot(id); after != before {
		t.Fatalf("the second run changed rows:\nbefore %s\nafter  %s", before, after)
	}
	if !strings.Contains(h.logs.String(), "already_applied=true") {
		t.Fatalf("the second run did not say it had nothing to do: %s", h.logs.String())
	}
	if draft := h.readDraft(accounted); draft.tool != "send_email" {
		t.Fatalf("the rewritten draft = %+v", draft)
	}
}

// The marker, not the rows' shape, decides: a grant or policy set on a new tool after the upgrade is
// the shape of a hand-typed row, and an old-shape row typed later is the shape of a stored one.
func TestRowsWrittenAfterTheUpgradeSurviveTheNextMigrate(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	if first := h.upgrade(); first != (Report{}) {
		t.Fatalf("first Report over no rows = %+v", first)
	}
	sendEmail := mcptools.NamespacedName("calendar", "send_email")
	h.grant(id, sendEmail, "")
	h.policy(id, mcptools.NamespacedName("calendar", "delete_email"), "", "deny")
	h.grant(id, oldTool, "send_email")
	before := h.snapshot(id)
	if second := h.upgrade(); second != (Report{}) {
		t.Fatalf("second Report = %+v, want zero", second)
	}
	if after := h.snapshot(id); after != before {
		t.Fatalf("rows written after the upgrade changed:\nbefore %s\nafter  %s", before, after)
	}
}

// The migrate role owns the tables, so row security does not hide other identities' rows from it:
// an upgrade that saw only some of them would report a clean, partial run.
func TestMigrateRoleSeesEveryIdentity(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.identity(), h.identity()
	h.grant(alice, oldTool, "send_email")
	h.grant(bob, oldTool, "delete_email")
	if report := h.upgrade(); report != (Report{Rewritten: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	if got := h.grants(bob); len(got) != 1 || got[0] != "calendar__delete_email/ by alice" {
		t.Fatalf("bob's grants = %v", got)
	}
}

// A failed upgrade fails the migrate: it reports nothing and leaves the marker unwritten.
func TestUpgradeOnALostConnectionFails(t *testing.T) {
	h := newHarness(t)
	conn, err := pgx.Connect(h.ctx, h.conn.Config().ConnString())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := conn.Close(h.ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	report, err := Upgrade(h.ctx, conn, h.log)
	if err == nil || report != (Report{}) {
		t.Fatalf("Upgrade on a closed conn = %+v, %v", report, err)
	}
	if got := h.strings(`SELECT name FROM aura.data_upgrades WHERE name = $1`, upgradeName); len(got) != 0 {
		t.Fatalf("the marker was written: %v", got)
	}
}
