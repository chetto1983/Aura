# Phase01B Source Audit

Status: source-audited. Phase01B1-B7 have implementation evidence in
`benchmark.md`. The 2026-05-15 parent closure verifier passed local and live
auth-boundary evidence, repaired Chat Hub `actor_id` run/event persistence, and
is blocked only on the live LLM provider credential required for the exact chat
marker probe.

## Canonical Requirements

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 1B | Durable principals, channel accounts, actors, grants, authz decisions, delegated actors, and fail-closed capability checks. | Treat Phase01B as identity/capability foundation, not a Telegram-only auth patch. | Do not keep dashboard bearer tokens or Telegram user IDs as final authority. | read |
| `D:/Aura/prd.md` identity section | API authenticates into an actor; `identity` owns authorization. | Every durable run should resolve to an actor; channel account IDs are not principal IDs. | Do not let API handlers own permission semantics. | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-020 | Stable Principals plus scoped CapabilityGrants. | Roles may bootstrap grants, but decisions check capability, resource, constraints, and delegation chain. | Do not hardcode roles/allowlists as the final model. | read |
| `D:/Aura/AGENTS.md` | Preserve cache/log/identity architecture rules and Codex loop. | Keep one bounded slice and verify locally before marking complete. | Do not execute old wave plans as queues. | read |

## Local Source Audit

| Source | Current Behavior | Phase01B Use | Risk / Required Guard |
| --- | --- | --- | --- |
| `D:/Aura/internal/db/migrations/migrations.go` | Current schema has `api_tokens`, `allowed_users`, `pending_users`; Phase01A added `runs`, `run_events`, `audit_events`. | Add migration v7 and fresh schema entries for identity tables; add a run actor linkage in a controlled step. | Fresh and upgraded schemas must converge. |
| `D:/Aura/internal/db/db.go` | `auradb.Open` enables `foreign_keys=ON`. | Foreign keys are acceptable for identity tables when using the shared open path. | Tests must use `testutil.OpenTestDB`/`auradb.Open`, not raw `sql.Open` production paths. |
| `D:/Aura/internal/api/auth/store.go` | Dashboard tokens store `user_id`; `allowed_users` is the allowlist source; approvals add users and mint tokens. | Backfill allowed users into principals/channel accounts/grants while preserving current login behavior. | Token lookup must authenticate an actor, not imply permission by itself. |
| `D:/Aura/internal/api/auth/middleware.go` | `RequireBearer` injects a user ID and checks allowlist. | Later slice should inject an actor context after token lookup. | Missing grant must still return 401/403 without leaking token state. |
| `D:/Aura/internal/api/chat.go` | Authenticated `/chat` defaults to context user, but request body can override `user_id`. | Capability slice must remove or constrain this override when auth is active. | Dashboard token must not impersonate arbitrary users. |
| `D:/Aura/internal/api/router.go` | Router wraps all API routes with bearer auth when `deps.Auth` exists. | Good authentication boundary; add authorization inside handlers or a route capability wrapper. | Bearer-authenticated must not mean globally authorized. |
| `D:/Aura/internal/api/skills_write.go` | Skill install/delete uses global `SkillsAdmin`. | Replace or wrap with `skills.install` / `skills.delete` capability checks. | Keep `SkillsAdmin` as migration compatibility only until capability parity exists. |
| `D:/Aura/internal/telegram/access.go` | `/start`, `/login`, pending approval, and owners depend on configured/persisted allowlist state. | Migrate allowlisted Telegram users into channel accounts and owner/user principals. | Do not remove allowlist checks before equivalent capability tests pass. |
| `D:/Aura/internal/agent/tools/registry/context.go` | Tool context carries `user_id` and current-turn allowed tool names. | Add actor context without breaking current tool plumbing. | Tool execution must fail closed if actor lacks required capability even when name is visible. |
| `D:/Aura/internal/agent/tools/registry/registry.go` | Registry has categories, visible definitions, and redacted arg-key logging. | Tool metadata can gain required capability/risk without exposing raw args. | Do not log tool argument values or raw payloads. |
| `D:/Aura/internal/agent/tools/sets/toolsets.go` | Toolsets and role presets constrain cron/swarm/tool surfaces by name. | Treat these as grant constraints and migration compatibility. | Do not let name allowlists replace capability decisions permanently. |
| `D:/Aura/internal/agent/tools/swarm/tools.go` | Swarm tools pass `CreatedBy`/`UserID` and role tool allowlists. | Later slice should create delegated child actors with subset grants. | Child actors must not exceed parent grants. |
| `D:/Aura/internal/cron/dispatch.go` | Manual `RunNow` agent jobs still call `JobRunner.RunJob` with `task.RecipientID` and safe tool allowlists. | Phase01B6 wraps this path with a delegated `cron` actor derived from caller context or recipient identity. | Missing identity delegator or missing parent actor must fail closed before the runner executes; full schedule-fire workflow belongs to Phase 8. |
| `D:/Aura/internal/channels/cron/loop.go` | Hub-routed cron agent jobs normalize into `chat.InboundMessage` and call `cron.JobRunner` from a silent channel loop. | Phase01B6 should pass delegated actor context through this loop when available so scheduled jobs use the same tool authorization boundary. | Do not make cron own a private agent loop or direct delivery path. |
| `D:/Aura/internal/swarm/manager.go` and `internal/swarm/types.go` | Swarm manager persists flat run/task rows and executes each assignment through `AgentRunner.Run(ctx, agent.Task)`. | Phase01B6 should let assignments carry a delegated actor ID and run each worker context as that actor. | Do not redesign swarm into Phase 8 RunGraph or persist raw child transcripts. |
| `D:/Aura/internal/identity/store.go` | Existing `Authorize` already forces delegated and parented actors to use direct actor grants. | Add a small delegation helper that verifies the parent authorization envelope before creating child actor grants. | Do not bypass `Authorize` by copying roles or allowlists directly into child authority. |
| `D:/Aura/cmd/aura/app.go` | Composition root wires auth, cron handler, cron Hub loop, web chat, swarm manager, and tool registry. | Phase01B6 should pass the shared auth/identity store into cron delegation and ensure web chat contexts expose both authorizer and delegator. | Do not create a second identity store, separate cron authority path, or app-only permission logic. |
| `D:/Aura/internal/chat/types.go` and `internal/storage/runs` | Runs already carry `PrincipalID`; run events have `actor_id` in storage schema. | Use Phase01A run/event foundation as the durable decision/audit target. | Actor resolution must happen before future privileged events. |
| `D:/Aura/internal/storage/runs/store.go` | The store can append `run_events` but has no typed `audit_events` writer yet. | Add a narrow metadata-only denial writer that appends `authorization_denied` run and audit evidence without schema churn. | Do not expose raw prompts, tool arguments, tool outputs, request bodies, or tokens in audit payloads. |
| `D:/Aura/internal/chat/hub.go` | The Hub creates durable runs when a `LifecycleStore` is configured and owns the run context passed to the agent loop. | Attach run provenance and a denial recorder to the agent-loop context so run-bound tool denials can correlate to Phase01A events. | Do not make each channel adapter invent a separate audit path. |
| `D:/Aura/internal/agent/tools/registry/registry.go` | Registry authorization already denies before `Tool.Execute` but does not pass run/event provenance into `identity.Authorize`. | Pass context run IDs into authorization and let the identity denial hook record metadata-only evidence. | Do not log or persist argument values, raw result previews, or tool payloads to prove denial. |
| `D:/Aura/internal/channels/telegram/invocation_builder.go` and `D:/Aura/cmd/aura/app.go` | Production Telegram and cron Hubs are currently created without a lifecycle store. | Wire the shared run store into Hub construction so denial recording is not test-only. | Do not create a second run database or a channel-private trace store. |

## External Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| Google Zanzibar paper / research page: `https://research.google/pubs/zanzibar-googles-consistent-global-authorization-system/` | Relationship-based authorization at scale. | Use relationship/tuple thinking as design inspiration for grants and resource scope. | Do not introduce a Zanzibar service or distributed consistency model in Phase01B. | read |
| OpenFGA docs: `https://openfga.dev/docs/concepts` | Authorization models, relationship tuples, contextual constraints. | Keep capability grants explicit and resource-scoped. | Do not add OpenFGA as a dependency. | read |
| OWASP Authorization Cheat Sheet: `https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html` | Deny by default, least privilege, validate permissions on every request. | Make `Authorize` default-deny and route/tool checks explicit. | Do not rely on hidden UI state or authentication alone. | read |
| SQLite foreign keys: `https://www.sqlite.org/foreignkeys.html` | Foreign key behavior depends on enforcement being enabled per connection. | Rely on `auradb.Open` PRAGMA and test FK behavior through Aura's DB opener. | Do not assume raw `sql.Open` enforces identity FKs. | read |

## Implementation Questions Closed By Audit

- The first Phase01B code slice should be schema plus a small `internal/identity`
  boundary, not direct Telegram/API rewrites.
- Existing `allowed_users` and `api_tokens.user_id` remain compatibility
  sources during migration; they are not final authority.
- Tool name allowlists remain visible-surface constraints; Phase01B4 added the
  registry capability guard so authenticated execution also requires
  `tool.execute` or a declared tool-specific capability.

## Remaining Verification Gap

- Parent closure verifier ran on 2026-05-15 and found no remaining local
  Phase01B code/auth blocker after the Chat Hub `actor_id` repair.
- The parent is still not complete because the live bearer chat marker returned
  `500`; container logs show the configured LLM provider rejected the request
  with `401 Missing Authentication header`. Rerun the marker after fixing the
  provider credential path.
