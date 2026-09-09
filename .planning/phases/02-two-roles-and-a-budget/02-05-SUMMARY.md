---
phase: 02-two-roles-and-a-budget
plan: 05
subsystem: auth
tags: [credit-policy, llm-client, swarm, cron, telegram, refusal-sentinel, go]

# Dependency graph
requires:
  - phase: 02-two-roles-and-a-budget
    provides: "Plan 02-01's internal/identitykey.Store (the encrypted per-identity OpenRouter key) and internal/runner.IdentityLLMResolver (the seam-A resolver, unwired into any production caller until this plan)"
provides:
  - "internal/identitykey/policy.go — the pure, single-file credit_policy decision (Decide/DecisionInput/Decision, four values) this phase's credit_policy mutation scope targets"
  - "cmd/aura/llm_client.go's creditExhaustedClient/creditExhaustedError — the CRED-05 pre-flight refusal sentinel, sibling to the pre-existing llmNotConfiguredClient"
  - "cmd/aura/llm_client_test.go — new file, pins both refusal sentinels' payloads (02-VALIDATION.md's Wave 0 gap closed)"
  - "internal/runner.IdentityLLMResolver.SnapshotFor now maps all four identitykey.Decision values, including the new CRED-05 exhausted-client branch and cache invalidation on top-up"
  - "internal/swarm.RunConfig.Resolver + swarm_llm_resolve.go's resolveWorkerLLM — the single choke point runChild resolves a worker's LLM client through (Resolver+IdentityID, then Runtime, then an already-resolved Client, else a fail-closed refusal)"
  - "internal/cron/handlers.AgentDeps.Resolver + AgentJobHandler.resolveLLM — the same priority for the headless cron path, keyed on identityctx.IdentityID(ctx) bound from the task row"
  - "internal/channels/telegram commandDeps.Resolver — the chat's linked identity resolves its own /cost spend profile instead of always reading the process-wide Runtime"
  - "cmd/aura's buildIdentityLLMResolver(chat) — the first production wiring of identitykey.NewStore + runner.NewIdentityLLMResolver, into the delegation worker, cron agent_job, and Telegram command deps"
  - "internal/runner/agent_construction_invariant_test.go — the six-file source-scan invariant that fails loudly if the closed boot-time fallback pattern returns"
affects: [02-06, 02-07, 02-08, 02-09, 02-10]

# Actuals (#2632)
actuals:
  tokens: 19929   # chars/4 over this plan's own diff (git diff plan_head_before..HEAD)
  tasks: 3
  commits: 7
  plan_head_before: 7b6fffbe7fc3552c5f488101fa1ef4be70af4811

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Credit decision as a single pure file (internal/identitykey/policy.go): zero I/O, three sequential questions (exemption? key? cap?), each a distinct Decision value and sentinel — mirrors internal/identity/capability_policy.go's own discipline, both now GO_SCOPES mutation-scope targets"
    - "Refusal sentinel pair sharing one marshal helper: cmd/aura/llm_client.go's llmNotConfiguredClient and creditExhaustedClient both build their JSON payload through marshalRefusalPayload rather than duplicating the block (dupl threshold 100)"
    - "Worker LLM resolution as a named priority function (resolveWorkerLLM / AgentJobHandler.resolveLLM), not an inline two-step read-then-override — Resolver+identity first, Runtime second, an already-resolved Client third (the synchronous swarm_spawn fan-out's legitimate shape), a fail-closed refusal last"
    - "Concrete-pointer nil-check BEFORE an interface-typed field assignment, at every buildIdentityLLMResolver call site — the same #2924-class typed-nil-in-interface trap 02-04 documented for a nil-pool store, and the one telegramSteerOrNil already guards Steer against in the same file"
    - "Headless identity resolution reads identityctx.IdentityID(ctx) — bound onto ctx from the durable row (task.IdentityID / job.IdentityID) by the existing scheduledOperationContext / delegation_run.go wiring, never from an HTTP-request principal, since a scheduled or claimed job has none"

key-files:
  created:
    - internal/identitykey/policy.go
    - internal/identitykey/policy_test.go
    - cmd/aura/llm_client_test.go
    - internal/swarm/swarm_llm_resolve.go
    - internal/swarm/swarm_llm_resolve_test.go
    - internal/cron/handlers/agentjob_identity_resolve_test.go
    - internal/channels/telegram/commands_identity_resolve_test.go
    - internal/runner/agent_construction_invariant_test.go
  modified:
    - cmd/aura/llm_client.go
    - internal/runner/runner_identity_llm.go
    - internal/runner/runner_identity_llm_test.go
    - internal/swarm/swarm.go
    - internal/swarm/delegation_run.go
    - internal/cron/handlers/handler.go
    - internal/cron/handlers/agentjob.go
    - internal/channels/telegram/commands.go
    - internal/channels/telegram/bot.go
    - internal/channels/telegram/bot_dispatch.go
    - cmd/aura/serve_delegation.go
    - cmd/aura/serve_dispatch.go
    - cmd/aura/serve_channels.go

key-decisions:
  - "swarm.RunConfig.Client/LLM were NOT removed as exported struct fields — they remain the legitimate third priority in resolveWorkerLLM, because internal/swarm/runner_adapter.go's RunnerAdapter.Run (the SYNCHRONOUS swarm_spawn fan-out, out of this plan's file list) sets RunConfig.Client directly from the parent turn's own already-resolved client (agent.SwarmContext) with NO Runtime set at all — that is swarm's existing, correct 'worker inherits the parent's snapshot' mechanism, and removing the field would have broken it. What WAS removed is the boot-time capture that fed Client with the process-wide deployment client for the DELEGATION (background claim-loop) path specifically: cmd/aura/serve_delegation.go no longer writes Client/LLM into the worker template, so for that path a nil Resolver+nil Runtime now genuinely refuses instead of silently defaulting. handlers.AgentDeps.Client/LLM were kept for the identical reason — many pre-existing cron tests construct AgentDeps{Client: ...} directly and that remains a valid, explicit input."
  - "allowsKeylessLocalLLMBaseURL (internal/runner) already mirrored cmd/aura/llm_client.go's allowsKeylessLLMBaseURL verbatim from plan 02-01 — this plan's Task 1 reused that SAME classification by having identitykey.Decide take a caller-supplied BackendBills bool rather than adding a third copy of the host list or importing across the layering boundary. Decide itself never inspects a base URL string."
  - "identitykey.Decide's exemption check now fires REGARDLESS of whether a stored key exists (both HasKey=true and HasKey=false decide DecisionExemptLocal on a non-billing backend). The pre-existing IdentityLLMResolver only reached the D-13 exemption from inside its no-key branch, so a local-backend identity that happened to hold a stored key would previously have been charged to that key instead of exempted — a latent bug Decide's ordering fixes, not a behavior this plan set out to change, and disclosed as a Rule 1 fix."
  - "DecisionRefuseNoKey's snapshot shape (nil client + ErrNoIdentityLLMKey, not the new exhaustedClient sentinel) was deliberately preserved byte-for-byte from plan 02-01's shipped, pinned test (TestResolveRefusesWhenNoKey) — the plan's own text explicitly leaves this choice open ('your call'); reusing the existing contract avoided rewriting an already-correct, already-tested behavior."
  - "cmd/aura's buildIdentityLLMResolver — and every one of its three call sites (serve_delegation.go, serve_dispatch.go, serve_channels.go) — compares the concrete *runner.IdentityLLMResolver pointer for nil BEFORE it is ever assigned to an interface-typed struct field (RunConfig.Resolver / AgentDeps.Resolver / telegram.Deps.LLMResolver). Assigning a nil pointer directly into those fields would box it into a non-nil interface value, which every one of resolveWorkerLLM/resolveLLM/activeModelProfile's `!= nil` checks would then treat as 'configured' — silently routing every headless credential resolution through IdentityLLMResolver.SnapshotFor's own `rs == nil` nil-receiver guard instead of falling through to the intended Runtime/Client secondary. Caught during self-review before any commit, not shipped and fixed later."

patterns-established:
  - "A resolver priority is a named function returning (client, cfg, error), never an inline two-line read-then-override at the call site — this is what makes resolveWorkerLLM/resolveLLM independently testable and greppable, and what let the invariant test assert the OLD pattern's absence by exact literal rather than by re-deriving 'correctness'."

requirements-completed: [CRED-05, CRED-07, CRED-09]

coverage:
  - id: D1
    description: "internal/identitykey/policy.go's Decide is a pure, zero-I/O function returning four distinct Decision values (Allow/RefuseNoKey/RefuseNoCredit/ExemptLocal); the D-13 exemption is evaluated before the no-key refusal; a cap of 0.004 stays above zero through the comparison"
    requirement: CRED-05
    verification:
      - kind: unit
        ref: "internal/identitykey/policy_test.go#TestDecide_NoKeyOnOpenRouterRefuses"
        status: pass
      - kind: unit
        ref: "internal/identitykey/policy_test.go#TestDecide_ZeroCapRefuses"
        status: pass
      - kind: unit
        ref: "internal/identitykey/policy_test.go#TestDecide_LocalBackendWithNoKeyIsExemptNotRefused"
        status: pass
      - kind: unit
        ref: "internal/identitykey/policy_test.go#TestDecide_CapPrecisionDoesNotRoundToZero"
        status: pass
    human_judgment: false
  - id: D2
    description: "cmd/aura's creditExhaustedClient refuses a zero-credit turn before any network call, with a JSON {error:\"credit_exhausted\", hint:...} payload matching the UI-SPEC copy and carrying no digit"
    requirement: CRED-05
    verification:
      - kind: unit
        ref: "cmd/aura/llm_client_test.go#TestCreditExhaustedClientStreamRefusesWithoutNetwork"
        status: pass
      - kind: unit
        ref: "cmd/aura/llm_client_test.go#TestCreditExhaustedErrorPayload"
        status: pass
      - kind: unit
        ref: "cmd/aura/llm_client_test.go#TestLLMNotConfiguredClientStillWorks"
        status: pass
    human_judgment: false
  - id: D3
    description: "IdentityLLMResolver.SnapshotFor maps all four Decide outcomes, including the new zero-cap-to-exhausted-sentinel branch, and a cached exhausted snapshot is not served stale after a cap change without Invalidate"
    requirement: CRED-05
    verification:
      - kind: unit
        ref: "internal/runner/runner_identity_llm_test.go#TestResolverReturnsCreditExhaustedOnZeroCap"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_identity_llm_test.go#TestResolverCacheInvalidatedOnCapChange"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_identity_llm_test.go#TestResolveRefusesWhenNoKey"
        status: pass
    human_judgment: false
  - id: D4
    description: "Every non-interactive agent-construction site (both swarm sites, both cron sites) resolves its client through a named priority function rather than a boot-time-captured default, and a RunConfig/AgentDeps carrying neither a Resolver, a Runtime, nor an already-resolved Client refuses rather than proceeding with a nil client"
    requirement: CRED-07
    verification:
      - kind: unit
        ref: "internal/swarm/swarm_llm_resolve_test.go#TestSwarmWorkerInheritsParentSnapshot"
        status: pass
      - kind: unit
        ref: "internal/swarm/swarm_llm_resolve_test.go#TestSwarmWorkerResolvesFromResolverOverRuntime"
        status: pass
      - kind: unit
        ref: "internal/swarm/swarm_llm_resolve_test.go#TestNilRuntimePortFailsClosed"
        status: pass
      - kind: unit
        ref: "internal/cron/handlers/agentjob_identity_resolve_test.go#TestCronResolvesFromJobOwningIdentity"
        status: pass
      - kind: unit
        ref: "internal/cron/handlers/agentjob_identity_resolve_test.go#TestNilRuntimePortFailsClosed"
        status: pass
      - kind: unit
        ref: "internal/runner/agent_construction_invariant_test.go#TestEveryAgentConstructionResolvesFromTheTurnIdentity"
        status: pass
    human_judgment: false
  - id: D5
    description: "The composition root no longer writes chat.client into swarm.RunConfig or handlers.AgentDeps; both greps the plan's own <verify> block specifies are clean"
    requirement: CRED-07
    verification:
      - kind: other
        ref: "grep -rnE 'client, cfg := (rc|deps)\\.(Client|LLM)' internal/swarm/ internal/cron/ internal/channels/ | grep -v _test.go -> 0 lines"
        status: pass
      - kind: other
        ref: "grep -rnE 'Client:\\s+chat\\.client' cmd/aura/ | grep -v _test.go -> 0 lines"
        status: pass
    human_judgment: false
  - id: D6
    description: "The local-backend exemption is reachable and distinct from a refusal, both at the policy layer and through the resolver and the Telegram command surface"
    requirement: CRED-09
    verification:
      - kind: unit
        ref: "internal/identitykey/policy_test.go#TestDecide_LocalBackendIsExempt"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_identity_llm_test.go#TestResolverExemptLocalReturnsProcessSnapshot"
        status: pass
    human_judgment: false
  - id: D7
    description: "The Telegram channel's /cost render resolves the chat's LINKED identity's own model/spend profile through the same Resolver, falling back to the process-wide Runtime when unwired or unlinked — production wiring is present (serve_channels.go), production INVOCATION (dispatchRich's ctx) is a documented open item"
    requirement: CRED-07
    verification:
      - kind: unit
        ref: "internal/channels/telegram/commands_identity_resolve_test.go#TestTelegramTurnResolvesFromLinkedIdentity"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/commands_identity_resolve_test.go#TestTelegramFallsBackToRuntimeWithNoLinkedIdentity"
        status: pass
    human_judgment: true
    rationale: "commands.go's own logic and its composition-root wiring (serve_channels.go) are both proven, but internal/channels/telegram/bot_dispatch.go's onText still calls t.cmds.dispatchRich(daemonCtx, ...) with the raw, un-scoped daemon context rather than an identity-scoped one — a pre-existing gap this plan's file list (commands.go, not bot_dispatch.go) does not close. In production today, /cost still falls through to the process-wide Runtime for every Telegram user. See 'Known Gaps' below; a human must decide whether closing bot_dispatch.go's wiring belongs to a later plan in this phase or is out of scope."

duration: 30min
completed: 2026-09-09
status: complete
---

# Phase 2 Plan 5: Two Roles and a Budget — Credit Refusal and Fallback Closure Summary

**A pure `identitykey.Decide` policy backs a new `creditExhaustedClient` refusal sentinel wired into `IdentityLLMResolver`, and every non-interactive agent-construction site (both swarm paths, both cron paths, Telegram's spend display) now resolves its LLM credential through a named priority function instead of a boot-time-captured deployment client, closing the fail-open `client, cfg := rc.Client, rc.LLM` pattern at its two composition-root sources.**

## Performance

- **Duration:** ~30 min measured from the first RED commit (`5b44d355a`, 2026-09-09T16:26:30+02:00) to the last task commit (`1cc865f66`, 2026-09-09T16:55:56+02:00) — excludes the extensive upfront reading of the plan, the codebase (`internal/identitykey`, `internal/runner`, `internal/swarm`, `internal/cron/handlers`, `internal/channels/telegram`, the `cmd/aura` composition root) and the phase's prior SUMMARYs/RESEARCH/PATTERNS/VALIDATION docs, which this plan's own `<required_reading>` mandated and which took considerably longer than the commit span
- **Tasks:** 3
- **Commits:** 7 (Task 1: 2, Task 2: 2, Task 3: 2, plus 1 out-of-task-boundary fix found during self-review)
- **Files created/modified:** 21 (8 created, 13 modified)

## Accomplishments

- **Task 1 — the pure credit decision.** `internal/identitykey/policy.go` declares `Decide(DecisionInput) (Decision, error)`: four distinct values (`DecisionAllow`, `DecisionRefuseNoKey`, `DecisionRefuseNoCredit`, `DecisionExemptLocal`), zero I/O, the D-13 exemption evaluated before the no-key check (pinned by `TestDecide_LocalBackendWithNoKeyIsExemptNotRefused`), and a sub-cent cap (`0.004`) surviving the comparison without rounding to a refusal. Reuses the package's own pre-existing `ErrNoKey` sentinel and `llm.ErrSpendNotApplicable` rather than declaring new ones for either case.
- **Task 2 — the refusal that fires before the network.** `cmd/aura/llm_client.go` gains `creditExhaustedClient`/`creditExhaustedError`, structurally identical to the pre-existing `llmNotConfiguredClient`, sharing a new `marshalRefusalPayload` helper to stay under `dupl`'s threshold. `cmd/aura/llm_client_test.go` is new — it pins both sentinels' payloads for the first time. `internal/runner`'s `IdentityLLMResolver.SnapshotFor` now runs `identitykey.Decide` and maps all four outcomes; the exhausted-client snapshot is cached (so `Invalidate` matters after a top-up), the no-key refusal keeps plan 02-01's exact pinned shape (nil client + `ErrNoIdentityLLMKey`), and the exemption is now reachable regardless of whether a stored key happens to exist (a latent bug fix, see Deviations).
- **Task 3 — the boot-time fallback closed at every non-interactive construction site.** `internal/swarm/swarm_llm_resolve.go` (new) is the single place `runChild` resolves a worker's client: Resolver+IdentityID first (the delegation claim loop's own job identity, now threaded via `delegation_run.go`'s `rc.IdentityID = job.IdentityID`), Runtime second, an already-resolved `rc.Client` third (the synchronous `swarm_spawn` fan-out's legitimate parent-inheritance shape), else a refusal. `internal/cron/handlers` gets the identical priority in `AgentJobHandler.resolveLLM`, keyed on `identityctx.IdentityID(ctx)` — bound from the task row by cron's existing `scheduledOperationContext`, never from an HTTP principal. `cmd/aura/serve_delegation.go` and `serve_dispatch.go` stop writing `chat.client`/`chat.cfg.LLM` into the worker templates and instead wire a new `buildIdentityLLMResolver(chat)` — the first production construction of `identitykey.Store` + `runner.IdentityLLMResolver` anywhere in the daemon. `internal/runner/agent_construction_invariant_test.go` source-scans six named files for the closed pattern.

## Task Commits

1. **Task 1 RED — the credit-decision policy, seeded off-by-one** — `5b44d355a` (test)
2. **Task 1 GREEN — fix the zero-cap boundary** — `30efe0b15` (feat)
3. **Task 2 RED — the credit-exhausted refusal + Decide-driven resolver, seeded typo** — `4ea1a39ff` (test)
4. **Task 2 GREEN — fix the `credit_exhausted` code typo** — `98892e713` (feat)
5. **Task 3 RED — close the boot-time fallback, seeded priority swap** — `e6adfe1c1` (test)
6. **Task 3 GREEN — fix the Runtime/Resolver priority swap** — `2fe4bbdbe` (feat)
7. **Out-of-task-boundary fix, found during Task 3's own acceptance-criteria self-check** — `1cc865f66` (fix)

**Plan metadata:** *(this commit, immediately following)*

## Files Created/Modified

- `internal/identitykey/policy.go` (+`policy_test.go`) — the pure credit decision (CRED-05/CRED-09)
- `cmd/aura/llm_client.go` (+`llm_client_test.go`, new) — `creditExhaustedClient`, `marshalRefusalPayload`
- `internal/runner/runner_identity_llm.go` (+`_test.go`) — Decide-driven `SnapshotFor`, `exhaustedClient` injection
- `internal/swarm/swarm_llm_resolve.go` (new, +`_test.go`), `swarm.go`, `delegation_run.go` — `resolveWorkerLLM`, `RunConfig.Resolver`, `rc.IdentityID` from `job.IdentityID`
- `internal/cron/handlers/handler.go`, `agentjob.go` (+`agentjob_identity_resolve_test.go`, new) — `AgentDeps.Resolver`, `AgentJobHandler.resolveLLM`
- `internal/channels/telegram/commands.go`, `bot.go`, `bot_dispatch.go` (+`commands_identity_resolve_test.go`, new) — `activeModelProfile(ctx)`, `commandDeps.Resolver`, `Deps.LLMResolver`
- `internal/runner/agent_construction_invariant_test.go` (new) — the six-file source-scan invariant
- `cmd/aura/serve_delegation.go`, `serve_dispatch.go`, `serve_channels.go` — `buildIdentityLLMResolver`, stop writing `chat.client`/`LLM` into worker templates, nil-safe interface wiring at all three call sites

## Decisions Made

See `key-decisions` in the frontmatter. The two most consequential: (1) `swarm.RunConfig.Client`/`LLM` and `handlers.AgentDeps.Client`/`LLM` were kept as fields rather than removed — they remain the legitimate third priority for the synchronous swarm fan-out and for existing test fixtures; only the *boot-time capture that populated them for the delegation/cron paths* was removed; (2) every `buildIdentityLLMResolver` call site compares the concrete pointer for nil before it reaches an interface-typed field, avoiding the exact typed-nil-in-interface trap 02-04's own key-decisions already documented and that `telegramSteerOrNil` already guards `Steer` against in the same file — caught during self-review before any commit.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `identitykey.Decide`'s exemption ordering fixes a latent bug in the pre-existing resolver**
- **Found during:** Task 1, while designing `Decide`'s contract against the plan's explicit `TestDecide_LocalBackendIsExempt` requirement ("whether or not a record exists")
- **Issue:** the pre-existing `IdentityLLMResolver.noKeySnapshot` (plan 02-01) only reached the D-13 local-backend exemption from *inside* the no-key branch — a local-backend identity that happened to hold a stored OpenRouter key would have been billed to that key instead of exempted, contradicting D-13 ("credits apply to OpenRouter only... local backend bills nothing").
- **Fix:** `Decide` evaluates the exemption unconditionally before checking key presence; `SnapshotFor`'s rewrite routes through it the same way.
- **Files modified:** `internal/identitykey/policy.go`, `internal/runner/runner_identity_llm.go`
- **Verification:** `TestDecide_LocalBackendIsExempt` asserts both `HasKey: true` and `HasKey: false`.
- **Committed in:** `30efe0b15` (Task 1 GREEN)

**2. [Rule 1 - Bug, caught pre-commit] typed-nil-in-interface at all three `buildIdentityLLMResolver` call sites**
- **Found during:** Task 3, self-review immediately after wiring `Resolver: buildIdentityLLMResolver(chat)` into three struct literals, before any of the three edits were committed
- **Issue:** `buildIdentityLLMResolver` returns a concrete `*runner.IdentityLLMResolver`, nil when disabled (no `AURA_AUTHULA_SECRET`). Assigning that directly into an interface-typed field (`RunConfig.Resolver`, `AgentDeps.Resolver`, `telegram.Deps.LLMResolver`) would box a nil pointer into a non-nil interface value — every `!= nil` check in `resolveWorkerLLM`/`resolveLLM`/`activeModelProfile` would then treat a *disabled* resolver as configured, routing every call through `SnapshotFor`'s own nil-receiver guard instead of falling through to Runtime/Client as intended. The exact class of bug 02-04's own key-decisions documented for a nil-pool store, and the one `telegramSteerOrNil` already guards `Steer` against in the same file.
- **Fix:** at each of the three sites, the concrete pointer is compared for nil in a local variable BEFORE the interface-typed field is ever assigned.
- **Files modified:** `cmd/aura/serve_delegation.go`, `serve_dispatch.go`, `serve_channels.go`
- **Verification:** repo-wide `go build ./...` clean; all three composition-root files compile with the guard in place; no test could have caught the un-guarded version without a live daemon boot with `AURA_AUTHULA_SECRET` unset, which is exactly why this was caught by inventory-before-commit rather than by a red test.
- **Committed in:** `e6adfe1c1` (Task 3 RED — the fix was present from the first commit, never shipped broken)

**3. [Rule 1 - Bug] `DecisionRefuseNoKey` covered only via `switch`'s `default`**
- **Found during:** Task 3's own acceptance-criteria self-check ("internal/runner/runner_identity_llm.go contains all four identitykey.Decision values")
- **Issue:** the literal identifier `identitykey.DecisionRefuseNoKey` appeared only in a comment on the `default:` branch, not as code — and a `default` branch silently absorbs any future fifth `Decision` value as a no-key refusal rather than surfacing it explicitly.
- **Fix:** `DecisionRefuseNoKey` is now its own `case`; a genuinely unrecognized value hits a new `default` that also refuses, naming the unrecognized value.
- **Files modified:** `internal/runner/runner_identity_llm.go`
- **Verification:** `go test ./internal/runner/...` green; `golangci-lint` clean.
- **Committed in:** `1cc865f66`

---

**Total deviations:** 3 (2 bug-fixes, 1 caught-before-commit). **Impact:** all three improve correctness without expanding scope; none required an architectural decision.

## Issues Encountered

- **The interactive runner (web + Telegram turns) still resolves the process-wide deployment client, not an identity's own.** `internal/runner/runner.go:392`'s `NewLlmAgent` call reads `r.llmSnapshot(ctx)`, which falls back to `r.runtime.Snapshot()` unless something has already called `IdentityLLMResolver.ScopeContextToIdentitySnapshot(ctx, identityID)` and put the result on `ctx` via `withLLMRuntimeSnapshot` BEFORE `turnLocked` runs. Nothing in the codebase does that today — confirmed by grep (`ScopeContextToIdentitySnapshot`/`withLLMRuntimeSnapshot` have exactly one production call site each, both inside `internal/runner` itself, neither reached from an HTTP handler). This is **not a regression introduced by this plan** — plan 02-01's own SUMMARY already disclosed it ("This plan does NOT wire IdentityLLMResolver into runner.turnLocked's live per-turn call path"), and this plan's Task 3 file list (`swarm.go`, `delegation_run.go`, `handler.go`, `agentjob.go`, `commands.go`, `serve_delegation.go`, `serve_dispatch.go`) never included `runner.go` or its HTTP entry point. Since Telegram routes every real turn through the SAME `runner.Run`/`turnLocked` path (confirmed: the only three production `agent.NewLlmAgent` call sites in the whole repo are `runner.go:392`, `swarm.go:293` and `cron/handlers/handler.go:129` — Telegram constructs no agent of its own), this means **every actual agent turn — web or Telegram — still bills the deployment-wide OpenRouter key today**, regardless of this plan's work. CRED-05's refusal and CRED-07's fallback closure are both real and tested for the paths this plan touched (swarm, cron, and Telegram's `/cost` display), but the single highest-value site — the live turn itself — remains unwired. This is the largest open item in the phase and should be the first thing `/gsd-verify-work` or a later plan addresses; a live run against `aura serve` today would show a zero-cap identity's chat message still succeeding, billed to the operator's own key.
- **Telegram's `/cost` Resolver wiring is present but not invoked with a scoped context in production.** `commands.go`'s `activeModelProfile` and its composition-root wiring (`serve_channels.go`'s `LLMResolver: buildIdentityLLMResolver(chat)`) are both correct and unit-tested, but `internal/channels/telegram/bot_dispatch.go`'s `onText` calls `t.cmds.dispatchRich(daemonCtx, chatID, text)` with the raw daemon context — never an identity-scoped one — even though `t.requireLinkedMessage` just resolved and validated the link one line earlier and discards the result. `bot_dispatch.go` was not in this plan's file list. Until a later change threads the resolved identity onto the ctx passed to `dispatchRich`, `/cost` continues to render the process-wide Runtime's figures for every Telegram user in production, same as before this plan.
- **Embedding/multimodal/asset/voice clients remain unconverted**, exactly as `02-CONTEXT.md`'s CRED-07 flagged assumption states: `cmd/aura/embedding_client.go`, `internal/multimodal/client.go`, `internal/assets/image_processor.go`, `internal/assets/audio_processor.go` and `internal/agui/voice_api.go` all spend at the provider and do not go through this resolver. Under a per-identity *key* (rather than a per-identity ledger) they are capped the moment they use the identity's client — but only once something resolves that client for them, which is outside this plan's inventory and stated as such rather than silently omitted.
- **A provider 403 arriving MID-turn is not specially handled — recorded, not changed, per the plan's own instruction.** `internal/agent/llm_agent_stream_retry.go`'s `retryableStreamOpenError` retries only `429` and `>=500` HTTP statuses (`option.WithMaxRetries(0)` at the openai_compat client layer already disables the SDK's own retries, confirming `02-RESEARCH.md` Q3's read); a `403` is NOT in that set, so it surfaces on the FIRST attempt. The caller (`llm_agent.go`'s turn loop) treats any non-`ErrBreakerOpen` stream-open error identically: `turnReason = "stream_open_error"; yield(nil, err)` — the turn's event stream ends with a real Go error in the `iter.Seq2` error slot, which the runner/AG-UI layer already renders as a `RUN_ERROR`. This means a mid-turn 403 (a top-up that lapses, or M-06's 25s re-block window) behaves EXACTLY like CRED-05's pre-flight refusal at the transport level — no retry, immediate surfacing — but with one difference: it carries OpenRouter's own raw error body, not `creditExhaustedClient`'s friendly `{"error":"credit_exhausted","hint":"..."}` payload, since the provider (not Aura's sentinel) produced it. The cockpit would render whatever generic-error handling it has for a non-JSON or differently-shaped `RUN_ERROR`, not the CRED-05 copy. This UX gap is real but explicitly out of this plan's scope per its own `<planner_assumptions>` — left for `/gsd-verify-work` to weigh.
- **Three pre-existing test failures, unrelated to this plan, confirmed environment-specific.** A full `go test ./...` run surfaced `TestContainedDirUnresolvableRoot` (`internal/idroot` — a Windows file-lock race removing a temp dir mid-test), `TestScopedRootsDeriveBothPerIdentityPaths` (`internal/skills` — a Windows-path-separator assertion expecting a Unix-style path), and `TestUnreachableJWKSIsReportedOnce` (`cmd/arcadedb-mcp` — DNS/network-timing dependent). None of the three packages imports anything this plan touched; none was in this plan's file list. Not fixed, per the deviation rules' scope boundary.

## User Setup Required

None — no external service configuration required. `buildIdentityLLMResolver` degrades to disabled (nil, falls back to Runtime) when `AURA_AUTHULA_SECRET` is unset, matching every other consumer of that secret in this codebase.

## Next Phase Readiness

- **Ready:** the credit-refusal sentinel and the fallback-closure pattern are real, tested and wired for every path this plan's file list named — both swarm sites, both cron sites, and Telegram's spend display's own logic. `identitykey.Decide` is the single pure file plan `02-10`'s `credit_policy` `GO_SCOPES` extension will target for mutation testing.
- **Blocker for `/gsd-verify-work`'s closing live run:** the interactive runner (the actual turn path, web AND Telegram) is NOT wired to seam A — a zero-cap identity's live chat turn today still succeeds against the process-wide deployment key. This is inherited from plan 02-01, not introduced here, but it means CRED-05's "refused before the model is called" and CRED-07's "no fallback to the deployment key" are proven at the unit/component level for this plan's files and NOT yet true end-to-end for a real turn. Whoever plans the wiring into `runner.turnLocked` (or the HTTP/AG-UI layer immediately above it) should read this SUMMARY's Issues Encountered section first — the seam (`ScopeContextToIdentitySnapshot`) already exists and needs exactly one caller.
- **Also open:** `bot_dispatch.go`'s `dispatchRich` call needs an identity-scoped ctx for `/cost`'s Resolver wiring to take effect in production; the embedding/multimodal/asset/voice clients remain outside any per-identity resolution; a mid-turn 403's UX (raw provider error vs. the CRED-05 friendly copy) is unaddressed by design.
- Migration numbers and coverage policy: this plan touched no migrations and no new packages requiring `scripts/coverage_package_policy.json` registration (`internal/identitykey` was already registered by plan 02-01).

## Self-Check: PASSED

- All 8 created files confirmed present on disk (`[ -f ]`): `internal/identitykey/policy.go`, `policy_test.go`; `cmd/aura/llm_client_test.go`; `internal/swarm/swarm_llm_resolve.go`, `swarm_llm_resolve_test.go`; `internal/cron/handlers/agentjob_identity_resolve_test.go`; `internal/channels/telegram/commands_identity_resolve_test.go`; `internal/runner/agent_construction_invariant_test.go`.
- All 7 task commit hashes confirmed in `git log --oneline`: `5b44d355a`, `30efe0b15`, `4ea1a39ff`, `98892e713`, `e6adfe1c1`, `2fe4bbdbe`, `1cc865f66`.
- Re-ran every acceptance-criteria grep from the plan's own `<verify>` blocks: all clean (0 matches for both fallback patterns; `credit_exhausted` present exactly once in `cmd/aura/llm_client.go`, zero times in `internal/` non-test files; no digit in the credit-exhausted hint).
- `go build ./...`, `go vet ./...`, `bash scripts/check-file-size.sh`, and `golangci-lint run` all clean on current HEAD.
- `go test -race -count=1 ./internal/identitykey/ ./internal/runner/ ./internal/swarm/ ./internal/cron/handlers/ ./internal/channels/telegram/ ./cmd/aura/` — green, run live via WSL (all 6 packages pass under the race detector).

---
*Phase: 02-two-roles-and-a-budget*
*Completed: 2026-09-09*
