# Phase 1: Baselines & Guardrails (Wave 0) - Context

**Gathered:** 2026-05-27
**Status:** Ready for planning
**Source:** PRD Express Path (`docs/phase-clean-plan-2026-05-27.md` §0 + §2)

<domain>
## Phase Boundary

This phase delivers Wave 0 of the v4.1 Codebase Cleanup milestone:

1. **A machine-readable baseline snapshot** (`docs/cleanup-baseline-2026-05-27.json`) capturing the pre-sweep state of three quality signals (deadcode, golangci-lint, dupl) so subsequent waves have a fixed reference point for regression detection.
2. **Two non-blocking CI warning steps** in `.github/workflows/ci.yml` that surface lint and dupl deltas during the cleanup sweep (Waves 1–5) without breaking the build. These are intentionally `continue-on-error: true`; Phase 4 (Wave 6) will later flip them to fail-on-delta.

The phase is **pure additive** — no Go source files are touched, no existing CI step is reordered or weakened, and Lefthook pre-commit is left alone (it already runs both linters locally).

</domain>

<decisions>
## Implementation Decisions

All decisions below are LOCKED in `docs/phase-clean-plan-2026-05-27.md` §0 (locked decisions table) and §2 (CLEAN-00 sketch). They are not open for re-litigation during planning.

### D-01 — Baseline file location and shape
- **File:** `docs/cleanup-baseline-2026-05-27.json` (new file, no existing baseline JSON to merge into).
- **Schema:** exactly three top-level keys —
  - `deadcode`: array of finding strings (currently empty per `docs/deadcode-baseline-2026-05-22.json` and the live `deadcode` CI step).
  - `golangci_lint`: object `{ "errcheck": 50, "staticcheck": 2, "total": 52 }`.
  - `dupl`: object `{ "production_cross_file": 15, "production_intra_file": 13, "test": 75, "total_clusters": 103 }`.
- **Counts are anchored to master `272579a2` (2026-05-27)** per source plan anchors. The planner must NOT re-run detection to "refresh" the counts — these numbers ARE the locked baseline; later phases prove their delta against them.
- Companion human-readable baselines already exist (`docs/deadcode-baseline-2026-05-22.json`, `docs/depguard-baseline-2026-05-22.md`); the new JSON is the single machine-readable anchor for the cleanup sweep.

### D-02 — CI integration: warning steps only (continue-on-error: true)
- Two new steps inserted in `.github/workflows/ci.yml` under the **`test` job** (not `deadcode`, not `frontend`), **after** the existing `Go vet` step (line 33–34) and **before** `Go build`. Placement after `Go vet` matches source plan §2.2 ("after `Go vet`, insert two **warning** steps").
- **Step 1: golangci-lint warning** — runs `golangci-lint run ./...` (full ruleset, not `--enable-only`), `tee /tmp/lint-current.txt`, then computes line-count delta against the `golangci_lint.total` baseline (52) and prints `delta: N`. `continue-on-error: true`.
- **Step 2: dupl warning** — runs `dupl -t 60 ./cmd ./internal`, `tee /tmp/dupl-current.txt`, extracts the trailing `Found total N clone groups.` line, compares N against `dupl.total_clusters` baseline (103), prints `delta: N`. `continue-on-error: true`.
- Both steps MUST emit `delta: 0` on the seed commit (the commit that lands the baseline + the workflow edits) because the baseline IS the snapshot of that commit.
- The dupl step needs the `dupl` binary on PATH — install via `go install github.com/mibk/dupl@latest` as the step's first sub-action (or a preceding install step). Mirror the install pattern used by the existing `deadcode` job (`go install golang.org/x/tools/cmd/deadcode@latest`) for consistency.

### D-03 — Invariants the planner MUST preserve
- **No existing CI step is reordered or weakened.** This includes `Depguard architecture boundary`, `File-size linter (600-LOC cap)`, `Go vet`, `Go build`, `Phase 2 regression guards`, `Run Go tests with race detector`, the entire `deadcode` job, and the entire `frontend` job.
- **Lefthook pre-commit is untouched.** Source plan §2.3 is explicit ("Do NOT touch lefthook (already runs both pre-commit)").
- **golangci-lint version pinning** — the existing depguard step pins `v2.12.2` via `golangci/golangci-lint-action@v8`. The new warning step SHOULD reuse the same `golangci/golangci-lint-action@v8` action and same `v2.12.2` version to avoid pulling a second toolchain. Use `args: ./...` (no `--enable-only=` filter) so all enabled linters in `.golangci.yml` run.

### D-04 — Atomicity and commit shape
- **Per CLAUDE.md C1 + source plan §0.3** — Phase 1 ships as **exactly one atomic commit**. The single commit MUST contain:
  - `docs/cleanup-baseline-2026-05-27.json` (new)
  - `.github/workflows/ci.yml` (edited — 2 new steps added)
- No accompanying refactor, no follow-up commits for "wiring", no separate baseline + CI commits. One slice = one commit (memory `feedback_one_module_per_slice.md`).

### D-05 — Verify gate before commit
Per source plan §2 "Verify command":
```bash
go vet ./...
go build ./...
golangci-lint run .github/workflows/ci.yml || true  # YAML lint not required
git diff --stat HEAD~1
```
The phase is pure additive (no Go changes), so `go vet` / `go build` should be no-ops; they are run anyway to prove the workflow edit did not accidentally break a Go file.

### D-06 — Deep-refactor-on-touch waiver for this slice
Source plan §2 CLEAN-00 "Deep-refactor checklist" explicitly states: **N/A — pure additive, no Go files touched.**
This is the **only** Phase-CLEAN slice exempt from the deep-refactor-on-touch rule (CLAUDE.md §Deep Refactor on Touch), because no Go module is touched. The planner must NOT inject deep-refactor tasks here.

### Claude's Discretion

The following implementation details are NOT locked by the PRD; the planner may choose the cleanest expression:

- **Bash one-liners for the delta computation** — exact shell syntax for computing `wc -l < /tmp/lint-current.txt` vs baseline 52, or `grep -oE 'Found total [0-9]+'` vs baseline 103. The source plan gives the semantics ("compare line count vs baseline; print delta only"); the planner picks the syntax.
- **JSON formatting** — pretty-printed (2-space indent) vs compact. Pretty-printed is recommended for human review during the cleanup sweep.
- **dupl install step placement** — inline as the first sub-command of the dupl warning step, OR as a separate preceding `Install dupl` step. Either is acceptable; separate step matches the existing `deadcode` job's "Install deadcode" pattern.
- **Step names** — exact display names for the two new steps in the GitHub Actions UI. Recommended: `Lint regression warning (golangci-lint)` and `Dupl regression warning (dupl -t 60)`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Source plan (the PRD)
- `docs/phase-clean-plan-2026-05-27.md` — full Phase-CLEAN locked plan; §0 = decisions table, §2 = CLEAN-00 sketch. This is the single source of truth for this phase.

### Existing baselines (input to the new JSON)
- `docs/deadcode-baseline-2026-05-22.json` — current deadcode finding list; the new `cleanup-baseline` JSON's `deadcode` array is a snapshot of this state.
- `docs/depguard-baseline-2026-05-22.md` — human-readable depguard baseline; informational only, not consumed by the new JSON.

### CI integration target
- `.github/workflows/ci.yml` — the file being edited. The new warning steps live in the `test` job, after `Go vet` (line 33–34) and before `Go build` (line 36–37). The existing `Depguard architecture boundary` step (line 24–28) and `File-size linter` step (line 30–31) demonstrate the warning-style action pattern to follow.

### Project rules
- `CLAUDE.md` §Behavioral Rules (C1 atomic commits, C2 deep-refactor-on-touch, C3 never modify tests to pass lint) — universal rules; only C1 applies to this slice (C2 waived by D-06, C3 not relevant).

### Companion gates (DO NOT MODIFY in this phase)
- `lefthook.yml` — locally runs `golangci-lint` + `dupl` pre-commit. Left alone per D-03.
- `.golangci.yml` — current linter config. Read-only here; Phase 4 (CLEAN-51) will make it explicit.

</canonical_refs>

<specifics>
## Specific Ideas

- **Exact baseline counts** (from source plan §2 step 1):
  - `golangci_lint.errcheck = 50`, `golangci_lint.staticcheck = 2`, `golangci_lint.total = 52`
  - `dupl.production_cross_file = 15`, `dupl.production_intra_file = 13`, `dupl.test = 75`, `dupl.total_clusters = 103`
  - `deadcode = []` (currently empty, per the live `deadcode` CI job's "0 findings, baseline: N" success path on master).
- **File anchor**: master `272579a2` (2026-05-27) per source plan anchors block.
- **Reference pattern for `continue-on-error: true` step** — none exist in current `ci.yml`; the planner introduces this idiom for the first time in the repo. GitHub Actions docs confirm `continue-on-error: true` at the step level allows the job to proceed even if the step exits non-zero.
- **Reference pattern for tee + delta computation** — the existing `deadcode` job (line 47–76) shows the canonical "run tool → tee to /tmp → parse → compare to baseline → printout" idiom. The new warning steps should mirror this shape for consistency, but SKIP the `exit 1` branch (warnings only).

</specifics>

<deferred>
## Deferred Ideas

Items explicitly out of scope for Phase 1, deferred to later phases per source plan §1:

- **Promotion of warning steps → fail-on-delta** — deferred to Phase 4 (CLEAN-50) per ROADMAP.
- **Making `.golangci.yml` explicit** about which linters are required (`errcheck`, `staticcheck`, `unused`, `ineffassign`, `govet` alongside the existing `depguard`) — deferred to Phase 4 (CLEAN-51).
- **A new `docs/dupl-baseline.txt` for diff-based dupl gating** — deferred to Phase 4 (CLEAN-50); the Phase 1 baseline is a numeric counts JSON, not a per-cluster text file.
- **Actual fixing of any errcheck / staticcheck / dupl findings** — Wave 1 / Wave 2 / Wave 3 / Wave 4 work (Phases 2, 3, 5, 6).

</deferred>

---

*Phase: 01-baselines-guardrails-wave-0*
*Context gathered: 2026-05-27 via PRD Express Path*
