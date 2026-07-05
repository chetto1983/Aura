# Phase 36: Multi-User Identity Isolation + Authula Cutover — Research

**Researched:** 2026-07-05
**Domain:** Multi-substrate per-identity isolation (Postgres RLS + Neo4j fail-closed + Garage bucket-per-identity), cross-store provisioning saga, Authula multi-user cutover, owner-scoped stores/jobs, Go monorepo (Go 1.26.4)
**Confidence:** HIGH (design is LOCKED in CONTEXT.md; every seam grounded in real files read this session; only Garage Admin API JSON field names carry residual MEDIUM)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** "Admin" = an identity holding capability grants on the EXISTING `capability_grants` seam via `RequireCapability(...)` → `Identities.HasCapability(ctx,id,cap)` (`internal/agui/auth.go`). No Casbin, no go-mizu, no role table in Phase 36.
- **D-02:** Admin-gated surfaces: Model settings (`settings.model.write`) and user/identity creation (existing `identity.create`). Telegram linking is a normal self-scoped USER action, NOT admin-gated.
- **D-03:** Settings write-routes (`PUT/DELETE /api/settings/{key}`, model config) get `RequireCapability(settings.model.write)`; frontend hides Settings page when the capability is absent. `GET /api/settings` may stay read-for-all or gate — planner's call.
- **D-04:** `HasCapability(ctx,id,cap)` is an interface; every route calls `RequireCapability` — later Casbin-backed impl is a zero-rework swap.
- **D-05:** Class-(c) per-user PIM/WhatsApp sidecar instances DEFERRED to Phase 37+.
- **D-06:** Cross-identity deny: **404 on read** (GET/list/search hides existence), **403 on mutate** (delete/archive/resolve on a known-foreign ID).
- **D-07:** **Postgres RLS + app-level `*ForIdentity` (defense-in-depth)** on owner-scoped `aura.*` tables. Policy `USING (owner_id = current_setting('app.current_identity'))`, pgx sets `SET LOCAL app.current_identity` per tx; keep the additive `*ForIdentity` query methods too. (Rejected go-saas/Go-Multitenancy.)
- **D-08:** Three planes each get a kernel/storage-enforced mechanism: **Postgres = RLS**, **Neo4j = fail-closed `EXISTS{}` ownership filter**, **Garage = bucket-per-identity + scoped key** (grants per-bucket NOT per-prefix, F-007).
- **D-09:** `internal/documents` graph pipeline is identity-blind today (proven leak, spike 085). Fix mirrors the shipped memory pattern (`9a4ca594`): add `IdentityID` to `IngestRequest`→`ExtractedDocument`; atomically `MERGE (:User)` + `MERGE (:User)-[:HAS_DOCUMENT]->(:Document)` (`WITH` required between MERGE and MATCH); add `EXISTS {…}` to EVERY retrieval query; empty identity fails closed; thread `identityctx` into `document_search` + ingest.
- **D-10:** Do NOT rely on the Postgres `CatalogService` metadata scoping or 077 catalog-injection for isolation — both bypassable; isolation is graph-side + fail-closed.
- **D-11:** Operator stays `local` — existing data untouched. NEW Authula users get fresh identity UUIDs, fully isolated.
- **D-12:** Documents-plane backfill: attach `(:User {local})-[:HAS_DOCUMENT]->` edges to ALL existing docs from the Postgres catalog map BEFORE flipping retrieval to fail-closed scoping.
- **D-13:** Rollout is config-flag gated + reversible (`AURA_MUSR_ISOLATION` via `aura.settings`/env): deploy scoping (flag off) → run backfill → verify → flip flag on. `golang-migrate` handles ordering. (Rejected GO Feature Flag service; OpenFeature only if dynamic flags ever needed.)
- **D-14:** Eager, idempotent provisioning saga at admin-create: identity row + default capability grants (`agent.run` + self-Telegram; NOT admin caps) + Garage bucket & scoped key + per-identity MCP-config & skills dirs + empty `Agent.md`. Idempotent/resumable steps, lightweight in-process (rejected Temporal + Kafka/CDC saga libs).
- **D-15:** First-login = admin-set initial password + forced change on first login + TOTP enrollment, via embedded Authula (no SMTP).
- **D-16:** Break-glass = CLI-minted recovery on the host (short-lived reset token / admin credential reset). Shipped FIRST per MUSR-06.
- **D-17:** Jobs use random unguessable IDs bound to session/actor; default TTL = 1h (env-overridable); on expiry record status + terminate the process group.
- **D-18:** Poll/kill authority = owner session/actor + an admin capability (cross-session operational recovery).
- **D-19:** Class-(a) stdio/stateless → run inside identity's context (box in 37); class-(b) agent-memory shared graph → ONE globally-managed always-on sidecar (`:8091`), admin-governed, every call carries mandatory `user_identifier`; class-(d) documents graph → scoping BUILT here (D-09); class-(c) deferred.
- **D-20:** Per-identity MCP config = `~/.aura/mcp/{id}/servers.json` (shared catalog read-only + per-identity enable/trust), identity-keyed `mcp_audit`. Per-identity enable/trust applies to class-(a), NOT the shared class-(b) server.
- **D-21:** `$AURA_SKILLS_DIR/{id}/` + `~/.aura/pyscripts/{id}/` + identity-keyed `skill_audit`/approval; built-ins shared read-only; `newSkillToolForIdentity(ctx)`; additive `*ForIdentity` methods, `local` fallback. (Snippet execution in the box = Phase 37.)
- **D-22:** MUSR-05: all conversation deletion (AG-UI, Telegram `/clear`, CLI) routes through a runner lifecycle method that cancels active work, expires pending pauses, evicts session tools, handles background jobs, THEN deletes persistence.
- **D-23:** Key in-memory session/pause/tool state by `(identity, session)` — carry `identityctx` in the session struct; never key a shared map by session-id alone.
- **D-24:** Generalize `IdentityLinker.LinkOperator` to link ANY provisioned user's chat-id → their identity. Unknown chat-id → reject + point to web linking. Linking is web-initiated (existing `POST /api/settings/telegram/link`). Reuse `telebot.v4`.
- **D-25:** `local` is seeded at bootstrap with admin caps (`settings.model.write` + `identity.create` + `governance.write`). CLI always runs as `local`; non-operators are web/Telegram identities only — no CLI. (No `--as-identity` in 36.)
- **D-26:** Admin grants/revokes caps via CLI (`aura identity grant/revoke <id> <cap>`, audited) AND an admin-gated Settings-page control.
- **D-27:** Soft-delete → purge after grace. Deactivate immediately (block login, kill sessions, terminate jobs), retain for a grace window, then a scheduled purge runs the de-provisioning saga (conversations, docs + `:User` edges, Garage bucket, memory node/edges, MCP/skills dirs, grants, Authula user) — lightweight in-process, journaled, resumable, symmetric to D-14.
- **D-28:** Identity-key the audit tables (`tool_invocations`/`mcp_audit`/`skill_audit`) AND ship a full admin audit UI now (admin-gated web view).
- **D-29:** Full live stack gates in CI — add Garage + Authula to the CI stack (alongside Postgres + Neo4j); two-identity cross-deny E2E runs live and GATES on Linux CI under no-skip-as-green. Reuse Aura's existing build-tag live-stack harness — NOT testcontainers.

### Claude's Discretion
- Whether `GET /api/settings` is admin-gated or read-for-all (D-03).
- Exact capability name (`settings.model.write` vs reusing `governance.write`) — planner's call.
- Saga journal storage shape (new `aura.*` table vs outbox) for D-14/D-27.

### Deferred Ideas (OUT OF SCOPE)
- Casbin authz engine + org-roles (own spike + phase; requires PRD-amendment reopening "no RBAC"; reached via `HasCapability` interface → zero rework). **NOTE: `casbin/casbin/v2 v2.135.0` + `pckhoi/casbin-pgx-adapter/v3 v3.2.0` are already present in go.mod as indirect deps from spikes 086/087a — Phase 36 must NOT import or wire them.**
- Class-(c) per-user PIM/WhatsApp sidecar instances (Phase 37+).
- Per-identity quotas/limits (Phase 37/OPS; `golang.org/x/time/rate` + counters).
- CLI `--as-identity` impersonation (not in 36).
- Rich web audit view beyond per-user activity.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| MUSR-01 | Conversation + approval stores expose owner-scoped methods; AG-UI/API list/get/search/mutate filter by principal; B can never list/get/delete/archive/resolve A's data (404/403); proven by two-identity live E2E. *(F-028)* | **Gap confirmed:** `ListConversations`/`GetConversation`/`DeleteConversation` (`internal/db/queries/conversations.sql`) have NO `WHERE identity_id` filter today → they leak across identities. Approvals/`paused_states` have no `identity_id` at all. Fix = additive `*ForIdentity` sqlc queries + handlers reading `identityctx` + RLS backstop (§Architecture Pattern 1). 404/403 split per D-06. |
| MUSR-02 | New Web conversations created under `identityctx.IdentityID(ctx)` (`local` = CLI fallback); B-created conversation owned by B and runs. *(F-028)* | `runner_conversation.go` `defaultConversationOwner` already prefers a `user` identity then `local`; must key on `identityctx.IdentityID(ctx)` from the authenticated principal (already threaded via `agui.withPrincipal` → `identityctx.WithIdentityID`). |
| MUSR-03 | Background jobs use random unguessable IDs bound to session/actor; poll/kill require matching session/actor (or admin cap); B cannot poll/kill A's job. *(F-032)* | **Gap confirmed:** `internal/agent/tools/shell_bg.go` mints sequential `sh_1`/`sh_2` IDs (`b.seq++`) — GUESSABLE — with NO owner binding; any caller polls/kills any `shell_id`. Fix = `crypto/rand` IDs + `(identity,session)` owner on `bgShell` + authority check (§Pitfall 6). |
| MUSR-04 | Background jobs have default TTL + owner/session/task IDs + age metrics; TTL expiry records status + terminates process group. *(F-012)* | **Gap confirmed:** no TTL today (jobs run until done/killed). `killProcessGroup(cmd)` + `cmd.Cancel` already exist. Fix = default 1h TTL (env `AURA_SHELL_BG_TTL`) + reaper. |
| MUSR-05 | All conversation deletion (AG-UI, Telegram `/clear`, CLI) routes through one runner lifecycle method: cancel active work → expire pauses → evict session tools → handle bg jobs → THEN delete persistence. *(F-039)* | `Runner.Stop` + `evictSessionToolState(convID)` + `SessionEvictor` (`internal/agent/tools/evict.go`) already exist. Fix = single delete-lifecycle entry point invoked by all three surfaces; add bg-job handling; key state by `(identity,session)` (D-23). |
| MUSR-06 | Authula becomes default auth (cutover from passphrase) with provisioning + break-glass shipped FIRST; capability-per-route enforced; long-lived tokens NEVER in URLs/query strings. *(F-050)* | Authula IS already the sole credential issuer (`serve_auth.go` `buildAuthDeps` fails boot without it; passphrase path is legacy/test-only). Provisioning saga exists (`onboarding_provision.go`). Break-glass infra exists (`0023_identity_recovery` tables). Token-in-URL surface = Telegram deep-link `?start=<token>` (short-lived 1h setup bootstrap — ALLOWED per MUSR-06's "query tokens reserved for short-lived setup bootstrap") + audit review needed for any session token in query strings. |
</phase_requirements>

## Summary

Phase 36 is a security-critical, **mostly-additive** hardening of an already-multi-user-capable codebase. The identity plumbing is further along than the phase name implies: `identityctx.IdentityID(ctx)` is threaded from `agui.withPrincipal`; Authula is already the sole credential issuer with a hardened session validator; `identity_auth_links` is already 1:N-ready; a cross-store provisioning saga with per-leg compensation already exists (`onboarding_provision.go`); the `*ForIdentity` convention is already established (`internal/assets`); audit tables are already identity-keyed. The work is to **close the leaks and add the kernel backstops**, not to build multi-user from scratch.

Three isolation planes each get a storage/kernel-enforced mechanism (D-08): **Postgres RLS** (new — no RLS exists today; `WithTx` is the exact hook seam for `SET LOCAL`), **Neo4j fail-closed `EXISTS{}`** (the documents pipeline is provably identity-blind — spike 085 — and needs the `:User`-ownership pattern that memory MCP already ships), and **Garage bucket-per-identity + scoped key** (needs a NEW Garage Admin API client + the `[admin]` block enabled in `garage.toml`, which is absent today). Background jobs (`shell_bg.go`) are the other concrete gap: sequential guessable IDs, no TTL, no owner binding.

The two biggest ordering hazards are **backfill-before-flip** (D-12: attach `:User{local}-[:HAS_DOCUMENT]` edges to all existing docs before flipping `AURA_MUSR_ISOLATION` on, or the operator's docs vanish) and the **RLS-on-pooled-reads problem** (non-tx reads via `s.q` on the pool cannot `SET LOCAL`; owner-scoped reads must move into a transaction).

**Primary recommendation:** Extend the existing seams, do not replace them. Add `db.WithIdentityTx` (SET LOCAL + tx) as the RLS carrier; add the `EXISTS{}` filter to all six documents retrieval queries; build a small `net/http` Garage Admin v2 client; extend `onboarding_provision.go` with Garage + filesystem legs and add a journal table for resumability; retrofit `shell_bg.go` with crypto-random IDs, `(identity,session)` owners, and a TTL reaper. Gate the fail-closed flip behind `AURA_MUSR_ISOLATION` and run the backfill first.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Owner-scoping conversations/approvals reads+mutates | Database (Postgres RLS) | API/Backend (`*ForIdentity` filters) | RLS is the kernel backstop for a forgotten app filter (D-07); the app filter is the observable 404/403 (D-06) |
| Documents retrieval isolation | Database (Neo4j fail-closed `EXISTS`) | API/Backend (`identityctx` thread) | The fulltext/vector index can't be node-restricted, so ownership is a graph predicate after YIELD (D-09) |
| Object-store isolation | Storage (Garage bucket-per-identity + scoped key) | Provisioning saga | Garage grants are per-bucket not per-prefix (F-007) — kernel-enforced by a distinct bucket+key per identity (D-08) |
| Capability-per-route authz | API/Backend (`RequireCapability` middleware) | Database (`capability_grants`) | The admin/user distinction is a route gate reading `HasCapability` (D-01/D-04) |
| Cross-store provisioning/de-provisioning | API/Backend (in-process saga) | Postgres + Neo4j + Garage + FS + Authula | Four stores cannot share a tx → saga with per-leg compensation + journal (D-14/D-27) |
| Background job ownership + TTL | API/Backend (in-process registry) | OS (process group kill) | Jobs are in-memory process-scoped; owner binding + TTL are app-layer (D-17/D-18) |
| Session/pause/tool in-memory state | API/Backend (Runner) | — | Key by `(identity, session)` to prevent cross-user collision (D-23) |
| Auth session issuance/validation | Frontend-server (embedded Authula) | Database (`authula` schema + `identity_auth_links`) | Authula owns credentials; Aura maps user-id→identity UUID (MUSR-06) |

## Standard Stack

### Core (all present in `go.mod` — VERIFIED)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/jackc/pgx/v5` | v5.9.2 | Postgres driver + `pgxpool` | Already the app pool; `WithTx` is the SET LOCAL/RLS hook `[VERIFIED: go.mod]` |
| `github.com/neo4j/neo4j-go-driver/v5` | v5.28.4 | Neo4j graph driver | Backs `knowledge.Client` Read/Write; runs the `EXISTS{}` ownership queries `[VERIFIED: go.mod]` |
| `github.com/aws/aws-sdk-go-v2/service/s3` | v1.104.0 | S3 data-plane (Garage) | Existing `internal/objectstore.S3Store` (path-style) `[VERIFIED: go.mod]` |
| `github.com/Authula/authula` | v1.11.0 | Embedded web-auth (password + TOTP) | Already the sole credential issuer; `CoreServices()` UserService/AccountService for provisioning `[VERIFIED: go.mod]` |
| `gopkg.in/telebot.v4` | v4.0.0-beta.9 | Telegram bot framework | Reuse — no bot-framework swap (D-24) `[VERIFIED: go.mod]` |
| `github.com/golang-migrate/migrate/v4` | v4.19.1 | SQL migration ordering | Handles the deploy→backfill→flip ordering (D-13) `[VERIFIED: go.mod]` |
| `github.com/google/uuid` | v1.6.0 | UUIDv7 conversation/identity IDs | Already used `[VERIFIED: go.mod]` |
| stdlib `crypto/rand`, `net/http` | Go 1.26.4 | Unguessable job IDs; Garage Admin client | Zero new deps `[VERIFIED: go.mod]` |

### Supporting (infrastructure)
| Component | Version | Purpose | When to Use |
|-----------|---------|---------|-------------|
| Garage | v2.0.0 (`dxflrs/garage:v2.0.0`) | Object store; **Admin API v2** on `:3903` | Per-identity bucket+key provisioning (admin block must be enabled — §Pitfall 3) `[VERIFIED: compose.yaml]` |
| Neo4j | 5.26 (Community + APOC + GDS) | Graph; `chunk_text` fulltext + `chunk_embedding` HNSW | `EXISTS{}` subquery supported + live-proven (spike 085) `[VERIFIED: spike 085 live-run]` |
| PostgreSQL | 15+ (port 5432) | `aura.*` schema; RLS | RLS since 9.5; `set_config`/`current_setting` core `[ASSUMED: PG 15+ — RLS not version-sensitive]` |

### Alternatives Considered (all rejected — do NOT re-litigate)
| Instead of | Rejected Alternative | Reason (from CONTEXT.md) |
|------------|----------------------|--------------------------|
| Postgres RLS backstop | go-saas/saas, LiamDotPro/Go-Multitenancy | Single-DB SaaS mismatch vs Aura's 3 substrates |
| `HasCapability` interface | Casbin, go-mizu/rbac | No RBAC in 36; Casbin deferred to own phase (present as indirect dep — do not wire) |
| Embedded Authula | Ory Kratos | Authula already embedded |
| In-process saga | Temporal + Kafka/CDC saga libs | Wrong shape for single-binary appliance |
| `AURA_MUSR_ISOLATION` env/settings flag | GO Feature Flag service, OpenFeature | Overkill for one appliance rollout flip |
| Build-tag live-stack harness | testcontainers-go | Forks Aura's established CI model (present as indirect dep — do not use) |

**Installation:** No new external packages. Garage Admin client is self-built on `net/http`.

## Package Legitimacy Audit

> Phase 36 installs **NO new external packages**. All libraries are pre-existing, established deps.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| jackc/pgx/v5 | Go modules | ~6 yrs | very high | github.com/jackc/pgx | N/A (present) | Pre-existing |
| neo4j/neo4j-go-driver/v5 | Go modules | 8+ yrs | high (official) | github.com/neo4j/neo4j-go-driver | N/A (present) | Pre-existing |
| aws-sdk-go-v2/service/s3 | Go modules | official | very high | github.com/aws/aws-sdk-go-v2 | N/A (present) | Pre-existing |
| Authula/authula | Go modules | present | low (niche) | github.com/Authula/authula | N/A (present, embedded) | Pre-existing |
| casbin/casbin/v2 | Go modules | mature | high | github.com/casbin/casbin | N/A | **DO NOT WIRE (deferred phase; indirect only)** |
| pckhoi/casbin-pgx-adapter/v3 | Go modules | present | low | github.com/pckhoi/casbin-pgx-adapter | N/A | **DO NOT WIRE (deferred phase; indirect only)** |
| testcontainers/testcontainers-go | Go modules | mature | high | github.com/testcontainers/testcontainers-go | N/A | **DO NOT USE (D-29 rejects; indirect only)** |

**Packages removed due to slopcheck [SLOP] verdict:** none (no installs)
**Packages flagged [SUS]:** none

*slopcheck N/A: no new package installs in this phase. The only new code artifact is a self-built Garage Admin v2 client (stdlib `net/http`).*

## Architecture Patterns

### System Architecture Diagram

```
                       ┌─────────────────────────────────────────────┐
   Web (SPA) ──login──▶│ Authula (/auth/*, authula schema, own pool)  │
                       │  session cookie __Host-authula_session       │
                       └───────────────┬─────────────────────────────┘
                                       │ validate → authula user-id
                                       ▼
                       ┌─────────────────────────────────────────────┐
   Telegram ──linked──▶│ session_validate.Validator.Validate         │
   (chat-id →          │  → ResolveIdentityID (identity_auth_links)   │
    identity)          │  → identityctx.WithIdentityID(ctx, UUID)     │
                       └───────────────┬─────────────────────────────┘
   CLI (== local) ─────────────────────┤ principal on ctx
                                       ▼
              ┌────────────────────────────────────────────────────┐
              │ RequireAuth → RequireCapability(cap) per route      │
              │ (admin routes: settings.model.write/identity.create)│
              └───────────────┬────────────────────────────────────┘
                              │ identityctx.IdentityID(ctx) = the ONE principal
        ┌─────────────────────┼───────────────────────┬───────────────────────┐
        ▼                     ▼                       ▼                       ▼
 ┌─────────────┐      ┌───────────────┐       ┌──────────────┐        ┌──────────────┐
 │ Postgres    │      │ Neo4j graph   │       │ Garage        │        │ Filesystem   │
 │ aura.*      │      │ :User owner   │       │ bucket-per-id │        │ ~/.aura/{id} │
 │ RLS: SET    │      │ EXISTS{}      │       │ + scoped key  │        │ mcp/skills/  │
 │ LOCAL app.  │      │ fail-closed   │       │ (Admin API v2)│        │ pyscripts/   │
 │ current_id  │      │ on retrieval  │       │               │        │ Agent.md     │
 │ + *ForIdent │      │ + ingest MERGE│       │               │        │              │
 └─────────────┘      └───────────────┘       └──────────────┘        └──────────────┘
        ▲                     ▲                       ▲                       ▲
        └──────────── Provisioning saga (D-14) fans out; De-provisioning (D-27) reverses ┘
                       journaled + resumable + per-leg compensation
```

### Pattern 1 — RLS carrier: `SET LOCAL` inside a pgx transaction (D-07)
**What:** RLS reads a session var (`app.current_identity`) that policies compare against `owner_id`. The only pooling-safe way to set it is `SET LOCAL` / `set_config(..., is_local=true)` **inside a transaction** (auto-resets at COMMIT/ROLLBACK).
**When to use:** Every owner-scoped read AND write. The existing `db.WithTx` (`internal/db/tx.go`) is the exact seam — add an identity-aware variant.
**Critical wiring problem:** Non-tx reads today use `s.q` (bound to the pool, e.g. `identity.Store`, `conversations.Store` list/get). Those cannot `SET LOCAL` (no tx). **Owner-scoped reads must move into a transaction** so the SET LOCAL applies. This is the central RLS decision.

```go
// Source: extends internal/db/tx.go WithTx (VERIFIED read)
// WithIdentityTx sets the RLS session var transaction-locally, then runs fn.
func WithIdentityTx(ctx context.Context, pool *pgxpool.Pool, identityID string, fn func(*sqlc.Queries) error) (err error) {
    tx, err := pool.Begin(ctx)
    if err != nil { return err }
    defer func() {
        if p := recover(); p != nil { _ = tx.Rollback(ctx); panic(p) }
        if err != nil { _ = tx.Rollback(ctx); return }
        err = tx.Commit(ctx)
    }()
    // is_local=true → resets at tx end; safe under pgxpool connection reuse.
    if _, err = tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, identityID); err != nil {
        return err
    }
    return fn(sqlc.New(tx))
}
```

```sql
-- Migration 0026 (next free slot after 0025). Policy shape, per owner-scoped table.
ALTER TABLE aura.conversations ENABLE ROW LEVEL SECURITY;
CREATE POLICY conversations_owner_isolation ON aura.conversations
  USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
-- NULLIF guards the empty-string case ('' ::uuid errors); an unset var → NULL → 0 rows (fail closed).
GRANT ... unchanged (aura_app already has DML).
```

**Table-owner bypass caveat (from web sources + verified role model):** By default the table OWNER bypasses RLS. In Aura, `aura_migrate` owns the tables (it runs DDL — see the `GRANT ALL ... TO aura_migrate` in every migration) and `aura_app` is a **non-owner, non-superuser** runtime role (DML-only grants). Therefore RLS applies to `aura_app` **without needing `FORCE ROW LEVEL SECURITY`**. This is convenient: the D-12 backfill migration runs as `aura_migrate` (owner) and legitimately bypasses RLS to touch all rows. **Action items:** (1) verify `aura_app` has neither `BYPASSRLS` nor `SUPERUSER`; (2) `ENABLE` (not `FORCE`) RLS; (3) run backfill as `aura_migrate`.

**Owner-scoped tables + their current identity column:**
| Table | Has identity col? | RLS approach |
|-------|-------------------|--------------|
| `aura.conversations` | ✅ `identity_id uuid` + `conversations_identity_status_idx` | Direct policy on `identity_id` |
| `aura.assets` | ✅ `identity_id uuid` + indexes | Direct policy |
| `aura.scheduler_tasks` | ✅ `identity_id` | Direct policy |
| `aura.pending_notifications` | ✅ `identity_id` (0014) | Direct policy |
| `aura.conversation_turns` | ❌ (scoped via `conversation_id` FK) | Policy via subquery `conversation_id IN (SELECT id FROM aura.conversations)` OR rely on RLS on parent (turns are only reachable through a scoped conversation read) |
| `aura.paused_states` (approvals) | ❌ (only `conversation_id` FK) | **Decision needed:** add `identity_id` column (migration + backfill from conversations) OR policy via `conversation_id` subquery. MUSR-01 explicitly names approvals — recommend the column for a clean `*ForIdentity`. |
| `aura.settings` | global (updated_by only) | NOT owner-scoped (admin-gated model knobs) |

### Pattern 2 — Neo4j fail-closed ownership filter (D-08/D-09)
**What:** After the fulltext/vector index YIELDs candidates, keep only nodes reachable from the caller's `:User` via `HAS_DOCUMENT`. The index cannot be node-restricted, so ownership is a post-YIELD predicate (the "seed then ownership-filter" shape memory MCP ships).
**When to use:** EVERY documents retrieval query. **Six concrete queries need it** (CONTEXT lists four; the codebase has six):

| Query const | File | Current scope | Add EXISTS? |
|-------------|------|---------------|-------------|
| `sparseSearchQuery` | `search.go:169` | none (global fulltext) | ✅ REQUIRED |
| `docScopedSparseQuery` | `search.go:194` | `document_id` only | ✅ (add on the `:Chunk` node) |
| `vectorSeedQuery` | `retrieve.go:209` | none (global HNSW) | ✅ REQUIRED |
| `docScopedVectorSeedQuery` | `retrieve.go:233` | `document_id` only | ✅ (add on the `:Chunk` node) |
| `neighborExpandQuery` | `retrieve.go:255` | winner_ids (from scoped seeds) | ✅ defense-in-depth |
| `graphExpandQuery` | `graphrag.go:99` | winner_ids (from scoped seeds) | ✅ defense-in-depth |

```cypher
// Source: spike 085 main.go docScopedSearchQuery (VERIFIED live, Neo4j 5.26)
CALL db.index.fulltext.queryNodes('chunk_text', $query, {limit: $candidate_limit})
YIELD node, score
WHERE EXISTS { (:User {identifier: $user_identifier})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }
  AND coalesce(node.active, true) = true AND node.deleted_at IS NULL
RETURN node.document_id AS document_id, node.id AS chunk_id, score AS score
ORDER BY score DESC LIMIT $limit
```

**Ingest ownership edge** — add to `documentUpsertQuery` (`indexer.go:219`, where `:Document` is MERGEd) atomically. The **`WITH` between `MERGE` and `MATCH` is REQUIRED** (spike 085 gotcha #a):
```cypher
// Source: spike 085 main.go FIX block (VERIFIED live)
MERGE (u:User {identifier: $identity_id}) ON CREATE SET u.id = $identity_id
WITH u
MATCH (d:Document {id: $document.id})
MERGE (u)-[:HAS_DOCUMENT]->(d)
```
Thread `IdentityID` into `documents.IngestRequest` → `ExtractedDocument` (`types.go` — both structs currently have NO identity field) → `documentParams` map → the indexer write. Add `IdentityID` to `documents.SearchRequest` → pass as `$user_identifier` param. **Empty identity fails closed** naturally (no `:User` has identifier `""`), but ALSO guard in Go (`Searcher.Search`/`Service.Retrieve` reject an empty identity when `AURA_MUSR_ISOLATION` is on).

**Transport note:** production `documents.KnowledgeClient` is `internal/knowledge.Client` (`client.go:226` Read / `:234` Write), which routes through the **mcp-neo4j-cypher MCP path** (see `graphview.go` comment); `schema.go`/`probe.go` use the native driver directly. The `EXISTS{}` Cypher + `$user_identifier` param work identically over either transport.

### Pattern 3 — Garage bucket-per-identity via Admin API v2 (D-08)
**What:** Each identity gets its own bucket `aura-<identityID>` and its own scoped access key; the key is granted read+write on ONLY that bucket. Garage grants are per-bucket (F-007), so this is storage-enforced.
**When to use:** Provisioning saga (create) + de-provisioning saga (delete). Requires the Garage **Admin API** (a NEW `net/http` client — the current `objectStoreBootstrap` only does S3 `PutBucketCors`; the shared bucket/key are created by `docker/aura/aura-garage-bootstrap.sh` via the `garage` CLI, unreachable from the daemon).

```go
// Source: garagehq.deuxfleurs.fr admin-api + garage-webui docs (CITED). Field names MEDIUM — verify against OpenAPI.
// POST http://garage:3903/v2/CreateBucket   Authorization: Bearer <admin_token>
//   {"globalAlias": "aura-<identityID>"}
// POST http://garage:3903/v2/CreateKey
//   {"name": "key-<identityID>"}            → response carries accessKeyId + secretAccessKey
// POST http://garage:3903/v2/AllowBucketKey
//   {"bucketId": "<id>", "accessKeyId": "<id>", "permissions": {"read": true, "write": true, "owner": false}}
// De-provision: POST /v2/DenyBucketKey, /v2/DeleteBucket, /v2/DeleteKey
```
**Enable the admin API** (absent in `docker/garage/garage.toml` today — VERIFIED):
```toml
[admin]
api_bind_addr = "[::]:3903"     # internal network only — do NOT publish to host
admin_token = "<AURA_GARAGE_ADMIN_TOKEN>"
```
New env: `AURA_GARAGE_ADMIN_ENDPOINT` (`http://garage:3903`) + `AURA_GARAGE_ADMIN_TOKEN`. **Secret storage decision (planner):** the per-identity `secretAccessKey` returned by CreateKey must be persisted to select at request time — a new `aura.identity_object_store` table (or SOPS). Same trust boundary as `.env`; encrypt-at-rest recommended. The current `S3Store` uses static creds (`s3.go`); wire a credential-resolver keyed on `identityctx` (or per-identity `S3Store` instances), selecting bucket `aura-<id>` + that identity's key. `aura.assets` already carries `object_bucket`/`object_key` columns.

### Pattern 4 — Extend the existing cross-store saga; add a journal for resumability (D-14/D-27)
**What:** `internal/agui/onboarding_provision.go` is the **exact template** — an ordered cross-store saga with narrow consumer-side ports and per-leg compensation (Leg B Authula → Leg A aura `WithTx` → Recovery → Leg C Telegram → Audit; each failure walks back the compensations). Phase 36 **extends** it with a **Garage leg** (CreateBucket+CreateKey+AllowBucketKey; COMP = DeleteBucket+DeleteKey) and a **filesystem leg** (`~/.aura/mcp/{id}`, `$AURA_SKILLS_DIR/{id}`, `~/.aura/pyscripts/{id}`, empty `Agent.md`; COMP = RemoveAll).
**The gap:** the existing saga is compensation-based (rolls back on failure) but **NOT journaled/resumable** (a mid-failure crash leaves partial state with no forward-recovery). D-14/D-27 require **journaled + resumable** ("re-run mid-failure converges"). **Add a journal** (Claude's Discretion: new `aura.*` table vs outbox):
```sql
-- one row per (saga_run, step); the resumer re-runs incomplete steps on restart.
CREATE TABLE aura.provisioning_saga (
  saga_id      uuid NOT NULL,
  identity_id  uuid NOT NULL,
  kind         text NOT NULL CHECK (kind IN ('provision','deprovision')),
  step         text NOT NULL,   -- authula_user | aura_identity | garage | filesystem | audit
  status       text NOT NULL CHECK (status IN ('pending','done','failed')),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (saga_id, step)
);
```
**Idempotent step markers:** identity `INSERT ON CONFLICT DO NOTHING` (already used, `0004`); grants `ON CONFLICT` (already, `GrantCapability`); Garage `CreateBucket`/`CreateKey` idempotent-by-alias/name (handle the 409-exists as success); `os.MkdirAll` idempotent; `Agent.md` write-if-absent. De-provisioning (D-27) is the symmetric inverse driven by the scheduled purge after the soft-delete grace window; reuse `internal/scheduler`.

### Pattern 5 — Flag-gated reversible rollout (D-13)
**What:** `AURA_MUSR_ISOLATION` gates the fail-closed flip across all retrieval/query paths. **The `internal/settings` `OverlayEnv` allowlist (`AllowedKeys`) deliberately excludes security/connection env** (only model-backend knobs) — so do NOT route the isolation flag through the model-knob overlay. Read it as a dedicated `config.Config` field from env (and optionally a direct `aura.settings` read, not the overlay). Overlay/env changes take effect on restart — consistent with the "flip → restart" rollout.
**Ordering (golang-migrate + ops):**
1. Deploy scoping code with `AURA_MUSR_ISOLATION=off` (queries carry the `EXISTS{}`/`*ForIdentity` paths but the flag selects fail-closed-vs-fallthrough).
2. Run backfill: Neo4j `:User{local}-[:HAS_DOCUMENT]` edges to ALL existing docs (source: `aura.assets` identity_id→document_id map, and/or `0025_document_control_plane`), plus any Postgres owner_id backfill (existing rows already carry `identity_id`, so mostly Neo4j-side).
3. Verify operator still sees own data (E2E as `local`).
4. Flip `AURA_MUSR_ISOLATION=on` + restart.
**Reversibility:** flag off = current `local`-fallback behavior. The scoping code MUST retain the flag-gated fallback path (do not hard-remove the unscoped branch until a later phase).

### Anti-Patterns to Avoid
- **Relying on `CatalogService` metadata for documents isolation** (D-10) — bypassable; isolation is graph-side fail-closed.
- **Garage key-prefix in a shared bucket** — grants are per-bucket not per-prefix (F-007); use bucket-per-identity.
- **`SET` (not `SET LOCAL`) for the RLS var** — persists on the pooled connection and leaks to the next borrower.
- **Keying in-memory session/tool/pause maps by session-id alone** (D-23) — collision/leak risk under concurrent multi-user.
- **Flipping `AURA_MUSR_ISOLATION` before the backfill** — the operator's existing docs become invisible (D-12).
- **Wiring `casbin`/`casbin-pgx-adapter`/`testcontainers`** — present as indirect deps but forbidden this phase.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-identity row filtering backstop | A middleware that appends `WHERE identity_id` to every query | Postgres RLS (`ENABLE` + policy) via `set_config` in `WithIdentityTx` | The kernel catches a forgotten `*ForIdentity` filter; app-layer filtering alone is one missed query from a leak (D-07) |
| Object-store per-tenant isolation | Prefix-scoping inside one bucket + app checks | Garage bucket-per-identity + scoped key (Admin API v2) | Garage grants are per-bucket; a prefix scheme is app-enforced (silent hole, F-007) |
| Cross-store atomic provisioning | A distributed-tx / 2PC across PG+Neo4j+Garage+FS+Authula | Extend `onboarding_provision.go` saga + a journal table | Four stores can't share a tx; the compensation+journal pattern already exists |
| Documents ownership scoping | A new bespoke tenancy mechanism | Mirror the shipped memory `:User`-`HAS_DOCUMENT` pattern (`9a4ca594`) | Documents + memory then isolate identically; proven live (spike 085) |
| Unguessable job IDs | `fmt.Sprintf("sh_%d", seq)` | `crypto/rand` 128-bit hex | Sequential IDs are trivially guessable (MUSR-03) |
| Password reset / break-glass | A new token scheme | Reuse `aura.password_reset_tokens`/`identity_recovery` (0023) via a CLI command | The short-lived hashed-token infra already exists (D-16) |
| Feature flag plumbing | A flag service / OpenFeature | One `config.Config` bool from env | One appliance rollout flip (D-13) |

**Key insight:** Nearly every "new" mechanism this phase needs already has a shipped precedent in the codebase (RLS is the sole genuinely-new primitive). The failure mode is re-inventing (a bespoke tenancy filter, a new token scheme) rather than extending the established seam.

## Runtime State Inventory

> This IS a cutover/migration phase (D-11/D-12/D-13). After every file is updated, what runtime state still carries pre-isolation assumptions?

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | (1) **Neo4j `:Document`/`:Chunk` nodes carry NO `:User` owner edge** — all existing docs are unowned → invisible under fail-closed scoping. (2) Existing `aura.conversations`/`aura.assets` rows already carry `identity_id` (mostly `local` after the `retireLegacyLocalIdentity` migration) — no data migration needed, only the read-path filter + RLS. (3) agent-memory graph already scoped by `:User` (`9a4ca594`) — no action. | **Data migration (Neo4j backfill, D-12):** attach `(:User{local})-[:HAS_DOCUMENT]->(:Document)` for every existing doc from the Postgres `identity_id→document_id` map, BEFORE the flip. Code edit only for Postgres (add `*ForIdentity` + RLS). |
| **Live service config** | **Garage `garage.toml` has NO `[admin]` block** — the admin API (port 3903) is not enabled; the single shared key `aura-assets-local` is created by `aura-garage-bootstrap.sh` (CLI, at container init), not exportable from the daemon. | **Config edit + infra:** add `[admin]` block (token + bind), expose 3903 internal-only, add the CI Garage service with admin enabled (D-29). Provision NEW per-identity buckets/keys via the daemon's new Admin client. |
| **OS-registered state** | Background shell jobs are **in-memory only** (`BackgroundShells` process-scoped map) — no OS registration, no persistence. Process groups are set (`setProcessGroup`) but not tracked by owner. | Code edit only (add owner + TTL + crypto IDs). No OS re-registration. A daemon restart drops all in-flight jobs (acceptable — they were never durable). |
| **Secrets/env vars** | New env needed: `AURA_MUSR_ISOLATION` (flag), `AURA_GARAGE_ADMIN_TOKEN` + `AURA_GARAGE_ADMIN_ENDPOINT`, `AURA_SHELL_BG_TTL` (default 1h). Per-identity Garage `secretAccessKey`s are NEW secret material to persist (table or SOPS). Existing `AURA_AUTHULA_SECRET`/`AURA_WEB_AUTH_SECRET` unchanged. | Add env catalog entries (PRD §env vars). Decide per-identity S3 secret storage (encrypt-at-rest). |
| **Build artifacts / installed packages** | `internal/webui/dist/*` (committed Vite build) — the admin audit UI (D-28) + Settings capability-gating (D-03) are frontend changes requiring a rebuilt `dist`. `casbin`/`testcontainers` indirect deps sit in `go.sum` from spikes — harmless if unused. | Rebuild `internal/webui/dist` after the SPA changes. Do NOT promote the casbin/testcontainers indirect deps to direct. |

**The canonical question answered:** After the repo edits land, the two runtime systems still holding pre-isolation state are (1) the **Neo4j graph** (unowned documents — needs the backfill data migration) and (2) **Garage** (single shared bucket/key + disabled admin API — needs config + new per-identity provisioning). Everything else is a code-path edit (`*ForIdentity` + RLS + identity threading).

## Common Pitfalls

### Pitfall 1: RLS silently does nothing on pooled non-tx reads
**What goes wrong:** You add the RLS policy and `SET LOCAL`, but list/get reads still return all identities' rows.
**Why:** `SET LOCAL` only affects the current transaction. Reads via `s.q` (pool-bound, e.g. `conversations.Store.List`, `identity.Store`) run outside any tx, so the var is unset → the policy sees `NULL` → **fail-closed = zero rows** (or, if you forgot the policy, all rows). Either way the read is wrong.
**How to avoid:** Route every owner-scoped read through `WithIdentityTx` (tx-scoped `set_config`). The `*ForIdentity` explicit filter is the primary correctness path; RLS is the backstop.
**Warning signs:** A `*ForIdentity` test passes but a plain list returns cross-identity rows, or an owner-scoped read returns 0 rows for the owner (var unset).

### Pitfall 2: Backfill ordering — flip before backfill hides the operator's own docs
**What goes wrong:** `AURA_MUSR_ISOLATION=on` before the Neo4j `:User` edges exist → every existing document is unowned → the operator's `document_search` returns nothing.
**Why:** Fail-closed means "no ownership edge ⇒ invisible."
**How to avoid:** Enforce the D-13 order (deploy off → backfill → verify → flip). Make the backfill idempotent (`MERGE`) and gate the flip on a verification E2E.
**Warning signs:** Operator reports "my uploaded docs disappeared after the update."

### Pitfall 3: Garage admin API not enabled / published to host
**What goes wrong:** Provisioning fails with connection-refused (admin API off) OR the admin port is exposed to the host (privilege-escalation surface).
**Why:** `garage.toml` has no `[admin]` block today; the port must be added but kept internal.
**How to avoid:** Add `[admin] api_bind_addr = "[::]:3903"` + `admin_token`; bind on the internal compose network only (do NOT add a `127.0.0.1:3903:3903` host publish). Token via `AURA_GARAGE_ADMIN_TOKEN`.
**Warning signs:** `curl localhost:3903` from the host succeeds (should be refused).

### Pitfall 4: `MERGE` → `MATCH` without `WITH` (Cypher)
**What goes wrong:** The ingest ownership-edge write errors at runtime.
**Why:** Cypher requires a `WITH` to separate a `MERGE` from a following `MATCH` (spike 085 gotcha #a).
**How to avoid:** Use the verified shape `MERGE (u:User{...}) WITH u MATCH (d:Document{...}) MERGE (u)-[:HAS_DOCUMENT]->(d)`.

### Pitfall 5: Empty/unset identity NOT failing closed everywhere
**What goes wrong:** A retrieval path with an empty `$user_identifier` returns the global index (leak) instead of nothing.
**Why:** A query that only conditionally applies the EXISTS (e.g. `$id = "" OR EXISTS{...}`) re-introduces the leak — mirroring today's `($document_id = "" OR node.document_id = $document_id)` fallthrough in `sparseSearchQuery`.
**How to avoid:** The EXISTS must be unconditional (no `$id = "" OR`). Empty identity → no `:User` match → 0 rows. Also guard in Go (`Searcher`/`Service` reject empty identity when the flag is on).
**Warning signs:** A test with `user_identifier=""` returns rows.

### Pitfall 6: Guessable/unowned background job IDs (MUSR-03/04)
**What goes wrong:** Session B polls/kills session A's job by guessing `sh_2`.
**Why:** `shell_bg.go` mints sequential IDs (`b.seq++` → `sh_%d`) with no owner check; `shell_poll`/`shell_kill` look up purely by `shell_id`.
**How to avoid:** Mint `crypto/rand` 128-bit hex IDs; store `(identity, session)` owner on `bgShell`; on poll/kill assert caller `(identity,session)` matches OR caller holds the admin cap (D-18). Add a 1h TTL reaper that `killProcessGroup` + records status on expiry (D-17).
**Warning signs:** A poll for another session's `shell_id` succeeds.

### Pitfall 7: File-size / god-class violations on touched files
**What goes wrong:** Edits push a file past the CLAUDE.md ≤600 LOC ceiling.
**Why:** Several touch-points are already large: `serve_webui.go` (~540 LOC, adds capability mounts), `shell_bg.go` (~500 LOC, adds owner+TTL+IDs), `onboarding_provision.go` (~504 LOC, adds Garage+FS legs).
**How to avoid:** Refactor-on-touch (CLAUDE.md): split `shell_bg_owner.go`/`shell_bg_ttl.go`, `onboarding_provision_resources.go` (Garage+FS legs), and a `serve_webui_musr.go` for the new route mounts.

## Code Examples

### Owner-scoped conversation read (the `*ForIdentity` + RLS pair)
```go
// Source: pattern from internal/assets/store.go GetForIdentity (VERIFIED read) + WithIdentityTx above.
// sqlc query (internal/db/queries/conversations.sql) — ADD:
//   -- name: ListConversationsForIdentity :many
//   SELECT ... FROM aura.conversations
//   WHERE identity_id = $1 AND status <> 'deleted'
//     AND (sqlc.arg(include_archived)::boolean OR status = 'active')
//   ORDER BY last_active_at DESC;
// Handler reads identityctx.IdentityID(ctx) and routes through WithIdentityTx so RLS backstops.
```

### Neo4j fail-closed retrieval (unconditional EXISTS)
```cypher
// Source: spike 085 (VERIFIED live). Applied to sparseSearchQuery / vectorSeedQuery.
CALL db.index.vector.queryNodes('chunk_embedding', $candidate_limit, $query_vector) YIELD node, score
WHERE EXISTS { (:User {identifier: $user_identifier})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }
  AND coalesce(node.active, true) = true AND node.deleted_at IS NULL
RETURN node.document_id AS document_id, node.id AS chunk_id, ... , score AS score
ORDER BY score DESC
```

### Background job owner + TTL (retrofit)
```go
// Source: extends internal/agent/tools/shell_bg.go (VERIFIED read).
type bgShell struct {
    id        string          // crypto/rand hex, NOT sequential
    ownerID   string          // identityctx.IdentityID at start
    sessionID string          // conversation/session key (D-23)
    startedAt time.Time
    ttl       time.Duration   // default 1h, AURA_SHELL_BG_TTL
    // ... existing fields
}
// poll/kill: require (caller.identity == ownerID && caller.session == sessionID) || caller has admin cap.
// reaper: on startedAt+ttl → killProcessGroup + record status "expired".
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Documents graph identity-blind (all chunks global) | `:User`-`HAS_DOCUMENT` ownership + fail-closed `EXISTS{}` | Phase 36 (mirrors memory `9a4ca594`, 2026-07-03) | Closes the spike-085 leak |
| Single shared Garage bucket + static key (`aura-assets-local`) | Bucket-per-identity + scoped key via Admin API v2 | Phase 36 | Removes F-007 shared-cred hole |
| App-only owner filtering (where present) | RLS kernel backstop + `*ForIdentity` | Phase 36 | Kernel catches a forgotten filter |
| Passphrase HMAC web-auth | Authula (already the sole issuer) | pre-36 (Phase 24) | Provisioning + TOTP + break-glass |
| Sequential in-memory job IDs, no TTL | crypto-random IDs + `(identity,session)` owner + 1h TTL | Phase 36 | MUSR-03/04 |

**Deprecated/outdated:**
- CLAUDE.md's "11 migrations 0001-0011" is **stale** — the tree has **25 migrations** (`0001`–`0025`); the next free slot for Phase 36 is **`0026`**. `[VERIFIED: internal/db/migrations/ listing]`
- Garage v2.0.0 replaced the pre-v1 admin API paths — use the `/v2/` base path (`garage json-api` subcommand also exists). `[CITED: garagehq.deuxfleurs.fr]`

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Garage v2 `CreateKey` returns fields named `accessKeyId` + `secretAccessKey` (exact JSON casing) | Pattern 3 | LOW — verify against the Garage v2 OpenAPI spec at build time; a wrong field name is a compile-quick fix |
| A2 | PostgreSQL is 15+ (RLS + `set_config` behave as documented) | Standard Stack | LOW — RLS/`set_config` have been stable since PG 9.5; only matters if <9.5 (implausible) |
| A3 | `aura_app` has neither `SUPERUSER` nor `BYPASSRLS` (so RLS applies without FORCE) | Pattern 1 | MEDIUM — if `aura_app` is unexpectedly privileged, RLS silently no-ops; **verify in `EnsureRoles`/`cmd/aura/db.go` before relying on the backstop** |
| A4 | RBAC-03 ("Per-identity Postgres RLS" listed as OUT in REQUIREMENTS.md §Non-Goals) is superseded by D-07 for identity-isolation (not RBAC) use | §Open Questions | MEDIUM — a PRD/REQUIREMENTS amendment may be required per CLAUDE.md's PRD-first discipline (see OQ-1) |
| A5 | The `document_search` tool + ingest path can be threaded with `identityctx` without a signature break to the MCP transport | Pattern 2 | LOW — the KnowledgeClient params map already carries arbitrary keys; add `user_identifier` |
| A6 | Per-identity Garage `secretAccessKey` is stored in a new `aura.*` table (Claude's Discretion overlaps saga-journal discretion) | Pattern 3 | LOW — storage-shape choice; encrypt-at-rest either way |

## Open Questions

1. **RBAC-03 vs D-07 conflict (needs a PRD/REQUIREMENTS amendment decision).**
   - What we know: REQUIREMENTS.md §Non-Goals lists **RBAC-03: "Per-identity Postgres row-level security"** as explicitly OUT of v2.0.0. CONTEXT.md **D-07** (2026-07-05, this phase's discuss gate) LOCKS RLS in.
   - What's unclear: whether this needs a formal PRD-amendment commit (CLAUDE.md: "Deviazioni dal PRD richiedono PRD-amendment commit prima dell'implementazione").
   - Recommendation: Honor D-07 (it is the later, phase-specific decision, and it reframes RLS as **identity-isolation defense-in-depth, NOT RBAC roles** — so the *intent* of RBAC-03's exclusion, "no role model," is preserved). **Flag for the planner to emit a REQUIREMENTS/PRD-amendment note** reclassifying RBAC-03 (RLS-for-isolation is in; RLS-for-roles remains out).

2. **`paused_states` (approvals) owner-scoping: add `identity_id` column vs policy-via-join.**
   - What we know: MUSR-01 explicitly names approvals; `aura.paused_states` has only `conversation_id` (FK to conversations), no `identity_id`.
   - Recommendation: add an `identity_id` column (migration + backfill from `conversations.identity_id`) for a clean `*ForIdentity` + direct RLS policy; the subquery-via-conversation policy is a fallback if a column add is undesirable.

3. **Capability name: `settings.model.write` (new) vs reuse `governance.write` (existing).**
   - What we know: `serve_webui.go` ALREADY gates `PUT/DELETE /api/settings/{key}` with `governance.write` and `GET /api/settings` with `governance.read`. D-03 mentions `settings.model.write`; Claude's Discretion allows reusing `governance.write`.
   - Recommendation: **Reuse `governance.write`** (already wired + load-bearing) unless the operator wants a finer-grained settings cap; adding `settings.model.write` is net-new work for no isolation gain. Planner's call.

4. **Per-identity Garage secret-key storage: new `aura.*` table vs SOPS.**
   - Recommendation: a small `aura.identity_object_store(identity_id, bucket, access_key, secret_key_enc)` table, encrypted at rest, same trust boundary as `.env`. Overlaps the saga-journal discretion — could co-locate.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| PostgreSQL (`aura.*`) | RLS + owner-scoping | ✓ (compose) | 15+ | — (blocking) |
| Neo4j (`chunk_text` fulltext, `chunk_embedding` HNSW) | Documents fail-closed | ✓ (compose) | 5.26 | — (blocking) |
| Garage S3 (`:3900`) | Object store | ✓ (compose) | v2.0.0 | — |
| Garage **Admin API (`:3903`)** | Per-identity bucket/key provisioning | ✗ (no `[admin]` block) | v2.0.0 | **None — must enable in `garage.toml`** |
| Authula (embedded) | Multi-user auth cutover | ✓ (in-binary) | v1.11.0 | — |
| `mcp-neo4j-cypher` | `knowledge.Client` Read/Write MCP path | ✓ (0.6.0, WSL) | 0.6.0 | native neo4j driver (schema.go/probe.go already use it) |
| Docker Go SDK (`moby/moby/client`) | (Phase 37 box — NOT this phase) | ✓ (go.mod) | — | — |

**Missing dependencies with no fallback:**
- Garage Admin API is not enabled — **blocks per-identity provisioning until the `[admin]` block + token are added** (config change, not a missing binary).

**Missing dependencies with fallback:**
- None material for Phase 36.

## Validation Architecture

> Nyquist validation is ENABLED (`workflow.nyquist_validation: true`). This section is consumed to generate VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib) + build tags; `pgregory.net/rapid` v1.3.0 for property tests; `go.uber.org/goleak` v1.3.0 |
| Config file | none (Go convention); build tags gate the live tiers |
| Quick run command | `go test ./internal/<pkg>/` (unit) |
| Full suite command | `go test -tags 'db_integration neo4j_integration' -race ./...` + new `garage_integration authula_integration musr_e2e` tags |

### Test Harness (D-29 base — VERIFIED)
The existing build-tag live-stack harness is the exact base to extend: `//go:build db_integration` / `neo4j_integration` files, `envOrSkip(t, "AURA_DB_URL")` which **`t.Fatalf` under `$CI` when the var is unset** and `t.Skip` locally (`internal/db/db_test.go:34`). Composed DSNs: `AURA_DB_URL` (aura_app role), `AURA_DB_MIGRATE_URL` (aura_migrate role), `POSTGRES_PASSWORD`. **D-29 extends this** with Garage + Authula CI services and new tags — NOT testcontainers.

### Phase Requirements → Test Map
| Req | Behavior (observable) | Test Type | Automated Command | File Exists? |
|-----|-----------------------|-----------|-------------------|--------------|
| MUSR-01 | Two-identity cross-deny: B gets **404** on GET/list/search of A's conversation/approval/document/asset; **403** on delete/archive/resolve of a known A id | live E2E | `go test -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' -run TestTwoIdentityCrossDeny ./...` | ❌ Wave 0 |
| MUSR-01 | **RLS backstop:** a query WITHOUT the `*ForIdentity` filter still returns 0 foreign rows (via RLS + `SET LOCAL`) | integration | `go test -tags db_integration -run TestRLSBackstop ./internal/db` | ❌ Wave 0 |
| MUSR-01 | **Neo4j fail-closed:** empty `user_identifier` → 0 hits; B scoped search misses A's doc; A finds own | integration | `go test -tags neo4j_integration -run TestDocumentsFailClosed ./internal/documents` | ❌ Wave 0 (spike 085 harness is the template) |
| MUSR-02 | B-created Web conversation is owned by B and runs; owner is `identityctx.IdentityID(ctx)` | live E2E | part of `TestTwoIdentityCrossDeny` | ❌ Wave 0 |
| MUSR-03 | Session B cannot poll/kill session A's job; IDs are unguessable (crypto/rand) | unit + integration | `go test -run TestBackgroundJobOwnerDeny ./internal/agent/tools` | ❌ Wave 0 |
| MUSR-04 | TTL expiry records status + terminates the process group; age metric present | unit | `go test -run TestBackgroundJobTTLExpiry ./internal/agent/tools` | ❌ Wave 0 |
| MUSR-05 | Conversation delete (all 3 surfaces) cancels active work, expires pauses, evicts session tools, handles bg jobs, THEN deletes | unit + integration | `go test -run TestConversationDeleteLifecycle ./internal/runner` (extends `runner_evict_test.go`) | partial (`runner_evict_test.go` exists) |
| MUSR-06 | Authula default; provisioning→login→isolated-run happy path; break-glass CLI mints a working reset; **no session token in any URL/query string** | live E2E + audit | `TestProvisionLoginIsolatedRun` + a static grep gate for query-string tokens | ❌ Wave 0 |
| D-14/D-27 | **Saga idempotency/resumability:** re-run mid-failure converges to one consistent identity; de-provision is symmetric | integration | `go test -tags 'db_integration garage_integration' -run TestProvisioningSagaResumable ./internal/agui` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/<pkg>/` + `-race` on touched packages (CLAUDE.md post-edit gate).
- **Per wave merge:** `go test -tags 'db_integration neo4j_integration' -race ./...` on the live stack.
- **Phase gate:** full matrix incl. `garage_integration authula_integration musr_e2e` green under `$CI` (no-skip-as-green); owned-surface coverage ≥ 85% (CLAUDE.md floor); the two-identity cross-deny E2E is the D-29 acceptance gate.

### Property-based candidates (`rapid`)
- RLS: for a random set of `(identity, rows)`, a read under `identity_i` returns exactly `identity_i`'s rows (never a foreign row) — property over random identity/row partitions.
- Job IDs: minted IDs are collision-free and unguessable across N concurrent starts.
- Saga: for a random failure-injection point in the leg sequence, re-run converges to the same terminal state (idempotency).

### Wave 0 Gaps
- [ ] `internal/db/rls_integration_test.go` — RLS backstop (`db_integration`), covers MUSR-01
- [ ] `internal/documents/fail_closed_integration_test.go` — empty-identity + cross-deny (`neo4j_integration`); port the spike-085 harness
- [ ] `internal/agent/tools/shell_bg_owner_test.go` + `shell_bg_ttl_test.go` — MUSR-03/04
- [ ] `internal/runner/conversation_delete_lifecycle_test.go` — MUSR-05 (extend `runner_evict_test.go`)
- [ ] `internal/agui/provisioning_saga_resumable_test.go` — D-14/D-27 (`db_integration garage_integration`)
- [ ] `cmd/aura/.../two_identity_e2e_test.go` — the `musr_e2e` cross-deny gate (`db_integration neo4j_integration garage_integration authula_integration`)
- [ ] New `internal/objectstore/garageadmin` — Admin v2 client + its integration test (`garage_integration`)
- [ ] CI: add Garage (admin enabled) + Authula services to the live-stack workflow; export composed DSNs + `AURA_GARAGE_ADMIN_*`
- [ ] A static analysis / grep gate asserting no session token appears in a URL/query string (MUSR-06)

## Security Domain

> `security_enforcement` enabled (absent = enabled). This is a security-critical phase.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control (this phase) |
|---------------|---------|-------------------------------|
| V1 Architecture | yes | Kernel/storage-enforced isolation (RLS, Garage bucket, Neo4j EXISTS) — not app-only (D-08) |
| V2 Authentication | yes | Authula password + TOTP; admin-set initial password + forced change (D-15); CLI break-glass (D-16) |
| V3 Session Management | yes | `__Host-authula_session`, Secure+SameSite=Strict, 12h TTL; **no long-lived token in URLs** (MUSR-06) |
| V4 Access Control | yes | `RequireCapability` per route + RLS + Neo4j fail-closed + Garage scoped key; 404-read/403-mutate (D-06); no privilege escalation in provisioning (`validateNoEscalation` already enforces subset ⊆ creator grants) |
| V5 Input Validation | yes | sqlc `$`-params (no interpolation); Cypher bound `$`-params; capability-name grammar (`capNameRe`); `sanitizeFulltextQuery` |
| V6 Cryptography | yes | `crypto/rand` job IDs; hashed recovery answers (0023); never hand-roll; Garage admin token as bearer over internal net |
| V7 Error Handling/Logging | yes | Identity-keyed audit tables (D-28); `provisionFail` never echoes secrets; append-only recovery audit trigger (0023) |

### Known Threat Patterns for {Go monorepo + Postgres/Neo4j/Garage multi-tenant}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cross-identity data read via a forgotten `WHERE identity_id` | Information Disclosure | RLS backstop (`SET LOCAL` + policy) + `*ForIdentity` (D-07) |
| Unscoped `document_search` leaks foreign chunks (spike 085) | Information Disclosure | Unconditional Neo4j `EXISTS{}` fail-closed (D-09) |
| Shared-bucket prefix bypass | Information Disclosure / Tampering | Garage bucket-per-identity + scoped key (F-007) |
| Guessable job ID → cross-session poll/kill | Elevation of Privilege / Tampering | crypto-random IDs + `(identity,session)` owner check (MUSR-03) |
| Session-id-keyed shared maps collide across users | Information Disclosure | Key by `(identity, session)` (D-23) |
| Session token in URL (referer/log leak) | Information Disclosure | Headers/secure cookies only; query tokens ONLY for ≤1h setup bootstrap (MUSR-06) |
| Privilege escalation at provisioning (grant `*` or unheld caps) | Elevation of Privilege | `validateNoEscalation` — requested ⊆ creator grants, no `*` (already enforced) |
| Partial provisioning leaves orphaned Authula user / Garage bucket | Repudiation / resource leak | Journaled resumable saga + per-leg compensation (D-14/D-27) |
| Admin API exposed to host | Elevation of Privilege | `[admin]` bound internal-only; bearer token; no host port publish (Pitfall 3) |

## Sources

### Primary (HIGH confidence — code read this session)
- `internal/db/tx.go` (`WithTx` — the RLS SET LOCAL hook), `internal/db/migrations/0004,0005,0020,0023,0024` (schema + roles + seed `local`)
- `internal/identity/store.go` (`HasCapability` wildcard), `internal/agui/auth.go` (`RequireCapability`/`withPrincipal`/`identityctx`)
- `internal/documents/{types,indexer,search,retrieve,graphrag,service}.go` (the six retrieval queries + ingest)
- `internal/agui/onboarding_provision.go` (the cross-store saga template), `internal/webauth/{authula,identity_link,session_validate}.go`
- `cmd/aura/{serve_webui,serve_auth,objectstore}.go` (route/capability mounts, Authula wiring, objectstore bootstrap)
- `internal/agent/tools/{shell_bg,evict}.go` (background jobs + SessionEvictor), `internal/runner/runner_conversation.go`
- `internal/mcp/managed_config.go`, `internal/settings/settings.go`, `internal/channels/telegram/bot_dispatch_auth.go`
- `internal/db/queries/conversations.sql` (MUSR-01 gap), `internal/db/db_test.go` (harness `envOrSkip`), `go.mod` (versions), `compose.yaml` + `docker/garage/garage.toml` + `docker/aura/aura-garage-bootstrap.sh` (Garage)
- `.planning/spikes/085-document-ingest-tenancy/{README,main.go}` (VERIFIED live leak+fix), `.claude/skills/spike-findings-Aura/references/multiuser-per-identity-isolation.md` (blueprint)

### Secondary (MEDIUM confidence — web, cross-checked)
- [Garage Admin API reference](https://garagehq.deuxfleurs.fr/documentation/reference-manual/admin-api/) — `[admin]` block, bearer token, `/v2/` endpoints
- [Garage v2 admin API doc (garage-webui mirror)](https://git.rul.sh/khairul169/garage-webui/src/commit/9e71200452d2e49f27d688b4d87daf0b5faa9c95/docs/garage-admin-api.md) — CreateBucket/CreateKey/AllowBucketKey request shapes
- [PostgreSQL RLS in Go: multi-tenancy](https://dev.to/__8fa66572/postgresql-rls-in-go-architecting-secure-multi-tenancy-4ifm) + [Daniel Imfeld — Postgres RLS](https://imfeld.dev/notes/postgresql_row_level_security) + [Rico Fritzsche — RLS multi-tenancy](https://ricofritzsche.me/mastering-postgresql-row-level-security-rls-for-rock-solid-multi-tenancy/) — SET LOCAL vs SET, FORCE/table-owner caveat, `set_config(is_local=true)`

### Tertiary (LOW confidence — to verify at build time)
- Exact Garage v2 `CreateKey` response field names (`accessKeyId`/`secretAccessKey`) — confirm against the Garage v2 OpenAPI JSON spec (A1)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every version read from `go.mod`; no new external deps
- Architecture (RLS carrier, Neo4j fail-closed, saga extension, flag rollout): HIGH — grounded in real seams (`WithTx`, `onboarding_provision.go`, the six documents queries) + a live-proven spike (085)
- Garage Admin API: MEDIUM — endpoints/auth/config CITED; exact JSON field names to verify (A1)
- Pitfalls: HIGH — the RLS-on-pooled-reads, backfill-before-flip, guessable-job-IDs, and MERGE/MATCH-WITH pitfalls are each grounded in a specific verified file or spike

**Research date:** 2026-07-05
**Valid until:** ~2026-08-04 (stable; the codebase seams are the volatile input — re-verify file line numbers if execution slips a milestone)
