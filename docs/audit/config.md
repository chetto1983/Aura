# Audit: internal/config

**Verdict:** needs-work — one genuinely unwired config knob, one confusing deferred-error UX, and a misleading parse fallback; no critical bugs or races.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][NOT-WIRED] `HistoryHardCapTurns` — config knob parsed but never consumed

**Location:** `internal/config/config.go:55,235`
**Confidence:** high

`HistoryHardCapTurns` (`AURA_HISTORY_HARD_CAP_TURNS`, default 50) is declared in `Config`, populated from the env by `loadBase()`, documented in the PRD as "L2.5 picobot hard rolling buffer cap", and tested for its default value in phase-4 verification docs. It is never read outside `internal/config`. No grep match for `.HistoryHardCapTurns`, `HardCapTurns`, or `HISTORY_HARD_CAP` appears in any non-config `.go` file (confirmed across the whole repo).

The PRD §Slice 1.8 describes this as an L2.5 rolling-buffer cap that `runner.Deps` should receive, analogous to `EvictAfter` (which IS wired at `cmd/aura/chat.go:181`). The runner struct at `internal/runner/runner.go:52` has no corresponding field, and `bootChatEnv` in `cmd/aura/chat.go` does not pass it.

**Impact:** an operator setting `AURA_HISTORY_HARD_CAP_TURNS` to any value gets silently ignored. The PRD-intended L2.5 cap is never enforced, regardless of configuration.

**Suggested fix:** Add a `HardCapTurns int` field to `runner.Deps` and pass `cfg.HistoryHardCapTurns` at the call site in `bootChatEnv`. Wire it through to `conversations.ApplyContextLadder` (or wherever the rolling-buffer drop is meant to occur). Alternatively, if the feature was deliberately deferred, add a `//nolint:unused` comment and a TODO referencing the PRD slice, so this doesn't look like accidental omission.

---

### [MEDIUM][BUG] `parseMCPServersJSON` silently falls through when `"mcpServers"` key is present but JSON-null

**Location:** `internal/config/config.go:347-366`
**Confidence:** high

```go
if wrapped.MCPServers != nil {
    return validateMCPServers(wrapped.MCPServers)
}
// fallthrough: attempt direct-map unmarshal of the ENTIRE JSON object
```

When the env var contains `{"mcpServers": null, ...}`, Go's JSON decoder sets `wrapped.MCPServers` to `nil`. The guard `!= nil` is false, so the code falls through and re-unmarshals the whole JSON blob as a flat `map[string]mcp.ServerConfig`. Every top-level key in the original object — including the literal key `"mcpServers"` — becomes a server name entry with `Command: ""`. `validateMCPServers` then returns an error `server "mcpServers" command cannot be empty`, which is stored in `MCPServersErr` (not returned from `Load`/`LoadDB`) and surfaces only when `buildRegistryWithMCP` is first called.

The operator sees: `mcp: AURA_MCP_SERVERS_JSON: server "mcpServers" command cannot be empty` — a confusing message that names `"mcpServers"` as if it is a server, giving no hint that the root cause is a null value in the JSON.

**Suggested fix:** Distinguish JSON-null from key-absent by checking whether the wrapper key was present before falling through:

```go
var wrapper struct {
    MCPServers *map[string]mcp.ServerConfig `json:"mcpServers"`
}
if err := json.Unmarshal([]byte(raw), &wrapper); err != nil { ... }
if wrapper.MCPServers != nil {
    return validateMCPServers(*wrapper.MCPServers)
}
// wrapper.MCPServers == nil means no "mcpServers" key at all — try direct format
```

With a pointer-to-map, both absent and null produce `nil`; the operator-intent ambiguity is resolved by also checking for the presence of any `"mcpServers"` key with explicit error if it's null.

---

### [LOW][NOT-WIRED] `Load()` / `LoadDB()` doc-contract gap: `MCPServersErr` deferred-error is undocumented

**Location:** `internal/config/config.go:146-173`
**Confidence:** high

Both `Load()` and `LoadDB()` may return a non-nil `*Config` while simultaneously having `config.MCPServersErr != nil`. The doc-comment on `Load()` mentions only the LLM error path (`ErrMissingAPIKey`) as a failure mode; `MCPServersErr` is not mentioned. Callers who check only the returned `error` value assume a non-error return means the config is fully valid. The `MCPServersErr` field is a deliberately deferred error (not a design bug — it avoids blocking `aura db migrate` when MCP config is malformed), but this contract is not documented on the public surface.

**Suggested fix:** Add a note to the `Load()` / `LoadDB()` godoc:

```
// MCP server parse errors are not returned here; they are stored in
// Config.MCPServersErr and surfaced when the agent registry is built.
// Callers that mount MCP servers (e.g. buildRegistryWithMCP) must check
// this field explicitly.
```

---

### [LOW][BUG] `envBoolDefault` silently ignores `"yes"` / `"on"` / `"YES"` — security-adjacent for CORS toggle

**Location:** `internal/config/config.go:416-426`
**Confidence:** medium

`envBoolDefault` delegates to `strconv.ParseBool`, which accepts `{1,t,T,TRUE,true,True,0,f,F,FALSE,false,False}` and rejects everything else (returns fallback). An operator who sets `AURA_AGUI_CORS_PERMISSIVE=yes` expecting permissive CORS silently gets the default (`false` = restrictive). In this specific case the fallback is the safer direction. However the same silent-fallback affects `AURA_VISION_CLOUD=yes` (silently stays local-sidecar mode) and all other bool knobs. A typo that silently keeps the default is hard to diagnose.

There is no log warning emitted when a malformed value is discarded.

**Suggested fix:** Emit a `slog.Warn` on parse failure so operators discover misconfigured env vars at boot rather than at runtime (already done for hard-fail keys via `llm.Load`). Example:

```go
b, err := strconv.ParseBool(v)
if err != nil {
    slog.Warn("config: malformed bool env var, using default", "key", key, "value", v, "default", fallback)
    return fallback
}
```

---

## What was checked and found clean

- **Nil-pointer dereference:** `Load()` and `loadBase()` never dereference a pointer before checking; `composeDSN` early-returns on empty password before URL construction.
- **Unchecked errors:** All errors from `mcp.ManagedConfigPath`, `mcp.LoadManagedConfig`, `mcpmanager.RuntimeServers`, `mcpmanager.RunnableManagedServers`, and `parseMCPServersJSON` are either returned or stored in `MCPServersErr`. `godotenv.Load()` is explicitly discarded with `_` (intentional best-effort).
- **`composeDSN` injection:** `url.UserPassword` percent-encodes credentials; `RawPath: "/" + url.PathEscape(dbname)` is load-bearing — without it, `url.URL.EscapedPath()` would not encode slashes in the dbname, leaking path segments. The test `TestComposeDSNEscapesComponents` covers this. `q.Set("sslmode", ...)` + `q.Encode()` percent-encodes the sslmode value, preventing query-string injection.
- **Race conditions:** No goroutines or shared mutable state in this package. `loadBase()` reads env vars synchronously; no global variables are mutated post-init.
- **Dead code (unexported):** `auraHomeDir`, `defaultSkillsDir`, `defaultSkillExportDir`, `defaultRunDir`, `defaultSkillInjectionBlocklist`, `envDefault`, `envIntDefault`, `envBoolDefault`, `envSliceDefault`, `composeDSN`, `loadMCPServers`, `parseMCPServersJSON`, `validateMCPServers` — all called within `loadBase()` or `Load()`. None are dead.
- **`defaultOtelExporter`/`defaultOtelEndpoint` constants:** used at lines 230-231; not dead.
- **`envSliceDefault` mutation hazard:** The fallback slice is constructed fresh each call via `defaultSkillInjectionBlocklist()` (which returns a new `[]string` literal), so there is no shared-slice aliasing risk.
- **Integer overflow (`envIntDefault`):** `strconv.Atoi` returns a platform-sized `int`; callers use `int` fields. No overflow on 64-bit platforms. No conversion to smaller types in this package.
