# Audit: internal/conversations

**Verdict:** needs-work — three actionable findings (one data-corruption risk, two not-wired gaps)

**Counts:** critical 0 / high 0 / medium 1 / low 3

---

## Findings

### [MEDIUM][BUG] `numericFromFloat` silently corrupts cost on NaN/Inf/overflow input

**Location:** `internal/conversations/store_helpers.go:151-161`

**Confidence:** high

**Detail:**

```go
func numericFromFloat(f float64) (pgtype.Numeric, error) {
    scaled := f * 1e4
    if scaled >= 0 { scaled += 0.5 } else { scaled -= 0.5 }
    mantissa := big.NewInt(int64(scaled))   // ← silent UB on overflow
    return pgtype.Numeric{...}, nil         // ← error return never fires
}
```

`int64(float64)` is implementation-defined when the value lies outside `[math.MinInt64, math.MaxInt64]`. In practice, Go on amd64 produces `math.MinInt64` (-9223372036854775808) for both `+Inf` and any value ≥ ~9.2×10¹⁸. If a provider emits an enormous or NaN usage cost (parsing bug, misbehaving model), the conversion wraps silently and stores a massive negative delta into `total_cost_usd` — a write that commits successfully. The function's `error` return is dead: no code path inside it returns a non-nil error.

The test suite only exercises benign values (0 – 9999.9999).

**Suggested fix:**

```go
func numericFromFloat(f float64) (pgtype.Numeric, error) {
    if math.IsNaN(f) || math.IsInf(f, 0) {
        return pgtype.Numeric{}, fmt.Errorf("numericFromFloat: non-finite value %v", f)
    }
    scaled := f * 1e4
    if scaled >= 0 {
        scaled += 0.5
    } else {
        scaled -= 0.5
    }
    // Guard against values that overflow int64 (~$9.2e14 after scaling).
    if scaled > float64(math.MaxInt64) || scaled < float64(math.MinInt64) {
        return pgtype.Numeric{}, fmt.Errorf("numericFromFloat: value %v overflows int64 mantissa", f)
    }
    mantissa := big.NewInt(int64(scaled))
    return pgtype.Numeric{Int: mantissa, Exp: -numericScale, Valid: true}, nil
}
```

---

### [LOW][NOT-WIRED] `ConversationCleaner` interface / `Config.Cleaner` field never injected in production

**Location:** `internal/conversations/store.go:49-58, 82`

**Confidence:** high

**Detail:**

The `ConversationCleaner` interface and `Config.Cleaner` field exist to inject `sandbox.WorkspaceManager.PurgeConversationDir` — the symlink-safe `os.Root` cascade that protects against path-traversal via a symlink in the workspace dir (documented as "ROADMAP crit 2, landmine #4"). Every production call site sets `Cleaner` to nil:

- `cmd/aura/chat.go:137-140`: `conversations.Config{RunDir: ..., TurnCapBytes: ...}` — no `Cleaner`
- `internal/agui/server_integration_test.go:116, 150, 187`: same
- `internal/runner/live_e2e_test.go:138`: same
- `internal/eval/skills_snippet_reuse_cot_eval_test.go:241, 378`: same

As a result, `Delete` always falls through to the `os.RemoveAll` fallback (store.go:542-552), and the security comment about "the os.Root no-follow cascade" (D-26) never executes. An attacker-planted symlink inside a conversation's sidecar workspace could be followed by `os.RemoveAll`, redirecting the removal outside `runDir`.

**Suggested fix:** Wire `Cleaner: sandboxWorkspaceManager` at the composition root in `cmd/aura/chat.go` (and any other production `conversations.New` call) once `sandbox.WorkspaceManager` is available at that call site. If the sandbox module is intentionally unavailable at `chat.go`, document this explicitly and accept the `os.RemoveAll` fallback as the sole path (then remove the interface or add a compile-time assertion that it is satisfied at boot).

---

### [LOW][NOT-WIRED] `ErrContextWindowExceeded` exported sentinel never inspected by callers

**Location:** `internal/conversations/context.go:49`

**Confidence:** high

**Detail:**

`ErrContextWindowExceeded` is exported and documented as "a normal-flow error the REPL surfaces (suggesting `aura chat new`), NEVER the iter.Seq2 error slot." The intent is that callers use `errors.Is(err, conversations.ErrContextWindowExceeded)` to display a friendly message.

No caller outside the package does this:
- `internal/runner/runner.go:226-230`: `yield(nil, err)` — propagated opaque
- `cmd/aura/chat.go`: no `errors.Is` check on the Turn iterator's error slot
- `internal/channels/telegram/`: no check

The user receives the raw error string which happens to mention "aura chat new" — but only because the message is embedded in the sentinel itself. This means: (a) there is no way for the caller to distinguish a context-window overflow from a DB or network error without string-matching, and (b) the exported symbol serves no purpose beyond what the string already provides.

**Suggested fix:** Either (a) add `errors.Is(err, conversations.ErrContextWindowExceeded)` checks in the runner's Turn consumer and in Telegram's dispatch loop so the user gets a typed, channel-appropriate response, or (b) unexport the sentinel (`errContextWindowExceeded`) since no caller uses it for branching.

---

### [LOW][BUG] `applyL1` eviction threshold uses seq-number delta, not turn-count distance

**Location:** `internal/conversations/context.go:207-219`

**Confidence:** medium

**Detail:**

```go
maxSeq := out[len(out)-1].Seq
threshold := maxSeq - evictAfter
for i := range out {
    t := &out[i]
    if t.Seq == 1 || t.Role != llm.RoleTool { continue }
    if t.Seq < threshold {
        t.Content = readToolOutputPointer(t.ToolCallID)
    }
}
```

The doc says "evictAfter turns (by turn distance from the newest)". But `threshold` is a seq-number, not a turn-count. When seqs are non-contiguous (gaps are possible: see `context_test.go`'s `Seq: 20` after `Seq: 4`), the distance in seq-number space is much larger than the distance in turn-count space. In the test fixture `{seqs: 1,2,3,4,20, evictAfter: 10}`: `threshold = 20 - 10 = 10`; the tool turn at seq=4 is evicted because `4 < 10` even though it is only 1 position from the tail in turn-count. The test asserts this behavior (so it's not a regression), but the stated semantic ("evict tool outputs older than `evictAfter` turns from the newest turn") is silently violated for gapped sequences, causing more aggressive eviction than intended.

**Suggested fix:** Track `evictAfter` as a count-from-tail over the `out` slice, not a seq-threshold. Alternatively, document that `ToolEvictAfterTurns` is a seq-distance budget (renaming the constant and config knob to `ToolEvictSeqThreshold`) so callers can calibrate correctly.

---

## What was checked and found clean

- **Nil-pointer paths**: `Store` fields (`pool`, `q`, `runDir`) are only populated via `New`; all exported methods gate on `parseUUID` before hitting the DB. No nil-deref path found.
- **Resource leaks**: No `sql.Rows`, HTTP `Body`, or file descriptors left unclosed. `ListTurnsBySeq` is a sqlc-generated query that returns a materialized slice (no rows iterator to close). `dirSize` uses `filepath.WalkDir` which manages its own handles. `generateTitle` drains the Stream channel fully.
- **Concurrency / races**: No goroutines are spawned in this package. The `sync.Once`-guarded encoder (`tiktoken.go:63-74`) is the sole shared mutable state and is correctly protected.
- **Sidecar spill atomicity**: The two-phase write (file → DB tx) is sound: if the DB tx fails after the file write, the orphan scan reconciles at next boot. The inverse (DB records a path for a file that was never written) cannot happen because `appendTurnWrites` returns an error on file failure before the tx runs.
- **Path traversal**: `validateID` correctly rejects `..`, `/`, `\`, and empty strings before any `filepath.Join`. `sidecarDir` calls it before constructing any path. `ScanOrphans` uses `os.Lstat` (not `os.Stat`) so symlinks are unlinked as links, not traversed.
- **`dropOldestPairs` termination**: The loop terminates because each iteration drops 2 elements from `body`; the guard `len(body) >= 2` ensures termination when only 0 or 1 turns remain.
- **`injectAlwaysBlock` slice aliasing**: Returns a fresh `[]Turn` via `make`+`append`; does not alias the input slice.
- **Dead unexported symbols**: All unexported helpers (`conversationFromRow`, `turnFromRow`, `turnToMessage`, `maybeSpill`, `writeTurnSidecar`, `optionalText`, `floatFromNumeric`, `numericFromFloat`, `validateID`, `parseUUID`, `decodeToolCalls`, `readToolOutputPointer`, `isAlwaysBlock`, `applyL1`, `applyContextLadder`, `injectAlwaysBlock`, `dropOldestPairs`, `totalTokens`, `countTokens`, `toMessages`, `hardCap`, `insertContextRotEvent`, `rotEmitter`, `encoder`, `parseTiktokenBPE`, `countRotEvents`) are referenced either within the package or via exported wrappers. No dead unexported symbol found.
