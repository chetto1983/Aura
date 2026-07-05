# Casbin authz engine + org-roles (deferred phase — PRD-amendment required)

The forward-bet authz work Phase 36 explicitly deferred (D-04, CONTEXT §Deferred):
ground-truth Casbin over Aura's real stack so it can later BACK the `HasCapability`
interface (zero rework) AND grow into SMB org-roles (manager/employee/viewer per
department) for the commercial DGX-Spark bundle. Proven live in spikes 086-089.

**Implementation is gated on a PRD-amendment reopening the locked "no RBAC / capability_grants"
decision.** Until then this is a forward bet reached through the existing interface.

## Requirements (non-negotiable for the real build)

- **Engine = `github.com/casbin/casbin/v2` v2.135.0** — the mature/stable line. NOT
  casbin/v3: an initial "use all v3" was reverted because the only v3 pgx-native adapter
  (`noho-digital`) lacks the schema/skip-create options Aura's migrate discipline needs,
  and `gorm-adapter/v3` is GORM (Aura is no-GORM). Operator directive: "most stable
  version fit in aura" → v2.
- **Adapter = `github.com/pckhoi/casbin-pgx-adapter/v3` v3.2.0** (the `/v3` is the
  ADAPTER's major; it targets casbin/v2 + pgx/v5). Native pgx. Blank-Xu/sql-adapter is a
  documented portable FALLBACK only.
- **golang-migrate owns `aura.casbin_rule`** — the adapter runs ZERO DDL
  (`WithSkipTableCreate`); the table ships as an Aura migration, sqlc may type it.
- **ONE RBAC-with-domains model** serves both Phase-36 flat capabilities AND org-roles.
- **`HasCapability(id, cap)` → `HasCapability(id, dom, cap)`** is the additive migration;
  the `internal/agui` `RequireCapability` gateway is unchanged (only the backend call swaps).
- **The management API MUST reuse `internal/identity.ValidateCapabilityName`** — the
  `*`-is-system-managed rule + the `^[a-z][a-z0-9._-]{0,63}$` grammar are NOT in the
  Casbin model.

## How to Build It

### 1. The model (one superset for flat caps + org-roles)

```ini
[request_definition]
r = sub, dom, obj, act
[policy_definition]
p = sub, dom, obj, act
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub, r.dom) && (p.dom == "*" || r.dom == p.dom) && (p.obj == "*" || r.obj == p.obj) && (p.act == "*" || r.act == p.act)
```

- Flat capability (086 parity): `p(id, "*", cap, "*")`; the seeded `local` wildcard =
  `p(local, "*", "*", "*")`. Casbin's default role manager self-links `g(x,x,dom)=true`,
  so a direct policy enforces without any role edge.
- Org-role permission: `p(role, dept, resource, verb)`.
- User→role grant PER domain: `g(user, role, dept)` — this is the isolation lever.
- Role hierarchy PER domain: `g(role-manager, role-employee, dept)`.
- Wildcard is an **explicit disjunct** (`p.obj == "*"`), NOT `keyMatch`/`regexMatch` —
  keeps cap-name dots literal, no accidental prefix/hierarchy match.

### 2. The adapter (migrate-owned table, shared pool)

golang-migrate up-migration (the SOLE schema owner — mirror pckhoi's shape exactly):

```sql
CREATE TABLE IF NOT EXISTS aura.casbin_rule (
    id text PRIMARY KEY,          -- policy hash (dedupes exact-duplicate policies)
    p_type text,
    v0 text, v1 text, v2 text, v3 text, v4 text, v5 text
);
```

Construct over Aura's existing pool:

```go
adapter, err := pgxadapter.NewAdapter("",
    pgxadapter.WithConnectionPool(pool),        // shares Aura's *pgxpool.Pool (native pgx)
    pgxadapter.WithSchema("aura"),              // "aura"."casbin_rule"
    pgxadapter.WithTableName("casbin_rule"),
    pgxadapter.WithSkipTableCreate(),           // migrate owns DDL; adapter runs none
)
e, _ := casbin.NewEnforcer(model, adapter)
e.EnableAutoSave(true)                          // Add/Remove* → INSERT/DELETE immediately
```

Per-tenant filtered load (domains at scale) — **trim trailing empty columns** (see
sharp edge):

```go
deptA := &pgxadapter.Filter{
    P: [][]string{{"", "dept-a"}},              // p rows where dom(v1)=dept-a
    G: [][]string{{"", "", "dept-a"}},          // g rows where dom(v2)=dept-a
}
e.LoadFilteredPolicy(deptA)
```

### 3. The gateway (drop-in over RequireCapability)

`internal/agui/auth.go`'s `RequireCapability` control flow is unchanged — only the
backend call becomes `Enforce`:

```go
id := principalFrom(r.Context())
if id == "" { http.Error(w, "forbidden", 403); return }
dom := routedDepartment(r)                       // server-side, NEVER a client header
ok, err := enforcer.Enforce(id, dom, obj, act)
if err != nil || !ok { http.Error(w, "forbidden", 403); return }
```

`HasCapability(ctx, id, cap)` is preserved as `Enforce(id, defaultDomain, cap, actAny)`
so Phase 36 callers keep their 2-arg semantics via a shim; org-roles pass the real dept.

### 4. The management API (grant/revoke = policy mutation, with the guard)

```go
func grantPermission(e *casbin.Enforcer, role, dom, obj, act string) error {
    if err := identity.ValidateCapabilityName(obj); err != nil { // REUSE Aura's real guard
        return err                                                // rejects "*" + bad grammar
    }
    _, err := e.AddPolicy(role, dom, obj, act)
    return err
}
// role grant/revoke: e.AddGroupingPolicy(user, role, dept) / e.RemoveGroupingPolicy(...)
```

Both the CLI (`aura identity grant/revoke`) and the admin Settings control call these —
D-26 parity. In a single-binary appliance mutations are instantly live (one enforcer).

## What to Avoid

- **DON'T skip `ValidateCapabilityName` in the management layer.** Casbin will blindly
  persist `p(role, *, "*", "*")` = a super-user, and does zero name validation. The
  `*`-guard + grammar are gone the moment you `AddPolicy` raw. (086/089.)
- **DON'T leave trailing empty columns in a pckhoi `Filter` row.** Its filter-SQL builder
  emits a trailing ` AND ` when a matched column is followed by later ones → invalid SQL
  (`v1 = $1 AND )`). Trim so the last element is the matched one (`{"", "dept-a"}`). Wrap
  `LoadFilteredPolicy` to normalise. (087a.)
- **DON'T use `keyMatch`/`regexMatch` for the capability match** — it would turn cap-name
  dots into globs. Use `==` + an explicit `"*"` disjunct. (086.)
- **DON'T reach for casbin/v3.** Its Postgres-adapter ecosystem is weaker for Aura
  (noho lacks schema/skip-create; gorm is GORM). Blank-Xu v1.2.1 forces v3 — pin v1.1.0
  if you ever use it. (087.)
- **DON'T grant a "global super-admin" as a role in one domain** expecting it everywhere.
  A role grant is domain-scoped (the point). Use a DIRECT `p(id, "*", "*", "*")`. (088.)
- **DON'T add a Watcher for the single appliance.** One in-process enforcer = mutations
  instantly live. A Casbin Watcher (Postgres LISTEN/NOTIFY) is ONLY for multi-replica. (089.)

## Constraints

- `casbin/casbin/v2 v2.135.0`; `pckhoi/casbin-pgx-adapter/v3 v3.2.0` (casbin/v2 + pgx/v5 —
  Aura is pgx/v5 v5.9.2); `Blank-Xu/sql-adapter v1.1.0` (fallback; v1.2.1 → casbin/v3).
- `GetPolicy()`/`GetGroupingPolicy()` return `([][]string, error)` in v2.135 (2 values).
- Adapter version-fit is decided by the ADAPTER ecosystem, not the core lib's latest tag —
  read each candidate's `go.mod` for its casbin-major target before adopting.
- Live-DB harnesses compose the DSN inline (never echo `POSTGRES_PASSWORD`) and open the
  pool via the real `internal/db.Open`; drop any spike-created `casbin_rule` on entry/exit.

## Adapter head-to-head (087a WINNER vs 087b RUNNER-UP)

| Axis | pckhoi (087a) | Blank-Xu (087b) |
|------|---------------|-----------------|
| Driver | **native pgx** | database/sql |
| Share Aura's pool | **direct** `WithConnectionPool` | via `stdlib.OpenDBFromPool` bridge |
| Migrate owns table | **explicit** `WithSkipTableCreate` | implicit `IsTableExist` guard |
| Schema | **`WithSchema("aura")`** | schema-qualified table name |
| Per-tenant domains load | **`Filter{P,G}` — yes** | single `Filter` — **no** |
| casbin/v2 pin | v3.2.0 (targets v2) | v1.1.0 (v1.2.1 → v3) |

**Adopt pckhoi.** Keep Blank-Xu noted as the portable (SQLite/MySQL/MSSQL) fallback.

## Origin

Synthesized from spikes: 086, 087a, 087b, 088, 089 (2026-07-05).
Source harnesses in: sources/086-casbin-hascapability-backing/,
sources/087a-casbin-adapter-pgx-native/, sources/087b-casbin-adapter-sql-stdlib/,
sources/088-casbin-rbac-domains-orgroles/, sources/089-casbin-nethttp-management-api/.
Re-arm the deps: `go get github.com/casbin/casbin/v2 github.com/pckhoi/casbin-pgx-adapter/v3 github.com/Blank-Xu/sql-adapter@v1.1.0`
(harnesses are `//go:build spike_casbin`; run with `-tags spike_casbin`).
