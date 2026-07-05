---
spike: 087a
name: casbin-adapter-pgx-native
type: comparison
validates: "Given Aura's live pgx/v5 pool + aura.* schema, when policy persists via pckhoi/casbin-pgx-adapter (casbin/v2), then load/save/autosave/filtered-load work AND golang-migrate owns the casbin_rule table (adapter runs zero DDL)"
verdict: WINNER
related: [086, 087b, 088]
tags: [casbin, adapter, pgx, postgres, persistence, phase-casbin]
---

# Spike 087a: Casbin pgx-native adapter (pckhoi) — WINNER

## What This Validates

Given Aura's live pgx/v5 pool + `aura.*` schema, **when** Casbin policy is persisted
via the pgx-native adapter, **then** it shares Aura's existing `*pgxpool.Pool`, lets
golang-migrate own the DDL, persists + reloads across enforcer lifecycles, and
supports per-tenant filtered load — all on the most-stable casbin/v2 line.

Head of the 087 comparison against 087b (Blank-Xu, database/sql).

## Research

- **Version choice (operator directive "most stable version fit in aura"):**
  `casbin/v2 v2.135.0` — the mature line — with `pckhoi/casbin-pgx-adapter/v3 v3.2.0`.
  The `/v3` is the ADAPTER's own major version; it targets `casbin/v2` + `pgx/v5`.
- **Why pckhoi over the casbin/v3 alternatives:** an earlier detour to casbin/v3
  (per an initial "use all v3") found the only v3 pgx-native adapter
  (`noho-digital/casbin-pgx-adapter`) lacks `WithSchema` and `WithSkipTableCreate` —
  it always runs its own `CREATE TABLE IF NOT EXISTS` and can only land the table via
  a `search_path` hack (it quotes the table name as a single identifier). pckhoi
  (v2) exposes exactly the options Aura's migrate/sqlc discipline needs. Since v2 is
  also the more stable line, the directive resolved to v2 + pckhoi. `casbin/gorm-adapter/v3`
  is v3-native but GORM (Aura is no-GORM → rejected).
- **pckhoi API (module-cache source, ground truth):** `NewAdapter(conn, opts...)`;
  `WithConnectionPool(*pgxpool.Pool)`, `WithSchema(string)`, `WithTableName(string)`,
  `WithSkipTableCreate()`, `WithTimeout`. Native pgx (`pool.Query` + `pgx.ForEachRow`),
  NOT a database/sql bridge. `Filter{P, G [][]string}` — separate positional filters
  for p-rows and g-rows (the key domains capability). Table DDL: `id text PK` (=policy
  hash), `p_type` + `v0..v5 text`.

## How to Run

```bash
PW=$(docker exec aura-postgres printenv POSTGRES_PASSWORD) \
AURA_DB_URL="postgres://aura:$PW@127.0.0.1:5432/aura?sslmode=disable" \
go run -tags spike_casbin ./.planning/spikes/087a-casbin-adapter-pgx-native
```
DSN composed inline so the password never prints. Idempotent: drops the spike table
on entry and exit — the live `aura` schema is left untouched.

## What to Expect

5 fit assertions PASS, `fails=0`: (1) migrate owns the table + adapter runs zero DDL,
(2) autosave persists 3 p + 3 g rows via the shared pool, (3) durability across a
fresh enforcer's LoadPolicy, (4) per-tenant filtered load (dept-a only, dept-b
excluded), (5) — wait, 5 assertions: reconciliation, autosave, durability, filtered.

## Investigation Trail

1. **Migrate ownership** — pre-created `aura.casbin_rule` with pckhoi's exact shape,
   then built the adapter `WithSkipTableCreate()`. Asserted via `information_schema`
   that exactly one table exists in `aura` and the `p_type` column is intact — the
   adapter ran NO DDL. This is the clean reconciliation: migrate is the sole schema
   owner, the adapter never touches DDL.
2. **Pool sharing** — passed Aura's real `db.Open()` pool via `WithConnectionPool`;
   the adapter is native pgx so it uses that pool directly (no second connection, no
   stdlib bridge). Verified writes are visible through the same pool.
3. **Autosave + durability** — `EnableAutoSave(true)`; AddPolicy/AddGroupingPolicy
   INSERT immediately (3 p + 3 g rows in the DB). A fresh enforcer's LoadPolicy
   reconstructs the exact allow/deny (alice manager@A allow, manager@B deny).
4. **Filtered per-tenant load** — hit a **pckhoi bug** first (see Results): the filter
   SQL builder emits a trailing ` AND ` when a matched column is followed by later
   columns. Trimming trailing empty columns fixed it; `Filter{P:{{"","dept-a"}},
   G:{{"","","dept-a"}}}` then loaded exactly dept-a's 2 p-rows + its g-rows,
   excluding dept-b. `IsFiltered()==true`.

## Results

**WINNER.** `fails=0` over 5 live assertions against Aura's real Postgres.

- **Shares Aura's `*pgxpool.Pool` directly** (native pgx) — no second connection path.
- **golang-migrate owns `casbin_rule`** via `WithSkipTableCreate()` — the adapter runs
  zero DDL, so the table lands as a normal Aura migration (sqlc can even generate a
  typed client over it if wanted).
- **`WithSchema("aura")`** puts the table in Aura's schema cleanly (`"aura"."casbin_rule"`),
  no `search_path` hack.
- **`Filter{P, G}`** expresses per-tenant domains load correctly (p filtered by
  dom=v1, g by dom=v2 in one call) — the capability the org-roles phase (088) needs
  at scale.

### Sharp edge (carry to the build)

- **pckhoi filter builder trailing-`AND` bug:** a `Filter` row whose matched column is
  followed by ANY later column (even empty `""`) yields invalid SQL (`v1 = $1 AND )`).
  **Always trim trailing empty columns** so the last element of each `Filter` row is
  the matched one (`{"", "dept-a"}`, not `{"", "dept-a", "", ""}`). A thin wrapper
  around `LoadFilteredPolicy` should normalise filter rows before passing them down.

### Constraints

- casbin/v2 (v2.135.0) — the stable line. pckhoi targets v2; it does NOT support
  casbin/v3.
- `id text` PK is a hash of `(p_type, rule)` — dedupes exact-duplicate policies for
  free (a repeat AddPolicy is a PK conflict the adapter handles).
