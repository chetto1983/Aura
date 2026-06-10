# Audit: internal/conversations

**Verdict:** needs-work — two correctness gaps (sidecar orphan accumulation, FTS search silently skips spilled content) and one not-wired interface branch; no critical runtime defects.

**Counts:** critical 0 / high 2 / medium 1 / low 1

---

## Findings

### [HIGH][BUG] Sidecar file orphaned when AppendTurn transaction rolls back

**File:** `internal/conversations/store.go:277-305` / `internal/conversations/store_helpers.go:94-106`

**Confidence:** high

**Detail:**

`appendTurnWrites` calls `maybeSpill` which writes the `.content` sidecar file to disk as a pure filesystem side-effect. For the caller-supplied-seq path (`p.Seq > 0`, line 277), this write happens BEFORE the `db.WithTx` call (line 282). If the transaction fails (DB unavailable, constraint violation, aggregate UPDATE error), the sidecar file remains on disk with no corresponding DB turn row.

For the auto-allocate path (`p.Seq <= 0`, line 290), `appendTurnWrites` is called inside the transaction closure (line 296), so the sidecar write also occurs inside the transaction scope. If `insertTurnAndAggregates` subsequently fails (line 300), the transaction rolls back, again leaving an orphaned file.

The comment at line 272 states "a later boot scan reconciles an orphaned file". However, `scanConversationOrphans` (`orphan_scan.go:61`) operates at conversation-directory granularity only: it removes `conversations/<id>/` directories that have no matching DB row. It does NOT scan individual `<seq>.content` files within a live conversation directory. An orphaned sidecar file within a live conversation's directory accumulates silently and is never cleaned up until the conversation itself is deleted.

`AppendAssistantTurnWithCacheMetric` (line 312) has the identical pattern and the identical gap.

**Suggested fix:** Either (a) scan individual `.content` files within the conversation directory in `scanConversationOrphans` and remove those whose seq has no matching turn row, or (b) move the sidecar write AFTER the transaction commits (requires the transaction to return the allocated seq, which it already does in the auto-allocate path). Option (b) is the lower-risk change: in `AppendTurn` with `p.Seq > 0`, defer the `maybeSpill` call until after `db.WithTx` returns nil.

---

### [HIGH][BUG] FTS search silently excludes sidecar-spilled turns

**File:** `internal/conversations/store.go:482-500` / `internal/db/queries/conversation_turns.sql:33-37`

**Confidence:** high

**Detail:**

`SearchConversationTurns` executes:
```sql
WHERE content % $1
ORDER BY similarity(content, $1) DESC
```

The pg_trgm operator `%` returns NULL when its left operand is NULL. Turns with large content that was spilled to a sidecar file have `content = NULL` (see `maybeSpill`, `store_helpers.go:95-105`: the inline `pgtype.Text` field is left zero-valued, i.e., `Valid: false`). The WHERE clause silently excludes these turns from search results.

In the `SearchConversationTurns` projection (line 493), `r.Content.String` will be an empty string for any NULL content row — but such rows cannot reach the projection because the WHERE clause filters them before they reach the scan loop.

The consequence: `SearchConversationTurns` returns incomplete results for any conversation that has ever triggered a sidecar spill. No error is returned and no warning is logged. The caller cannot detect the omission.

**Suggested fix:** Join against the sidecar content or add a fallback. Simplest approach that preserves the locked query contract: in the application layer (`SearchConversationTurns`), if a result has empty `Content`, attempt to read the sidecar file (by querying the turn for its `content_sidecar_path`). Alternatively, extend the SQL to `COALESCE(content, (SELECT content FROM …))` via a CTE — but this changes the LOCKED query, so it requires a D-A5-03 amendment. A stopgap is to log a WARN when spilled turns exist in the searched conversation.

---

### [MEDIUM][NOT-WIRED] ConversationCleaner interface and Delete cleaner branch are entirely dead in production

**File:** `internal/conversations/store.go:49-58, 72, 82, 530-541`

**Confidence:** high

**Detail:**

`ConversationCleaner` is an exported interface with one method `PurgeConversationDir(convID string) error`. It is documented as the `sandbox.WorkspaceManager` injection point for a symlink-safe `os.Root` cascade on conversation delete (ROADMAP crit 2, landmine #4).

Every production call site of `conversations.New` passes a `Config` without the `Cleaner` field:
- `cmd/aura/chat.go:137-140`: `Config{RunDir: cfg.RunDir, TurnCapBytes: cfg.ConversationTurnCapBytes}` — no `Cleaner`
- `internal/agui/server_integration_test.go:116`: no `Cleaner`
- `internal/eval/skills_snippet_reuse_cot_eval_test.go:241`: no `Cleaner`
- `internal/runner/live_e2e_test.go:138`: no `Cleaner`

The `if s.cleaner != nil` branch in `Delete` (line 530) is never taken. The symlink-safe `os.Root` cascade described in comments never executes. The existing fallback is `os.RemoveAll` (line 549), which DOES follow symlinks and could be redirected by a malicious symlink inside the sidecar-writable workspace — the exact threat the cleaner was designed to close.

No type outside `internal/conversations` implements `PurgeConversationDir`. The `ConversationCleaner` interface is defined only here.

**Suggested fix:** Wire the cleaner at the composition root (`cmd/aura/chat.go`) by injecting a real implementation (stub or the actual `sandbox.WorkspaceManager.PurgeConversationDir`). Until the sandbox workspace manager exists, at minimum replace `os.RemoveAll` in the `nil`-cleaner fallback with an `os.Root`-based equivalent (available since Go 1.24) to close the traversal gap independently of the injection.

---

### [LOW][BUG] Error message reports seq=0 when auto-allocate fails before seq is assigned

**File:** `internal/conversations/store.go:301-303`

**Confidence:** high

**Detail:**

In `AppendTurn` with `p.Seq <= 0`, after `db.WithTx` returns an error, the error message is:
```go
return fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
```

If `allocateTurnSeq` itself fails (line 291-294), `p.Seq` has never been updated from 0 (the value the caller passed). The error message says "seq 0", which is misleading for diagnostic purposes: a `seq 0` in the log suggests the seq-allocation step failed rather than the turn-insert step, but the message gives no indication of that.

The same pattern exists in `AppendAssistantTurnWithCacheMetric` (line 351-354).

**Suggested fix:** Capture the allocated seq separately or include a contextual note in the error message distinguishing an allocation failure from an insert failure. Alternatively, only format the seq into the error if it was actually allocated (`if p.Seq > 0 { ... }`).
