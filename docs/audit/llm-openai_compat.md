# Audit: internal/llm/openai_compat

**Verdict:** needs-work — two dead-code items; no bugs, no races, no missing wiring
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

### [MEDIUM][DEAD-CODE] `toolCallAcc.firstSeen` and `accumulator.order` are written but never read

**Location:** `internal/llm/openai_compat/accumulate.go:32,40,53-54`
**Confidence:** high

`toolCallAcc` carries a `firstSeen int` field (line 32). `accumulator` carries an `order int` counter (line 40). In `add()` (lines 53-54), every new index-entry is stamped with `firstSeen: a.order` and `a.order` is incremented. Neither `firstSeen` nor `order` is ever read anywhere in the package or the wider repo (grep across `D:/Aura` confirms zero non-definition, non-test references to `.firstSeen`).

`finalize()` sorts accumulated calls by wire index (`sort.Ints(indices)`, line 83), ignoring `firstSeen` entirely. The doc-comment on `toolCallAcc` says "firstSeen records arrival order as a stable tiebreaker" — that role is never exercised.

**Suggested fix:** Remove `firstSeen int` from `toolCallAcc`, remove `order int` from `accumulator`, and remove the write `firstSeen: a.order` / `a.order++` in `add()`. If arrival-order tiebreaking is desired in the future, re-add it with a read site.

---

### [LOW][DEAD-CODE] Redundant loop-variable shadow `tc := tc` in Go 1.26

**Location:** `internal/llm/openai_compat/sse.go:73`
**Confidence:** high

```go
for _, tc := range acc.finalize() {
    tc := tc  // shadow copy — unnecessary in Go ≥ 1.22
    if !emit(llm.Chunk{ToolCall: &tc}) {
```

Go 1.22 (loopvar fix, enabled by default from 1.22) makes each loop iteration's `tc` a distinct variable, so `&tc` in `emit()` is already stable per-iteration. `go.mod` declares `go 1.26.4`. The shadow copy is a no-op — not harmful, but it's misleading defensive code.

**Suggested fix:** Remove the `tc := tc` line.

---

## Clean

The following categories were checked and found clean:

**Bugs:** `parseSSE` correctly handles partial reads (both `line` and `readErr` from `bufio.Reader.ReadString`), the `[DONE]` sentinel is caught before `json.Unmarshal`, `io.EOF` is compared by identity (correct for `bufio.Reader`), and `finalize()` cannot be called twice in one stream. The `newHTTPError` body-read / body-close sequence is correct (read in `newHTTPError`, closed by the caller at `client.go:125` — one close, not two). The final `emit(llm.Chunk{Usage: &u})` at `client.go:164` discards its return value safely — it is the last goroutine statement; either path (ctx cancelled → false, consumer draining → true) exits cleanly via `defer close(out)`. The `wireRequest` struct excludes the API key (set only on the Authorization header), so wire-body tracing cannot leak credentials.

**Races:** The stream goroutine is the sole owner of `accumulator`, `parseResult`, and `resp.Body`; no shared mutable state is accessed concurrently. `reasoningtrace.Record` is mutex-guarded. The `emit` closure selects on a buffered channel and `ctx.Done()` — both goroutine-safe primitives.

**Not-wired:** `Client.Stream` is wired in `cmd/aura/chat.go`, `internal/runner/live_e2e_test.go`, `internal/agent/live_finalize_test.go`, and multiple eval harnesses. `HTTPError` is used as a typed error by callers via `errors.As`. `Usage`/`toLLMUsage` projects into `llm.Usage` consumed by the agent's `consume()`. `newHTTPError` is called on every non-2xx response. No exported or unexported symbol in the package is unconnected to production flow (other than the `firstSeen` field documented above).

**`ToolsCacheControl` in `llm.Request` ignored here:** intentional by design — it is an Anthropic-direct marker, documented in `llm/client.go:112-116`, consumed by the future Anthropic-native wire client only.
