---
phase: 42-llm-conversation-compaction
plan: 09
subsystem: rollout-runtime
tags: [go, postgres, rollout, cas, runtime, i18n]
requires:
  - phase: 42-08
    provides: durable rollout store and immutable decision ledger
provides:
  - immutable versioned evaluator decisions and durable rollout controller
  - persisted effective-config reader and runtime claim/finalize version fence
  - boot/serve composition with signal-scoped evaluator lifecycle
  - PostgreSQL restart, replica, stale-finalize, and atomic rollback proof
affects: [42-10, compaction-rollout, runtime-composition]
key-files:
  created: [internal/conversations/compaction_eval/evaluator.go, internal/conversations/compaction_rollout.go, internal/conversations/compaction_rollout_chain_integration_test.go]
  modified: [internal/config/config_compaction.go, internal/runner/interfaces.go, internal/runner/runner_compact.go, cmd/aura/chat_boot.go, cmd/aura/serve.go]
key-decisions:
  - "Runtime activation reloads the persisted effective snapshot before claim and after backend work; a changed version disables finalization."
  - "Evaluator evidence and rollback reasons remain locale-neutral structured data; English and Italian localization stays at consuming operator surfaces."
  - "Boot fails closed when the durable rollout scope cannot be preflighted, and serve evaluation is owned by the signal context."
requirements-completed: [IC-13, IC-14]
duration: 31min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 09: Durable Rollout Runtime Chain Summary

**Evaluator evidence now flows through PostgreSQL CAS state into validated runtime config, with replica-safe rollback fencing every coordinator activation.**

## Accomplishments

- Added canonical immutable cohort decisions with SHA-256 evidence, exact version identities, bounded strata/censor data, and numerical promotion/rollback gates.
- Added a stateless controller that reloads durable state, promotes only after 24 hours and 1,000 eligible attempts, and atomically rolls back safety, continuation, failure, latency, restore, corruption, and stale-version breaches.
- Added a validated persisted effective-config reader, injected it into Runner and every compact surface, and fenced activation with matching pre/post durable versions.
- Wired the rollout store/controller into chat boot and the evaluator lifecycle into serve cancellation.
- Proved the full chain with two PostgreSQL pools, close/reopen restart, stale finalize rejection, immutable ledger verification, and atomic LKG restoration.

## Task Commits

1. **Evaluator and durable controller** — `ce74786d5`
2. **Persisted runtime composition and coordinator fence** — `9cea9563f`
3. **PostgreSQL full-chain proof** — `cb8af0b24`
4. **Named composition/lifecycle contract tests** — `62f463909`

## Decisions Made

- PostgreSQL effective state is the only activation authority; process memory carries only a single-operation version fence.
- Any version change between claim and finalize is treated as rollback/staleness and cannot activate a result.
- Durable reasons use stable snake-case codes and evidence uses structured numeric fields, preserving i18n independence.

## Deviations from Plan

### Auto-fixed Issues

- **[Rule 2 - Missing Critical] Boot ordering preserved earlier fail-closed adapter/hook validation.** Durable rollout preflight initially masked the existing command-hook error contract, so it was moved after hook validation and before Runner construction.
- **[Rule 2 - Missing Critical] Added executable named composition tests.** The first broad regex reported no matching cmd tests; the five required boot/serve lifecycle names were added and executed.

## Issues Encountered

- Gate retry: Task 1's first hook run rejected seven missing exported-symbol comments; exact lint diagnostics were fixed and the single normal retry passed.
- Gate retry: the first PostgreSQL fixture incorrectly assumed `Create` seeded live windows; it was corrected to perform the required bootstrap-to-shadow CAS.
- Gate retry: the next PostgreSQL run omitted that bootstrap decision from the expected ledger count; the exact three-entry ledger then passed.
- Gate retry: PowerShell stripped the first WSL regex quoting and Bash treated alternation as pipelines; the shell-safe rerun produced explicit `GATE_OK`.
- No hook was bypassed or duplicated. Existing `.planning/graphs/` dirt stayed unstaged.

## Verification

- Exact WSL/CGO PostgreSQL race test `TestCompactionRolloutFullChainPostgres`: passed with `GATE_OK`.
- Plan-wide WSL/CGO race gate across conversations, config, runner, and cmd/aura: passed with `GATE_OK`.
- Required named composition/lifecycle tests: passed.
- All four hook-enabled commits passed gofmt, vet, lint with zero issues, and file-size enforcement.

## Self-Check: PASSED

- All task artifacts exist and commits `ce74786d5`, `9cea9563f`, `cb8af0b24`, and `62f463909` are present.
- Runtime composition consumes the durable reader; rollback cannot be bypassed by static config.
- Durable reason/error vocabulary remains locale-neutral for English/Italian consuming surfaces.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
