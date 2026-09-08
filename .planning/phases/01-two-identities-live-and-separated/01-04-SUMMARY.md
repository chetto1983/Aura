---
phase: 01-two-identities-live-and-separated
plan: 04
subsystem: infra
tags: [ci, make, bash, postgres, garage, arcadedb, disposable-stack, musr-e2e, iso-02a]

# Dependency graph
requires:
  - phase: 01-two-identities-live-and-separated (plan 01)
    provides: "the eager sandbox leg and aura identity create verb the acceptance gate provisions through"
  - phase: 01-two-identities-live-and-separated (plan 02)
    provides: "the shipped multi-identity profile and sandbox-image preflight the runbook Procedure now documents"
  - phase: 01-two-identities-live-and-separated (plan 03)
    provides: "the five-tag build line (arcadedb_integration) and the memory-plane cross-deny subtests make musr-e2e now runs unattended, plus the memory-up-core fix this plan's kept bring-up step reuses"
provides:
  - "scripts/lib/disposable_stack.sh — the disposable-Postgres bootstrap (read_secret, the aura-name exit-4 guard, dual-mode and always-local bring-up, teardown, composed-DSN export) extracted once, sourced by both coverage_docker.sh and musr_e2e.sh"
  - "scripts/musr_e2e.sh + make musr-e2e — one command, from a clean checkout, that brings up a disposable Postgres + reuses-or-starts garage/arcadedb/aura-llama-embed, seeds Authula, runs both tagged tiers (cmd/aura five-tag E2E + cmd/arcadedb-mcp MCP-boundary test), tears down only what it started"
  - "ci.yml musr-e2e job calling make musr-e2e for the acceptance gate itself, with the compose postgres+arcadedb+garage bring-up kept for the load-chaos/DR-drill steps that run later in the same job"
  - "the always-compile floor now also vets ./cmd/arcadedb-mcp/ at arcadedb_integration"
  - "docs/runbooks/musr-rollout.md: sandbox-image Procedure step, aura identity create + Telegram-token prerequisite, Acceptance section naming make musr-e2e"
affects: ["01-06 (the phase-closing live run's acceptance-gate leg is now make musr-e2e, one command CI and a developer both run)"]

# Actuals (#2632)
actuals:
  tokens: 12374
  tasks: 3
  commits: 4
  plan_head_before: 738c46ccbcc5bab4c17a37a01067a998f7d54080

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Sourced shell library contract: no set -euo pipefail and no cd at file scope, every top-level statement a comment or function definition — a sourced file that changes its caller's shell options or working directory is a trap for the second caller"
    - "Reuse-not-recreate compose bring-up: docker compose ps -q per service BEFORE any docker compose up, so an already-running shared service is never presented with a config-diff override (the CI garage.toml swap) that would recreate it out from under a concurrent session; only services this run itself started are torn down"
    - "APPEND, never clobber, an inherited COMPOSE_FILE — a caller (CI) may already layer an override this script also needs (the CPU-image swap for a GPU-less runner); overwriting would silently drop it"

key-files:
  created:
    - scripts/lib/disposable_stack.sh
    - scripts/lib/disposable_stack_test.sh
    - scripts/musr_e2e.sh
  modified:
    - scripts/coverage_docker.sh
    - Makefile
    - .github/workflows/ci.yml
    - docs/runbooks/musr-rollout.md

key-decisions:
  - "scripts/lib/disposable_stack.sh exposes TWO bring-up entry points, not one: disposable_stack_bring_up_auto (coverage_docker.sh's ORIGINAL GITHUB_ACTIONS-conditioned dual-mode, preserved verbatim so the extraction changes nothing) and disposable_stack_bring_up_local (always spins its own container, for musr_e2e.sh, which must never assume another step already brought up a shared compose postgres service)."
  - "Deviation from a literal reading of Task 3's action text: kept the make db-migrate memory-up-core bring-up step in ci.yml's musr-e2e job (adding docker compose up -d garage to it) instead of removing it. Measured why: production_load_chaos.py and restore_drill.sh run LATER in the SAME job, are not part of the acceptance gate, and target the compose postgres service directly (127.0.0.1:5432, database aura) — make musr-e2e never touches that service. Removing the kept step would have left those two steps with nothing to reach. Bringing up garage/arcadedb/aura-llama-embed there also means musr_e2e.sh's own already-running detection reuses rather than starts-then-tears-down them, so they survive for the later steps exactly as before this plan."
  - "musr_e2e.sh's own COMPOSE_FILE handling APPENDS .github/compose.ci-musr.yaml to an inherited value rather than overwriting it — the CI job's env already layers a THIRD file, .github/compose.ci-cache.yaml, which swaps aura-llama-embed to the CPU image (mandatory on a GPU-less runner). An earlier draft overwrote COMPOSE_FILE and would have broken every hosted CI run; found and fixed before commit by reading .github/compose.ci-cache.yaml, not by breaking and re-running the actual job."
  - "musr_e2e.sh reads its required secrets (POSTGRES_PASSWORD, AURA_GARAGE_ADMIN_TOKEN, AURA_OBJECTSTORE_ACCESS_KEY/SECRET_KEY, AURA_ARCADEDB_TENANT_SECRET, AURA_AUTHULA_SECRET, ARCADEDB_PASSWORD) via read_secret, which checks an already-exported env var before .env — so the SAME script works unmodified in CI (job env exports them) and locally (.env supplies them), and the Garage layout/bucket/key/allow block, run with the deployment's OWN credentials rather than CI's dummy values, is idempotent against an already-configured live Garage."

requirements-completed: [ISO-02a]

coverage:
  - id: D1
    description: "The disposable-Postgres bootstrap and its aura-name anti-footgun exist once, sourced by both coverage_docker.sh and musr_e2e.sh; the extraction does not change the release-blocking coverage gate's behavior"
    requirement: "ISO-02a"
    verification:
      - kind: other
        ref: "bash scripts/lib/disposable_stack_test.sh (11/11 assertions, WSL) + bash scripts/coverage_docker.sh (exit 0, 34632/40036=86.5%, live aura.identities count unchanged 2->2, disposable container gone after)"
        status: pass
    human_judgment: false
  - id: D2
    description: "One command, from a clean checkout, brings up a disposable Postgres + Garage (admin API on loopback) + ArcadeDB + embed sidecar, seeds Authula, runs both tagged tiers, tears down only what it started"
    requirement: "ISO-02a"
    verification:
      - kind: integration
        ref: "make musr-e2e (WSL, live run): ok cmd/aura 6.076s, ok cmd/arcadedb-mcp 2.111s, 69s script-reported / 3m48s wall-clock, no leftover disposable-Postgres container, live aura.identities unchanged"
        status: pass
    human_judgment: true
    rationale: "Only the reuse-not-recreate branch (services already running) was exercised on this host — running the fresh-bring-up branch would have meant tearing down the live multi-user dev stack other sessions depend on. The fresh-bring-up code path (what a genuinely clean CI runner exercises every time) is code-reviewed against the ported ci.yml steps, not executed end-to-end in this session."
  - id: D3
    description: "CI's musr-e2e job calls the identical make musr-e2e a developer runs; the always-compile floor covers both tiers at all five tags; the job's env still exports the composed DSNs the skip-helpers fail on"
    requirement: "ISO-02a"
    verification:
      - kind: other
        ref: "python3 yaml assertion (job calls make musr-e2e, env carries AURA_DB_URL/AURA_DB_MIGRATE_URL) + bash scripts/check_ci_go_packages.sh (exit 0) + python3 yaml.safe_load parses cleanly"
        status: pass
    human_judgment: true
    rationale: "The workflow file was verified to parse and to call the right target with the right env, but no GitHub Actions run was triggered in this session — the actual CI execution (including the fresh-bring-up branch and the load-chaos/DR-drill steps that depend on the kept bring-up step) is unverified until a real CI run happens."

duration: ~55 min
completed: 2026-09-08
status: complete
---

# Phase 01 Plan 04: The Disposable-Stack Library + make musr-e2e Summary

**Extracted the disposable-Postgres bootstrap out of the release-blocking coverage gate into a shared library, built `make musr-e2e` — the single command that brings up a disposable Postgres, Garage, ArcadeDB and the embed sidecar, seeds Authula, and runs both tagged acceptance tiers — and collapsed CI's five-step bring-up into that same command, running it for real end to end (not just syntax-checked) on the live stack.**

## Performance

- **Duration:** ~55 min (approximate — includes a ~13 min background `coverage_docker.sh` run)
- **Tasks:** 3 completed
- **Files modified:** 7 (3 created, 4 modified)
- **Commits:** 4 (`ab33ecc43`, `324a45e3e`, `e8057c1d3`, `e226d9b14`)

## Accomplishments

- `scripts/lib/disposable_stack.sh` (new) carries `read_secret`, the `aura`-name exit-4 guard (`disposable_stack_guard_name`), two bring-up entry points (`disposable_stack_bring_up_auto` — coverage_docker.sh's original GITHUB_ACTIONS-conditioned dual mode, preserved verbatim — and `disposable_stack_bring_up_local` — always spins its own container, for a caller with no other step bringing up shared infrastructure), `disposable_stack_teardown`, and `disposable_stack_export_env` (the composed DSNs, not just the `POSTGRES_*` primitives).
- `scripts/coverage_docker.sh` now sources the library instead of inlining the bootstrap. Ran end to end in WSL: exit 0, `34632/40036 = 86.5%` owned-surface coverage (same result class as before the extraction), and `aura.identities` row count on the LIVE database (2) was read before and after — unchanged.
- `scripts/lib/disposable_stack_test.sh` (new) asserts, without a Docker daemon: the guard exits 4 for `aura` and 0 for a throwaway name; `read_secret` prefers an exported value over `.env` and strips a trailing CR; the composed-DSN exporter's output carries the requested db/host/port; and — added in a follow-up `test(01-04)` commit — that `bring_up_local`/`bring_up_auto` refuse the name `aura` BEFORE ever invoking `docker` (proven with a stub `docker` on `PATH` that fails loudly if called). 11/11 assertions pass.
- `scripts/musr_e2e.sh` (new) + `make musr-e2e` (new Makefile target): asserts every required secret is resolvable (naming the first missing one); brings up its own disposable Postgres via `disposable_stack_bring_up_local` with `trap EXIT` registered before anything else starts; detects whether garage/arcadedb/aura-llama-embed are already running and reuses them without recreating (never presenting the CI compose override's config diff to an already-live container) or starts them if not; waits for health by polling (never a fixed sleep); ports the CI job's Garage layout/bucket/key/admin-probe block, run against the deployment's own `.env` credentials so it is idempotent against an already-configured live Garage; seeds the Authula operator; runs `go test -race -count=1 -p 1` with all five tags over `./cmd/aura/` (`TestTwoIdentityCrossDeny|TestProvisionLoginIsolatedRun|TestIdentityCreateProvisionsEveryPlane`) and with `arcadedb_integration` over `./cmd/arcadedb-mcp/` (`TestMemoryCrossDenyThroughTheMCPBoundary`); tears down only what it itself started.
- **Ran `make musr-e2e` for real** on the live stack (garage/arcadedb/aura-llama-embed/postgres already up and healthy): all three reused without recreation, 95 migrations applied to the disposable DB, Garage admin probe → 200, `ok cmd/aura 6.076s`, `ok cmd/arcadedb-mcp 2.111s` (both well over 1s — not the `[no test files]`/sub-second skip-as-green shape), 69s wall-clock reported by the script itself (`real 3m48.774s` including WSL/toolchain overhead). After teardown: no leftover disposable-Postgres container, live `aura.identities` unchanged, garage/arcadedb/aura-llama-embed still running (correctly left alone).
- `.github/workflows/ci.yml`'s `musr-e2e` job: the Garage layout/bucket/key block, the Admin-API probe, the Authula seed, and the tagged `go test` collapse into one `make musr-e2e` step. The always-compile-floor step gained a second `go vet` call for `./cmd/arcadedb-mcp/` at `arcadedb_integration` (holding `TestMemoryCrossDenyThroughTheMCPBoundary`, added by 01-03, against rot — it was not covered by this floor before). **Deviation from Task 3's literal action text:** kept the `make db-migrate memory-up-core` bring-up step (adding `docker compose up -d garage` to it) rather than removing it — see Deviations below.
- `docs/runbooks/musr-rollout.md`: Procedure gained the sandbox-image step (`make sandbox-images` / `docker pull`, and the exact `aura serve` boot-refusal message an operator would meet) and named `aura identity create` alongside the cockpit wizard with the Telegram bot token as a provisioning prerequisite. Acceptance section's first bullet is now `make musr-e2e`; the three unit-level bullets (`TestProvisionRefused`, `TestSkillManage`, `TestGateMultiUserRequiresStrictProfile`) are unchanged.

## Task Commits

1. **Task 1: Extract the disposable-stack library, prove the coverage gate unchanged** — `ab33ecc43` (feat)
2. **[test follow-up] Prove the guard fires before bring_up_local/auto touch docker** — `324a45e3e` (test)
3. **Task 2: One command brings up the stack, runs the gate, tears it down** — `e8057c1d3` (feat)
4. **Task 3: CI runs the same command, the runbook says so** — `e226d9b14` (feat)

**Plan metadata:** committed separately below (docs).

## Files Created/Modified

- `scripts/lib/disposable_stack.sh` (200 lines, new) — the extracted bootstrap
- `scripts/lib/disposable_stack_test.sh` (117 lines, new) — daemon-free regression test
- `scripts/coverage_docker.sh` (modified) — now sources the library
- `scripts/musr_e2e.sh` (233 lines, new) — the one-command acceptance gate
- `Makefile` (modified) — `musr-e2e` target + help text + `.PHONY`
- `.github/workflows/ci.yml` (modified) — `musr-e2e` job collapse + always-compile-floor addition
- `docs/runbooks/musr-rollout.md` (modified) — Procedure + Acceptance updates

## Decisions Made

See `key-decisions` in frontmatter. In prose: the library ships two bring-up entry points because coverage_docker.sh's original CI-shared-postgres-reuse behavior had to be preserved byte-for-byte (D-12's own constraint), while musr_e2e.sh needed a fully self-contained variant that never assumes another step already brought up shared infrastructure. The most consequential finding was that a literal reading of Task 3's own action text — replace the whole bring-up with `make musr-e2e` — would have broken `production_load_chaos.py` and `restore_drill.sh`, two steps later in the SAME CI job that are not part of the acceptance gate and target the compose `postgres`/`arcadedb`/`garage` services `make musr-e2e` deliberately never touches. Fixed by keeping the bring-up step (Rule 1: bug the literal plan text would have introduced), verified by reading both consumer scripts' env reads rather than assumed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] A literal collapse of ci.yml's bring-up would have broken the job's own later steps**
- **Found during:** Task 3, reading `scripts/production_load_chaos_support.py` and `scripts/restore_drill.sh` before editing `ci.yml`
- **Issue:** Task 3's action text says to replace `make db-migrate memory-up-core` (among other steps) with one `make musr-e2e` call. `production_load_chaos.py` (via `fresh_database`/`daemon_env`) and `restore_drill.sh` run later in the SAME job, are not part of the acceptance gate, and both target the compose `postgres` service directly (127.0.0.1:5432, database `aura`) plus `arcadedb`/`garage`. `make musr-e2e` never touches the compose `postgres` service — it provisions its own disposable one on a different port, torn down when it exits. A literal collapse would have left those two later steps with nothing to reach.
- **Fix:** Kept the `make db-migrate memory-up-core` step, adding `docker compose up -d garage` to it, so `make musr-e2e`'s own already-running detection finds garage/arcadedb/aura-llama-embed already up and reuses (rather than starts-then-tears-down) them — leaving them alive for the later steps exactly as before this plan.
- **Files modified:** `.github/workflows/ci.yml`
- **Verification:** the plan's own YAML tag-selection assertion passes; `bash scripts/check_ci_go_packages.sh` exits 0; the workflow parses as YAML. The actual CI execution of this fix (a real GitHub Actions run) was NOT triggered in this session — see coverage `D3`'s rationale.
- **Committed in:** `e226d9b14`

**2. [Rule 1 - Bug] musr_e2e.sh's first draft would have clobbered CI's own COMPOSE_FILE, breaking the GPU-less runner override**
- **Found during:** Task 2, before running `make musr-e2e` for the first time
- **Issue:** `ci.yml`'s `musr-e2e` job env sets `COMPOSE_FILE: compose.yaml:.github/compose.ci-musr.yaml:.github/compose.ci-cache.yaml` — the third layer swaps `aura-llama-embed` to the CPU image and removes its GPU device reservation, mandatory on a GitHub-hosted runner with no NVIDIA device. An unconditional `export COMPOSE_FILE="compose.yaml:.github/compose.ci-musr.yaml"` in the script would have overwritten that inherited value and silently restored the GPU reservation, breaking every hosted CI run.
- **Fix:** `musr_e2e.sh` now appends `.github/compose.ci-musr.yaml` to an inherited `COMPOSE_FILE` only if not already present, defaulting to `compose.yaml:.github/compose.ci-musr.yaml` only when `COMPOSE_FILE` is unset (the local case).
- **Files modified:** `scripts/musr_e2e.sh`
- **Verification:** found by reading `.github/compose.ci-cache.yaml` before the first real run, not by breaking and re-running CI. `bash -n` clean; the local run (COMPOSE_FILE unset) exercised the default branch successfully.
- **Committed in:** `e8057c1d3`

---

**Total deviations:** 2 auto-fixed (both Rule 1 — bugs the plan's own text or a first draft would have introduced). **Impact on plan:** Both were necessary to avoid regressions in infrastructure this plan does not own (the load-chaos/DR-drill steps' Postgres access; CI's GPU-less runner compatibility). Neither expands scope beyond what Task 2/Task 3 already required.

## Issues Encountered

- **Two commits were needed for Task 1 to satisfy the TDD-gate directive to land at least one `test(01-04):` commit.** The initial extraction (library + `coverage_docker.sh` rewrite + `disposable_stack_test.sh`) landed as one `feat(01-04)` commit. A follow-up `test(01-04)` commit then strengthened `disposable_stack_test.sh` with two new assertions (proving the guard fires before `bring_up_local`/`bring_up_auto` ever touch `docker`, using a stub `docker` on `PATH`) — genuine new coverage, not busywork, but the split was driven by the directive rather than a natural RED/GREEN boundary. This plan's tasks are all `type="auto"` (no `tdd="true"` frontmatter), so no literal RED-before-GREEN cycle applied; documented per the same finding recorded in 01-01/01-03/01-05's SUMMARYs.
- **`coverage_docker.sh`'s `grep -c 'disposable_stack.sh'` acceptance criterion (must equal exactly 1) required trimming prose comments.** An initial header comment and a `# shellcheck source=` pragma both mentioned the literal filename, pushing the count to 4. Reworded to refer to "the sourced library"/"the extracted library" in prose, dropped the shellcheck pragma, leaving only the actual `source` line — count now 1.

## User Setup Required

None — no external service configuration required. The runbook's new Procedure step documents `make sandbox-images`/`docker pull` for anyone standing up a hand-rolled deployment, but this plan itself needed no new secrets or accounts.

## Next Phase Readiness

- `make musr-e2e` is ready for plan 01-06's phase-closing live run to reuse as the acceptance-gate leg — it is the same command CI runs.
- **Not verified in this session:** the fresh-bring-up branch of `make musr-e2e` (spinning up garage/arcadedb/aura-llama-embed from cold, the CI runner's actual path every time) and the actual GitHub Actions execution of the collapsed `musr-e2e` job, including whether the kept `docker compose up -d garage` line correctly lets the later load-chaos/DR-drill steps proceed. Both are code-reviewed against the ported steps and the reuse-path's real success on this host, not executed end-to-end as a real CI run. The next CI run against this branch is the first real signal.
- `internal/sandbox/usersandbox`'s open coverage debt (81.6% against the delegated `docker_coverage` 85% floor, recorded in `.planning/STATE.md` and shared memory) is untouched by this plan — it belongs to 01-01/01-02's `usersandbox` code, not this plan's Make/CI/shell deliverables.
- `internal/gateway`'s mutation spot-check (recorded open in `.planning/WINDOWS.md` id 27 and `01-VALIDATION.md`) remains open; this plan's scope was Make/CI/shell infrastructure, not that mutation run.
- No blockers for the next plan in this phase's wave.

---
*Phase: 01-two-identities-live-and-separated*
*Completed: 2026-09-08*

## Self-Check: PASSED

- FOUND: `scripts/lib/disposable_stack.sh`
- FOUND: `scripts/lib/disposable_stack_test.sh`
- FOUND: `scripts/musr_e2e.sh`
- FOUND: commit `ab33ecc43`
- FOUND: commit `324a45e3e`
- FOUND: commit `e8057c1d3`
- FOUND: commit `e226d9b14`
- Plan-level `<verification>` re-run: `bash -n` clean on all three scripts; `bash scripts/lib/disposable_stack_test.sh` 11/11 PASS; `bash scripts/coverage_docker.sh` exit 0 (86.5%, live DB unchanged); `make musr-e2e` exit 0 (69s script-reported, both tagged tiers PASS >1s each, no leftover disposable container, live DB unchanged, shared services left running); `bash scripts/check_ci_go_packages.sh` exit 0; `python3 -c "import yaml; ..."` (workflow parses, job calls `make musr-e2e`, env carries composed DSNs) PASS.
