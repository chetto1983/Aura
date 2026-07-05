---
spike: 087b
name: casbin-adapter-sql-stdlib
type: comparison
validates: "Given Aura's live Postgres, when policy persists via Blank-Xu/sql-adapter over database/sql (pgx stdlib), then load/save/autosave/reconcile-with-migrate work — but pool-sharing needs the stdlib bridge and per-tenant DOMAINS load is not expressible"
verdict: RUNNER-UP
related: [086, 087a, 088]
tags: [casbin, adapter, sql, stdlib, postgres, phase-casbin]
---

# Spike 087b: Casbin database/sql adapter (Blank-Xu) — RUNNER-UP

## What This Validates

The portable `database/sql` side of the 087 comparison. Given Aura's live Postgres,
**when** policy persists via Blank-Xu/sql-adapter, **then** it persists, reloads, and
reconciles with a migrate-owned table — but it needs the stdlib bridge to share
Aura's pool and its single-`Filter` API cannot express a per-tenant DOMAINS load.

## Research

- **Version:** casbin/v2 via `Blank-Xu/sql-adapter v1.1.0` — the last v2 line.
  **`v1.2.1` jumped to `casbin/casbin/v3`** (confirmed from its go.mod): adopting
  latest would force the whole authz stack to casbin/v3, an ecosystem-drift risk
  against the chosen stable v2. Pin v1.1.0 for a v2 comparison.
- **API (module-cache source):** `NewAdapter(db *sql.DB, driverName, tableName)`.
  Takes a `*sql.DB`, not a pool — sharing Aura's pgxpool requires
  `stdlib.OpenDBFromPool(pool)`. driverName `"pgx"` selects the postgres dialect.
  `tableName` is interpolated unquoted, so `"aura.casbin_rule"` schema-qualifies.
  Constructor guards CREATE behind `IsTableExist` — an IMPLICIT skip-create (no
  explicit option). `Filter{PType, V0..V5 []string}` — a SINGLE constraint set applied
  across all p_types; no batch/OR form.

## How to Run

```bash
PW=$(docker exec aura-postgres printenv POSTGRES_PASSWORD) \
AURA_DB_URL="postgres://aura:$PW@127.0.0.1:5432/aura?sslmode=disable" \
go run -tags spike_casbin ./.planning/spikes/087b-casbin-adapter-sql-stdlib
```
Idempotent; drops the spike table on entry/exit.

## What to Expect

5 assertions PASS incl. one that CONFIRMS a limitation: single-column filter works,
but the per-tenant domains load (grouping rows co-filtered by domain) does not.

## Investigation Trail

1. **Bridge + reconcile** — bridged Aura's pool with `stdlib.OpenDBFromPool`, passed
   the schema-qualified `"aura.casbin_rule"`. The `IsTableExist` guard found the
   migrate-created table and ran no DDL (implicit skip-create).
2. **Persist + reload** — autosave INSERTed 3 p + 3 g rows; a fresh enforcer's
   LoadPolicy reconstructed the correct allow/deny.
3. **Filtered load** — `Filter{V1:[dept-a]}` loaded the 2 dept-a p-rows (domain=v1)
   but ZERO g-rows: in a domains model the grouping rows carry the domain in **v2**,
   not v1, and a single `Filter` can't constrain v1-for-p AND v2-for-g at once.
   Under that filtered load role resolution breaks (bob's viewer@dept-a grant isn't
   loaded → deny). Confirmed empirically as the fit limitation.

## Results

**RUNNER-UP.** `fails=0` — it works, but loses to pckhoi (087a) on two Aura-specific axes:

- **Pool sharing is indirect:** it operates through `database/sql`, so sharing Aura's
  `*pgxpool.Pool` requires `stdlib.OpenDBFromPool`. pckhoi takes the pool natively.
- **No per-tenant DOMAINS load:** the single-`Filter` API cannot co-filter p-rows
  (dom=v1) and g-rows (dom=v2) in one call — the exact pattern the org-roles phase
  needs. pckhoi's `Filter{P, G}` does this directly.

### What it does keep (why it's a viable fallback)

- **Portable** — the same adapter serves SQLite/MySQL/MSSQL, useful if Aura ever
  needs a non-Postgres authz store.
- **Implicit skip-create** via `IsTableExist` reconciles with golang-migrate with no
  explicit option.
- **Schema-qualified table name** works (`aura.casbin_rule`).

### Constraint / landmine

- **Latest (v1.2.1) is casbin/v3-only.** Staying on v2 pins v1.1.0; a future casbin/v3
  migration could adopt v1.2.1, but that is a coupled version jump, not a drop-in.

## Head-to-head verdict (087a vs 087b)

| Axis | pckhoi (087a) | Blank-Xu (087b) |
|------|---------------|-----------------|
| Driver | **native pgx** | database/sql |
| Share Aura's pool | **direct** `WithConnectionPool` | via `stdlib.OpenDBFromPool` bridge |
| Migrate owns table | **explicit** `WithSkipTableCreate` | implicit `IsTableExist` guard |
| Schema | **`WithSchema("aura")`** | schema-qualified table name |
| Per-tenant domains load | **`Filter{P,G}` — yes** | single `Filter` — **no** |
| casbin/v2 pin | v3.2.0 (targets v2) | v1.1.0 (v1.2.1 → v3) |
| Sharp edge | filter trailing-`AND` (trim empties) | — |

**Adopt pckhoi/casbin-pgx-adapter.** Keep Blank-Xu noted as the portable fallback.
