# Phase01B Benchmark

Status: Phase01B1 is closed: local gates passed and subagent verification found
no remaining Phase01B1 blockers. Phase01B2 allowlist backfill is implemented
and Go-verified locally. Phase01B3 and Phase01B4 are container-verified.
Phase01B5, Phase01B6, and Phase01B7 are locally verified and
container-updated. The 2026-05-15 parent closure verifier passed local code
gates and live auth-boundary probes, repaired missing Chat Hub `actor_id`
persistence, repaired the DB-backed provider secret path, removed runtime
`.env`, and passed the exact live chat-marker probe. A later interactive debug
repair removed the legacy web `/api/chat` runner and verified that the route now
uses `chat.Hub` plus the shared `runs.Store` in repo-level tests and a live
container `/api/chat` probe. A follow-up stability pass propagated the active
tool allowlist into the web tool execution context, restored the local
CGO/race toolchain with `D:/tmp/w64devkit`, and passed a three-turn live web
chat probe. Phase01B is closed for the identity/capability slice.

## Baseline Commands

Run before the first Phase01B code edit if the worktree remains dirty:

```powershell
go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron
```

This catches unrelated auth/cron/telegram drift before identity changes begin.

## Phase01B1 Required Gates

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Migration registration | `go test ./internal/db/migrations -run TestRegisteredReturnsCopy` plus full migration package | v7 registered in order | passed via `go test ./internal/db/migrations ./internal/identity` | passed |
| Fresh schema | `go test ./internal/db/migrations -run TestRunCreatesCurrentFreshSchema` | identity tables and indexes exist | passed via targeted migration run | passed |
| Upgrade convergence | `go test ./internal/db/migrations -run TestFreshAndUpgradedSchemasConverge` | fresh and upgraded schemas match | passed via targeted migration run | passed |
| Identity store unit tests | `go test ./internal/identity` | default deny, actor/principal grant allow, delegated actor direct grants, disabled principal deny, resource scopes, revoked/expired deny, decision rows recorded | passed | passed |
| Narrow package gate | `go test ./internal/db/migrations ./internal/identity` | green | passed | passed |
| Build | `go build ./...` | green | passed | passed |
| Subagent goal verifier | read-only `gsd-verifier` | PASS with no B1 blockers | PASS 10/10 for Phase01B1 only | passed |
| Subagent code-risk recheck | read-only explorer | PASS with no B1 blockers | PASS 9.5/10; remaining notes now map to B3-B7 integration work after the B2 backfill slice | passed |

Additional targeted checks run:

```powershell
go test ./internal/db/migrations -run "TestRunCreatesCurrentFreshSchema|TestFreshAndUpgradedSchemasConverge|TestIdentityCapabilityTablesAreUsable"
go test ./internal/identity -run "TestAuthorize|TestCreateOrResolveChannelAccount"
go vet ./internal/db/migrations ./internal/identity
go build ./internal/db/migrations ./internal/identity
go test ./internal/db/migrations ./internal/identity ./internal/storage/runs ./internal/chat
go clean -testcache
go test -count=1 ./internal/db/migrations ./internal/identity
```

Broader checks:

```powershell
go build ./...
go vet ./...
go test ./internal/telegram
go test ./...
go test -count=1 ./...
```

All broader checks passed. The final full run used `go test -count=1 ./...`
after clearing the Go test cache.

## Phase01B1 Subagent Closure

- First code-risk verifier blocked at 6/10 on delegated actor inheritance,
  channel-account/principal mismatch, and disabled-principal authorization.
- Repairs landed in `internal/identity`: principal status is checked before
  grant lookup, only unparented session actors inherit principal grants,
  delegated/parented actors require direct actor grants, actor creation rejects
  channel-account/principal mismatch, and grant subjects are validated before
  insert.
- Goal verifier returned PASS 10/10 for Phase01B1 and explicitly warned not to
  close the Phase01B parent.
- Final code-risk recheck returned PASS 9.5/10 with no B1 blockers; remaining
  notes now map to B3-B7 integration work after the B2 backfill slice.

## Phase01B2 Required Gates

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Existing persisted allowlist backfill | migration v8 plus upgrade fixture assertions | `allowed_users` rows become deterministic Telegram principals, channel accounts, session actors, and grants; e2e bootstrap rows are excluded | passed via `go test ./internal/db/migrations` | passed |
| Auth bootstrap identity parity | `go test ./internal/api/auth` | first-run bootstrap still preserves allowlist behavior and creates owner identity rows/grants | passed | passed |
| Pending approval identity parity | `go test ./internal/api/auth` | approval still closes pending users and creates human identity rows/grants | passed | passed |
| Identity authorizer parity | `go test ./internal/identity` | backfilled session actors inherit principal grants; owner grants include admin capabilities, user grants do not | passed | passed |
| Phase01B benchmark | `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron` | green | passed | passed |
| Shared quality gate | `go build ./...`; `go vet ./...`; `go test ./...` | green | passed | passed |

Phase01B2 intentionally did not wire dashboard bearer requests into actor
context, enforce tool capabilities, create cron/swarm delegated actors, or
persist authorization denial events. Those remain Phase01B3-B7.

## Phase01B3 Gates

Status: implemented, locally Go-verified, and container E2E verified on
2026-05-15.

Phase01B3 owns dashboard bearer actor context for web/API chat. Cron
delegation, swarm delegation, tool required-capability metadata, and
metadata-only denial events remain Phase01B4-B7 unless a later plan narrows
them into this slice.

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Bearer token resolves actor context | `go test ./internal/api ./internal/api/auth` with `TestChatBearerActorContext` | request context contains the token owner's actor ID; handler uses that actor for chat/run input | passed; `recordingChatService` observed `actor:telegram:session:alice` from request context | passed |
| Request body cannot impersonate another user under bearer auth | `go test ./internal/api` with `TestChatBearerRejectsBodyUserOverride` | response rejects or ignores the body override according to the chosen implementation; no run/chat state is attributed to user B | passed; bearer `alice` plus body `bob` returns `403 Forbidden`, chat service is not called | passed |
| Missing grant fails closed with explicit status | `go test ./internal/api ./internal/api/auth` with `TestChatBearerMissingGrantDenied` | bearer-authenticated actor without `api.chat` grant receives `403 Forbidden`, and no chat run proceeds; `401 Unauthorized` remains reserved for missing or invalid bearer authentication | passed; revoked `api.chat` grant returns `403 Forbidden`; missing token path returns `401 Unauthorized` via `TestChatBearerRequiresTokenBeforeGrant` | passed |
| SQL evidence proves actor/grant/authz path | API test fixture plus direct SQLite inspection of `principals`, `channel_accounts`, `actors`, `capability_grants`, and `authz_decisions` | rows show token principal -> channel account -> session actor -> grant decision; denial path records an authz decision without raw payload | passed; tests assert principal/account/actor/grant row counts plus allow and missing-grant denial decision rows | passed |
| Repeated run is idempotent | `TestChatBearerActorContext` repeated chat request plus SQLite row-count assertions | repeated bearer chat setup does not duplicate principals, channel accounts, actors, or grants; only expected authz decision rows are appended | passed; principal/account/actor/grant counts stay at 1, authz allow decisions advance from 1 to 2 | passed |
| Container image updated | `docker compose build aura`; `docker compose up -d --no-deps aura` | rebuilt image contains current Phase01B3 code and running container is recreated without resetting volumes or sidecars | passed; `aura:local` image `sha256:11729a8095ecf219efc70582d01d24abe8a3e771ef239c1ef1847a295b0d1ba8`, container started `2026-05-15T13:09:17Z` | passed |
| Container health and auth boundary | `GET /health`; `GET /api/health` without bearer; `GET /api/health` with `AURA_E2E_TOKEN`; `POST /api/chat` without bearer | public health remains available; API stays bearer-gated | passed; statuses `200`, `401`, `200`, and `401` respectively | passed |
| Container chat E2E | `POST /api/chat` with `AURA_E2E_TOKEN` and message asking for exact marker | real container chat path returns marker, reports metrics, and does not call tools | passed; reply `AURA_E2E_PHASE01B3_OK`, client latency 2829 ms, API latency 2635 ms, `llm_calls=1`, `tool_calls=0`, `tokens=8654` | passed |
| Container impersonation denial | `POST /api/chat` with bearer token plus mismatched body `user_id` | request returns `403 Forbidden`; no chat execution for the body user | passed; status `403` | passed |
| Container SQL evidence | `sqlite3 /data/aura.db` inside `aura` container | live DB has identity rows/grant and records `api.chat` authorization decision | passed; `principals=1`, `channel_accounts=1`, `actors=1`, `api_chat_grants=1`, latest `authz_decisions` row is `allow|api.chat|api|chat|active_grant` | passed |
| Shared quality gate | `go test ./internal/api ./internal/api/auth`; `go test ./internal/...`; `go build ./...`; `go vet ./...`; `go test ./...`; container E2E probes above | green | passed on 2026-05-15 | passed |

## Phase01B4 Gates

Status: implemented, repo-verified, container-updated, and containerized
registry fail-closed verified on 2026-05-15.

Phase01B4 owns tool required-capability metadata and the registry execution
authorization guard. Telegram identity parity and cron/swarm delegation were
closed in later slices; denial-event run/audit persistence remains Phase01B7.

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Registry fails closed without tool grant | `go test ./internal/agent/tools/registry -run TestRegistryExecuteRequiresIdentityGrant -count=1` | a visible registered tool is not executed when the actor lacks `tool.execute`; SQLite `authz_decisions` records `deny|tool.execute|tool|fake|missing_grant` | passed locally and in `docker compose run --rm test go test ./internal/agent/tools/registry -run "TestRegistryExecuteRequiresIdentityGrant|TestRegistryExecuteUsesToolSpecificCapability" -count=1` | passed |
| Tool-specific capability is honored | `go test ./internal/agent/tools/registry -run TestRegistryExecuteUsesToolSpecificCapability -count=1` | broad `tool.execute` does not satisfy a tool that declares `tool.execute.fake`; the tool runs only after the narrow grant exists | passed locally and in the containerized test service | passed |
| Registry fails closed without authorizer | `go test ./internal/agent/tools/registry -run TestRegistryExecuteWithoutAuthorizerFailsClosed -count=1` | a registered tool is not executed when no identity authorizer is present; error wraps `identity.ErrUnauthorized` | passed on 2026-05-15; tool execution count stayed zero | passed |
| Dashboard chat passes identity authorizer into the agent path | `go test ./internal/api ./internal/api/auth` with `TestChatBearerActorContext` | authenticated chat context contains the deterministic actor ID and an `identity.Authorizer` before `ChatService.Chat` runs | passed | passed |
| Security validation tuple | focused tests plus code trace from `Registry.Execute` to `identity.Authorize` | attacker-controlled tool name reaches the guard before `Tool.Execute`; denial returns before tool execution; logs record tool name/capability/reason and existing arg-key redaction remains value-free | passed via registry tests and `argKeys` redaction tests | passed |
| Baseline regression gate | `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron` | green before and after patch | passed before patch and after patch | passed |
| Shared quality gate | `go test ./internal/identity`; `go test ./internal/agent/tools/registry`; `go test ./internal/api ./internal/api/auth`; `go test ./internal/agent ./internal/telegram ./internal/channels/telegram`; `go test ./internal/...`; `go build ./...`; `go vet ./...`; `go test ./...` | green | passed on 2026-05-15 | passed |
| Container image updated | `docker compose build aura`; `docker compose up -d --no-deps aura` | rebuilt image contains Phase01B4 code and running container is recreated without resetting volumes or sidecars | passed; `aura:local` image `sha256:660d7207696deeeab01303a702adef04ebb53f24ad35455de3651040b125f140`, container started `2026-05-15T13:29:23Z`, health `healthy` | passed |
| Container live chat regression | `GET /health`; `GET /api/health` without bearer; `GET /api/health` with `AURA_E2E_TOKEN`; `POST /api/chat` with exact Phase01B4 marker | public/API auth boundary remains intact; bearer chat still completes with no tool call when no tool is requested | passed; statuses `200`, `401`, `200`; chat reply `AURA_E2E_PHASE01B4_OK`, client latency 2049 ms, API latency 1994 ms, `llm_calls=1`, `tool_calls=0`, `tokens=8633` | passed |
| Container SQL evidence | `sqlite3 /data/aura.db` inside `aura` container | live chat records the API authorization path while tool denial tests avoid mutating production grants | passed; latest `api.chat` row is `allow|api.chat|api|chat|active_grant`; tool-denial ground truth is covered by disposable SQLite fixtures in local and containerized registry tests | passed |

## Phase01B5 Gates

Status: implemented, locally verified, and container-updated on 2026-05-15.
Phase01B5 owns Telegram login/bootstrap/approval identity parity only.
Cron/swarm delegation was closed in Phase01B6. Denial run/audit events stay in
Phase01B7.

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Printing Press reference recorded | `rg -n "Reference registrata per Aura|D:/tmp/cli-printing-press" docs/cli-printing-press-study.md` | reference is documented as a pattern source, not an executable queue | passed; reference section is at the top of `docs/cli-printing-press-study.md` and maps adopted/rejected patterns plus destinations | passed |
| Configured allowlist identity parity | `go test ./internal/api/auth ./internal/telegram -run "TestEnsureTelegramAllowlistedIdentity_Configured|TestOnLoginConfiguredAllowlistEnsuresIdentityBeforeToken" -count=1` | a configured allowlisted Telegram user can still log in, is not inserted into `allowed_users`, and receives deterministic owner principal/channel-account/session-actor/grants before token issuance | passed; configured allowlist user token lookup succeeds, `allowed_users` count stays zero for that user, and `skills.install` authorizes through owner grants | passed |
| Persisted allowlist identity parity | `go test ./internal/api/auth ./internal/telegram -run "TestEnsureTelegramAllowlistedIdentity_Persisted|TestOnLogin" -count=1` | a persisted bootstrap/approved user still logs in and missing identity rows are repaired from the stored `allowed_users.source` before token issuance | passed; persisted approved user receives user grants from stored source, `api.chat` allows, and `skills.install` denies | passed |
| First-run bootstrap parity | `go test ./internal/telegram -run TestOnLoginBootstrapsFirstRunAndSendsToken -count=1` plus auth SQL assertions | no configured allowlist still lets the first user claim the install; bootstrap creates owner identity/grants and closes its own pending request | passed; first `/login` user is stored, token resolves, and owner grant authorizes `skills.install` | passed |
| Approval parity | `go test ./internal/api/auth ./internal/telegram -run "TestApprove|TestApproveAccessCreatesIdentityBeforeSendingToken" -count=1` | approving a pending user still stores `allowed_users`, sends a token through Telegram, and creates human grants before the token can be used | passed; approved user token resolves, `api.chat` allows, and owner-only `skills.install` denies | passed |
| Pending and deny unchanged | `go test ./internal/telegram ./internal/api/auth -count=1` | unknown users still enter pending flow; deny creates no grants; owner notification set remains configured allowlist plus persisted real users, excluding e2e rows | passed through full auth/telegram package tests including collect-owner and pending approval fixtures | passed |
| SQL evidence | auth/telegram fixtures inspect `principals`, `channel_accounts`, `actors`, `capability_grants`, `allowed_users`, and `authz_decisions` where applicable | identity rows/grants are durable, configured allowlist does not become an `allowed_users` row, and authorization decisions remain metadata-only | passed; tests assert persisted SQL rows and authorization decisions from `identity.Authorize` | passed |
| Baseline regression gate | `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron -count=1` | green before and after patch | passed before patch and after patch | passed |
| Shared quality gate | `go test ./internal/identity`; `go test ./internal/api/auth ./internal/telegram`; `go test ./internal/...`; `go build ./...`; `go vet ./...`; `go test ./...` | green | passed on 2026-05-15 with `-count=1` for focused and full Go tests | passed |
| Container update and proof | `docker compose build aura`; `docker compose up -d --no-deps aura`; `GET /health`; unauthenticated `GET /api/health`; unauthenticated `POST /api/chat` | rebuilt image contains Phase01B5 code; container remains healthy; Telegram/auth identity parity passes in disposable fixtures or a documented fixture-blocker is recorded | passed; image `sha256:d6b8f5dc8e521a0a94ca8f6a60c038618c125352f927c2d1477b26e944acd05e`, container healthy, `/health=200`, unauthenticated `/api/health=401`, unauthenticated `/api/chat=401`; live Telegram fixture and bearer chat marker blocked because `AURA_E2E_TOKEN` is not set | passed |

## Full Phase01B Gates

| PRD Gate | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Telegram fixture resolves actor | `go test ./internal/telegram ./internal/api/auth` | `/start`, `/login`, approve/deny behavior preserved and identity records created before token issuance | passed for configured allowlist, persisted allowlist, bootstrap, and approval identity paths in Phase01B5 | passed |
| Web/API fixture resolves actor | `go test ./internal/api ./internal/api/auth` | bearer token maps to actor; no body-user impersonation under auth | passed in Phase01B3 | passed |
| Dashboard token cannot bypass capability | targeted API auth tests | missing grant returns unauthorized/forbidden and records decision | passed in Phase01B3 for `api.chat`; broader dashboard route capability splits remain later slices | partial |
| Visible tool fails closed | `go test ./internal/agent/tools/registry`; containerized registry test service | tool name visibility is insufficient without capability, and missing identity authorizer denies before execution | passed in Phase01B4 and the fail-closed repair; disposable fixtures prove missing-grant and missing-authorizer denial before tool execution | passed |
| Cron actor delegation | `go test ./internal/cron ./internal/channels/cron -run "TestRunNowDelegatesCronActor|TestRunNowDelegationRejectsMissingParentGrant|TestCronAgentLoopDelegatesActorContext" -count=1` | scheduled job runs under delegated actor/grant snapshot | passed in Phase01B6 with SQLite actor/grant/authz evidence | passed |
| Swarm child delegation | `go test ./internal/agent/tools/swarm ./internal/swarm -run "Test.*DelegatedActor|Test.*SwarmSpawnCapability" -count=1` | child grants are subset of parent; excess requests fail closed | passed in Phase01B6 with SQLite actor/grant/authz evidence | passed |
| Authorization denials durable | targeted `internal/chat` / `internal/storage/runs` tests | denial persisted as metadata-only run/audit event | passed in Phase01B7 with SQL-backed `authz_decisions`, `run_events`, and `audit_events` evidence | passed |
| Shared quality gate | `go build ./...`; `go vet ./...`; `go test ./...` | green | passed after Phase01B7 and the fail-closed repair on 2026-05-15 | passed |

## Phase01B6 Gates

Status: implemented, repo-verified, and container-updated on 2026-05-15.
Phase01B6 owns cron/swarm delegated actor creation only.
Phase 8 schedule-fire workflow semantics and Phase01B7 denial events remain
separate.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Threshold / PRD Gate | Actual Result | Status |
| --- | --- | --- | --- | --- | --- | --- |
| Identity delegation helper creates bounded child actors | `go test ./internal/identity -run TestDelegateActor -count=1` | disposable SQLite identity fixture with a parent Telegram session actor and parent grants | `actors` contains a child actor with `parent_actor_id`; `capability_grants` contains direct actor grants only for parent-authorized capabilities; `authz_decisions` records parent allow/deny decisions | Child actor cannot receive a capability the parent lacks; covers PRD gate "child swarm actor cannot receive capabilities its parent lacks" | passed on 2026-05-15; tests assert child actor rows, direct child grants, allow decisions, and missing-capability denial without child actor creation | passed |
| Cron RunNow uses delegated cron actor when caller authority exists | `go test ./internal/cron -run TestRunNowDelegatesCronActor -count=1` | disposable cron task plus identity store, parent actor context, and recording `JobRunner` | runner context carries delegated `cron` actor ID and authorizer; SQL rows show `actor_type='cron'`, `parent_actor_id=<parent>`, direct actor grant for `tool.execute`, and parent `cron.run`/capability authorization decisions | Cron fixture resolves an actor and cannot exceed parent grant envelope | passed on 2026-05-15; `JobRunner` saw delegated actor context and SQL evidence matched | passed |
| Cron RunNow fails closed without identity delegation | `go test ./internal/cron -run TestRunNowRequiresIdentityDelegation -count=1` | disposable cron task with no identity delegator configured | `RunNowResult.OK=false`, `LastError` contains `identity.ErrUnauthorized`, and `JobRunner.RunJob` is not called | No manual cron agent job can run on a no-authority path | passed on 2026-05-15; runner calls stayed zero | passed |
| Cron delegated actor fails closed on missing parent capability | `go test ./internal/cron -run TestRunNowDelegationRejectsMissingParentGrant -count=1` | disposable cron task where parent lacks one requested delegated capability | `RunNow` fails before `JobRunner.RunJob`; SQL records deny decision for the missing capability; no child grant is created | Missing parent authority blocks delegated cron execution | passed on 2026-05-15; `RunNowResult.OK=false`, `JobRunner` not called, no child actor created, deny decision recorded | passed |
| Cron Hub loop propagates delegated actor context | `go test ./internal/channels/cron -run TestCronAgentLoopDelegatesActorContext -count=1` | disposable identity fixture, cron `InboundMessage` with `PrincipalID`, and recording `JobRunner` | `JobRunner.RunJob` receives context containing delegated `cron` actor ID and authorizer; SQL rows prove `actor_type='cron'`, `parent_actor_id`, direct child grants, and parent authorization decisions | Hub-routed scheduled agent jobs use the same delegated actor boundary as manual RunNow | passed on 2026-05-15; loop-derived parent session actor delegated a cron actor and runner context carried it | passed |
| Cron Hub loop fails closed without identity delegation | `go test ./internal/channels/cron -run TestCronAgentLoopRequiresIdentityDelegation -count=1` | cron `InboundMessage` with no configured identity delegator | `Run` returns `identity.ErrUnauthorized` and `JobRunner.RunJob` is not called | Hub-routed cron jobs cannot bypass identity when wiring is missing | passed on 2026-05-15 after rerun outside sandbox because Go build cache access was denied | passed |
| Swarm tools require `swarm.spawn` and create worker child actors | `go test ./internal/agent/tools/swarm ./internal/swarm -run "Test.*DelegatedActor|Test.*SwarmSpawnCapability" -count=1` | disposable swarm store, identity store, parent actor context, and recording `AgentRunner` | registry/tool execution requires `swarm.spawn`; each worker run context carries a delegated `swarm` actor; SQL rows show `actor_type='swarm'`, `parent_actor_id=<parent>`, role/tool constraints, and direct grants no broader than the parent | Swarm child fixture resolves actors and child grants cannot exceed parent authority | passed on 2026-05-15; missing `swarm.spawn` denied before worker execution, granted parent produced child swarm actor and direct `tool.execute` grant | passed |
| Swarm delegated worker fails closed without identity delegation | `go test ./internal/swarm -run TestManagerDelegationRequiresIdentity -count=1` | disposable swarm run with delegated worker capabilities but no identity delegator in context | run fails with `identity.ErrUnauthorized`, status is `failed`, and `AgentRunner.Run` is not called | Swarm child execution cannot run on a no-authority path | passed on 2026-05-15; runner calls stayed zero | passed |
| Composition root wires the shared identity authority | `go test ./cmd/aura -run TestAuthStoreImplementsDelegationInterfaces -count=1`; `go build ./cmd/aura` | app composition package plus shared auth store type | `auth.Store` satisfies `identity.Delegator`; app wiring compiles when cron handler/cron Hub loop receive the shared auth store and web chat exposes delegation context | Composition root does not create a second identity authority or leave cron/swarm on a private permission path | passed on 2026-05-15; `auth.Store` compile-checks as `identity.Delegator` and `go build ./...` passed with app wiring | passed |
| Baseline regression gate | `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron -count=1` | current Phase01B package set | green before and after patch | catches unrelated auth/cron/swarm drift before B6 edits | passed before and after patch on 2026-05-15 | passed |
| Shared quality gate | `go test ./internal/identity ./internal/cron ./internal/channels/cron ./internal/agent/tools/swarm ./internal/swarm ./cmd/aura -count=1`; `go build ./...`; `go vet ./...`; `go test ./... -count=1` | repository packages | all commands green | required before B6 can be marked locally verified | passed on 2026-05-15; full repo `go test ./... -count=1` passed | passed |
| Container update and live precheck | `docker compose build aura`; `docker compose up -d --no-deps aura`; `GET /health`; unauthenticated API probes | running local compose stack | rebuilt image is healthy and auth boundary remains intact | precheck only; does not close B6 without SQL/unit delegation evidence | passed; image `sha256:946e9c4f5d71429bb6e487a0cea50bc17fde1171dd37133b1a31f6e1c329a0ec`, `aura-aura-1` healthy, `/health=200`, unauthenticated `/api/health=401` | passed |

## Phase01B7 Gates

Status: implemented, repo-verified, and container-updated on 2026-05-15.
Phase01B7 owns authorization denial run/audit event correlation only. Phase 8
RunGraph, schedule-fire workflows, and full web-chat Hub migration remain
separate.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Threshold / PRD Gate | Actual Result | Status |
| --- | --- | --- | --- | --- | --- | --- |
| Run store records metadata-only authorization denials | `go test ./internal/storage/runs -run TestRecordAuthorizationDenial -count=1` | disposable SQLite run store with one existing run and one denied authorization decision | `run_events` contains `type='authorization_denied'`, `actor_id`, decision/capability/resource/reason metadata, and `redaction_level='metadata'`; `audit_events` contains matching `type='authorization_denied'` and references the run event | Proves Phase01A run/audit foundation can store auth denial evidence without raw payloads | passed on 2026-05-15; SQL assertions covered `run_events`, `audit_events`, payload fields, redaction level, and idempotent repeat recording | passed |
| Identity denial hook preserves default-deny decision and emits recorder only on deny | `go test ./internal/identity -run TestAuthorize_DenialRecorder -count=1` | disposable identity store with allow and deny grants plus a recording denial sink in context | every `Authorize` call still records exactly one `authz_decisions` row; denied calls invoke the recorder with decision ID, actor ID, capability, resource, reason, and run ID; allowed calls do not emit denial events | Denial event hook cannot bypass or replace `authz_decisions` and cannot fire on allow | passed on 2026-05-15; denied call recorded `authz_decisions.run_id` and fired the recorder once, allowed call recorded allow without firing recorder | passed |
| Chat Hub attaches run provenance to agent-loop authorization | `go test ./internal/chat -run TestReceiveMessage_WithLifecycleStoreRecordsAuthorizationDenial -count=1` | Hub with `LifecycleStore`, fake loop that executes a registry tool without a grant, and disposable SQLite identity/run stores | run starts, registry denial stops before tool execution, `authz_decisions.run_id` equals the Hub run, `run_events` has `authorization_denied`, and `audit_events` has matching metadata-only denial | Run-bound authorization denials become causal run events | passed on 2026-05-15; fake tool did not execute and SQL rows were correlated to the Hub run ID without raw prompt or tool argument values | passed |
| Registry tool denial records correlated denial evidence | `go test ./internal/agent/tools/registry -run TestRegistryExecuteRecordsAuthorizationDenialEvent -count=1` | registry fake tool, actor context, run ID context, denial recorder context, and missing `tool.execute` grant | fake tool is not called; `authz_decisions` has deny with `run_id`; denial recorder receives capability/resource/reason without argument values | Tool visibility remains insufficient and denial evidence stays metadata-only | passed on 2026-05-15; `authz_decisions` included `run-registry-denial` and the recorder received only metadata fields | passed |
| Production Hub wiring uses shared run store | `go test ./cmd/aura -run TestChatHubLifecycleStoreWiring -count=1`; `go build ./cmd/aura` | composition root compile checks and constructor tests | Telegram and cron Hub construction receive the shared `internal/storage/runs.Store`; no second DB or channel-private trace store is introduced | Denial recording is not test-only | passed on 2026-05-15; `runs.Store` compile-checks as both `chat.LifecycleStore` and `identity.AuthorizationDenialRecorder`, and `go build ./cmd/aura` passed after production wiring | passed |
| Identity package boundary stays storage-agnostic | `rg -n "internal/storage/runs" internal/identity` | source import scan | no matches | `identity` owns narrow interfaces and does not import the concrete run store | passed on 2026-05-15; no matches | passed |
| Baseline regression gate | `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron -count=1` | current Phase01B package set | green before and after patch | catches unrelated auth/chat/tool drift before B7 edits | passed on 2026-05-15 | passed |
| Shared quality gate | `go test ./internal/storage/runs ./internal/identity ./internal/chat ./internal/agent/tools/registry ./cmd/aura -count=1`; `go build ./...`; `go vet ./...`; `go test ./... -count=1` | repository packages | all commands green | required before B7 can be marked locally verified | passed on 2026-05-15; full `go test ./... -count=1` passed again after removing fail-open authority paths | passed |
| Container update and live precheck | `docker compose build aura`; `docker compose up -d --no-deps aura`; `Invoke-WebRequest http://127.0.0.1:18080/health`; unauthenticated `Invoke-WebRequest http://127.0.0.1:18080/api/health`; unauthenticated `Invoke-WebRequest -Method POST http://127.0.0.1:18080/api/chat` | running local compose stack | rebuilt image is healthy; `/health` returns `200`; unauthenticated `/api/health` returns `401`; unauthenticated `/api/chat` returns `401` | precheck only; does not close B7 without SQL denial event evidence | passed on 2026-05-15; image `sha256:3c1e5ea6893c3425bd508fb8925528f741e7d315efc3ab41f215949291312777`, `aura-aura-1` healthy, `/health=200`, unauthenticated `/api/health=401`, unauthenticated `/api/chat=401`; log tail inspected with no startup errors | passed |

## Phase01B Fail-Closed Repair Gates

Status: implemented, repo-verified, and container-updated on 2026-05-15.
This repair removes the former Phase01B fail-open authority paths. It does not
remove unrelated historical compatibility code outside the identity/tool/cron/
swarm authority boundary.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Threshold / PRD Gate | Actual Result | Status |
| --- | --- | --- | --- | --- | --- | --- |
| Registry denies missing authorizer | `go test ./internal/agent/tools/registry -run TestRegistryExecuteWithoutAuthorizerFailsClosed -count=1` | registered fake tool with no identity authorizer in context | error wraps `identity.ErrUnauthorized`; fake tool is not called | Tool execution cannot run when authority is missing | passed on 2026-05-15 | passed |
| Cron manual jobs deny missing identity delegation | `go test ./internal/cron -run TestRunNowRequiresIdentityDelegation -count=1` | persisted agent job with no identity delegator configured | `RunNowResult.OK=false`; `LastError` contains `identity.ErrUnauthorized`; `JobRunner.RunJob` is not called | Manual cron jobs cannot bypass identity when wiring is missing | passed on 2026-05-15 | passed |
| Cron Hub jobs deny missing identity delegation | `go test ./internal/channels/cron -run TestCronAgentLoopRequiresIdentityDelegation -count=1` | cron inbound message with no configured identity delegator | `Run` returns `identity.ErrUnauthorized`; `JobRunner.RunJob` is not called | Hub-routed cron jobs cannot bypass identity when wiring is missing | passed on 2026-05-15 after rerun outside sandbox because Go build cache access was denied | passed |
| Swarm workers deny missing identity delegation | `go test ./internal/swarm -run TestManagerDelegationRequiresIdentity -count=1` | swarm assignment with delegated capabilities and no identity delegator in context | run fails, status is `failed`, and `AgentRunner.Run` is not called | Child-agent execution cannot bypass identity when wiring is missing | passed on 2026-05-15 | passed |
| Broad authority regression gate | `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron -count=1`; `go test ./internal/storage/runs ./internal/identity ./internal/chat ./internal/agent/tools/registry ./internal/cron ./internal/channels/cron ./internal/swarm ./cmd/aura -count=1`; `go build ./...`; `go vet ./...`; `go test ./... -count=1` | repository packages after fail-closed patch | all commands green | No test fixture or production path still depends on no-authority execution | passed on 2026-05-15; one initial full-test failure exposed an `internal/agent` fixture without authority and was repaired before the final full pass | passed |

## Phase01B Parent Closure Verifier - 2026-05-15

Status: Phase01B closure gates passed. The parent verifier passed local code
gates, rebuilt the container, passed live auth-boundary probes, repaired the
settings-vs-secrets provider credential collision, removed runtime `.env`, and
passed exact live chat markers. SQLite records `api.chat` allow decisions for
`actor:telegram:session:1148481707`.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Phase01B target packages | `go test ./internal/identity ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/swarm ./internal/cron ./internal/channels/cron ./internal/swarm ./internal/storage/runs ./internal/chat ./cmd/aura -count=1` | local Go packages | all identity, API, Telegram, tool, cron, swarm, run-store, chat, and composition-root gates green | passed on 2026-05-15 | passed |
| Chat Hub actor persistence repair | `go test ./internal/storage/runs ./internal/chat -count=1` | disposable SQLite run store and Chat Hub lifecycle tests | `runs.actor_id` and durable lifecycle `run_events.actor_id` inherit the actor from context | passed; added regression coverage for run/event actor persistence | passed |
| Repo quality gate | `go build ./...`; `go vet ./...`; `go test ./... -count=1` | full repository | build, vet, and full test suite green after the actor persistence repair | passed on 2026-05-15 | passed |
| Container image update | `docker compose build aura`; `docker compose up -d --no-deps aura`; `docker compose ps aura` | local compose stack | rebuilt `aura:local` contains current code and `aura-aura-1` is healthy | passed; image manifest `sha256:37467baf138b32745fc7764b89528287614e434a1f1ac2a9164c2f09ee9c06f1`, service healthy on `127.0.0.1:18080` | passed |
| Live auth boundary | `GET /health`; unauthenticated `GET /api/health`; unauthenticated `POST /api/chat`; bearer `GET /api/health`; bearer `POST /api/chat` with mismatched `user_id` | running local compose stack plus E2E bearer minted for owner `1148481707` | public health stays open; API requires bearer; authenticated body-user impersonation is denied before chat execution | `/health=200`, unauth `/api/health=401`, unauth `/api/chat=401`, bearer `/api/health=200`, mismatched `/api/chat=403` | passed |
| Live SQL auth evidence | readonly `sqlite3 /data/aura.db` in the `aura` container | live SQLite identity and authz rows | owner principal/session actor has `api.chat`; `authz_decisions` records allow decisions for bearer chat attempts | `principals=1`, `actors=1`, `api.chat` grant present, latest decisions include `allow|actor:telegram:session:1148481707|api.chat|api|chat|active_grant` | passed |
| Settings/secrets provider repair | code fix from `efce2036 fix(api): route secret-shaped dashboard writes to secrets store`; live SQLite repair; `docker compose stop aura`; `docker compose exec -T aura sqlite3 /data/aura.db ...`; remove `D:/Aura/data/.env*` | running compose DB plus runtime volume | secret-shaped keys that `applySecretsToConfig` reads are canonical in `secrets`, legacy `settings.LLM_API_KEY` row is gone, and runtime does not load `.env` | passed; live DB shows `llm_api_key` present with expected provider-key shape and `settings.LLM_API_KEY` row count `0`; no `.env*` file remains in `D:/Aura/data` | passed |
| Container image update after provider repair | `docker compose build aura`; `docker compose up -d --no-deps aura`; `docker compose ps aura` | local compose stack | rebuilt `aura:local` contains current code and starts without runtime `.env` | passed; image manifest `sha256:ea46cdf1d5eafb534e5494c501d14227d4a3d556e0cdfe4fc0ed281ecf6cb9b0`; `aura-aura-1` healthy on `127.0.0.1:18080` | passed |
| Live bearer chat marker | `go run ./cmd/chat -quiet -m "Rispondi esattamente AURA_PIPE_OK e niente altro."`; bearer `POST /api/chat` asking for exact `PHASE01B_CLOSE_OK` | running local compose stack and DB-backed configured LLM provider | chat returns `200`, exact marker, LLM metrics, and zero tool calls | passed; `cmd/chat` returned exact `AURA_PIPE_OK`; direct bearer `/api/chat` returned exact `PHASE01B_CLOSE_OK`, elapsed 5208 ms, `llm_calls=1`, `tool_calls=0`, `tokens=8790` | passed |

## Phase01B Interactive Debug Repair - 2026-05-15

Status: repo and container verified. This repair closes the two P1 findings from
`D:/Aura/docs/aura-interactive-debug-report-2026-05-15.md`: the old web
`/api/chat` path bypassed durable `runs`, and Telegram Hub entrypoints lost
actor context before lifecycle persistence. The legacy web runner was removed
instead of wrapped.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Web API chat uses Hub run plane | `go test ./cmd/aura -run TestHubBackedWebChatPersistsRunAndActor -count=1` | fake LLM plus disposable SQLite run store and actor context | reply metrics remain compatible, `runs` has one completed `channel='web'` row with `actor_id`, and lifecycle `run_events` have matching actor context | passed on 2026-05-15; SQL assertions cover `runs`, `run_started`, `message_done`, `usage`, and `done` | passed |
| Telegram Hub context carries actor authority | `go test ./internal/telegram -run TestTelegramHubContextCarriesActorAndAuthority -count=1` | Telegram auth-store fixture | Hub context contains `actor:telegram:session:<id>`, an `identity.Authorizer`, and an `identity.Delegator` | passed on 2026-05-15 | passed |
| Legacy web direct runner removed | `rg -n "NewWebChatService|webChatService|agent\.RunTask\(ctx, deps, task\)|api.NewWebChatService" D:/Aura/internal D:/Aura/cmd -g "*.go"` | source tree | no direct API web chat service and no direct `agent.RunTask(ctx, deps, task)` web route remain | passed; no matches found | passed |
| Shared regression gate | `go test ./cmd/aura ./internal/api ./internal/channels/web ./internal/telegram -count=1`; `go test ./internal/storage/runs ./internal/chat ./internal/channels/web ./internal/telegram ./internal/api ./cmd/aura -count=1`; `go build ./...`; `go vet ./...`; `go test ./... -count=1` | repository packages | all targeted, build, vet, and full tests pass after deleting the legacy file | passed on 2026-05-15 | passed |
| Container/live rerun | `docker compose build aura`; `docker compose up -d --no-deps aura`; `GET /health`; unauthenticated `GET /api/health`; temporary bearer `POST /api/chat`; SQLite `runs` and `run_events` query; token revocation query | running local compose stack with temporary bearer for `1148481707` | rebuilt container contains the Hub-backed web route; live `/api/chat` returns exact marker; SQLite records a completed `channel='web'` run with actor context and matching actor on lifecycle events; temporary bearer is revoked | passed; image `sha256:0dcb1cb74ba687b5e921121beda58782e27aff6762e97a9d8b781f00ef741139`, health `200`, unauth API health `401`, reply `WEB_HUB_LIVE_OK`, `llm_calls=1`, `tool_calls=0`, `tokens=9820`, run `6983e1e41855db95|actor:telegram:session:1148481707|web|completed|WEB_HUB_LIVE_OK`, `run_events` count `4` with matching actor, temporary token revoked count `1` | passed |

## Phase01B Web Chat Stability Pass - 2026-05-15

Status: repo, race, and container verified. This pass hardens the Hub-backed
web chat replacement after the legacy route was removed. It proves the web
tool executor carries the same model-visible tool context expected by internal
tool orchestration, and that the live container can handle repeated web chat
turns with durable run/event actor evidence.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Web tool executor carries visible tool context | `go test ./cmd/aura -run TestWebToolExecutorCarriesVisibleToolContext -count=1` | fake registry tool plus identity authorizer context | tool receives `AllowedToolNamesFromContext` containing the visible web tool list and `UserIDFromContext` containing the API user | passed on 2026-05-15 | passed |
| Targeted regression gate | `go test ./cmd/aura -run "TestWebToolExecutorCarriesVisibleToolContext|TestHubBackedWebChatPersistsRunAndActor" -count=1`; `go test ./internal/telegram -run TestTelegramHubContextCarriesActorAndAuthority -count=1` | local package tests | web Hub run persistence, web tool context propagation, and Telegram actor/authority context remain green | passed on 2026-05-15 | passed |
| Shared Go gate | `go test ./cmd/aura ./internal/api ./internal/channels/web ./internal/telegram ./internal/chat ./internal/storage/runs ./internal/agent/tools/registry -count=1`; `go vet ./...`; `go build ./...`; `go test ./... -count=1` | repository packages | targeted packages, vet, build, and full test suite pass after the stabilization patch | passed on 2026-05-15 | passed |
| Race gate for touched runtime packages | `go test -race ./cmd/aura ./internal/api ./internal/channels/web ./internal/telegram ./internal/chat ./internal/storage/runs -count=1` with `D:/tmp/w64devkit/bin` first in `PATH` | local Windows Go toolchain plus installed `w64devkit` | race detector builds and passes on the web/API/Telegram/chat/run-store runtime packages | passed on 2026-05-15 after installing `D:/tmp/w64devkit/bin/gcc.exe`; `go env CC/CXX` point to w64devkit | passed |
| Three-turn live web stability probe | temporary bearer `POST /api/chat` exact markers `WEB_STABLE_A`, `WEB_STABLE_B`, `WEB_STABLE_C`; SQLite `runs` and `run_events` queries; token revocation query | rebuilt local compose stack, image `sha256:e5cc463996ec1abc67077c690d83aa8898a2548c7cd0621f6e3df042db78eb77` | each marker returns exactly, each turn creates a completed `channel='web'` run with actor `actor:telegram:session:1148481707`, each run has matching actor on lifecycle events, and the temporary bearer is revoked | passed; replies exact, `llm_calls=1`, `tool_calls=0`, tokens `9830/9839/9863`, run IDs `a632a3749f93e881`, `53f595d4ad841334`, `f17712e30bbe9f17`, each with 4 actor-matched events, revoked count `1` | passed |
| Container log health | `docker compose logs --since=10m aura` filtered for `warn`, `error`, `fatal`, and `panic` log levels | running local compose stack after live probes | no warning/error/fatal/panic levels in recent Aura logs | passed; no matching log lines | passed |

## Phase01B RunTask RunID Propagation Repair - 2026-05-16

Status: repo, race, and container verified. This repair closes a remaining
legacy bridge gap where cron/swarm delegated actor paths could enter
`agent.RunTask` with an empty executor run ID even though the parent run was
known in context. The benchmark is intentionally fixture-backed: the ground
truth is the `RunID` seen by tool context plus delegated actor SQL evidence,
not a live smoke check.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| `RunTaskDeps.RunID` reaches tool context | `go test ./internal/agent -run TestRunTaskDepsRunIDPropagatesToToolContext -count=1` | fake LLM plus fake registry tool | tool receives `identity.RunIDFromContext(ctx) == "run-task-123"` and executor is constructed with the same run ID | passed on 2026-05-16 | passed |
| Cron app adapter uses `cron.JobRequest.RunID` | `go test ./cmd/aura -run TestAgentJobRunnerAdapterPassesRequestRunID -count=1` | fake tool-calling LLM plus app adapter and fake tool | tool context receives the cron job request run ID instead of an empty bridge value | passed on 2026-05-16 | passed |
| Swarm app adapter uses context run ID | `go test ./cmd/aura -run TestSwarmRunnerAdapterPassesContextRunID -count=1` | fake tool-calling LLM plus app adapter and fake tool | tool context receives `swarm-run-123` from `identity.WithRunID` | passed on 2026-05-16 | passed |
| Cron manual delegation carries run ID | `go test ./internal/cron -run TestRunNowDelegatesCronActor -count=1` | disposable cron task, identity store, and recording `JobRunner` | `JobRequest.RunID` is non-empty and delegated cron actor row has matching `run_id` | passed on 2026-05-16 | passed |
| Cron Hub delegation carries run ID | `go test ./internal/channels/cron -run TestCronAgentLoopDelegatesActorContext -count=1` | disposable cron Hub inbound run and recording `JobRunner` | `JobRequest.RunID == run.ID` and delegated cron actor row has `run_id == run.ID` | passed on 2026-05-16 | passed |
| Swarm delegated worker carries run ID | `go test ./internal/swarm -run TestManagerDelegatesAssignmentActorContext -count=1` | disposable swarm store, identity store, and recording `AgentRunner` | worker context carries `res.Run.ID`; delegated swarm actor row has `run_id == res.Run.ID` | passed on 2026-05-16 | passed |
| Touched-package regression gate | `go test ./internal/agent ./cmd/aura ./internal/cron ./internal/channels/cron ./internal/swarm ./internal/agent/tools/registry -count=1`; `go build ./...`; `go vet ./...`; `go test ./... -count=1` | repository packages | all commands green after RunID propagation and the daily briefing/classifier repairs | passed on 2026-05-16 | passed |
| Race gate for touched runtime packages | `go test -race ./internal/agent ./cmd/aura ./internal/cron ./internal/channels/cron ./internal/swarm ./internal/agent/tools/registry -count=1` with `D:/tmp/w64devkit/bin` first in `PATH` | local Windows Go toolchain plus installed `w64devkit` | race detector builds and passes on agent/cmd/cron/swarm/registry packages | passed on 2026-05-16 | passed |
| Container update and live precheck | `docker compose build aura`; `docker compose up -d --no-deps aura`; `GET /health`; unauthenticated `GET /api/health`; recent log scan | running local compose stack, image `sha256:5b2d7496c2a8fb56c4b06d1bba7dc200266ded99885830191853c18215b7026a` | rebuilt image contains the RunID propagation repair, container is healthy, auth boundary remains intact, and logs contain no warn/error/fatal/panic levels | passed; `/health=200`, unauthenticated `/api/health=401`, no matching log lines since restart | passed |

## Manual Probe Candidates

Use only after code wiring reaches the relevant surface:

- `cmd/probe_chat` for authenticated chat actor resolution.
- Existing Telegram debug smoke only when Telegram wiring is changed.
- Direct SQLite artifact inspection for `authz_decisions` and grant snapshots.

Tool-call counts alone are not sufficient; inspect durable rows when validating
authority behavior.
