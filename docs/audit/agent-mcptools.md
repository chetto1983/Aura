# Audit: internal/agent/mcptools

**Verdict:** needs-work — two logic bugs (one silent semantic corruption, one schema passthrough defect) in otherwise well-structured code.
**Counts:** critical 0 / high 1 / medium 1 / low 1

## Findings

---

### [HIGH][BUG] `namespacedName`: namespace delimiter silently stripped for namespaces ≥ 62 characters

**Location:** `internal/agent/mcptools/name.go:57-58`
**Confidence:** high

**Detail:**
When `len(sanitizeName(namespace)) + 2` (prefix length) exceeds `budget = maxToolNameLen - len(suffix) = 51`, the code executes:

```go
return prefix[:budget] + suffix
```

`prefix` is `sanitizeName(namespace) + "__"`. When the sanitized namespace is ≥ 50 characters, `prefix` is ≥ 52 bytes. `prefix[:51]` truncates INTO or PAST the trailing `"__"` delimiter. For namespace length ≥ 62 chars (sanitized ≥ 62, prefix ≥ 64), `prefix[:51]` is all `n` characters — the `"__"` is gone entirely.

Verified with the production function:

```
ns=62n, tool="alpha":
result = "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn_5608cce1a837"
contains "__": false
```

The model-facing tool name looks like a non-namespaced tool, defeating the shadow-protection invariant (the tool can no longer be distinguished from a built-in by name prefix). Two different tools under the same 62+-char namespace produce distinct names (different hash suffixes), so the uniqueness property holds — only the namespace separator is lost.

The existing adversarial test suite (`TestNamespacedName_AdversarialLenSweep`, `TestNamespacedName_LongNamespaceCap`) checks length ≤ 64 and suffix-distinctness but NOT `"__"` presence for the long-namespace path, so this goes undetected.

**Suggested fix:**
After computing `budget` and detecting `len(prefix) > budget`, preserve the `"__"` delimiter by using the namespace portion only:

```go
if len(prefix) > budget {
    // Keep as many namespace chars as fit before the suffix; the
    // tool part is fully dropped (the hash distinguishes inputs).
    nsChars := budget - len(nsDelimiter)
    if nsChars < 0 {
        nsChars = 0
    }
    return sanitizeName(namespace)[:nsChars] + nsDelimiter[:budget-nsChars] + suffix
}
```

Or simpler: always include the delimiter if it fits, otherwise just use the namespace prefix truncated:

```go
if len(prefix) > budget {
    // Truncate to budget; if "budget" >= 2 keep the delimiter, else just ns chars.
    cut := prefix
    if len(cut) > budget {
        cut = cut[:budget]
    }
    return cut + suffix
}
```

The real fix is to ensure at least the `"__"` separator is preserved by computing the ns-portion budget as `budget - len(nsDelimiter)` and joining explicitly. Add a test asserting `strings.Contains(result, "__")` for all adversarial inputs.

---

### [MEDIUM][BUG] `bridgeTools`: JSON `null` `inputSchema` passes through as tool Parameters instead of falling back to `emptyObjectSchema`

**Location:** `internal/agent/mcptools/bridge.go:85-88`
**Confidence:** high

**Detail:**
The check for "no inputSchema" is:

```go
if len(strings.TrimSpace(string(d.InputSchema))) > 0 {
    params = d.InputSchema
}
```

`json.RawMessage("null")` has `TrimSpace = "null"`, `len = 4 > 0`, so it passes through as `params = json.RawMessage("null")`. This is valid JSON but not a valid JSON Schema object. Downstream:

- `tools/bm25.go:45`: `json.Unmarshal(s.Parameters, &node)` succeeds but parses `null` as a null node (no properties indexed for BM25 search).
- `tools/manifest.go:64`: `def.Function.Parameters = null` is sent to the OpenAI-compatible API, which typically rejects a `null` input schema with a 400 error (the API requires an object schema).

MCP servers are permitted to omit `inputSchema` (represented as `null` in JSON decoding of a `{"inputSchema": null}` field). This can happen with some MCP server implementations.

The current code also accepts whitespace-only `InputSchema` (e.g., `"   "`) but not `null`. The empty-string case (`json.RawMessage("")` or `nil`) is correctly handled.

**Suggested fix:**
Add a JSON-type check after the length check, falling back if the schema is not an object:

```go
params := emptyObjectSchema
raw := d.InputSchema
if len(raw) > 0 && json.Valid(raw) {
    var probe struct{ Type string `json:"type"` }
    if json.Unmarshal(raw, &probe) == nil && probe.Type != "" {
        params = raw
    } else if probe.Type == "" {
        // Could be a schema without "type" (still valid); pass through unless null.
        var check any
        if json.Unmarshal(raw, &check) == nil {
            if _, isNil := check.(nil); !isNil {
                params = raw
            }
        }
    }
}
```

Or more simply — check that the raw bytes are not the `null` literal:

```go
if len(strings.TrimSpace(string(d.InputSchema))) > 0 && !bytes.Equal(bytes.TrimSpace(d.InputSchema), []byte("null")) {
    params = d.InputSchema
}
```

---

### [LOW][BUG] `registerBridged`: type assertion `t.(*bridgedTool)` panics if caller passes a non-`*bridgedTool` element

**Location:** `internal/agent/mcptools/bridge.go:128`
**Confidence:** medium

**Detail:**
`registerBridged` is unexported but accepts `[]tools.Tool`, a public interface. The type assertion:

```go
bt := t.(*bridgedTool)
```

will panic if any element is not a `*bridgedTool`. Today the only caller is `Mount → Bridge → bridgeTools`, which only ever appends `*bridgedTool` — so this cannot trigger in production. However, there is no package-level protection against a future internal caller passing a different `tools.Tool` implementation, and there is no compile-time assertion. A missed assertion produces a runtime panic in a production mount, not a graceful error.

**Suggested fix:**
Change to a safe assertion that returns a graceful error:

```go
bt, ok := t.(*bridgedTool)
if !ok {
    return nil, fmt.Errorf("mcp bridge: internal error: non-bridgedTool passed to registerBridged (%T)", t)
}
```

This costs two lines and is zero-overhead on the happy path.

---

## What was checked

- All three non-test source files: `bridge.go`, `mount.go`, `name.go` — read in full.
- Test files read to understand intended invariants and coverage gaps.
- Greps across the full repo (`D:/Aura`) to confirm usage of every exported and unexported symbol.
- Live execution of `go test ./internal/agent/mcptools/` — all 25 tests pass.
- Manual tracing of `namespacedName` overflow branches for namespace lengths 50, 55, 62, 63 to find the delimiter-loss threshold.
- Manual tracing of `registerBridged` for 2-way and 3-way sanitize collisions.
- Downstream consumers of `spec.Parameters` traced through `tools/bm25.go` and `tools/manifest.go` to assess blast radius of the `null` schema passthrough.
- No races: `Registry` is built single-threaded at boot; `bridgedTool` is read-only after construction; no shared mutable state; no goroutines spawned in this package.
- No dead code: `Bridge` and `Server` are exported for external callers (spike scripts use them; they are a designed API surface); all unexported helpers are reachable through the exported entry points.
- No not-wired code: `MountServer` and `MountManagedServer` are called from `cmd/aura/main.go` (production boot). `Mount` is called from both `MountServer` and spike scripts.
