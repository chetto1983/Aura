# Phase01 Plan - Stabilize the Foundation Map

Status: Phase01 implementation foundation is closed for Phase01A, Phase01B,
and Phase01C. The package-map stabilization child remains an orientation
scaffold only, not an active implementation gate.

## Goal

Recreate Phase 1 as the parent/master container for the package-map and
foundation sub-phases in `D:/Aura/prd.md`.

## Scope

Phase01 owns:

- `Phase01_Stabilize_Map`
- `Phase01A_Run_Event_Foundation`
- `Phase01B_Identity_Capability_Grants`
- `Phase01C_Question_Gate`

## Non-Goals

- Do not implement code while creating this planning scaffold.
- Do not move lettered sub-phases beside the parent folder.
- Do not treat old wave plans as executable queues.

## Roadmap

1. Verify package map and naming boundaries.
2. Persist run/event foundation before channel migration grows.
3. Add identity and capability grants.
4. Add question/approval gate linked to run events.

## Dependencies

- `D:/Aura/prd.md`
- `D:/Aura/.planning/aura-deep-refactor-decisions.json`
- `D:/Aura/AGENTS.md`
- `D:/Aura/docs/chat-interface-prd.md`

## Decisions Before Implementation

| Decision | Default | Why |
| --- | --- | --- |
| First code slice | Phase01A | Durable run/events support later channel, identity, cron, and swarm work. |
| Run store package | `internal/storage/runs` | Keeps canonical run persistence out of cache and channel packages. |
| Verification status | closed for implemented children | Phase01A verifier repair passed; Phase01B parent closure verifier passed; Phase01C falsification and CI passed. |

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Package map names reflect target architecture | `subphases/Phase01_Stabilize_Map/plan.md` | `subphases/Phase01_Stabilize_Map/benchmark.md` | `source.md` | orientation scaffold; not an active blocker |
| Durable run/event foundation | `subphases/Phase01A_Run_Event_Foundation/plan.md` | `subphases/Phase01A_Run_Event_Foundation/benchmark.md` | `source.md` | implemented and Go-verified locally |
| Identity and grants | `subphases/Phase01B_Identity_Capability_Grants/plan.md` | `subphases/Phase01B_Identity_Capability_Grants/benchmark.md` | `source.md` | closed for Phase01B |
| Question gate | `subphases/Phase01C_Question_Gate/plan.md` | `subphases/Phase01C_Question_Gate/benchmark.md` | `source.md` | closed E2E with CI |

## Implementation Gates

- Each child phase has `plan.md`, `source.md`, `benchmark.md`, and `progress.md`.
- Parent summary names the first bounded implementation slice.
- No active Phase01 implementation blocker remains. New package-map rename work
  must reopen `Phase01_Stabilize_Map` with a fresh source/benchmark first.

## Rollback / Deviation Rule

If implementation discovers a PRD or source conflict, stop, record the conflict
in the child `progress.md`, and update this parent plan before editing code.
