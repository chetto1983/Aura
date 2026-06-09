# Audit: internal/askuser

**Verdict:** needs-work — two medium findings (panic-unsafe transaction, fake/real content mismatch) plus a low doc inconsistency; no critical or high issues.

**Counts:** critical 0 / high 0 / medium 2 / low 1

## Findings

### [MEDIUM][BUG] `MarkResumedBatch` transaction not panic-safe — leaked connection on library panic

**Location:** `internal/askuser/store.go:253-292`

**Confidence:** medium

**Detail:**

`MarkResumedBatch` opens a transaction manually with `s.pool.Begin` and guards rollback via a deferred closure that reads the outer `err` variable:

```go
tx, err := s.pool.Begin(ctx)
defer func() {
    if err != nil {
        _ = tx.Rollback(ctx)
    }
}()
```

The deferred closure only rolls back when `err != nil`. If a panic occurs at a point where `err` is still `nil` — e.g., during `tx.Exec` before `execErr` is assigned — the deferred closure fires with `err == nil` and skips the rollback. The transaction is never rolled back and the connection is held until the server-side idle timeout.

By contrast, `db.WithTx` (used by every other multi-statement write in the codebase: `AutoResolveForConversation`, `conversations.Store.AppendTurn`, etc.) wraps the deferred in a `recover()` block and rolls back before re-panicking. The internal package comment at `internal/askuser/store.go:55` says both `MarkResumedBatch` and `AutoResolveForConversation` "wrap db.WithTx", which is false — only `AutoResolveForConversation` does.

In practice, panics from `pgx.Tx.Exec` are extremely unlikely. The risk is low-probability but the inconsistency is unnecessary: `MarkResumedBatch` could be refactored to `db.WithTx` the same as its sibling, or the deferred closure should be extended with `recover`.

**Suggested fix:**

Replace the manual `pool.Begin` + deferred rollback with `db.WithTx`:

```go
func (s *Store) MarkResumedBatch(ctx context.Context, answers map[string]ResumeAnswer) error {
    if len(answers) == 0 {
        return nil
    }
    return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
        for token, ans := range answers {
            id, err := parseUUID("token", token)
            if err != nil {
                return fmt.Errorf("mark resumed batch: %w", err)
            }
            answer, err := encodeAnswer(ans)
            if err != nil {
                return fmt.Errorf("mark resumed batch %s: %w", token, err)
            }
            tag, err := q.DB().Exec(ctx, markResumedSQL, id, answer)
            if err != nil {
                return fmt.Errorf("mark resumed batch %s: %w", token, err)
            }
            if tag.RowsAffected() == 0 {
                return fmt.Errorf("mark resumed batch %s: %w", token, ErrPauseNotFound)
            }
        }
        return nil
    })
}
```

Alternatively, add a `recover` to the existing deferred closure to match `db.WithTx`'s contract.

---

### [MEDIUM][BUG] Fake `AutoResolveForConversation` writes a different `autoTerminatedContent` than the real store

**Location:**
- Real constant: `internal/askuser/store.go:43`
- Fakes: `cmd/aura/cachefakes.go:285`, `cmd/aura/cmdfakes_test.go:248`, `internal/runner/fakes_test.go:377`

**Confidence:** high

**Detail:**

The real `AutoResolveForConversation` writes:

```go
const autoTerminatedContent = "<auto-terminated: conversation ended>"
```

All three in-memory fake implementations write:

```go
askuser.ResumeAnswer{Action: askuser.ActionCancel, Content: "<auto-terminated>"}
```

The `Content` values are different strings. Any test or code path that asserts on the `Content` of an auto-resolved pause (e.g., operator tooling that reads `Record.ResumedAnswer` from `ListRecent`) will see different values depending on whether it runs against the real store or a fake. The `autoTerminatedContent` constant is unexported, so the fakes cannot reference it directly — this is the root cause. Because the constant is unexported, it cannot be reused by the fake implementations.

Currently no tests assert on the exact content string of auto-terminated rows (only `action == "cancel"` is checked), so this does not cause test failures today. However, the divergence will silently mask any future behavior that branches on this content, and it means integration tests (real store) and unit tests (fakes) exercise different semantics.

**Suggested fix:**

Export the constant as `AutoTerminatedContent` and reference it from all three fake implementations:

```go
// internal/askuser/store.go
const AutoTerminatedContent = "<auto-terminated: conversation ended>"
```

Then in each fake:

```go
m.answers[tok] = askuser.ResumeAnswer{Action: askuser.ActionCancel, Content: askuser.AutoTerminatedContent}
```

---

### [LOW][OTHER] Package doc comment and `db.WithTx` doc incorrectly claim `MarkResumedBatch` uses `db.WithTx`

**Location:**
- `internal/askuser/store.go:5` (package doc)
- `internal/askuser/store.go:55` (Store type comment)
- `internal/db/tx.go:12` (db.WithTx doc, out-of-scope file but verifiable)

**Confidence:** high

**Detail:**

Three doc comments assert that `MarkResumedBatch` uses `db.WithTx`:

- `store.go:5`: "db.WithTx for atomic multi-row writes"
- `store.go:55`: "multi-row atomic writes (MarkResumedBatch, AutoResolveForConversation) wrap db.WithTx"
- `db/tx.go:12`: lists `askuser.Store.MarkResumedBatch` as a user of `WithTx`

In reality, `MarkResumedBatch` uses a manual `pool.Begin` / defer-rollback pattern. Only `AutoResolveForConversation` calls `db.WithTx`. The docs are stale or were written with a different implementation in mind. This is a documentation bug that obscures the panic-safety gap noted in askuser-1.

**Suggested fix:**

Update the comments to accurately describe the manual transaction pattern, and ideally resolve by fixing askuser-1 (switching to `db.WithTx`).

## Clean (what was checked and found correct)

- **UUID parsing**: all Store methods call `parseUUID` before any DB access; nil-pool tests prove parse short-circuits correctly.
- **Error classification**: `pgx.ErrNoRows → ErrPauseNotFound` sentinel (GetByToken line 149), `RowsAffected() == 0 → ErrPauseNotFound` (MarkResumed line 237, MarkResumedBatch line 283). No string matching.
- **Error wrapping**: all `fmt.Errorf` calls use `%w`; no swallowed errors.
- **Deferred rollback correctness**: in `MarkResumedBatch`, the `err` variable is captured by reference and all error paths assign to it before returning, so the rollback correctly fires on every non-panic error path.
- **Slice aliasing**: `fromRow` defensively copies `ResumeContext` via `append(json.RawMessage(nil), r.ResumeContext...)` to avoid aliasing with pgx's internal buffer.
- **`int32` overflow on Priority**: `int32(p.Priority)` at line 129 is unchecked, but Priority is a domain-bounded small integer (0–100 scale); no production caller passes large values. Low enough to not warrant a finding.
- **`int32` overflow on ListRecent limit**: the `limit <= 0` clamp guarantees `limit >= 1` before cast; all callers pass bounded values (literal 50 or small CLI integers).
- **Inner `err` shadowing in ListRecent**: the `:=` in the loop body is a new variable scoped to the loop iteration, not the outer `err`; no shadowing hazard.
- **`encodeAnswer` on `json.Marshal`**: the `%w` wrapper on line 349 uses `%v` for the inner error (`ErrInvalidAnswer` is already the sentinel), which is correct since `json.Marshal(ResumeAnswer{...})` cannot fail (both fields are strings).
- **Goroutine leaks**: no goroutines are launched; goleak is installed on the integration tier.
- **Shared mutable state**: `Store` has no fields modified after construction. No races.
- **Dead code**: `autoTerminatedContent`, `encodeAnswer`, `decodeResumedAnswer`, `fromRow`, `parseUUID` are all reachable. `ErrInvalidAnswer` is exported and returned through the public API even though no current production caller does `errors.Is` on it — this is a valid sentinel for future callers.
- **Wiring**: all Store methods are exercised via `internal/runner` (interface `PauseStore`), `cmd/aura/chat.go`, `cmd/aura/paused_states.go`, and `internal/agui/server.go`. No method is defined but unreachable.
