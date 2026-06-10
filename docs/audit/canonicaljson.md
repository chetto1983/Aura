# Audit: internal/canonicaljson

**Verdict:** needs-work (low) — one inaccurate comment; logic, races, dead code, and wiring are all clean.

**Counts:** critical 0 / high 0 / medium 0 / low 1

## Findings

### [LOW][BUG] Inaccurate comment: underflow does not return `ErrRange` in Go 1.26

**Location:** `internal/canonicaljson/canonicaljson.go:104-105`

**Confidence:** high

**Detail:**

The comment on lines 104-105 reads:

> Underflow (1e-999 → 0 with ErrRange) is finite and correctly passes through.

This is factually wrong. `strconv.ParseFloat("1e-999", 64)` returns `(0, nil)` in Go 1.22+ (including 1.26); it does NOT return `ErrRange` for underflow-to-zero. The ErrRange path only fires for **overflow** (e.g. `1e9999 → +Inf`). Confirmed locally:

```
ParseFloat("1e-999"): f=0, err=<nil>
ParseFloat("5e-325"): f=0, err=<nil>
ParseFloat("1e9999"): f=+Inf, err=strconv.ErrRange, IsInf=true
```

The **code** is correct — it ignores `err` entirely and only inspects `math.IsNaN(f)` / `math.IsInf(f, 0)`, so both cases behave as intended regardless of the error value. But the comment describing ErrRange for underflow is wrong and will mislead future maintainers reasoning about the error-path.

**Suggested fix:**

```go
// ParseFloat returns +Inf (with ErrRange) on overflow literals such as 1e999, so
// inspect the parsed value directly rather than gating on err == nil.
// Underflow (e.g. 1e-999 → 0, err=nil) is finite and passes through correctly.
```

---

## Clean (everything else)

**Bugs:** No nil dereferences, no unchecked errors that matter (`bytes.Buffer.Write*` never error; `json.Marshal(string)` never errors; `ParseFloat` error is intentionally ignored and the code is correct without it). No resource leaks — the function is synchronous and uses only `bytes.Buffer` (no IO, no file handles, no rows). No integer conversions. No incorrect `%w` wrapping. No context to propagate (pure serializer).

**Races:** The package is stateless — no package-level variables, no caches, no goroutines. `Marshal` is a pure function. Concurrent callers are safe by construction.

**Dead code:** All five unexported helpers (`normalize`, `encode`, `encodeString`, `encodeNumber`, `encodeArray`, `encodeObject`) are reachable from `Marshal`. The only exported symbol, `Marshal`, is called from four production sites confirmed by grep:
- `cmd/aura/cache_audit.go:264`
- `internal/agent/llm_agent.go:539`
- `internal/agent/workflow/loop.go:290`
- `internal/agent/prompt/hash.go:35`

The `default` branch in `encode`'s type switch is unreachable via `normalize`-normalized input (which can only produce `nil`, `bool`, `json.Number`, `string`, `[]any`, `map[string]any`) but is a legitimate defensive guard for any future direct caller of `encode`. Not dead code worth removing.

**Not-wired:** N/A — this is a pure library package with no commands, handlers, or flags to wire.

**Tests checked:** Unit tests cover key-order independence (flat + nested), distinct number literals (D-08 invariant), strict rejection of NaN/Inf/overflow/func/chan, UUID input, idempotency, and fuzz + property-based round-trip. Coverage is comprehensive for the package surface.
