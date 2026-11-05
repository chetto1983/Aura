# Completed broker sessions and snapshots implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans task by task.
> The operator prioritized this follow-up; implement the specified high-priority data scope.

**Goal:** Make daily data selection consistent and let reports reuse one immutable full-OHLCV dataset.

**Architecture:** One common session policy in data acquisition, one explicit market_snapshot
tool reusing existing audit storage, and provider-free replay in the existing reports.

**Tech Stack:** Existing Python/MCP/Pydantic/pandas/yfinance/uv and Aura plugin runtime.

**Spec:** `docs/superpowers/specs/2026-11-03-broker-complete-sessions-snapshots-design.md`

## Global Constraints

- Work in the existing broker main branch; preserve unrelated work and old public fields.
- No new cache manager/protocol/calendar dependency, migration daemon or indicator.
- Future bars never enter; replay never fetches; completeness stays frozen at acquisition.
- Retain lazy discovery and truthful tool effects; preserve old archives before VM upgrade.

## Review Focus

- A closed session today differs from a still-open daily bar; Yahoo can name the next session.
- Crypto's provider end at 23:59 differs from its next-midnight boundary.
- Snapshot policy/period defaults and missing benchmark/FX must not silently fetch or refilter.
- Omitted report policy inherits the snapshot; explicitly conflicting values fail.
- ETF profile and directional comparison/portfolio FX coverage must be captured up front.
- Exact integer volume, GBP normalization, FX alignment and immutable-source provenance.
- Explicit home override wins; an audit read creates nothing; old archives survive upgrade.

### Task 1: Shared completed-session policy

**Files:** broker `data.py`, `models.py`, `analysis.py`, `workbench.py`, `portfolio.py`,
`hooks.py`, `server.py` and focused tests.
**Interfaces:** history/FX acquisition takes the explicit policy; reports carry session metadata.

- [x] Write failing fixtures for the session matrix and same-frame report disagreement.
- [x] Implement shared selection using existing metadata keys without lazy provider fetches.
- [x] Propagate flags to reports/FX; preserve live hooks through explicit opt-in; remove UTC cutoff.
- [x] Run full offline tests/lint and commit only this coherent policy change.

### Task 2: Immutable snapshot acquisition and replay

**Files:** existing `audit.py`/`hooks.py`, a focused snapshot module, server/models/report loaders,
and tests. The new module owns serialization/coverage validation, not a second storage engine.
**Interfaces:** market_snapshot returns a typed object hash; reports consume it without provider I/O.

- [x] Write failing storage-precedence, exact-roundtrip, no-network replay and conflict tests.
- [x] Reuse audit selectors/put/read; add the explicit acquisition tool and report snapshot inputs.
- [x] Test same-snapshot standard/custom and SPY invariants, archive integrity and 15-tool lazy discovery.
- [x] Run full offline tests/lint/build and commit the dataset/replay contract.

### Task 3: Review and real Aura acceptance

**Files:** release metadata/launchers/docs and dated verification artifacts.
**Interfaces:** immutable source pins and existing live plugin upgrade path.

- [x] Fresh review, one TDD fix pass for Important/Critical findings, then full offline suite.
- [x] Preserve old-home files into PLUGIN_DATA with verified collision checks before upgrading.
- [x] Pin tested source, push/check CI, upgrade plugin through Governance, verify actual version/tools.
- [x] Run real acquisition/replay across MCP sessions with provider forbidden, cross-tool agreement,
  current-session exclusion/opt-in and frozen metadata; record measured limits and results.

## Functional follow-up

### Task 4: Associate historical bars with captured provider schedules

**Files:** broker data/snapshot modules and session/snapshot regressions.

- [ ] Reproduce the November 4/5 mismatch with a dated provider-schedule fixture; watch RED.
- [ ] Fetch yfinance's existing lazy schedule only for an unmatched latest non-crypto bar;
  persist dated evidence, use the selected session end and retain conservative missing-data behavior.
- [ ] Validate new schedule shape/time/completion on replay; retain exact old snapshot replay.
- [ ] Run full offline suite/lint and commit the coherent session fix.

### Task 5: Separate numeric analysis from chart constraints and improve diagnostics

**Files:** broker custom/workbench/charts, analysis/audit and focused regressions.

- [ ] Watch failing regressions for chart=false with five lower groups, SMA1000 warmup,
  duplicate same-source warnings and path-free missing/unreadable record errors.
- [ ] Enforce pane limits only for charts, preserve all other validation and emit accurate
  insufficient-history warnings; sanitize audit errors and keep integrity failures explicit.
- [ ] Run full offline suite/lint/build and commit the focused fixes.

### Task 6: Review and release the measured functional fixes

**Files:** version/pins/release docs and this plan's dated verification record.

- [ ] Fresh review of tasks 4-5, TDD fix pass for Important/Critical findings, full suite.
- [ ] Build/check CI, upgrade through Governance and check actual installed version/readiness.
- [ ] Re-run old immutable snapshot, new dated acquisition, numerical-only panel/warmup/error
  scenarios, provider-forbidden replay and an actual Aura agent completion; record limits.

## Recorded decisions

- Explicit acquisition keeps ordinary market reads read-only; snapshots are additive local writes.
- Existing live hooks retain their explicit intraday behavior; report defaults become completed-only.
- Unmatched latest session metadata excludes that candidate with an explanation, even for the
  measured old Tokyo bar; this conservative policy can lag one row.
- Reuse audit.root's pure path semantics and hooks.home's existing mkdir wrapper.
- Baseline source 0.3.1 `edbb031`, packaging `0525e82`, current docs-only head `9f0cfa7`.
- Completed 0.4.0 source `831e466`, packaging `84d01b1`: 316 offline tests, Ruff/build/CI pass;
  actual Governance upgrade, 15 tools, cached start 3.79 s online / 2.33 s offline, four
  provider-forbidden replay processes and exact legacy readback. Real-agent completion and
  shared-ID ledger checks passed; see the dated verification record for limits.
