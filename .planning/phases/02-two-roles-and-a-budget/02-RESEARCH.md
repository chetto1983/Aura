# Phase 2 Research — Two Roles and a Budget

**Measured:** 2026-09-08, by reading the tree at `4a243243b`. Every claim below cites a file and
a line. Where nothing is cited, it is marked as an open question rather than asserted.

## The finding that changes the shape of this phase

`02-CONTEXT.md` C-01 says no per-identity credential resolution exists and that threading one
through the turn is the largest piece of work in the phase. **The first half is right and the
second half is wrong.** The seam already exists and is already per-turn.

`internal/runner/runner_llm_runtime.go:15-23`:

```go
func (r *Runner) llmSnapshot(ctx context.Context) llm.RuntimeSnapshot {
	if snapshot, ok := ctx.Value(llmRuntimeSnapshotContextKey{}).(llm.RuntimeSnapshot); ok {
		return snapshot
	}
	if r == nil || r.runtime == nil {
		return llm.RuntimeSnapshot{}
	}
	return r.runtime.Snapshot()
}
```

`llm.RuntimeSnapshot` (`internal/llm/runtime.go:8-12`) is "one immutable client/config pair used
for a complete LLM run" — it carries **both** the `Client` and the `Config`. A snapshot already on
the context **wins** over the process-wide runtime, and `withLLMRuntimeSnapshot`
(`runner_llm_runtime.go:11`) is how one gets there; `runner.go:214` already does exactly this per
turn.

Every downstream consumer reads through that one function: `runner.go:213`, `runner.go:377`
(`buildAgent`), `runner_context.go:41` and `:75`, `runner_conversation.go:41`,
`runner_resume.go:46`, and the tracker variant `trackerLLMSnapshot` at `runner_llm_runtime.go:25`
used by `runner_persist.go:335` and `runner_reasoning_persist.go:36`.

So the work is not "thread a credential through the hot path". It is **resolve the right snapshot
for the identity and put it on the context**, which is one function and one cache. The hot path
does not change.

### Why the key must ride on the Client, not on the request

`internal/llm/openai_compat/client.go:37-66`: `New(cfg llm.Config)` builds the HTTP client and
folds the credential in at construction —

```go
if llm.ReasoningTarget(cfg.Provider, cfg.BaseURL) == llm.ReasoningTargetOpenRouter && strings.TrimSpace(cfg.APIKey) != "" {
	opts = append(opts, option.WithAPIKey(cfg.APIKey))
}
...
chat: openai.NewChatCompletionService(opts...),
```

The key is bound into `Client.chat` when the service is constructed. There is no per-call option
seam without changing `llm.Request` and both `Stream` and the non-streaming path. Since
`RuntimeSnapshot` already pairs a `Client` with a `Config`, the natural unit is **one
`openai_compat.Client` per identity**, cached, selected at turn start — not a key injected per
call.

`DisableKeepAlives: true` is already set at `client.go:43`, so N clients cost N idle-free
transports rather than N connection pools. That materially lowers the cost of per-identity
clients.

`cmd/aura/llm_client.go:23-28` is the constructor to reuse: it already returns a
`llmNotConfiguredClient` sentinel when the key is empty and the base URL is not a local one
(`allowsKeylessLLMBaseURL`, `llm_client.go:30-48`), and that sentinel's `Stream` returns a
structured `{"error":"llm_not_configured","hint":...}`. **This is the shape CRED-05's clean
refusal should copy** — a client-level sentinel that fails before any network call, with a
machine-readable code, rather than a new refusal mechanism.

### Candidate seams, with the recommendation

| Seam | Where | Cost |
|---|---|---|
| **A — per-identity snapshot on ctx (recommended)** | the `/agent/run` handler already knows the principal (`principalIdentityID`, `internal/agui/auth.go`); it puts the identity's `RuntimeSnapshot` on the request ctx | one resolver + one cache; zero changes below `llmSnapshot` |
| B — per-request key in `llm.Request` | `openai_compat.Stream` appends `option.WithAPIKey` per call | touches the streaming path and every `llm.Client` implementation, including fakes |
| C — swap the process `llm.Runtime` per turn | `internal/llm/runtime.go:16` is an `atomic.Pointer` | unsafe: the runtime is process-wide and concurrent turns would race each other's identity |

Seam A. C is listed only to be ruled out explicitly — the atomic pointer makes it look easy and it
is a correctness trap under two concurrent identities, which is exactly what Phase 1 delivered.

## Q2 — sub-agents, the scheduler, and every other caller

**This is where a per-path accounting design would have gone wrong, and why the key-carries-the-cap
design does not.** Because the cap lives on the OpenRouter key and OpenRouter enforces it
(02-OPENROUTER-API.md, measured: HTTP 403 at `limit: 0`), any call made with an identity's key is
capped — regardless of which component made it. There is no per-path ledger to write and no
component that can "forget" to bill. The only way a path escapes the cap is by using a *different*
key, so the audit that matters is **which components resolve their own LLM client** rather than
reading the turn's snapshot.

Three call sites construct an agent, and they are the inventory the planner must close:

- `internal/runner/runner.go:392` — the interactive path. Reads `llmSnapshot(ctx)`. **Covered by
  seam A.**
- `internal/swarm/swarm.go:293` — swarm workers. `swarm.go:34` declares its own
  `Snapshot() llm.RuntimeSnapshot` provider interface, so it takes the same shape but through its
  own injection point. A worker runs on behalf of the parent turn's identity and must inherit the
  parent's snapshot, exactly as `agent.Budget` already shares one counter across the whole tree
  (`internal/agent/budget.go`, the D-10 shared-atomic design).
- `internal/cron/handlers/handler.go:129` — the scheduler. Headless: there is no HTTP request and
  therefore no principal on the context. It must resolve the snapshot from the *job's owning
  identity* instead. This is the one path where seam A does not apply unchanged.

Beyond agent turns, these paths also spend on the provider and do **not** go through
`turn_usage.go` — `internal/llm/spend.go:27-30` says so in as many words, that deriving spend from
stored token counts "misses every request that did not go through the turn-persistence path —
vision, rerank and embeddings — and silently under-reports":

- `cmd/aura/embedding_client.go:22`
- `internal/multimodal/client.go`, `internal/assets/image_processor.go`,
  `internal/assets/audio_processor.go`
- `internal/agui/voice_api.go`
- `cmd/aura/serve_delegation.go:348`, `cmd/aura/serve_dispatch.go:156` (both take `chat.client`)
- `internal/channels/telegram/` (Telegram-originated turns)

Under a per-identity ledger every one of these would have been a separate integration and each
omission a silent under-charge. Under a per-identity key they are already capped the moment they
use the identity's client, and OpenRouter's analytics dimensions them without our help —
`api_key_id`, `external_user`, `session_id`, `generation_id` (02-OPENROUTER-API.md). **The planner's
job here is an inventory of client resolution, not an integration per surface.**

**Open question:** whether each of the paths above resolves its client from the runner's snapshot
or holds a long-lived `chat.client` captured at boot. `serve_delegation.go:348` and
`serve_dispatch.go:156` both read `chat.client`, which reads like a boot-time capture. This must be
enumerated before the plan is written; it is the difference between "one resolver" and "one
resolver plus N corrections".

## Q3 — the clean pre-flight refusal

Two shapes already exist and CRED-05 should reuse one rather than invent a third:

1. `llmNotConfiguredClient` (`cmd/aura/llm_client.go:36-63`) — a `llm.Client` whose `Stream`
   returns a structured error before any network call. A `creditExhaustedClient` alongside it
   would refuse a zero-cap identity with the same machine-readable shape, at the same seam, with
   no new refusal path in the agent loop.
2. `agent.Budget` (`internal/agent/budget.go`) — the existing resource-exhaustion control, bounding
   steps and wallclock across the whole agent tree. It is the precedent for *how a run is refused
   for exhaustion*, and its shared-counter design is the reason a swarm cannot multiply the bound.

Recommendation: shape 1 for the refusal, because it fires before the model is called, which is
exactly what CRED-05 requires, and because it inherits the sentinel pattern the codebase already
ships.

**Open question, and it matters:** what the agent loop does when a provider 403 arrives *mid-turn*
— after a top-up lapses, or on the 25s window of M-06. `internal/agent/llm_agent_retry.go` and the
`openai_compat` error path need reading before the plan commits to a behaviour. Note
`option.WithMaxRetries(0)` at `client.go:47`: provider retries are explicitly disabled, so a 403
surfaces rather than being retried into a longer stall.

## Q4 — retiring the wildcard, declaring capabilities once

`HasCapability` (`internal/identity/store.go:137-150`) resolves `capability = '*' OR capability = $2`.
The wildcard is seeded in two places: `0004_identity.up.sql:31` and
`cmd/aura/serve_bootstrap.go:258` (which also writes `GrantedCapabilities: []string{"*"}` into the
identity audit at `serve_bootstrap.go:~282`).

Capability names are declared in six places today and must collapse to one:

| Constant | File |
|---|---|
| `agentRunCapability = "agent.run"` | `cmd/aura/serve_webui_routes.go:73` |
| `governanceReadCapability = "governance.read"` | `cmd/aura/serve_webui_routes.go:78` |
| `governanceWriteCapability = "governance.write"` | `cmd/aura/serve_webui_routes.go:88` |
| `identityCreateCapability = "identity.create"` | `cmd/aura/serve_webui_routes.go:278` **and** `internal/agui/onboarding_provision.go:56` |
| `adminShellCapability = "governance.write"` | `internal/agent/tools/shell_bg_owner.go:30` |
| `skillManageCapability = "governance.write"` | `internal/agent/tools/skill_manage.go:12` |
| `wildcardCapability = "*"` | `internal/agui/onboarding_session.go:18` |
| `sharePublicCapability = "share.public"` | only in `cmd/aura/share_public_route_test.go:76` — **the enforced name has no non-test constant** |

Consumers span `cmd/aura`, `internal/agui`, `internal/agent/tools` and `internal/identity`. The
declaration must therefore live in a package all four can import without a cycle —
`internal/identity` already holds `ValidateCapabilityName` (`store.go:241`) and the grammar regex,
which makes it the natural home, but this needs an import-graph check before it is asserted.

The migration must rewrite existing `*` rows into the explicit set. Note `0026` deliberately made
the `local` identity's grants explicit "so the admin contract would survive any future narrowing of
the wildcard" — that identity is the one pre-existing row most likely to be affected, and the
migration should be checked against it specifically.

## Q5 — the admin surface

**Routes that exist** (`internal/agui/audit_api.go:76-78`, mounted in
`cmd/aura/serve_webui_musr.go:28-30`):

```
GET    /api/admin/identities
POST   /api/admin/identities/{id}/capabilities
DELETE /api/admin/identities/{id}/capabilities/{capability}
```

Both mutating routes are registered in `internal/agui/idempotency_http.go:65-66` with
`httpMutationMeta("capability_grant")` / `("capability_revoke")`. **Any new mutating admin route
must be registered there too** — identity removal and a credit-cap update are both mutations and
both need an idempotency identity.

Onboarding routes (`internal/agui/onboarding_api.go:150-154`): `GET /api/onboarding/status`,
`POST /api/onboarding/profile`, `POST /api/onboarding/start`,
`POST /api/onboarding/{sessionToken}/provision`, `GET /api/onboarding/{sessionToken}/telegram-status`.

**Routes that do not exist:** anything that removes an identity, and anything that reads or sets a
credit cap. There is no HTTP surface over `deprovision.go` at all.

**The removal saga already exists** — `internal/agui/deprovision.go`. Its documented contract
(file header, lines 9-24): soft-delete is two-phase; `Deactivate` is immediate (kills Authula
sessions, terminates the identity's background jobs, stamps `deactivated_at` + a grace-window
`purge_after`), `Purge` runs after the grace window and tears down every plane in reverse order.
Steps journaled on `aura.provisioning_saga` with `kind='deprovision'` (migration `0028`), each
idempotent (`Delete`/`Deny` by-id 404=success, `RemoveAll`, FK-cascade delete), so an interrupted
purge re-runs and converges with no orphaned Authula user, Garage bucket, `:User` graph edge or
per-identity directory. Saga steps at `deprovision.go:148-238`: `sagaStepDeactivate`,
`sagaStepSandbox`, `sagaStepConversations`, `sagaStepMemory`, `sagaStepObjectStore`,
`sagaStepDirs`, `sagaStepIdentityRow`, `sagaStepAuthula`.

Grace window default: `defaultDeprovisionGrace = 7 * 24 * time.Hour` (`deprovision.go:28`),
overridable via `DeprovisionDeps.GraceWindow`. Ports are consumer-side interfaces and **a nil port
skips its plane**, which is how the unit tests inject fakes — and also a hazard: a mis-wired
composition root silently under-tears rather than failing.

Already wired: `cmd/aura/serve_provisioning.go` (`identityDeactivatorAdapter` at `:215-244`,
`filesystemProvisionAdapter.DeprovisionIdentityDirs` at `:142`), the CLI
(`cmd/aura/identity.go:30-35`, verbs `deactivate` and `purge`, both requiring `--confirm`), and the
grace sweep (`internal/cron/handlers/identity_purge.go`, dispatched at
`cmd/aura/serve_dispatch.go:77` via `buildDeprovisioner(chat)`).

Frontend: `web/src/admin/` — `adminApi.ts`, `AdminSection.tsx`, `useAdmin.ts`. Per ROADMAP's Notes
section, a four-step "Create identity" wizard (Credentials → Capabilities → …) and per-grant Revoke
already exist under Settings → Identity and permissions, measured live 2026-09-07. Under D-01 the
wizard's capability step loses its per-capability choices, and the section gains a removal action
plus a credit panel.

## Q6 — testing against this repo's gates

**The httptest convention exists and is the one to copy.** `internal/llm/spend.go:53` takes
`client *http.Client, baseURL, apiKey string` as parameters precisely so a test can point it at an
`httptest.Server`; `FetchModelPrice` (`internal/llm/pricing_source.go:291`) has the same shape.
Every new OpenRouter call should take its `*http.Client` and base URL as parameters rather than
reaching for a package-level default, so the unit tier stays daemon-free and network-free.

Build tags in use: `db_integration`, `garage_integration`, `authula_integration`, `musr_e2e`,
`docker_integration`, `arcadedb_integration` (from Phase 1's verify commands in
`01-01-PLAN.md`).

Recommended tiering:

| Code | Tier | Why |
|---|---|---|
| Provisioning-API client (mint/patch/delete/read) | unit, httptest fake | no daemon; mirrors `spend.go` |
| `aura.identity_llm_key` store | `db_integration` | RLS and cascade are the point and only Postgres proves them |
| Capability declaration + wildcard retirement | unit + `db_integration` | the SQL change needs a real database |
| Per-identity snapshot resolver + cache | unit, with a fake `llm.Client` | `internal/agent/agenttest/fakeclient.go` already exists |
| Admin routes | unit (handler) + `musr_e2e` | idempotency registration and the gate need the mounted mux |
| Removal route → saga | `musr_e2e` | the saga's own tests exist; the route needs the live planes |
| The closing permission-and-credit matrix | live run, per ROADMAP | not a suite |

CLAUDE.md's no-skip-as-green rule applies: any tier whose env is unset must `t.Fatal` under `$CI`,
never skip. Mutation ≥70% is required per REL-06 on gateway, identity, profile, sandbox and
frontend — the refusal branches (zero cap, ungrantable admin capability, last-admin protection) are
exactly the kind of branch that is covered but not killed, so they need assertions on the refusal
*reason*, not merely on the error being non-nil.

## Constraints confirmed by measurement

- **Migration numbering:** highest on disk today is `0120_worker_steer_scopes`. The real number is
  assigned when the phase lands — `ls internal/db/migrations/ | tail -1`, never deduced from here.
- **600 LOC ceiling:** `internal/agui/onboarding_provision.go` is **568 lines** and gains a
  key-minting leg. It must be split before it is grown, not after. `internal/llm/spend.go` is 102,
  `prices.go` 88, `pricing_source.go` 426 — the last one is the file to watch if the provisioning
  client is put there rather than in its own package.
- **The credential store pattern:** `internal/mcpoauth/store.go` — AES-256-GCM (`cipher.NewGCM`,
  `store.go:104-110`), KEK derived from `AURA_AUTHULA_SECRET`, RLS under migration `0100`'s two
  layers, `ON DELETE CASCADE` on `identity_id`, and `aura_app` granted
  `SELECT, INSERT, UPDATE, DELETE` (`0100_identity_mcp_oauth.up.sql:57`). Its listing query
  deliberately selects **no ciphertext** (`internal/db/sqlc/identity_mcp_oauth.sql.go:86`) — the
  new store should do the same, since the admin roster needs the cap and the hash but never the key.
- **The hash is not a secret.** OpenRouter's `hash` addresses the key for PATCH and DELETE and
  appears in error messages the provider itself returns. It belongs in a plaintext column beside
  the ciphertext, so a revoke can run without decrypting anything.

## What this means for planning

In dependency order:

1. **Capability declaration in one place** + retire the wildcard (migration + `HasCapability` +
   both bootstrap paths). Everything else in RBAC depends on the name set existing.
2. **The user set granted at provisioning**, and the two administrative capabilities made
   ungrantable through the grant/revoke API. Includes the last-admin protection.
3. **`aura.identity_llm_key`** — migration, store on the `mcpoauth` pattern, sqlc wiring.
4. **The OpenRouter provisioning client** — mint, patch, delete, read; httptest-tested; taking its
   `*http.Client` and base URL as parameters.
5. **Provisioning and deprovisioning legs** — mint at a zero cap (with `external.user` set to the
   identity id, per 02-OPENROUTER-API.md), revoke on removal and verify the 404. Split
   `onboarding_provision.go` first.
6. **The per-identity snapshot resolver + cache**, seam A, plus the `creditExhaustedClient`
   sentinel. Requires the client-resolution inventory flagged in Q2 to be closed first.
7. **Swarm and cron inheritance** — worker inherits the parent's snapshot; the scheduler resolves
   from the job's owning identity.
8. **Admin HTTP routes** — removal, cap read/update; registered in `idempotency_http.go`.
9. **Cockpit** — removal action with typed confirmation, credit panel, and the ~25s top-up latency
   stated in the UI.
10. **The closing live run**, per ROADMAP.

Item 6 is the one with a real unknown in front of it and should not be planned until Q2's open
question is answered.

## Validation Architecture

`workflow.nyquist_validation` is `true` in `.planning/config.json` — this section is required.
Derived from what this repo actually runs (`Makefile`, `scripts/coverage_gate.sh`,
`scripts/coverage_package_policy.json`, `scripts/critical_mutation_gate.py`,
`.github/workflows/ci.yml`), not from a generic pyramid.

### Test Framework

| Property | Value |
|---|---|
| Framework (backend) | Go standard `testing` + `//go:build` tags — no external test framework. Tags in use: `db_integration`, `garage_integration`, `authula_integration`, `musr_e2e`, `arcadedb_integration`, `docker_integration` |
| Framework (frontend) | Vitest (`web/vitest.config.ts`) + Stryker (`web/stryker.config.json`) — both pre-existing, unchanged by this phase |
| Config file | none for Go (tag lines are the config); `web/vitest.config.ts` / `web/stryker.config.json` for the frontend |
| Quick run command (per CLAUDE.md Post-edit validation) | `go vet ./... && go build ./... && go test ./internal/<touched>/ && go test -race ./internal/<touched>/` |
| Full suite command | `make quality-full` (deadcode+vet+file-size+lint+`test-race`+vuln+build, then the ≥85% `db_integration` coverage floor) + `make musr-e2e` (live two-identity acceptance) + `make critical-mutation` (≥70% killed, five named scopes below) + `make web-quality` (lint+test+mutation, frontend) |
| Coverage floor | 85% aggregate, package-local policy per `scripts/coverage_package_policy.json` (`Makefile:92-93`, `scripts/coverage_gate.sh:29`) |

**Two packages this phase lands directly on are already flagged low in `scripts/coverage_package_policy.json`** (cited in `.planning/STATE.md`'s Blockers/Concerns, measured 2026-09-07):
`internal/approvalgrants` at a **4/57 = 7.0%** baseline (`coverage_package_policy.json:15`) and
`internal/webauth` at **187/360 = 51.9%** (`coverage_package_policy.json:79`) — both are pinned as
`"mode": "baseline"` with an exact non-regression floor, not the 85% target. RBAC-07's last-admin
refusal and RBAC-04/06's grant/revoke refusal are exactly the kind of new branch that would land in
`internal/identity` (already `"mode": "target"`, `coverage_package_policy.json:38`, full 85% floor)
rather than these two — but any code this phase adds *inside* `approvalgrants` or `webauth` only
has to hold the pinned non-regression floor, not jump to 85%, unless the planner chooses to raise it.

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|---|---|---|---|---|
| RBAC-01 | `HasCapability` no longer expands `*`; migration rewrites existing wildcard rows | unit + `db_integration` | `go test -race -count=1 ./internal/identity/ -run TestHasCapability` and `go test -tags db_integration -race -count=1 -p 1 -run '^TestMigrateNNNN_' ./internal/db/` (mirrors `ci.yml:345`'s `TestMigrate0019_` pattern; `NNNN` is the number `ls internal/db/migrations/ \| tail -1` yields at landing, per C-04) | ❌ new (`store_test.go` exists at `internal/identity/`, extended) |
| RBAC-02 | Every enforced capability declared in exactly one place | static/grep-based CI assertion — no runtime behavior to unit-test | a new shell test in the style of `scripts/check_ci_go_packages.sh`: assert the capability-name string literals in `cmd/aura`, `internal/agui`, `internal/agent/tools` are zero (only the declaring package, `internal/identity` per Q4, defines them) | ❌ new — Wave 0 gap, see below |
| RBAC-03 | Exactly `identity.create`+`identity.delete` administrative; every other capability granted at provisioning | `db_integration` | `go test -tags db_integration -race -count=1 -run TestProvisionGrantsUniformCapabilitySet ./internal/agui/` (or wherever the provisioning saga's grant step lives) | ❌ new |
| RBAC-04 | `identity.create` refused without the capability | unit + integration (handler) | `go test -race -count=1 ./internal/identity/ -run TestHasCapability_IdentityCreate` and a handler-level test on the create route returning 403 without it | ❌ new |
| RBAC-05 | `identity.delete` required; removal runs the full reverse saga, not a row mark | integration (`db_integration` or `musr_e2e`, since the saga touches every plane) | new HTTP-route test extending `internal/agui/deprovision_test.go`'s existing `TestDeprovisionPurgeReversesEveryLeg` (`deprovision_test.go:164`) with an HTTP-level caller | ⚠️ partial — saga tests exist (`deprovision_test.go`, `deprovision_branches_test.go`, `deprovision_sandbox_test.go`), the route-level test is new |
| RBAC-06 | Admin pair never grantable via `POST`/`DELETE /api/admin/identities/{id}/capabilities` for any caller | unit (handler) | `go test -race -count=1 ./internal/agui/ -run TestAdminCapabilityGrantRefusesEscalation` | ❌ new |
| RBAC-07 | Last administrative identity cannot remove/deactivate itself | unit + integration | `go test -race -count=1 ./internal/agui/ -run TestLastAdminCannotRemoveSelf` | ❌ new |
| RBAC-08 | Fresh install grants the explicit admin set, no wildcard | integration + live | extend `cmd/aura/serve_bootstrap_test.go` and the closing `musr-e2e`/live run | ⚠️ partial — `serve_bootstrap_test.go`, `serve_bootstrap_hint_test.go`, `serve_bootstrap_resources_test.go` exist, extended |
| RBAC-09 | Unknown capability / unresolved principal / store error all deny | unit (fake store returning an error) | `go test -race -count=1 ./internal/identity/ -run TestHasCapability_FailsClosed` | ❌ new |
| RBAC-10 | Every denial auditable (who/capability/route/when), readable from the admin surface | `db_integration` | extend `internal/identity/audit_store_test.go` and `internal/agui/audit_api_test.go` | ⚠️ partial — both files exist |
| RBAC-11 | Cockpit creates and removes an identity without leaving the UI | frontend (Vitest component) for the mechanics; **live/human for "without leaving the UI"** | `cd web && npm run test -- src/settings src/onboarding` against the new Roster + shortened 3-phase wizard | ⚠️ partial — `CapabilityAdminPanel.test.tsx`, `CapabilityPicker.test.tsx`, `OnboardingWizard.test.tsx` etc. exist and are rewritten/retired (UI-SPEC §Surface Map) |
| CRED-01 | Per-identity OpenRouter key minted at provisioning, stored encrypted, never returned to a browser | unit (httptest) + `db_integration` (RLS/cascade) | `go test -race -count=1 ./internal/<new-key-package>/` (httptest, mirrors `internal/llm/spend_test.go`'s `keyServer` helper at `spend_test.go:21-35`) and `go test -tags db_integration -race -count=1 ./internal/<new-key-package>/` (mirrors `internal/mcpoauth/store_integration_test.go`) | ❌ new |
| CRED-02 | New identity starts at zero cap | unit (httptest asserting `POST /keys` body carries `limit: 0`) | `go test -race -count=1 ./internal/<provisioning-client>/ -run TestMintAtZeroCap` | ❌ new |
| CRED-03 | Admin sets cap + reset interval from the cockpit, can change both | unit (handler) + frontend | `go test -race -count=1 ./internal/agui/ -run TestAdminSetCredit` + `cd web && npm run test -- src/settings` | ❌ new |
| CRED-04 | Cap enforced by OpenRouter, not Aura's accounting | **unit only reproduces the refusal SHAPE (httptest 403); real enforcement is NOT CI-reproducible** — `ci.yml:366-377` sets `OPENROUTER_API_KEY: ci-degraded-no-network` explicitly because "No real OPENROUTER_API_KEY in CI" | `go test -race -count=1 ./internal/<key-package>/ -run TestRefusesOn403` (unit, fake); the actual provider-side guarantee is **already measured** live (CONTEXT.md M-04, 2026-09-08: `limit: 0` → HTTP 403 `Key limit exceeded`) and is re-confirmed only by the phase's closing live run, never by CI | ❌ new (unit fake) |
| CRED-05 | Zero-credit turn refused before the model is called | unit — mirrors the existing `llmNotConfiguredClient` sentinel test pattern | `go test -race -count=1 ./cmd/aura/ -run TestCreditExhaustedClient` | ❌ new — `cmd/aura/llm_client.go` has **no existing test file** (`llm_client_test.go` does not exist); Wave 0 gap |
| CRED-06 | Cockpit shows cap/remaining/spend from Aura's in-band ledger, not the provider's lagged counter | `db_integration` + frontend | `go test -tags db_integration -race -count=1 ./internal/agent/ -run TestTurnUsage` (extends existing `turn_usage.go` coverage) + `cd web && npm run test -- src/settings` for the credit-panel render | ⚠️ partial — `turn_usage.go` and `aura.cache_metrics` already tested for other purposes, extended here |
| CRED-07 | No fallback to the deployment key; fail-closed | unit | `go test -race -count=1 ./internal/runner/ -run TestSnapshotResolverNoFallback` (or wherever seam A's resolver lands) | ❌ new |
| CRED-08 | Removing an identity revokes its key; revocation is verified, not assumed | unit (httptest: `DELETE` → `{"deleted":true}`, then `GET` → 404, matching 02-OPENROUTER-API.md's documented contract) | `go test -race -count=1 ./internal/<key-package>/ -run TestRevokeVerifiesDeletion` | ❌ new; live confirmation already measured (CONTEXT.md M-10: 401 within 5s, 404 on re-GET) |
| CRED-09 | Local backend exempt, says so rather than showing `$0.00` | unit | `go test -race -count=1 ./internal/llm/ -run TestErrSpendNotApplicable` (existing pattern, `spend.go`'s `ErrSpendNotApplicable`) + frontend `Empty` composition test | ⚠️ partial — `internal/llm/spend_test.go` exists; frontend test is new |
| REL-06 | `mutation-report.json` ≥70% killed for gateway/identity/profile/sandbox/frontend | mutation (`go-mutesting` ×4 Go scopes + Stryker frontend) | `make critical-mutation` → `scripts/critical_mutation_gate.py` | see the scope-mismatch finding directly below — **not a drop-in pass** |

### The REL-06 mutation-scope mismatch (must be resolved before planning treats criterion 8 as covered)

`scripts/critical_mutation_gate.py`'s `GO_SCOPES` (lines 17-22) run `go-mutesting` against exactly
**one fixed file per scope**, not a package:

```python
GO_SCOPES = {
    "gateway": "internal/gateway/classify.go",
    "identity_isolation": "internal/identityctx/operator.go",
    "profile_validation": "internal/config/config_runtimeprofile.go",
    "sandbox": "internal/sandbox/usersandbox/spec.go",
}
```

plus a fifth, `frontend`, parsed from `web/reports/mutation/mutation.json` (Stryker) against
`web/stryker.config.json`. **None of these four Go files is touched by this phase's own
dependency-ordered plan** (§What this means for planning, items 1-9: capability declaration and
wildcard retirement land in `internal/identity`/migrations; the credential store in a new
`aura.identity_llm_key` package; the provisioning client is new; the snapshot resolver touches
`internal/runner`/`internal/swarm`/`internal/cron`; admin routes touch `internal/agui`). Re-running
`make critical-mutation` unmodified would **pass the script** (it only re-scores the same four
pre-existing files, none of which this phase changes) while leaving success criterion 8's actual
requirement — "the refusal branches **this phase adds** are provably killed" — completely
unaddressed. `identity_isolation`'s target, `internal/identityctx/operator.go`, is specifically
*no-principal attribution* (`operator.go:1-50`), not capability grants — a same-sounding name that
resolves to the wrong file if assumed rather than read.

**This is a planning decision, not a research one; it is flagged rather than resolved here.** The
planner must choose one of:
1. Add new entries to `GO_SCOPES` (and, symmetrically, ensure the new refusal-bearing files are
   single-file-mutable — `go-mutesting` scores one path per scope) pointing at wherever RBAC-06/07's
   grant-refusal logic and CRED-05/07's credit-refusal logic actually land once the plan names those
   files.
2. Accept that REL-06 as currently wired measures four *pre-existing* boundaries plus the frontend,
   and treat "the refusal branches this phase adds" as covered instead by the `db_integration`/unit
   line-coverage floor (85%) on the *touched* branches — which is a materially weaker guarantee than
   mutation-killed, and does not satisfy the phase's own wording of criterion 8.
Do not treat `make critical-mutation` passing as evidence for criterion 8 without first confirming
which option the plan took.

### Not machine-checkable at any tier (backstop / human observation)

- **Success criterion 4** ("the reverse saga is observed to land on every plane," "through the
  cockpit rather than by curl") — the saga's individual steps are unit/integration-tested
  (`deprovision_test.go`), but "observed to land on every plane, driven through the UI" as stated
  is the closing live run's job, per `.planning/config.json`'s `human_verify_mode: end-of-phase`.
- **Success criterion 7**'s second half ("what it cannot do is erase the provider's record") — this
  is a *negative* claim about OpenRouter's own analytics retention. 02-OPENROUTER-API.md's own
  "Not measured — do not assume" section already states this correctly: "Deleted keys keep their
  consumption in the analytics. Nothing observed here removes it" — an absence of a
  deletion-cascades-to-analytics API, not a falsification attempt. It stays `[ASSUMED]`/documented,
  never `[VERIFIED]`, and no tier of this repo's test matrix can prove a negative about a third
  party's data retention. State it in the UI copy (already done, per UI-SPEC's removal dialog body)
  rather than trying to test it.
- **CRED-04**'s live-provider enforcement (above) — CI has no real `OPENROUTER_API_KEY`
  (`ci.yml:366-377`); only the closing live run and the 2026-09-08 measurement (CONTEXT.md M-04)
  cover the real provider behavior.
- **RBAC-11**'s "without leaving the UI" — this repo has no browser-driven E2E harness for the
  frontend (`web-integration-test` in `ci.yml:587` is SearXNG/SSRF tooling, unrelated to the
  cockpit); Vitest component tests cover the mechanics, the full flow is the closing live run.

I checked 02-OPENROUTER-API.md and 02-RESEARCH.md for any claim that the Provisioning API does
**not** offer some capability, per the INVENTORY BEFORE INVENTION rule — none exists. Every absence
in those documents is already hedged under "Not measured — do not assume" (M-14's adjacent
workspaces/guardrails surface, the `limit_reset` rollover behavior, `/activity`'s filter behavior)
rather than asserted as a documented non-capability. No correction is needed here.

### Sampling Rate

- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/<touched>/ && go test -race ./internal/<touched>/` (CLAUDE.md Post-edit validation); `cd web && npm run test` for frontend-touching tasks.
- **Per wave merge:** the tagged tier the wave's files require (`db_integration` at minimum, given every RBAC/CRED table row above needs it), plus `cd web && npm run test -- --coverage` for frontend waves.
- **Phase gate:** `make quality-full` green, `make critical-mutation` green (with the scope question above resolved), `make web-quality` green, then `make musr-e2e`/the closing live run per ROADMAP — full suite green before `/gsd-verify-work`.

### Wave 0 Gaps

- [ ] `cmd/aura/llm_client_test.go` — does not exist today; needed before CRED-05's `creditExhaustedClient` sentinel can be tested at all.
- [ ] A capability-declaration-location assertion script (RBAC-02) — no existing precedent test does this; `scripts/check_ci_go_packages.sh` is the closest shape to copy.
- [ ] A shared httptest fixture for the OpenRouter Provisioning API (`POST/GET/PATCH/DELETE /keys`) — `internal/llm/spend_test.go`'s `keyServer` helper (`spend_test.go:21-35`) covers only `GET /key`; CRED-01/02/03/08 need mint/patch/delete stubs modeled on 02-OPENROUTER-API.md's documented response shapes.
- [ ] The `GO_SCOPES` mutation-scope decision above, before `REL-06`/criterion 8 can be marked plannable as "covered."
- [ ] A migration test template for the new migration number (`TestMigrateNNNN_`, mirrors `ci.yml:345`'s existing `TestMigrate0019_` convention) — the number itself is assigned at landing (C-04), the test shape is not.

*(No gap exists for the reverse-saga tier itself — `internal/agui/deprovision_test.go`,
`deprovision_branches_test.go` and `deprovision_sandbox_test.go` already cover it; only the
HTTP-route wrapper is new.)*
