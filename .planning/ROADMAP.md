# Roadmap: Aura v4.1 Codebase Cleanup

**Milestone:** v4.1 Codebase Cleanup (Phase-CLEAN)
**Defined:** 2026-05-27
**Source plan:** `docs/phase-clean-plan-2026-05-27.md` (1031 LOC, locked 2026-05-27)
**Execution shape:** Codex one commit at a time (`codex exec --skip-git-repo-check --sandbox workspace-write "<prompt>"`) OR Claude inline. Atomic per-story; deep-refactor-on-touch mandatory per CLAUDE.md.

## Phase Sequence (Locked)

The plan §1 locks this order — **do not reorder**:

```
Phase 1  →  Phase 2  →  Phase 3  →  Phase 4  →  Phase 5  →  Phase 6  →  Phase 7
  W0        W1 (9)       W4 (1)      W6 (2)      W2 (12)     W3 (7)      W5 (12)
```

Rationale:
- W6 (CI hard-gate) runs **before** W2/W3 dupl folds so production cleanup commits are CI-protected from regression.
- W5 (test cleanup) runs **last** so fixture extractions are gate-protected by the same CI rails.

## Phase Overview

| # | Phase | Goal | Wave | Reqs | Status |
|---|-------|------|------|------|--------|
| 1 | Baselines & Guardrails | Capture pre-sweep state; wire `golangci-lint` + `dupl` into CI as **warnings** | W0 | 1 | Pending |
| 2 | Errcheck Production | Eliminate 50 errcheck findings across `internal/` and `cmd/` | W1 | 9 | Pending |
| 3 | Staticcheck | Kill 2 staticcheck QF1001 findings (De Morgan rewrites) | W4 | 1 | Pending |
| 4 | CI Hard-Gate | Flip CI lint + dupl from warning → fail; make `.golangci.yml` explicit | W6 | 2 | Pending |
| 5 | Cross-File Dupl | Fold 12 cross-file dupl clusters via shared helpers + substrate packages | W2 | 12 | Pending |
| 6 | Intra-File Dupl | Fold 7 intra-file dupl clusters (one with 5 sub-stories) | W3 | 7 | Pending |
| 7 | Test Cleanup | Fixture extraction across 12 test families; mainline post-hard-gate | W5 | 12 | Pending |

**Total:** 44 requirements across 7 phases. Coverage: 100% ✓ (all REQ-IDs traced to exactly one phase per REQUIREMENTS.md).

---

## Phase Details

### Phase 1: Baselines & Guardrails (Wave 0)

**Goal:** Capture the pre-sweep state in version control, then wire `golangci-lint` and `dupl` into CI as warning steps so regressions during the sweep are visible without breaking the build.

**Requirements:** CLEAN-00

**Plans:** 1 plan
- [ ] 01-01-PLAN.md — Baseline JSON + CI golangci-lint/dupl warning steps (one atomic commit, files: docs/cleanup-baseline-2026-05-27.json + .github/workflows/ci.yml)

**Success criteria:**
1. `docs/cleanup-baseline-2026-05-27.json` exists with `deadcode`, `golangci_lint`, and `dupl` sections matching the current snapshot.
2. CI runs show two new green steps (golangci-lint + dupl) with `delta: 0` printout on the seed commit.
3. No existing CI step is reordered or weakened.
4. Lefthook pre-commit untouched (already runs both).

---

### Phase 2: Errcheck Production (Wave 1)

**Goal:** Eliminate all 50 errcheck findings across `internal/` and `cmd/` so silent failure modes (corrupted partial downloads, swallowed rollback errors, dropped logs, leaked file handles) become impossible at the type-checker level.

**Requirements:** CLEAN-01, CLEAN-02, CLEAN-03, CLEAN-04, CLEAN-05, CLEAN-06, CLEAN-07, CLEAN-08, CLEAN-09

**Success criteria:**
1. `golangci-lint run --enable-only=errcheck ./...` reports 0 findings in production code (`internal/` + `cmd/`).
2. Every touched file passes the per-slice verify gate (`go vet` + `go build` + package tests + `golangci-lint` + `dupl -t 60`).
3. No silent failure regressions — observable failures (rollback, log rotation) use `slog.Warn`; defer-Close on resources that survive the function use `_ = x.Close()`.
4. Each story shipped as exactly one atomic commit (no batching outside Wave 5).
5. Deep-refactor-on-touch applied — touched files have stale comments removed, headers re-read, no dead code left behind.

---

### Phase 3: Staticcheck (Wave 4)

**Goal:** Kill the 2 outstanding staticcheck QF1001 findings via De Morgan rewrites. Trivial mechanical fix, single commit.

**Requirements:** CLEAN-29

**Success criteria:**
1. `golangci-lint run --enable-only=staticcheck ./...` reports 0 QF1001 findings.
2. `go test ./internal/agent/... ./internal/storage/memoryindex/...` green (semantic preservation verified — De Morgan transform must not flip intent).
3. Both rewrites read with the surrounding ±3 lines preserved before commit.

---

### Phase 4: CI Hard-Gate (Wave 6)

**Goal:** Promote the Wave 0 warning steps to hard-fail status now that production lint is clean. Make `.golangci.yml` explicit about which linters are required, removing reliance on v2 defaults. After this phase, "no new lint findings" is mechanically enforced — Wave 2 / 3 / 5 commits land under CI protection.

**Requirements:** CLEAN-50, CLEAN-51

**Success criteria:**
1. `.github/workflows/ci.yml` runs `golangci-lint run --new-from-rev=HEAD~1 ./...` and `dupl -t 60 ./cmd ./internal | diff - docs/dupl-baseline.txt` as **fail-on-delta** steps.
2. `docs/dupl-baseline.txt` exists with the post-Wave-1-and-4 state.
3. A test commit introducing a new errcheck finding fails CI (verified once before merge).
4. `.golangci.yml` v2 config enables `depguard`, `errcheck`, `staticcheck`, `unused`, `ineffassign`, `govet` explicitly.
5. Stale `_archive_phaseG_dead_dispatch` reference removed from `.golangci.yml` exclusions if the directory no longer exists.

---

### Phase 5: Cross-File Dupl Production (Wave 2)

**Goal:** Fold all 12 cross-file production dupl clusters into shared helpers, creating new substrate packages where depguard requires it (`internal/storage/sqlitex/`, `internal/sliceutil/`, `cmd/internal/debugio/`, `internal/wiki/graph_helpers.go`). Largest single-cluster ROI: CLEAN-10 (wiki 6-way).

**Requirements:** CLEAN-10, CLEAN-11, CLEAN-12, CLEAN-13, CLEAN-14, CLEAN-15, CLEAN-16, CLEAN-17, CLEAN-18, CLEAN-19, CLEAN-20, CLEAN-21

**Success criteria:**
1. `dupl -t 60 ./cmd ./internal` reports 0 cross-file production clusters from the source-plan inventory.
2. Each new helper package passes `golangci-lint run --enable-only=depguard` — substrate packages (`sqlitex`, `sliceutil`) reachable from all callers; domain helpers placed at the depguard-permitted callee.
3. Per-story acceptance: `go test ./internal/<touched-pkg>/...` green AND `dupl -t 60 <touched files>` reports 0 cluster from that story.
4. LLM-visible behavior preserved — for CLEAN-10 (wiki graph helpers), the wiki page render is byte-stable across the change; verified via `cmd/probe_chat` or equivalent.
5. Deep-refactor-on-touch applied — touched files have stale comments removed and stay ≤600 LOC (no new `.file-size-baseline.txt` entries).
6. Each story shipped as exactly one atomic commit per rule C1.

---

### Phase 6: Intra-File Dupl Production (Wave 3)

**Goal:** Fold the 7 remaining intra-file production dupl clusters. CLEAN-27 is itself 5 sub-stories (one commit each per rule C1). Special care on CLEAN-23 (`tool_definitions.go`) — LLM-visible JSON Schema manifest must be byte-identical post-fold.

**Requirements:** CLEAN-22, CLEAN-23, CLEAN-24, CLEAN-25, CLEAN-26, CLEAN-27 (5 sub-stories: 27a..27e), CLEAN-28

**Success criteria:**
1. `dupl -t 60 ./cmd ./internal` reports 0 intra-file production clusters from the source-plan inventory.
2. CLEAN-23: tool-manifest JSON Schema is byte-identical before/after (verified via probe).
3. Per-story acceptance: `go test ./internal/<pkg>/...` green AND `dupl -t 60 <touched file>` clean for that intra-file cluster.
4. CLEAN-20 (`internal/telegram/documents.go`) stays ≤600 LOC after fold; split into `documents.go` + `documents_audio.go` if needed.
5. Each story (and each CLEAN-27 sub-story) shipped as exactly one atomic commit.

---

### Phase 7: Test Cleanup (Wave 5, mainline post-hard-gate)

**Goal:** With CI hard-gate (Phase 4) in place, lift test fixture extraction into a mainline phase. Mechanical errcheck sweep first (CLEAN-30, batching exception), then 8 fixture-family extractions (CLEAN-31..38), with 3 reserved overflow slots (CLEAN-39..41).

**Requirements:** CLEAN-30, CLEAN-31, CLEAN-32, CLEAN-33, CLEAN-34, CLEAN-35, CLEAN-36, CLEAN-37, CLEAN-38, CLEAN-39, CLEAN-40, CLEAN-41

**Success criteria:**
1. `golangci-lint run --enable-only=errcheck ./...` reports 0 findings repo-wide (production + tests).
2. `dupl -t 60` reports 0 test-cluster from the source-plan inventory families covered by CLEAN-31..38.
3. New `_test.go` fixture helpers (`openaifixture_test.go`, `documentsfixture_test.go`, etc.) live alongside the source they test; no production code touched.
4. `go test ./... -count=1 -race` green on amd64 Linux CI.
5. CLEAN-30 (batch sweep) shipped as one commit per rule C1's batched-sweep exception; CLEAN-31..38 each shipped as one atomic commit per family.
6. CLEAN-39..41 reserved for fallout — used only if a fixture extraction reveals an unmodeled test family.

---

## Next Step

`/clear` then:

`/gsd:discuss-phase 1` — gather phase 1 context and clarify approach.

Also: `/gsd:plan-phase 1` — skip discussion, plan directly from the source plan's CLEAN-00 story.

Phase 1 has only one requirement (CLEAN-00) and is purely additive — `discuss-phase` is optional. Direct plan-phase is the faster path.

---
*Roadmap defined: 2026-05-27 after milestone bootstrap.*
