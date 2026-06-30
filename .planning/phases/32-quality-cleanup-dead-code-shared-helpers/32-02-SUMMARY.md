---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 02
subsystem: testing
tags: [dead-code, deadcode, reinvented-stdlib, agui, cmd-aura, rerank, assets, qual-02]

# Dependency graph
requires:
  - phase: 31-stabilization
    provides: quality-audit findings (QA-C-10 reinvented-stdlib, QA-C-13 truncateRunes dup, RequestID re-stamp) → QUAL-02
provides:
  - "Load-bearing regression test pinning the cmd/aura dry-run RequestID re-stamp (KEEP verdict, T1)"
  - "agui governance_api stdlib-based env-chip parsing + inlined non-nil NetworkAllowlist copy (indexByte/stringList deleted, T3/T4)"
  - "Recorded accept-vs-fold resolution for the truncateRunes 5-line dup (T8, OQ#2 default)"
affects: [future deadcode/audit runs, 32-03+ shared-helper extractions, agui cockpit NetworkAllowlist contract]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Load-bearing proof: pin a removal-candidate line with a regression test, then verify RED by temporarily deleting it (never committed)"
    - "Reinvented-stdlib swap preserving wire contract: inline append([]string{}, ...) keeps an empty slice non-nil so JSON stays [] not null"
    - "Accept-with-cross-reference: document a deliberately-retained near-duplicate in-place instead of extracting a coverage-gated micro-package"

key-files:
  created:
    - internal/agui/governance_api_mcp_test.go
    - .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-02-SUMMARY.md
  modified:
    - cmd/aura/agent.go
    - cmd/aura/agent_test.go
    - internal/agui/governance_api.go
    - internal/assets/context.go
    - internal/rerank/client.go

key-decisions:
  - "T1 KEEP: cmd/aura/agent.go RequestID re-stamp is load-bearing on the dry-run path (the fake InfiniteToolCallAgent never stamps RequestID); proven by a regression test + RED-on-removal, not deleted"
  - "T3/T4 SWAP: replace agui.indexByte with strings.IndexByte and inline agui.stringList as append([]string{}, ...); both helpers deleted, non-nil-empty allowlist contract preserved (Pitfall 4)"
  - "T8 ACCEPT: keep the truncateRunes 5-line dup with a cross-reference comment on each copy; no internal/strutil created (OQ#2 default)"

patterns-established:
  - "RED-on-removal load-bearing verification for KEEP verdicts"
  - "Sibling test file (governance_api_mcp_test.go) to honor the 600-LOC cap rather than grow a test file past it"

requirements-completed: []

coverage:
  - id: D1
    description: "cmd/aura dry-run stamps a non-zero uniform RequestID on EVERY event; removing agent.go:134 emits the zero UUID (load-bearing KEEP, T1)"
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "go test -race -run Dry ./cmd/aura/ (WSL, exit 0); TestDryRun_EveryEventCarriesRequestID_LoadBearing"
        status: pass
      - kind: other
        ref: "RED verified by temporarily deleting the re-stamp → 'line 0 carries the zero RequestID 00000000-...' (removal NOT committed)"
        status: pass
  - id: D2
    description: "agui envChips uses strings.IndexByte and NetworkAllowlist inlines append([]string{}, ...); both reinvented-stdlib helpers deleted; empty allowlist marshals [] not null (T3/T4)"
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "go test -race ./internal/agui/ (WSL, exit 0); TestEnvChips_KeyExtractionAcrossUnionCases + TestGovernanceMCPEmptyAllowlistIsArrayNotNull"
        status: pass
      - kind: other
        ref: "deadcode -test ./internal/agui/ — 3 deterministic runs, 0 matches for indexByte/stringList; rg 'func indexByte|func stringList' returns nothing; strings.IndexByte present"
        status: pass
  - id: D3
    description: "truncateRunes dup resolved by documented ACCEPT; both copies carry a cross-reference comment; no internal/strutil package created (T8)"
    requirement: "QUAL-02"
    verification:
      - kind: other
        ref: "go build ./internal/assets/ ./internal/rerank/ (WSL, exit 0); cross-reference comment on both copies; internal/strutil absent"
        status: pass
    human_judgment: false

# Metrics
duration: ~40min
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 02: QUAL-02 Stdlib Swaps + Keeps Summary

**The two named QUAL-02 KEEPs are protected and the reinvented-stdlib is gone: the cmd/aura dry-run `RequestID` re-stamp is pinned load-bearing by a regression test (proven RED on removal); `agui.indexByte`/`stringList` are deleted in favor of `strings.IndexByte` + an inlined non-nil `append([]string{}, ...)` (empty allowlist stays `[]`, never `null`); and the `truncateRunes` 5-line dup is resolved by an explicit ACCEPT with cross-reference comments — no `internal/strutil`, all packages race-green.**

## Performance

- **Duration:** ~40 min (3 tasks; extended by a concurrent parallel session — see Issues)
- **Completed:** 2026-06-30T09:17:21+02:00 (final task commit)
- **Tasks:** 3 of 3 executed
- **Files:** 5 modified, 1 created (`internal/agui/governance_api_mcp_test.go`)

## What Was Built

### Task 1 — RequestID re-stamp is load-bearing (KEEP, T1) — commit `e35f0aeb`

The QUAL-02 triage initially listed `cmd/aura/agent.go`'s `ev.RequestID = requestID` as a
removal candidate, then flipped it to load-bearing. The dry-run drives the fake
`agenttest.InfiniteToolCallAgent`, whose `Run` builds step events with **no** `RequestID`
(a real `LlmAgent.newEvent` copies `ic.RequestID`; the fake does not), and the LoopAgent's
`scopeToToolCall` returns those single-tool step events unchanged. Only the re-stamp puts
the uniform run id on the N step events — the LoopAgent terminal event already carries
`ic.RequestID`. Added `TestDryRun_EveryEventCarriesRequestID_LoadBearing` asserting every
emitted event carries the supplied `requestID` and never the zero UUID, and clarified the
in-code comment to record the KEEP rationale. **Load-bearing proven**: temporarily deleting
the line turned the test RED with `line 0 carries the zero RequestID 00000000-0000-0000-0000-000000000000`;
the line was immediately restored and the removal was never committed.

### Task 2 — agui reinvented-stdlib swap (T3/T4) — commit `99e48231`

Replaced `indexByte(entry, '=')` with `strings.IndexByte(entry, '=')` in `envChips` and
deleted the local `indexByte` helper; inlined `stringList(server.Runtime.Network)` as
`append([]string{}, server.Runtime.Network...)` at the `NetworkAllowlist` assignment and
deleted `stringList`. The `[]string{}` literal is non-nil **by design** (Pitfall 4 /
T-32-02-AL) so an empty allowlist marshals `"networkAllowlist":[]`, never `null`, to the
cockpit. Added two characterization tests (env-chip key extraction across the
`k=v` / secret / `novalue` / `"="` / `""` union; empty-allowlist → `[]`), confirmed green
**before and after** the swap. `deadcode -test ./internal/agui/` reports zero
`indexByte`/`stringList` across 3 deterministic runs.

### Task 3 — truncateRunes dup ACCEPT (T8) — commit `2c9d6385`

Applied the planner default (OQ#2): **ACCEPT** the 5-line `truncateRunes` duplication with a
cross-reference comment on each copy instead of extracting `internal/strutil`. The two copies
are **near-duplicates with a deliberate behavioral difference** — `internal/assets/context.go`
appends `"..."` for human-readable summaries, `internal/rerank/client.go` trims **without**
an ellipsis for the sidecar wire body — so a naive fold would be wrong without a variant
parameter, and a new package for a 5-liner would itself need coverage-gate registration. No
`internal/strutil` created.

## Decisions Made

- **T1 KEEP** — re-stamp retained, pinned by a load-bearing regression test (RED-on-removal verified).
- **T3/T4 SWAP** — `strings.IndexByte` + inline `append([]string{}, ...)`; non-nil-empty allowlist preserved.
- **T8 ACCEPT** — documented dup, no micro-package. Chosen over folding because the copies differ behaviorally and a 5-liner util would add coverage-gate overhead for negative net value.

## Deviations from Plan

**1. [refactor-on-touch / file-size cap] New sibling test file instead of extending `governance_api_test.go`**
- **Found during:** Task 2 (first commit attempt)
- **Issue:** Adding the two new agui tests inline pushed `internal/agui/governance_api_test.go` to 644 LOC, tripping the pre-commit file-size hook (CLAUDE.md "NO GOD CLASS >600 LOC; refactor on touch").
- **Fix:** Moved the two new tests into a new same-package file `internal/agui/governance_api_mcp_test.go` (88 LOC), reusing the existing fixtures (`govServer`, `doGov`, `scriptedMCPBoard`, `boolPtr`). `governance_api_test.go` ended **unchanged from HEAD** (573 LOC).
- **Net effect:** The plan named `internal/agui/governance_api_test.go` as the test home; the equivalent tests live in `governance_api_mcp_test.go` instead. Behavior and coverage are identical; the file-size invariant is honored.
- **Commit:** `99e48231`

Otherwise the plan executed as written. The three prohibitions were respected: line 134 (re-stamp) kept; the allowlist copy stays `append([]string{}, ...)` (non-nil); MCP trust-normalization and `decode*Body` strict-decode were not touched.

## Issues Encountered

**Concurrent parallel session (non-blocking, no scope change).** A parallel Codex session was
committing a document-ingestion feature on the same branch throughout this plan:
- **Ref-lock race:** the Task 2 commit's pre-commit hook (~45s) overlapped a parallel commit
  (`da162585`), so the ref update failed (`cannot lock ref 'HEAD'`). Verified Task 1
  (`e35f0aeb`) was still an ancestor of HEAD (nothing orphaned — the parallel work landed on
  top of it), then retried the commit successfully.
- **Whole-repo vet hook vs. parallel WIP:** the lefthook `vet` step runs `go vet ./...`, which
  failed on the parallel session's incomplete `internal/documents/` (undefined `NormalizeTags`,
  then `CreateDocumentRequest`). This is **out of scope** (SCOPE BOUNDARY — not caused by this
  plan's changes). Resolved by **polling for a green window** and retrying — never by editing the
  parallel files and never with `--no-verify`. My staged set stayed strictly scoped to this
  plan's files throughout (verified via `git diff --cached --stat` before every commit).

**Flaky deadcode one-liner (resolved).** The plan's `deadcode … | grep … ; test $? -ne 0`
one-liner reported a spurious match once (a shell/cached-build artifact over the 1400-line
whole-closure output). Re-ran deterministically 3× capturing to file: **0 matches** for
`indexByte`/`stringList` every run; the file-scoped `rg 'func indexByte|func stringList'` is
the authoritative, deterministic check and returns nothing.

## Threat Model Disposition

- **T-32-02-AL (Tampering/Info — NetworkAllowlist marshaling):** *mitigated.* The inline keeps
  `append([]string{}, ...)` (non-nil), and `TestGovernanceMCPEmptyAllowlistIsArrayNotNull`
  asserts an empty `Runtime.Network` marshals `[]`, never `null`.
- **T-32-02-DEF (deferred MCP trust-norm / decode*Body):** *accept / untouched* (QA-C-03→38,
  QA-C-01→38/40) — flagged prohibition honored.
- **T-32-SC (package installs):** *accept* — no installs this plan.

No new threat surface introduced (no new endpoints, auth paths, file access, or schema changes;
the agui change is a behavior-preserving serialization of an existing field).

## Known Stubs

None. All changes are tests, a code comment, and a behavior-preserving stdlib swap — no
hardcoded empty values, placeholders, or unwired data paths introduced.

## Verification

- `go test -race ./cmd/aura/ ./internal/agui/ ./internal/assets/ ./internal/rerank/` — all `ok` (WSL).
- `go test -race -run Dry ./cmd/aura/` — exit 0; load-bearing test present and RED-verified on removal.
- `go test -race ./internal/agui/` — exit 0; envChips union + empty-allowlist→`[]` tests pass.
- `deadcode -test ./internal/agui/` — 0 `indexByte`/`stringList` (3 deterministic runs).
- `rg 'func indexByte|func stringList' internal/agui/governance_api.go` — nothing; `strings.IndexByte` + `append([]string{}` present.
- `go build ./internal/assets/ ./internal/rerank/` — exit 0; both `truncateRunes` copies carry a cross-reference comment; `internal/strutil` absent.

## Task Commits

1. **Task 1** — `e35f0aeb` `test(32-02): pin dry-run RequestID re-stamp as load-bearing (QUAL-02 KEEP)`
2. **Task 2** — `99e48231` `refactor(32-02): replace agui reinvented-stdlib indexByte/stringList (QUAL-02)`
3. **Task 3** — `2c9d6385` `docs(32-02): record truncateRunes dup resolution as accept (QUAL-02 T8)`

## Next Phase Readiness

- The two named QUAL-02 KEEPs (RequestID re-stamp, allowlist contract) are now test-pinned, so
  later deadcode/audit runs treat them as known-protected rather than re-flagging.
- `agui.indexByte`/`stringList` removed; ROADMAP C1 reinvented-stdlib item advanced.
- QUAL-02 is shared across plans 32-01 … 32-10; it is **not** marked complete by this plan
  (other reinvented-stdlib/placeholder items remain in sibling plans).

## Self-Check: PASSED

- `cmd/aura/agent.go`, `cmd/aura/agent_test.go` — FOUND (re-stamp comment + load-bearing test)
- `internal/agui/governance_api.go`, `internal/agui/governance_api_mcp_test.go` — FOUND (stdlib swap + tests)
- `internal/assets/context.go`, `internal/rerank/client.go` — FOUND (cross-reference comments)
- `.planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-02-SUMMARY.md` — FOUND
- Task commits `e35f0aeb`, `99e48231`, `2c9d6385` — FOUND in git log

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
