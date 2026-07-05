# PRD-Amendment #78 — Casbin authz engine + org-roles (DRAFT — needs operator ratification)

**Status:** DRAFT — reopens a LOCKED decision, do NOT land in `prd.md` until the operator ratifies.
**Author date:** 2026-07-05
**Driver:** Commercial DGX-Spark SMB bundle eventually needs real org-roles (manager/employee/viewer, per-department). Grounded live in spikes 086–089 (Session-23).
**Gate:** CLAUDE.md PRD-first — this amendment commit must land BEFORE any Casbin implementation commit (same `git log` ordering gate as #44/#62/#63/#64).

---

## Why this is a draft and not a direct edit

This amendment **reopens the locked "no RBAC" decision** (REQUIREMENTS.md line 8(b),
RBAC-01, RBAC-02). Reopening a LOCKED decision is exactly the case CLAUDE.md
§PRD-first reserves for an explicit amendment commit. The operator must ratify the
scope (what reopens, what stays deferred) before it touches `prd.md`. Everything below
is paste-ready once ratified.

---

## Part A — Seam verification (the "zero-rework swap" premise, confirmed in code)

The whole deferral rests on one claim: Casbin can back `HasCapability` with zero rework
to Phase 36. **Verified against HEAD, confirmed true.**

| Layer | File / symbol | State |
|---|---|---|
| Consumer-side interface | `internal/agui/auth.go:35` `identityChecker { GetIdentityByID; HasCapability(ctx, id, capability) (bool, error) }` | ✅ Narrow, consumer-declared (accept-interfaces). agui does NOT import `internal/identity`. |
| The gateway | `internal/agui/auth.go:275` `RequireCapability(next, deps, capability)` → `deps.Identities.HasCapability(...)` | ✅ Sole authz decision point. Every gated route flows through it. |
| Concrete impl | `internal/identity/store.go:128` `(*Store).HasCapability` → SQL wildcard-or-exact (`sqlc.HasCapability`) | ✅ Single implementation; `*` wildcard seeded on `local` by migration 0004. |
| Composition-root bridge | `cmd/aura/serve_auth.go:37-51` `identityCheckerAdapter` passes `HasCapability` straight through | ✅ The ONE place to swap the backing at wiring time. |

**Swap mechanics (proven by spike 086, 15/15 vs SQL oracle, byte-identical):**
- A Casbin-backed `HasCapability(ctx, id, cap)` becomes an alternative implementation of
  `agui.identityChecker`. Swap happens at `identityCheckerAdapter` (or a new adapter) at
  the composition root. `RequireCapability` and every call site are **untouched**.
- **Signature nuance (be honest):** flat-capability parity is *signature-identical*
  (086). Org-roles adds a **domain** argument — the additive migration is
  `HasCapability(id, cap)` → `HasCapability(id, dom, cap)`. Phase-36 call sites pass a
  default domain (`"*"`), so the change is additive at the interface, not a rewrite.
- **Phase 36 keeps the seam clean by contract:** `36-01-PLAN.md:31` acceptance =
  "No Casbin / role table imported or wired"; `36-10-PLAN.md:57` = "seam untouched —
  Casbin-swap-ready". Preserving that is a Phase-36 acceptance criterion, not a nice-to-have.

**Conclusion:** The forward bet is safe. Casbin can be integrated LATER at the adapter
seam with zero rework to Phase 36. It must NOT be integrated INTO Phase 36 (locked no-RBAC).

---

## Part B — The prd.md amendment block (paste-ready once ratified)

> Insert near the RBAC/multi-user requirements section (companion to Amendment #64, the
> Phase-28 authz-boundary amendment, which #78 is the deliberate inverse of).

```markdown
> **▶ Amendment #78 (Authz engine + org-roles — the locked "no RBAC" decision is
> partially reopened for a dedicated post-Phase-36 phase, 2026-07-05) — BLOCKING for the
> Casbin phase only; lands before any Casbin implementation commit (CLAUDE.md PRD-first;
> the `git log` ordering is the gate, same pattern as #44/#62/#63/#64).** Amendment #64
> held `capability_grants` as the ONLY authz model and deferred real RBAC post-v1.0.0.
> This amendment reopens RBAC-01 (real role/permission model) — and ONLY RBAC-01 — for a
> dedicated phase, driven by the commercial DGX-Spark SMB bundle's need for org-roles
> (manager/employee/viewer, per-department). Grounded live end-to-end in spikes 086–089.
> The reopening is **scoped and additive, NOT a rewrite of the authz surface**:
> - **What reopens:** RBAC-01 (role/permission model, admin-vs-user + org-roles). A new
>   phase replaces the SQL backing of `HasCapability` with an Apache Casbin enforcer and
>   grows it into RBAC-with-domains for per-department org-roles.
> - **What STAYS deferred:** RBAC-02 (OAuth multi-provider / multi-tenant SaaS login) —
>   Authula stays the embedded provider (#64). Per-identity **quotas** (Phase 37/OPS).
>   The Phase-36 **identity-isolation** planes (RLS / Neo4j EXISTS / Garage bucket, RBAC-03
>   as amended) are unchanged — Casbin governs *capabilities/roles*, NOT data-plane isolation.
> - **Zero-rework swap (proven):** spike 086 showed casbin/v2 backs `HasCapability`
>   byte-identically (15/15 vs the SQL oracle). The `internal/agui` `identityChecker`
>   interface + `RequireCapability` gateway are UNCHANGED; only the composition-root
>   backing swaps (`cmd/aura/serve_auth.go` `identityCheckerAdapter`). The org-roles
>   migration is additive: `HasCapability(id, cap)` → `HasCapability(id, dom, cap)`,
>   Phase-36 call sites passing domain `"*"`.
> - **Locked implementation choices (spikes 086–089):** engine =
>   `github.com/casbin/casbin/v2` **v2.135.0** (NOT v3 — the v3 pgx-native adapter
>   ecosystem is weaker and GORM-coupled; operator directive "most stable version fit");
>   adapter = `github.com/pckhoi/casbin-pgx-adapter/v3` **v3.2.0** (native pgx, shares
>   Aura's pool via `WithConnectionPool` + `WithSchema("aura")` + `WithSkipTableCreate`);
>   the `aura.casbin_rule` table is owned by **golang-migrate** (adapter runs ZERO DDL),
>   sqlc may type it; ONE **RBAC-with-domains superset model** serves both flat caps and
>   org-roles; the management API (runtime grant/revoke) MUST reuse
>   `internal/identity.ValidateCapabilityName` (`*`-is-system-managed + the
>   `^[a-z][a-z0-9._-]{0,63}$` grammar live in the API, not the Casbin model).
> - **Invariants preserved:** no-op under `dev`/`local_trusted` (REQUIREMENTS locked (e));
>   the seeded `local` identity keeps its `*` wildcard (`p(local,"*","*","*")`); Watcher
>   only if/when multi-instance. Blueprint:
>   `.claude/skills/spike-findings-Aura/references/casbin-authz.md`; ground-truth:
>   spikes 086–089 + `.planning/spikes/MANIFEST.md`.
```

---

## Part C — Companion edits (land in the SAME amendment commit)

### C1 — `.planning/REQUIREMENTS.md`

- **Line 8(b)** — annotate the locked decision so the reopening is discoverable:
  > (b) multi-user = identity isolation only, **NO RBAC/OAuth/roles** *(RBAC-01 partially
  > reopened for a dedicated post-Phase-36 org-roles phase — see prd.md Amendment #78;
  > RBAC-02 OAuth stays deferred)*.

- **RBAC-01** (line 125) — amend, mirroring the RBAC-03 amendment style:
  > **RBAC-01**: Real role/permission model (admin vs user) — ~~explicitly OUT of v2.0.0~~
  > **AMENDED 2026-07-05 (Amendment #78):** reopened for a dedicated post-Phase-36 phase
  > (Casbin org-roles for the DGX-Spark SMB bundle). Lands via the `HasCapability`
  > interface (zero rework, spike 086). RBAC-02 (OAuth) remains deferred.

- **Line 140 table row** — update "RBAC / roles / OAuth multi-tenant | Locked decision (c)…"
  to note org-roles reopened (#78), OAuth still locked.

### C2 — `.planning/DECISIONS.md` §6 (Deferred v2) — add a registry row / promote the Casbin bet

  > | D-CASBIN | **Authz engine + org-roles** | 🔄 reopened | Casbin phase | **Amendment #78
  > (2026-07-05):** RBAC-01 reopened for a dedicated post-Phase-36 phase; engine casbin/v2
  > v2.135.0 + pckhoi adapter v3.2.0, migrate-owned `aura.casbin_rule`, RBAC-with-domains
  > superset. Grounded spikes 086–089. Lands via `HasCapability` interface, zero rework. |

### C3 — `.planning/ROADMAP.md` — schedule the phase

Add after the honest-10/10 closeout (exact slot = roadmap owner's call; it is post-v2.0.0
and commercial-bundle-driven, so likely a v2.1 milestone item):

  > #### Phase 42 (v2.1 candidate): Authz Engine + Org-Roles (Casbin)
  > **Requirements:** RBAC-01 (reopened, #78)
  > Swap `HasCapability` SQL backing → casbin/v2 enforcer (byte-identical flat caps),
  > grow to RBAC-with-domains (per-department manager/employee/viewer), migrate-owned
  > `aura.casbin_rule`, net/http management API (runtime grant/revoke). Blueprint:
  > `references/casbin-authz.md`; ground-truth spikes 086–089.

---

## Part D — What lands in the Casbin phase (NOT in Phase 36)

From `references/casbin-authz.md` + spikes 086–089 — the implementation checklist for
the future phase (recorded here so the amendment is self-contained):

1. **Migration** — `aura.casbin_rule` table (mirror pckhoi's shape exactly), golang-migrate
   owned, `WithSkipTableCreate` on the adapter. sqlc may type it.
2. **Enforcer wiring** — casbin/v2 `NewEnforcer` with the RBAC-with-domains model +
   pckhoi adapter (`WithConnectionPool` sharing Aura's pgxpool, `WithSchema("aura")`).
   Per-tenant filtered load (trim trailing empty `Filter` columns — pckhoi trailing-`AND`
   builder bug, a spike sharp-edge).
3. **Backing swap** — new `casbinChecker` implementing `agui.identityChecker.HasCapability`;
   wire at `cmd/aura/serve_auth.go`. Seed `p(local,"*","*","*")` = wildcard parity.
4. **Additive domain param** — `HasCapability(id, cap)` → `HasCapability(id, dom, cap)`;
   Phase-36 sites pass `"*"`. Org-role sites pass the department domain.
5. **Management API** — net/http grant/revoke (089), reusing `identity.ValidateCapabilityName`;
   `*`/grammar guards in the API, not the model.
6. **Tests** — the 086 SQL-oracle parity suite (byte-identical), 088 cross-domain isolation,
   089 runtime grant/revoke live; live-stack `db_integration` harness (Aura's model, not testcontainers).

---

## Part E — Ratification checklist (operator sign-off before commit)

- [ ] Confirm scope: reopen **RBAC-01 only**; RBAC-02 (OAuth) stays deferred. (Y/N)
- [ ] Confirm the phase is **post-Phase-36** (v2.1 candidate), not pulled into 36. (Y/N)
- [ ] Confirm engine/adapter pins: casbin/v2 v2.135.0 + pckhoi v3.2.0. (Y/N)
- [ ] Confirm roadmap slot (Phase 42 / v2.1 vs elsewhere). (Y/N)
- [ ] On sign-off: paste Part B into `prd.md`, apply Part C edits, commit as
      `docs(prd): amend #78 — reopen RBAC-01 for dedicated Casbin org-roles phase`
      (PRD-amendment commit; must precede any Casbin code commit).
