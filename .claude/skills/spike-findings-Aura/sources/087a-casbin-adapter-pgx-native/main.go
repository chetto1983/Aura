//go:build spike_casbin

// Spike 087a: casbin-adapter-pgx-native (pckhoi/casbin-pgx-adapter, casbin/v2)
//
// Comparison spike (vs 087b Blank-Xu/sql-adapter). Question: does the pgx-native
// Casbin adapter persist policy against Aura's LIVE Postgres while honouring
// Aura's discipline — share the existing *pgxpool.Pool, live in the `aura` schema,
// let golang-migrate OWN the DDL (adapter runs none), and support per-tenant
// filtered load for the org-roles domains (088)?
//
// Version choice (operator directive "most stable version fit in aura"): casbin/v2
// v2.135.0 — the mature line — with pckhoi/casbin-pgx-adapter/v3 v3.2.0 (its `/v3`
// is the ADAPTER's own major; it targets casbin/v2 + pgx/v5). Unlike the casbin/v3
// noho adapter, pckhoi exposes WithSchema + WithSkipTableCreate + WithConnectionPool,
// which is exactly what Aura's migrate/sqlc discipline needs.
//
// Run (live Postgres required; compose the DSN inline so the password never prints):
//
//	PW=$(docker exec aura-postgres printenv POSTGRES_PASSWORD) \
//	AURA_DB_URL="postgres://aura:$PW@127.0.0.1:5432/aura?sslmode=disable" \
//	go run -tags spike_casbin ./.planning/spikes/087a-casbin-adapter-pgx-native
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	pgxadapter "github.com/pckhoi/casbin-pgx-adapter/v3"

	"github.com/chetto1983/aura/internal/db"
)

func ts() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func logf(cat, format string, a ...any) {
	fmt.Printf("[%s] %s %s\n", ts(), cat, fmt.Sprintf(format, a...))
}

// domainsModel — canonical Casbin RBAC-with-domains (shared with 088). Columns:
// p = sub,dom,obj,act → v0=sub v1=dom v2=obj v3=act ; g = _,_,_ → v0=user v1=role v2=dom.
const domainsModel = `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && r.dom == p.dom && r.obj == p.obj && r.act == p.act
`

// migrateDDL mirrors pckhoi's exact createTable shape (id text PK = policy hash,
// p_type + v0..v5 text). In production this is a golang-migrate up-migration; the
// adapter is then built WithSkipTableCreate so it runs NO DDL at all — migrate is
// the sole schema owner (the clean Aura fit). Schema-qualified via WithSchema, no
// search_path hack needed (pckhoi quotes `"aura"."casbin_rule"`).
const migrateDDL = `
CREATE TABLE IF NOT EXISTS aura.casbin_rule (
	id text PRIMARY KEY,
	p_type text,
	v0 text, v1 text, v2 text, v3 text, v4 text, v5 text
);`

func fail(msg string, err error) {
	logf("ERROR", "%s: %v", msg, err)
	logf("SUMMARY", "INVALIDATED — %s", msg)
	os.Exit(1)
}

func main() {
	ctx := context.Background()
	dsn := os.Getenv("AURA_DB_URL")
	if dsn == "" {
		fail("AURA_DB_URL unset", fmt.Errorf("compose it inline; see header"))
	}
	logf("SETUP", "spike 087a — pckhoi pgx-native adapter (casbin/v2) over Aura's live Postgres, schema=aura")

	pool, err := db.Open(ctx, &db.Config{URL: dsn})
	if err != nil {
		fail("db.Open (is the stack up?)", err)
	}
	defer pool.Close()

	fails := 0
	check := func(cond bool, pass, failmsg string) {
		if cond {
			logf("CASE", "PASS  %s", pass)
		} else {
			logf("CASE", "FAIL  %s", failmsg)
			fails++
		}
	}

	// Clean slate so a prior run can't skew counts (spike is idempotent).
	dropSpikeTable(ctx, pool)

	// ---- (1) golang-migrate owns the DDL; adapter built WithSkipTableCreate ----
	if _, err := pool.Exec(ctx, migrateDDL); err != nil {
		fail("apply migrate-shaped DDL", err)
	}
	logf("PROBE", "reconciliation — migrate pre-created aura.casbin_rule; adapter WithSkipTableCreate runs NO DDL")

	adapter, err := pgxadapter.NewAdapter("",
		pgxadapter.WithConnectionPool(pool), // share Aura's exact *pgxpool.Pool (native pgx)
		pgxadapter.WithSchema("aura"),       // schema-qualified "aura"."casbin_rule"
		pgxadapter.WithTableName("casbin_rule"),
		pgxadapter.WithSkipTableCreate(), // migrate is the sole schema owner
	)
	if err != nil {
		fail("NewAdapter(WithConnectionPool/WithSchema/WithSkipTableCreate)", err)
	}
	// Prove skip-create truly skipped: exactly ONE casbin_rule in aura, and it is
	// the migrate-shaped one (has the `p_type` column, i.e. adapter didn't recreate).
	tblCount := countTable(ctx, pool)
	hasPType := columnExists(ctx, pool, "p_type")
	check(tblCount == 1 && hasPType, "adapter ran zero DDL — migrate-owned aura.casbin_rule is the sole table (p_type column intact)",
		fmt.Sprintf("schema drift: casbin_rule count=%d hasPType=%v", tblCount, hasPType))

	// ---- (2) pool sharing is by construction (WithConnectionPool). Verify writes
	// are visible through Aura's SAME pool. ----
	m, err := model.NewModelFromString(domainsModel)
	if err != nil {
		fail("model", err)
	}
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		fail("NewEnforcer", err)
	}
	e.EnableAutoSave(true)

	// ---- (3) autosave persistence: AddPolicy/AddGroupingPolicy hit the DB ----
	seed := [][]string{
		{"role-manager", "dept-a", "budget", "write"},
		{"role-viewer", "dept-a", "budget", "read"},
		{"role-manager", "dept-b", "budget", "write"},
	}
	for _, p := range seed {
		if _, err := e.AddPolicy(p[0], p[1], p[2], p[3]); err != nil {
			fail("AddPolicy", err)
		}
	}
	grants := [][]string{
		{"u-alice", "role-manager", "dept-a"},
		{"u-alice", "role-viewer", "dept-b"},
		{"u-bob", "role-viewer", "dept-a"},
	}
	for _, g := range grants {
		if _, err := e.AddGroupingPolicy(g[0], g[1], g[2]); err != nil {
			fail("AddGroupingPolicy", err)
		}
	}
	pRows := countRows(ctx, pool, "p")
	gRows := countRows(ctx, pool, "g")
	check(pRows == 3 && gRows == 3, "autosave persisted 3 p-rows + 3 g-rows, visible through Aura's shared pool",
		fmt.Sprintf("row mismatch: p=%d g=%d (want 3/3)", pRows, gRows))

	// ---- (4) durability across enforcer lifecycle: a FRESH enforcer LoadPolicy ----
	m2, _ := model.NewModelFromString(domainsModel)
	e2, err := casbin.NewEnforcer(m2, adapter)
	if err != nil {
		fail("NewEnforcer (reload)", err)
	}
	if err := e2.LoadPolicy(); err != nil {
		fail("LoadPolicy", err)
	}
	aliceMgrA, _ := e2.Enforce("u-alice", "dept-a", "budget", "write") // manager in A → allow
	aliceMgrB, _ := e2.Enforce("u-alice", "dept-b", "budget", "write") // only viewer in B → deny
	check(aliceMgrA && !aliceMgrB, "policy survived enforcer lifecycle: alice manager@A allow, manager@B deny (reloaded from DB)",
		fmt.Sprintf("durability wrong: mgrA=%v mgrB=%v", aliceMgrA, aliceMgrB))

	// ---- (5) filtered per-tenant load: load ONLY dept-a rows ----
	m3, _ := model.NewModelFromString(domainsModel)
	e3, err := casbin.NewEnforcer(m3, adapter)
	if err != nil {
		fail("NewEnforcer (filtered)", err)
	}
	// pckhoi Filter is positional: P rows [sub,dom,...] ("" = any) → dom(v1)=dept-a;
	// G rows [user,role,dom] → dom(v2)=dept-a.
	// SHARP EDGE (pckhoi bug): the filter-SQL builder appends a trailing ` AND ` when a
	// matched column is followed by ANY later column (even empty), producing invalid SQL
	// (`v1 = $1 AND )`). Trailing empty columns MUST be trimmed, so the last element of
	// each row is the matched one — hence `{"", "dept-a"}` not `{"", "dept-a", "", ""}`.
	deptA := &pgxadapter.Filter{
		P: [][]string{{"", "dept-a"}},
		G: [][]string{{"", "", "dept-a"}},
	}
	if err := e3.LoadFilteredPolicy(deptA); err != nil {
		fail("LoadFilteredPolicy", err)
	}
	filtered := e3.IsFiltered()
	loadedPolicies, _ := e3.GetPolicy()
	pLoaded := len(loadedPolicies)
	bobViewA, _ := e3.Enforce("u-bob", "dept-a", "budget", "read") // in dept-a set → allow
	mgrBExcluded, _ := e3.Enforce("role-manager", "dept-b", "budget", "write")
	check(filtered && pLoaded == 2 && bobViewA && !mgrBExcluded,
		fmt.Sprintf("per-tenant filtered load: dept-a only (2 p-rows, isFiltered=%v, dept-b excluded)", filtered),
		fmt.Sprintf("filtered load wrong: filtered=%v pLoaded=%d bobViewA=%v mgrBExcluded=%v", filtered, pLoaded, bobViewA, mgrBExcluded))

	// ---- cleanup (explicit — os.Exit skips defers; spike 085 gotcha) ----
	dropSpikeTable(ctx, pool)
	logf("CLEANUP", "dropped aura.casbin_rule (spike-created); Aura schema restored")

	logf("RESULT", "fails=%d over 5 fit assertions", fails)
	if fails == 0 {
		logf("SUMMARY", "VALIDATED — pckhoi pgx adapter shares Aura's pool, lets migrate own the DDL (WithSkipTableCreate), persists+reloads, and does per-tenant filtered load on casbin/v2")
		os.Exit(0)
	}
	logf("SUMMARY", "INVALIDATED — %d fit assertion(s) failed", fails)
	os.Exit(1)
}
