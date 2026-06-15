---
phase: 21
slug: plugins-hooks
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-15
validated: 2026-06-15
---

# Phase 21 Validation Strategy

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing`; command-hook helper uses the Go test binary as the external command |
| Quick command | `go test ./internal/agent -run 'TestLlmAgentHooks|TestHookManager|TestCommandHook' -count=1` |
| Full touched surface | `go test ./internal/agent/... -count=1` |
| Cache invariant | `bash scripts/cache_invariant_audit.sh` |

## Requirement Test Map

| Req | Behavior | Automated Evidence | Status |
|-----|----------|--------------------|--------|
| R1 | HookManager + Hook contract, nil manager no-op | `TestLlmAgentHooks_NilManagerNoop` | green |
| R2 | Five insertion points and first-non-nil-wins | hook recording tests + `TestHookManagerFirstNonNilWins` | green |
| R3 | In-process Go and trust-gated command program authoring | `TestLlmAgentHooks_*`; `TestCommandHook_*` | green |
| R4 | Governance composition and audit consistency | `TestLlmAgentHooks_BeforeToolCanRewriteArgsAndAuditUsesRewrite`; budget short-circuit test | green |
| R5 | KV-cache invariant preserved | `scripts/cache_invariant_audit.sh` | green |
| R6 | Zero-hook safety | `TestLlmAgentHooks_NilManagerNoop`; full `internal/agent/...` regression | green |

## Nyquist Rationale

The hook surface has four independent failure planes:

1. Lifecycle ordering and first-non-nil composition.
2. Model short-circuit after budget consumption.
3. Tool-call rewrite/veto/result-rewrite before audit/history.
4. External command trust and timeout failure modes.

The focused test suite samples each plane directly at the agent loop boundary. The full `internal/agent/...` run catches adjacent package regressions, and the cache-invariant script samples the byte-stable prompt prefix that unit tests cannot infer from behavior alone.

## Sign-Off

- [x] Hook contract covered
- [x] Five insertion points covered
- [x] Command hook trust mismatch covered
- [x] Command hook timeout covered
- [x] Tool invocation rewritten-args audit covered
- [x] Cache invariant covered
- [x] `nyquist_compliant: true`
