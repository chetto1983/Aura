# Audit: internal/agent/prompt

**Verdict:** needs-work — two low-severity issues; no critical or high defects.
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Scope

Files audited (non-test):
- `internal/agent/prompt/builder.go`
- `internal/agent/prompt/cache_anthropic.go`
- `internal/agent/prompt/hash.go`
- `internal/agent/prompt/reasoning_policy.go`

Tests read for intent (not audited as targets):
- `builder_test.go`, `budget_block_test.go`, `hash_test.go`, `reasoning_policy_test.go`

Cross-repo grep performed across all `*.go` files under `D:/Aura` to verify wiring claims.

## Findings

### [MEDIUM][DEAD-CODE] `ApplyAdaptiveReasoning` is exported with no external callers

**Location:** `internal/agent/prompt/reasoning_policy.go:40`
**Confidence:** high

`ApplyAdaptiveReasoning` is an exported function. Grep across the entire repo (`prompt\.ApplyAdaptiveReasoning`) returns zero results. Its only call site is the internal method `BuildWithReasoningTier` in `builder.go:86` — within the same package, so no import of the symbol from outside is needed or present.

Exporting a function that is only called within its own package violates the least-privilege surface principle and signals API intent that doesn't match usage. External callers are supposed to go through `BuildWithReasoningTier`, not call `ApplyAdaptiveReasoning` directly (the builder enforces the correct ordering of `buildBase` → `ApplyAdaptiveReasoning` → `injectCacheControl`). A caller that calls `ApplyAdaptiveReasoning` directly on an already-built request bypasses `injectCacheControl`, producing a silently wrong result for the Anthropic provider.

**Suggested fix:** Unexport: rename to `applyAdaptiveReasoning` and update the call in `builder.go:86`. The only external reference in the repo is `.planning/` spike documentation, not production code.

---

### [LOW][DEAD-CODE] `isSyntheticUserHint` contains unreachable XML-tag branches

**Location:** `internal/agent/prompt/reasoning_policy.go:97-108`
**Confidence:** high

`isSyntheticUserHint` guards `LastGenuineUserContent`, which is called in production exactly once: `llm_agent_reasoning.go:20` passes `a.history`. The four XML-prefix checks — `<budget>`, `<workspace>`, `<current_time>`, `<today>` — can never be true when the argument is `a.history`, because the budget/workspace/time block is injected by `buildBase` only into a *copy* of history (`msgs = append(append([]llm.Message(nil), history...), ...)`), never into `a.history` itself. Confirmed by grepping `a.history = append` across `internal/agent/*.go`: no budget or XML tag content is ever appended to the live history slice.

These checks would only fire if `LastGenuineUserContent` were called on the assembled *request* messages (which include the tail copy) rather than on the agent's history. No production code does this today. The branches are therefore dead.

The risk is forward-looking: the XML guards give readers a false impression that the budget block can appear in `a.history`, which could mislead a future maintainer into assuming the invariant is maintained in both directions. If the budget-injection pattern ever changes to mutate history in-place, the guards would silently prevent the reasoning router from seeing any turn that contains time/workspace context — returning an empty string from `LastGenuineUserContent` and falling back to `ReasoningTierLow` (see `llm_agent_reasoning.go:21-23`).

**Suggested fix:** Remove the `<budget>`, `<workspace>`, `<current_time>`, `<today>` checks from `isSyntheticUserHint`. If future slices need `LastGenuineUserContent` to operate on assembled messages rather than raw history, add the XML guards back at that point with an explicit comment explaining the call-site contract.

## Clean — checked and clear

The following were checked and found clean:

- **Nil-pointer / unchecked errors:** `PrefixHash` propagates `canonicaljson.Marshal` errors with `%w`. `buildBase` handles nil `history` safely (`append([]llm.Message(nil), nil...)` = empty slice). No unchecked errors.
- **Races:** `PromptBuilder` is a stateless zero-field struct. `Budget` is a value type. `PrefixHash` has no shared state. No goroutines, channels, or shared mutable state anywhere in the package.
- **Slice aliasing / history mutation:** `buildBase` correctly copies history before appending the budget tail: `append(append([]llm.Message(nil), history...), msg)`. The inner `append` allocates a new slice of exactly `len(history)` elements, guaranteeing the caller's backing array is never mutated. Verified by the `TestBudgetBlockByteStable / Build never mutates the caller's history slice` test.
- **`block()` returning empty string:** `present()` guards `block()` in `buildBase`; `block()` is only called when at least one `Budget` field is non-zero, so it never injects an empty user message.
- **`cappedTokens` / `configuredOrDefault` arithmetic:** Correct. `cappedTokens(target, 0)` returns `target`; `cappedTokens(512, 100)` returns 100 (configured max respected). No overflow possible (values are small well-typed ints from config).
- **`injectCacheControl` string constants:** `providerAnthropic` and `cacheControlEphemeral` are unexported and only used within `cache_anthropic.go`. Wiring to `ToolsCacheControl` is correct — no history-message mutation.
- **`isSyntheticUserHint` coverage of actual history nudges:** The four agent nudges that ARE appended to `a.history` (`recoveryNudgeGeneric`, `recoveryNudgeToolPrefix+...`, `recoveryNudgeEmpty`, completion veto) are all matched. Backtick byte identity between `reasoning_policy.go` and `llm_agent_finalize.go` confirmed (both ASCII 0x60). `"Completion check FAILED:"` correctly prefix-matches `"Completion check FAILED: reason"` because `strings.HasPrefix` does not require an exact suffix.
- **`buildBase` double-allocation via `append(append(...))` pattern:** The nested append is idiomatic and correct. The outer append forces a reallocation since the inner slice has `len==cap`, so the appended element never aliases the original.
- **Dead-code check for other unexported symbols:** `present()`, `block()`, `buildBase`, `injectCacheControl`, `isSyntheticUserHint`, `cappedTokens`, `configuredOrDefault`, `boolPtr`, and the two token-cap constants are all reachable from within the package. None are dead.
- **`PrefixHash` with empty index set:** Returns the SHA-256 of zero bytes (the empty-digest), deterministic across calls. The forward-compat design (`{0,1,2}` over a single-message history equals `{0}`) is correctly implemented by the `i >= len(msgs)` skip guard.
