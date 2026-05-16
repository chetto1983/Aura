# Aura Deep Refactor Phase Index

This directory is the developer-facing execution map for the Aura deep
refactor. The canonical product route remains `D:/Aura/prd.md`; this folder
turns that route into bounded implementation units.

Status: planning state refreshed on 2026-05-16 after Phase01C was pushed and
GitHub Actions CI passed, then repaired to include the current Phase07C
Projection Freshness Registry evidence. The clean skeleton was recreated on
2026-05-15; some folders remain scaffolds, while the phase files now
distinguish closed slices from not-run plans.

## Plan Authority

Use this ladder when multiple files look like plans:

1. `D:/Aura/prd.md` is the canonical PRD route and phase order.
2. `D:/Aura/.planning/aura-deep-refactor-decisions.json` is the ADR route.
3. `D:/Aura/.planning/deep-refactor/INDEX.md` is this execution folder map.
4. `D:/Aura/CONTINUE-HERE.md`,
   `D:/Aura/.planning/HANDOFF.json`, and
   `D:/Aura/.planning/deep-refactor/.continue-here.md` name the current resume
   state and next likely slice.
5. Each phase or sub-phase folder owns only its local `plan.md`, `source.md`,
   `benchmark.md`, and `progress.md`.

Historical and evidence-only files:

- `D:/Aura/docs/aura-master-plan.md` is a historical predecessor, not the active
  plan.
- `D:/Aura/docs/aura-restructure-prd*.md`,
  `D:/Aura/docs/chat-interface-prd.md`, and review files are evidence for why
  the PRD exists.
- Old wave/context plans may contribute requirements and tests, but must be
  re-authored into the PRD phase folders before implementation.

Current closure slice:

- `Phase01C Question Gate` - closed E2E on 2026-05-16 after live
  falsification repair: durable `chat_questions`, question request/answer
  events, exclusive ask_user pause, stable web pipe thread ids, restart-safe
  Telegram answer routing, repo-wide Go gates, Telegram package/fixture tests,
  compose test-profile verification, and production web-pipe DB probes.
- Source folder:
  `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01C_Question_Gate`.
- Do not bundle Phase 5/6 fine-grained tool approval policy or Phase 8
  RunGraph/swarm redesign into the closed Phase01C primitive.

Closed or closable phase state captured in this sweep:

- Phase01A/Phase01B/Phase01C are closed for their implemented foundation,
  identity/capability, and durable question-gate slices.
- Phase02 is closed for Telegram fixture protection.
- Phase03 is closed for the Telegram-streaming migration arc; the later web
  `/api/chat` Hub migration was closed during Phase01B repair and Phase01C
  falsification.
- Phase04 is closed for the legacy `agent.Runner` removal / `RunTask` collapse
  arc.
- Phase05 and Phase06 are closed for their documented in-scope slices.
- Phase07A, Phase07B, and Phase07C are closed for their implemented compact
  archive, typed collection, and projection freshness slices.
- Phase10 is closed for the SQLite/secrets source-of-truth config slice.

Still open / not green:

- Phase07D-F, Phase08, and Phase09 remain planned or scaffolded.
- `Phase01/subphases/Phase01_Stabilize_Map` remains an orientation scaffold,
  not a separate verified implementation gate.

## Layout Contract

```text
PhaseNN/
  plan.md
  source.md
  benchmark.md
  progress.md
  subphase-summary.md
  subphases/
    PhaseNN_Description/
      plan.md
      source.md
      benchmark.md
      progress.md
    PhaseNNX_Description/
      plan.md
      source.md
      benchmark.md
      progress.md
```

Standalone phases without lettered children use:

```text
PhaseNN_Description/
  plan.md
  source.md
  benchmark.md
  progress.md
```

## Rules

- Do not implement a phase until `source.md`, `plan.md`, and `benchmark.md`
  name the slice, affected files, baseline tests, and non-goals.
- Do not execute old `.planning/wave*` files directly. They are evidence mines.
- Every source row must say what Aura adopts and what Aura rejects.
- `benchmark.md` must separate planned checks from live results.
- `progress.md` is append-only and must record verification, blockers, and
  deviations.
- Lettered Phase 1 children live only under `Phase01/subphases/`.

## Phase Folders

- `Phase01`
  - `subphases/Phase01_Stabilize_Map`
  - `subphases/Phase01A_Run_Event_Foundation`
  - `subphases/Phase01B_Identity_Capability_Grants`
  - `subphases/Phase01C_Question_Gate`
- `Phase02_Protect_Telegram`
- `Phase03_Move_Channels_Behind_Chat`
- `Phase04_Collapse_Agent_Runtime`
- `Phase05_Consolidate_Tools`
- `Phase06_Tool_Experience_Loop`
- `Phase07_Rebuild_RAG_Typed_Memory`
  - `subphases/Phase07A_Compact_Archive_Hygiene`
  - `subphases/Phase07B_Typed_Collection_Registry`
  - `subphases/Phase07C_Projection_Freshness_Registry`
- `Phase08_Cron_And_Swarm_RunGraph`
- `Phase09_Memory_Source_Discipline`
- `Phase10_Single_Source_Of_Truth_Config`

## Next Operating Step

Current canonical slice: none selected after the Phase07C planning repair.
Select the next phase from the prepared phase folders before editing more code.
If continuing memory/RAG work, Phase07D is the next open Phase07 sub-phase; if
starting Phase08 or Phase09, first promote its self-audited scaffold into a
source-backed plan and benchmark.

Use `$aura-plan-builder` when the selected phase or sub-phase is still a
self-audited scaffold, missing source/plan/benchmark coverage, or needs
promotion before implementation. Use `$aura-implementation-loop` once a
bounded slice is operationally ready.
