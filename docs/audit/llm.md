# Audit: internal/llm

**Verdict:** needs-work — four low-severity issues found; no critical or high-severity defects.

**Counts:** critical 0 / high 0 / medium 1 / low 3

## Findings

---

### [MEDIUM][DEAD-CODE] `firstSeen` field written but never read

**Location:** `internal/llm/openai_compat/accumulate.go:32,53`

**Confidence:** high

**Detail:**
`toolCallAcc.firstSeen` is set on line 53 (`acc = &toolCallAcc{firstSeen: a.order}`) but
is never read by any method, including `finalize()`. The `finalize()` method sorts by
`index` (delta wire index) via `sort.Ints(indices)`, not by `firstSeen`. The comment
on line 26 says it "records arrival order as a stable tiebreaker" — but no tiebreaking
logic exists. The `accumulator.order` counter is incremented for this field only;
removing `firstSeen` would allow removing `order` too.

**Suggested fix:**
Remove the `firstSeen` field from `toolCallAcc` and the `order` field from `accumulator`.
The sort is already deterministic: tool-call indices are stable integers emitted by the
model in ascending order. If insertion-order tiebreaking is ever needed, reintroduce
with the actual sort key.

```go
// Before
type toolCallAcc struct {
    id, typ, name, args string
    firstSeen           int   // dead — remove
}
type accumulator struct {
    byIndex map[int]*toolCallAcc
    order   int   // dead — remove
}

// In add():
acc = &toolCallAcc{firstSeen: a.order}   // dead — simplify to &toolCallAcc{}
a.order++                                 // dead — remove

// After
type toolCallAcc struct{ id, typ, name, args string }
type accumulator struct{ byIndex map[int]*toolCallAcc }
```

---

### [LOW][DEAD-CODE] `tc := tc` self-shadow is dead in Go 1.22+

**Location:** `internal/llm/openai_compat/sse.go:73`

**Confidence:** high

**Detail:**
`go.mod` declares `go 1.26.4`. Since Go 1.22 each `for _, tc := range` iteration
already captures a fresh copy of `tc`, so the self-shadowing line `tc := tc` on line 73
is a no-op. Taking `&tc` on line 74 is already safe without the shadow. The line was a
pre-1.22 loop-escape idiom that is now dead code and misleading to readers.

**Suggested fix:**
Delete line 73 (`tc := tc`).

---

### [LOW][DEAD-CODE] `ReasoningEffortXHigh` and `ReasoningEffortMinimal` constants never referenced

**Location:** `internal/llm/client.go:133,137`

**Confidence:** high

**Detail:**
`ReasoningEffortXHigh = "xhigh"` and `ReasoningEffortMinimal = "minimal"` are exported
constants in the `ReasoningEffort` block. A repo-wide search confirms no production file
(excluding the defining file and tests) references either constant. The `reasoning_policy.go`
consumer only uses `ReasoningEffortLow`, `ReasoningEffortNone`, `ReasoningEffortMedium`,
and `ReasoningEffortHigh`. The two dead constants represent effort levels that are
defined in the vocabulary but unimplemented in the adaptive policy.

**Suggested fix:**
Either remove both constants, or add a `// Forward-compat; unused until policy X` comment
and register them in `reasoning_policy.go`'s effort ladder when the policy is extended.
Leaving them silently undefined in the policy creates a footgun: a caller that sets
`Effort: ReasoningEffortXHigh` gets a value that OpenRouter may or may not honour but
that Aura's own policy never selects.

---

### [LOW][NOT-WIRED] `SupportsAudio` exported function has no production caller

**Location:** `internal/llm/models.go:51-53`

**Confidence:** high

**Detail:**
`SupportsAudio` is exported and documented as a forward-compat stub ("audio is
sidecar-routed this phase"). A repo-wide search finds it referenced only in its
definition and in `models_test.go`. No production file in any package calls it.
`SupportsVision` (same file) has genuine callers in `internal/channels/telegram/`.

This is an intentional stub by design (see the comment), so it is not a defect.
It is recorded here so the fixer can decide: either annotate it with a
`//nolint:deadcode` or a `// Stub — wired by Phase N` comment, or delete it and
re-add when audio routing moves from sidecar to model.

**Suggested fix:**
Add `// Stub: audio is sidecar-only this phase. Wire when a native-audio model is added.`
to the godoc, or delete and re-add at Phase 13 / audio model introduction. No functional
change required.

---

## Clean areas checked

- **Goroutine / resource leaks:** `Stream` goroutine closes `resp.Body` via `defer`, and
  exits on `ctx.Done()` via the `emit` select — no leak path found. `DisableKeepAlives`
  suppresses persistConn goroutines. No ticker or timer created anywhere in the package.
- **Nil-pointer derefs:** `Usage.Cost` is a `*float64` properly nil-checked at every
  call site. `providerCost` is guarded before deref in `CostUSDValue`.
- **Error handling:** All `json.Unmarshal`, `os.ReadFile`, `strconv.Atoi/ParseFloat`
  errors are propagated. The `godotenv.Load()` best-effort blank-assign is intentional
  and documented. `resp.Body.Close()` errors are intentionally ignored (standard
  close-after-read idiom).
- **Race conditions:** `accumulator` is single-goroutine (one stream goroutine owns it).
  `reasoningtrace.Record` uses a package-level mutex. `Config` is value-copied into
  `Client.cfg` at `New()` time — no shared mutable state.
- **Context propagation:** `http.NewRequestWithContext(ctx, ...)` propagates the caller's
  context end-to-end; cancellation unblocks `resp.Body.Read` within the transport's
  cancel window (~100ms).
- **Price / cost logic:** `CostUSDValue` and `CostUSD` correctly prefer
  `providerCost` over table lookup, and return `(0, false)` for unknown models.
  Price table key `"deepseek/deepseek-v4-flash:exacto"` matches the default
  `Config.Model`; the design is intentionally suffix-exact (unlike the capability table
  which normalizes). No mismatch with current callers.
- **Config load:** `Load()` 4-tier precedence is correctly ordered. `envBool` is
  non-fatal by design (documented). `envInt`/`envFloat` are fail-fast on
  set-but-malformed values. Missing `.env` and missing `~/.aura/llm.json` are
  both handled gracefully.
