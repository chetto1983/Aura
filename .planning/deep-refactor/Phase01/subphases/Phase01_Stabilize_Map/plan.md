# Phase01 Plan - Stabilize the Map

Status: self-audited scaffold. Not verified.

## Goal

Make package names reflect the target architecture without changing product
behavior.

## Scope

- API and setup/auth surfaces belong under `api`.
- Identity logic belongs under `identity`.
- Search, sources, and projection storage belong under `storage`.
- Chat hub remains the central chat package.
- Channel adapters live under `channels`.
- Scheduler naming becomes `cron`.
- Tool-related packages move under `agent/tools`.

## Non-Goals

- No broad behavior refactor.
- No Telegram delivery changes.
- No old wave execution.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| No import cycles | this file | `benchmark.md` | `source.md` | planned |
| Build/vet/test green | this file | `benchmark.md` | `source.md` | planned |
| No god package created | this file | `benchmark.md` | `source.md` | planned |
| Package name explains responsibility | this file | `benchmark.md` | `source.md` | planned |

## Implementation Gate

Only run this subphase when a concrete package move is selected and targeted
tests are known.
