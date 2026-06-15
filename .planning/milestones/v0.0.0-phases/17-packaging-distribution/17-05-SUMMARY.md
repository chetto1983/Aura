---
phase: 17-packaging-distribution
plan: 05
subsystem: aggregate-doctor
tags: [packaging, doctor, health-checks, ops, tdd]
requires: [17-01, 17-04]
provides:
  - Top-level aura doctor aggregate health command
  - Direct Postgres, Neo4j, embed, MCP binary, and LLM-key checks
  - Build-tagged live doctor integration leg
affects: [cmd-aura]
tech-stack:
  added: []
  patterns:
    - Seamed direct service probes
    - LLM-free config loading through config.LoadDB()
    - Informational keyless LLM check
key-files:
  created:
    - cmd/aura/doctor.go
    - cmd/aura/doctor_test.go
    - cmd/aura/doctor_integration_test.go
    - .planning/phases/17-packaging-distribution/17-05-SUMMARY.md
  modified:
    - cmd/aura/main.go
key-decisions:
  - `aura doctor` uses direct service probes only; it does not use Docker status or require a Docker socket.
  - Missing `OPENROUTER_API_KEY` is reported as informational because keyless serve boot is valid after 17-04.
  - Live doctor coverage is behind `db_integration neo4j_integration` tags and fails under CI when required env is absent.
requirements-completed: [OPS-01]
metrics:
  duration: ~45min
  tasks: 2
  files-modified: 4
  completed: 2026-06-14
---

# Phase 17 Plan 05: Aggregate Doctor Summary

`aura doctor` is now a top-level operator health check. It loads config with `config.LoadDB()` so it does not require an LLM key, then prints one pass/fail line for Postgres, Neo4j, the embed sidecar, the `mcp-neo4j-cypher` binary, and LLM-key presence.

## Performance

- Started: 2026-06-14T14:25:00+02:00
- Completed: 2026-06-14T15:12:00+02:00
- Duration: ~45 min active work
- Tasks completed: 2
- Files changed: 4

## TDD Evidence

- RED: `go test ./cmd/aura -run TestDoctor -v` failed with undefined doctor seams before `cmd/aura/doctor.go` existed.
- GREEN: `go test ./cmd/aura -run TestDoctor -v` passed after adding the aggregate runner, probe seams, and unit tests.
- HARDENED: default probe tests cover Postgres/Neo4j seam success paths, embed dimension checking, MCP binary lookup, and failure naming.

## Accomplishments

- Added `cmd/aura/doctor.go` with five direct probes and sysexits-style results: unreachable services return `exitUnreachable`, missing MCP binary returns `exitInfra`, bad invocation returns `exitUsage`, and all-green returns 0.
- Wired `aura doctor` into `cmd/aura/main.go` dispatch, usage, and the top-level command comment.
- Added table-driven unit coverage for all-green output, hard-failure output and exit codes, and missing LLM key as a non-failing informational check.
- Added a build-tagged live `TestDoctorLiveStack` leg for real Postgres, Neo4j, embed, and MCP binary verification; it skips locally without env and hard-fails under CI if env is missing.
- Confirmed `cmd/aura/doctor.go` contains no `docker compose ps` usage.

## Task Commits

| Task | Commit | Summary |
| --- | --- | --- |
| 1-2 | a7509279 | Added the aggregate doctor command, dispatch wiring, unit tests, and live integration leg. |

## Verification Evidence

- `go test ./cmd/aura -run TestDoctor -v` passed.
- `go test -race ./cmd/aura -run TestDoctor -v` passed.
- `go test ./cmd/aura -run "TestDoctor|TestServeKeyless|TestLLMNotConfigured|TestProductionContainerArtifacts" -v` passed.
- `go test ./cmd/aura -v` passed.
- `go test -tags "db_integration neo4j_integration" ./cmd/aura -run TestDoctorLiveStack -v` compiled and skipped locally because live env was absent.
- `Select-String -Path cmd/aura/doctor.go -Pattern "docker compose ps"` returned no matches.
- `go vet ./cmd/aura` passed.
- `go build ./...` passed.
- Coverage function report for `cmd/aura/doctor.go`: runner core 77.8%, checks list 100%, Postgres probe 88.9%, Neo4j probe 81.8%, embed probe 72.7%, MCP binary probe 71.4%, LLM-key probe 100%; only the `os.Exit` wrapper remains uncovered.
- Pre-commit `gofmt`, `vet`, and file-size hooks passed.

## Deviations

- The live tagged test was added as `cmd/aura/doctor_integration_test.go` instead of inside `doctor_test.go` so the build tags cleanly gate the real-stack leg.
- Unit tests validate real default probe logic through seams where possible, while the full all-green live path remains tag-gated because local Postgres/Neo4j/embed services are not guaranteed.

## Issues Encountered

- The owned-surface coverage check initially exposed weak coverage on the default Postgres/Neo4j paths, so client-opening seams were added and tested.

## User Setup Required

None.

## Next Phase Readiness

Plan `17-06` can hook installer/post-install guidance to a real `aura doctor` command that validates the runtime through direct in-box probes.

## Self-Check: PASSED
