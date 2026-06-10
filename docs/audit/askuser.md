# Audit: internal/askuser

**Verdict:** needs-work — three low-severity defects (one aliasing inconsistency, one stale comment with semantic impact, one leaked abstraction); no criticals or highs.

**Counts:** critical 0 / high 0 / medium 1 / low 3

---

## Findings

### [MEDIUM][BUG] `fromRow` copies `ResumeContext` defensively but aliases `Options` directly

**Location:** `internal/askuser/store.go:333-337`

**Confidence:** high

**Detail:**

```go
func fromRow(r sqlc.AuraPausedStates) Pending {
    return Pending{
        ...
        Options:       r.Options,                                        // ← shared slice header
        ResumeContext: append(json.RawMessage(nil), r.ResumeContext...), // ← defensive copy
    }
}
```

`ResumeContext` is copied with `append(nil, ...)` so mutations by the caller cannot reach the sqlc-returned backing array. `Options` is not copied — the returned `Pending.Options` shares the underlying `[]byte` array with the sqlc row. An in-place write to `Pending.Options` (e.g. `pending.Options[0] = '{'`) would corrupt the sqlc buffer.

No caller currently mutates `Options` in-place (all usages unmarshal via `json.Unmarshal` into a fresh struct, which is safe). The risk is latent, but the asymmetry is a footgun for future callers and an internal contract violation relative to `ResumeContext`.

**Suggested fix:**

```go
Options: append(json.RawMessage(nil), r.Options...),
```

---

### [LOW][BUG] `autoTerminatedContent` comment claims "accept-shaped" but the action is `cancel`

**Location:** `internal/askuser/store.go:40-43` and `store.go:302`

**Confidence:** high

**Detail:**

The constant comment reads:

```go
// autoTerminatedContent is the resumed_answer content written when a conversation
// ends with open pendings (SPEC Req#11). It is an accept-shaped marker so a
// resumed loop sees a benign answer rather than a missing one.
```

But the usage encodes the marker with `ActionCancel`, not `ActionAccept`:

```go
answer, err := encodeAnswer(ResumeAnswer{Action: ActionCancel, Content: autoTerminatedContent})
```

`ActionCancel` and `ActionAccept` are handled differently by the runner (`runner_resume.go:73-74`): cancel short-circuits to `cancelConversation`, accept injects content and continues. The comment's "accept-shaped" claim is wrong. In practice, `AutoResolveForConversation` is called AFTER the runner has already injected cancelled answers into conversation history and terminated the turn — the `resumed_answer` DB column is then only visible to the `aura paused-states list` CLI, not to any resumed loop. The comment describes non-existent behavior and will mislead future maintainers.

**Suggested fix:** Replace the comment:

```go
// autoTerminatedContent is stored in resumed_answer when Loop.Stop closes all open
// pendings for a conversation (SPEC Req#11 / AutoResolveForConversation). The action
// is ActionCancel — this is a record-keeping marker only; the runner injects the
// cancellation into conversation history separately before calling AutoResolveForConversation.
```

---

### [LOW][OTHER] `CleanupResumedOlderThan` leaks `pgtype.Timestamptz` into the public API

**Location:** `internal/askuser/store.go:319`

**Confidence:** high

**Detail:**

```go
func (s *Store) CleanupResumedOlderThan(ctx context.Context, cutoff pgtype.Timestamptz) error {
```

All other `Store` methods accept and return plain Go types (`string`, `int`, `json.RawMessage`) and perform pgtype conversion internally. This one method exposes the pgx storage type directly, forcing every caller to import `github.com/jackc/pgx/v5/pgtype` and construct `pgtype.Timestamptz{Time: t, Valid: true}` manually (`cmd/aura/paused_states.go:98`). It is not part of the `PauseStore` interface (runner-facing) so swapping the signature is non-breaking to the interface consumers.

**Suggested fix:**

```go
func (s *Store) CleanupResumedOlderThan(ctx context.Context, cutoff time.Time) error {
    if err := s.q.CleanupResumedOlderThan(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true}); err != nil {
        return fmt.Errorf("cleanup resumed: %w", err)
    }
    return nil
}
```

---

### [LOW][NOT-WIRED] `Record.ResumedAnswer` discards the `Action` field — action is written to DB but never surfaced to any consumer

**Location:** `internal/askuser/store.go:354-363`, `store.go:215`

**Confidence:** medium

**Detail:**

`decodeResumedAnswer` unmarshals the `{action, content}` JSON but returns only `ans.Content`. The `Record.ResumedAnswer` field (exposed by `ListRecent` to the `aura paused-states list` CLI) carries only the content string; the action (`accept` / `decline` / `cancel`) is silently dropped.

```go
func decodeResumedAnswer(token string, raw []byte) (string, error) {
    ...
    var ans ResumeAnswer
    json.Unmarshal(raw, &ans)
    return ans.Content, nil  // ans.Action is discarded
}
```

The operator querying the CLI cannot distinguish whether a resolved pause was accepted, declined, or cancelled — the STATE column only shows `resolved` vs `pending`, and the ANSWER column only shows the content (which for a cancel is `"user cancelled the request"` or `"<auto-terminated: conversation ended>"`, readable but not machine-distinguishable).

This is a UX gap rather than a correctness bug; it is only surfaced as not-wired because the `Action` field is written to every resolved row but no code path reads it back via the Store's public surface.

**Suggested fix:** Add an `Action` field to `Record` and populate it from `decodeResumedAnswer`, or expose the full `ResumeAnswer` on `Record`:

```go
type Record struct {
    ...
    Resumed        bool
    ResumedAction  string // "" when pending; "accept" | "decline" | "cancel" when resolved
    ResumedAnswer  string
}
```

---

## What was checked and found clean

- **Transaction lifecycle (`MarkResumedBatch`):** The deferred rollback correctly captures `err` by reference (not value). All early-exit error paths set `err` before `return`, triggering `Rollback`. The Commit-then-Rollback ordering is safe under pgx v5 semantics (aborted tx sends ROLLBACK or drops the connection). No double-commit, no orphaned transaction.
- **Context propagation:** All public methods accept and thread `ctx` through every DB call. `AutoResolveForConversation` and `MarkResumedBatch` both pass `ctx` to `Begin`/`Rollback`/`Exec`/`Commit`. A cancelled `ctx` at `Rollback` is acceptable (pgx discards the connection).
- **UUID parsing:** Every UUID-string input goes through `parseUUID`, which returns a clear wrapped error before any DB call. Verified with nil-pool unit tests.
- **Error sentinel wrapping:** `ErrPauseNotFound` and `ErrInvalidAnswer` are wrapped with `%w` throughout. `errors.Is` chains work correctly.
- **`MarkResumedBatch` + `AutoResolveForConversation` atomicity:** Batch uses a manual `pool.Begin`/`Rollback`/`Commit` sequence; auto-resolve uses `db.WithTx`. Both are correct.
- **FIFO ordering:** `ListPendingPausedStates` SQL uses `priority DESC, created_at ASC, token ASC` — the token tiebreaker prevents non-determinism when rows share a transaction timestamp.
- **`fromRow` copy of `ResumeContext`:** Defensive copy via `append(json.RawMessage(nil), ...)` is correct and non-aliasing.
- **No goroutine leaks:** The package has no background goroutines. All DB calls are synchronous.
- **No unchecked errors:** Every `s.q.*` and `tx.*` return value is checked. `Rollback` errors are explicitly discarded (`_ =`) per convention.
- **Dead code:** `parseUUID`, `encodeAnswer`, `decodeResumedAnswer`, and `fromRow` are all called within the package. No unexported symbol is unreferenced. Exported types (`Store`, `Pending`, `Record`, `InsertParams`, `ResumeAnswer`) and constants (`ActionAccept`, `ActionDecline`, `ActionCancel`, `ErrPauseNotFound`, `ErrInvalidAnswer`) are all consumed by multiple callers across the repo.
