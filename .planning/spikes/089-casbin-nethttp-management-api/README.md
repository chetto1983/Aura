---
spike: 089
name: casbin-nethttp-management-api
type: standard
validates: "Given the RequireCapability middleware shape over Aura's gateway + a live management API (grant/revoke ≡ Casbin policy mutation), when routes enforce + admin mutates policy at runtime, then 403/pass correct + CLI/Settings parity (D-26) and the 086 wildcard/grammar guard is enforced in the management layer"
verdict: VALIDATED
related: [086, 088]
tags: [casbin, net-http, middleware, management-api, phase-casbin]
---

# Spike 089: Casbin through the net/http gateway + management API

## What This Validates

The integration spike. Given Casbin wired through the ACTUAL gateway shape Aura ships
(`internal/agui/auth.go` `RequireCapability` — read the principal off the request
context, ask the authz backend, 403-or-pass), **when** protected routes enforce and an
admin mutates policy at runtime, **then** the middleware 403s/passes correctly, the
runtime grant/revoke takes effect in-process (D-26: CLI + Settings mutate one store),
cross-domain isolation holds end-to-end, and the 086 wildcard/grammar guard is enforced
in the management layer.

## Research

- **Middleware shape:** `requireCasbin(next, e, obj, act)` mirrors the shipped
  `RequireCapability` control flow byte-for-byte — `id := principalFrom(ctx); if id ==
  "" { 403 }; ok, err := enforce(...); if err != nil || !ok { 403 }`. Only the backend
  call changes (`e.Enforce(id, dom, obj, act)` instead of `HasCapability(id, cap)`), so
  the gateway is a drop-in.
- **Domain source:** the request's tenant (here `X-Dept` header; in prod the routed
  identity/department). Reads ONLY server-side context + a routed value, never a
  client-supplied identity header (auth.go's T-24-13 posture).
- **086 landmine reused REAL code:** the management write path calls
  `github.com/chetto1983/aura/internal/identity.ValidateCapabilityName` — Aura's actual
  `^[a-z][a-z0-9._-]{0,63}$` grammar + `*` rejection — before `AddPolicy`. This is the
  production seam, not a reimplementation.

## How to Run

```bash
go run -tags spike_casbin ./.planning/spikes/089-casbin-nethttp-management-api
```
Driven with `net/http/httptest` — real requests through the real middleware. Exit 0 =
VALIDATED.

## What to Expect

8 assertions PASS: no-principal 403, ungranted 403, runtime-grant→200, cross-domain
403, runtime-revoke→403, and three management-API validation cases (`*` rejected, bad
name rejected, valid grant accepted).

## Investigation Trail

1. **Enforcement through the gateway** — an unauthenticated request (no principal) and
   an ungranted principal both 403 through the middleware, exactly as auth.go does.
2. **Runtime grant (D-26)** — `AddGroupingPolicy(u-bob, role-manager, dept-a)` on the
   live enforcer flips bob's next request 403→200 with NO restart. In a single-binary
   appliance there is ONE enforcer, so an admin's CLI or Settings mutation is
   immediately effective in-process — no reload plumbing needed.
3. **Cross-domain isolation end-to-end** — bob-as-manager@dept-a is still 403 for
   dept-b through the gateway (088's isolation holds at the HTTP boundary).
4. **Runtime revoke** — `RemoveGroupingPolicy(...)` flips 200→403 immediately.
5. **086 landmine closed** — `grantPermission` rejects a `*` capability grant and a
   grammar-invalid name (`Bad.Name!`) via the real `identity.ValidateCapabilityName`,
   and accepts a valid `reports.export`. Casbin itself would blindly persist
   `p(role, *, "*", "*")` (a super-user) — the guard MUST live in the management API,
   and reusing Aura's existing validator makes that a one-line call.

## Results

**VALIDATED.** `fails=0` over 8 gateway + management-API assertions.

- **Zero-rework gateway:** the Casbin middleware is the shipped `RequireCapability`
  control flow with a different backend call — `internal/agui` needs no structural
  change (confirms 086's type-level swap at the HTTP layer).
- **Runtime management = D-26 parity:** grant/revoke via `AddGroupingPolicy` /
  `RemoveGroupingPolicy` (or `AddPolicy`/`RemovePolicy`) is immediately live; the CLI
  (`aura identity grant/revoke`) and the admin Settings control both call the same
  enforcer methods. Casbin's Management/RBAC API is the reference D-26 named.
- **The 086 guard is not optional** — the `*`-is-system-managed + name-grammar
  protections live in the management layer and are satisfied by reusing
  `identity.ValidateCapabilityName`.

### Constraints / build notes

- **Watcher only for multi-instance.** Single-binary Aura = one in-process enforcer →
  mutations are instantly live. If Aura ever runs multiple replicas, a Casbin Watcher
  (Postgres LISTEN/NOTIFY, e.g. `casbin/pg-watcher`) is needed to propagate a policy
  change to peers; combined with autosave (087) that is the multi-node story. Not
  needed for the appliance.
- **Domain plumbing:** the middleware must resolve the request's department/tenant
  server-side (routed identity), never trust a client header for the domain in prod —
  the `X-Dept` header here is a spike affordance.
