# Phase01B Plan - Establish Identity and Capability Grants

Status: Phase01B1 closed with local gates and subagent verification. Phase01B2
allowlist backfill is implemented and Go-verified locally. Phase01B3 and
Phase01B4 are container-verified. Phase01B5 is the next separate
implementation slice.

## Goal

Give every run, tool call, cron job, and swarm child a single authority model:
authentication resolves an actor, and `identity.Authorize` decides whether that
actor may perform a capability on a resource.

## Canonical Store

SQLite in the main Aura database is canonical for principals, channel accounts,
actors, capability grants, and authorization decisions. Cache, tool indexes,
logs, and prompt-visible tool allowlists are not authority.

## Non-Goals

- Do not remove existing Telegram/dashboard allowlists until equivalent
  capability checks pass.
- Do not add OpenFGA, Zanzibar, Redis, or an external policy service.
- Do not implement Phase 8 swarm topology or full agent-team scheduling here.
- Do not migrate wiki/memory write policy beyond the minimal capability hooks
  needed to prove the boundary.

## Phase01B1 - First Bounded Implementation Slice

Purpose: land the durable schema and minimal identity package without changing
user-visible access behavior.

Owned files:

- `D:/Aura/internal/db/migrations/migrations.go`
- `D:/Aura/internal/db/migrations/migrations_test.go`
- `D:/Aura/internal/identity/` (new package)

Implementation steps:

1. Add migration v7 and fresh-schema SQL for:
   - `principals`
   - `channel_accounts`
   - `actors`
   - `capability_grants`
   - `authz_decisions`
2. Add a non-breaking `runs.actor_id TEXT NOT NULL DEFAULT ''` column and
   `idx_runs_actor_updated`, but do not require callers to populate it yet.
3. Add `internal/identity` constants and small data types for principal kinds,
   actor kinds, capability names, resource references, decisions, and grant
   constraints.
4. Add a minimal store/authorizer surface:
   - create or resolve principal
   - create or resolve channel account
   - create actor
   - create/revoke grant
   - `Authorize(ctx, actorID, capability, resource)` default-deny
   - record `authz_decisions`
5. Add tests for schema convergence, foreign-key enforcement through
   `auradb.Open`, default deny, active grant allow, revoked grant deny, and
   expired grant deny.

Verification for this slice is listed in `benchmark.md`.

## Later Slices

| Slice | Purpose | Main Files | Exit Gate |
| --- | --- | --- | --- |
| Phase01B2 | Backfill/migrate current allowlisted Telegram users into principals, channel accounts, session actors, and owner/user grants while preserving `allowed_users`. | `internal/api/auth`, `internal/identity`, migrations/tests | Done locally; existing `/start`, `/login`, pending approval, and token tests keep behavior. |
| Phase01B3 | Resolve dashboard bearer tokens into actor context and remove/constrain `/chat` body user override under auth. | `internal/api/auth`, `internal/api/chat.go`, `internal/api/router.go` | Dashboard token cannot impersonate another user or bypass missing grant. |
| Phase01B4 | Add tool required-capability metadata and a registry/tool execution authorization guard. | `internal/agent/tools/registry`, selected tool tests | Done; visible tool fails closed without required grant in local and containerized registry tests. |
| Phase01B5 | Move Telegram login/bootstrap/approval onto identity grants while keeping allowlist parity. | `internal/telegram/access.go`, `internal/telegram/bot_test.go`, `internal/api/auth` | Env allowlist, persisted allowlist, first-run bootstrap, and approved users all mint tokens only after deterministic Telegram identity/grants exist; pending/deny behavior is unchanged. |
| Phase01B6 | Add delegated actor creation for cron and swarm child runs. | `internal/cron`, `internal/agent/tools/swarm`, `internal/swarm` | Child/cron actors cannot exceed parent or owner grant envelope. |
| Phase01B7 | Wire auth denials into Phase01A run/audit events and run broader behavior gates. | `internal/chat`, `internal/storage/runs`, `internal/api`, `internal/agent/tools` | Denials are durable metadata events without raw payloads. |

## Phase01B5 Slice Contract

Scope:

- Preserve the existing allowlist compatibility plane:
  `TELEGRAM_ALLOWLIST`, persisted `allowed_users`, first-run bootstrap, pending
  approval, and deny keep their current user-visible behavior.
- Add identity parity before token issuance on Telegram login paths. A token may
  still identify a Telegram user ID for compatibility, but the matching
  principal, channel account, session actor, and grants must exist first.
- Do not persist env-only allowlist entries into `allowed_users` just to satisfy
  identity lookup. Env allowlist remains configuration; identity/grants become
  the authority layer for capabilities.
- Keep `SourceE2EBootstrap` excluded from real Telegram owner notifications and
  identity backfill.

Verification is benchmark-driven in `benchmark.md`; no smoke-only Telegram
claim can close this slice.

## Capability Seed Set

Use explicit strings, not roles, as the authorization API:

- `api.chat`
- `dashboard.read`
- `dashboard.write`
- `tool.execute`
- `tool.execute.<tool_name>` when a narrower grant is needed
- `skills.install`
- `skills.delete`
- `settings.write`
- `cron.create`
- `cron.run`
- `swarm.spawn`
- `memory.user.write`
- `wiki.write`

Roles may bootstrap bundles of these grants, but `Authorize` checks the
capability string, resource, constraints, expiry, and revocation state.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Durable principal table | Phase01B1 | `benchmark.md` schema checks | `source.md` local DB audit | B1 closed |
| Durable channel account table | Phase01B1 | `benchmark.md` schema checks | `source.md` auth/telegram audit | B1 closed |
| Durable actor table | Phase01B1 | `benchmark.md` schema checks | `source.md` chat/run audit | B1 closed |
| Durable capability grant table | Phase01B1 | `benchmark.md` grant tests | `source.md` ADR-020/external audit | B1 closed |
| Durable authz decision table | Phase01B1 and B7 | `benchmark.md` decision tests | `source.md` PRD/ADR audit | B1 closed; B7 planned |
| Telegram allowlist migration | Phase01B2/B5 | `benchmark.md` Telegram parity | `source.md` Telegram audit | B2 local backfill closed; B5 wiring planned |
| Dashboard token authenticates actor | Phase01B3 | `benchmark.md` dashboard tests | `source.md` API auth audit | passed |
| `Authorize(actor, capability, resource)` boundary | Phase01B1/B3/B4 | `benchmark.md` identity/tool tests | `source.md` OWASP/OpenFGA audit | B1/B3/B4 passed |
| Tool allowlists mapped to capability checks | Phase01B4 | `benchmark.md` tool fail-closed test | `source.md` tool registry audit | passed |
| Delegated actors for cron/swarm | Phase01B6 | `benchmark.md` delegated actor tests | `source.md` cron/swarm audit | planned |
| Authorization denials as run/audit events | Phase01B7 | `benchmark.md` event/audit tests | `source.md` Phase01A run-event audit | planned |

## Implementation Gates

- Telegram, web/API, cron, and swarm fixtures resolve an actor.
- Dashboard token cannot bypass a missing capability.
- A visible tool still fails closed when the actor lacks its required
  capability.
- A child swarm actor cannot exceed the parent actor grant envelope.
- A revoked grant stops future runs without rewriting history.
- Authorization denials are recorded as metadata-only decisions/events.

## Stop Conditions

Stop and update this plan before implementation if:

- `internal/api/auth` has uncommitted user changes in the same lines needed for
  token/allowlist migration.
- Fresh/upgraded schema convergence fails after the migration.
- Existing Telegram login/pending approval behavior changes before capability
  parity tests are written.
- A proposed shortcut makes tokens, tool allowlists, cron recipients, or swarm
  role names the final authority.
