# Audit: internal/toolinvocations

**Verdict:** needs-work — one regex pattern causes significant over-redaction that destroys forensic context; one field-validity logic produces inconsistent NULL/value states; one pattern has a short-value under-redaction gap.

**Counts:** critical 0 / high 1 / medium 2 / low 1

---

## Findings

### [HIGH][BUG] `authorization_header` pattern is unbounded — greedily redacts entire line after Authorization keyword

**Location:** `internal/toolinvocations/redact.go:59`
**Confidence:** high

**Detail:**
The pattern `(?i)authorization\s*[:=]\s*[^\r\n]+` uses `[^\r\n]+` (everything to end of line). Tool argument JSON is a single logical line (no embedded `\r\n`), so the pattern matches from `Authorization:` to the end of the entire string. In a production shell_exec invocation like:

```
{"command":"curl -X POST https://api.example.com/v1/chat -H \"Authorization: Bearer sk-real-secret\" -H \"Content-Type: application/json\" -d '{\"model\":\"gpt-4\"}'"}
```

The result is:
```
{"command":"curl -X POST https://api.example.com/v1/chat -H \"[REDACTED]
```

Everything after `Authorization: Bearer sk-real-secret` — including the Content-Type header, request body, and closing JSON — is destroyed. Verified via Go regex: 101 chars lost in a typical multi-header curl call, the URL survives but all trailing headers and the request body are gone. This defeats the forensic purpose of the ledger for debugging multi-header API calls.

**Suggested fix:** Bound the value match to the token itself, not "rest of line." For HTTP header form, the credential value ends at a closing quote, whitespace, or `"` (when embedded in JSON). A tighter pattern:

```go
{"authorization_header", regexp.MustCompile(`(?i)authorization\s*[:=]\s*\S+`)},
```

Or, to handle the `Bearer <token>` sub-form explicitly:

```go
{"authorization_header", regexp.MustCompile(`(?i)authorization\s*[:=]\s*(?:bearer\s+)?[^\s"',\]]+`)},
```

This redacts only the credential value, leaving the rest of the argument intact for forensics.

---

### [MEDIUM][BUG] `json_credential` pattern skips short secrets (< 4 chars) leaving them unredacted

**Location:** `internal/toolinvocations/redact.go:70`
**Confidence:** high

**Detail:**
The pattern `(?i)"(password|api[_-]?key|token|secret)"\s*:\s*"[^"]{4,}"` requires a minimum 4-character value (`{4,}`). A JSON payload like `{"token":"abc"}` (3-char value) is skipped by `json_credential` and also not caught by `inline_credential` (because `inline_credential`'s value charset `[^"\s&]+` stops at the opening `"` of the JSON value, preventing a match on the `token":` pattern).

Verified in Go: `{"token": "abc", "other": "safe"}` passes through all patterns unredacted.

The `{4,}` bound exists to avoid false-positives on empty/trivially-short strings, but 1-3 character secrets — while rare — are plausible for pin codes, short API slugs, or test tokens in CI.

**Suggested fix:** Lower to `{1,}` or `+` (one or more characters), which is safe since the pattern requires both a known keyword and JSON quoting syntax to match:

```go
{"json_credential", regexp.MustCompile(`(?i)"(password|api[_-]?key|token|secret)"\s*:\s*"[^"]+"`)},
```

---

### [MEDIUM][BUG] Inconsistent NULL/value state when `ArgsBytes > 0` but `Arguments == ""`

**Location:** `internal/toolinvocations/store.go:142-143`
**Confidence:** medium

**Detail:**
The validity condition for both `ArgsRaw` and `ArgsBytes` is:

```go
ArgsRaw:   textOrNull(RedactForLedger(e.Arguments, ArgsRawCapBytes), e.Arguments != "" || e.ArgsBytes > 0),
ArgsBytes: int4OrNull(e.ArgsBytes, e.Arguments != "" || e.ArgsBytes > 0),
```

When a caller sets `e.ArgsBytes > 0` but `e.Arguments == ""` (e.g., arguments were pre-truncated by the caller before being placed in the struct), `RedactForLedger("")` returns `""` immediately (line 81 early exit). The result is that `args_raw` stores an empty string (not NULL) while `args_bytes` stores the positive count. A forensic reader sees a non-zero byte count alongside a blank `args_raw` — a contradictory state.

In the current production code path (`runner/runner_persist.go`), `Arguments` and `ArgsBytes` are always both set or both unset from the same source (`ti.Arguments` / `ti.ArgsBytes`), so this is not triggered today. But it is a latent correctness issue at the store boundary since any future emitter can independently set one field.

**Suggested fix:** When `e.Arguments == ""`, force `ArgsRaw` to NULL regardless of `ArgsBytes`, by splitting the validity conditions:

```go
ArgsRaw:   textOrNull(RedactForLedger(e.Arguments, ArgsRawCapBytes), e.Arguments != ""),
ArgsBytes: int4OrNull(e.ArgsBytes, e.Arguments != "" || e.ArgsBytes > 0),
```

---

### [LOW][BUG] `exitCodeOrNull` silently truncates exit codes outside int32 range

**Location:** `internal/toolinvocations/store.go:205`
**Confidence:** low

**Detail:**
```go
return pgtype.Int4{Int32: int32(*exitCode), Valid: true}
```

`ExitCode` is `*int` (platform-native width). On a 64-bit system, values outside `[-2147483648, 2147483647]` silently truncate (not clamp) to the lower 32 bits — producing a wrong (possibly negative) exit code in the ledger. All other int→int32 conversions in this package use `clampInt32` (lines 125, 175-176). The inconsistency is intentional for exit codes (which are POSIX 0-255 or Windows 0-65535 in practice), but a future synthetic exit code (e.g., a sentinel `-1 << 32`) would silently corrupt.

**Suggested fix:** Apply `clampInt32` for consistency and safety:

```go
return pgtype.Int4{Int32: clampInt32(*exitCode), Valid: true}
```

---

## What was checked

- `store.go`: `Insert`, `ListByConversation`, `toParams`, all helper functions (`uuidParam`, `timestamptz`, `textOrNull`, `int4OrNull`, `clampInt32`, `int8OrNull`, `boolOrNull`, `exitCodeOrNull`, `jsonBytes`, `eventFromRow`). No goroutines, no shared mutable state, no resource leaks. pgx `rows.Close()` is in generated code with `defer`. Context propagated correctly through all DB calls.
- `redact.go`: All 6 regex patterns, `RedactForLedger`, `capUTF8`. UTF-8 boundary walk is correct. No races (all package-level vars are read-only after init). The `capMarker` (11 bytes, 9 chars) inflates stored output beyond `capBytes` by 11 bytes, which is safe since the `text` column is unbounded — the comment "bounds the durable column to 8 KiB" is slightly inaccurate but harmless.
- Dead code: all unexported helpers (`uuidParam`, `timestamptz`, `textOrNull`, `int4OrNull`, `int8OrNull`, `boolOrNull`, `exitCodeOrNull`, `jsonBytes`, `eventFromRow`, `capUTF8`, `clampInt32`, `secretPattern` type, `secretPatterns` var, `redactedPlaceholder`, `capMarker`) are used within the package. `RedactForLedger`, `ArgsRawCapBytes`, `ResultPreviewCapBytes`, `EventStart`, `EventEnd`, `New`, `Store`, `Event`, `Insert`, `ListByConversation` are all consumed externally (runner, cmd/aura, eval).
- Not-wired code: none found. `Store.New` is wired at `cmd/aura/chat.go:144`. The `ToolInvocationStore` interface (`runner/interfaces.go:76`) is satisfied by `*Store` and injected at multiple production sites.
- Races: the package has no goroutines; `secretPatterns` is a read-only package-level var (safe for concurrent reads); no maps written concurrently.
- The DB schema's `tool_invocations_event_shape` CHECK correctly enforces `started_at NOT NULL` for start events. The `toParams` default (lines 116-118) correctly handles forgetful emitters. Confirmed via test `TestEventToParams_StartDefaultsStartedAt`.
