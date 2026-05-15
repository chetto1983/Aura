# Phase01B Plan - Establish Identity and Capability Grants

Status: Phase01B1 closed with local gates and subagent verification. Phase01B2
allowlist backfill is implemented and Go-verified locally. Phase01B3 and
Phase01B4 are container-verified. Phase01B5, Phase01B6, and Phase01B7 are
locally verified and container-updated. The parent closure verifier has passed
local and live auth-boundary gates, repaired Chat Hub actor persistence, and is
blocked only on the live LLM marker probe until the configured provider
credential returns a valid authenticated response.

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
| Phase01B5 | Move Telegram login/bootstrap/approval onto identity grants while keeping allowlist parity. | `internal/telegram/access.go`, `internal/telegram/bot_test.go`, `internal/api/auth` | Configured allowlist, persisted allowlist, first-run bootstrap, and approved users all mint tokens only after deterministic Telegram identity/grants exist; pending/deny behavior is unchanged. |
| Phase01B6 | Add delegated actor creation for cron and swarm child runs. | `internal/cron`, `internal/channels/cron`, `internal/agent/tools/swarm`, `internal/swarm`, `cmd/aura` | Done; child/cron actors cannot exceed parent or owner grant envelope. |
| Phase01B7 | Wire auth denials into Phase01A run/audit events and run broader behavior gates. | `internal/identity`, `internal/storage/runs`, `internal/chat`, `internal/agent/tools/registry`, `internal/channels/telegram`, `cmd/aura` | Done locally; denials are durable metadata events without raw payloads. |

## Phase01B5 Slice Contract

Scope:

- Preserve the existing allowlist compatibility plane:
  the configured Telegram allowlist, persisted `allowed_users`, first-run
  bootstrap, pending approval, and deny keep their current user-visible
  behavior.
- Add identity parity before token issuance on Telegram login paths. A token may
  still identify a Telegram user ID for compatibility, but the matching
  principal, channel account, session actor, and grants must exist first.
- Do not copy configured allowlist entries into `allowed_users` just to satisfy
  identity lookup. The configured allowlist remains DB/settings-backed
  configuration; identity/grants become the authority layer for capabilities.
- Keep `SourceE2EBootstrap` excluded from real Telegram owner notifications and
  identity backfill.

Verification is benchmark-driven in `benchmark.md`; no smoke-only Telegram
claim can close this slice.

## Phase01B6 Slice Contract

Scope:

- Add a reusable identity delegation helper that creates `cron` or `swarm`
  actors as children of an already authenticated parent actor.
- A delegated actor may receive only capabilities the parent actor is currently
  authorized to use. Parent principal grants may authorize an unparented session
  actor, but child actors receive direct actor grants only.
- Cron agent-job execution must run under a delegated `cron` actor. Missing
  identity delegator or missing parent actor is fail-closed before `JobRunner`
  executes. Phase 8 still owns schedule-fire workflow/run request semantics.
- Swarm tools must require `swarm.spawn`, then create delegated `swarm` actors
  for worker assignments and execute each worker under that child actor.
- Existing tool allowlists remain surface constraints. The delegated grants make
  those workers fail closed when the parent lacks the requested execution
  capability.

Non-goals:

- Do not add schedule-fire tables, missed-run policy, outbox delivery, or
  RunGraph topology changes; those belong to Phase 8.
- Do not wire authorization denials into run/audit events; that remains
  Phase01B7.
- Do not remove `UserID` compatibility fields from cron jobs or swarm
  assignments in this slice.
- Do not grant write-capable swarm behavior. Existing role/tool allowlists and
  proposal-only constraints remain in force.

Owned files:

- `D:/Aura/internal/identity/`
- `D:/Aura/internal/cron/dispatch.go`
- `D:/Aura/internal/cron/dispatch_test.go`
- `D:/Aura/internal/channels/cron/loop.go`
- `D:/Aura/internal/agent/tools/swarm/tools.go`
- `D:/Aura/internal/agent/tools/swarm/tools_test.go`
- `D:/Aura/internal/swarm/manager.go`
- `D:/Aura/internal/swarm/types.go`
- `D:/Aura/internal/swarm/manager_test.go`
- `D:/Aura/cmd/aura/app.go`

Verification is benchmark-driven in `benchmark.md`; no Phase01B6 claim is
complete until SQLite identity rows and authorization decisions prove the
delegation envelope.

## Phase01B7 Slice Contract

Scope:

- Use the existing Phase01A `runs`, `run_events`, and `audit_events` schema as
  the durable target. Add storage methods only if the current store lacks a safe
  way to append metadata-only denial evidence.
- Preserve `authz_decisions` as the first authorization decision table. B7 adds
  run/audit correlation for denied decisions; it does not replace the
  `authz_decisions` row.
- Add context-level run provenance for authorization calls: `run_id`,
  optional `event_id`, and an optional denial recorder. The `identity` package
  must not import `internal/storage/runs`; use a narrow interface or context
  seam.
- When `identity.Authorize` denies and a denial recorder is present, record a
  metadata-only `authorization_denied` run event and an
  `authorization_denied` audit event. The payload may include decision ID,
  actor ID, capability, resource type/id, reason, and redaction level only.
  It must not include raw prompts, messages, tool arguments, tool outputs,
  bearer tokens, provider payloads, or stack traces.
- Chat Hub dispatch must attach the run ID and recorder to the agent-loop
  context when a `LifecycleStore` supports denial recording. Tool registry
  authorization must pass context run provenance into `identity.Authorize` so
  tool denials inside a run become correlated durable events.
- Production Hub construction must pass the shared run store into Telegram and
  cron Hubs, rather than leaving run/audit denial recording only in tests.

Non-goals:

- Do not create fake runs merely to record pre-run API denials. A handler that
  denies before a run exists may keep its `authz_decisions` row and, if a safe
  audit writer exists, an audit event with an empty `run_id`.
- Do not persist raw request bodies, tool argument values, raw tool results,
  LLM prompts, or chain-of-thought as denial evidence.
- Do not redesign web chat onto the full Hub path in this slice. That belongs
  to later channel/runtime consolidation unless B7 cannot prove a run-bound
  denial without it.
- Do not add schedule-fire tables, workflow outbox behavior, missed-run policy,
  or swarm RunGraph topology; those remain Phase 8.

Owned files:

- `D:/Aura/internal/identity/context.go`
- `D:/Aura/internal/identity/store.go`
- `D:/Aura/internal/identity/store_test.go`
- `D:/Aura/internal/storage/runs/store.go`
- `D:/Aura/internal/storage/runs/store_test.go`
- `D:/Aura/internal/chat/hub.go`
- `D:/Aura/internal/chat/hub_test.go`
- `D:/Aura/internal/agent/tools/registry/registry.go`
- `D:/Aura/internal/agent/tools/registry/registry_test.go`
- `D:/Aura/internal/channels/telegram/invocation_builder.go`
- `D:/Aura/internal/telegram/deps.go`
- `D:/Aura/cmd/aura/app.go`
- `D:/Aura/cmd/aura/main.go`

Verification is benchmark-driven in `benchmark.md`; B7 is not complete until
SQL evidence shows a denied authorization decision, a correlated
`run_events` row, and a correlated `audit_events` row with metadata-only
payload.

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
| Durable authz decision table | Phase01B1 and B7 | `benchmark.md` decision tests | `source.md` PRD/ADR audit | B1 and B7 passed |
| Telegram allowlist migration | Phase01B2/B5 | `benchmark.md` Telegram parity | `source.md` Telegram audit | passed |
| Dashboard token authenticates actor | Phase01B3 | `benchmark.md` dashboard tests | `source.md` API auth audit | passed |
| `Authorize(actor, capability, resource)` boundary | Phase01B1/B3/B4 | `benchmark.md` identity/tool tests | `source.md` OWASP/OpenFGA audit | B1/B3/B4 passed |
| Tool allowlists mapped to capability checks | Phase01B4 | `benchmark.md` tool fail-closed test | `source.md` tool registry audit | passed |
| Delegated actors for cron/swarm | Phase01B6 | `benchmark.md` delegated actor tests | `source.md` cron/swarm audit | passed |
| Authorization denials as run/audit events | Phase01B7 | `benchmark.md` event/audit tests | `source.md` Phase01A run-event audit | passed |

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
- `internal/channels/cron/loop.go` or `internal/cron/dispatch.go` can only be
  wired through a private cron authority path instead of the shared identity
  delegator.
- `cmd/aura/app.go` cannot pass the existing shared auth/identity store into
  cron and web-chat delegation without creating a second store or separate
  permission model.
- Swarm child execution cannot carry delegated actor context to worker tool
  execution without changing Phase 8 RunGraph topology.
- Production Telegram or cron Hubs cannot receive the shared run store as their
  `LifecycleStore` without a broad channel/runtime redesign.
- The denial recorder would need to store raw prompts, request bodies, tool
  argument values, raw tool results, bearer tokens, or stack traces to prove
  the B7 benchmark.
