# Telegram tool-call status pane — debug & fix
Date: 2026-05-17 (same day as the broken ship)
Scope: the bug behind "non funziona" on commits `2e2e170f` + `782df120` + `8e3e4887`
Reader: assumes you wrote `statusPane` and `Outbound.ConsumeStream` an hour ago

## 1. Bug analysis — confirmed, and double-broken

### The hypothesis is correct. Evidence:

`readyToFlush` in [outbound.go:113-121](../internal/channels/telegram/outbound.go) returns true on **two** disjoint paths:

```go
if sb.Len() >= streamingMinThreshold { return true }                          // 30 chars CONTENT
if sb.Len() == 0 && cotBuf.Len() >= streamingReasoningMinThreshold { return true } // 8 chars REASONING
```

`flush()` at [outbound.go:137-145](../internal/channels/telegram/outbound.go) treats *any* `readyToFlush` true as "we're committing to a content edit" and unconditionally calls `pane.EnterContentMode()` on the first such flush:

```go
if msg == nil && pane != nil { pane.EnterContentMode() }
```

`EnterContentMode` at [status_pane.go:152-164](../internal/channels/telegram/status_pane.go) sets `contentMode = true` permanently and archives any active round.

`OnToolStart` at [status_pane.go:102-107](../internal/channels/telegram/status_pane.go) hard-guards on `contentMode`:

```go
if p.finalized || p.contentMode { return }
p.activeRound = append(...)   // skipped
```

That means once `EnterContentMode` has fired, `OnToolStart` is a no-op — **including the `append`**. The entry never lands in `activeRound`. Then `OnToolEnd` ([status_pane.go:128-140](../internal/channels/telegram/status_pane.go)) loops over `activeRound` looking for the matching callID; finds nothing; the mutation silently no-ops. `roundComplete()` returns false (activeRound empty). Nothing archives. `roundHistory` stays empty.

### Trace — reasoning LLM, 1 round, 1 tool

Modern reasoning models (GPT-5 reasoning, DeepSeek-R1, gpt-oss-120b, anything routed through OpenRouter with `reasoning_effort` set) emit `delta.reasoning` BEFORE the `tool_calls` are completely formed. The `internal/llm` `Stream` interface accumulates fragmented tool-call JSON until they're whole, so `Token.ToolCalls` only appears on the *terminal* `Done` token of a round. Reasoning tokens stream throughout.

Concretely for `cmd/probe_chat "trova in memoria phantom guard"`:

| t | event source | sb.Len | cotBuf.Len | flush() acts | pane state |
|---|---|---|---|---|---|
| 0ms | Stream open | 0 | 0 | — | clean, contentMode=false |
| 100ms | `Reasoning: "Devo cercare in"` | 0 | 16 | `readyToFlush`=true (CoT branch) → `EnterContentMode()` → edit body `🧠 _Devo cercare in_` | **contentMode=true**, activeRound empty |
| 200ms | `Reasoning: " memoria con search_memory"` | 0 | 42 | within throttle, coalesced | unchanged |
| 600ms | `Done` + `ToolCalls=[search_memory]` | 0 | 42 | exits ConsumeStream, returns `HasToolCalls=true` | unchanged |
| 605ms | loop dispatches → `OnToolStart("c1","search_memory",["query"])` | — | — | — | **guard returns early — entry NOT appended** |
| 1200ms | tool returns → `OnToolEnd("c1",true,595ms,"")` | — | — | — | matching loop finds nothing; `roundComplete()` false (empty); no flush; **state lost** |
| 1300ms | Stream round 2: `Content: "Phantom guard è una protezione…"` (long enough) | 50+ | 42 | first content flush — `msg != nil` so `EnterContentMode` is skipped (idempotent), `withFooter` empty (`roundHistory` empty → `FooterMarkdown=""`) → body = `🧠 _Devo cercare…_\n\nPhantom guard è…` | unchanged |
| 1900ms | `Done` (no tool calls) | … | … | final clean-answer edit — drops both CoT prefix and footer (footer is empty anyway) | unchanged |

**User sees**: the reasoning text mid-turn, then the final answer. **Identical to pre-Slice-2 behavior.** No "🛠 Sto lavorando…", no blockquote, no footer. From their POV nothing shipped. That's the "non funziona".

### Trace — non-reasoning LLM (why the E2E test passes)

If the LLM emits zero reasoning, `cotBuf` stays empty during the tool phase. `OnToolStart` runs **before** `Stream` is even called the first time (no — wait, it runs AFTER the first `Stream` returns its tool_calls via Done). For a tool-only round there's no `Content` and no `Reasoning` — `flush()` never fires inside that round. So `EnterContentMode` doesn't fire. `OnToolStart` runs after round 1 ends — it lands cleanly because contentMode is still false. The pane writes the blockquote. **This is exactly the path covered by `TestStatusPane_E2E_ToolOnlyTurnLeavesPaneInControl`** — which is why the test is green and the production behavior is broken for reasoning models.

The E2E was written under the implicit assumption "reasoning never coexists with tool calls in the same round". That assumption is false for every modern reasoning LLM.

### Second-order bugs found while reading

**B2 — Footer permanently empty after the race.** Even when content eventually flows and `withFooter` is called, `composeFooterTextLocked` returns `""` because `len(roundHistory) == 0` (the entries never made it into the round, so the round never archived). Tools fired but the footer claims zero tools used. Silent data loss.

**B3 — `OnToolEnd` comment is wrong.** Status pane [line 126-127](../internal/channels/telegram/status_pane.go): *"Even in contentMode, we still mutate state so Footer() reports accurate counts."* This is the *intended* behavior. But the precondition (the entry was appended by OnToolStart) is broken by the symmetric guard on OnToolStart. The two methods have inconsistent guards.

**B4 — `EnterContentMode` archives empty activeRound silently.** `archiveRound()` at [line 230-251](../internal/channels/telegram/status_pane.go) early-returns when activeRound is empty, so no malformed entry is appended. Not a bug per se but it means there's no observable trace in roundHistory either — making the issue invisible in logs.

**B5 — Reasoning-only flush edits the placeholder with no rollback path.** Once `🧠 _reasoning_` lands in the placeholder, if the next round's content is short (`<30 chars`) AND no further reasoning arrives (`<8 more chars`), no second flush ever fires. The placeholder is stuck on the reasoning text from round 1 until `EventFinal` does its cleanup. Not the user's reported bug but worth knowing.

**B6 — Throttle is NOT actually shared.** Outbound's `lastEdit` is a local `time.Time` (line 110), not the pane's. `pane.MarkEdited()` is called after Outbound edits, but Outbound never reads `pane.lastEdit`. So if pane edits at t=0, Outbound is free to edit at t=100ms — a 500ms-too-early write. Currently safe only because in the broken state the pane stops editing immediately. After the fix, this race will appear unless explicitly handled.

## 2. Ownership models — comparison matrix

The design question: when reasoning + tool calls coexist in a single placeholder, who owns the body composition?

Aura constants: one `*tele.Message` placeholder per turn; 600ms shared throttle target; `expandable_blockquote` available; MarkdownV2 via tgmd; existing `composeStreamingMessage(cot, content)` builds `🧠 _cot_\n\ncontent`; `pane.FooterMarkdown()` returns `_🛠 N strumenti usati…_`.

### Model A — current "first-flush wins" (status quo, broken)

Whoever flushes first owns the placeholder forever. CoT triggers `EnterContentMode`. **Broken for reasoning + tool turns**, as traced above. Reject.

### Model B — split `EnterContentMode` into `EnterReasoningMode` + `EnterContentMode`

Pane retains placeholder ownership during reasoning-only AND tool phases. Hands off only when *narrative content* (not reasoning) arrives (`sb.Len() >= streamingMinThreshold` becomes the sole trigger). `cotBuf >= 8` no longer triggers handoff — instead it triggers pane to render `🧠 _reasoning_` ABOVE the status blockquote.

| Aspect | Detail |
|---|---|
| Reasoning-only | Outbound composes `composeStreamingMessage(cot, "")` → `🧠 _cot_`. **But who edits the placeholder?** Either: (a) pane composes by reading cotBuf — couples them; (b) Outbound edits and pane stops; same race. |
| Reasoning + tool | Pane has full control; needs to know CoT exists to render it above the blockquote. Couples pane to LLM stream state. |
| Tool-end → content | Pane hands off cleanly when sb crosses 30 chars. Outbound then prepends `FooterMarkdown()`. |
| Race risk | Medium — still two writers, gated by a phase flag that must flip atomically. Coupling pane to cotBuf adds API surface. |
| Cost | ~80 LOC; new `EnterReasoningMode` method; pane needs a CoT-string injection point. |
| Tests | E2E test for reasoning-only + tool sequence; coupling test; existing tests untouched if the new mode is additive. |

### Model C — single owner (pane) for the whole pre-content phase

The pane is the sole writer to the placeholder until the first narrative *content* token (≥30 chars). Outbound's `flush()` does NOT call `EnterContentMode` on reasoning — only on content. During reasoning-only or tool-only or reasoning+tool phases, **Outbound never edits the placeholder.** Pane composes the body by combining its own status block with an optional CoT prefix it pulls from a shared accumulator.

| Aspect | Detail |
|---|---|
| Reasoning-only | Outbound accumulates `cotBuf` silently. Pane sees the cotBuf (via a callback or shared pointer) and renders `🧠 _cot_\n\n🛠 Sto lavorando…\n…blockquote…`. Pane edits at throttle. |
| Reasoning + tool | Pane natively renders both: CoT prefix + blockquote + history. One writer, no race. |
| Tool-end → content | First content token crossing 30 chars → Outbound calls `EnterContentMode()`, takes over, prepends `FooterMarkdown()`. Pane stops. |
| Race risk | Low — one writer per phase, atomic flip. |
| Cost | ~50-80 LOC: split `EnterContentMode` into a content-only trigger; pane accepts a `cotProvider func() string` callback; Outbound stops calling Telegram for reasoning-only flushes. |
| Tests | Existing reasoning-only fixture (`with_cot`) **changes byte-output** because the edit text now includes pane chrome when a pane is present. **Fixture currently uses `nil` pane so byte-parity is unaffected.** New E2E for reasoning+tool. |

### Model D — phase machine with explicit handoff events

Add `EnterReasoningMode` + `EnterContentMode` + `LeaveContentMode` (final answer wipes both). Pane is a state machine: `Idle → Reasoning → Tools → Content → Final`. Outbound emits transitions; pane composes accordingly.

| Aspect | Detail |
|---|---|
| Generality | Cleanest mental model; future-proof for `EnterFinalMode`, `EnterErrorMode`, etc. |
| Cost | ~150-200 LOC. Adds public methods. Refactor of `flush()`. Risk of off-by-one transitions. |
| Race risk | Low if states are exclusive; high if not. Easy to introduce bugs. |
| Tests | All E2E tests rewritten around state transitions. |

### Decision

**Model C.** Reasons:

1. The user's reported bug is precisely that *two writers race for one placeholder when reasoning + tool coexist*. Single-writer-per-phase fixes it at the root, not at the symptom.
2. Pane already understands "status block above body" rendering. Adding "CoT prefix above status block" is a minor extension of its existing composer — no new state machine.
3. Outbound already knows when content starts (`sb.Len() >= streamingMinThreshold`). That's the *only* handoff trigger we need. Reasoning is data, not control.
4. Existing fixture tests pass `nil` pane → byte-parity locked. Unit tests for the pane keep their golden assertions unchanged because Layouts A/B/C/D/E render identically when no CoT is present.
5. Model B couples pane to LLM stream state but still has two writers — leaves the throttle race (B6) unsolved. Model D is over-engineered for a one-bug fix.
6. Master-direct workflow, one commit. Model C fits in ~80 LOC.

## 3. Concrete patch plan

### 3.1 `internal/channels/telegram/status_pane.go`

**Add** to `statusPane` struct:
```go
cotProvider func() string // optional; pane reads to render CoT prefix above the status block
```

**Add** method:
```go
// SetCoTProvider lets Outbound inject a thunk that returns the current
// reasoning buffer. The pane reads it each flush so a single edit shows
// "🧠 _reasoning_\n\n🛠 Sto lavorando…\n[blockquote]". Pass nil to clear.
func (p *statusPane) SetCoTProvider(provider func() string) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.cotProvider = provider
}
```

**Modify** `composeLocked` to prepend the CoT prefix:
```go
func (p *statusPane) composeLocked() (string, tele.Entities) {
    var cotPrefix string
    if p.cotProvider != nil {
        if cot := strings.TrimSpace(p.cotProvider()); cot != "" {
            cotPrefix = "🧠 _" + cot + "_\n\n"
        }
    }
    if len(p.activeRound) == 0 && len(p.roundHistory) == 0 && cotPrefix == "" {
        return "", nil
    }
    var sb strings.Builder
    sb.WriteString(cotPrefix)
    sb.WriteString(statusHeader)
    sb.WriteString("\n")
    // entity offsets recompute from the now-prefixed buffer; bqStart uses tgmd.UTF16Len(sb.String())
    // which already includes the prefix → no offset bug.
    ...
}
```

**Remove** the `contentMode` guard from `OnToolStart`. The guard exists today to prevent edits *after handoff*; but in Model C, handoff only happens at content time, which is also after the loop dispatches tools for the *current* round. The contentMode invariant becomes "Outbound owns the body" — pane's edit attempts are still suppressed by `flushLocked`'s existing `if p.contentMode { return }` at line 256. State accumulation must continue regardless so footer counts stay accurate.

```go
func (p *statusPane) OnToolStart(callID, name string, argKeys []string) {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.finalized { return } // dropped: || p.contentMode
    p.activeRound = append(p.activeRound, toolEntry{...})
    p.flushLocked() // flushLocked already no-ops when contentMode
}
```

### 3.2 `internal/channels/telegram/outbound.go`

**Modify** `streamStatus` interface:
```go
type streamStatus interface {
    EnterContentMode()
    FooterMarkdown() string
    MarkEdited()
    SetCoTProvider(func() string) // new
}
```

**Modify** `readyToFlush` — split content trigger from CoT-only flush:
```go
readyToContentFlush := func() bool {
    return sb.Len() >= streamingMinThreshold
}
// readyToFlush kept for the no-pane path (fixture compatibility): below threshold
// CoT still triggers a flush when no pane is wired, preserving current snapshot bytes.
readyToFlush := func() bool {
    if sb.Len() >= streamingMinThreshold { return true }
    if sb.Len() == 0 && cotBuf.Len() >= streamingReasoningMinThreshold { return true }
    return false
}
```

**Modify** `flush` — different policy depending on whether a pane is attached:
```go
flush := func() {
    if pane != nil {
        // Pane-attached path: Outbound only edits once narrative content
        // crosses streamingMinThreshold. Reasoning-only flushes are
        // surfaced via the pane (it reads cotBuf via SetCoTProvider).
        if !readyToContentFlush() { return }
        if msg == nil {
            pane.EnterContentMode() // handoff happens here, not on CoT
        }
        // ... existing edit logic unchanged
    } else {
        // No-pane path (fixture / future channels): unchanged, byte-parity preserved
        if !readyToFlush() { return }
        // ... existing edit logic unchanged
    }
}
```

**Wire** the CoT provider once, on entry (only when pane != nil):
```go
if pane != nil {
    pane.SetCoTProvider(func() string {
        // Read cotBuf without locking sb.Mutex — strings.Builder.String() is
        // not safe under concurrent writes, but the token loop is single-
        // goroutine, and flushLocked's caller holds p.mu, not the writer's.
        // We invoke the provider from inside the pane's mutex (flushLocked)
        // — which means the token goroutine could be mid-WriteString. Race.
        //
        // Fix: maintain a parallel atomic snapshot. Cheapest: a *atomic.Value
        // holding the latest CoT string, updated on every reasoning token
        // before invoking flush().
        return cotSnapshot.Load().(string)
    })
    defer pane.SetCoTProvider(nil) // sever pointer at turn end
}
```

Update the reasoning branch in the token loop:
```go
if tok.Reasoning != "" {
    cotBuf.WriteString(tok.Reasoning)
    cotSnapshot.Store(cotBuf.String())  // atomic snapshot for pane
    flush()
}
```

**Final-edit branch** (`tok.Done && !resp.HasToolCalls`): unchanged behaviorally — it already strips footer + CoT. Once `pane.MarkEdited()` was called, the pane's flushLocked never re-fires (contentMode permanently on).

### 3.3 `internal/channels/telegram/invocation_builder.go`

No changes. `pane` is constructed and passed exactly as today; the new `SetCoTProvider` wiring lives entirely inside `Outbound.ConsumeStream` (Outbound owns the cotBuf, so it's the right place).

### 3.4 `internal/channels/telegram/chat_client.go`

No changes — pane already flows through.

### Function-level summary

| File | Function | Change |
|---|---|---|
| status_pane.go | `OnToolStart` | drop `\|\| p.contentMode` guard |
| status_pane.go | `composeLocked` | prepend `🧠 _cot_\n\n` when cotProvider yields non-empty |
| status_pane.go | `SetCoTProvider` | new method, ~6 LOC |
| status_pane.go | `streamStatus` impl | new method satisfies interface |
| outbound.go | `streamStatus` interface | add `SetCoTProvider(func() string)` |
| outbound.go | `flush` closure | branch on `pane != nil`; pane path uses content-only trigger |
| outbound.go | `ConsumeStream` top | declare `cotSnapshot atomic.Value` init `""` |
| outbound.go | reasoning branch | update `cotSnapshot` after `cotBuf.WriteString` |
| outbound.go | `ConsumeStream` top | call `pane.SetCoTProvider(loadSnapshot)`; defer-clear at end |

Total: ~60 LOC added, ~10 LOC removed. Single commit.

### Italian copy — unchanged

Header, blockquote summary, per-call lines, footer all untouched. The only new visual is the CoT italic prefix `🧠 _…_` — which is the **existing** behavior already covered by `composeStreamingMessage`, just lifted to render above the pane instead of replacing it.

### Throttle (B6 fix as bonus)

The pane is already the sole writer during the pre-content phase, so the throttle race I flagged in §1 stops being reachable for reasoning-only/tool-only/reasoning+tool turns. Once Outbound takes over (content phase), pane's `flushLocked` early-returns on `contentMode`. The shared `MarkEdited()` mechanism continues to serve as defense-in-depth but is no longer load-bearing.

## 4. Test impact assessment

| Test | File | Impact | Action |
|---|---|---|---|
| Layouts A/B/C/D/E | status_pane_test.go | Unchanged: tests don't set `cotProvider`, composer behaves identically | none |
| `TestStatusPane_LayoutD_ContentModeFooter` | status_pane_test.go | Unchanged | none |
| `TestStatusPane_CoalescesWithinThrottleWindow` | status_pane_test.go | Unchanged | none |
| `TestStatusPane_NotModifiedGuard` | status_pane_test.go | Unchanged | none |
| `TestStatusPane_FinalizeMarksOrphansFailed` | status_pane_test.go | **Sensitive**: relied on `OnToolStart` running before contentMode. Still works since `contentMode=false` in this test | none |
| `TestStatusPane_ConcurrentSafeUnderRace` | status_pane_test.go | Unchanged | none |
| `TestStatusPane_E2E_FooterPrependedDuringStreamThenStrippedOnFinal` | status_pane_e2e_test.go | Drives reasoning-free tokens → exercises content path; trigger now requires `sb.Len() >= 30` only (which it already meets); footer assertion unchanged | none |
| `TestStatusPane_E2E_ToolOnlyTurnLeavesPaneInControl` | status_pane_e2e_test.go | Drives `Done` + ToolCalls with zero reasoning; pane's lone edit still fires; `EnterContentMode` no longer triggers from reasoning so this test is more representative of the pane-only path | none |
| `TestSnapshotsByteParity` (4 scenarios) | fixture/byte_parity_test.go | `with_cot` scenario: pane is `nil` so the no-pane branch executes → exact same `readyToFlush` semantics → **byte-identical**. Other 3 scenarios have no CoT or no pane impact | none — verify with `go test -count=1 ./internal/channels/telegram/fixture/` |

### New test required

`TestStatusPane_E2E_ReasoningBeforeToolDoesNotHijackPlaceholder` — the regression test for this bug:

```go
// Drives: Reasoning(50 chars) → Done(+ToolCalls) → exits ConsumeStream;
// simulates loop calling OnToolStart/OnToolEnd; reopens stream with
// Content(60 chars) → Done.
// Asserts:
//   - first edit body is the pane (header + blockquote + 🧠 _reasoning_ prefix)
//   - OnToolStart's tool name appears in some pane edit
//   - second-round content edit prepends a non-empty FooterMarkdown
//     ("1 strumento usato …")
//   - final edit body is the answer only, no 🛠, no 🧠
```

Place alongside `status_pane_e2e_test.go`. ~80 LOC including helpers.

### Existing test that becomes a tautology

`TestStatusPane_E2E_FooterPrependedDuringStreamThenStrippedOnFinal` no longer exercises the bug — it only covers content-only turns. Keep it (regression for the `withFooter` prepend behavior) but rename its docstring to be honest about the scope.

## 5. Risks remaining after the fix

| Risk | Reasoning | Mitigation |
|---|---|---|
| `cotSnapshot` atomic.Value type-assert panic on first read | We initialise with `""` so `Load()` always returns string. If init missed → panic. | Init at the top of `ConsumeStream` with `cotSnapshot.Store("")` before SetCoTProvider runs. Unit-test the empty-load path. |
| Pane's CoT prefix bloats the body past Telegram's 4096 UTF-16 cap | Long reasoning + long blockquote + history. `RenderForEntities` already splits at the cap, but the pane's `editFn` doesn't paginate. | Existing `editFn` calls `bot.Edit(placeholder, text, ents)` directly — if the body exceeds 4096, Telegram returns 400. Pane logs at Debug and stops. Acceptable for slice 1; future slice can apply `RenderForEntities` inside the pane. **Add a guard**: when `tgmd.UTF16Len(body) > 3800`, truncate CoT first (it's the unbounded part). |
| Multiple reasoning rounds: round-2 reasoning replaces round-1 reasoning in the prefix | `cotBuf` is single-buffer across rounds. Visually: prefix grows. UX-acceptable but verbose. | None for this fix. Future: a `ResetCoT()` between rounds. |
| `SetCoTProvider(nil)` at end of `ConsumeStream` races with pending pane edit | `defer pane.SetCoTProvider(nil)` runs after the loop. If a goroutine inside the pane had captured the old provider pointer, it would read freed data. The pane only invokes the provider inside `flushLocked` which holds the mutex, and `SetCoTProvider` also takes the mutex — so the write is serialized. Safe. | Verified via mutex inspection; covered by `TestStatusPane_ConcurrentSafeUnderRace`. |
| Reasoning emitted AFTER content has started (some models do this) | `EnterContentMode` already fired → pane's `flushLocked` no-ops on contentMode → CoT prefix doesn't render. Outbound's `composeStreamingMessage` still renders `🧠 _cot_\n\ncontent` because it reads `cotBuf` directly. **So mid-stream reasoning still shows, owned by Outbound** — current behavior preserved. | None needed. Document. |
| The "second-round CoT" edge case: round-1 had no content, round-2 starts with reasoning | After round-1 tool execution, ConsumeStream is re-invoked. New Outbound `cotBuf` is empty. Pane retains `roundHistory`. CoT provider is freshly bound. Works correctly. | Verify via the new E2E test. |
| User-visible regression on reasoning-only turns with no tool calls | This is the existing `with_cot` fixture path — pane is nil in fixture but in production pane is always set. After the fix, reasoning-only turns will edit through Outbound's `readyToContentFlush` branch — which won't fire until 30 chars of *content*. CoT now drives pane edits via cotProvider. **The user sees CoT-then-content live**, exactly as today, but routed via the pane's editFn instead of Outbound's. Byte-output differs (now includes the `🛠 Sto lavorando…` chrome). | **This is a behavior change**: reasoning-only turns will show the pane header even when no tools fire. Two options: (1) accept — gives the user the same "I'm working" signal; (2) suppress the header when `len(activeRound)==0 && len(roundHistory)==0` and only render the CoT prefix. **Pick option 2** — keep noise low. Implement by gating header emission on `len(activeRound)+len(roundHistory) > 0`. ~3 LOC. |

The option-2 mitigation above is a one-liner inside `composeLocked` — gate the header on tool activity, render CoT-only when no tools.

## 6. One-commit shape

```
fix(telegram): show tool pane during reasoning+tool turns (race fix)

Reasoning LLMs (GPT-5, DeepSeek-R1, OpenRouter routed) emit reasoning
tokens before tool_calls land. Outbound.flush() treated reasoning as
content and called pane.EnterContentMode() prematurely — pane was
locked out for the rest of the turn, OnToolStart silently dropped
entries, footer permanently empty. User saw plain "⏳" or reasoning
text only; the Slice 2 status pane never appeared.

  outbound.go    : split readyToFlush — pane path triggers handoff only
                   on >=30 content chars, not on reasoning. CoT now
                   surfaces through the pane via SetCoTProvider.
                   No-pane path (fixture) unchanged → byte parity holds.
  status_pane.go : composeLocked prepends "🧠 _cot_\n\n" when provider
                   yields non-empty; OnToolStart drops contentMode guard
                   so state still accumulates if pane is suppressed by a
                   late content takeover (flushLocked already gates the
                   edit itself).

Tests: existing 13 layouts + throttle + E2E green; new
TestStatusPane_E2E_ReasoningBeforeToolDoesNotHijackPlaceholder pins
the regression; fixture byte-parity unchanged.
```
