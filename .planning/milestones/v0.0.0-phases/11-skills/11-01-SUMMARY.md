---
phase: 11-skills
plan: 01
subsystem: docs/prd
tags: [prd-amendment, skills, slice-7, doc-only, wave-0]
requires: []
provides:
  - "Amended prd.md §Slice 7 + §Slice 7e-core + §Caps & Limits (D-33 package, amendment #48)"
  - "Re-specced ROADMAP Phase 11 (SC#1 native clone, SC#5 catalog default-ON, Goal)"
  - "Aligned REQUIREMENTS CAP-07/CAP-08 wording"
affects:
  - prd.md
  - .planning/ROADMAP.md
  - .planning/REQUIREMENTS.md
tech-stack:
  added: []
  patterns:
    - "Doc-only Wave-0 PRD-amendment plan (fifth after 05-01/08-01/09-01/10-01)"
key-files:
  created:
    - .planning/phases/11-skills/11-01-SUMMARY.md
  modified:
    - prd.md
    - .planning/ROADMAP.md
    - .planning/REQUIREMENTS.md
decisions:
  - "All 12 D-33 supersessions applied with inline D-NN citations for decision-coverage tracing"
  - "skill.approve removed everywhere incl. the generic Risk-Based Governance pipeline line (D-03)"
  - "Old AURA_SKILL_TTL_DAYS renamed → AURA_SKILL_SNIPPET_TTL_DAYS; AURA_SKILL_TTL_SWEEP_INTERVAL_HR removed (cron TaskKind, D-16)"
metrics:
  duration: ~30min
  completed: 2026-06-05
  tasks: 2
  files: 3
  commits: 1
---

# Phase 11 Plan 01: Slice 7 Truth-Source Amendment Summary

Wave-0 doc-only PRD-amendment (D-32/D-33, amendment #48) realigning the stale Slice 7 truth-source to the locked D-01..D-38 decisions and shipped seams, before any Phase 11 code per the PRD-first absolute rule.

## What Was Done

### Task 1 — prd.md §Slice 7 + §Slice 7e-core + env catalog (D-33 package)

Applied all 12 supersessions, each citing its decision ID inline:

1. **Sandbox seam (D-15b/#44)** — every `sandbox.Runner.Execute(...)` / `sandbox.Run(...)` reference marked DEAD; execution is the shipped `tools.SandboxExec` → `internal/sandboxagent.Client` HTTP `:2468` by-path exec (`python3 /skills/<name>.py`, interpreter+path never exec bit).
2. **Migration numbers (D-32)** — `0007_skill_audit`/`0012_snippet_runs` → floor `0009`; skills at `0010` (+ optional `0011_snippet_runs`). The 0010 migration also ALTERs the 0009 `scheduler_tasks.kind` CHECK to admit `skill_ttl_sweep`.
3. **Tool names (D-01)** — dotted `skill.list`/`skill.create`/… → ONE `skill` tool with an `action` enum (`list|info|use|create|update|delete|install|catalog|restore|archive`); `skill.approve` dropped (D-03), no bespoke `run` action (D-04); OpenAI-wire `^[a-zA-Z0-9_-]+$` + no root `oneOf`/`enum` noted.
4. **Prompt injection (D-06/D-07)** — "skills enter the system prompt" → manifest in the `skill` tool Description (turn-stable, BM25 overflow past `AURA_SKILL_MANIFEST_CAP_BYTES`); `always:true` bodies render at `messages[1]`; `messages[0]` gets ONE frozen mechanism sentence (CAP-04 invariant preserved); L2.5 evictor must protect `messages[1]`.
5. **TTL sweep (D-16)** — `ttl_sweeper.go` goroutine → cron `skill_ttl_sweep` TaskKind seeded daily.
6. **Catalog (D-11/D-12/D-14/D-15)** — HTML scrape (`catalogItemRE`) → skills.sh `/api/search` JSON lax-decode; amendment #14 FLIPPED (browse default-ON, `aura skills disable-catalog` escape hatch); install = native Go `git clone --depth 1 --single-branch -c core.autocrlf=false` (node/npx dropped; `--ignore-scripts` was a no-op).
7. **Metadata consolidation (D-19)** — dropped `skill_audit` ALTER columns (last_used_at/use_count); live-state is a per-skill sidecar JSON + optional `snippet_runs` forensics table.
8. **Audit matrix (D-29)** — three-way equivalence → the 5-row coherence matrix (approval_source/paused_state_token/gate_recommended/gate_taken) landed as a table + CHECK SQL spec; Pitfall #6 belt-and-suspenders triggers + `aura_app` role separation kept.
9. **Manifest-packing acceptance (D-09)** — "ALL skills listed even 100+" → cap + "N more — search with skill action=list {query}" BM25 overflow.
10. **Dep strategy (D-36)** — trichotomy (build-time bake / on-demand uv / hybrid) + the corrected "not a forced bake" framing; xlsx North-Star dep set noted.
11. **Egress posture (D-37)** — `needs_network:true` routes through the allowlisted host forward-proxy at RISKY tier (advisory on Docker Desktop, enforced native-Linux); Phase-8 proxy-restoration dependency cross-referenced.
12. **Sandbox token + hardening floor (D-38)** — `AURA_SANDBOX_AGENT_TOKEN` bearer wiring obligation + portable hardening floor; gVisor `runsc` overlay cross-referenced as a Phase-8 sandbox-wide regression (NOT owned by Phase 11).

Env catalog: added the 8 `AURA_SKILL_*` D-34 vars + `AURA_SANDBOX_AGENT_TOKEN`; renamed `AURA_SKILL_TTL_DAYS` → `AURA_SKILL_SNIPPET_TTL_DAYS` and marked `AURA_SKILL_TTL_SWEEP_INTERVAL_HR` removed. Commit-message templates (7a-7e) rewritten to the amended content.

### Task 2 — ROADMAP Phase 11 + REQUIREMENTS CAP-07/CAP-08

- ROADMAP SC#1: `npx skills add --ignore-scripts` → native Go `git clone --depth 1 --single-branch` (D-14/D-15).
- ROADMAP SC#5: "catalog disabled — run enable-catalog" → catalog browse default-ON via `/api/search` JSON; `disable-catalog` escape hatch (D-12, amendment #14 FLIPPED).
- ROADMAP Goal: re-specced to the ONE `skill` tool + manifest-in-Description + messages[1] + native clone + `skill_ttl_sweep` cron TaskKind.
- REQUIREMENTS CAP-07: removed "enable-catalog"/"HTML scrape"; added default-ON + native clone + D-29 matrix + migration 0010 + D-12/D-15 citations.
- REQUIREMENTS CAP-08: removed "session-bound" (superseded by #44/D-15b); added the `:2468`/`SandboxExec` sandbox-agent seam + `skill_ttl_sweep`/D-16 TTL.

## Verification Evidence

- `grep -c "AURA_SKILL_" prd.md` = 39; `AURA_SANDBOX_AGENT_TOKEN` present.
- Acceptance tokens present: `skill action=list`, `messages[1]`, `messages[0]`, `/api/search`, `core.autocrlf=false`, `paused_state_token`, `gate_taken`; inline D-01/D-06/D-07/D-11/D-15/D-16/D-29/D-36/D-37/D-38.
- `skill.approve` = 0 occurrences (removed everywhere, incl. the generic governance pipeline line).
- ROADMAP SC#1 has "git clone", no "npx skills add --ignore-scripts"; SC#5 has "default-ON" + "disable-catalog".
- REQUIREMENTS CAP-07: 0 "enable-catalog"/"HTML scrape"; CAP-08: 0 "session-bound".
- `git diff --diff-filter=D HEAD~1 HEAD` = no deletions. Pre-commit hooks (vet + file-size) green.
- `git diff --stat` for the commit: 3 files (prd.md, ROADMAP.md, REQUIREMENTS.md). STATE.md left unstaged for the final docs commit.

## Deviations from Plan

None affecting scope. One in-scope clarification: the plan's acceptance required prd.md to NOT contain `skill.approve`, but a generic line in §Risk-Based Governance (the resume-handler pipeline, shared with Slice 6 `task.approve`) still read `skill.approve`. Rewrote it to "skill Writer.Activate (NO model-facing skill approve action, D-03)" so the D-03 cut holds everywhere and the acceptance grep is clean. This is consistent with D-03 (no model-facing approve) and within the amendment's scope.

## Known Stubs

None — doc-only plan; no code stubs introduced.

## Threat Flags

None — this plan touches no runtime surface (threat register: T-11-01-T mitigation IS this plan; T-11-01-R accepted via the atomic commit + git history).

## Self-Check: PASSED

- FOUND: `.planning/phases/11-skills/11-01-SUMMARY.md`
- FOUND: `prd.md` (modified)
- FOUND: commit `3a9a65e1`
