# Audit: internal/toolinvocations

**Verdict:** needs-work — one high-severity security gap in the credential redaction logic; two low-severity design defects; one dead struct field.

**Counts:** critical 0 / high 1 / medium 1 / low 2

## Findings

---

### [HIGH][BUG] inline_credential regex misses JSON-encoded key-value credential arguments

**Location:** `internal/toolinvocations/redact.go:69`

**Confidence:** high

**Detail:**

The `inline_credential` pattern is:

```
(?i)(password|api[_-]?key|token|secret)\s*[:=]\s*("?)[^"\s&]+
```

After the keyword it requires `\s*[:=]\s*` immediately, but in JSON-encoded tool arguments the credential key is double-quoted, so the actual character sequence between keyword and colon is `"` (a closing quote), not optional whitespace or `[`:

```
{"api_key": "ghp_realGitHubPAT12345678901234"}
 ^keyword^  no match: '"' between keyword and ':'
{"token": "xoxb-real-slack-bot-token-12345"}
{"password": "hunter2"}
{"secret": "my-very-secret-value"}
```

All four of these produce zero matches across every pattern in `secretPatterns`. Because `Arguments` in the ledger is the raw JSON from `call.Function.Arguments` (the LLM wire payload), this is the canonical format for every tool invocation — `shell_exec`, `web_fetch`, `write_file`, any future tool that accepts credentials as a named parameter. The append-only ledger's mutation trigger makes this durable and un-deletable.

The only credential shapes that ARE caught in JSON-argument form are OpenAI-style `sk-…` keys (caught by `openai_key` prefix shape regardless of surrounding JSON), AWS keys (same), and Bearer tokens in `Authorization:` header strings embedded in a shell command.

A GitHub PAT (`ghp_`), a Slack bot token (`xoxb-`), a Stripe key (`sk_live_` is caught but `pk_live_` is not), a generic `password` or `token` JSON value — all survive verbatim into the immutable ledger.

**Suggested fix:**

Add a companion pattern that covers JSON-encoded `"keyword": "value"` shape:

```go
// JSON-encoded credential object field: "api_key": "value" or "token": "value"
{"json_credential", regexp.MustCompile(`(?i)"(password|api[_-]?key|token|secret)"\s*:\s*"[^"]{4,}"`)},
```

Place it before `inline_credential` in the table (most-specific-first order is already the documented convention). The `{4,}` lower bound avoids matching short error strings like `"token": "ok"` or `"token": "nil"`.

---

### [MEDIUM][BUG] ON CONFLICT DO NOTHING silently discards duplicate inserts — caller receives nil error

**Location:** `internal/db/sqlc/tool_invocations.sql.go:28` (generated from `internal/db/queries/tool_invocations.sql`)

**Confidence:** high

**Detail:**

The INSERT uses `ON CONFLICT (conversation_id, request_id, tool_call_id, event_kind) DO NOTHING`. When a duplicate is sent — for example due to an at-least-once retry, a runner restart replaying events, or a test bug — `Exec` returns `nil` error and `CommandTag.RowsAffected() == 0`. `InsertToolInvocation` propagates the `nil` error, so `persistToolInvocation` and the Runner both see "success" while the second fact is silently dropped. Because the ledger is append-only and forensic, a silent no-op insert on the "end" event of a tool call means the end fact is permanently missing with no diagnostics.

The Runner's log line (`slog.Warn("tool invocation ledger insert failed …")`) never fires because there is no error.

**Suggested fix:**

After `Exec`, check `CommandTag.RowsAffected()`:

```go
tag, err := q.db.Exec(ctx, insertToolInvocation, ...)
if err != nil {
    return err
}
if tag.RowsAffected() == 0 {
    return fmt.Errorf("tool invocation duplicate suppressed (conversation=%s request=%s call=%s event=%s)",
        arg.ConversationID, arg.RequestID, arg.ToolCallID, arg.EventKind)
}
return nil
```

Callers that treat duplicate inserts as non-fatal can ignore the error class on their side, but the information is not swallowed.

Note: this is in generated sqlc code. The fix belongs in `internal/db/queries/tool_invocations.sql` (e.g. `ON CONFLICT … DO UPDATE SET id = EXCLUDED.id RETURNING (xmax = 0) AS inserted`) or by wrapping `InsertToolInvocation` in the store with a rows-affected check.

---

### [LOW][BUG] exitCodeOrNull performs silent int32 truncation instead of saturation

**Location:** `internal/toolinvocations/store.go:205`

**Confidence:** medium

**Detail:**

```go
func exitCodeOrNull(exitCode *int) pgtype.Int4 {
    if exitCode == nil {
        return pgtype.Int4{}
    }
    return pgtype.Int4{Int32: int32(*exitCode), Valid: true}
}
```

`int32(*exitCode)` is an unchecked narrowing conversion. For standard shell exit codes (0–255) this is harmless, but `exitCodeFromMeta` also handles `float64` (from JSON decode) and `int64` variants. A pathological meta value `exit_code: 2147483648` would result in `int32` wrapping to `-2147483648`, silently recording a negative exit code that never occurred. The rest of the file uses `clampInt32` exactly to prevent this. The inconsistency is a trap for future callers that produce non-POSIX exit codes (e.g., Windows NTSTATUS values).

**Suggested fix:**

```go
return pgtype.Int4{Int32: clampInt32(*exitCode), Valid: true}
```

---

### [LOW][DEAD-CODE] secretPattern.name field is write-only — never read outside the struct literal

**Location:** `internal/toolinvocations/redact.go:49`

**Confidence:** high

**Detail:**

`secretPattern` is defined with two fields: `name string` and `re *regexp.Regexp`. In `RedactForLedger` the loop iterates `secretPatterns` and accesses only `p.re`. The `name` field is populated in every struct literal but is never read at runtime. It is not exported, not logged, not used for debugging, and not referenced anywhere in the repo outside the struct definition. It exists solely as an annotation comment target.

**Suggested fix:**

Remove the `name` field from the struct and from all struct literals, or convert the slice to `[]*regexp.Regexp` directly. If pattern names are desired for future observability (e.g. logging which pattern matched), keep the field but document the intent and add at least one use.

## What was checked and found clean

- **Nil pointer dereference:** `eventFromRow` guards `r.ExitCode.Valid` before dereferencing; `toParams` guards `e.ExitCode == nil`; `uuidParam` returns a typed error — no nil-deref paths.
- **Unchecked errors:** All `json.Marshal` / `uuid.Parse` / `InsertToolInvocation` / `ListToolInvocationsByConversation` errors are checked and wrapped with `%w`.
- **Resource leaks:** `rows.Close()` is deferred in the generated code; no goroutines are started; no `time.Ticker` or `time.Timer` usage.
- **Race conditions:** The package is stateless after construction (`Store` holds only an immutable `*sqlc.Queries`); `secretPatterns` is a package-level `var` that is read-only after init; `capUTF8` and `RedactForLedger` are pure functions. No synchronization required.
- **Context propagation:** Both `Insert` and `ListByConversation` accept and forward `ctx` to the sqlc layer.
- **capUTF8 correctness:** The rune-boundary walk-back loop is correct; the `cut > 0` guard prevents going below index 0; the `utf8.RuneStart` check correctly identifies the start of multi-byte sequences. The only cosmetic point is that the stored string can exceed `capBytes` by up to 11 bytes (the `capMarker` overhead), which is intentional and documented.
- **eventFromRow round-trip:** All nullable columns use `.Valid` guards before reading `.String`/`.Int32`/`.Bool`; the meta unmarshal error is logged rather than swallowed (correct for a forensic read path).
- **toParams timestamp defaulting:** The start-event `started_at` defaulting (IN-01) is correct; the asymmetric lack of a similar guard for `ended_at` on end events is a potential trap for future emitters but is not currently exploited because `llm_agent_events.go` always sets `EndedAt` on end events.
- **Not-wired symbols:** `ListByConversation` is referenced in `internal/eval/skills_snippet_reuse_cot_eval_test.go` (an eval-tier test) and both integration tests — it is not wired to any production CLI or HTTP route, but this matches the package's role as an internal forensic store. `EventStart`, `EventEnd`, `ArgsRawCapBytes`, `ResultPreviewCapBytes`, and `RedactForLedger` are all referenced in production or test code outside the package.
