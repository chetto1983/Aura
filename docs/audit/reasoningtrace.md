# Audit: internal/reasoningtrace

**Verdict:** needs-work — two not-wired exported APIs, one repeated syscall per trace field, one redaction gap for non-string nested types.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][not-wired] `Log` and `LogContext` exported but never called

**Location:** `internal/reasoningtrace/reasoningtrace.go:40-57`
**Confidence:** high

`Log` and `LogContext` are the two highest-level exported entry points (they call `slog.InfoContext` in addition to `Record`). Grep across the entire repo (`D:/Aura/**/*.go`) finds zero non-definition, non-test references to `reasoningtrace.Log` or `reasoningtrace.LogContext`. Every external caller goes directly to `Record`. The slog mirroring path they provide (which would feed structured logs into the process log output) is therefore completely unreachable in production.

**Suggested fix:** Either remove `Log`/`LogContext` (if the slog mirroring is intentionally foregone) or replace the direct `reasoningtrace.Record(...)` call sites with `reasoningtrace.Log(...)` / `reasoningtrace.LogContext(ctx, ...)` where a context is available. Keeping dead exported API inflates the public surface and invites future misuse.

---

### [MEDIUM][bug] `os.Environ()` syscall called once per string field per `Record` invocation

**Location:** `internal/reasoningtrace/reasoningtrace.go:134-149` (`redactString`), called from `redactValue` line 116 and from `Record` line 76.

**Confidence:** high

`redactString` calls `os.Environ()` each time it is invoked. `Record` invokes `redactValue` on every field value in the `fields` map (line 68-70), and `redactValue` calls `redactString` for every `string`-typed value (line 116). After marshaling, `Record` calls `redactString` once more on the entire JSON blob (line 76). A single `Record` call with N string fields therefore issues N+1 `os.Environ()` syscalls.

Under active tracing (e.g., the SSE parsing loop in `internal/llm/openai_compat/sse.go` emits one `Record` per token delta), this becomes a per-chunk `os.Environ()` storm. `os.Environ()` is not free — it copies the entire process environment.

**Suggested fix:** Cache the result of `os.Environ()` at package init time (or lazily on first call, protected by `sync.Once`) into a pre-parsed `[]redactRule` slice. Refresh only if needed (process environment is static for a long-running daemon). Alternatively, call `redactString` once on the final JSON blob only (remove the per-value `redactValue` string redaction) to reduce it to a single `os.Environ()` call per `Record`.

---

### [LOW][bug] `redactString` misses secrets whose JSON encoding differs from raw value

**Location:** `internal/reasoningtrace/reasoningtrace.go:76` and `134-149`

**Confidence:** medium

`redactValue`'s `default:` branch (line 129) passes non-string, non-slice, non-map values through unmodified. If such a value serializes to a JSON object or string that embeds a secret (e.g., a custom `MarshalJSON` method on a struct that includes an API token), the first redaction pass (`redactValue`) leaves it intact. The second pass (`redactString` on the JSON blob, line 76) searches for the raw secret value as a plain substring of the marshaled JSON. If the secret contains any character JSON would escape (`"`, `\`, non-ASCII code points, control characters), the raw value will not match the escaped representation and the secret leaks into the JSONL file.

Current callers pass only plain ASCII tokens and passwords, so the practical risk is low — but the assumption is implicit and not enforced.

**Suggested fix:** Either restrict `Record`'s `fields` values to `string`/`int`/`bool` only (enforced via a typed map or by convention in a doc comment), or apply `redactValue` recursively to struct fields by adding a JSON round-trip: `json.Unmarshal(json.Marshal(v))` into `map[string]any` before calling `redactValue`. The simplest safe change is to note the assumption in a code comment and add a test case with a JSON-escapable secret.

---

### [LOW][not-wired] `Enabled` and `Env` constant exported but unreferenced externally

**Location:** `internal/reasoningtrace/reasoningtrace.go:16` (`Env`), `22-29` (`Enabled`)

**Confidence:** high

`Enabled()` is called internally (inside `Record` and `LogContext`) but never from any external package. `Env` (the constant naming the env-var key `"AURA_REASONING_TRACE"`) has zero external references. These are minor dead-surface exports that add noise to the package API. Note that since `Log`/`LogContext` are also unused (finding above), callers never reach the `Enabled()` guard through them either.

**Suggested fix:** If the intent is to allow callers to conditionally skip building expensive trace arguments (a valid pattern), keep `Enabled()` exported but add a doc comment explaining that purpose. Otherwise demote to unexported `enabled()`. `Env` can be demoted to an unexported `const envKey` since no external code needs to reference it by name.

---

## What was checked and found clean

- **Mutex coverage:** `mu` correctly wraps the full open/write/close critical section on lines 79-93. No read of the file path or file descriptor occurs outside the lock.
- **Context propagation:** `LogContext` accepts and propagates `ctx` to `slog.InfoContext`; the nil-ctx guard on line 49 is correct.
- **File handling:** `f.Close()` is deferred on line 90; write errors are logged (not swallowed) on line 91-92.
- **No goroutine leaks:** `Record` is entirely synchronous; no goroutines are spawned.
- **No data races:** `mu` serializes all file I/O. `os.Getenv`/`os.Environ` are thread-safe Go runtime calls.
- **`attrsToMap` odd-length:** the loop `i < len(attrs)-1` silently drops a trailing unpaired key, which matches `log/slog` convention and is not a bug.
- **`append(line, '\n')`:** safe; `line` is a freshly allocated `[]byte` from a string conversion (no aliasing).
- **Go version:** module is `go 1.26.4`; loop-variable capture fix applies; no pre-1.22 loop bugs possible.
- **`RuneLen`:** trivially correct wrapper for `utf8.RuneCountInString`.
