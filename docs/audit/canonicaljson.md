# Audit: internal/canonicaljson

**Verdict:** needs-work — one logic bug breaks the documented strict-reject guarantee for overflow numeric literals.

**Counts:** critical 0 / high 1 / medium 0 / low 0

---

## Findings

### [HIGH][BUG] `encodeNumber` bypasses the non-finite guard for overflow-range literals

**Location:** `internal/canonicaljson/canonicaljson.go:102–109`

**Confidence:** high

**Detail:**

`encodeNumber` guards against non-finite floats with:

```go
if f, err := strconv.ParseFloat(n.String(), 64); err == nil {
    if math.IsNaN(f) || math.IsInf(f, 0) {
        return fmt.Errorf("non-finite number %q", n.String())
    }
}
buf.WriteString(n.String())
return nil
```

When `strconv.ParseFloat` overflows float64 (e.g., `1e999`), it returns `(+Inf, ErrRange)`. The `err == nil` branch is skipped entirely, so the NaN/Inf test never executes, and the literal — which is semantically infinite — is emitted unchanged as `"1e999"`.

Verified:
- `strconv.ParseFloat("1e999", 64)` → `(+Inf, *NumError{ErrRange})`
- Current code: `encodeNumber(json.Number("1e999"))` → `"1e999"`, `nil` error
- Package contract (package comment + `encodeNumber` comment): "un-canonicalizable input (NaN, Inf, …) is strict-rejected with an error and nil bytes — never silently coerced"

This is a contract breach. A `json.RawMessage` containing `1e999` round-trips through `normalize()` as `json.Number("1e999")` and exits `Marshal` without error, producing output that represents a value no float64 can represent exactly — violating the dedup fingerprint integrity documented in D-08.

Affected inputs: any `json.Number` whose string `strconv.ParseFloat` returns an `ErrRange` error. This includes `1e999`, `1.7976931348623157e+309`, `-1e999`, etc.

**Suggested fix:**

```go
func encodeNumber(buf *bytes.Buffer, n json.Number) error {
    f, err := strconv.ParseFloat(n.String(), 64)
    if err == nil && (math.IsNaN(f) || math.IsInf(f, 0)) {
        return fmt.Errorf("non-finite number %q", n.String())
    }
    if err != nil {
        // ErrRange: the literal overflows float64 (semantically Inf) — reject it.
        return fmt.Errorf("non-finite number %q: %w", n.String(), err)
    }
    buf.WriteString(n.String())
    return nil
}
```

This makes the guard cover both the `NaN/Inf` direct case AND the `ErrRange` overflow case, matching the documented contract.

---

## What was checked and found clean

- **Nil-pointer / panic paths:** `bytes.Buffer` write methods never return errors and never panic on valid inputs. No pointer dereferences on values derived from `normalize()` output are possible since all types are controlled by the JSON decoder.
- **Error wrapping:** All `fmt.Errorf` calls use `%w`. No swallowed errors.
- **Resource leaks:** No goroutines, no files, no `io.Closer` types used. No leaks possible.
- **Races:** Entirely stateless pure functions. No shared mutable state.
- **Dead code:** All unexported helpers (`normalize`, `encode`, `encodeString`, `encodeNumber`, `encodeArray`, `encodeObject`) are reachable from `Marshal`. `Marshal` is used in 4 production call sites (`internal/agent/llm_agent.go`, `internal/agent/workflow/loop.go`, `internal/agent/prompt/hash.go`, `cmd/aura/cache_audit.go`).
- **Not-wired code:** None. The single exported symbol `Marshal` is wired into every relevant subsystem.
- **Trailing-token vulnerability:** `normalize` calls `json.Marshal(v)` first, which validates the input and produces a single complete JSON value. The subsequent `Decode` therefore always consumes exactly that one value with no trailing data.
- **Key sort stability:** `sort.Strings` is deterministic and total-ordered. Correct.
- **String encoding:** `encodeString` delegates to `json.Marshal(string)`, which handles invalid UTF-8 via Unicode replacement characters — consistent and deterministic.
- **Loop var capture:** Go module is `go 1.26.4`; loop variable capture bug does not apply.
