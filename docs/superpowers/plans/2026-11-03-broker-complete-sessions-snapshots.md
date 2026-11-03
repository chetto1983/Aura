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

- [ ] Write failing fixtures for the session matrix and same-frame report disagreement.
- [ ] Implement shared selection using existing metadata keys without lazy provider fetches.
- [ ] Propagate flags to reports/FX; preserve live hooks through explicit opt-in; remove UTC cutoff.
- [ ] Run full offline tests/lint and commit only this coherent policy change.

### Task 2: Immutable snapshot acquisition and replay

**Files:** existing `audit.py`/`hooks.py`, a focused snapshot module, server/models/report loaders,
and tests. The new module owns serialization/coverage validation, not a second storage engine.
**Interfaces:** market_snapshot returns a typed object hash; reports consume it without provider I/O.

- [ ] Write failing storage-precedence, exact-roundtrip, no-network replay and conflict tests.
- [ ] Reuse audit selectors/put/read; add the explicit acquisition tool and report snapshot inputs.
- [ ] Test same-snapshot standard/custom and SPY invariants, archive integrity and 15-tool lazy discovery.
- [ ] Run full offline tests/lint/build and commit the dataset/replay contract.

### Task 3: Review and real Aura acceptance

**Files:** release metadata/launchers/docs and dated verification artifacts.
**Interfaces:** immutable source pins and existing live plugin upgrade path.

- [ ] Fresh review, one TDD fix pass for Important/Critical findings, then full offline suite.
- [ ] Preserve old-home files into PLUGIN_DATA with verified collision checks before upgrading.
- [ ] Pin tested source, push/check CI, upgrade plugin through Governance, verify actual version/tools.
- [ ] Run real acquisition/replay across MCP sessions with provider forbidden, cross-tool agreement,
  current-session exclusion/opt-in and frozen metadata; record measured limits and results.

## Decisions

- Explicit acquisition keeps ordinary market reads read-only; snapshots are additive local writes.
- Existing live hooks retain their explicit intraday behavior; report defaults become completed-only.
- Unmatched latest session metadata excludes that candidate with an explanation, even for the
  measured old Tokyo bar; this conservative policy can lag one row.
- Reuse audit.root's pure path semantics and hooks.home's existing mkdir wrapper.
- Baseline source 0.3.1 `edbb031`, packaging `0525e82`, current docs-only head `9f0cfa7`.
