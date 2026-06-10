# Audit: internal/llm

**Verdict:** needs-work — one high-severity swallowed error that silently truncates stream failures; three low-severity dead-code items.

**Counts:** critical 0 / high 1 / medium 1 / low 3

## Findings

---

### [HIGH][BUG] parseSSE error silently swallowed — consumer receives a clean channel close on mid-stream failure

**Location:** `internal/llm/openai_compat/client.go:145–165`

**Confidence:** high

**Detail:**

The goroutine launched by `Stream` calls `parseSSE(resp.Body, emit)` and captures the returned error in `parseErr`. The error is serialized to a string and written to the reasoning trace (`reasoningtrace.Record`) but is **never forwarded to the consumer**. When `parseSSE` returns a non-nil error (e.g. a malformed JSON chunk — `"openai_compat: malformed SSE chunk: ..."` — or a transport read error), the goroutine still closes `out` normally. The caller's `for c := range ch` loop exits cleanly with zero indication that the stream was truncated. `llm_agent.go:consume()` then returns a partial text / empty finish_reason with no error, and the agent loop treats the turn as a normal "no finish reason" case. A partial tool-call accumulation would produce an invalid JSON arguments string that is silently dispatched.

```go
// client.go:145–165 (abridged)
res, parseErr := parseSSE(resp.Body, emit)
// parseErr only goes to the trace — never sent on `out`, never returned
if res.hasUsage {
    emit(llm.Chunk{Usage: &u})
}
// goroutine exits, `out` is closed — consumer sees a normal end-of-stream
```

The `llm.Client` interface contract documents that `Stream` returns `(nil, error)` on pre-flight errors; there is no mechanism to carry a post-flight error through the channel. This is an architectural gap: the channel is `chan llm.Chunk` with no error slot.

**Suggested fix:**

Add an error chunk variant to `llm.Chunk` (e.g. `Err error`) or use a separate `errCh <-chan error` companion. At minimum, the goroutine should emit a sentinel chunk (e.g. `llm.Chunk{FinishReason: "error"}`) so `consume()` can detect and return an error to the agent loop rather than treating it as a clean stop. A concrete short-term fix: define `Chunk.Err error`; `parseSSE` path emits `llm.Chunk{Err: parseErr}` before closing the channel; `consume()` checks `c.Err != nil` and propagates it.

---

### [MEDIUM][NOT-WIRED] ReasoningEffortXHigh / ReasoningEffortMedium / ReasoningEffortMinimal declared but never used in production

**Location:** `internal/llm/client.go:133,135,137`

**Confidence:** high

**Detail:**

Three of the five `ReasoningEffort` constants are never referenced outside the definition file:

- `ReasoningEffortXHigh = "xhigh"` (line 133)
- `ReasoningEffortMedium = "medium"` (line 135)
- `ReasoningEffortMinimal = "minimal"` (line 137)

Grep across `D:/Aura/**/*.go` confirms the only uses of `ReasoningEffort*` values in production code are `ReasoningEffortHigh`, `ReasoningEffortLow`, and `ReasoningEffortNone` (in `internal/agent/prompt/reasoning_policy.go`). The three orphan constants do not appear in any non-definition, non-test file. They are exported, so technically not dead code by the Go compiler, but there is no wiring path to OpenRouter for them and no test exercises any behaviour specific to these values.

**Suggested fix:**

If the intent is forward-compat scaffolding, add a comment marking them as reserved. If they are genuinely not planned, remove them to reduce the API surface a caller can accidentally mis-use (e.g. `"xhigh"` is not a documented OpenRouter effort level as of this audit).

---

### [LOW][DEAD-CODE] firstSeen / order fields in accumulator are written but never read

**Location:** `internal/llm/openai_compat/accumulate.go:26,32,41,53–54`

**Confidence:** high

**Detail:**

`toolCallAcc.firstSeen int` is assigned (`firstSeen: a.order`) when a new index is first seen, and `accumulator.order int` is incremented with each new index. The comment says "records arrival order as a stable tiebreaker," but `finalize()` sorts only by the wire index (`sort.Ints(indices)` — line 83) and never reads `firstSeen` or `order`. The two fields are entirely inert.

**Suggested fix:**

Remove `firstSeen` from `toolCallAcc` and `order` from `accumulator`. If arrival-order tiebreaking is needed in the future (e.g. for a provider that sends out-of-order indices), add it back then.

---

### [LOW][DEAD-CODE] Loop-variable shadow `tc := tc` is a no-op in Go 1.22+

**Location:** `internal/llm/openai_compat/sse.go:73`

**Confidence:** high

**Detail:**

```go
for _, tc := range acc.finalize() {
    tc := tc   // line 73: pre-1.22 loop-capture guard
    if !emit(llm.Chunk{ToolCall: &tc}) {
```

`go.mod` declares `go 1.26.4`. Since Go 1.22 the loop variable is scoped per iteration; the re-declaration is a no-op. It is harmless but constitutes dead code that misleads readers into thinking a capture problem exists.

**Suggested fix:**

Remove the `tc := tc` line. Modern Go loop semantics make it unnecessary.

---

### [LOW][NOT-WIRED] SupportsAudio is tested but never called in production code

**Location:** `internal/llm/models.go:51–53`

**Confidence:** high

**Detail:**

`SupportsAudio(model string) bool` is defined and tested in `internal/llm/models_test.go`. Grep across `D:/Aura/**/*.go` finds zero non-test, non-definition references: the function is not called by any production package (channels, agent, config, runner, cmd/aura). The doc-comment acknowledges "audio is sidecar-routed this phase" and the function "exists for forward-compat symmetry," but there is no wiring site at all — not even a guarded branch.

**Suggested fix:**

This is intentional forward-compat scaffolding per the doc-comment; keep it. Consider adding a `//nolint:unused` annotation or a `_ = SupportsAudio` compile-time check test to make the intent explicit and prevent a future linter run from suggesting removal.

---

## What was checked and found clean

- **Resource leaks**: `resp.Body` is closed in a `defer` inside the goroutine regardless of `parseSSE` outcome. The `newHTTPError` path closes the body before returning. No unclosed file, row, or ticker found.
- **Goroutine leaks**: The `emit` select on `ctx.Done()` ensures the stream goroutine exits on cancellation. `DisableKeepAlives: true` prevents lingering `persistConn` goroutines.
- **Context propagation**: `http.NewRequestWithContext` carries the caller's ctx through to the transport; body read unblocks on cancel.
- **Races**: The `accumulator` is single-goroutine (owned by the one stream goroutine). `Client.cfg` is read-only after construction. `modelCapabilityTable` is a package-level var written only at init. No shared mutable state found.
- **JSON/nil safety**: `wireChunk` uses value types for Choices; `usageWire.Cost` is `*float64` and is correctly propagated as a pointer. `json.Unmarshal` on `[]byte(payload)` allocates a new slice — no aliasing risk.
- **Config load-order**: The 4-tier precedence is correctly implemented; `applyEnvOverrides` is fail-fast on malformed numerics; `envBool` is intentionally non-fatal. `ErrMissingAPIKey` is a proper sentinel compared with `errors.Is`.
- **Price table**: `defaultPrices()` returns a fresh map each call — no mutation of a shared seed.
- **`SupportsVision`**: `normalizeModelID` strips `:suffix` and whitespace before a full-id map lookup — the conservative-false-for-unknown-models invariant is correct.
- **`CostUSDValue` / `CostUSD`**: Delegation is correct; `"n/a"` is returned (not "$0") for unknown models.
