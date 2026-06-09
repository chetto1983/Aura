# Audit: internal/conversations

**Verdict:** needs-work — one latent data-integrity bug + one misleading doc comment + one not-wired sentinel; no critical issues.

**Counts:** critical 0 / high 1 / medium 2 / low 2

## Findings

---

### [HIGH][BUG] AppendAssistantTurnWithCacheMetric: metric.Seq not set in the Seq>0 fast path

**Location:** `internal/conversations/store.go:312–329`

**Confidence:** high

**Detail:**
When `p.Seq > 0` (pre-known sequence number), the `Seq <= 0` allocation branch (lines 332–354) correctly sets `metric.Seq = int32(seq)` inside the transaction. The `Seq > 0` branch (lines 312–329) does NOT update `metric.Seq` to match `p.Seq`; it passes `metric` verbatim to `q.InsertCacheMetric`. A caller that constructs `metric` with `Seq = 0` (the runner always does: `r.cacheMetricParams(convID, 0, u, cost)`) and then triggers this branch would persist a `cache_metrics` row with `seq = 0` for a turn that actually has a non-zero seq.

Today the runner always passes `Seq = 0`, so it never hits the `Seq > 0` branch, masking the bug. Any future caller that passes a pre-known seq will silently write a wrong `cache_metrics.seq`.

**Suggested fix:**
Add `metric.Seq = int32(p.Seq)` inside the `if p.Seq > 0` block immediately before `db.WithTx`:

```go
if p.Seq > 0 {
    metric.Seq = int32(p.Seq)  // ← add this line
    turn, agg, err := s.appendTurnWrites(p)
    ...
}
```

---

### [MEDIUM][BUG] Sidecar orphan in live conversation not reconciled by boot scan (doc comment incorrect)

**Location:** `internal/conversations/store.go:276–288` (AppendTurn Seq>0 path), comment at line 274

**Confidence:** high

**Detail:**
The doc comment states: "the file write is not part of the DB atomicity; a later boot scan reconciles an orphaned file if the tx rolls back."

This claim is only correct for the case where the entire conversation row is absent (orphan directory). `ScanOrphans` in `orphan_scan.go` removes `conversations/<id>/` directories that have no matching DB row, but it does NOT scan for individual stale `<seq>.content` files inside a live conversation's directory. If `AppendTurn` with `p.Seq > 0` spills to `conversations/<id>/<seq>.content` and the subsequent transaction rolls back, the sidecar file leaks permanently inside the live conversation's directory — the boot scan never touches it because the conversation row still exists.

The runner always passes `Seq = 0`, so today this path is never exercised in production. But the misleading comment could lead a future implementer to rely on non-existent reconciliation.

**Suggested fix:**
Either: (a) move `appendTurnWrites` (including `maybeSpill`) INSIDE the `db.WithTx` callback for the `Seq > 0` path as well, so the sidecar write happens inside the transaction scope (same ordering as the `Seq <= 0` path), OR (b) add per-file orphan cleanup to `scanConversationOrphans` that removes `.content` files with no matching DB row. Also correct the doc comment.

---

### [MEDIUM][NOT-WIRED] ErrContextWindowExceeded exported sentinel never checked by callers

**Location:** `internal/conversations/context.go:49`

**Confidence:** high

**Detail:**
`ErrContextWindowExceeded` is an exported sentinel error wrapped with `%w` at line 158. Its purpose (per the comment) is that "the REPL surfaces" it with a special action. However, no caller in the repo performs `errors.Is(err, conversations.ErrContextWindowExceeded)`:

- `internal/runner/runner.go:234–238`: receives the error from `LoadManagedHistory`, propagates it via `yield(nil, err)`.
- `cmd/aura/chat_repl.go:67`: prints it as `"turn error: ..."`.
- `internal/agui/server.go` and `internal/channels/telegram`: similarly propagate without type-checking.

The user-facing error message "start a new chat with `aura chat new`" is embedded in the error string, so the UX is not broken. But the exported sentinel provides no additional programmatic value over an unexported error in its current wired state: no caller can distinguish this from, say, a DB connection error. The AG-UI gateway in particular could benefit from emitting a distinct event type for this recoverable condition.

**Suggested fix:**
Either check `errors.Is(err, conversations.ErrContextWindowExceeded)` in the runner or REPL to give a distinct UX (e.g., print the message differently, emit a special AG-UI event), or demote `ErrContextWindowExceeded` to unexported (`errContextWindowExceeded`) until a caller needs to type-check it.

---

### [LOW][DEAD-CODE] validateID in Delete's cleaner branch is redundant after parseUUID

**Location:** `internal/conversations/store.go:533–535`

**Confidence:** high

**Detail:**
`Delete` calls `parseUUID("id", conversationID)` at line 522. A string that passes `parseUUID` is a canonical UUID (`xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`) that can never contain `..`, `/`, or `\`. The subsequent `validateID("conversation_id", conversationID)` at line 533 (inside the `s.cleaner != nil` branch) will therefore always succeed and never fire its guard. The same is true for `validateID` inside `sidecarDir` → `turnSidecarPath` → `maybeSpill` when the call chain originates from `appendTurnWrites` (which itself calls `parseUUID` first).

The redundancy is harmless but adds confusing defensive code whose invariant is not obvious from the call site.

**Suggested fix:**
Remove the `validateID` call at line 533 and add a comment explaining the invariant: `// conversationID already validated as a canonical UUID by parseUUID above; path traversal impossible.`

---

### [LOW][DEAD-CODE] sweepTmp calls os.RemoveAll on symlinks without the os.Remove treatment given in scanConversationOrphans

**Location:** `internal/conversations/orphan_scan.go:144–155`

**Confidence:** medium

**Detail:**
`scanConversationOrphans` (line 82–86) explicitly detects symlinks via `Lstat` and removes them with `os.Remove` (not `os.RemoveAll`) to avoid traversing to an external target. `sweepTmp` also uses `os.Lstat` to obtain `ModTime` but then calls `os.RemoveAll(full)` for both directories AND symlinks older than the TTL.

`os.RemoveAll` on a symlink removes the link itself without following it, so this is safe on POSIX systems. However the inconsistency between the two scan functions is a maintenance hazard: the rationale in `scanConversationOrphans` (symlink guard) was evidently not ported to `sweepTmp`, making the security model harder to reason about.

**Suggested fix:**
Add a symlink check in `sweepTmp` parallel to the one in `scanConversationOrphans`: if `info.Mode()&os.ModeSymlink != 0`, use `os.Remove` instead of `os.RemoveAll`. Document that `os.Remove` vs `os.RemoveAll` is an explicit security choice, not an accident.
