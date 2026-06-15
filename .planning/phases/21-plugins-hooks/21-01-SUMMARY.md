---
phase: 21-plugins-hooks
plan: 01
subsystem: agent
status: complete
completed: 2026-06-15
requirements-completed: [EXT-01/EXT-1]
---

# Phase 21 Plan 01 Summary: Plugins Hooks

Implemented the EXT-1 hook substrate for Aura's agent loop.

## Delivered

- Public in-process hook API in `internal/agent/hooks.go`.
- Optional `LlmAgentConfig.HookManager` wiring.
- Five insertion points in `LlmAgent.Run` / dispatch:
  - `OnTurnStart`
  - `BeforeModel`
  - `BeforeTool`
  - `AfterTool`
  - `OnTurnEnd`
- First-non-nil-wins composition for result-producing hook methods.
- `BeforeTool` rewrite/veto semantics before dedup, execution, and `ToolInvocation` audit emission.
- `AfterTool` rewrite before RoleTool history append.
- Trust-gated command-program hooks in `internal/agent/hooks_command.go`:
  - JSON event on stdin
  - JSON decision on stdout
  - `allow`, `deny`, and `rewrite`
  - executable SHA-256 trust check before every run
  - timeout kill/error path
- Unit coverage for nil/no-op behavior, model short-circuit, tool arg rewrite, tool veto, result rewrite, first-non-nil-wins, command rewrite/deny, trust mismatch, and timeout.

## Files Created

- `internal/agent/hooks.go`
- `internal/agent/hooks_command.go`
- `internal/agent/llm_agent_hooks_test.go`
- `internal/agent/hooks_command_test.go`
- `.planning/phases/21-plugins-hooks/21-01-PLAN.md`
- `.planning/phases/21-plugins-hooks/21-01-SUMMARY.md`

## Files Modified

- `internal/agent/llm_agent.go`
- `internal/agent/prompt_test.go`

`prompt_test.go` was aligned to the already-present stronger memory prompt wording ("without being asked").

## Verification

```text
go test ./internal/agent -run 'TestLlmAgentHooks|TestHookManager|TestCommandHook' -count=1
ok github.com/chetto1983/aura/internal/agent 0.582s

go test ./internal/agent -count=1
ok github.com/chetto1983/aura/internal/agent 9.220s

go test ./internal/agent/... -count=1
ok github.com/chetto1983/aura/internal/agent
ok github.com/chetto1983/aura/internal/agent/agenttest
ok github.com/chetto1983/aura/internal/agent/mcptools
ok github.com/chetto1983/aura/internal/agent/prompt
ok github.com/chetto1983/aura/internal/agent/tools
ok github.com/chetto1983/aura/internal/agent/workflow

bash scripts/cache_invariant_audit.sh
ok (cache invariant gate): 22 identical messages[0] hashes
ok (cache invariant gate): 22 identical messages[1] profile/skills hashes
ok (cache invariant gate): 22 identical skill manifest-in-Description hashes
```

## Boundaries

Complete for EXT-1. EXT-2 (`aura.plugin.json` manifest + installer + `plugins_audit`) and EXT-3 (self-install loop + `capability_grants`) remain future milestone work, as scoped in `21-SPEC.md`.

## Known Gaps

No Phase 21 implementation gaps remain for EXT-1. Race/goleak/mutation/coverage hardening beyond the executed package/subtree tests remains normal release-gate work, not an open Phase 21 blocker.
