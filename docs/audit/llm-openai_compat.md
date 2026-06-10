# Audit: internal/llm/openai_compat

**Verdict:** needs-work — two real defects (swallowed parse error, dead struct field); no races; no critical issues.

**Counts:** critical 0 / high 1 / medium 1 / low 1

---

## Findings

### [HIGH][BUG] SSE parse errors silently swallowed — consumer cannot distinguish failure from clean stream end

**ID:** llm-openai_compat-1
**File:** `internal/llm/openai_compat/client.go:145-165`
**Confidence:** high

**Detail:**
`parseSSE` can return a non-nil error (e.g., `fmt.Errorf("openai_compat: malformed SSE chunk: %w", jErr)` — `sse.go:107`). The goroutine in `Stream` captures this in `errString` and writes it to a reasoningtrace record, but never forwards it to the consumer. The channel simply closes. Because `llm.Chunk` has no error field and `<-chan llm.Chunk` carries no error sidecar, the consumer (the agent's `consume` loop at `llm_agent.go:451`) sees a premature channel close that looks identical to a clean, complete stream. A mid-stream JSON decode error — e.g., from a truncated provider SSE frame — is therefore indistinguishable from a normally terminated stream. The agent will then proceed to process a partial tool-call set or a missing text response as if it were complete, leading to incorrect behavior (silent data loss or a malformed response treated as valid).

Additionally, when a parse error occurs after usage was captured (`res.hasUsage == true`), the goroutine still emits a Usage chunk before closing, which may confuse a consumer that wants to correlate usage to a successful parse.

**Suggested fix:**
Two viable approaches:
1. Add an `Err error` field to `llm.Chunk` and emit a sentinel chunk `Chunk{Err: parseErr}` before closing. Every consumer does `if c.Err != nil { handle }`. This is the most explicit and composable fix.
2. Add a second return channel `<-chan error` to `llm.Client.Stream` (a breaking interface change, bigger surgery).

Option 1 is lower risk. Emit the error chunk immediately after `parseSSE` returns a non-nil error, before the usage emit and before the deferred `close(out)`:
```go
res, parseErr := parseSSE(resp.Body, emit)
if parseErr != nil {
    emit(llm.Chunk{Err: parseErr})
}
```
The `emit` select handles both the send and a concurrent cancel safely.

---

### [MEDIUM][DEAD-CODE] `toolCallAcc.firstSeen` written but never read

**ID:** llm-openai_compat-2
**File:** `internal/llm/openai_compat/accumulate.go:32,53`
**Confidence:** high

**Detail:**
`toolCallAcc` declares a `firstSeen int` field (line 32) documented as "a stable tiebreaker" for emission order. It is set once at construction (`acc = &toolCallAcc{firstSeen: a.order}` — line 53) but never read anywhere in the package or the wider repo (confirmed with grep across `D:/Aura`). The `finalize()` method sorts by wire `index` via `sort.Ints(indices)` and never consults `firstSeen`. The `accumulator.order` counter is likewise only written (incremented implicitly through `firstSeen` assignment) but its value is never used to affect output order.

This dead field inflates struct size, misleads readers (the comment implies a tiebreak that doesn't exist), and will survive indefinitely unless removed because the compiler cannot flag unexported struct fields as unused.

**Suggested fix:**
Remove `firstSeen int` from `toolCallAcc`, remove the `a.order` counter from `accumulator`, and remove the `a.order++` increment in `add()`. Update or remove the "stable tiebreaker" comment on line 26. The actual ordering guarantee (by wire `index`) is already implemented and documented in the `finalize` comment on line 74.

---

### [LOW][NOT-WIRED] `wireRequest` omits `temperature` `omitempty` — sends `temperature:0` for zero-value configs

**ID:** llm-openai_compat-3
**File:** `internal/llm/openai_compat/client.go:60`
**Confidence:** medium

**Detail:**
The `wireRequest.Temperature float64` field has no `omitempty` tag (`json:"temperature"`). When a caller constructs `llm.Request{Temperature: 0}` (which is valid Go zero-value and happens in unit tests and some eval harnesses), the wire body sends `"temperature":0` explicitly. OpenRouter/DeepSeek interprets `temperature:0` as a deliberate "greedy/deterministic" setting, not as "use the model default". This is arguably intentional for production (the default config at `llm/config.go:23` sets `defaultTemperature = 0.7` which is always non-zero for real callers), but any hand-built `llm.Request{}` in tests or future callers that omits `Temperature` silently overrides the provider default with deterministic sampling. The field is not "dead" (it always marshals), but the lack of `omitempty` can produce surprising wire behavior for zero-value callers.

This is flagged low because the production load path (`llm.Load`) always sets a non-zero temperature, and the test harness `testConfig` sets `Temperature: 0.7`.

**Suggested fix:**
Add `omitempty` (`json:"temperature,omitempty"`) if the intent is to omit the field and let the provider default apply when temperature is zero. If sending `0` explicitly is intentional for deterministic evals, add a comment documenting this.
