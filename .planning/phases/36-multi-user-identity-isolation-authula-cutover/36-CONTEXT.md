# Phase 36: Multi-User Identity Isolation + Authula Cutover - Context

**Gathered:** 2026-07-05
**Status:** Ready for planning

<domain>
## Phase Boundary

Owner-scope every user-facing store / API / job to the authenticated principal
(`identityctx.IdentityID(ctx)`, `local` as the CLI/no-principal fallback) and turn the
already-embedded single-user Authula into a real multi-user provider — **identity
isolation only, NO RBAC**. Delivers MUSR-01..06 (+ QUAL Authula DSN).

**In scope:** data/API/job owner-scoping (conversations, approvals, background jobs) with
cross-identity deny; Authula multi-user cutover (provisioning + break-glass + capability-
per-route + no long-lived tokens in URLs); Garage bucket-per-identity; per-identity MCP
**config** + skills-dir **filesystem rooting**; the documents-plane leak fix (spike 085);
runner-lifecycle conversation deletion (MUSR-05); the two-identity live E2E acceptance gate.

**Out of scope (explicit):** the per-identity **full-capability sandbox box** (Phase 37,
SBX-01..05); class-(c) **per-user PIM/WhatsApp sidecar instances** (Phase 37+); a real authz
engine / org-roles (**Casbin** — own spike + phase, PRD-amendment); per-identity **quotas**
(Phase 37/OPS). Snippet/skill **execution** isolation is Phase 37 (the box); Phase 36 does
skills-dir *storage* rooting only.
</domain>

<decisions>
## Implementation Decisions

### Authz model — admin vs user (locked: no RBAC)
- **D-01:** "Admin" = an identity holding **capability grants** on the EXISTING
  `capability_grants` seam via `RequireCapability(...)` → `Identities.HasCapability(ctx,id,cap)`
  (`internal/agui/auth.go`). No Casbin, no go-mizu, no role table in Phase 36. Honors the
  locked PRD "no RBAC / capability_grants" decision.
- **D-02:** Admin-gated surfaces: **Model settings** (`settings.model.write`) and **user/
  identity creation** (existing `identity.create`). **Telegram linking is a normal self-scoped
  USER action** (each user links their own Telegram to their own identity), NOT admin-gated.
- **D-03:** Settings write-routes (`PUT/DELETE /api/settings/{key}`, model config) get
  `RequireCapability(settings.model.write)`; the frontend hides the Settings page / admin
  controls when the capability is absent. `GET /api/settings` may stay read-for-all or gate —
  planner's call.
- **D-04:** `HasCapability(ctx,id,cap)` is an **interface** and every route calls
  `RequireCapability` — so a later Casbin-backed implementation is a **zero-rework swap**.
  This is why Casbin can safely be deferred (see Deferred).

### Isolation surface split (Phase 36 vs 37)
- **D-05:** Class-(c) **per-user PIM/WhatsApp sidecar instances** (each user needs their own
  calendar/whatsapp instance + OAuth/pairing onboarding — spike 084) are **DEFERRED to Phase
  37+**; they pair with the per-identity box (idle-suspend together). Phase 36 ships the
  storage/kernel-enforced core only.
- **D-06:** **Cross-identity deny semantics: 404 on read** (GET/list/search of a foreign
  resource hides existence), **403 on mutate** (delete/archive/resolve on a known-foreign ID).

### Kernel/storage-enforced isolation (the "storage-enforced not app-enforced" non-negotiable)
- **D-07:** **Postgres RLS + app-level `*ForIdentity` (defense-in-depth)** on owner-scoped
  `aura.*` tables. RLS policy `USING (owner_id = current_setting('app.current_identity'))`,
  pgx sets `SET LOCAL app.current_identity` per tx; keep the additive `*ForIdentity` query
  methods too. Kernel backstops a forgotten filter. (Rejected go-saas/Go-Multitenancy — single-
  DB SaaS mismatch vs Aura's 3 substrates.)
- **D-08:** Three planes each get a kernel/storage-enforced mechanism: **Postgres = RLS**,
  **Neo4j = fail-closed `EXISTS{}` ownership filter**, **Garage = bucket-per-identity + scoped
  key** (grants are per-bucket NOT per-prefix, F-007).

### Documents plane (spike 085 — the leak fix that IS the "missing" work)
- **D-09:** The `internal/documents` graph pipeline is **identity-blind today** (proven leak
  through the production `Searcher`). Fix mirrors the shipped memory pattern (`9a4ca594`):
  1. Add `IdentityID` to `documents.IngestRequest` → `ExtractedDocument`; on upsert atomically
     `MERGE (:User {identifier})` + `MERGE (:User)-[:HAS_DOCUMENT]->(:Document)`
     (`indexer.go` `chunkUpsertQuery`; `WITH` required between `MERGE` and `MATCH`).
  2. Add `IdentityID` to `documents.SearchRequest`; add
     `EXISTS { (:User {identifier:$id})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }`
     to **every** retrieval query — `sparseSearchQuery`, `docScopedVectorSeedQuery`, the
     two-stage `Retrieve` seeds, the graphrag expand. **Empty identity fails closed** (the
     `chunk_text` fulltext index can't be node-restricted → filter after the yield).
  3. Thread `identityctx` into the `document_search` tool call + ingest path.
- **D-10:** Do NOT rely on the Postgres `CatalogService` metadata scoping or 077 catalog-
  injection for isolation — both are bypassable; isolation is graph-side + fail-closed.

### Cutover & migration
- **D-11:** **Operator stays `local`** — the operator's Authula user continues to resolve to
  the `local` identity (as `IdentityLinker` binds today); existing data untouched. NEW Authula
  users get fresh identity UUIDs, fully isolated. Lowest migration risk.
- **D-12:** **Documents-plane backfill:** attach `(:User {local})-[:HAS_DOCUMENT]->` edges to
  ALL existing docs from the Postgres catalog's identity→document_id map **before** flipping
  retrieval to fail-closed scoping (else operator's existing docs go invisible).
- **D-13:** **Rollout is config-flag gated + reversible** (`AURA_MUSR_ISOLATION` via
  `aura.settings`/env): deploy scoping code (flag off) → run backfill migrations → verify
  operator still sees own data → flip flag on. `golang-migrate` handles ordering. (Rejected GO
  Feature Flag service — overkill for one appliance rollout flag; OpenFeature only if dynamic
  flags ever needed.)

### Provisioning & first-login (admin-only; no self-signup; no SMTP)
- **D-14:** **Eager, idempotent provisioning saga** at admin-create: identity row + default
  capability grants (`agent.run` + self-Telegram; NOT admin caps) + Garage bucket & scoped key
  + per-identity MCP-config & skills dirs + empty `Agent.md`. Idempotent/resumable steps
  (`INSERT ON CONFLICT` / `MERGE` / `garage key create`), lightweight **in-process** saga
  (rejected Temporal + Kafka/CDC saga libs — wrong shape for a single-binary appliance).
- **D-15:** **First-login = admin-set initial password + forced change** on first login +
  TOTP enrollment, via the embedded Authula (no SMTP). (Kratos admin-invite/recovery-link is
  the reference pattern but Authula is already embedded — don't swap.)
- **D-16:** **Break-glass = CLI-minted recovery** on the host (short-lived reset token / admin
  credential reset). Host access = proof of ownership; no standing bypass in the running
  server. Shipped FIRST per MUSR-06.

### Background jobs (MUSR-03/04)
- **D-17:** Jobs use **random unguessable IDs bound to session/actor**; **default TTL = 1h**
  (env-overridable); on expiry **record status + terminate the process group**.
- **D-18:** **Poll/kill authority = owner session/actor + an admin capability** (cross-session
  operational recovery), per MUSR-03's "explicit admin capability".

### MCP isolation classes (per-identity `MountForIdentity` resolves a mix)
- **D-19:** Class-(a) **stdio/stateless local** → run inside the identity's context (box in 37);
  isolated for free. Class-(b) **agent-memory shared graph** → ONE **globally-managed, always-on**
  sidecar (`:8091`); only **admin** governs its enable/trust; every call carries the mandatory
  `user_identifier` scope key (fork `9a4ca594` `:User`-ownership). Users can't toggle shared
  infra. Class-(d) **documents graph** → scoping BUILT here (D-09). Class-(c) deferred (D-05).
- **D-20:** Per-identity MCP config = `~/.aura/mcp/{id}/servers.json` (shared catalog read-only
  + per-identity enable/trust), identity-keyed `mcp_audit`. Per-identity enable/trust applies
  to class-(a) (+ class-(c) later), NOT the shared class-(b) server.

### Per-identity skills
- **D-21:** `$AURA_SKILLS_DIR/{id}/` + `~/.aura/pyscripts/{id}/` + identity-keyed
  `skill_audit`/approval; built-ins shared read-only. `newSkillToolForIdentity(ctx)`. Additive
  `*ForIdentity` methods, `local` fallback. (Snippet **execution** in the box = Phase 37.)

### Runner lifecycle & session keying
- **D-22:** MUSR-05: all conversation deletion (AG-UI, Telegram `/clear`, CLI) routes through a
  runner lifecycle method that cancels active work, expires pending pauses, evicts session
  tools, handles background jobs, THEN deletes persistence.
- **D-23:** **Key in-memory session/pause/tool state by `(identity, session)`** — carry
  `identityctx` in the session struct; never key a shared map by session-id alone (leak/collision
  risk under concurrent multi-user).

### Telegram multi-user routing
- **D-24:** Generalize `IdentityLinker.LinkOperator` (single-operator today) to link ANY
  provisioned user's chat-id → their identity. **Unknown chat-id → reject + point to web
  linking** (no agent runs for unlinked users). Linking is **web-initiated**: Settings→Telegram
  mints a one-time code (existing `POST /api/settings/telegram/link`) → user sends it to the bot
  → chat-id binds to their identity. Reuse `telebot.v4` — no bot-framework swap.

### CLI / `local`
- **D-25:** `local` is **seeded at bootstrap with admin caps** (`settings.model.write` +
  `identity.create` + `governance.write`). The **CLI always runs as `local`** (host access =
  ownership); non-operators are web/Telegram identities only — no CLI. (No `--as-identity`
  impersonation in 36.)

### Capability grant management
- **D-26:** Admin grants/revokes caps via **CLI (`aura identity grant/revoke <id> <cap>`,
  audited) AND an admin-gated Settings-page control**. Casbin's management API is the reference
  for the later phase.

### User deletion (de-provisioning)
- **D-27:** **Soft-delete → purge after grace.** Deactivate immediately (block login, kill
  sessions, terminate jobs), retain for a grace window, then a scheduled purge runs the
  **de-provisioning saga** (conversations, docs + `:User` edges, Garage bucket, memory node/
  edges, MCP/skills dirs, grants, Authula user) — lightweight in-process, journaled, resumable,
  symmetric to the provisioning saga (D-14).

### Admin audit visibility
- **D-28:** Identity-key the audit tables (`tool_invocations`/`mcp_audit`/`skill_audit`) AND
  ship a **full admin audit UI now** — an admin-gated web view for per-user activity.

### Acceptance gate — two-identity live E2E (MUSR-01)
- **D-29:** **Full live stack gates in CI** — add **Garage + Authula** to the CI stack
  (alongside Postgres + Neo4j); the two-identity cross-deny E2E runs live and GATES on Linux CI
  under no-skip-as-green (skip-helpers `t.Fatal` under `$CI`); local may skip on RAM. **Reuse
  Aura's existing build-tag live-stack harness** — NOT testcontainers (which would fork the
  established CI model).

### Claude's Discretion
- Whether `GET /api/settings` is admin-gated or read-for-all (D-03).
- Exact capability name (`settings.model.write` vs reusing `governance.write`) — planner's call.
- Saga journal storage shape (new `aura.*` table vs outbox) for D-14/D-27.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §MUSR (MUSR-01..06 + QUAL Authula DSN) — the locked requirements.
- `.planning/ROADMAP.md` — Phase 36 goal + success criteria (and Phase 37 SBX boundary).

### Spike findings (the implementation blueprint — MUST read)
- `.claude/skills/spike-findings-Aura/references/multiuser-per-identity-isolation.md` — the full
  Phase 36–37 blueprint: Garage bucket-per-identity, the 4 MCP isolation classes, the documents
  plane, agent-sandbox contract. **The single most important ref.**
- `.planning/spikes/085-document-ingest-tenancy/README.md` + `main.go` — the documents-plane
  leak reproduced through the production `Searcher` + the `:User`-ownership fix (D-09/D-12).
- `.claude/skills/spike-findings-Aura/SKILL.md` §Requirements (Multi-user) — non-negotiables.
- Spike sources `082`–`085` under `.claude/skills/spike-findings-Aura/sources/` — live-run evidence.

### Code seams (owner-scoping + auth touch-points)
- `internal/identityctx/identityctx.go` — `IdentityID(ctx)`, the one principal all planes key on.
- `internal/agui/auth.go` — `RequireCapability` + `HasCapability` interface (the admin seam).
- `cmd/aura/serve_webui.go` — capability mounts (`governance.write`, `identity.create`, `agent.run`).
- `cmd/aura/serve_auth.go` + `internal/webauth/authula.go` + `internal/webauth/identity_link.go`
  — Authula construction (schema-isolated, migration 0019) + `IdentityLinker`.
- `internal/agui/settings_api.go` + `web/src/settings/*` — the settings routes + panels to gate.
- `internal/documents/{indexer,search,types}.go` — the identity-blind pipeline to scope (D-09).
- `internal/agent/tools/shell_bg.go` — background jobs (TTL, IDs, poll/kill — D-17/D-18).
- `internal/channels/telegram/{bot_dispatch,agui_subscriber}.go` — Telegram routing (D-24).

### Library-fit verdicts (researched; documented so the planner doesn't re-litigate)
- **Casbin** `github.com/apache/casbin/v2` (Apache-2.0, stable) — the right engine IF/when
  org-roles are needed → deferred to its own spike + phase (PRD-amendment). Reached via the
  `HasCapability` interface, zero rework.
- **Rejected:** go-mizu/mizu/middlewares/rbac (framework-coupled, pre-v1); go-saas/saas +
  LiamDotPro/Go-Multitenancy (single-DB SaaS mismatch); Ory Kratos (Authula already embedded);
  Temporal + microservice saga libs (wrong shape for single-binary); GO Feature Flag service
  (overkill for one rollout flag); testcontainers-go (forks Aura's live-stack CI model).
- **Adopted patterns:** Postgres RLS (kernel-enforced backstop); `golang.org/x/time/rate`
  (noted for the deferred quota work); lightweight in-process idempotent saga.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `capability_grants` + `RequireCapability`/`HasCapability` (interface) — the entire admin/user
  mechanism already exists and is dormant/active; add `settings.model.write`, seed `local`.
- `IdentityLinker` (`ResolveIdentityID`, `LinkOperator`) — generalize for multi-user Telegram.
- Memory MCP `:User`-ownership scoping (`9a4ca594`, shipped) — the exact template for the
  documents-plane fix and the isolation shape.
- Embedded Authula (migration 0019, schema-isolated) — provisioning + first-login + TOTP.
- The build-tag live-stack integration harness (`db_integration`/`neo4j_integration` + composed
  DSNs + `t.Fatal`-under-`$CI`) — extend for the two-identity E2E.

### Established Patterns
- Additive `*ForIdentity` methods with `local` fallback (same shape as the existing conversation/
  approval owner-scoping) — the convention for every store touched.
- Deferred-tool pattern + identity-keyed audit tables (`mcp_audit`, `skill_audit`,
  `tool_invocations`).
- `SET LOCAL` per pgx tx for RLS session var; fail-closed graph filters for Neo4j.

### Integration Points
- Provisioning/de-provisioning sagas span Postgres (`aura.*`) + Neo4j (`:User` edges) + Garage
  (bucket/key) + filesystem (`~/.aura/{mcp,agents,pyscripts}/{id}`, `$AURA_SKILLS_DIR/{id}`) +
  Authula user — one saga, idempotent steps.
- The `AURA_MUSR_ISOLATION` config flag gates the fail-closed flip across all retrieval/query paths.
</code_context>

<specifics>
## Specific Ideas

- Operator explicitly wants **battle-tested industrial libraries over bespoke** — every design
  choice above was researched against 2026 options; the verdicts (and the honest "existing seam
  beats the library" calls) are recorded in `<canonical_refs>`.
- Operator surfaced `go-mizu/mizu/middlewares/rbac` and **Apache Casbin** as authz candidates;
  the concrete driver is the **commercial DGX-Spark SMB bundle** eventually wanting real
  org-roles (manager/employee/viewer, per-department) — hence Casbin is a *deferred forward bet*,
  not a Phase-36 need.
- Admin/user distinction is deliberately minimal: "the main difference is the settings page —
  only admin changes model settings + creates users; Telegram is per-user."
</specifics>

<deferred>
## Deferred Ideas

- **Casbin authz engine + org-roles** — its own `/gsd-spike` (ground-truth Casbin over Aura's
  `net/http` gateway + pgx Postgres adapter + backing `HasCapability` + RBAC-with-domains for
  tenant/department) THEN a dedicated phase. **Requires a PRD-amendment commit** reopening the
  locked "no RBAC" decision before implementation. Lands via the `HasCapability` interface →
  zero rework to Phase 36.
- **Class-(c) per-user PIM/WhatsApp sidecar instances** — Phase 37+ (pair with the per-identity
  box; N × {calendar, whatsapp} instances + N OAuth/pairing onboardings).
- **Per-identity quotas/limits** (concurrent jobs, disk, object-store, LLM spend) — Phase 37/OPS;
  tools noted: `golang.org/x/time/rate` + counters.
- **CLI `--as-identity` impersonation** — not in 36; revisit with the admin/governance work.
- **Rich web audit view beyond per-user activity** — the deeper governance/audit surface can
  grow with the Casbin phase.

### Reviewed Todos (not folded)
None — no pending todos matched Phase 36 (`todo.match-phase` returned 0).
</deferred>

---

*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Context gathered: 2026-07-05*
