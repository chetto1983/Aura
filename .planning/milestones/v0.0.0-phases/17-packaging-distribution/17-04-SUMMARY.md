---
phase: 17-packaging-distribution
plan: 04
subsystem: serve-boot-llm-config
tags: [packaging, keyless-boot, llm, config, tdd]
requires: [17-01, 17-02, 17-03]
provides:
  - Serve-path keyless LLM config loading
  - Fail-closed llm_not_configured call-time guard
  - Updated fat-image container artifact regression
affects: [cmd-aura, internal-config, internal-llm]
tech-stack:
  added: []
  patterns:
    - Thin wrapper over shared config-load core
    - Composition-root loader injection
    - Structured JSON error from a guarded llm.Client
key-files:
  created:
    - cmd/aura/keyless_test.go
    - cmd/aura/llm_client.go
    - internal/config/config_serve_test.go
    - .planning/phases/17-packaging-distribution/17-04-SUMMARY.md
  modified:
    - cmd/aura/chat.go
    - cmd/aura/container_artifacts_test.go
    - cmd/aura/serve.go
    - internal/config/config.go
    - internal/config/config_test.go
    - internal/llm/config.go
    - internal/llm/config_test.go
key-decisions:
  - `llm.Load()` remains fail-fast; `llm.LoadAllowEmptyKey()` is the only empty-key-tolerant LLM loader.
  - `config.LoadServe()` is the serve-only composite; `config.Load()` and `config.LoadDB()` keep their existing semantics.
  - The call-time guard is an `llm.Client` wrapper, so empty-key serve boots but LLM calls fail locally before any upstream dial.
  - The legacy root-Dockerfile hardening regression test now tracks the Phase-17 fat-image box model.
requirements-completed: [OPS-01]
metrics:
  duration: ~35min
  tasks: 2
  files-modified: 10
  completed: 2026-06-14
---

# Phase 17 Plan 04: Keyless Boot Summary

`aura serve` now boots through a keyless-tolerant config path while `aura chat` keeps its existing friendly `ErrMissingAPIKey` fail-fast. Runtime LLM calls made without a key return structured `llm_not_configured` JSON locally, before any upstream request can be built.

## Performance

- Started: 2026-06-14T12:46:00Z
- Completed: 2026-06-14T13:56:00Z
- Duration: ~35 min active work
- Tasks completed: 2
- Files changed: 10

## TDD Evidence

- RED: `go test ./internal/llm ./internal/config -run "TestLoadAllowEmptyKey|TestLoadServe|TestLoadDB_NoLLMKeyRequired" -v` failed with undefined `llm.LoadAllowEmptyKey` and `LoadServe`.
- RED: `go test ./cmd/aura -run "TestServeKeyless|TestChatBootStillRequiresAPIKey|TestLLMNotConfigured|TestLLMConfigured" -v` failed with undefined `newLLMClient`.
- GREEN: both focused commands passed after the load variants, serve wiring, and client guard were implemented.

## Accomplishments

- Refactored `internal/llm/config.go` to keep `Load()` fail-fast while adding `LoadAllowEmptyKey()` over the same four-tier resolution chain.
- Added `config.LoadServe()` so the daemon can load LLM defaults with an empty `OPENROUTER_API_KEY`; `Load()` and `LoadDB()` remain covered as unchanged behaviors.
- Split the chat composition root so `bootChatEnv` uses `config.Load()` and `bootServe` uses the new serve loader.
- Added `newLLMClient`: keyed configs use `openai_compat.New`, while empty keys return a guarded client that emits `{"error":"llm_not_configured","hint":...}` without dialing upstream.
- Refreshed the stale container artifact regression to assert the current fat-image contract under `docker/aura/Dockerfile`.

## Task Commits

| Task | Commit | Summary |
| --- | --- | --- |
| 1-2 | 0241c4da | Added keyless serve loading, call-time LLM guard, tests, and the fat-image artifact regression refresh. |

## Verification Evidence

- `go test ./internal/llm ./internal/config -run "TestLoadServe|TestLoadAllowEmptyKey|TestConfigMissingKey|TestWebDefaults" -v` passed.
- `go test ./cmd/aura -run "TestServeKeyless|TestLLMNotConfigured|TestChatBootStillRequiresAPIKey|TestLLMConfigured" -v` passed.
- `go test ./internal/llm ./internal/config ./cmd/aura -v` passed.
- `go test -race ./internal/llm ./internal/config ./cmd/aura -run "TestLoadServe|TestLoadAllowEmptyKey|TestConfigMissingKey|TestWebDefaults|TestServeKeyless|TestLLMNotConfigured|TestChatBootStillRequiresAPIKey|TestLLMConfigured|TestProductionContainerArtifacts" -v` passed.
- `go vet ./internal/llm ./internal/config ./cmd/aura` passed.
- `go build ./...` passed.
- Pre-commit `gofmt`, `vet`, and file-size hooks passed; `internal/config/config_test.go` was split back under 600 LOC.

## Deviations

- The plan expected a full serve boot success with no key, but a unit-level boot test intentionally stops at infra validation with DB/Neo4j secrets unset. This proves `ErrMissingAPIKey` is no longer the serve blocker without requiring a live database.
- Updating `cmd/aura/container_artifacts_test.go` was an extra cleanup discovered by the broader `cmd/aura` package test. The old assertion still expected the removed root Dockerfile and hardened compose knobs from before plan 17-02.

## Issues Encountered

- PowerShell rejected a shell-style `&&`; staging and commit were rerun as separate commands.
- The first commit attempt failed the 600-LOC hook for `internal/config/config_test.go`; the new serve config test moved to `config_serve_test.go`.

## User Setup Required

None.

## Next Phase Readiness

Plan `17-05` can build the aggregate doctor check on top of a daemon that can boot before the OpenRouter key exists and report missing LLM configuration at call time.

## Self-Check: PASSED
