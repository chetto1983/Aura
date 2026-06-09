# Audit: internal/llm

**Verdict:** needs-work — one medium bug (swallowed mid-stream parse error), one low dead-code field, two low dead-code constants.

**Counts:** critical 0 / high 0 / medium 1 / low 3

## Findings

---

### [MEDIUM][BUG] mid-stream parse error silently swallowed — channel closes without any error signal

**Location:** `internal/llm/openai_compat/client.go:145–165`

**Confidence:** high

**Detail:**

`parseSSE` can return a non-nil error for two causes: a malformed SSE chunk (JSON parse failure) and a non-EOF transport read failure. In both cases the `Stream` goroutine captures the error into `errString`, logs it to the trace file, and exits — closing `out` with no in-band error signal.

```go
res, parseErr := parseSSE(resp.Body, emit)
errString := ""
if parseErr != nil {
    errString = parseErr.Error()   // logged only
}
reasoningtrace.Record(...)         // trace only
if res.hasUsage {
    u := res.usage.toLLMUsage()
    emit(llm.Chunk{Usage: &u})     // still emits usage even on error
}
// goroutine exits → close(out)
```

The consumer (`llm_agent.go:consume`) iterates `for c := range ch` and simply terminates when the channel closes, returning `finish = ""` (empty finish reason) and zero usage. The agent loop then treats this as a normal empty-response turn and routes to `maybeRecoverEmptyResponse()` or `finalize()` — masking an infrastructure failure as a model-side empty reply. A malformed-JSON chunk mid-stream (e.g., a provider sending a partial chunk on network failure) is therefore indistinguishable to the agent from a clean context-cancel or an intentional empty response.

The `llm.Client.Stream` doc comment says "closes the channel on [DONE], EOF, or ctx-cancel" — parse errors are not listed, and there is no `Chunk.Err` field in the interface.

**Suggested fix:**

Add a sentinel chunk type to signal in-band parse errors, or add an `Err` field to `llm.Chunk`:

```go
type Chunk struct {
    // ... existing fields ...
    Err error // non-nil on a mid-stream parse/transport error
}
```

Then in the goroutine:

```go
if parseErr != nil {
    emit(llm.Chunk{Err: parseErr})
}
```

And in `consume`, check `c.Err != nil` and propagate it as an infra failure via `yield(nil, err)`. Alternatively, keep the current channel-based design but update the doc comment to document the silent-close-on-error behavior so callers can detect truncation via `finish == ""` and missing usage.

---

### [LOW][DEAD-CODE] `toolCallAcc.firstSeen` field written but never read

**Location:** `internal/llm/openai_compat/accumulate.go:26–54`

**Confidence:** high

**Detail:**

The `toolCallAcc` struct has a `firstSeen int` field and `accumulator.order int` counter that are both written in `add()`:

```go
acc = &toolCallAcc{firstSeen: a.order}
a.order++
```

The field is documented as "records arrival order as a stable tiebreaker." However, `finalize()` sorts by `index` (via `sort.Ints(indices)`), never by `firstSeen`. The `firstSeen` field and `a.order` counter are dead — their values are never consumed.

Verified with `grep -r firstSeen D:/Aura --include="*.go"`: only two write sites, zero read sites.

**Suggested fix:**

Remove the `firstSeen int` field from `toolCallAcc` and the `order int` field from `accumulator`. If arrival-order tiebreaking is ever needed (parallel multi-call streams where indices collide), add it back with a sorting key in `finalize()`.

---

### [LOW][DEAD-CODE] `ReasoningEffortXHigh` and `ReasoningEffortMinimal` — exported constants with zero non-definition references

**Location:** `internal/llm/client.go:133,137`

**Confidence:** high

**Detail:**

```go
ReasoningEffortXHigh   ReasoningEffort = "xhigh"
ReasoningEffortMinimal ReasoningEffort = "minimal"
```

Verified with `grep -r "ReasoningEffortXHigh\|ReasoningEffortMinimal" D:/Aura --include="*.go"`: only the definition lines match. No production code, no test code references them.

`ReasoningEffortMedium` (`"medium"`) is similarly unused: `grep "ReasoningEffortMedium"` returns only its definition line.

These are forward-compat stubs for effort levels that neither `reasoning_policy.go` nor any caller currently exercises. They carry no runtime cost but pollute the exported API surface.

**Suggested fix:**

Either document them explicitly as "reserved for future use" (a `//nolint:unused` comment or a `_ = ReasoningEffortXHigh` compile-check in a `_test.go` file to confirm they build), or remove them until the caller that needs them is written. At minimum, add a comment stating they are intentionally unused stubs, so a future linter run does not report a false finding.

---

### [LOW][DEAD-CODE] `ReasoningEffortMedium` — exported constant with zero non-definition references

**Location:** `internal/llm/client.go:135`

**Confidence:** high

**Detail:**

See llm-3 above. Listed separately for tracking clarity. `ReasoningEffortMedium = "medium"` has zero references outside its own definition line.

**Suggested fix:** Same as llm-3.

---

## Clean sections

The following aspects were checked and found clean:

- **Nil-pointer / unchecked errors:** `Load()` guards every returned pointer before use; `applyFileConfig` / `overlayFile` handle nil `fileConfig` fields correctly. `newHTTPError` calls `io.ReadAll` on a `LimitReader` — no unbounded read. `resp.Body.Close()` is always deferred after a successful `Do()`.
- **Context propagation:** `http.NewRequestWithContext` carries `ctx` into the HTTP round-trip; `bufio.Reader.ReadString` unblocks when the connection closes on ctx-cancel (~100ms). The `emit` select properly exits on `ctx.Done()`.
- **Goroutine leaks:** The one goroutine spawned by `Stream` is guaranteed to exit: `resp.Body.Close()` is deferred, and the `emit` select exits on ctx-cancel. `DisableKeepAlives: true` prevents lingering `persistConn` goroutines.
- **Resource leaks:** `resp.Body` is closed either by `newHTTPError` (non-2xx) or by the `defer func() { _ = resp.Body.Close() }()` in the goroutine (2xx).
- **Races:** The goroutine closes over `ctx`, `out`, and `resp` — all of which are owned exclusively by the goroutine after launch; `ctx` is read-only from the goroutine side. `accumulator` is single-goroutine by design.
- **Config load chain:** The 4-tier precedence (default < .env < llm.json < env overrides) is correct; numeric env overrides are fail-fast; the bool toggle is non-fatal (intentional). `overlayFile` never clobbers defaults with a nil pointer.
- **Price / cost logic:** `CostUSDValue` short-circuits to the provider's reported cost when non-nil (correct D-18 precedence); the table fallback uses exact key match — consistent with all callers passing `cfg.Model` verbatim (which carries the full `:exacto` suffix).
- **SSE framing:** `bufio.Reader.ReadString('\n')` avoids the `bufio.Scanner` 64 KiB token cap; `[DONE]` is checked before `json.Unmarshal`; `:` comment lines and blank lines are skipped; EOF-without-DONE finalizes correctly.
- **Tool-call accumulation:** Delta concatenation in `add()` and `sort.Ints`-ordered emission in `finalize()` are correct; the `index`-based sort is deterministic.
- **Model capability table:** `normalizeModelID` strips `:` suffixes before the map lookup; the conservative `false` default for unknown models is correct for threat T-13-03-UnknownModelVision.
