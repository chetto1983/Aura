# Milestones

Append-only log of shipped milestones. New milestones added at the top.

## v4.1 — Codebase Cleanup (Phase-CLEAN) — IN FLIGHT

**Started:** 2026-05-27
**Goal:** Eliminate the lint backlog accumulated across 2 weeks of GSD-free shipping. Flip the CI hard-gate on so the no-debt rules from CLAUDE.md are mechanically enforced going forward.

**Scope:** 44 atomic Codex stories across 7 phases (Waves 0/1/4/6/2/3/5 in locked execution order). Source plan: `docs/phase-clean-plan-2026-05-27.md`.

**Status:** Planning complete; awaiting `/gsd:plan-phase 1`.

---

## Pre-v4.1 milestones (archive)

`.planning/` was wiped on 2026-05-12 (commit `295c86bc dedete gsd`) after Phase 2 of an earlier milestone series closed. Roughly 2 weeks of substantive shipping happened GSD-free between 2026-05-12 and 2026-05-27 (Phase-OP+, Phase-FIX, Phase-MM Wave 1.5/2/3 audio I/O, Phase-WIKI-B Wave A/B + WIKI-FIX, Phase-CONS).

For pre-wipe history see `git log --all -- .planning/MILESTONES.md` (commit `ff8e6cf9 docs: start milestone v4.0 Production Hardening` and predecessors).

Pre-wipe milestones are NOT re-imported into this file — pulling them in would create false "validated requirements" entries with no traceability to current code. The codebase rescan in `.planning/codebase/` is the new ground truth.

---
*Last updated: 2026-05-27.*
