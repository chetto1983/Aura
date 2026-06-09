# Audit: internal/canonicaljson

**Verdict:** needs-work — one not-wired guarantee (D-08 number distinction) in production callers; one silent coercion violates strict-reject contract.

**Counts:** critical 0 / high 0 / medium 1 / low 1

---

## Findings

### [MEDIUM][NOT-WIRED] D-08 numeric-literal distinction silently bypassed by production callers

**Location:** `internal/canonicaljson/canonicaljson.go:1-16` (package contract) vs `internal/agent/llm_agent.go:536` and `internal/agent/workflow/loop.go:287` (callers)

**Confidence:** high

**Detail:**

The package contract (D-08, package-level comment) states:

> numbers are preserved as literal text via json.Number (UseNumber), so 1 and 1.0 stay DISTINCT and never round-trip through float64

This guarantee holds only when the caller supplies a `json.RawMessage` or a `json.Number`-typed value. Both production callers — `canonicalArgs` in `llm_agent.go` and `canonArgs` in `loop.go` — decode the LLM's raw argument string with plain `json.Unmarshal` (no `UseNumber`) before passing the result to `Marshal`:

```go
// llm_agent.go:534-543
func canonicalArgs(rawArgs string) []byte {
    var v any
    if err := json.Unmarshal([]byte(rawArgs), &v); err != nil {  // all numbers -> float64
        return []byte(rawArgs)
    }
    canon, err := canonicaljson.Marshal(v)
    ...
}
```

Plain `json.Unmarshal` decodes every JSON number as `float64`, so `{"n":1}` and `{"n":1.0}` both become `map[string]any{"n": float64(1)}` before canonicaljson is invoked. After normalization, `json.Marshal(float64(1))` produces `"1"` for both inputs. The callers therefore produce **identical** canonical bytes for what the LLM intended as distinct literals, collapsing dedup fingerprints for tool calls that differ only in number representation.

Verified by in-package probe:
```
production path {n:1}:   {"n":1}
production path {n:1.0}: {"n":1}   <- same, guarantee is inactive
```

The `prompt.PrefixHash` caller (`internal/agent/prompt/hash.go:35`) calls `canonicaljson.Marshal(msgs[i])` where `msgs[i]` is an `llm.Message` struct — `json.Marshal` on a struct field uses the field's Go type, not literal text preservation. Confirmed: `UseNumber` is called zero times across all four production call sites (grep across `internal/` and `cmd/` confirms).

In practice the LLM consistently uses one representation per semantic value, so this gap rarely causes a missed dedup. However, it means D-08 is documented as a property of the package but is not enforced on the hot path, and a future change that passes raw JSON strings through a different path could silently break dedup correctness.

**Suggested fix:**

Replace plain `json.Unmarshal` in both callers with a `json.Decoder` with `UseNumber`:

```go
func canonicalArgs(rawArgs string) []byte {
    dec := json.NewDecoder(strings.NewReader(rawArgs))
    dec.UseNumber()
    var v any
    if err := dec.Decode(&v); err != nil {
        return []byte(rawArgs)
    }
    canon, err := canonicaljson.Marshal(v)
    if err != nil {
        return []byte(rawArgs)
    }
    return canon
}
```

Apply the same fix to `canonArgs` in `internal/agent/workflow/loop.go:285-295`. This makes the D-08 guarantee active on the production path.

---

### [LOW][BUG] `json.Number("")` silently coerced to `"0"` — violates strict-reject contract

**Location:** `internal/canonicaljson/canonicaljson.go:49-61` (`normalize`)

**Confidence:** high

**Detail:**

The package contract states: "un-canonicalizable input (NaN, Inf, func, chan, ...) is strict-rejected with an error and nil bytes — never silently coerced."

However, `json.Marshal(json.Number(""))` returns `[]byte("0")` (not an error) because the Go stdlib treats an empty `json.Number` as `0`. This means `Marshal(json.Number(""))` silently returns `[]byte("0")` instead of an error:

```
Marshal(json.Number("")): result=0 err=<nil>   // observed
```

`normalize()` calls `json.Marshal(v)` first, so the coercion happens before the package has a chance to reject it. The empty-string `json.Number` is a semantically invalid input (it carries no numeric meaning), but canonicaljson accepts it.

This is a contract violation, not a safety hazard: no real caller (LLM JSON decode, struct marshal, or `json.RawMessage`) constructs `json.Number("")` under normal operation. The risk of a false-dedup collision from this path in production is negligible.

**Suggested fix:**

Add a guard in `normalize` or `encodeNumber`:

```go
func encodeNumber(buf *bytes.Buffer, n json.Number) error {
    if n.String() == "" {
        return fmt.Errorf("empty json.Number literal")
    }
    if f, _ := strconv.ParseFloat(n.String(), 64); math.IsNaN(f) || math.IsInf(f, 0) {
        return fmt.Errorf("non-finite number %q", n.String())
    }
    buf.WriteString(n.String())
    return nil
}
```

Alternatively, add `if s := n.String(); s == "" { return nil, fmt.Errorf(...) }` in `normalize` before calling `json.Marshal`.

---

## What was checked and found clean

- **Nil-pointer derefs:** none. `normalize` and all `encode*` functions receive typed values; no pointer is dereferenced without a guard.
- **Unchecked errors:** `buf.Write*` calls return no error (bytes.Buffer contract); all other errors are checked and propagated.
- **`%w` wrapping:** two wrap sites in `Marshal` (`canonicalize value: %w`, `encode canonical json: %w`) are correct.
- **Resource leaks:** no goroutines, no I/O, no closeable resources. `bytes.Buffer` is stack-allocated.
- **Races:** no shared mutable state; no goroutines started; pure stateless functions.
- **Dead code:** all unexported functions (`normalize`, `encode`, `encodeString`, `encodeNumber`, `encodeArray`, `encodeObject`) are called within the package. `Marshal` is exported and imported by four production callers (`cmd/aura/cache_audit.go`, `internal/agent/llm_agent.go`, `internal/agent/workflow/loop.go`, `internal/agent/prompt/hash.go`) plus two test files.
- **`encodeNumber` overflow guard:** The `_ =` discard of `ParseFloat`'s error is intentional and correct. Overflow literals (e.g. `1e9999`) parse to `±Inf` (returned even with `ErrRange`), which `math.IsInf` catches. Underflow literals (e.g. `1e-9999`) parse to `0`, which is finite and passes correctly. Syntactically invalid inputs are caught upstream by `json.Marshal` in `normalize`.
- **Map iteration non-determinism:** correctly addressed by `sort.Strings(keys)` in `encodeObject`.
- **Integer overflow / off-by-one:** no integer arithmetic beyond slice indexing.
