---
phase: 10-scheduler
plan: 01
subsystem: docs
tags: [prd-amendment, scheduler, cron, gronx, env-catalog, spawn-seam]

# Dependency graph
requires:
  - phase: 10-scheduler (discuss/plan)
    provides: 10-CONTEXT.md D-06..D-29 locked decisions reconciled into the PRD
provides:
  - Amended PRD Slice 6 (grammar triad at|every|cron + per-task IANA tz, DST-safe)
  - gronx parser-only cron lib documented; DIY 30s tick retained
  - Direct-LlmAgent spawn seam (mirroring swarm.runChild); dead Coordinator/RejectingResponder/TierConfig refs struck
  - ONE non-deferred task tool with action enum via ActionRouter (supersedes 5-file task_* table)
  - Composite delivery (OQ2 resolved) via WhatsApp/mail MCP self-send + stdout fallback
  - Migration number 0009 pinned; 8 new AURA_SCHEDULER_*/AURA_BACKUP_DIR env vars catalogued
affects: [10-02, 10-03, 10-04, 10-05, 10-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Wave-0 doc-only PRD-amendment gate before any phase code (precedent 05-01/08-01/09-01)"
    - "Inline decision-ID citation (amendment #46 / D-06..D-29) for auditable provenance"

key-files:
  created:
    - .planning/phases/10-scheduler/10-01-SUMMARY.md
  modified:
    - prd.md
    - .env.example

key-decisions:
  - "PRD amendment bundled under a single global number #46 (highest prior was #45); cites Phase-10 CONTEXT decision IDs D-06..D-29 inline"
  - "Dead Slice-6 spawn refs (coordinator-spawn/rejecting-responder/tier-config) reworded to drop the literal identifiers so all four Slice-6 sites are struck; the remaining global grep hits are Slice-3 (Phase 9) live machinery that D-25 mirrors and are out of scope"
  - "ROADMAP SC#1 --at/--every note NOT applied here — worktree executor must not write .planning/ROADMAP.md (orchestrator-owned); deferred to orchestrator"

patterns-established:
  - "PRD amendment #46 reconciles a stale slice with locked CONTEXT decisions across six axes in one auditable header + targeted inline edits"

requirements-completed: [CAP-06]

# Metrics
duration: 18min
completed: 2026-06-04
---

# Phase 10 Plan 01: Scheduler PRD-Amendment Gate Summary

**Reconciled PRD Slice 6 with the 29 locked Phase-10 CONTEXT decisions (amendment #46): grammar triad `at|every|cron` with per-task IANA tz, `adhocore/gronx` parser-only cron lib, direct-LlmAgent spawn seam, one `task` tool with an action enum, composite WhatsApp/mail delivery, migration 0009, and 8 new scheduler/backup env vars — so downstream code plans 10-02..10-06 cite a consistent spec.**

## Performance

- **Duration:** ~18 min
- **Completed:** 2026-06-04
- **Tasks:** 2/2
- **Files modified:** 2 (prd.md, .env.example)

## Accomplishments

- **Task 1 — Slice 6 prose amendment (prd.md):** Replaced the stale `daily HH:MM`/`in=10m` grammar with the industrial triad `at | every | cron` (per-task IANA `tz` column, DST-safe in-zone `next_run_at` recompute, DB stays UTC). Changed "nessuna libreria cron esterna" to parser-only `github.com/adhocore/gronx` with the DIY 30s tick loop retained. Replaced the dead `Coordinator.Spawn`/`RejectingResponder`/`TierConfig` agent_job references with the direct-LlmAgent spawn seam (mirroring `swarm.runChild`, registry minus `swarm_spawn`, budget from `agent_job_runs.step_budget`, ephemeral session `agent_job:<run_id>`, ask_user auto-reject = inject-and-continue). Consolidated the 5-file `task_*` tool table into ONE non-deferred `task` tool with an `action` enum via the `ActionRouter` helper (`required=["action"]` only, no root `oneOf`/`anyOf`/`enum`). Resolved OQ2 to composite delivery (WhatsApp/mail MCP self-send + stdout fallback) and OQ3 to the direct-spawn seam. Cut `tier`/`toolsets` payload fields v1. Pinned migration number `0009`. Added the amendment #46 header documenting all six axes.
- **Task 2 — env catalog (prd.md) + .env.example:** Added 8 `AURA_<DOMAIN>_<UNIT>` env vars (`AURA_SCHEDULER_TZ`, `AURA_SCHEDULER_NOTIFY_DEFAULT`, `AURA_SCHEDULER_NOTIFY_RECIPIENT`, `AURA_SCHEDULER_QUIET_HOURS`, `AURA_SCHEDULER_TICK_SECONDS`, `AURA_SCHEDULER_MAX_CONCURRENT_RUNS`, `AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS`, `AURA_BACKUP_DIR`) to both the PRD env-catalog table and `.env.example`, each with defaults and a new `## ---- Slice 6 (Phase 10): scheduler + backups` section in `.env.example`.

## Task Commits

Each task was committed atomically:

1. **Task 1: Amend prd.md Slice 6 — grammar, gronx, spawn seam, tool surface, delivery** - `1e38becb` (docs)
2. **Task 2: Env catalog + .env.example entries** - `20b433c2` (docs)

_(ROADMAP SC#1 wording note deferred to the orchestrator — see Deviations.)_

## Files Created/Modified

- `prd.md` - Slice 6 section fully amended (grammar triad, gronx, direct-LlmAgent spawn seam, one task tool + ActionRouter, composite delivery, OQ2/OQ3 resolved, migration 0009) + 8 new env-catalog rows. Amendment header #46 added.
- `.env.example` - New `Slice 6 (Phase 10): scheduler + backups` section with the 8 env vars and defaults.
- `.planning/phases/10-scheduler/10-01-SUMMARY.md` - This summary.

## Deviations from Plan

### Skipped (worktree-mode orchestrator-owned write)

**1. ROADMAP SC#1 `--at`/`--every` CLI-triad note NOT applied**
- **Plan step:** Task 2 instructed appending a note to `.planning/ROADMAP.md` Phase 10 SC#1 that the operator CLI also accepts `--at`/`--every` (per D-14/D-29).
- **Why skipped:** The executor prompt's `<parallel_execution>` constraint explicitly forbids this worktree agent from modifying `.planning/ROADMAP.md` (orchestrator-owned write, applied centrally after the wave merges). The edit was drafted and verified (only SC#1 touched, SC#2/#3/#4 byte-unchanged) then reverted to keep the worktree clean.
- **Action required by orchestrator:** Append to ROADMAP Phase 10 SC#1: "The operator CLI also accepts `--at` (one-shot) and `--every` (interval), not just `--cron`, so the one-shot North-Star reminder query (Q3) is verifiable without an LLM (per D-14/D-29, amendment #46)." Do NOT change SC#2 (chaos test stays as written per D-02).
- **Impact:** The PRD env-catalog + Slice 6 grammar already document the CLI triad (`aura task schedule --at/--every/--cron`), so the downstream code plans have the contract. Only the ROADMAP success-criterion wording note is pending.

### Auto-resolved (Rule 1 — scope boundary on the over-broad automated check)

**2. `! grep -q "RejectingResponder" prd.md` literal check left 1 global hit**
- **Found during:** Task 1 verification.
- **Issue:** The plan's automated check `! grep -q "RejectingResponder" prd.md` is whole-file. After striking all four dead **Slice 6** reference sites, one `RejectingResponder` remains at prd.md line ~1460 — but that is inside the **Slice 3 (Swarm, Phase 9)** section and is the *legitimate live* responder machinery that D-25's auto-reject explicitly mirrors. Removing it would corrupt the shipped Phase 9 spec.
- **Resolution:** The substantive acceptance criteria are met: all four Slice-6 dead-ref sites (originally prd.md:1973/2008/2044/2075) are struck — verified via `awk 'NR>=1935 && NR<=2110'` showing NONE of `RejectingResponder`/`Coordinator.Spawn`/`TierConfig` remain in the Slice-6 region. Per CLAUDE.md SCOPE CONTROL (amend only Slice 6) the out-of-scope Slice-3 reference was left untouched. The Slice-6-region grep is the correct verification, not the whole-file grep.

## Verification

- `grep -q "at | every | cron" prd.md` → PASS
- `grep -q "adhocore/gronx" prd.md` → PASS
- Slice-6 region (lines 1935-2110) has NONE of `RejectingResponder`/`Coordinator.Spawn`/`TierConfig` → PASS (all four dead refs struck)
- ONE `task` tool with `action` enum documented; 5-file `task_*` table removed → PASS
- agent_job spawn paragraph references `agent_job:<run_id>` ephemeral session + budget-from-row (D-24) → PASS
- OQ2 marked resolved to composite MCP self-send delivery (D-19); OQ3 resolved to direct-LlmAgent (D-24) → PASS
- All 8 new env vars present in BOTH prd.md AND .env.example → PASS
- migration number `0009` pinned (4 occurrences) → PASS
- No `.go` files modified, no migrations added (doc-only gate) → PASS
- ROADMAP.md unmodified (orchestrator-owned) → confirmed clean

## Threat Flags

None — this plan writes no executable surface (doc-only). The env vars become load-bearing in 10-02..10-06 where their validation/threat-model lands (per the plan's threat register T-10-DOC-01 accept).

## Known Stubs

None — documentation amendment only.
