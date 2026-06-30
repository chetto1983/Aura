---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 05
subsystem: shared-leaves
tags: [qual-03, leaf-extraction, dedup, neostore, pgnumeric, envutil, refactor, crash-resume]

# Dependency graph
requires:
  - phase: 32-02
    provides: "QUAL-02 KEEP/swap baseline so the leaf extractions touch already-clean call sites"
provides:
  - "internal/neostore — canonical HashText/AsString/AsFloats + GraphClient seam (reasoningstore/toolselectstore/activelearn migrated, 3 copies deleted)"
  - "internal/pgnumeric — canonical NumericFromFloat/FloatFromNumeric/DefaultNumericMaxCost (conversations/cachemetrics migrated, 2 copies + dup consts deleted)"
  - "internal/envutil — canonical IntDefault/BoolDefault (config/telegram/registry migrated, copies deleted)"
affects: [32-09, 32-10, 33, conversations, cachemetrics, config, channels, reasoningstore, toolselectstore, activelearn]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Leaf extraction (D-06): stdlib(+pgtype) packages with NO internal imports → every consumer depends on the seam, no import cycle"
    - "Characterization parity (D-09/D-10): union-of-inputs table asserting value+err-presence (never the error string, Pitfall 5), the behavior + its test move to the new leaf"
    - "Cycle-avoidance home selection: a shared helper's home must not be a package whose own test suite imports a consumer (see Deviation 2)"

key-files:
  created:
    - internal/neostore/neostore.go
    - internal/neostore/neostore_test.go
    - internal/pgnumeric/pgnumeric.go
    - internal/pgnumeric/pgnumeric_test.go
    - internal/envutil/envutil.go
    - internal/envutil/envutil_test.go
  modified:
    - internal/reasoningstore/store.go
    - internal/toolselectstore/store.go
    - internal/toolselectstore/store_test.go
    - internal/activelearn/learner.go
    - internal/conversations/store_helpers.go
    - internal/conversations/store_append.go
    - internal/conversations/store_unit_test.go
    - internal/conversations/store_sequence_test.go
    - internal/cachemetrics/store.go
    - internal/cachemetrics/store_helpers.go
    - internal/cachemetrics/store_unit_test.go
    - internal/cachemetrics/store_helpers_test.go
    - internal/config/config_env.go
    - internal/config/config.go
    - internal/config/config_test.go
    - internal/channels/telegram/config.go
    - internal/channels/registry.go
  deleted: []

key-decisions:
  - "neostore is the canonical home for the sha256 content-MERGE hash + the APOC column coercers (the nil-vs-empty AsFloats distinction is load-bearing and pinned by test)."
  - "Numeric home is internal/pgnumeric, NOT internal/db (Open Question #1's first instinct): internal/db's cache_metrics_integration_test.go is `package db` and imports internal/cachemetrics, so cachemetrics importing internal/db forms a db<->cachemetrics test cycle (caught by `go vet -tags db_integration`). A dedicated pg-flavoured leaf keeps the 'Postgres home, not neostore' intent and stays cycle-free (D-06)."
  - "envutil kept minimal (IntDefault + BoolDefault only): the string/slice variants have no cross-package copy to fold, and pulling agent-tool knob reads to construction time is QUAL-04 (a later phase), explicitly OUT."

requirements-completed: []  # QUAL-03 partial — left to the orchestrator/verifier. 32-06/07/08 carry the remaining QUAL-03 items.

coverage:
  - id: T1
    description: "internal/neostore extracted; reasoningstore/toolselectstore/activelearn use neostore.* with their hashText/hashQuery/asString/asFloats/GraphClient copies deleted; parity (sha256 vectors, AsString fallback, AsFloats nil-vs-empty) green pre- and post-move."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "go test -race -cover ./internal/neostore/ → 100.0%"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/reasoningstore/ ./internal/toolselectstore/ ./internal/activelearn/ (green)"
        status: pass
      - kind: other
        ref: "grep -rE 'func (hashText|hashQuery|asString|asFloats)' internal/reasoningstore internal/toolselectstore internal/activelearn → NONE"
        status: pass
    human_judgment: false
  - id: T2
    description: "internal/pgnumeric extracted (NOT internal/db — cycle); conversations/cachemetrics use pgnumeric.* with their numericFromFloat/floatFromNumeric copies + duplicated numericMaxCost/numericScale consts deleted; characterization (Int+Exp+err-presence, never the error string) green; no import cycle under db_integration."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "go test -race -cover ./internal/pgnumeric/ → 100.0%"
        status: pass
      - kind: integration
        ref: "go vet -tags db_integration ./internal/db/ ./internal/conversations/ ./internal/cachemetrics/ ./internal/pgnumeric/ (compiles — cycle resolved)"
        status: pass
      - kind: other
        ref: "grep -rE 'func (numericFromFloat|floatFromNumeric)' internal/conversations internal/cachemetrics → NONE"
        status: pass
    human_judgment: false
  - id: T3
    description: "internal/envutil extracted; config (35 call sites)/telegram/registry use envutil.* with the local IntDefault/BoolDefault copies deleted; t.Setenv parity table green; D-13 coverage-gate inclusion confirmed."
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "go test -race -cover ./internal/envutil/ → 100.0%"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/config/ ./internal/channels/ ./internal/channels/telegram/ (green; config 85.1%, channels 100%)"
        status: pass
      - kind: other
        ref: "grep -rE 'func envIntDefault|func envBoolDefault' internal/config internal/channels → NONE; coverage_gate.sh exclude list = db/sqlc|sandbox|agenttest|llm/client.go only (new leaves auto-included)"
        status: pass
    human_judgment: false

# Metrics
duration: ~2h (PC-crash resume + concurrent-Codex isolation)
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 05: Pure-Leaf Duplicated-Helper Extraction Summary

**Extracted the three pure-leaf duplicated helper families to canonical homes (QUAL-03), each test-first per D-09/D-10: `internal/neostore` (Neo4j store-helper seam + coercers), `internal/pgnumeric` (numeric(10,4) USD-cost conversions — a dedicated leaf, not `internal/db`, to avoid a `db↔cachemetrics` test cycle), and `internal/envutil` (silent-fallback env parsing). All call sites migrated, every old copy deleted, parity tests green pre- and post-move, and each new leaf at 100% coverage.**

## Context: PC-crash resume

This plan was resumed after a **PC crash mid-execution**. Task 1 (`internal/neostore`) was already complete-but-uncommitted in the working tree when the session started; it was verified green (100% cov, `-race`, no leftover copies) and committed before continuing with Tasks 2 and 3. A parallel Codex session was committing unrelated `internal/objectstore`/document work to `master` throughout — every commit here used explicit-pathspec `git commit -o --` to include only this plan's files (see Deviation 1).

## Accomplishments

- **Task 1 — `internal/neostore`:** stdlib-only leaf exporting `HashText` (sha256→hex content-MERGE key), `AsString`, `AsFloats` (APOC-JSON-string + `[]any` decode, with the load-bearing nil-vs-empty distinction), and the `GraphClient` Cypher seam. `reasoningstore`/`toolselectstore` dropped their `hashText`/`hashQuery`/`asString`/`asFloats`/`GraphClient` copies; `activelearn` uses `neostore.HashText`. `*knowledge.Client` already satisfies `neostore.GraphClient` structurally. **100% coverage.**
- **Task 2 — `internal/pgnumeric`:** stdlib+pgtype leaf exporting `NumericFromFloat`/`FloatFromNumeric`/`DefaultNumericMaxCost`. `conversations` and `cachemetrics` dropped their byte-identical `numericFromFloat`/`floatFromNumeric` copies and the duplicated `numericMaxCost`/`numericScale` consts. The characterization table asserts the `pgtype.Numeric` `Int`+`Exp`+err-presence (never the error string — the two copies differed only there, Pitfall 5) and includes the `Float64Value`-overflow→0 read-boundary guard re-homed from cachemetrics. **100% coverage.**
- **Task 3 — `internal/envutil`:** stdlib leaf exporting `IntDefault`/`BoolDefault`. `config` migrated its 35 call sites and deleted `envIntDefault`/`envBoolDefault`; `telegram/config.go` dropped its self-documented `envIntDefault` copy; `registry.envChannelEnabled` keeps building `AURA_CHANNEL_<UP>_ENABLED` but delegates the parse to `envutil.BoolDefault(key, true)`. Kept minimal — string/slice and agent-tool knobs are out of scope (QUAL-04). **100% coverage.**

## Task Commits

Each extraction committed atomically (D-11), direct `git commit` with explicit pathspec:

1. **Task 1 — neostore extraction + 3 call-site migrations** — `bdf0ac46` (see Deviation 1: the crash-survivor staged files were absorbed into a concurrent Codex commit titled `feat: list object store contents`; the neostore code is byte-correct and verified, just co-located with unrelated objectstore files in that commit).
2. **Task 2 — pgnumeric extraction + conversations/cachemetrics migration** — `0d77fe73` (refactor).
3. **Task 3 — envutil extraction + config/telegram/registry migration** — `6b32a965` (refactor).

## Decisions Made

- **Numeric home = `internal/pgnumeric`, not `internal/db` (Open Question #1 revisited).** The plan's first instinct (`internal/db.NumericFromFloat`) creates an import cycle **in the test build**: `internal/db/cache_metrics_integration_test.go` is `package db` and imports `internal/cachemetrics`, so making `cachemetrics` import `internal/db` yields `db→cachemetrics→db` (caught immediately by `go vet -tags db_integration`). Converting that one test to `package db_test` was rejected — `envOrSkip`/`bootstrapURL` are `package db` helpers shared by 6+ integration test files. A dedicated pg-flavoured **leaf** is the clean resolution: it preserves the "Postgres home, NOT neostore" intent and is cycle-free because it imports nothing internal.
- **envutil minimal:** only `IntDefault`/`BoolDefault` (the two with cross-package copies). `envDefault`/`envSliceDefault` stay in `config` (no copies elsewhere); agent-tool knob construction-time reads remain `os.Getenv` (QUAL-04, OUT).

## Deviations from Plan

**1. [Concurrency] Task 1's commit was absorbed by a parallel Codex whole-index commit**
- **Found during:** Task 1 commit, after the PC-crash resume.
- **Issue:** A concurrent Codex session commits the *entire shared git index*. While this session's pre-commit file-size hook ran (~47s), Codex ran `git add internal/objectstore/ && git commit`, sweeping this session's already-`git add`-ed neostore files into Codex's commit `bdf0ac46 "feat: list object store contents"`, and the subsequent HEAD-ref update raced.
- **Fix:** Confirmed the neostore code committed correctly (verified 100% cov / `-race` / no leftover copies). For Tasks 2 and 3, switched to `git commit -o -F - -- <paths>` (`--only` mode, no pre-`git add` sweep window) so only this plan's files commit — both landed cleanly (`0d77fe73`, `6b32a965`). User directive: "stay on master, tolerate mix."
- **Impact:** Task 1's neostore extraction lives in `bdf0ac46` (bundled with unrelated objectstore work) rather than its own `refactor(32-05)` commit. Behaviour and correctness are unaffected.

**2. [Architecture — home change] numeric leaf is `internal/pgnumeric`, not `internal/db/numeric.go`**
- **Found during:** Task 2 (`go vet -tags db_integration`).
- **Issue:** The plan's `files_modified` names `internal/db/numeric.go` with exports on `package db`; that forms a `db↔cachemetrics` test cycle (see Decisions).
- **Fix:** Created `internal/pgnumeric` leaf; `internal/db/numeric.go` was created then removed and `internal/db/db_unit_test.go` reverted; call sites use `pgnumeric.*`. `conversations/store_append.go` keeps its pre-existing `internal/db` import (for `db.WithTx`).
- **Verification:** `go vet -tags db_integration` over db/conversations/cachemetrics/pgnumeric compiles (cycle gone); pgnumeric 100%.

**3. [Scope — necessary caller/test files] more files than `files_modified` listed**
- **Issue:** Deleting the unexported copies (required by the acceptance `rg` checks) forces updating every caller and the package-local tests that exercised them. Beyond the plan's list this touched `store_append.go`, `store.go` (production callers), and the `*_test.go` fixtures (`store_unit_test.go`, `store_sequence_test.go`, `store_helpers_test.go`, `config_test.go`); the redundant package-local numeric/env characterization tests were moved into the new leaves.
- **Impact:** None on scope/behaviour — mechanical call-site migration; the behavior + its characterization test live together in the new leaf.

---
**Total deviations:** 3 (1 concurrency/commit-bundling, 1 architecture home-change forced by a test cycle, 1 necessary-files expansion).

## Issues Encountered

- **Concurrent Codex on master:** whole-index commits + HEAD races (Deviation 1). Mitigated with `--only` pathspec commits; objectstore/document files were never staged or touched by this session.
- **`gofmt` not on PATH in WSL:** lives in `$(go env GOROOT)/bin`, not `~/go/bin` — invoked by full path.
- **`telegram` untagged coverage 83.6%:** below the 85% floor on the *untagged* unit tier only (the full `db_integration`/live matrix adds the telegram integration tier). Not a new leaf and not a regression from this plan (the deleted `envIntDefault` was a 10-line covered helper); deferred to the phase-level full-matrix coverage gate.

## User Setup Required

None — behaviour-preserving internal refactor. No env, schema, or external-service changes.

## Next Phase Readiness

- **QUAL-03 pure-leaf families are extracted.** Remaining QUAL-03 items are same-package extractions in **32-06** (`internal/canonicaljson` + agent retry/stream-retry) and **32-07** (`internal/agentrender`), plus the frontend dedup in **32-08**.
- New leaves `internal/neostore`, `internal/pgnumeric`, `internal/envutil` are auto-included by `scripts/coverage_gate.sh` (D-13 confirmed — exclude list is `db/sqlc|sandbox|agenttest|llm/client.go` only).

## Self-Check: PASSED

- FOUND: internal/neostore/{neostore.go,neostore_test.go} (100% cov)
- FOUND: internal/pgnumeric/{pgnumeric.go,pgnumeric_test.go} (100% cov, NOT internal/db — cycle)
- FOUND: internal/envutil/{envutil.go,envutil_test.go} (100% cov)
- FOUND: reasoningstore/toolselectstore/activelearn migrated to neostore.* (copies deleted)
- FOUND: conversations/cachemetrics migrated to pgnumeric.* (copies + dup consts deleted)
- FOUND: config/telegram/registry migrated to envutil.* (copies deleted)
- FOUND commit: bdf0ac46 (Task 1 neostore — bundled, see Deviation 1)
- FOUND commit: 0d77fe73 (Task 2 pgnumeric)
- FOUND commit: 6b32a965 (Task 3 envutil)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
