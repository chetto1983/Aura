# Phase 1: Two Identities, Live and Separated - Research

**Researched:** 2026-09-07
**Domain:** Multi-tenant identity isolation in an existing Go monolith (config gating, a
cross-store provisioning saga, Postgres RLS, per-identity ArcadeDB tenancy, a per-identity
Docker sandbox, and a live AG-UI/Authula HTTP surface) — not an unfamiliar external
technology. Every claim below is anchored to source read this session.
**Confidence:** HIGH for what exists today (all read directly); MEDIUM/LOW is called out
per-claim below for what is inferred about behaviour under conditions not yet run live.

## Summary

Five of six requirement-bearing capabilities in this phase already exist in the codebase in a
more complete state than the roadmap prose implies, and two things that look like small flag
flips are gated by contracts that make them deliberately non-trivial. `AURA_MUSR_ISOLATION`
is a pure provisioning gate — the field comment states explicitly "It is NOT a query switch —
no read path branches on it, and none ever should" (`internal/config/config.go:304-305`) — so
flipping it does not touch any read path; every plane it would expose is already scoped by
identity in SQL/RLS/ArcadeDB/Garage. The real work in Success Criterion 1 is the *chain*
around the flag: a Fatal gate requires a strict `AURA_PROFILE`
(`internal/config/config_validate.go:113-129`), and a strict profile routes every shell/file
tool through the per-identity sandbox, whose image is pulled lazily on the *first* tool call
(`internal/sandbox/usersandbox/docker_backend_lifecycle.go:228-241`) — there is no boot
preflight today, so a strict deployment with an unbuilt/unpublished image boots healthy and
then denies every tool call with no diagnostic at boot time.

The provisioning saga (`internal/agui/onboarding_provision.go`) already runs eager, idempotent,
compensated legs for ArcadeDB memory, Garage objects and filesystem roots
(`internal/agui/onboarding_provision_resources.go`) — Success Criterion 1's E2E-02 resources
are three-quarters built. The sandbox leg is genuinely missing from *provisioning*, but its
*de-provisioning* mirror already exists and is already wired at the composition root
(`cmd/aura/serve_provisioning.go:296-313`, `internal/agui/deprovision.go:76-194`) — the new
provisioning leg has a proven `Resolve`/`Destroy` pair on `SandboxRouter` to wrap
(`internal/sandbox/usersandbox/router.go:79-99,152-167`), not a new mechanism to invent.

The two_identity_e2e test file that the roadmap says gets "promoted from harness to a live
run" is, as read, a harness in the strict sense: `TestTwoIdentityCrossDeny` and
`TestProvisionLoginIsolatedRun` both provision identities by a raw SQL `INSERT`
(`musrProvisionIdentity`, `cmd/aura/two_identity_e2e_harness_test.go:93-114`), never call
`onboardingService.Provision`, and drive conversations through an in-process
`httptest.NewServer` wrapping `agui.NewServer` directly — never a spawned `aura serve`
process, never a real Authula login. Promoting this to Success Criterion 2/E2E-01's bar (two
authenticated concurrent `/agent/run` conversations through the real gateway) is closer to new
work than a tag addition. The nearest existing analog is `scripts/agui_smoke.sh`, which
already builds the binary, backgrounds `aura serve`, polls `/healthz`, drives a real Authula
CSRF+cookie login (including TOTP verify), and asserts on live SSE frames
(`scripts/agui_smoke.sh:129-190,203-330`) — but it drives the bootstrap-seeded `local`
identity, which never passes through `onboarding_provision.go`'s `EnforceFirstLogin` (forced
password change + mandatory TOTP *enrollment*, not just verification, on first login;
`internal/agui/onboarding_provision.go:204-210`). No existing script exercises that
first-login-of-a-provisioned-identity flow end to end.

A concrete, previously-undocumented CI hazard surfaced during this research and directly
threatens D-10/D-13's plan to add ArcadeDB to the `musr-e2e` job: `arcadedb-mcp` declares
`depends_on: aura: condition: service_started` (`compose.yaml:695-699`), so any
`docker compose up` that names `arcadedb-mcp` also starts the *entire* `aura` daemon —
scheduler included — against the same Postgres a tagged Go test writes to. This exact
mechanism is a **measured** production incident cited in the Makefile itself
("Measured on CI #1809 ... the coverage job ... started the daemon at 16:56:59 and the gate
at 16:57:05, and internal/cron's bounded-retry test saw one delivery fewer than it
performed sweeps", `Makefile:262-270`) and the fix already exists as a pattern
(`memory-up-core`, not `memory-up`, `Makefile:275-279`) and as a second pattern (`docker run`
the arcadedb-mcp image directly with `--no-deps`-equivalent isolation, bypassing compose
`depends_on` entirely, already proven in the `web-e2e` CI job, `.github/workflows/ci.yml:1683-1710`).
D-11 item 3 (a real OAuth-verified `arcadedb-mcp` call) needs the sidecar live, which needs one
of these two avoidance patterns, not the current musr-e2e job's `make db-migrate memory-up`
(`.github/workflows/ci.yml:481-486`), which uses the daemon-starting form today.

**Primary recommendation:** Treat this phase as three separable engineering fronts — (1) the
boot/install/config chain (flag + profile + preflight + install.sh sandbox image), (2) the
provisioning-saga completion (CLI verb + eager sandbox leg + memory cross-deny plane), and
(3) the live closing run (a *new* committed multi-identity harness modeled on
`scripts/agui_smoke.sh`'s process lifecycle, not an extension of the existing
`two_identity_e2e_*` harness file) — because each has a different existing-code baseline and a
different risk profile, and none of the three currently blocks the others.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Multi-identity boot gate (`AURA_MUSR_ISOLATION` + strict profile) | Backend / config | — | `internal/config` validates at daemon boot; no browser or CDN concern |
| Sandbox-image boot preflight | Backend / daemon boot | Docker runtime | New Fatal-or-INFO gate must run before HTTP listeners open; needs Docker I/O, which today's `Config.Validate()` contract explicitly forbids (`internal/config/config_validate.go:79-85` "It NEVER first-fails ... performs no other I/O") |
| Identity provisioning saga (Authula + aura.* + ArcadeDB + Garage + fs + sandbox + Telegram) | Backend / API | Database, ArcadeDB, Garage, Docker | `internal/agui/onboarding_provision.go` is the sole cross-store writer; every leg is a backend concern |
| `aura identity create` CLI | Backend / CLI | — | Thin wrapper calling the same in-process service (`onboarding_api.go:199`'s pair), no new HTTP surface |
| Data-plane cross-deny (Postgres RLS, ArcadeDB tenancy, Garage keys) | Database / Storage | Backend (query construction) | Isolation is enforced server-side by Postgres RLS and ArcadeDB's per-database credential, not by Go code filtering |
| Execution-plane cross-deny (Budget, Registry, gateway ledger, steer inbox, sidecar spillover) | Backend / Agent runtime | Database (steer_queue, gateway ledger tables) | Lives entirely inside the `internal/agent`/`internal/runner`/`internal/gateway` process; no browser/CDN involvement |
| The closing live run (two concurrent `/agent/run` conversations) | Backend / API (AG-UI gateway) | Browser (none — a scripted harness, not a UI) | D-16 explicitly chose a committed harness over an operator-driven browser session |

## Project Constraints (from CLAUDE.md)

These directives are treated as locked and were used to scope every recommendation below:

- **NEVER SUPPOSE / READ THE DOCUMENTATION FIRST** — every claim below cites a `path:line`
  read this session; nothing is asserted from training memory about this codebase.
- **INVENTORY BEFORE INVENTION** — this research found the sandbox leg's compensation
  (`DestroySandbox`), the disposable-Postgres pattern (`coverage_docker.sh`), and the
  live-daemon-driving pattern (`agui_smoke.sh`) already exist; the plan should wrap them, not
  rebuild them.
- **NO GOD CLASS (≤600 LOC)** — `internal/agui/onboarding_provision.go` is close to its
  ceiling; a new sandbox leg belongs in `onboarding_provision_resources.go` (the file this
  concern already forced a split into), not the main saga file. `cmd/aura/serve.go` is 598
  lines (`wc -l` measured this session) — one line under the ceiling — so a new boot preflight
  needs its own file, not an addition to `serve.go`.
- **Migration numbering** — `ls internal/db/migrations/ | tail -1` returned
  `0119_drop_orphan_content_parts.up.sql` this session (measured, not looked up in a doc). If
  this phase turns out to need a migration (none of the CONTEXT.md decisions call for one —
  D-10's fifth build tag and D-12's script extraction are not schema changes), `0120` is the
  next free slot **as of this research date**; the planner/executor must re-run the `ls`
  command at landing time, not copy this number.
- **COVERAGE FLOOR 85% / package-local policy** — see Validation Architecture below;
  `internal/agent`, `internal/agui`, `internal/config`, `internal/gateway`, `internal/steer`
  are all `"mode": "target"` (85% floor) in `scripts/coverage_package_policy.json`;
  `internal/arcadedb` and `internal/sandbox/usersandbox` are `"mode": "delegated"` to the
  `arcadedb_integration` and `docker_integration` reports respectively — this phase touches
  all of them.
- **NO-SKIP-AS-GREEN** — every new live test must follow the existing `musrEnvOrSkip` /
  `envOrSkip` shape (`t.Fatal` under `$CI`, `t.Skip` locally).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** The shipped multi-identity default lives in `.env.example` and in
  `scripts/install.sh`'s fresh-`.env` heredoc (around `scripts/install.sh:612`), **not** in
  `compose.yaml`. Both set `AURA_PROFILE=single_user_hardened`, `AURA_MUSR_ISOLATION=true`
  and `AURA_SANDBOX_IMAGE`. `compose.yaml:150` (`${AURA_MUSR_ISOLATION:-false}`) and
  `compose.yaml:313` (`${AURA_PROFILE:-dev}`) keep their upgrade-safe fallbacks unchanged.
- **D-02:** `ensure_internal_env_secrets` (`scripts/install.sh:541-554`) is **not** touched —
  it runs on the already-have-a-`.env` upgrade path.
- **D-03:** `aura serve` gains a boot preflight: under a strict profile with
  `AURA_MUSR_ISOLATION=true`, refuse to start when `AURA_SANDBOX_IMAGE` is neither present nor
  pullable, naming the exact build/pull command.
- **D-04:** `scripts/install.sh` builds (or pulls) the sandbox image as an install step, before
  `docker compose up`.
- **D-05:** Existing operators learn multi-identity is available from a single INFO line at
  boot when a non-strict deployment starts, pointing at `docs/runbooks/musr-rollout.md`. The
  runbook is updated with the sandbox-image step and with `make musr-e2e` replacing the raw
  `go test -tags '...'` line in its Acceptance section.
- **D-06:** Telegram stays a **required** port of the provisioning saga
  (`internal/agui/onboarding_provision.go:160`).
- **D-07:** `aura identity create` is added to `cmd/aura` as a thin front over the identical
  service: it calls `onboardingService.StartSession(ctx, operatorID)` then
  `Provision(operatorID, token, req)` — the exact pair `internal/agui/onboarding_api.go:199`
  uses. No new saga entry point, no relaxed validation, no parallel implementation.
- **D-08:** The acceptance gate provisions with a **fake `TelegramMint`** (in-memory
  `InsertPending`/`DeletePending`/`PendingConsumed` plus a stub bot name) wired at the test
  composition root. The **real** bot is used exactly once, in the phase-closing scored live
  run.
- **D-09:** The per-identity sandbox box becomes an **eager, idempotent, compensated leg** of
  the provisioning saga alongside the existing memory/Garage/filesystem legs in
  `internal/agui/onboarding_provision_resources.go`. Today it is created lazily at
  `SandboxRouter.Route` → `createBox` (`internal/sandbox/usersandbox/docker_backend_lifecycle.go:58`).
- **D-10:** The long-term-memory plane joins the existing gate: `cmd/aura/two_identity_e2e_test.go`
  gains `arcadedb_integration` as a **fifth** build tag and a memory plane, and the `musr-e2e`
  CI job (`.github/workflows/ci.yml:380`) gains `arcadedb` to its `docker compose up -d` (it
  brings up only `garage` today, `ci.yml:490`). The always-compile floor step (`ci.yml:465`)
  gains the tag too.
- **D-11:** Memory cross-deny is asserted on **all three** surfaces: (1) the model-facing tool
  under identity B's `identityctx`, proving `identityctx → DatabaseFor → derived credential`;
  (2) identity B's HMAC-derived credential aimed directly at identity A's database over
  ArcadeDB's HTTP API, expecting a `SecurityException`; (3) the `arcadedb-mcp` sidecar called
  as B with a verified access token whose `sub` is B, proving the tenant selector honours the
  token and not a caller-supplied header.
- **D-12:** `scripts/lib/disposable_stack.sh` is **extracted** from `scripts/coverage_docker.sh`
  (provision disposable Postgres container, create `aura_app`/`aura_migrate`, create the
  throwaway DB owned by `aura_migrate`, trap-drop on exit, hard-refuse the name `aura` with
  exit 4). Both `coverage_docker.sh` and the new `scripts/musr_e2e.sh` source it.
- **D-13:** `make musr-e2e` brings up everything it needs — disposable Postgres via the lib,
  `docker compose up -d garage arcadedb`, readiness wait, `go run ./scripts/authula_seed_e2e.go`,
  the tagged test, then teardown of what it started. The CI job calls the same target.
- **D-14:** The proof is a **concurrent-runner white-box test**: two `LlmAgent` runs driven
  concurrently under two `identityctx` values in one process, under `-race` and `goleak`, with
  a deliberate collision attempt (same tool, same arguments, overlapping in time) that must
  neither replay nor cross. Not a randomized-interleaving harness and not a production hot-path
  assertion.
- **D-15:** Four shared surfaces must each be enumerated and proven disjoint (research may
  measure them; it may **not** silently shorten this list): (1) run-dir sidecar spillover
  (`internal/agent/tools/result.go:205`); (2) gateway ledger/idempotency + approvals
  (`ReservationKey{ConversationID, RequestID, ToolCallID}`, `internal/gateway/approve.go:218-220`);
  (3) `tools.Registry`, `Budget`, `SteerInbox`; (4) `llm.Client`, `PromptBuilder`, KV prefix.
- **D-16:** A **committed harness** drives both conversations: authenticates as each identity
  and drives two concurrent `POST /agent/run` conversations through the real AG-UI gateway
  with real tool calls, capturing both transcripts and their timing.
- **D-17:** Both identities perform the **same three tasks on their own data, deliberately
  raced**: a document search, a memory write, a sandbox command, timed to overlap.
- **D-18:** A ≥9.8 scoring rubric is written down (dimensions, weights, what 9.8 means) and the
  transcripts are scored against it each phase — **but the rubric does not gate.** What blocks
  the phase is the machine-checkable half. `internal/agenteval/case.go:14-17` stands
  **unamended**.

### Claude's Discretion

- Every cross-deny assertion carries a **positive control**: identity A must still read its own
  document/fact/object in the same run.
- Naming of the CLI verb's flags, the rubric's exact dimension names, and the internal
  structure of `scripts/lib/disposable_stack.sh` are open.

### Deferred Ideas (OUT OF SCOPE)

- Full ISO-10-shaped deprovisioning drill beyond the new eager sandbox leg's own compensation
  (Phase 5 / ISO-10).
- A randomized-interleaving / property harness for execution isolation (the LibreChat Bombadil
  shape) — revisit if D-14 proves too coarse, or with Phase 3.
- A fail-closed identity assertion in the runner hot path — reconsider only if a D-15 surface
  turns up something a test cannot constrain.
- Publishing the sandbox image to a registry as a general practice beyond the edge channel
  default already in install.sh — revisit at Phase 7.
- Cockpit-driven identity creation — already owned by Phase 2 SC5.
- Rotation path for `AURA_ARCADEDB_TENANT_SECRET` — Phase 5 concern.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ISO-01 | `AURA_MUSR_ISOLATION` on in the shipped profile; second identity provisioned end to end | `internal/config/config.go:303-321,554`, `internal/config/config_validate.go:113-129`, `.env.example:44`, `compose.yaml:150,313`, `scripts/install.sh:507-538,588-661` — the flag/profile/preflight/install chain is fully mapped below |
| ISO-02 | Two identities cannot read each other's data on any plane | `cmd/aura/two_identity_e2e_test.go` (5 planes proven live), `internal/arcadedb/tenant.go`, `cmd/arcadedb-mcp/identity.go` + `tenant.go` (the missing 6th plane's mechanism, confirmed present) |
| ISO-02a | The gate runs unattended, CI + clean checkout | `scripts/coverage_docker.sh:1-130` (disposable-stack pattern to extract), `.github/compose.ci-musr.yaml` (Garage :3903 loopback publish already fixed), `.github/workflows/ci.yml:380-546` (current job shape) |
| ISO-05 | One identity's turn cannot observe/affect another's execution | `internal/agent/llm_agent.go:39-167` (per-turn construction confirmed), `internal/agent/agent.go:58-91` + `internal/agent/budget.go` (Budget is per-turn, not global — refines D-15 item 3), `internal/agent/tools/result.go:195-206`, `internal/gateway/approve.go:73-227`, `internal/steer/pg_store.go:75-243` (all three surfaces confirmed conversation-keyed, not identity-keyed) |
| E2E-01 | Two identities hold real concurrent conversations, scored ≥9.8 | `scripts/agui_smoke.sh` (nearest existing process-lifecycle/auth pattern), `internal/agenteval/case.go:1-60` (no-rubric-gates philosophy, D-18 consistent) |
| E2E-02 | Second identity onboarded zero to useful conversation, no manual step | `internal/agui/onboarding_provision.go` (full saga read), `internal/agui/onboarding_provision_resources.go` (existing eager legs), `internal/agui/deprovision.go:76-194` + `cmd/aura/serve_provisioning.go:296-313` (the sandbox leg's existing compensation half) |
</phase_requirements>

## Standard Stack

This phase adds **no new external dependency**. Every capability is built from packages
already in `go.mod` and services already in `compose.yaml`. The "stack" here is internal
composition, not third-party selection.

### Core (existing, reused)

| Component | Where | Purpose | Why it's the standard here |
|---|---|---|---|
| `internal/config` profile/gate machinery | `internal/config/config_validate.go` | Fatal/Warn violation aggregation at boot | Every other strict-tier gate (`gateObjectStoreCreds`, `gateGarageRPCSecret`) already follows this shape; a new preflight should match it structurally even though it cannot literally live inside `ValidateProfile` (see Pitfall below) |
| `internal/agui` onboarding saga | `internal/agui/onboarding_provision*.go` | Cross-store provisioning with per-leg compensation | Sole existing writer for identity creation; adding a leg here is additive, not a new pattern |
| `usersandbox.SandboxRouter` | `internal/sandbox/usersandbox/router.go` | Per-identity Docker box lifecycle | `Resolve`/`Destroy` are already the exact create/compensate pair D-09 needs |
| `internal/arcadedb.TenantCredentials` / `DatabaseFor` | `internal/arcadedb/tenant.go` | Per-identity ArcadeDB database + HMAC-derived credential | Already the server-enforced isolation primitive D-11 items 1-2 need to call directly |
| `cmd/arcadedb-mcp` OAuth `sub`-keyed tenant resolution | `cmd/arcadedb-mcp/identity.go:15-36`, `tenant.go` | MCP-level per-identity credential selection | Already reads `req.Extra.TokenInfo.UserID`, never a client-supplied header — exactly what D-11 item 3 needs to prove |
| `go.uber.org/goleak` | already imported across `internal/agent`, `internal/swarm`, `internal/arcadedb` test files | Goroutine-leak detection for the D-14 concurrent-runner test | `internal/agent/main_test.go` already runs `goleak.VerifyTestMain`; a new concurrent test in that package inherits it for free |
| Authula email-password + TOTP plugin | `internal/webauth/authula.go:42-43,150,196-210` (`github.com/Authula/authula/plugins/totp`) | Real login for the D-16 harness | Already in `go.mod`; no new auth library needed — but see Pitfall on forced first-login TOTP enrollment |

### Alternatives Considered

| Instead of | Could use | Tradeoff |
|---|---|---|
| Extending `two_identity_e2e_test.go`'s in-process `httptest` harness for the closing run | A brand-new script/binary modeled on `scripts/agui_smoke.sh` | The existing harness never spawns a real `aura serve` process or a real Authula login — extending it in place would require adding both, at which point it is no longer the same kind of test as its siblings in that file (in-process store calls). `agui_smoke.sh`'s shape (build → background-start → poll `/healthz` → curl → teardown) already solves process lifecycle and cookie-based auth; D-16 is closer to "port this pattern to two identities" than "extend the harness file" |
| A new boot-time Docker probe inside `Config.Validate()` | A separate boot step in `chat_boot.go`/`serve.go` that runs after `cfg.Validate()` succeeds | `ValidateProfile`'s own doc comment states it "performs no other I/O" (`config_validate.go:79-85`) beyond two named exceptions; embedding an `ImageInspect`/`ImagePull` call there breaks that documented contract for every other caller of `ValidateProfile` (including `serve_settings.go:201`'s live-reload path, which must stay side-effect-free) |

**Installation:** N/A — no new packages.

## Package Legitimacy Audit

**Not applicable.** This phase adds no new external package to any ecosystem. Every
component named above already resolves in `go.mod` / the existing `compose.yaml` service set.

**Packages removed due to `[SLOP]` verdict:** none.
**Packages flagged as suspicious `[SUS]`:** none.

## Architecture Patterns

### System Architecture Diagram

```
                         ┌─ operator ─┐
                         │ (identity  │
                         │  .create)  │
                         └─────┬──────┘
                               │ 1. StartSession → Provision
                               ▼
                  ┌────────────────────────────┐
                  │ onboardingService.Provision │  internal/agui/onboarding_provision.go
                  │  (cross-store saga, one     │
                  │   writer, per-leg comp.)    │
                  └──────────────┬──────────────┘
        ┌───────────┬────────────┼───────────┬──────────────┬───────────┐
        ▼           ▼            ▼           ▼              ▼           ▼
   Authula      aura.* tx    Recovery    ArcadeDB tenant  Garage      Telegram
   (Leg B)      (Leg A:      challenge   database+cred    bucket+key  mint
                identity+                (existing leg)   (existing   (Leg C,
                grants)                                   leg)        required)
        │                                       │              │
        │            [MISSING TODAY — D-09]     │              │
        │            per-identity sandbox box ──┤ eager leg to add,
        │            (SandboxRouter.Resolve)    │ mirrors the memory/
        │                                       │ Garage/fs legs above
        ▼                                       ▼
  ┌─────────────────────────────────────────────────────────┐
  │                  identity B logs in                      │
  │   (Authula CSRF + email/password + forced first-login    │
  │    password change + mandatory TOTP enrollment)           │
  └──────────────────────────┬────────────────────────────────┘
                              ▼
              POST /agent/run  (AG-UI gateway, SSE)
                              ▼
        ┌──────────────────────────────────────────────┐
        │ runner.buildAgent → FRESH LlmAgent + fresh    │  per-turn: safe by
        │ Budget, seeded with identityctx-scoped history│  construction
        └───────────────────┬────────────────────────────┘
                              │  reads/writes through:
        ┌─────────────────────┼─────────────────────────────┐
        ▼                     ▼                              ▼
  tools.Registry        gateway.Gateway ledger          SteerInbox
  (process-wide,        (ReservationKey keyed on         (Postgres,
   shared across         ConversationID — NOT identity)   Drain(conv) —
   identities)                                            NOT identity)
                              │
                              ▼
                    sidecarPath(runDir, sessionID, spillID)
                    session-keyed (=conversation UUID),
                    NOT identity-keyed
```

The diagram's bottom half is exactly D-15's four surfaces, drawn as data flow rather than a
file list: every one of them is reachable from a live turn, and every one of them is scoped by
**conversation UUID uniqueness**, never by an explicit identity check — confirmed by reading
`sidecarPath` (`internal/agent/tools/result.go:198-206`), `ReservationKey`
(`internal/gateway/approve.go:213-227`) and `PostgresStore.Drain`
(`internal/steer/pg_store.go:214-243`) this session. `Budget` is the one item in D-15's list
that, on inspection, is **not** a cross-identity risk by construction: `runner.buildAgent`
calls `agent.NewBudget(...)` fresh on every turn (`internal/runner/runner.go:381-387`), so a
`*Budget` is shared only *within* one turn's own swarm sub-tree (parent + children), by
deliberate design (D-10 in `internal/agent/budget.go:1-13`) — never across identities or even
across two turns of the same identity. `tools.Registry`, the gateway `*Gateway`, the LLM
`Breaker` and the `ReasoningClassifier` genuinely are process-wide singletons, confirmed by
`runner.go`'s own comments ("shared, anchors built once", "shared process-lifetime breaker",
`internal/runner/runner.go:407-408`).

### Recommended Project Structure

No new top-level packages are implied by anything in CONTEXT.md. New files land inside
existing packages:

```
internal/agui/
├── onboarding_provision.go            # saga orchestration (existing, near 600-LOC — touch carefully)
├── onboarding_provision_resources.go  # ADD: SandboxProvisioner leg (D-09), alongside the
│                                      #   existing MemoryProvisioner/ObjectStoreProvisioner/
│                                      #   FilesystemProvisioner interfaces
├── deprovision.go                     # existing SandboxPurger — the new leg's compensation
│                                      #   mirror is ALREADY wired here
cmd/aura/
├── serve.go                           # 598 LOC measured this session — a new boot preflight
│                                      #   needs its own file (e.g. serve_sandbox_preflight.go)
├── two_identity_e2e_test.go           # ADD: arcadedb_integration 5th tag + memory plane subtests
├── two_identity_e2e_harness_test.go   # ADD: arcadedb tenant helpers alongside the existing
│                                      #   musrProvision* helpers
├── identity.go (new, D-07)            # `aura identity create` CLI verb over StartSession+Provision
scripts/
├── coverage_docker.sh                 # source lib extracted (D-12)
├── lib/disposable_stack.sh (new)      # extracted disposable-Postgres pattern
├── musr_e2e.sh (new, D-13)            # bring-up + tagged test + teardown, sourcing the lib
├── agui_smoke.sh                      # NOT modified — the process-lifecycle/auth MODEL for
│                                      #   the new D-16 harness, not itself extended
docs/runbooks/
├── musr-rollout.md                    # updated Acceptance section (D-05)
```

### Pattern 1: Eager, journaled, idempotent, symmetric-compensation saga legs

**What:** Every resource leg in the provisioning saga (`ProvisionMemory`,
`ProvisionObjectStore`, `ProvisionIdentityDirs`) is journaled through `run.step`, idempotent,
and paired with a compensation call that best-effort-reverses it on a later leg's failure.

**When to use:** Any new eager resource acquisition inside `onboarding_provision.go`'s flow —
this is the shape D-09's sandbox leg must follow.

**Example (existing code, not proposed):**
```go
// Source: internal/agui/onboarding_provision_resources.go:72-81
if s.memory != nil {
    if err := run.step(ctx, sagaStepMemory, func(ctx context.Context) error {
        return s.memory.ProvisionMemory(ctx, identityID)
    }); err != nil {
        if derr := s.memory.PurgeMemory(context.WithoutCancel(ctx), identityID); derr != nil {
            slog.Error("onboarding: COMP memory after provision failure failed", "step", "compensate")
        }
        return compResources, provisionFail("memory provision", err)
    }
}
```
`SandboxRouter.Resolve(ctx, spec)` (create) and `SandboxRouter.Destroy(ctx, identityID)`
(compensate) are the existing pair to slot into this same shape
(`internal/sandbox/usersandbox/router.go:79-99,152-167`); `sandboxPurgeAdapter` in
`cmd/aura/serve_provisioning.go:296-313` already wraps `Destroy` for the deprovision side, so a
symmetric `SandboxProvisioner` adapter wrapping `Resolve` is the missing half, not a new
concept.

### Pattern 2: Server-enforced tenancy over row filters

**What:** `internal/arcadedb/tenant.go` derives one database name and one HMAC-derived
credential per identity (`DatabaseFor`, `TenantCredentials.PasswordFor`,
`tenant.go:36-116`). ArcadeDB's server refuses cross-database access with a
`SecurityException` — there is no WHERE clause to forget.

**When to use:** Confirmed applicable, not proposed: this is the existing mechanism D-11 item
2 tests directly.

### Pattern 3: OAuth `sub`-scoped tenant selection at the MCP boundary

**What:** `cmd/arcadedb-mcp/identity.go:15-24`'s `identityFromToken` reads
`req.Extra.TokenInfo.UserID` — the verified bearer token's subject — never a header the caller
supplies. `internal/agent/mcptools/bridge_identity.go:26-43`'s `IdentityBindingMiddleware`
additionally refuses to let one identity's `identityctx` drive a session minted for a different
OAuth subject (`errRemoteIdentityMismatch`).

**When to use:** This is the exact mechanism D-11 items 1 and 3 must exercise; a test needs a
real (or realistically shaped) verified token whose `sub` is identity B, not a forged header.

### Anti-Patterns to Avoid

- **Embedding a Docker I/O call inside `Config.ValidateProfile`:** its own doc comment says it
  "performs no other I/O" beyond two named exceptions (`config_validate.go:79-85`); every other
  gate in that function is a pure comparison. D-03's preflight needs a separate boot step.
- **`docker compose up -d arcadedb-mcp` (or `make memory-up`) inside a job that also runs a
  tagged Go test against the same Postgres:** `arcadedb-mcp`'s `depends_on: aura:
  condition: service_started` (`compose.yaml:695-699`) starts the full daemon as a side effect,
  which raced a `internal/cron` test on CI #1809 (`Makefile:262-270`, a measured incident, not
  a hypothesis).
- **Reusing `two_identity_e2e_harness_test.go`'s raw-SQL `musrProvisionIdentity` for
  Success-Criterion-1/E2E-02 evidence:** it bypasses the actual saga entirely (no Authula user,
  no Telegram leg, no ArcadeDB/Garage/sandbox legs) and therefore proves nothing about the
  *documented provisioning path* the roadmap requires.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| Disposable Postgres for a live tagged test | A new bootstrap script | Extract `scripts/coverage_docker.sh:47-130`'s dual-mode pattern (local: throwaway container on port 5433; CI: throwaway database inside the existing shared container) into `scripts/lib/disposable_stack.sh` (D-12) | Already handles the exact `AURA_COVERAGE_DB != "aura"` guard that a real data-loss incident on 2026-07-10 forced into existence — reproducing it independently risks reintroducing that bug |
| A process to drive `aura serve` and authenticate against it from a script | A bespoke curl/session flow | Port `scripts/agui_smoke.sh:129-330`'s build→background-start→poll `/healthz`→Authula CSRF login→cookie-jar pattern | It already handles Windows-vs-POSIX process cleanup, port collision, and the Authula CSRF/cookie/TOTP-verify dance; re-deriving it is exactly the "custom solution in a domain with a working one" CLAUDE.md forbids |
| Per-identity ArcadeDB credential derivation for a cross-deny test | A second HMAC scheme or a mock | `arcadedb.NewTenantCredentials()` + `DatabaseFor`/`PasswordFor` (`internal/arcadedb/tenant.go:85-116`), called directly from the test | This *is* the production mechanism; testing a stand-in would prove nothing about it |
| Sandbox provisioning at saga time | A parallel `usersandbox` construction path | `SandboxRouter.Resolve`/`Destroy` (already used by every live tool call and by the existing deprovision leg) | Two paths to create the same container class is the exact duplication CLAUDE.md's "REUSABLE CODE" rule forbids |

**Key insight:** almost everything this phase needs to prove already has a proven primitive
sitting one level away from where the roadmap's prose implies new work is needed. The actual
gap is narrower and more specific than "build two-identity isolation" — it is "wire five
already-correct primitives into one saga leg, one CI job, and one new closing-run harness."

## Common Pitfalls

### Pitfall 1: `arcadedb-mcp`'s `depends_on: aura` silently starts a second Postgres writer

**What goes wrong:** Any `docker compose up` invocation that names `arcadedb-mcp` (directly, or
via `make memory-up`) also starts the full `aura` daemon, because `compose.yaml:695-699`
declares `depends_on: aura: condition: service_started`. If a Go test process is concurrently
writing to the same Postgres database (as `musr-e2e`'s tagged tests do), the daemon's own
background work (the Makefile names the cron notification sweep specifically) races the test.

**Why it happens:** `arcadedb-mcp` mounts as "that daemon's memory server" per its own
compose comment, so the dependency is intentional for the case where you actually want the
daemon — it is only wrong when a job wants the *sidecar* without the *daemon*.

**How to avoid:** Two proven alternatives already exist in this repo: (a) `memory-up-core`
(`Makefile:275-279`) brings up `arcadedb` + the embed sidecar without the MCP/daemon; (b) the
`web-e2e` CI job (`.github/workflows/ci.yml:1683-1710`) starts `arcadedb-mcp` with a raw
`docker run --network host ...` against the *published* image, entirely bypassing compose's
`depends_on` graph. D-13's `make musr-e2e` (which needs `arcadedb` up for the raw-credential
test, D-11 item 2) does not need this care; whatever brings up `arcadedb-mcp` for D-11 item 3
does.

**Warning signs:** A CI job whose "bring up the stack" step name promises fewer services than
`docker compose ps` actually shows running afterward; intermittent off-by-one counts in any
concurrent Postgres-writing assertion.

### Pitfall 2: The forced first-login flow (password change + mandatory TOTP enrollment) has no existing end-to-end driver

**What goes wrong:** `onboarding_provision.go:204-210`'s `EnforceFirstLogin` marks every newly
provisioned identity so its first login **forces** a password change and **requires** TOTP
enrollment — this is stronger than `scripts/agui_smoke.sh`'s existing TOTP handling, which only
*verifies* an already-enrolled code (`agui_smoke.sh:267-300`). No script or test in this
codebase currently drives a freshly provisioned identity through first login end-to-end.

**Why it happens:** The bootstrap-seeded `local` identity (which every other live smoke test
authenticates as) is created outside this saga and never carries the first-login flag.

**How to avoid:** Read `internal/webauth/authula.go`'s TOTP plugin wiring
(`github.com/Authula/authula/plugins/totp`) and its enrollment API shape *before* writing the
D-16 harness's login step for identity B; do not assume the existing verify-only flow covers
it.

**Warning signs:** A harness that logs in successfully against the bootstrap `local` identity
but 4xxs (or silently mis-handles a `totp_redirect`-shaped response with no enrollment secret)
the first time it logs in as a freshly provisioned identity.

### Pitfall 3: A missing/unbuilt sandbox image degrades silently, not loudly, today

**What goes wrong:** `DockerBackend.ensureImage` (`docker_backend_lifecycle.go:228-241`) only
pulls on `ImageInspect` miss, at the *first* tool call — there is no boot-time check. A strict
deployment with `AURA_SANDBOX_IMAGE` pointed at a name no registry serves boots healthy and then
denies **every** shell/file tool with no diagnostic until an operator actually tries one.

**Why it happens:** This is intentional lazy provisioning for the happy path (avoid pulling an
image nobody will use), but it has no counterpart boot-time check for the strict-profile case
where the box is not optional.

**How to avoid:** D-03's preflight closes this, but must live outside `Config.Validate()`'s
I/O-free contract (see Anti-Pattern above).

**Warning signs:** `/readyz` and `/healthz` both green, but the sandbox readiness probe
(`cmd/aura/serve_sandbox_readiness.go:31-58`, which already exists and calls
`router.CheckRuntime` — an `ImageInspect`, not a pull) is the *only* thing that would catch
this, and only if an operator/monitor is polling it.

### Pitfall 4: Pinned-version installs never get a default `AURA_SANDBOX_IMAGE`

**What goes wrong:** `scripts/install.sh:511-538`'s `ensure_edge_channel_env` only sets
`AURA_SANDBOX_IMAGE`/`AURA_SANDBOX_EGRESS_IMAGE` inside a `case "$(env_value AURA_IMAGE)" in
*:edge)` branch. A pinned install (`AURA_INSTALL_REF=vX.Y.Z`, `IMAGE_TAG` = the version, not
`edge`, per `install.sh:90-100`) never enters that branch, so `AURA_SANDBOX_IMAGE` stays unset
and the Go default `aura-sandbox:latest` (`internal/config/config_sandbox.go:25`) — a name no
registry serves — is what a strict pinned deployment will try to pull.

**Why it happens:** The edge-channel default was added specifically to fix the moving-tag case
(`install.sh:525-533`'s own comment: "no appliance has ever built and no registry serves"
`aura-sandbox:latest`); pinned installs were not in scope for that fix.

**How to avoid:** D-04's "install.sh builds (or pulls) the sandbox image as an install step"
must cover the pinned-install path specifically, since the edge-channel default already
half-solves this for the master/edge-tracking install path.

**Warning signs:** `AURA_IMAGE` is a semver tag (not `:edge`) and `AURA_SANDBOX_IMAGE` is unset
in the resulting `.env`.

## Code Examples

### The existing eager-leg + compensation shape (model for D-09's sandbox leg)

```go
// Source: internal/agui/onboarding_provision_resources.go:44-106 (read this session, verbatim)
func (s *onboardingService) provisionResourceLegs(ctx context.Context, run *sagaRun, identityID string) (compResources func(), err error) {
	compResources = func() {
		cctx := context.WithoutCancel(ctx)
		if s.filesystem != nil {
			if derr := s.filesystem.DeprovisionIdentityDirs(cctx, identityID); derr != nil {
				slog.Error("onboarding: COMP filesystem (remove identity dirs) failed", "step", "compensate")
			}
		}
		// ... objectStore, memory follow the same shape, reverse order
	}
	if s.memory != nil {
		if err := run.step(ctx, sagaStepMemory, func(ctx context.Context) error {
			return s.memory.ProvisionMemory(ctx, identityID)
		}); err != nil {
			// ... compensate + return
		}
	}
	// ... objectStore, filesystem follow
	return compResources, nil
}
```

### The existing SandboxRouter create/destroy pair (already proven, not new)

```go
// Source: internal/sandbox/usersandbox/router.go:79-99 (Resolve is called via Route)
func (r *SandboxRouter) Route(ctx context.Context) (BoxHandle, error) {
	if r == nil || r.backend == nil {
		return BoxHandle{}, errBackendUnavailable
	}
	id := r.identityID(ctx)
	h, err := r.backend.Resolve(ctx, r.specFor(id))
	// ...
}

// Source: internal/sandbox/usersandbox/router.go:152-167
func (r *SandboxRouter) Destroy(ctx context.Context, identityID string) error {
	if r == nil || r.backend == nil {
		return errBackendUnavailable
	}
	// ... deletes the cached handle and destroys the box
}
```

### The existing concurrent-goroutine + goleak test shape (model for D-14)

```go
// Source: internal/arcadedb/concurrent_fact_write_test.go:1-35 (build tag arcadedb_integration)
//go:build arcadedb_integration
func TestConcurrentWorkerFactWriteSameContentMergesIntoOneFact(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := integrationClient(t)
	// N real goroutines against the live sidecar, not a sequential for loop —
	// "SWARM-07/SC#5's real hazard: N workers writing into ONE identity's graph at
	// the same time. A sequential `for` loop ... passes green while leaving the
	// actual race ... completely unexercised."
}
```

### The existing live-daemon process-lifecycle shape (model for D-16, not itself extended)

```bash
# Source: scripts/agui_smoke.sh:129-190 (build, background-start, poll, no fixed sleep)
go build -o "${BIN}" ./cmd/aura
"${BIN}" serve >"${SERVE_LOG}" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 60); do
  if ! kill -0 "${SERVE_PID}" 2>/dev/null; then
    echo "FAIL: aura serve exited during boot"; cat "${SERVE_LOG}" >&2; exit 1
  fi
  if curl -fsS -o "${NULL_OUT}" "${BASE}/healthz" 2>/dev/null; then READY=1; break; fi
  sleep 0.5
done
```

## State of the Art

| Old approach | Current approach | When changed | Impact |
|---|---|---|---|
| `docs/runbooks/musr-rollout.md` described the flag as gating "a scoped vs. unscoped documents-retrieval path" needing an owner-edge backfill and an `aura documents backfill` command | The runbook itself now states, in a callout, that this was wrong: "Both are gone: the graph the documents plane lived in was deleted, ownership is a column now, and there is nothing to backfill. The command ... never existed at all" (`musr-rollout.md:7-13`) | Documented at some point before this research, exact commit not identified | The flag is confirmed, by the runbook's own correction, to be a pure provisioning gate today — reinforces the config.go comment cited in the Summary |
| ArcadeDB pinned at stable `26.8.1` | Pinned at `26.9.1-SNAPSHOT` by digest | `compose.yaml:588-602`'s own comment: a pre-26.9.1 grouped `vector.fuse` search "read the HNSW graph ONLY — skipping the in-memory delta buffer," making freshly ingested content invisible until a rebuild | Not directly load-bearing for this phase's memory cross-deny test (`memory_vector.go` recall issues ungrouped searches, unaffected per the same comment), but worth knowing before touching any ArcadeDB query in this phase |
| Graph-DB spike material (`spike-findings-Aura` skill) says "STAY with Neo4j" | The shipped codebase uses ArcadeDB exclusively for long-term memory (`internal/arcadedb`, `cmd/arcadedb-mcp`, `compose.yaml`) | Between the spike sessions (dated 2026-07) and now | The spike reference material's Neo4j recommendation is **stale** for this phase — do not let it steer any design choice; CLAUDE.md's own persistence section names ArcadeDB as current truth |

**Deprecated/outdated:** The `spike-findings-Aura` skill's `multiuser-per-identity-isolation.md`
reference (dated to Session-21, 2026-06-29) describes the memory plane as "one Neo4j graph,
multi-tenant-able" — this is superseded; the live mechanism is ArcadeDB's one-database-per-identity
model in `internal/arcadedb/tenant.go`, not a Neo4j `:User`-ownership edge. The spike's general
*shape* (class (b): shared sidecar + mandatory per-identity scope key) still matches ArcadeDB's
actual mechanism closely enough to be directionally useful, but any literal Neo4j/Cypher detail
in that reference file is `[ASSUMED: stale]` against this codebase.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `TestProvisionLoginIsolatedRun` and `TestTwoIdentityCrossDeny` never call `onboardingService.Provision` and therefore never exercise the Authula/Telegram legs — this was read directly in both test files this session and is `[VERIFIED: cmd/aura/two_identity_e2e_test.go:342-383, cmd/aura/two_identity_e2e_harness_test.go:93-114]`, not assumed. Listed here only because the *implication* ("promoting the harness to a live run is closer to new work than a tag addition") is an inference from that fact, not itself a directly-read claim. | Summary, Standard Stack | If the planner instead extends the existing harness file believing it already drives a real daemon, the resulting plan under-scopes D-16 |
| A2 | The exact API shape of Authula's TOTP *enrollment* endpoint (as opposed to *verify*, which `agui_smoke.sh` already exercises) was not read this session — only its existence and the fact that `EnforceFirstLogin` requires it were confirmed via `onboarding_provision.go`'s comment and `internal/webauth/authula.go`'s plugin import. | Pitfall 2 | The D-16 harness's identity-login step could take longer to build than estimated if the enrollment flow has an undocumented wrinkle (e.g., a QR/secret round-trip) |
| A3 | Whether `docker compose up -d garage arcadedb` (D-13's literal command, no `arcadedb-mcp`) avoids the CI #1809 daemon-start hazard was inferred from `compose.yaml`'s dependency graph (only `arcadedb-mcp` depends on `aura`; `arcadedb` itself does not, confirmed by the separate `web-e2e` job bringing up `arcadedb` alone via `docker compose up -d arcadedb` at `.github/workflows/ci.yml:1689` with no daemon side effect implied by its surrounding comments) rather than by actually running it in this session. | Pitfall 1, D-13 support | If `arcadedb` transitively depends on something that pulls in `aura` through a path not read this session, the hazard could still occur for D-13's exact command |
| A4 | The next free migration slot is `0120` (successor to `0119_drop_orphan_content_parts`, the `tail -1` result at research time). | Project Constraints | This is a point-in-time measurement and CLAUDE.md is explicit that it must be re-measured at landing time, not trusted from this document — flagged so the planner does not hardcode it |

## Open Questions

> **Status (2026-09-07, at plan time):** Q1 and Q3 are editorial — their answers were already
> fully determined by the plans that consume them, so each carries a disposition below, appended
> before this phase's plans were finalised. Q2 is the one genuine unknown: nothing in this
> repository has ever driven a freshly provisioned identity through first login, so it is
> *measured* rather than dispositioned. Plan `01-07` Task 1 reads the installed plugin at wave 1
> — ahead of every plan that consumes the answer — and Task 2 appends its verdict here and marks
> this heading. Until then Q2 is open, and it says so.

1. **Does any Success Criterion in this phase actually require a new Postgres migration?**
   - What we know: none of D-01 through D-18 describes a schema change; the fifth build tag
     (D-10) and the script extraction (D-12) are not schema work.
   - What's unclear: whether the eager sandbox leg (D-09) needs any new persisted state (e.g.,
     a `sandbox_provisioned_at` column) or whether the existing `run.step` journal
     (`aura.provisioning_saga`, referenced in `deprovision.go:16`) already has a generic enough
     shape to record it with no new column.
   - Recommendation: the planner should grep `aura.provisioning_saga`'s schema (migration that
     created it) before assuming a new migration is or is not needed, rather than defaulting to
     either answer.
   - **RESOLVED (2026-09-07, at plan time — editorial; no measurement was owed):** No new
     migration is planned, by disposition rather than by pre-empting the grep. None of D-01
     through D-18 describes a schema change; `sagaStepSandbox` already exists and the `run.step`
     journal records a generic `(saga_id, step)` pair, so the eager sandbox leg (D-09) persists
     nothing new. This is enforced rather than believed: plan `01-01` Task 1 re-greps both facts
     before the leg is written, and halts to `ls internal/db/migrations/ | tail -1` if the grep
     contradicts them — the directory is the source of the next slot, never this document
     (CLAUDE.md migration-numbering rule). A4's `0120` therefore stays a point-in-time reading,
     not a number any plan hardcodes.

2. **What exact TOTP enrollment wire contract does Authula's plugin expose, and can it be
   automated headlessly (no human scanning a QR code) for a CI-run closing harness?**
   - What we know: `EnforceFirstLogin` requires it; `agui_smoke.sh` handles verify-only, not
     enrollment; the plugin package is `github.com/Authula/authula/plugins/totp`.
   - What's unclear: whether the enrollment endpoint returns a raw TOTP secret (which a
     headless harness could feed directly into an RFC 6238 code generator) or only a QR image.
   - Recommendation: read `github.com/Authula/authula/plugins/totp`'s exported API directly
     (it is a real dependency in `go.mod`, not vendored-and-modified) before the planner commits
     to D-16's harness login-step design.

3. **Is the `arcadedb_integration` build tag's existing test suite (34 files) already exercising
   enough of `internal/arcadedb`'s per-identity path that D-11 item 2's raw-credential
   `SecurityException` assertion is close to a copy-paste from an existing test, or genuinely
   new?**
   - What we know: 34 files carry the tag; `concurrent_fact_write_test.go` and others exist.
   - What's unclear: whether any of them already construct two identities' credentials and
     assert cross-database denial (as opposed to testing one identity's database in isolation).
   - Recommendation: the planner/executor should grep those 34 files for a second identity/
     database construction before assuming D-11 item 2 is greenfield.
   - **RESOLVED (2026-09-07, at plan time — editorial; no measurement was owed):** Neither
     answer is asserted here; the question is handed to the grep that settles it inside the plan
     that consumes it. Plan `01-03` carries an `<inventory_before_invention>` block whose four
     greps over the `arcadedb_integration` files run before a line of D-11 item 2 is written and
     decide how much of it is new. Whatever they find, anything already asserted is reused rather
     than duplicated (CLAUDE.md REUSABLE CODE, INVENTORY BEFORE INVENTION), and the grep results
     are recorded in that plan's SUMMARY so the answer becomes readable evidence rather than a
     claim in this table.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|---|---|---|---|---|
| Docker Engine | Sandbox boot preflight (D-03), ArcadeDB/Garage bring-up (D-13) | Not probed this session (research is source-only; no live stack was started) | — | N/A — required, no fallback |
| ArcadeDB `26.9.1-SNAPSHOT` (digest-pinned) | D-10/D-11's memory plane | Compose service defined (`compose.yaml:587-644`); publishes `2480`/`7687` to loopback already, no CI override needed for that port | pinned by `@sha256:f6e9948e...` | N/A |
| Garage Admin API `:3903` on loopback | D-11 item 3 wiring, existing 5-plane gate | Already fixed in commit `a3536af5d` (`.github/compose.ci-musr.yaml`) | — | N/A |
| `github.com/Authula/authula/plugins/totp` | D-16's forced-first-login harness step | In `go.mod` (imported by `internal/webauth/authula.go:42-43`) | not version-checked this session | N/A |
| `arcadedb-mcp` sidecar image | D-11 item 3 | Buildable from `docker/arcadedb-mcp/Dockerfile`; also published (`AURA_ARCADEDB_MCP_IMAGE`, referenced in `web-e2e`'s pinned digest, `ci.yml:1631`) | — | Local dev can `docker compose build arcadedb-mcp` |

**Missing dependencies with no fallback:** a running Docker Engine — this entire phase is
Docker-dependent by nature (sandbox boxes, ArcadeDB, Garage) and there is no lesser-isolation
fallback consistent with the phase's goal.

**Missing dependencies with fallback:** none identified beyond the image-build/pull duality
already documented in install.sh.

## Validation Architecture

### Test Framework

| Property | Value |
|---|---|
| Framework | Go's standard `testing` package + build tags; no external test framework |
| Config file | none — tag-gated files (`//go:build ...`) are the "config" |
| Quick run command | `go vet -tags 'db_integration garage_integration authula_integration musr_e2e arcadedb_integration' ./cmd/aura/` (compile-only floor, mirrors the existing `ci.yml:465` step, D-10 extends its tag set) |
| Full suite command | `make musr-e2e` (D-13, new target — brings up disposable Postgres + `garage` + `arcadedb`, seeds, runs the tagged tests, tears down) |

### Build tags / tiers in play and which gate owns each

| Tag | What it gates | Owning gate (this phase) |
|---|---|---|
| `db_integration` | Postgres-backed assertions (conversations, approvals, documents/RLS) | `musr-e2e` CI job (existing) |
| `garage_integration` | Garage object-store cross-deny | `musr-e2e` CI job (existing) |
| `authula_integration` | Real Authula-configured stack precondition | `musr-e2e` CI job (existing) |
| `musr_e2e` | The two-identity acceptance file itself | `musr-e2e` CI job (existing) |
| `arcadedb_integration` | The new 5th tag (D-10) — ArcadeDB memory cross-deny (D-11 items 1-3) | `musr-e2e` CI job, extended (new work this phase) |
| unit (no tag) | Config gate logic (`TestGateMultiUserRequiresStrictProfile`, `TestMUSRIsolationDefaultOff`), CLI flag parsing for `aura identity create` | `make quality` (existing) |
| `-race` + `goleak` (no separate tag, applies within `musr_e2e`) | D-14's concurrent-runner test | `musr-e2e` CI job, `-race` flag already present in the job's `go test` invocation (`ci.yml:543`) |

### Phase Requirements → Test Map

| Req ID | Behaviour | Test Type | Automated Command | File Exists? |
|---|---|---|---|---|
| ISO-01 | Strict profile + `AURA_MUSR_ISOLATION=true` boots; non-strict + flag on is Fatal | unit | `go test ./internal/config/ -run TestGateMultiUserRequiresStrictProfile` | ✅ (`internal/config/config_validate_test.go:233-247`) |
| ISO-01 | Sandbox-image boot preflight refuses with named remedy | unit | new test, name TBD by planner | ❌ Wave 0 (D-03 is new production code) |
| ISO-02 | 5 existing planes cross-deny | integration | `go test -race -tags 'db_integration garage_integration authula_integration musr_e2e' -run TestTwoIdentityCrossDeny ./cmd/aura/` | ✅ (existing, passed live 2026-09-07 per roadmap) |
| ISO-02 | Memory plane cross-deny, 3 surfaces (D-11) | integration | `go test -race -tags '... arcadedb_integration' -run TestTwoIdentityCrossDeny ./cmd/aura/` (extended) | ❌ Wave 0 — new subtests inside the existing `t.Run` tree |
| ISO-02a | Gate runs unattended, clean checkout, CI | integration/CI-shape | `make musr-e2e` | ❌ Wave 0 (new Makefile target + new script) |
| ISO-05 | Concurrent-runner white-box, 4 surfaces disjoint | integration (in-process, `-race`+`goleak`) | new test in `internal/agent/` or `internal/runner/`, name TBD | ❌ Wave 0 |
| E2E-01 | Two concurrent live `/agent/run` conversations, scored | live/manual-scored | new committed harness (D-16), invoked once at phase close | ❌ Wave 0 (this is the "Closes on" artifact itself) |
| E2E-02 | Second identity onboarded end to end, no manual step | live | `aura identity create` (D-07) exercised inside `make musr-e2e`'s seed step or the D-16 harness | ❌ Wave 0 (new CLI verb) |

### Sampling Rate

- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/<touched>/` +
  `go test -race ./internal/<touched>/` per CLAUDE.md's mandatory post-edit validation.
- **Per wave merge:** the relevant tagged tier — `go vet -tags '...'` compile floor at minimum,
  the full `make musr-e2e` once ArcadeDB/Garage bring-up is wired.
- **Phase gate:** `make musr-e2e` green in CI, followed by the D-16 live closing run scored
  against the written rubric (D-18 — recorded evidence, not a blocking gate itself; the
  machine-checkable half of the same run is what blocks).

### What a sample would miss

- **`-race` + `goleak` on the D-14 concurrent-runner test samples ONE deliberate collision
  shape** (same tool, same arguments, overlapping in time, per D-14). It would not catch a
  cross-identity leak that only manifests under a *different* collision shape (e.g., different
  tools racing, or a three-way race) — D-15's list is enumerated as a floor, not a ceiling, and
  the CONTEXT explicitly forbids silently shortening it, but nothing in D-14 forbids a
  collision shape that is *narrower* than what a real production race could someday produce.
  This is consistent with the CONTEXT's own framing (D-14: "Not a randomized-interleaving
  harness" — that gap is explicitly deferred, not hidden).
- **The `musr-e2e` CI job's `-p 1` serialization** (`ci.yml:543`, "serializes the shared
  Postgres writes") means the tagged test file itself is single-threaded at the process level
  even though it asserts *application-level* concurrent-identity correctness — the deliberate
  concurrency this phase needs to prove (D-14) must be **goroutines within one `go test`
  process**, not separate `go test` invocations, or `-p 1` would silently serialize away the
  very race condition being tested.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---|---|---|
| V2 Authentication | Yes | Authula email-password + mandatory TOTP on first login (`EnforceFirstLogin`) — existing, not built this phase, but exercised for the first time end-to-end by D-16 |
| V3 Session Management | Yes | Authula CSRF-cookie session (`/api/auth/config`, `X-AUTHULA-CSRF-TOKEN`) — existing, read via `agui_smoke.sh`'s working login flow |
| V4 Access Control | Yes | `RequireCapability(identity.create)` gates provisioning at the route mount (`onboarding_api.go`'s own comment: "The capability gate ... is on the parent-mux mount, so an operator without the cap is 403 before this runs"); `validateNoEscalation` refuses granting `*` or capabilities the creator lacks |
| V5 Input Validation | Yes | `validateOnboardingProvision` (`onboarding_api.go:331-356`) bounds every field length and requires `LinkTelegram` |
| V6 Cryptography | Yes | `TenantCredentials.PasswordFor` — HMAC-SHA256 over the database name with a server-side secret (`internal/arcadedb/tenant.go:109-116`) — never hand-rolled by this phase, only *called* by the new memory cross-deny test |
| V11 Business Logic / Resource control | Partial | `Budget`'s per-turn step/wallclock/dedup caps (`internal/agent/budget.go:1-13`) are pre-existing and out of scope for this phase's *tests* (ISO-06 is Phase 4), but D-14's concurrent-runner test does incidentally confirm Budget does not leak across identities |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation (existing, confirmed) |
|---|---|---|
| Cross-tenant SQL row read via a forgotten `WHERE identity_id = $1` | Information Disclosure | Postgres RLS backstop, migration 0032 (`assertRLSCount` in the harness proves the raw-connection case fails closed) |
| Cross-tenant ArcadeDB read via a shared credential | Information Disclosure / Elevation of Privilege | One database + one HMAC-derived credential per identity — server refuses, not application-filtered (`tenant.go:14-28`'s own header comment explains why the WHERE-clause alternative was rejected) |
| MCP session hijack (identity A's turn reusing identity B's OAuth-bound session) | Spoofing | `IdentityBindingMiddleware` refuses when `identityctx.IdentityID(ctx) != owner` (`bridge_identity.go:26-43`) |
| Approval/steer-message cross-conversation replay | Tampering | `ReservationKey`/`SteerInbox.Drain` are keyed on conversation UUID, which is a UUIDv7 (effectively unguessable) — but this is **uniqueness-by-construction, not an identity check**; D-15 explicitly names this as a surface to *prove* disjoint, not one already proven disjoint by an explicit gate |
| Sandbox escape via a Docker-socket mount or host bind | Elevation of Privilege | Out of scope for this phase (ISO-07 is Phase 3); `SandboxSpec`'s host-exposure flags are already "unrepresentable" per the spike reference, but that claim was not re-verified against current source this session — `[ASSUMED: spike-findings-Aura reference, dated 2026-06-29]` |

## Sources

### Primary (HIGH confidence — read directly this session)

- `internal/config/config.go`, `config_validate.go`, `config_runtimeprofile.go`, `config_knobs.go`, `config_sandbox.go`
- `internal/agui/onboarding_provision.go`, `onboarding_provision_resources.go`, `onboarding_session.go`, `onboarding_api.go`, `deprovision.go`
- `internal/sandbox/usersandbox/router.go`, `docker_backend_lifecycle.go`
- `internal/arcadedb/tenant.go`
- `cmd/arcadedb-mcp/identity.go`, `tenant.go`
- `internal/agent/mcptools/bridge_identity.go`
- `internal/agent/llm_agent.go`, `agent.go`, `budget.go`
- `internal/runner/runner.go`
- `internal/agent/tools/result.go`
- `internal/gateway/approve.go`
- `internal/steer/pg_store.go`
- `internal/webauth/authula.go`
- `cmd/aura/two_identity_e2e_test.go`, `two_identity_e2e_harness_test.go`, `serve_sandbox_readiness.go`, `serve_webui_musr.go`, `serve_provisioning.go`, `chat_boot.go`
- `scripts/install.sh`, `scripts/coverage_docker.sh`, `scripts/agui_smoke.sh`
- `compose.yaml`, `.env.example`, `.github/compose.ci-musr.yaml`, `.github/workflows/ci.yml`
- `Makefile`
- `docs/runbooks/musr-rollout.md`
- `internal/agenteval/case.go`
- `scripts/coverage_package_policy.json`
- `.planning/config.json`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`, `.planning/ROADMAP.md`
- `.planning/phases/01-two-identities-live-and-separated/01-CONTEXT.md`, `01-DISCUSSION-LOG.md`
- `.planning/spikes/103-agent-runtime-correctness/README.md`
- `.claude/skills/spike-findings-Aura/SKILL.md`, `references/multiuser-per-identity-isolation.md`

### Secondary (MEDIUM confidence)

- The exact wire shape of Authula's TOTP enrollment endpoint (package import confirmed, endpoint
  contract not read).

### Tertiary (LOW confidence — flagged explicitly as stale or unverified)

- `spike-findings-Aura`'s `multiuser-per-identity-isolation.md`'s literal Neo4j/Cypher details —
  superseded by the shipped ArcadeDB implementation (see State of the Art).
- The claim that sandbox `SandboxSpec` host-exposure flags are "unrepresentable" — asserted in
  the spike reference (dated 2026-06-29) but not re-verified against current
  `internal/sandbox/usersandbox` source this session; this phase does not touch that surface
  (ISO-07 is Phase 3), so it was not chased down.

## Metadata

**Confidence breakdown:**
- Standard stack / architecture: HIGH — every component is existing code read directly this
  session; no external library selection was needed.
- The provisioning-saga completion path (D-09): HIGH — both halves (create via
  `SandboxRouter.Resolve`, compensate via the already-wired `sandboxPurgeAdapter`) were read
  directly.
- The closing-run harness (D-16) and the memory cross-deny test (D-11): MEDIUM — the mechanisms
  they must call are all confirmed present, but no live run was performed this session to
  confirm the exact wire contract of TOTP enrollment or the arcadedb-mcp token-issuance path end
  to end.
- Pitfalls: HIGH for Pitfall 1 (cites a named, measured CI incident) and Pitfall 4 (traced
  through install.sh's actual branching logic); MEDIUM for Pitfall 2 (mechanism confirmed,
  exact API contract not read).

**Research date:** 2026-09-07
**Valid until:** ~2026-10-07 (30 days) for the architectural claims (stable, internal code);
the CI-hazard and install.sh findings should be re-checked if either file changes before
planning begins, since both were mid-flux this session (commit `a3536af5d` landed the same day).
