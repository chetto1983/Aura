# Audit: internal/llm/openai_compat

**Verdict:** needs-work — two dead-state fields + one not-wired struct field carrier; no critical bugs or races.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][DEAD-CODE] `toolCallAcc.firstSeen` is written but never read

**Location:** `internal/llm/openai_compat/accumulate.go:32,53`

**Confidence:** high

**Detail:**
`toolCallAcc.firstSeen` is documented as "records arrival order as a stable tiebreaker" and is set in `add()` via `&toolCallAcc{firstSeen: a.order}`. However, `finalize()` sorts by wire index using `sort.Ints(indices)` — it never reads `firstSeen`. No other code in the repo (confirmed by Grep across `D:/Aura`) reads the field. The state is written, incremented, and thrown away. The `a.order` counter likewise increments uselessly on every new index.

**Suggested fix:**
Remove `firstSeen int` from `toolCallAcc` and remove the `order int` counter from `accumulator`. The sort-by-wire-index is already deterministic without a tiebreaker. Update the doc comment on `toolCallAcc` to remove the tiebreaker claim.

---

### [MEDIUM][DEAD-CODE] Exported `Usage` type has zero external consumers

**Location:** `internal/llm/openai_compat/usage.go:27`

**Confidence:** high

**Detail:**
`Usage` is an exported struct serving as an intermediate projection between `usageWire.toUsage()` and `Usage.toLLMUsage()`. Grep confirms `openai_compat.Usage` appears nowhere in any Go file outside the package (only in planning `.md` docs). All callers of `openai_compat.New` receive `llm.Usage` chunks via the `llm.Client` interface — they never interact with `openai_compat.Usage` by name. Exporting an implementation-internal pipeline stage leaks the abstraction boundary.

**Suggested fix:**
Rename to `usage` (unexported). Update `parseResult.usage` field type, `usageWire.toUsage()` return type, and `(usage).toLLMUsage()` receiver accordingly. No external callers need updating.

---

### [LOW][NOT-WIRED] `HTTPError.RetryAfterSec` is parsed but no production caller reads it

**Location:** `internal/llm/openai_compat/httperror.go:26,49`

**Confidence:** high

**Detail:**
`newHTTPError` correctly parses the `Retry-After` header on 429 responses and stores it in `HTTPError.RetryAfterSec`. The field surfaces in the `Error()` string. However, the only production retry logic (`internal/agent/llm_agent_stream_retry.go:retryableStreamOpenError`) does not check `*HTTPError` at all — a 429 falls through to returning `false` (no retry), and no backoff based on `RetryAfterSec` is implemented. Only tests (`client_test.go`, `httperror_test.go`) read the field. The parsed value sits in the struct with no consumer that honors it.

This is an intentional design choice (Req#4: zero retries at the wire layer), but the value's presence in the struct implies it should be usable by callers. No production caller imports the type to call `errors.As` and read it.

**Suggested fix:**
Either (a) document explicitly that `RetryAfterSec` is reserved for future callers and add a placeholder in `retryableStreamOpenError` or the agent loop, or (b) if the field will never be consumed, remove it and only surface the hint in the `Error()` string from the raw header value. The current state misleads readers into believing someone uses this.

---

### [LOW][DEAD-CODE] Redundant loop-variable shadow `tc := tc` in `finalize` closure

**Location:** `internal/llm/openai_compat/sse.go:73`

**Confidence:** high

**Detail:**
The line `tc := tc` inside the `for _, tc := range acc.finalize()` loop was the pre-Go-1.22 idiom to capture the loop variable before taking its address. The module is `go 1.26.4`; since Go 1.22, loop variables have per-iteration scope, so the shadow is a no-op. The shadow variable and the original are identical; `&tc` refers to the same allocation either way.

**Suggested fix:**
Delete `tc := tc`. The loop body becomes:
```go
if !emit(llm.Chunk{ToolCall: &tc}) {
    return false
}
```
No behavioral change; reduces noise and eliminates the false impression that an escape-prevention technique is still needed.
