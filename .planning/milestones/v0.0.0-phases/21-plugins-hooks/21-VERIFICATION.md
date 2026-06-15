---
phase: 21-plugins-hooks
verified: 2026-06-15T00:00:00Z
status: passed
score: 6/6 requirements verified
---

# Phase 21: Plugins Hooks Verification Report

**Status:** PASSED

## Goal Achievement

Aura now has an optional hook layer on `LlmAgent` for EXT-1: in-process Go hooks plus trust-gated command-program hooks can observe, rewrite, veto, or short-circuit at the five specified loop points.

## Verified Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Nil hook manager is a valid no-op | verified | `TestLlmAgentHooks_NilManagerNoop`; full `go test ./internal/agent` green |
| 2 | `BeforeModel` can short-circuit after budget consumption | verified | `TestLlmAgentHooks_BeforeModelShortCircuitsAfterBudget` |
| 3 | `BeforeTool` can rewrite args and audit emits rewritten args | verified | `TestLlmAgentHooks_BeforeToolCanRewriteArgsAndAuditUsesRewrite` |
| 4 | `BeforeTool` can veto execution with a synthetic result | verified | `TestLlmAgentHooks_BeforeToolVetoSkipsExecution` |
| 5 | `AfterTool` can rewrite result before next model request | verified | `TestLlmAgentHooks_AfterToolCanRewriteResult` |
| 6 | Command hooks are trust-gated and bounded | verified | `TestCommandHook_BeforeToolRewrite`, `TestCommandHook_BeforeToolDeny`, `TestCommandHook_TrustHashMismatchRefusesBeforeExecution`, `TestCommandHook_TimeoutIsError` |

## Verification Commands

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
ok (cache invariant gate): 22 identical messages[0] hashes (0daddf9343736e5ef8182d191d158ddc4b3c9ad4440adf492c5a3b33721332b1)
ok (cache invariant gate): 22 identical messages[1] profile/skills hashes (69a5c1b07bce039951efdd95cbc078f6e994a0e490e23b8ca4f39dc2a4c14537)
ok (cache invariant gate): 22 identical skill manifest-in-Description hashes (4ec31b4741033bb51ad61683eb49ba3dad37ff6dbd9351ca22b6689091912889)
```

## Human Verification Required

None. Phase 21 is an internal agent-loop extension surface with deterministic unit and regression coverage; there is no separate live/manual channel boundary.

## Gap Summary

No EXT-1 gaps remain. EXT-2 and EXT-3 are intentionally out of scope for this phase.
