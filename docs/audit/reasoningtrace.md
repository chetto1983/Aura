# Audit: internal/reasoningtrace

**Verdict:** needs-work — two dead-code exports, one security correctness gap in redaction, one performance anti-pattern in hot paths.

**Counts:** critical 0 / high 1 / medium 2 / low 2

---

## Findings

### [HIGH][BUG] redactString on marshaled JSON fails for secrets with JSON-special characters

**Location:** `internal/reasoningtrace/reasoningtrace.go:76`

**Confidence:** high

**Detail:**
`Record` redacts individual string field values via `redactValue` before marshaling (line 69), then applies a second `redactString` pass on the entire JSON string (line 76). The second pass searches for the raw secret value using `strings.ReplaceAll`. However, `json.Marshal` HTML-escapes `"`, `\`, `<`, `>`, and `&` inside string values. If a secret (e.g., a password like `p@ss"word`) contains any of these characters, the JSON blob will contain the escaped form (`p@ss\"word`) while `redactString` looks for the unescaped form — the lookup fails and the secret leaks into the JSONL trace file.

Verified locally: `json.Marshal(map[string]any{"key": "p@ss\"word"})` produces `{"key":"p@ss\"word"}`, and `strings.Contains` of the raw form returns false.

For `string`-typed field values this is mitigated by `redactValue` running before marshaling, so the secret is replaced before JSON encoding. The exposure window is non-string field values that happen to embed a secret when serialized to JSON (uncommon in the current call sites, but structurally present).

**Suggested fix:**
Remove the post-marshal `redactString` pass entirely — it provides no additional protection once `redactValue` is called on all fields, and it silently fails for secrets with special chars. If defense-in-depth on the JSON blob is desired, scan for JSON-escaped variants of each secret value instead of the raw form, or use `json.HTMLEscape`-aware matching.

```go
// Remove line 76:
// line = []byte(redactString(string(line)))
```

---

### [MEDIUM][DEAD-CODE] Log and LogContext exported but never called outside the package

**Location:** `internal/reasoningtrace/reasoningtrace.go:40,45`

**Confidence:** high

**Detail:**
`Log` (line 40) and `LogContext` (line 45) are exported functions. A grep across the entire repo (`D:/Aura`) finds zero references to `reasoningtrace.Log` or `reasoningtrace.LogContext` in any file outside `reasoningtrace.go`. All callers in the codebase use `reasoningtrace.Record` directly. `Enabled` (line 22) is likewise only referenced within the package.

`Log` and `LogContext` also duplicate structure already in `Record`: they call `attrsToMap` to convert a variadic key-value list to a map, then pass it to `Record`. Since no callers exist, the conversion layer and the `attrsToMap` unexported helper are dead weight.

**Suggested fix:**
Remove `Log`, `LogContext`, and `attrsToMap`. If the slog-mirroring behavior (`slog.InfoContext`) is desired at call sites, callers can do it themselves. If these are intentional public API surface reserved for future use, document that explicitly with a `//nolint:deadcode` directive.

---

### [MEDIUM][BUG] redactString calls os.Environ() on every invocation — hot path

**Location:** `internal/reasoningtrace/reasoningtrace.go:135`

**Confidence:** high

**Detail:**
`redactString` calls `os.Environ()` on every invocation. `Record` calls `redactValue` per field (which calls `redactString` per string field), then calls `redactString` again on the full JSON blob. During a single LLM stream, `Record` is called 6+ times (verified: 6 calls each in `sse.go` and `llm_agent.go`), each with multiple string fields. This means `os.Environ()` — which allocates a fresh copy of the process environment — is called dozens of times per turn, inside the global `mu` lock.

Environment variables do not change at runtime in this process. The secrets list is stable after startup.

**Suggested fix:**
Cache the filtered secrets list at package init or on first use (sync.Once). Example:

```go
var (
    secretsOnce sync.Once
    cachedSecrets []struct{ upper, value string }
)

func loadSecrets() {
    secretsOnce.Do(func() {
        for _, env := range os.Environ() {
            name, value, ok := strings.Cut(env, "=")
            if !ok || len(value) < 8 { continue }
            upper := strings.ToUpper(name)
            if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") ||
               strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET") {
                cachedSecrets = append(cachedSecrets, struct{upper, value string}{upper, value})
            }
        }
    })
}
```

Note: if secrets are injected into the environment after init (unusual but possible in tests), the cache must be invalidated.

---

### [LOW][DEAD-CODE] Enabled() exported but never called outside the package

**Location:** `internal/reasoningtrace/reasoningtrace.go:22`

**Confidence:** high

**Detail:**
`Enabled()` is exported but no caller outside `internal/reasoningtrace/reasoningtrace.go` references it. Both `LogContext` (line 46) and `Record` (line 61) use it as an internal guard. If the intent was to let callers gate expensive argument construction before calling `Record`, no caller does so — they call `Record` directly and let `Record`'s own guard short-circuit. Keeping `Enabled` exported is not harmful but signals an incomplete or abandoned API design.

**Suggested fix:**
Either unexport it (`enabled()`) or add a doc comment clarifying its public contract and at least one call site demonstrating the pattern.

---

### [LOW][BUG] attrsToMap silently drops trailing element on odd-length input

**Location:** `internal/reasoningtrace/reasoningtrace.go:103`

**Confidence:** high

**Detail:**
The loop condition `i < len(attrs)-1` means if `attrs` has an odd number of elements, the last one is silently ignored (it becomes a dangling key with no value). No error, no log. This is a classic slog-style variadic API footgun. Since `Log` and `LogContext` are currently unreachable from outside the package (see finding reasoningtrace-2), the risk is latent rather than active, but if these functions are ever wired up, callers passing `"key", value, "orphan"` will silently lose the orphan.

**Suggested fix:**
Add a guard: if `len(attrs)%2 != 0`, emit a slog.Warn and/or include an `"_malformed_attrs"` key in the output. This matches slog's own behavior for odd-length key-value lists.

---

## Summary

Five non-overlapping issues. The high finding (JSON-escape bypass in `redactString`) is a structural correctness defect in the security-sensitive redaction path, though it only fires for secrets containing JSON-special characters, and `redactValue` does protect string-typed fields. The two medium findings (dead exported functions and hot `os.Environ()` inside the global mutex) are clean-up and performance issues. Two low findings cover latent API surface. No races, no nil-pointer derefs, no unclosed resources, no goroutine leaks.
