# Phase 1: Two Identities, Live and Separated - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-09-07
**Phase:** 1-two-identities-live-and-separated
**Areas discussed:** Shipped profile & upgrade path, The documented provisioning path,
Long-term memory cross-deny, Making the gate unattended (ISO-02a), Execution-isolation proof
(ISO-05), The scored ≥9.8 live run, What each identity does in the run

**User-supplied reading:** `D:/tmp/LibreChat` — requested during area selection and
inventoried before the first question of area 1 was answered. Findings recorded in
CONTEXT.md `<specifics>` and `<canonical_refs>`.

---

## Shipped profile & upgrade path

### Where the shipped default lives

| Option | Description | Selected |
|--------|-------------|----------|
| `.env.example` carries it (LibreChat pattern) | compose keeps upgrade-safe fallbacks; `.env.example` ships strict + musr on | ✓ |
| A compose overlay file | `compose.musr.yaml`, mirroring `.github/compose.ci-musr.yaml` | |
| Flip compose defaults + upgrade preflight | genuinely ships on, changes behaviour for every existing operator | |

**Notes:** Chosen because an existing `.env` wins on upgrade, so no boot loop — the failure
mode commit `37211f83d` documented. Rejected the overlay because "on if you name a second
file" is not shipped-on.

### Who guarantees the sandbox image exists

| Option | Description | Selected |
|--------|-------------|----------|
| Boot preflight refuses to start | fail-fast with the exact build/pull command | ✓ |
| Published image + compose pulls it | no local build; adds a publish leg to the release | |
| Both | publish and preflight | |

### Where the three names actually land (asked after measuring)

Measurement that reframed the first answer: `.env.example:44` has `#AURA_PROFILE=dev`
commented out with no `AURA_MUSR_ISOLATION` / `AURA_SANDBOX_IMAGE` at all, and
`scripts/install.sh:612` writes its own `.env` from a heredoc that names none of the three —
so editing `.env.example` alone reaches nobody who runs the documented installer.

| Option | Description | Selected |
|--------|-------------|----------|
| Fresh-install heredoc + uncomment `.env.example` | both writers; `ensure_internal_env_secrets` untouched | ✓ |
| Heredoc only | one writer, nothing to drift against | |
| A `--profile` flag on install.sh | first-class installer concept; adds flag, test, docs leg | |

**Notes:** The sharp edge was `ensure_internal_env_secrets` running on the already-have-a-`.env`
path too — putting the profile there would flip existing deployments.

### Where the sandbox image gets built

| Option | Description | Selected |
|--------|-------------|----------|
| install.sh builds it as an install step | keeps the whole chain inside the one documented path | ✓ |
| compose builds it as a service | fewer moving parts in install.sh; couples image to compose | |
| You decide during planning | constraint only | |

### How an existing operator learns it is available

| Option | Description | Selected |
|--------|-------------|----------|
| Runbook only — silence at runtime | zero noise; discovery depends on release notes | |
| Runbook + one-line boot INFO | discoverable without reading release notes | ✓ |
| Report it in an existing health surface | visible when asked, not per boot | |

---

## The documented provisioning path

Measurement before the questions: the saga already provisions ArcadeDB database + derived
credential + schema, Garage bucket + scoped key, and the filesystem roots eagerly and
idempotently with symmetric compensation
(`internal/agui/onboarding_provision_resources.go`). **But** `onboarding_provision.go:160`
makes a configured Telegram bot a hard requirement of provisioning at all.

### Telegram as a hard dependency

| Option | Description | Selected |
|--------|-------------|----------|
| Make the Telegram leg optional | unblocks CI and Telegram-less deployments | |
| Keep it required — Telegram is part of the product | one onboarding story, one more prerequisite | ✓ |
| You decide during planning | constraint only | |

### Which surface is the documented path

| Option | Description | Selected |
|--------|-------------|----------|
| Cockpit wizard only | nothing new built; no headless route | |
| Add a CLI verb over the same saga (LibreChat pattern) | one honest route for operator, CI and E2E | ✓ |
| Both, wizard canonical | two documented routes over one saga | |

### How the gate provisions in CI, given Telegram stays required

| Option | Description | Selected |
|--------|-------------|----------|
| Fake `TelegramMint` in the test composition root | deterministic, no secret, fork-runnable | |
| A real bot token as a CI secret | proves the deep link; third-party network in the gate | |
| Fake in CI, real bot once in the scored live run | deterministic gate + one real proof | ✓ |

### How the CLI verb gets past the session requirement

| Option | Description | Selected |
|--------|-------------|----------|
| `StartSession` then `Provision`, in-process | the exact pair `onboarding_api.go:199` uses | ✓ |
| The CLI talks HTTP to a running daemon | works remotely; needs a running daemon + creds | |
| You decide during planning | constraint only | |

### The sandbox: lazy or an eager leg

| Option | Description | Selected |
|--------|-------------|----------|
| Leave it lazy — the run proves it | no idle container; proof is the run's sandbox command | |
| Add an eager sandbox leg to the saga | failures surface at provision, not first tool call | ✓ |

---

## Long-term memory cross-deny

Measurement: no cross-tenant memory test exists anywhere; the `musr-e2e` CI job brings up
only Garage (`ci.yml:490`), so the memory plane costs an ArcadeDB service however it is placed.

### Placement

| Option | Description | Selected |
|--------|-------------|----------|
| Extend `TestTwoIdentityCrossDeny` to 5 tags | one command, six planes; gate needs ArcadeDB up | ✓ |
| Sibling file, same job | existing gate's deps unchanged; "the gate" becomes two commands | |

### Which surface must refuse (multi-select)

| Option | Description | Selected |
|--------|-------------|----------|
| The model-facing tool, as identity B | proves `identityctx → DatabaseFor → credential` | ✓ |
| B's derived credential against A's database | proves the server-enforced boundary itself | ✓ |
| The `arcadedb-mcp` sidecar with B's token | proves the sidecar honours the token, not a header | ✓ |

**Notes:** All three selected — the roadmap's "any surface it can reach" was taken literally.

---

## Making the gate unattended (ISO-02a)

Measurement: `scripts/coverage_docker.sh:63-116` already provisions a disposable Postgres
container, roles and throwaway DB with trap teardown and an `aura`-name refusal (exit 4),
written after a real data loss on 2026-07-10.

### How the gate gets its disposable database

| Option | Description | Selected |
|--------|-------------|----------|
| Extract a shared lib, both scripts source it | one guard, one implementation | ✓ |
| Copy the pattern into a standalone script | faster; duplicates the data-loss guard | |
| The Go test provisions its own DB | self-sufficient; provisioning inside a test helper | |

### What `make musr-e2e` does about the rest of the stack

| Option | Description | Selected |
|--------|-------------|----------|
| The target brings up everything it needs | CI calls the same target; one sequence, no drift | ✓ |
| Assumes the stack is up; documents the prerequisite | fast on a warm box; clean checkout = two commands | |

---

## Execution-isolation proof (ISO-05)

Measurement: `LlmAgent` is already per-turn by construction (`llm_agent.go:47,105`), so the
leak surface is the process-wide singletons around it. Concrete finding surfaced during the
discussion: `internal/agent/tools/result.go:205` spills tool results to
`$AURA_RUN_DIR/conversations/{sessionID}/` — one root, one uid, namespaced by conversation
UUID rather than by identity.

### Shape of the proof

| Option | Description | Selected |
|--------|-------------|----------|
| Concurrent-runner white-box test | deterministic, `-race`, next to the code it constrains | ✓ |
| Interleaving invariant over randomized schedules (Bombadil shape) | finds orderings a scripted test never tries; new harness | |
| A fail-closed assertion inside the runner | enforced in production; hot-path code | |

### Which shared surfaces must be proven disjoint (multi-select)

| Option | Description | Selected |
|--------|-------------|----------|
| Run-dir sidecar spillover | conversation-named, not identity-named | ✓ |
| Gateway ledger / idempotency + approvals | `ReservationKey` carries no identity | ✓ |
| `tools.Registry`, `Budget`, `SteerInbox` | held by pointer; `Budget` shared across the swarm tree | ✓ |
| LLM client, prompt builder and KV prefix | one of each per process; `messages[0]` must stay stable | ✓ |

**Notes:** All four selected, and recorded in CONTEXT.md as a list research may measure but
may not silently shorten.

---

## The scored ≥9.8 live run

Measurement: `internal/agenteval` already exists and states an explicit position —
"There is no LLM judge and no rubric… a gate that needs a model to decide whether it passed
cannot be trusted to gate the model" (`case.go:14-17`) — which collides with the CLAUDE.md
≥9.8 bar.

### How agenteval's position and the ≥9.8 bar fit

| Option | Description | Selected |
|--------|-------------|----------|
| agenteval cases are the gate; ≥9.8 is the human verdict on top | keeps the principle intact | |
| Write a scoring rubric and score the transcript | makes the bar concrete and repeatable | ✓ |
| You decide during planning | constraint only | |

### Does the rubric gate the phase? (follow-up, because the choice contradicted a documented position)

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — it gates, and `case.go`'s comment is amended | position changed deliberately, on the record | |
| No — the machine checks gate; the rubric is the recorded verdict | nothing contradicts `case.go`; it stands unamended | ✓ |
| It gates, and agenteval stays out of it entirely | two harnesses, overlapping purpose | |

**Notes:** The follow-up resolved the tension without amending `internal/agenteval/case.go`.
A rubric is written and used for scoring, but only the machine-checkable half blocks the phase.

### How the pair of conversations is driven

| Option | Description | Selected |
|--------|-------------|----------|
| A committed harness drives both over AG-UI | re-runnable by anyone | ✓ |
| Operator drives one, harness drives the other | closest to real use; half re-runnable | |
| Two live human-driven sessions | unarguable transcript; not re-runnable | |

---

## What each identity does in the run

| Option | Description | Selected |
|--------|-------------|----------|
| Same three tasks, deliberately racing | leak is self-evident; exercises the shared singletons | ✓ |
| Different work, merely overlapping | realistic; leak harder to spot unambiguously | |
| Same tasks racing, plus one deliberate reach across | adds an active probe; partly Phase 3 territory | |

---

## Claude's Discretion

- Positive controls on every deny assertion — identity A must still read its own document /
  fact / object in the same run, so an empty result for B cannot be an unwritten fact passing
  as isolation. Recorded as test hygiene, not put to the user.
- Flag names for `aura identity create`, the rubric's exact dimension names, and the internal
  structure of `scripts/lib/disposable_stack.sh`.

## Deferred Ideas

- Full ISO-10-shaped deprovisioning drill (the new eager sandbox leg's own compensation is in
  scope; the cross-plane teardown is not) — Phase 5 / ISO-10.
- A randomized-interleaving property harness for execution isolation (the LibreChat Bombadil
  shape) — revisit if the concurrent-runner test proves too coarse, or with Phase 3.
- A fail-closed identity assertion in the runner hot path — reconsider only if a D-15 surface
  turns up something a test cannot constrain.
- Publishing the sandbox image to a registry — revisit at Phase 7, where a publish leg needs
  its own bundle evidence.
- Cockpit-driven identity creation — already owned by Phase 2 SC5.
- Rotation path for `AURA_ARCADEDB_TENANT_SECRET` — named in STATE.md as a Phase 5 concern.
