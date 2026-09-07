# Phase 1: Two Identities, Live and Separated - Context

**Gathered:** 2026-09-07
**Status:** Ready for planning

<domain>
## Phase Boundary

The shipped deployment profile boots with multi-identity provisioning on, a second identity
is provisioned from zero through one documented path, and the two are proven separated in
**data** and in **execution** while both are live and doing real work.

Requirements: ISO-01, ISO-02, ISO-02a, ISO-05, E2E-01, E2E-02.

Not this phase: capability grants and the `*` wildcard (Phase 2), the adversarial suite and
sandbox escape battery (Phase 3), load/chaos/observability evidence (Phase 4), restart /
rollback / restore (Phase 5), deprovisioning symmetry beyond what the new eager sandbox leg
owes its own compensation (ISO-10, later).

</domain>

<decisions>
## Implementation Decisions

### Shipped profile and upgrade path

- **D-01:** The shipped multi-identity default lives in `.env.example` and in
  `scripts/install.sh`'s fresh-`.env` heredoc (around `scripts/install.sh:612`), **not** in
  `compose.yaml`. Both set `AURA_PROFILE=single_user_hardened`, `AURA_MUSR_ISOLATION=true`
  and `AURA_SANDBOX_IMAGE`. `compose.yaml:150` (`${AURA_MUSR_ISOLATION:-false}`) and
  `compose.yaml:313` (`${AURA_PROFILE:-dev}`) keep their upgrade-safe fallbacks unchanged.
  Measured: `.env.example:44` carries `#AURA_PROFILE=dev` commented out, and install.sh's
  heredoc writes none of the three names, so editing `.env.example` alone reaches nobody who
  runs the documented installer. Pattern confirmed against LibreChat, which ships
  `ALLOW_REGISTRATION=true` in `.env.example` while its compose hardcodes nothing.
  — **Reversibility:** reversible — three lines in two files, no migration, no contract.

- **D-02:** `ensure_internal_env_secrets` (`scripts/install.sh:541-554`) is **not** touched.
  It runs on the already-have-a-`.env` upgrade path, so writing the profile there would flip
  existing deployments into the Fatal `gateMultiUserRequiresStrictProfile` boot loop that
  commit `37211f83d` already ruled out.
  — **Reversibility:** reversible.

- **D-03:** `aura serve` gains a boot preflight: under a strict profile with
  `AURA_MUSR_ISOLATION=true`, refuse to start when `AURA_SANDBOX_IMAGE` is neither present nor
  pullable, naming the exact build/pull command. Rationale: a strict profile routes shell and
  file tools into the per-identity sandbox, which otherwise fails closed at the operator's
  first `shell_exec` with no explanation.
  — **Reversibility:** costly — it is a new Fatal boot condition; removing it later means
  accepting a daemon that boots into a silently non-functional tool surface.

- **D-04:** `scripts/install.sh` builds (or pulls) the sandbox image as an install step,
  before `docker compose up`. The preflight then guards only hand-rolled deployments, and the
  documented fresh path never ends at the refusal.
  — **Reversibility:** reversible.

- **D-05:** Existing operators learn multi-identity is available from a single INFO line at
  boot when a non-strict deployment starts, pointing at `docs/runbooks/musr-rollout.md`. The
  runbook is updated with the sandbox-image step and with `make musr-e2e` replacing the raw
  `go test -tags '...'` line in its Acceptance section.
  — **Reversibility:** reversible.

### The documented provisioning path

- **D-06:** Telegram stays a **required** port of the provisioning saga
  (`internal/agui/onboarding_provision.go:160`). A second identity is a person who talks to
  Aura and Telegram is a shipped channel; the docs must state the bot token as a prerequisite
  of hosting a second identity.
  — **Reversibility:** reversible — the check is one boolean expression.

- **D-07:** `aura identity create` is added to `cmd/aura` as a thin front over the identical
  service: it calls `onboardingService.StartSession(ctx, operatorID)` then
  `Provision(operatorID, token, req)` — the exact pair `internal/agui/onboarding_api.go:199`
  uses. No new saga entry point, no relaxed validation, no parallel implementation. This is
  LibreChat's `config/create-user.js` → `registerUser` shape. The cockpit wizard remains
  available; the CLI is what the E2E gate, CI and a headless operator use.
  — **Reversibility:** reversible.

- **D-08:** The acceptance gate provisions with a **fake `TelegramMint`** (in-memory
  `InsertPending`/`DeletePending`/`PendingConsumed` plus a stub bot name) wired at the test
  composition root, so the saga runs unmodified — all legs, real compensation — with no CI
  secret, no live bot and no third-party network inside the gate, and forks can run it. The
  **real** bot is used exactly once, in the phase-closing scored live run, and that transcript
  is the evidence the deep link works.
  — **Reversibility:** reversible.

- **D-09:** The per-identity sandbox box becomes an **eager, idempotent, compensated leg** of
  the provisioning saga alongside the existing memory / Garage / filesystem legs in
  `internal/agui/onboarding_provision_resources.go`. Today it is created lazily at
  `SandboxRouter.Route` → `createBox` (`internal/sandbox/usersandbox/docker_backend_lifecycle.go:58`).
  Making it eager means all four E2E-02 resources land at provisioning time and failures
  surface there rather than at the user's first tool call. Accepted cost: a container per
  identity from minute zero.
  — **Reversibility:** costly — it adds a leg with symmetric compensation to a six-leg saga;
  removing it later means revisiting the compensation ordering and the deprovision mirror in
  `internal/agui/deprovision.go`.

### Proving data separation

- **D-10:** The long-term-memory plane joins the existing gate: `cmd/aura/two_identity_e2e_test.go`
  gains `arcadedb_integration` as a **fifth** build tag and a memory plane, and the
  `musr-e2e` CI job (`.github/workflows/ci.yml:380`) gains `arcadedb` to its
  `docker compose up -d` (it brings up only `garage` today, `ci.yml:490`). ISO-02a asks for
  one unattended gate; six planes behind one command is that. The always-compile floor step
  (`ci.yml:465`) gains the tag too.
  — **Reversibility:** costly — every consumer of that tag line (CI job, make target, runbook
  Acceptance section, local docs) moves together.

- **D-11:** Memory cross-deny is asserted on **all three** surfaces, not one:
  1. the model-facing tool (`memory_search` / `memory_facts_about` / `memory_recall`) under
     identity B's `identityctx`, proving the whole chain `identityctx → DatabaseFor → derived
     credential`;
  2. identity B's HMAC-derived credential aimed directly at identity A's database over
     ArcadeDB's HTTP API, expecting a `SecurityException` — the server-enforced property
     `internal/arcadedb/tenant.go:14-34` was written to buy;
  3. the `arcadedb-mcp` sidecar called as B with a verified access token whose `sub` is B,
     proving the tenant selector honours the token and not a caller-supplied header.
  — **Reversibility:** reversible.

- **D-12:** `scripts/lib/disposable_stack.sh` is **extracted** from `scripts/coverage_docker.sh`
  (provision disposable Postgres container, create `aura_app`/`aura_migrate`, create the
  throwaway DB owned by `aura_migrate`, trap-drop on exit, hard-refuse the name `aura` with
  exit 4). Both `coverage_docker.sh` and the new `scripts/musr_e2e.sh` source it. The guard
  exists because pointing the `db_integration` tier at a live `aura` destroyed auth data on
  2026-07-10 (`scripts/coverage_docker.sh:47-53`); it must not exist twice.
  — **Reversibility:** costly — `coverage_docker.sh` is a release-blocking gate, so the
  extraction has to be proven not to change its behaviour.

- **D-13:** `make musr-e2e` brings up everything it needs — disposable Postgres via the lib,
  `docker compose up -d garage arcadedb`, readiness wait, `go run ./scripts/authula_seed_e2e.go`,
  the tagged test, then teardown of what it started. The CI job calls the same target, so CI
  and a clean local checkout run one sequence rather than two that drift.
  — **Reversibility:** reversible.

### Proving execution separation (ISO-05)

- **D-14:** The proof is a **concurrent-runner white-box test**: two `LlmAgent` runs driven
  concurrently under two `identityctx` values in one process, under `-race` and `goleak`, with
  a deliberate collision attempt (same tool, same arguments, overlapping in time) that must
  neither replay nor cross. `LlmAgent` is already per-turn by construction
  (`internal/agent/llm_agent.go:47,105`), so the leak surface is the process-wide singletons
  around it — that is what the test constrains. Not a randomized-interleaving harness
  (Bombadil shape) and not a production hot-path assertion.
  — **Reversibility:** reversible.

- **D-15:** Four shared surfaces must each be enumerated and proven disjoint. Research may
  measure them; it may **not** silently shorten this list:
  1. **Run-dir sidecar spillover** — `internal/agent/tools/result.go:205` writes
     `$AURA_RUN_DIR/conversations/{sessionID}/{spillID}.result`: one root, one uid, namespaced
     by conversation UUID and not by identity. Decide whether identity belongs in that path or
     whether the strict profile's per-identity sandbox is the accepted mitigation for a
     filesystem shared by construction.
  2. **Gateway ledger / idempotency + approvals** — `ReservationKey{ConversationID, RequestID,
     ToolCallID}` carries no identity (`internal/gateway/approve.go:218-220`). Conversation
     UUIDs make a collision improbable, not impossible by construction, and this is the
     machinery the F-1 replay defect already lived in.
  3. **`tools.Registry`, `Budget`, `SteerInbox`** — the process-wide objects an `LlmAgent`
     holds by pointer; `Budget` is documented as shared across the swarm tree
     (`internal/agent/llm_agent.go:98-104`). Prove a steer message cannot be drained by the
     wrong run.
  4. **`llm.Client`, `PromptBuilder`, KV prefix** — one of each serves every identity, and
     `messages[0]` must stay byte-identical. Prove no identity-derived content reaches the
     stable prefix and that two concurrent requests cannot interleave into one another's
     message list.
  — **Reversibility:** reversible.

### The phase-closing live run

- **D-16:** A **committed harness** drives both conversations: it authenticates as each
  identity and drives two concurrent `POST /agent/run` conversations through the real AG-UI
  gateway with real tool calls, capturing both transcripts and their timing. Re-runnable by
  anyone; the operator's role is to read and score.
  — **Reversibility:** reversible.

- **D-17:** Both identities perform the **same three tasks on their own data, deliberately
  raced**: a document search, a memory write, a sandbox command, timed to overlap. Symmetry
  makes a leak self-evident — B's answer carrying A's token needs no interpretation — and
  racing is what actually exercises the shared singletons of D-15.
  — **Reversibility:** reversible.

- **D-18:** A ≥9.8 scoring rubric is written down (dimensions, weights, what 9.8 means) and
  the transcripts are scored against it each phase — **but the rubric does not gate.** What
  blocks the phase is the machine-checkable half: the isolation assertions, required tools and
  expected tokens. The rubric score is recorded as phase evidence.
  `internal/agenteval/case.go:14-17` states that "a gate that needs a model to decide whether
  it passed cannot be trusted to gate the model"; under this split nothing contradicts it and
  the comment stands **unamended**. Do not silently reverse that position.
  — **Reversibility:** costly — the rubric becomes the cross-phase scoring instrument every
  later phase's closing run is measured with.

### Claude's Discretion

- Every cross-deny assertion carries a **positive control**: identity A must still read its
  own document / fact / object in the same run. Without it, an empty result for B could be an
  unwritten fact passing as isolation. Test hygiene, not a gray area.
- Naming of the CLI verb's flags, the rubric's exact dimension names, and the internal
  structure of `scripts/lib/disposable_stack.sh` are open.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone and requirements
- `.planning/ROADMAP.md` §Phase 1 — success criteria 1-4 and the "Closes on (live run)" line
- `.planning/REQUIREMENTS.md:75-80,93-94` — ISO-01, ISO-02, ISO-02a, ISO-05, E2E-01, E2E-02
  verbatim, each with its measured file:line evidence
- `.planning/PROJECT.md` §Constraints — the read-the-reference-implementations constraint
- `.planning/STATE.md` §Blockers/Concerns — **"Before phase 1 starts"**: the working tree
  carried uncommitted production code in `internal/arcadedb/` on 2026-09-07. Land or stash it
  before the phase begins.
- `prd.md` — architectural truth-source; PROJECT.md is its planning projection

### The switch and the profile chain
- `docs/runbooks/musr-rollout.md` — the existing 85-line rollout runbook: what is already
  scoped whatever the flag says, the shared administrator control planes, the procedure,
  reversibility, and the Acceptance block this phase replaces
- `internal/config/config_validate.go:113-131` — `gateMultiUserRequiresStrictProfile`, Fatal
- `internal/config/config_validate.go:199-292` — the other strict gates (`gateObjectStoreCreds`,
  `gateGarageRPCSecret`, `gateWebAuth`); install.sh already generates all four secrets they
  demand, so a strict fresh install does not trip them
- `internal/config/config.go:321,554` and `internal/config/config_knobs.go:158` — the knob
- `compose.yaml:148-150,294-313,336-337` — the fallbacks and the comments explaining them
- `scripts/install.sh:463-482,541-554,605-663` — secret generation, the fresh-`.env` heredoc
  and its own warning that the heredoc drifts
- Commit `37211f83d` — why flipping the compose default alone boot-loops every upgrade
- Commit `a3536af5d` — Garage admin API published on loopback (removes one of ISO-02a's two
  manual steps)

### Provisioning
- `internal/agui/onboarding_provision.go` — the saga: pre-validate, Leg B, Leg A, recovery,
  resource legs, Leg C, audit, seed. `:128-131` `errIsolationDisabled`; `:160` the required-ports
  check that makes Telegram mandatory
- `internal/agui/onboarding_provision_resources.go` — `MemoryProvisioner` / `ObjectStoreProvisioner`
  / `FilesystemProvisioner`, eager and idempotent with symmetric compensation
- `internal/agui/onboarding_session.go:278-320` — `StartSession` and `sessionForRequester`
- `internal/agui/onboarding_api.go:199` — the `StartSession` call site the CLI verb mirrors
- `internal/agui/deprovision.go` and `deprovision_sandbox_test.go` — the mirror the new eager
  sandbox leg owes compensation to
- `internal/sandbox/usersandbox/router.go:81`, `docker_backend_lifecycle.go:58,185` — how the
  box is created today

### Isolation proof
- `cmd/aura/two_identity_e2e_test.go` — the gate; its header documents which planes it covers
- `cmd/aura/two_identity_e2e_harness_test.go:42-90` — `musrEnvOrSkip`, `musrMigratedPool`,
  `musrProvisionIdentity`
- `internal/arcadedb/tenant.go:14-75` — one database and one derived credential per identity,
  and why a WHERE clause was rejected
- `internal/agent/tools/result.go:198-205` — `sidecarPath`
- `internal/gateway/approve.go:73-95,213-227` — `ReservationKey`, session grants, `alwaysGranted`
- `internal/agent/llm_agent.go:42-120` — what is per-turn and what is held by pointer
- `internal/agenteval/case.go:1-55` and `cases.go` — the existing behaviour gate and its
  explicit no-rubric position (D-18 must not contradict it)
- `.github/workflows/ci.yml:380-560` — the `musr-e2e` job, its env, its compose overrides and
  the always-compile floor step
- `.github/compose.ci-musr.yaml` — the CI override that publishes Garage :3903
- `scripts/coverage_docker.sh:40-120` — the disposable-stack pattern D-12 extracts

### Reference implementations (PROJECT.md constraint — read before designing)
- `D:/tmp/LibreChat` — `config/create-user.js` (CLI → the same `registerUser` the HTTP path
  uses), `config/{invite,list,delete,ban}-user.js`, `.env.example:861` shipped
  `ALLOW_REGISTRATION=true`, `e2e/bombadil/*.specification.ts` (temporal invariants over
  randomized interleavings). **Counter-example, do not copy:** its isolation is a `user` +
  `tenantId` field on shared Mongo collections (`packages/data-schemas/src/schema/convo.ts:372`)
  with no per-tenant connection — strictly weaker than Aura's RLS + per-identity database +
  per-identity Garage key.
- `D:/tmp/hermes-agent`, `D:/tmp/system-prompts-and-models-of-ai-tools` — named in
  PROJECT.md §Constraints

### Standing project rules
- `CLAUDE.md` — inventory before invention, read the documentation first, 600-LOC ceiling,
  no-skip-as-green, coverage floor 85%, migration numbering via `ls internal/db/migrations/ | tail -1`
- `docs/aura-quality-snapshot.md` — measurement ledger; update a row only when its metric is
  actually measured

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/agui/onboarding_provision_resources.go` — the memory / Garage / filesystem legs
  are **already eager, idempotent and compensated**. E2E-02's four resources are largely built;
  what is missing is the flag and the sandbox leg, not the legs.
- `scripts/coverage_docker.sh:63-116` — a complete disposable-Postgres bootstrap with trap
  teardown and an `aura`-name refusal, written after a real data loss. D-12 extracts it rather
  than writing it a second time.
- `.github/compose.ci-musr.yaml` — already publishes Garage's admin API for host-run tiers;
  the local target can layer the same override.
- `internal/agenteval` — an existing machine-checkable behaviour harness (`Case` with
  `AnswerContains` / `AnswerMustNotContain` / `RequiredTools`) and a live tier. The closing
  run's machine-checked half should look like this, not like a new invention.
- `scripts/authula_seed_e2e.go` — the seed the CI job already runs; `make musr-e2e` reuses it.

### Established Patterns
- **Server-enforced tenancy over filters.** `internal/arcadedb/tenant.go` chose a database and
  credential per identity precisely so no WHERE clause can be forgotten. Any new isolation
  work follows that shape, not LibreChat's field-filter shape.
- **Fail-closed gates with an actionable message.** `config_validate.go` gates carry the exact
  knob and the exact remedy — and one comment records that an earlier gate named a variable
  nothing read, leaving operators in a boot loop following its own instructions. D-03's
  preflight message must name a command that works.
- **Saga legs are idempotent with symmetric compensation**, journaled through `run.step`, with
  compensation on a `context.WithoutCancel` context. The new sandbox leg follows it exactly.
- **No-skip-as-green.** Every env read in the gate goes through `musrEnvOrSkip`, which
  `t.Fatal`s under `$CI`. A sub-second "integration" runtime is a skip tell.
- **Per-turn `LlmAgent`, shared collaborators.** Anything identity-derived must be per-turn or
  keyed by identity; the four surfaces in D-15 are where that assumption is unproven.

### Integration Points
- `scripts/install.sh` heredoc + `.env.example` → the shipped posture (D-01)
- `aura serve` boot path → the sandbox-image preflight and the non-strict INFO line (D-03, D-05)
- `cmd/aura` command tree → `aura identity create` over `StartSession` + `Provision` (D-07)
- `onboardingService` resource legs → the new eager sandbox leg and its compensation (D-09)
- `cmd/aura/two_identity_e2e_test.go` tag line + `ci.yml` musr-e2e job + `Makefile` → the
  five-tag gate and `make musr-e2e` (D-10, D-13)
- `internal/agent` → the concurrent-runner test and whatever the four surfaces force (D-14, D-15)

</code_context>

<specifics>
## Specific Ideas

- **LibreChat was named by the user as required reading** and it changed two decisions: the
  shipped-default location (`.env.example`, mirroring `ALLOW_REGISTRATION=true`) and the CLI
  shape (a thin front over the one service, mirroring `create-user.js` → `registerUser`). Its
  isolation model is explicitly the counter-example.
- The ≥9.8 bar is a **recorded verdict**, not a gate. The user was explicit that the machine
  checks decide whether the phase closes.
- Symmetric racing tasks were chosen over realistic asymmetric work specifically because a
  leak must be *self-evident in the transcript*, not a judgement call.
- The sandbox leg was made eager against the "no idle container" argument, because a failure
  belongs at provisioning time rather than at the user's first tool call.

</specifics>

<deferred>
## Deferred Ideas

- **Deprovisioning symmetry (ISO-10-shaped).** The new eager sandbox leg owes its own
  compensation inside the saga — that is in scope. A full teardown drill across every plane is
  not; it belongs with ISO-10 and the Phase 5 lifecycle work.
- **A randomized-interleaving / property harness for execution isolation** (the Bombadil
  shape from LibreChat). Rejected for Phase 1 in favour of the deterministic concurrent-runner
  test; revisit if D-14 proves too coarse, or alongside Phase 3's adversarial suite.
- **A fail-closed identity assertion in the runner hot path.** Considered for ISO-05 and not
  taken — it is production code in the hot path and still needs the test. Reconsider if the
  D-15 surfaces turn up something a test cannot constrain.
- **Publishing the sandbox image to a registry** so `docker compose pull` fetches it. Not
  taken; install.sh builds it. Revisit at Phase 7, where a publish leg would need its own
  bundle evidence.
- **Cockpit-driven identity creation.** Phase 2 SC5 already owns "the cockpit admin section
  creates an identity"; Phase 1 ships the CLI and leaves the UI leg there.
- **Rotation path for `AURA_ARCADEDB_TENANT_SECRET`** — named in STATE.md as a Phase 5 concern.

</deferred>

---

*Phase: 1-Two Identities, Live and Separated*
*Context gathered: 2026-09-07*
