# Audit: internal/reasoningtrace

**Verdict:** needs-work — two not-wired exported symbols, one medium security gap (post-marshal redaction misses JSON-escaped secrets in non-string field values), and a performance hotspot in debug mode.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][NOT-WIRED] `Log` and `LogContext` are exported but never called outside the package

**Location:** `internal/reasoningtrace/reasoningtrace.go:40-57`
**Confidence:** high

`Log` delegates to `LogContext`, and `LogContext` calls both `slog.InfoContext` and `Record`. Neither is called anywhere in the repo outside the package itself. Grepped `reasoningtrace\.Log\b` and `reasoningtrace\.LogContext\b` across all `.go` files — zero external hits. `Record` is the only entry-point callers use (35+ call-sites in `internal/agent`, `internal/agui`, `internal/llm`, `internal/channels/telegram`). Both functions are thus dead API surface that carries the cost of a duplicated `Enabled()` check (inside `LogContext`) on top of the one callers would already have done.

**Suggested fix:** Either remove `Log`/`LogContext` (they are strict supersets of `Record` plus an slog mirror nobody uses), or demote them to unexported helpers (`log`/`logContext`) until a caller is added. If the slog mirror is intentional for future use, add a `//nolint:unused` annotation or a comment explaining the API reservation, so a `deadcode` sweep doesn't remove it silently.

---

### [MEDIUM][BUG] Post-marshal `redactString` cannot redact secrets that contain JSON-escapable characters

**Location:** `internal/reasoningtrace/reasoningtrace.go:76`
**Confidence:** medium

`Record` applies `redactValue` to each field before marshaling (correct — handles string, `[]any`, `map[string]any` values). After marshaling, it applies `redactString` on the raw JSON bytes again (line 76). `redactString` does a verbatim `strings.ReplaceAll(jsonLine, secretValue, "[REDACTED_...]")`. If a secret env var value contains any JSON-special character (`"`, `\`, newline, etc.), `json.Marshal` will escape those characters in the JSON output, so the verbatim search will not match. Concretely: a password like `p@ss"word` stored verbatim in an env var becomes `p@ss\"word` in the JSON line — the literal `strings.ReplaceAll` call misses it.

The impact is limited because:
- `redactValue` already catches string-typed field values pre-marshal, so the post-marshal pass is only a safety net for `stage` (always a hard-coded literal in practice) and non-string-typed fields (unlikely to contain secrets).
- All current callers pass hard-coded stage strings.

The gap is real but narrow given current call patterns.

**Suggested fix:** Apply `redactString` inside `redactValue` to each string before marshaling (already done), and remove the redundant post-marshal `redactString` call — or replace it with a redaction that works on the pre-marshal map entirely. If the post-marshal pass is kept as a belt-and-suspenders measure, document that it only catches ASCII-safe secrets.

---

### [LOW][BUG] `Enabled()` not exported but called by `Record` on every invocation — two `os.Getenv` calls per `LogContext` call

**Location:** `internal/reasoningtrace/reasoningtrace.go:45-57`
**Confidence:** high

`LogContext` checks `Enabled()` (one `os.Getenv` call), then calls `Record`, which checks `Enabled()` again (a second `os.Getenv` call). Under the disabled-by-default hot path the early-return in `Record` makes this cheap, but it adds an extra syscall for every `LogContext` invocation when tracing IS enabled. Not a correctness bug — just redundant work.

**Suggested fix:** Export a package-level `enabled bool` cached at init time, or have `LogContext` call the internal logic of `Record` directly after the single `Enabled()` check.

---

### [LOW][BUG] `redactString` calls `os.Environ()` on every invocation, O(env_size) per field value

**Location:** `internal/reasoningtrace/reasoningtrace.go:135`
**Confidence:** high

`redactString` calls `os.Environ()` every time it is invoked, iterating all environment variables. `redactValue` calls `redactString` recursively for every string leaf in the field map, and then `Record` calls `redactString` once more on the full JSON line. For a call like `openai_compat_request` (8 string fields, one of which is a large JSON blob), `os.Environ()` is read 9+ times in a single `Record` invocation. On a system with 100 env vars, each redact-sensitive env var adds an additional `strings.Contains` + conditional `strings.ReplaceAll` per field per env var.

This only matters when `AURA_REASONING_TRACE=1` (disabled by default), so it has no production latency impact. In a debug session tracing hundreds of SSE chunks per request, it adds measurable overhead.

**Suggested fix:** Cache the list of secret env var `(name, value)` pairs at first use with a `sync.Once` (or re-read at each `Record` call but only once per `Record`, passing the slice into `redactValue`). This reduces `os.Environ()` calls from O(fields) to O(1) per `Record` call.
