# Phase01B Benchmark

Status: Phase01B1 is closed: local gates passed and subagent verification found
no remaining Phase01B1 blockers. Phase01B2 allowlist backfill is implemented
and Go-verified locally. Phase01B3 and Phase01B4 are container-verified.
Phase01B parent remains open for B5-B7.

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
authorization guard. Telegram identity parity, cron delegation, swarm
delegation, and denial-event run/audit persistence remain Phase01B5-B7.

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Registry fails closed without tool grant | `go test ./internal/agent/tools/registry -run TestRegistryExecuteRequiresIdentityGrant -count=1` | a visible registered tool is not executed when the actor lacks `tool.execute`; SQLite `authz_decisions` records `deny|tool.execute|tool|fake|missing_grant` | passed locally and in `docker compose run --rm test go test ./internal/agent/tools/registry -run "TestRegistryExecuteRequiresIdentityGrant|TestRegistryExecuteUsesToolSpecificCapability" -count=1` | passed |
| Tool-specific capability is honored | `go test ./internal/agent/tools/registry -run TestRegistryExecuteUsesToolSpecificCapability -count=1` | broad `tool.execute` does not satisfy a tool that declares `tool.execute.fake`; the tool runs only after the narrow grant exists | passed locally and in the containerized test service | passed |
| Compatibility path without authorizer is explicit | `go test ./internal/agent/tools/registry -run TestRegistryExecuteWithoutAuthorizerPreservesCompatibility -count=1` | existing non-authenticated tool plumbing remains executable until Phase01B5-B6 wire every channel actor path | passed | passed |
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
Cron/swarm delegation and denial run/audit events stay in Phase01B6-B7.

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
| Visible tool fails closed | `go test ./internal/agent/tools/registry`; containerized registry test service | tool name visibility is insufficient without capability | passed in Phase01B4; disposable SQLite fixture records missing-grant denial before tool execution | passed |
| Cron actor delegation | `go test ./internal/cron` | scheduled job runs under delegated actor/grant snapshot | not run | planned |
| Swarm child delegation | `go test ./internal/agent/tools/swarm ./internal/swarm` | child grants are subset of parent; excess requests fail closed | not run | planned |
| Authorization denials durable | targeted `internal/chat` / `internal/storage/runs` tests | denial persisted as metadata-only run/audit event | not run | planned |
| Shared quality gate | `go build ./...`; `go vet ./...`; `go test ./...` | green | passed after Phase01B4 | passed |

## Manual Probe Candidates

Use only after code wiring reaches the relevant surface:

- `cmd/probe_chat` for authenticated chat actor resolution.
- Existing Telegram debug smoke only when Telegram wiring is changed.
- Direct SQLite artifact inspection for `authz_decisions` and grant snapshots.

Tool-call counts alone are not sufficient; inspect durable rows when validating
authority behavior.
