//go:build spike_casbin

// Spike 087b: casbin-adapter-sql-stdlib (Blank-Xu/sql-adapter, casbin/v2)
//
// The database/sql side of the 087 comparison (vs 087a pckhoi pgx-native). Same
// contract — persist Casbin policy against Aura's LIVE Postgres — but through the
// generic `database/sql` layer instead of native pgx. It shows what Aura gives up
// (and keeps) by choosing the portable-SQL adapter.
//
// Version: casbin/v2 via Blank-Xu/sql-adapter v1.1.0 (the last v2 line; v1.2.1
// jumped to casbin/v3 — an ecosystem-drift risk noted in the README). The adapter
// takes a *sql.DB, so to SHARE Aura's pool it must be bridged with
// stdlib.OpenDBFromPool — pckhoi takes the *pgxpool.Pool directly.
//
// Run (live Postgres required):
//
//	PW=$(docker exec aura-postgres printenv POSTGRES_PASSWORD) \
//	AURA_DB_URL="postgres://aura:$PW@127.0.0.1:5432/aura?sslmode=disable" \
//	go run -tags spike_casbin ./.planning/spikes/087b-casbin-adapter-sql-stdlib
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	sqladapter "github.com/Blank-Xu/sql-adapter"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/chetto1983/aura/internal/db"
)

func ts() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func logf(cat, format string, a ...any) {
	fmt.Printf("[%s] %s %s\n", ts(), cat, fmt.Sprintf(format, a...))
}

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

// migrateDDL mirrors Blank-Xu's exact postgres shape (VARCHAR(32) p_type,
// VARCHAR(255) v0..v5 DEFAULT ” NOT NULL). The adapter's constructor guards its
// CREATE behind IsTableExist, so a migrate-owned table = zero adapter DDL (an
// IMPLICIT skip-create — no explicit option like pckhoi's WithSkipTableCreate).
const migrateDDL = `
CREATE TABLE IF NOT EXISTS aura.casbin_rule (
	p_type VARCHAR(32)  DEFAULT '' NOT NULL,
	v0 VARCHAR(255) DEFAULT '' NOT NULL,
	v1 VARCHAR(255) DEFAULT '' NOT NULL,
	v2 VARCHAR(255) DEFAULT '' NOT NULL,
	v3 VARCHAR(255) DEFAULT '' NOT NULL,
	v4 VARCHAR(255) DEFAULT '' NOT NULL,
	v5 VARCHAR(255) DEFAULT '' NOT NULL
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
	logf("SETUP", "spike 087b — Blank-Xu sql-adapter (casbin/v2) over database/sql bridged from Aura's pgxpool")

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

	dropSpikeTable(ctx, pool)
	if _, err := pool.Exec(ctx, migrateDDL); err != nil {
		fail("apply migrate-shaped DDL", err)
	}

	// ---- (1) pool sharing requires the stdlib bridge (vs pckhoi's direct pool) ----
	sqlDB := stdlib.OpenDBFromPool(pool)
	logf("PROBE", "reconciliation — migrate pre-created aura.casbin_rule; adapter uses the IsTableExist guard (implicit skip-create)")
	adapter, err := sqladapter.NewAdapter(sqlDB, "pgx", "aura.casbin_rule")
	if err != nil {
		fail("NewAdapter(*sql.DB via stdlib bridge, schema-qualified table)", err)
	}
	// Skip-create was implicit: the migrate-shaped table still has the VARCHAR(32)
	// p_type (adapter did not drop/recreate it).
	hasPType := columnExists(ctx, pool, "p_type")
	tblCount := countTable(ctx, pool)
	check(tblCount == 1 && hasPType, "adapter ran zero DDL (IsTableExist guard) — migrate-owned aura.casbin_rule intact; pool shared via stdlib.OpenDBFromPool",
		fmt.Sprintf("schema drift: table=%d hasPType=%v", tblCount, hasPType))

	// ---- (2) autosave persistence ----
	m, err := model.NewModelFromString(domainsModel)
	if err != nil {
		fail("model", err)
	}
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		fail("NewEnforcer", err)
	}
	e.EnableAutoSave(true)
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
	check(pRows == 3 && gRows == 3, "autosave persisted 3 p-rows + 3 g-rows to aura.casbin_rule via database/sql",
		fmt.Sprintf("row mismatch: p=%d g=%d (want 3/3)", pRows, gRows))

	// ---- (3) durability across enforcer lifecycle ----
	m2, _ := model.NewModelFromString(domainsModel)
	e2, err := casbin.NewEnforcer(m2, adapter)
	if err != nil {
		fail("NewEnforcer (reload)", err)
	}
	if err := e2.LoadPolicy(); err != nil {
		fail("LoadPolicy", err)
	}
	aliceMgrA, _ := e2.Enforce("u-alice", "dept-a", "budget", "write")
	aliceMgrB, _ := e2.Enforce("u-alice", "dept-b", "budget", "write")
	check(aliceMgrA && !aliceMgrB, "policy survived enforcer lifecycle: alice manager@A allow, manager@B deny (full reload)",
		fmt.Sprintf("durability wrong: mgrA=%v mgrB=%v", aliceMgrA, aliceMgrB))

	// ---- (4) filtered load: single-column works, per-tenant DOMAINS does NOT ----
	// Blank-Xu's Filter is a SINGLE V0..V5 constraint set applied uniformly across
	// ALL p_types. In RBAC-with-domains the domain sits in v1 for p-rows but v2 for
	// g-rows — so ONE filter cannot select "p@dept-a OR g@dept-a" (pckhoi's Filter{P,G}
	// can). Filter{V1:[dept-a]} loads the 2 dept-a p-rows but ZERO g-rows (their v1 is
	// the role), so role resolution breaks under filtered load — the fit limitation.
	m3, _ := model.NewModelFromString(domainsModel)
	e3, err := casbin.NewEnforcer(m3, adapter)
	if err != nil {
		fail("NewEnforcer (filtered)", err)
	}
	if err := e3.LoadFilteredPolicy(&sqladapter.Filter{V1: []string{"dept-a"}}); err != nil {
		fail("LoadFilteredPolicy", err)
	}
	loadedP, _ := e3.GetPolicy()
	loadedG, _ := e3.GetGroupingPolicy()
	bobViewA, _ := e3.Enforce("u-bob", "dept-a", "budget", "read") // grouping NOT loaded → deny
	singleColWorks := len(loadedP) == 2
	domainsLimitation := len(loadedG) == 0 && !bobViewA
	check(singleColWorks, "single-column filtered load works (2 dept-a p-rows loaded)",
		fmt.Sprintf("expected 2 p-rows, got %d", len(loadedP)))
	check(domainsLimitation,
		"CONFIRMED LIMITATION: single-Filter API cannot co-filter g-rows by domain (v2) → 0 grouping rows, role resolution breaks under per-tenant load",
		fmt.Sprintf("expected the limitation (0 g-rows, deny), got gRows=%d bobViewA=%v", len(loadedG), bobViewA))

	dropSpikeTable(ctx, pool)
	logf("CLEANUP", "dropped aura.casbin_rule (spike-created); Aura schema restored")

	logf("RESULT", "fails=%d over 5 assertions (incl. the documented domains-filter limitation)", fails)
	if fails == 0 {
		logf("SUMMARY", "VALIDATED (runner-up) — Blank-Xu sql-adapter persists+reloads over database/sql and reconciles with migrate, BUT needs the stdlib bridge to share the pool and its single-Filter API cannot do per-tenant DOMAINS load (pckhoi wins on both)")
		os.Exit(0)
	}
	logf("SUMMARY", "INVALIDATED — %d assertion(s) failed", fails)
	os.Exit(1)
}
