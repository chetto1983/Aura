# Phase04 Plan - Collapse the Agent Runtime

Status: closed 2026-05-15 for the legacy `agent.Runner` removal /
stateless `agent.RunTask` collapse arc.

## Goal

Move Aura to one loop and one runtime path.

## Scope

- Extract governance from the loop.
- Extract deterministic prompt/context assembly into an agent-owned bundle.
- Merge `agentloop` and `agentruntime` into `agent` when safe.
- Remove compatibility wrappers only when production references are gone.

## Non-Goals

- No channel migration in this phase.
- No prompt behavior drift without snapshots.
- No removal of compatibility wrapper while callers remain.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| No duplicate loop body | this file | `benchmark.md` | `source.md` | met |
| Deterministic prompt bundle snapshots | deferred follow-up | `benchmark.md` | `source.md` | not part of Phase-G closure |
| Prompt evals cover behavior | deferred follow-up | `benchmark.md` | `source.md` | not part of Phase-G closure |
| Agent/chat/swarm/cron tests green | this file | `benchmark.md` | `source.md` | met |

## Implementation Gate

Closed for the Runner-removal arc: production references were migrated to
`agent.RunTask`, `runner.go` and `runner_test.go` were deleted, and the current
tree contains no `*agent.Runner` production reference. Prompt snapshot/eval
hardening remains a separate follow-up if selected.
