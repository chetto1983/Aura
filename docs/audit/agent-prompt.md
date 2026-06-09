# Audit: internal/agent/prompt

**Verdict:** needs-work — one medium latent aliasing risk + one low dead-code constant; all other claims verified clean.

**Counts:** critical 0 / high 0 / medium 1 / low 2

---

## Findings

### [MEDIUM][BUG] buildBase returns unprotected slice alias when budget is absent

**Location:** `internal/agent/prompt/builder.go:91-104`

**Confidence:** medium

**Detail:**

```go
func (b *PromptBuilder) buildBase(...) llm.Request {
    msgs := history        // msgs IS history — same backing array
    if budget.present() {
        msgs = append(append([]llm.Message(nil), history...), ...)  // copy only here
    }
    req := llm.Request{Messages: msgs, ...}
    return req
}
```

When `budget.present()` is false the returned `req.Messages` is the caller's `history` slice header (same pointer, same backing array). Any append to `history` after `Build` returns — even a capacity-safe one that does not reallocate — will not corrupt `req.Messages` (different slice header), but a mutation of an existing element (`history[i].Content = ...`) WILL silently reach through into `req.Messages[i]` because both point at the same underlying array.

The production caller (`internal/agent/llm_agent.go:201-203`) does not mutate elements between `Build` and use, so this is currently latent. However, `buildBase` is also called by `BuildWithReasoningTier`, and future maintenance that adds history mutation after the build call (e.g., appending a recovery nudge mid-turn — which already happens in `maybeRecover`) would be silently wrong if the budget was absent at build time.

Compounding: in production `budget.present()` is always true (workspace + currentTime are always populated at `llm_agent.go:190-196`), so the copy path IS always taken today. The risk is a regression if callers change.

**Suggested fix:** Unconditionally copy the slice in the zero-budget path:

```go
msgs := append([]llm.Message(nil), history...)
if budget.present() {
    msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: budget.block()})
}
```

This is a single extra alloc on zero-budget calls (which are unit-test-only paths in production today) and eliminates the aliasing surface.

---

### [LOW][DEAD-CODE] `providerAnthropic` and `cacheControlEphemeral` constants are unexported and referenced only within the package

**Location:** `internal/agent/prompt/cache_anthropic.go:8,15`

**Confidence:** high

**Detail:**

```go
const providerAnthropic = "anthropic"
const cacheControlEphemeral = "ephemeral"
```

Both constants are unexported. A repo-wide grep for `providerAnthropic` and `cacheControlEphemeral` finds zero references outside `cache_anthropic.go` itself (confirmed). They are consumed only by `injectCacheControl` (also in this file). This is not wrong — the constants exist to avoid a magic string — but they are pure package-internal implementation detail. This is a style/documentation observation, not a real defect.

Note: `injectCacheControl` is also unexported and is tested directly via package-internal test (`builder_test.go:143`). Its only production callers are `Build` and `BuildWithReasoningTier` within the same package. The seam is intentional (marked dormant, D-03) and the constants are correctly scoped.

**Suggested fix:** No action required. If the Anthropic seam ever graduates from dormant to active (Slice 13), these constants may be promoted or used by a wire-translation layer. The current scoping is correct.

---

### [LOW][NOT-WIRED] `Build` call at `llm_agent.go:201` is always superseded when adaptive reasoning is enabled

**Location:** `internal/agent/llm_agent.go:201-203` (caller of `internal/agent/prompt`)

**Confidence:** high

**Detail:**

```go
req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)  // always runs
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
}
```

`adaptiveTierOK` is true whenever `cfg.AdaptiveReasoning` is enabled and the provider is OpenRouter (the production default). In those circumstances the `req` produced at line 201 — including its `reg.RenderToolDefs()` call, its message slice copy, and its `injectCacheControl` call — is immediately discarded and recomputed at line 203. The first `Build` is dead work every turn.

This is not a bug in the `prompt` package (both builders are correct), but it is a not-wired / dead-execution pattern: the result of `Build` at line 201 is unreachable in practice.

**Suggested fix:** Eliminate the unconditional `Build` call:

```go
var req llm.Request
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
} else {
    req = a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
}
```

---

## What was checked and found clean

- **hash.go / PrefixHash**: bounds check `i < 0 || i >= len(msgs)` is correct; negative-index and beyond-len indices silently skip; empty-index-set produces the stable SHA-256 zero-data digest consistently. No error swallowing (error is propagated via `%w`).
- **reasoning_policy.go / ApplyAdaptiveReasoning**: triple-guard (`AdaptiveReasoning`, `IsOpenRouterReasoningTarget`, `tier.Valid()`) is correct; `BuildWithReasoningTier` short-circuits cleanly when any guard fails and leaves `req` unchanged. Verified against test cases `disabled_leaves_request_unchanged` and `non_openrouter_leaves_reasoning_empty`.
- **reasoning_policy.go / cappedTokens + configuredOrDefault**: arithmetic is correct; `configuredMax <= 0` path returns the constant cap; no overflow risk (values are bounded by LLM provider limits, well within int range).
- **reasoning_policy.go / isSyntheticUserHint**: all five nudge prefix literals (`"Stop calling tools."`, `"You have run out of tool-call budget"`, `"You have already called \``"`, `"Your last response was empty."`, `"Completion check FAILED:"`) are verified to match the production constants in `llm_agent_finalize.go` and `llm_agent_completion.go` byte-for-byte via grep. The four XML-tag prefixes (`<budget>`, `<workspace>`, `<current_time>`, `<today>`) cover every ordering `Budget.block()` can produce as the first tag of its output.
- **reasoning_policy.go / LastGenuineUserContent**: walk-from-tail loop with role + empty-content + synthetic guards is correct; no off-by-one; empty-history returns `""` without panic.
- **builder.go / Budget.present()**: correctly returns false for the zero-value struct (all zero/empty), so the zero-budget zero-workspace zero-time path does NOT inject a trailing message. Matches test `TestBudgetBlockByteStable/zero_counts_omit_the_budget_block`.
- **builder.go / Build vs BuildWithReasoningTier**: both call `buildBase` first, then the appropriate mutation, then `injectCacheControl` — ordering is correct.
- **cache_anthropic.go / injectCacheControl**: providerAnthropic comparison is case-sensitive (`!=`); the callers in `llm_agent.go` always pass `a.cfg.Provider` as-is. Config loading lower-cases provider names on load (verified: `llm/config.go`), so `"anthropic"` is the correct canonical form. No case-sensitivity bug.
- **No goroutines, channels, mutexes, or shared state**: the package is entirely stateless (PromptBuilder is an empty struct). No race conditions are possible.
- **No resource leaks**: no I/O, no file handles, no HTTP connections opened.
- **No context propagation**: none needed — the package is pure, synchronous computation with no I/O.
