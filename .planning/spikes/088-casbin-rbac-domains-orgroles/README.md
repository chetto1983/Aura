---
spike: 088
name: casbin-rbac-domains-orgroles
type: standard
validates: "Given manager/employee/viewer roles per department (domains) + role hierarchy, when a user is manager@deptA + viewer@deptB, then enforce isolates cross-domain (no manager power leaks into deptB) and inherits the hierarchy"
verdict: VALIDATED
related: [086, 087a, 089]
tags: [casbin, rbac, domains, org-roles, tenant, phase-casbin]
---

# Spike 088: RBAC-with-domains org-roles

## What This Validates

The VALUE spike — the reason to adopt Casbin at all. Given manager/employee/viewer
roles per department (Casbin *domains*) + a per-domain role hierarchy, **when** a user
is manager in dept-A but only viewer in dept-B, **then** enforce isolates the two
(her manager power does NOT leak into dept-B) and inherits the hierarchy within each
department. This is the SMB org-roles the commercial DGX-Spark bundle wants.

## Research

- **Model:** the canonical Casbin RBAC-with-domains (`r/p = sub, dom, obj, act`;
  `g = _, _, _` — user→role PER domain), extended so each of `dom`/`obj`/`act` allows
  an explicit `"*"` wildcard OR exact match. That extension makes ONE model a strict
  superset of BOTH Phase 36's flat capabilities (086) AND org-roles: a flat global
  cap is `p(id, "*", "*", "*")`; an org-role policy is `p(role, dept, resource, verb)`.
- **The isolation mechanism** is `g(r.sub, p.sub, r.dom)` — role membership and role
  hierarchy are looked up WITH the request's domain, so a `g(u-alice, role-manager,
  dept-a)` grant makes alice a manager only in dept-a. This is what the whole
  org-roles story rests on, so the spike proves it directly.

## How to Run

```bash
go run -tags spike_casbin ./.planning/spikes/088-casbin-rbac-domains-orgroles
```
In-memory enforcer (pure enforce semantics; persistence of this exact model is
already proven live in 087a/087b). Exit 0 = VALIDATED.

## What to Expect

15 enforce cases PASS + a `GetImplicitRolesForUser` probe showing alice has 3
effective roles in dept-a (manager→employee→viewer) and 1 in dept-b (viewer only).

## Investigation Trail

1. **Hierarchy within a domain** — seeded `g(role-manager, role-employee, dept)` +
   `g(role-employee, role-viewer, dept)` for each department. alice-as-manager@A
   then writes budget (direct), writes tickets (via employee), and reads budget (via
   viewer) — the full downward inheritance.
2. **Cross-domain isolation** — alice is `role-manager` in dept-a but only
   `role-viewer` in dept-b. Enforce denies her budget-write and ticket-write in
   dept-b: the manager role is scoped to dept-a and does not leak. This is the core
   result.
3. **One-way hierarchy** — bob-as-employee@A gets employee+viewer perms but NOT
   manager (cannot write budget). Inheritance flows manager→employee→viewer, never up.
4. **No-role isolation** — bob has no role in dept-b → denied everything there.
5. **086 parity in the same model** — `p(local, "*", "*", "*")` reproduces the flat
   capability_grants `*` wildcard: `local` is allowed in any domain, on any obj/act.
6. **Domain-scoped implicit roles** — `GetImplicitRolesForUser(alice, dept-a)` = 3
   roles; `(alice, dept-b)` = 1. Empirical proof the hierarchy is per-domain.

## Results

**VALIDATED.** `fails=0` over 15 enforce cases + the hierarchy-scope probe.

- **Per-department manager/employee/viewer with hierarchy** works exactly as the SMB
  bundle needs.
- **Cross-domain isolation is real** — the single most important property (no role
  leak between departments), proven by alice manager@A / viewer@B.
- **One unified model** serves both Phase 36's flat capabilities (via the `*`
  wildcard, 086 parity) and the org-roles domains — so the eventual migration is
  additive: keep issuing flat `p(id,"*",cap,"*")`-style grants for existing caps,
  start issuing `(role, dept, resource, verb)` for org-roles, no model fork.

### Migration path to Aura (design note)

- 086's `HasCapability(id, cap)` becomes `HasCapability(id, dom, cap)` where Phase 36
  passes a constant default domain (e.g. `"global"`) and org-roles pass the real
  department. The interface gains one argument; the matcher already handles both.
- Existing `capability_grants` rows map to `p(id, "*", capability, "*")` at cutover
  (or `p(role, dept, ...)` once departments exist). The 087 backfill is a straight
  INSERT into `casbin_rule`.

### Constraint

- Cross-domain super-admin: a truly global role assignment must be expressed as a
  DIRECT `p(id, "*", "*", "*")` (self-link resolves in every domain), not as a role
  granted in one domain — a role grant is always domain-scoped (which is the point).
