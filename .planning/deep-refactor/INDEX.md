# Aura Deep Refactor Phase Index

This directory is the developer-facing execution map for the Aura deep
refactor. The canonical product route remains `D:/Aura/prd.md`; this folder
turns that route into bounded implementation units.

Status: legacy deep-refactor execution map. It remains useful for provenance
and pre-post-drift phase folders, but the current post-drift route is `PRD.md`
section 7.5 plus `.planning/post-drift-2026-05-21/INDEX.md`. Do not treat this
folder's old "next operating step" as the active queue without reconciling it
against the PRD.

## Plan Authority

Use this ladder when multiple files look like plans:

1. `D:/Aura/PRD.md` is the canonical PRD route and phase order.
2. `D:/Aura/.planning/aura-deep-refactor-decisions.json` is the ADR route.
3. `D:/Aura/.planning/deep-refactor/INDEX.md` is this execution folder map.
4. `.planning/post-drift-2026-05-21/INDEX.md` is the current post-drift
   execution pointer.
5. Each phase or sub-phase folder owns only its local `plan.md`, `source.md`,
   `benchmark.md`, and `progress.md`.

Historical and evidence-only files:

- Removed predecessors such as `docs/aura-master-plan.md`,
  `docs/aura-restructure-prd*.md`, and `docs/chat-interface-prd.md` are
  historical evidence in git history, not active files on disk.
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
- Phase07A, Phase07B, Phase07C, Phase07D, Phase07E, and Phase07F are closed for their
  implemented compact archive, typed collection, projection freshness,
  user/operational typed recall, source span byte-offset, and wiki
  frontmatter metadata promotion slices.
- Phase10 is closed for the SQLite/secrets source-of-truth config slice.

Still open / not green:

- Phase08 now has a reconciled source-backed draft folder after the 2026-05-19
  OpenHuman study, but it is not verified or ready for implementation.
- Phase09 remains planned/scaffolded.
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
  - `subphases/Phase07D_User_Operational_Memory_Typed_Tiers`
  - `subphases/Phase07E_Source_Span_Byte_Offsets`
  - `subphases/Phase07F_Wiki_Frontmatter_Schema_And_Prompt_Version_Promotion`
- `Phase08_Cron_And_Swarm_RunGraph`
- `Phase09_Memory_Source_Discipline`
- `Phase10_Single_Source_Of_Truth_Config`

## Next Operating Step

Current canonical next slice lives in `PRD.md` section 7.5 and
`.planning/post-drift-2026-05-21/INDEX.md`: Phase-CTX planning repair after
Phase-GRAPH-FULL closure. The Phase08 note below is historical until a fresh
user request reopens cron/swarm RunGraph work.

Use `$aura-plan-builder` when the selected phase or sub-phase is still a
self-audited scaffold, missing source/plan/benchmark coverage, or needs
promotion before implementation. Use `$aura-implementation-loop` once a
bounded slice is operationally ready.
