# Audit: internal/agent/prompt

**Verdict:** needs-work — one exported symbol is unreachable from outside the package; one caller in `llm_agent.go` builds the request twice per turn, discarding the first result; both are low-severity.

**Counts:** critical 0 / high 0 / medium 1 / low 1

---

## Findings

### [MEDIUM][NOT-WIRED] `ApplyAdaptiveReasoning` is exported but has no caller outside its own package

**Location:** `internal/agent/prompt/reasoning_policy.go:40`

**Confidence:** high

**Detail:**
`ApplyAdaptiveReasoning` is exported (uppercase) but is called from exactly one site in the entire repo: `BuildWithReasoningTier` in `internal/agent/prompt/builder.go:86`, which is inside the same package. No external package imports it directly.

Grep evidence: a repo-wide search for `prompt\.ApplyAdaptiveReasoning` returns zero matches. The only definition-site and call-site are both in `internal/agent/prompt`.

Exporting the function implies a stable public contract that downstream callers can depend on. Currently it is dead surface: callers wanting adaptive reasoning go through `BuildWithReasoningTier`, making this export a footgun — someone could call `ApplyAdaptiveReasoning` directly and bypass the `injectCacheControl` step that `BuildWithReasoningTier` applies after it.

**Suggested fix:**
Lowercase the function to `applyAdaptiveReasoning`. Update the one internal call in `builder.go:86`. No external callers to update.

---

### [LOW][BUG] `llm_agent.go` builds the request twice per turn when adaptive reasoning is active, discarding the first result

**Location:** `internal/agent/llm_agent.go:201-203` (caller, not in the `prompt` package itself — flagged here because it is a misuse of the `prompt` API surface)

**Confidence:** high

**Detail:**
```go
req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)  // line 201
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)  // line 203
}
```

`adaptiveTierOK` is true in every production path: `adaptiveReasoningTier` returns `true` on success, on router timeout (fallback to `ReasoningTierLow`), on empty user content (fallback), and on an invalid tier string (fallback). The only path where `adaptiveTierOK=false` is when `cfg.AdaptiveReasoning=false` or the provider is not OpenRouter — both rare/non-default configurations.

When `adaptiveTierOK=true`, the `Build` call at line 201 executes `reg.RenderToolDefs()` (a sort + slice copy over the tool manifest) and `injectCacheControl`, then the result is immediately overwritten. This is wasted CPU per LLM call.

There is no correctness defect — `BuildWithReasoningTier` is a strict superset of `Build` — but the redundant call is pure waste.

**Suggested fix:**
```go
var req llm.Request
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
} else {
    req = a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
}
```

---

## Clean — what was checked and found sound

The following were audited and are clean:

**`PrefixHash` (hash.go):** Correct SHA-256 accumulation, `i < 0` guard prevents negative-index panic, `canonicaljson.Marshal` errors are propagated with `%w`, no h.Write error is possible (sha256.Hash.Write never returns an error). Forward-compat index semantics (`{0,1,2}` on a one-message slice equals `{0}`) are correct and tested.

**`PromptBuilder.Build` / `buildBase` (builder.go):** History copy-on-write when `budget.present()` uses `append(append([]llm.Message(nil), history...), ...)` — the two-append idiom is correct and the caller's slice is never mutated. `Budget` zero value correctly short-circuits to no trailing message. No slice aliasing hazard.

**`Budget.present()` / `Budget.block()` (builder.go):** `present()` correctly returns false for the zero value (all-empty budget). `block()` renders tags in a stable order (budget → workspace → current_time → today); the order is consistent with what `isSyntheticUserHint` expects to see as leading prefixes.

**`isSyntheticUserHint` (reasoning_policy.go):** All five synthetic message prefixes injected by `llm_agent_finalize.go` and `llm_agent_completion.go` are matched:
- `<budget>` / `<workspace>` / `<current_time>` / `<today>` — budget block tags
- `"Stop calling tools."` — matches `finalizeNudge`
- `"You have run out of tool-call budget"` — matches `recoveryNudgeGeneric`
- `"You have already called \`"` — matches `recoveryNudgeToolPrefix`
- `"Completion check FAILED:"` — matches `completionVetoPrefix` (`"Completion check FAILED: "` starts with `"Completion check FAILED:"`, so `HasPrefix` returns true correctly)

No genuine user message can accidentally start with any of these XML-bracket prefixes in a way that would misfire; the only false-positive risk (a user literally typing `<budget>`) is acceptable for this internal routing heuristic.

**`ApplyAdaptiveReasoning` / `IsOpenRouterReasoningTarget` (reasoning_policy.go):** Guard chain is correctly ordered (`AdaptiveReasoning` flag → provider/URL check → tier validity). `IsOpenRouterReasoningTarget` correctly handles an empty BaseURL (treated as openrouter-compatible). No race: `ApplyAdaptiveReasoning` mutates only the `*llm.Request` argument, which is stack-local in the caller.

**`injectCacheControl` / `cache_anthropic.go`:** Dormant no-op under non-Anthropic providers. Only sets `ToolsCacheControl`; never touches `Messages`. Consistent with test assertions.

**`boolPtr` (reasoning_policy.go:126):** Unexported helper, used three times in the same file. Correct escapes-to-heap pattern for pointer-to-bool literals. No duplication with the identically-named function in `internal/mcp/managed_config_test.go` — different package, different file scope.

**Race / concurrency:** The package has no goroutines, no shared mutable state, no maps written concurrently, and no sync primitives. `PromptBuilder` is stateless (empty struct). All functions are pure or pointer-to-stack-local.

**Dead code:** `boolPtr`, `cappedTokens`, `configuredOrDefault`, `isSyntheticUserHint`, `block()`, `present()`, `buildBase`, `injectCacheControl` are all unexported and confirmed reachable from within-package callers. No orphaned unexported symbols found.
