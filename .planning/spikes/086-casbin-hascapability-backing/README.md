---
spike: 086
name: casbin-hascapability-backing
type: standard
validates: "Given seeded `local` (`*` wildcard) + a user with flat grants, when a Casbin enforcer backs HasCapability(ctx,id,cap), then it returns byte-identical allow/deny to capability_grants.sql (wildcard match-all + exact) with callers unchanged"
verdict: VALIDATED
related: [081, 088, 089]
tags: [casbin, authz, capability, hascapability, phase-casbin, deferred]
---

# Spike 086: Casbin backs HasCapability (zero-rework swap)

## What This Validates

Given the seeded `local` identity (`*` wildcard, migration 0004) and a normal user
with a single flat grant, **when** an Apache Casbin (`github.com/casbin/casbin/v2`)
enforcer backs the `HasCapability(ctx, id, cap) (bool, error)` method the whole
gateway keys on (`internal/agui/auth.go` `RequireCapability` → the unexported
`identityChecker`), **then** it returns byte-identical allow/deny to
`capability_grants.sql` — the `(capability = '*' OR capability = $2)` EXISTS —
with the callers unchanged.

This is the **kill-risk** for the deferred Casbin phase: Phase 36 D-04 defers Casbin
on the promise that `HasCapability` is *already* an interface, so a Casbin-backed
impl is a "zero-rework swap." If Casbin can't reproduce the exact flat semantics
behind the identical signature, that promise is void and Phase 36's admin model
would have to change.

## Research

- **Engine:** `github.com/casbin/casbin/v2 v2.135.0` (Apache-2.0, mature). Pulls
  `casbin/govaluate v1.3.0` + `bmatcuk/doublestar/v4` transitively. No CGO.
- **Model choice:** RBAC (`g` present) rather than plain ACL, because the org-roles
  forward bet (088) needs `g` and Casbin's default role manager treats `g(x, x)` as
  **true** (every subject links to itself) — so flat `p(id, cap)` policies enforce
  correctly *today* with the SAME matcher that will carry roles *tomorrow*. Zero
  matcher rework between the flat phase and the roles phase.
- **Wildcard is an explicit disjunct, NOT keyMatch/regexMatch:**
  `m = g(r.sub, p.sub) && (p.obj == "*" || r.obj == p.obj)`. Using `keyMatch` would
  make `settings.*` or a literal `.`/`*` behave as a glob — the real system has ONLY
  the full `*`, no hierarchical wildcard, so `==` keeps cap-name dots literal.
- Oracle = the shipped SQL logic itself (a 4-line deterministic EXISTS). No live DB
  is needed to establish ground truth for a pure-function equivalence probe (spike
  CONVENTIONS: "binary yes/no questions" verify by fact, not by UI).

## How to Run

```bash
go run -tags spike_casbin ./.planning/spikes/086-casbin-hascapability-backing
```
Exit 0 = VALIDATED (zero mismatches), exit 1 = a divergence from the SQL oracle.
Build-tag-gated (`//go:build spike_casbin`) per CONVENTIONS — `go get` casbin live,
`git checkout go.mod go.sum` at session end; the harness stays out of `go build ./...`.

## What to Expect

15 equivalence cases (Casbin vs oracle) all `MATCH`, then 3 superset assertions:
role inheritance grants a new cap, the flat grant still works, and an unrelated user
is unaffected. `mismatches=0`.

## Investigation Trail

1. **First cut** — modelled the flat grants as `p(id, cap)` in an RBAC model and
   asked whether Casbin's `g(r.sub, p.sub)` would even fire for a subject with no
   role edges. Confirmed from the run: it does — the default role manager self-links
   every subject, so direct policies enforce without any `g` line added. This is the
   linchpin that lets one model serve both the flat phase and the roles phase.
2. **Wildcard fidelity** — verified the seeded `*` matches EVERY requested cap
   including never-granted and the literal `"*"`, exactly as the SQL `capability='*'`
   disjunct does.
3. **No-glob discipline** — probed sibling (`settings.model.read`), prefix
   (`settings.model`), suffix (`settings.model.write.extra`) and case
   (`SETTINGS.MODEL.WRITE`) against a user granted `settings.model.write`. ALL deny —
   proving `==` introduces no accidental hierarchy the flat table never had.
4. **Deny surface** — ungranted user, unknown identity, empty id, empty cap all deny
   in both backends.
5. **Superset preview** — added `g(u-alice, role-admin)` + `p(role-admin,
   governance.write)`; alice inherits `governance.write` via the role while her flat
   grant is intact and `u-bob` is unaffected — the model is a strict superset, no
   flat-path rework.

## Results

**VALIDATED.** `mismatches=0` over 15 equivalence cases + 3 superset assertions.

- The swap is zero-rework at the **type level**: both `*sqlOracle` and
  `*casbinChecker` satisfy the same `consumerChecker` interface (compile-time
  `var _ consumerChecker = …` assertions), so the composition root binds a different
  concrete type and nothing else in `internal/agui` moves.
- The swap is zero-rework at the **behaviour level**: every wildcard, exact, deny,
  and edge case agrees with the shipped SQL.
- The chosen RBAC model is a **strict superset** — org-roles (088) add `g`/role
  policies with no matcher change.

### Surprises / landmines (carry to 088/089)

- **`*` is NOT system-managed by the model.** Casbin will happily enforce a
  `p(u-bob, "*")` policy, making bob a super-user. The real store's
  `ErrWildcardManaged` guard (reject granting `*`) lives in the **management layer**,
  not the model — spike 089's grant/revoke API MUST replicate `ValidateCapabilityName`
  (the `^[a-z][a-z0-9._-]{0,63}$` grammar + `*` rejection) before `AddPolicy`, or the
  grammar/wildcard protections silently vanish on cutover.
- **Casbin does no cap-name validation.** Name grammar stays a management-layer
  concern; the enforcer treats any string as an opaque object.
- **`Enforce` returns `(bool, error)`** — identical shape to `HasCapability`; a matcher
  or model error surfaces as the error leg, so the gateway's existing
  `if err != nil || !ok { 403 }` needs no change.
