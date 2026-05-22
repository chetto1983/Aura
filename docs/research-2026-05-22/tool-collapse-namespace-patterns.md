# Tool surface collapse and namespace patterns

Research sources: `D:/tmp/{picobot, openhuman, cli-printing-press, codex}` snapshots at 2026-05-22.
Aura today: 22 native tools + N dynamic MCP tools. Target: 10-15 max model-visible at any time.
Aura pain point (user 2026-05-22): "rag deve auto-aggiornare sui nuovi tool, non hardcoded alla cazzo."

Pattern catalogue; no adoption decisions. Rating legend: 1 = anti-pattern for Aura, 5 = drop-in.

---

## 1. Kitchen-sink action enum

### picobot — `FilesystemTool` collapses 3 tools into 1
`D:/tmp/picobot/internal/agent/tools/filesystem.go:40-60`

```go
func (t *FilesystemTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "action": map[string]interface{}{
                "type": "string",
                "description": "The filesystem operation to perform",
                "enum":        []string{"read", "write", "list"},
            },
            "path":    map[string]interface{}{"type": "string", ...},
            "content": map[string]interface{}{"type": "string",
                "description": "Content to write (required when action is 'write')"},
        },
        "required": []string{"action", "path"},
    }
}
```

Dispatch: `switch action { case "read": ... case "write": ... case "list": ... }` (`filesystem.go:85-133`). Tool count saved: 2.

Nuance: picobot did NOT collapse memory similarly. `memory.go` keeps 5 separate tools (`list/read/edit/delete/write_memory`). Heuristic: collapse when verbs share schema (all need `path`); split when schemas diverge (`target`-only delete vs `target+old+new` edit). Applicability 4/5.

### openhuman — `ComposioTool` collapses 1000+ SaaS actions into 1
`D:/tmp/openhuman/src/openhuman/tools/impl/network/composio.rs:522-562`

```rust
fn parameters_schema(&self) -> serde_json::Value {
    json!({
        "type": "object",
        "properties": {
            "action": {"type": "string", "enum": ["list", "execute", "connect"],
                "description": "The operation: 'list' (list available actions), 'execute' (run an action), or 'connect' (get OAuth URL)"},
            "app":         {"type": "string", "description": "Toolkit slug filter for 'list'..."},
            "action_name": {"type": "string"},
            "tool_slug":   {"type": "string"},
            "params":      {"type": "object", "description": "Parameters to pass to the action"},
            ...
        },
        "required": ["action"]
    })
}
```

Most aggressive collapse: ONE tool fronts ~1000 OAuth integrations. Price: `params: object` escape hatch loses schema validation at the leaf. Mitigated by §1.4 expansion. Applicability 3/5 (tempting for Aura's MCP tools, but lost validation is expensive at scale).

### openhuman — dynamic typed-tool expansion on intent narrowing (the inverse pattern)
`D:/tmp/openhuman/src/openhuman/agent/harness/subagent_runner/ops.rs:526-540`

```rust
// When `integrations_agent` is spawned with a `toolkit` argument (e.g.
// `toolkit="gmail"`), build one [`ComposioActionTool`] per action
// in that toolkit and inject them into the sub-agent's tool list.
// Each carries the action's real JSON schema, so the LLM's native
// tool-calling path validates arguments before they hit the wire.
//
// Generic dispatchers (`composio_execute`, `composio_list_tools`)
// are stripped from the parent-filtered indices in this path so
// the model only sees one way to call each action.
```

Dual to kitchen-sink: when agent narrows intent (`toolkit="gmail"`), the collapsed tool EXPANDS into N typed tools, and the kitchen-sink dispatcher is STRIPPED so there's only one way to invoke. Applicability 5/5. For Aura: collapse `create_xlsx/docx/pdf` → `create_document(format)` in default mode; expand to typed tools when a files-narrowed sub-context is spawned.

### codex — `automation_update(mode=...)` for CRUD-on-resource
`D:/tmp/codex/codex-rs/core/src/tools/handlers/tool_search.rs:152-164` (test fixture)

```rust
let dynamic_tools = [DynamicToolSpec {
    namespace: Some("codex_app".to_string()),
    name: "automation_update".to_string(),
    description: "Create, update, view, or delete recurring automations.".to_string(),
    input_schema: serde_json::json!({
        "type": "object",
        "properties": {"mode": { "type": "string" }},
        "required": ["mode"],
        "additionalProperties": false,
    }),
    defer_loading: true,
}];
```

`mode` enum generalises CRUD-on-resource into one tool. Applicability 5/5 — Aura's `schedule_task / list_tasks / cancel_task` is a direct 3-into-1 candidate.

### cli-printing-press — NO action-enum collapse (one tool per endpoint)
Generator emits `{resource}_{action}` per HTTP method (`internal/discovery/naming.go:54-76`): `GET /tags/{id}` → `tags_get_tags`, `POST /tags` → `tags_create`, `DELETE /tags/{id}` → `tags_delete`. 30-endpoint API → ~30 tools. Argues verb-per-tool is what lets agents pick without trial-and-error. Trade-off paid in §6. Applicability 2/5 — exactly what Aura wants to stop doing.

---

## 2. Tool tiering — always-loaded vs deferred discoverable

### codex — 4-state `ToolExposure` enum (the killer pattern)
`D:/tmp/codex/codex-rs/tools/src/tool_executor.rs:7-33`

```rust
pub enum ToolExposure {
    /// Include this tool in the initial model-visible tool list.
    Direct,
    /// Register this tool for later discovery, but omit it from the initial
    /// model-visible tool list.
    Deferred,
    /// Include this tool in the initial model-visible tool list only.
    /// In code-mode-only sessions, this keeps the tool callable as a normal
    /// model tool while excluding it from the nested code-mode tool surface.
    DirectModelOnly,
    /// Keep this tool registered for dispatch without exposing it to the model.
    Hidden,
}
```

When a tool is deferred, the spec is rewritten before indexing (`tool_search_entry.rs:25-46`):

```rust
let output = match spec {
    ToolSpec::Function(mut tool) => {
        tool.defer_loading = Some(true);
        tool.output_schema = None;   // search results omit output schema; full spec only on hit
        LoadableToolSpec::Function(tool)
    }
    ToolSpec::Namespace(mut namespace) => { ... }
    ToolSpec::ToolSearch { .. } | ToolSpec::ImageGeneration { .. }
    | ToolSpec::WebSearch { .. } | ToolSpec::Freeform(_) => return None,
};
```

Boundary signal: each tool implements `search_info() -> Option<ToolSearchInfo>` (`registry.rs:46-50`); BM25 corpus = union of all `Some(_)` returns. Auto-rebuilds when registry rebuilds. Applicability 5/5. This is the Anthropic-2026-aligned reference.

### openhuman — 3 orthogonal axes (`ToolScope` × `permission_level` × `category`)
`D:/tmp/openhuman/src/openhuman/tools/traits.rs:8-71`

```rust
pub enum ToolScope { All, AgentOnly, CliRpcOnly }
pub enum PermissionLevel { None = 0, ReadOnly = 1, Write = 2, Execute = 3, Dangerous = 4 }
pub enum ToolCategory { System, Skill }
```

`ToolScope` filters at dispatch (CLI vs agent loop vs RPC). `permission_level` is channel-cap-driven. `category` is sub-agent scoping. **No Deferred equivalent — all visible tools are in the prompt.** Applicability 4/5; clean axes but missing Deferred is a real gap.

### picobot — NO tiering
All tools always visible. `isSystemChannel(channel)` (`loop.go:46-53`) only gates session state, not tool surface. Bearable because native tool count is naturally low (~14). Applicability 1/5 (naive baseline = Aura today).

### cli-printing-press — NO tiering (manifest-only)
All generated tools ship in `tools-manifest.json` + runtime `mcp_tools.go`. No runtime tiering. Applicability 2/5.

### Deferred-tier presence summary
- picobot: NO
- cli-printing-press: NO
- openhuman: NO (3-axis policy gating instead)
- codex: YES (`ToolExposure::Deferred` + `tool_search` handler + BM25 index)

**Codex is the only one of the four with true deferred loading per Anthropic Oct-2026 guidance.**

---

## 3. Capability gating — per-tool required permission

### openhuman — `permission_level` × per-channel cap
`D:/tmp/openhuman/src/openhuman/tools/traits.rs:154-159`

```rust
/// Permission level required to execute this tool.
/// Channels with a lower maximum permission level will reject this tool.
/// Default: `ReadOnly`. Override for write/execute/dangerous tools.
fn permission_level(&self) -> PermissionLevel {
    PermissionLevel::ReadOnly
}
```

Session build (`agent_tool_policy/engine.rs:39-91`):

```rust
for tool in tools {
    let required_permission = tool.permission_level();
    let explicitly_hidden =
        !visible_tool_names.is_empty() && !visible_tool_names.contains(&name);
    let exceeds_permission = required_permission > allowed_permission;
    let action = if explicitly_hidden { ToolPolicyAction::HideFromPrompt }
                 else if exceeds_permission { ToolPolicyAction::Deny }
                 else { ToolPolicyAction::Allow };
    ...
}
```

Migration-safety knob (`engine.rs:108-115`): empty config → `Dangerous` (unrestricted legacy mode). Once any channel configured, unknown channels default to ReadOnly.

Arg-aware override (`traits.rs:224-226`): tools whose effect depends on args (composio: `action=list` no-gate, `action=execute` gate) override `external_effect_with_args`. Applicability 5/5; maps directly to Aura's `internal/identity/`.

### codex — `PreToolUseHookResult` callback chain (no static enum)
`D:/tmp/codex/codex-rs/core/src/tools/registry.rs:136-163`

```rust
pub(crate) struct PreToolUsePayload {
    pub(crate) tool_name: HookToolName,
    pub(crate) tool_input: Value,
}
```

Every invocation routes through `run_pre_tool_use_hooks` / `run_post_tool_use_hooks`. Hooks can rewrite input or deny. More flexible (regex on `bash` args) but heavier per-call and harder to render as UI. Applicability 3/5.

### picobot — NONE
All tools always allowed. Sandbox enforced at tool implementation (`os.Root` for filesystem).

### cli-printing-press — `NoAuth/PublicCount/TotalCount` tagging
`internal/mcpdesc/compose.go:46-52` — annotation-only, no enforcement. Applicability 2/5.

---

## 4. MCP description override

### cli-printing-press — `mcp-descriptions.json` sidecar (unique pattern)
`D:/tmp/cli-printing-press/internal/mcpoverrides/overrides.go:1-56`

```go
// The override file is the sanctioned path for hand-authored MCP tool
// descriptions on typed endpoint tools: direct edits to internal/mcp/tools.go
// and tools-manifest.json are wiped on the next regen because both files carry
// the generator's DO-NOT-EDIT header.

const Filename = "mcp-descriptions.json"
type Overrides struct {
    Descriptions map[string]string `json:"descriptions"`
}

func Load(cliDir string) (Overrides, error) {
    path := filepath.Join(cliDir, Filename)
    data, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return Overrides{}, nil  // empty is steady state
        }
        ...
    }
}
```

`Apply` (`overrides.go:73-107`) mutates parsed spec in-place; returns unmatched keys so typos surface (no silent drop). Applicability 5/5. For Aura, this is the missing layer — MCP server author's description is often terrible ("Search HN") and Aura should be able to hand-rewrite per-tool descriptions without forking the MCP server.

### codex — namespace-level override only
`tool_search_spec.rs:24-47` exposes per-source descriptions in `tool_search` help text, dedup'd. But it's description-of-the-source, not per-tool. Applicability 3/5.

### picobot — concat-only
`internal/agent/tools/mcp.go:26-32`: `fmt.Sprintf("[MCP: %s] %s", t.serverName, desc)`. Description from MCP server verbatim. Applicability 1/5.

### openhuman — delegates to upstream Composio API
No local override. The kitchen-sink wrapper means only ~3 actions to describe. Applicability 2/5.

**Only cli-printing-press implements a sanctioned, file-based, regen-safe override sidecar.**

---

## 5. Tool result schema validation + truncation

### openhuman — typed `ToolResult` + per-tool `max_result_size_chars`
`D:/tmp/openhuman/src/openhuman/tools/traits.rs:241-243`

```rust
fn max_result_size_chars(&self) -> Option<usize> { None }
```

Override at `system/shell.rs:128-130`:

```rust
/// Cap shell output at ~30k chars before threading into history.
/// Verbose commands (`find /`, dependency installs, log dumps)
/// can otherwise blow past 100k chars in one call.
fn max_result_size_chars(&self) -> Option<usize> { Some(30_000) }
```

Plus `web_fetch.rs:86`. Harness truncates with marker BEFORE result enters history. `ToolResult` itself is typed (MCP content blocks + `is_error`), not string blob. Applicability 5/5.

### codex — centralized `truncation_policy` on session
`D:/tmp/codex/codex-rs/core/src/tools/handlers/mcp.rs:97-103`

```rust
Ok(boxed_tool_output(McpToolOutput {
    result: result.result,
    tool_input: result.tool_input,
    wall_time: started.elapsed(),
    original_image_detail_supported: can_request_original_image_detail(&turn.model_info),
    truncation_policy: turn.truncation_policy,
}))
```

Truncation centralized in renderer. Applicability 4/5. Per-tool (openhuman) is right when tools have wildly different "useful body" sizes; centralized (codex) is right with one knob.

### picobot — string blob, no truncation
`registry.go:65-91` Execute returns `(string, error)`. Chatty-tool risk real. Applicability 1/5.

### cli-printing-press — generated typed shapes per-endpoint
Each tool emits a typed struct matching OpenAPI response. Agent gets validated, named fields. No truncation. Applicability 3/5.

---

## 6. Tool retrieval-as-tool — `tool_search(query)` vs full manifest

### codex — BM25 over registry-snapshot, exposed as `tool_search` tool
`D:/tmp/codex/codex-rs/core/src/tools/handlers/tool_search.rs:23-54`

```rust
pub struct ToolSearchHandler {
    entries: Vec<ToolSearchEntry>,
    search_source_infos: Vec<ToolSearchSourceInfo>,
    search_engine: SearchEngine<usize>,
}

impl ToolSearchHandler {
    pub(crate) fn new(search_infos: Vec<ToolSearchInfo>) -> Self {
        ...
        let documents: Vec<Document<usize>> = entries
            .iter()
            .map(|entry| entry.search_text.clone())
            .enumerate()
            .map(|(idx, search_text)| Document::new(idx, search_text))
            .collect();
        let search_engine = SearchEngineBuilder::<usize>::with_documents(
            Language::English, documents).build();
        ...
    }
}
```

**BM25, NOT embeddings.** `search_text` is concatenated tool description + metadata. Indexed by `bm25` crate, queried with user's `query`. Re-built whenever registry rebuilds (boot, MCP reconnect).

Tool description (`tool_search_spec.rs:49-51`):
```rust
"# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nYou have access to tools from the following sources:\n{source_descriptions}\nSome of the tools may not have been provided to you upfront, and you should use this tool (`tool_search`) to search for the required tools. For MCP tool discovery, always use `tool_search` instead of `list_mcp_resources` or `list_mcp_resource_templates`."
```

Key design points:
- **BM25 over embeddings.** Cheap, deterministic, in-process, no embedding sidecar. Worth A/B vs Aura embed stack for corpora <500 entries.
- **Auto-rebuild on registry change.** No hardcoded RAG — the index IS the registry walked at boot.
- **Limit clamp.** `TOOL_SEARCH_DEFAULT_LIMIT` (~8 tools/page).
- **Result trim:** `defer_loading: Some(true)`, `output_schema: None` (ships without output schema; full spec only on actual call).

Applicability 5/5 — direct answer to user's "rag deve auto-aggiornare".

### openhuman, picobot, cli-printing-press — NONE
All use full static manifest (filtered, in openhuman's case, by policy). cli-printing-press's `tools-manifest.json` is documentation for override agents, not a runtime query tool. Applicability 1-2/5.

### Discovery scorecard
| System | Method | Auto-updates on registry change? |
|--------|--------|----------------------------------|
| picobot | Static prompt manifest | Yes (rebuilt at registry mutation) |
| openhuman | Static manifest + policy filter | Yes (per-session at start) |
| cli-printing-press | Static manifest (generated) | At regen, not runtime |
| codex | **BM25 index over `search_text`** | Yes, rebuilt on registry change |

**Only codex uses retrieval-style discovery. NONE use embeddings.** BM25 is the lowest-effort path to "auto-aggiornare". If embeddings preferred, Aura's existing cache trivially keys on `tool_name+description+schema` hash, rebuilt on the same trigger as Aura's wiki/Qdrant reconciler.

---

## 7. Per-channel tool config

### openhuman — `channel_permissions: HashMap<channel, max_perm>`
`D:/tmp/openhuman/src/openhuman/agent_tool_policy/engine.rs:104-148`

```rust
fn permission_for_channel(channel_permissions: &HashMap<String, String>,
                          channel: &str) -> PermissionLevel {
    if channel_permissions.is_empty() {
        return PermissionLevel::Dangerous;  // legacy unrestricted
    }
    match channel_permissions.get(channel) {
        Some(raw) => match parse_permission_level(raw) {
            Some(permission) => permission,
            None => PermissionLevel::ReadOnly,
        },
        None => PermissionLevel::ReadOnly,  // unknown channel = readonly
    }
}
```

Web can be `write`; Telegram can be `execute`; cron can be `read_only`. Cleanest reference of the four. Three migration-safety properties: (a) channels are first-class strings on session, (b) empty config = legacy unrestricted, (c) unknown channels fail-closed to ReadOnly. Applicability 5/5.

### picobot — per-channel session state, NOT tool set
`internal/agent/loop.go:46-53`

```go
func isSystemChannel(channel string) bool {
    switch channel {
    case "heartbeat", "cron": return true
    default: return false
    }
}
```

Only gates session loading (heartbeat/cron skip session). Tool surface identical. Applicability 2/5 (seed signal for the policy layer, not substitute).

### codex — exposure is global, not per-channel
Per-session narrowing happens at sub-agent spawn (similar pattern to openhuman §1.4). Applicability 3/5.

### cli-printing-press — N/A (generator)

---

## 8. Tool versioning / migration

**None of the four address this.** All are single-binary deployments — when binary restarts with a new tool schema, the LLM context also resets, so in-flight contexts with the old shape don't exist for more than one running session.

Closest thing: codex's `tool_dispatch_trace.rs` logs every dispatch with spec hash for post-mortems. Applicability 1/5 — not a problem at these timescales.

---

## Cross-cutting observations

### A. Tool count comparison
- **picobot**: 14 native + N MCP. Filesystem collapses 3→1; memory split as 5.
- **codex code-mode**: 2 public tools (`exec`, `wait`) + N nested via JavaScript. `code-mode/src/lib.rs:36-38`:
  ```rust
  pub const PUBLIC_TOOL_NAME: &str = "exec";
  pub const WAIT_TOOL_NAME:   &str = "wait";
  ```
  Model writes JS that calls tools (`await tools.gmail_search({...})`) instead of issuing tool calls directly.
- **openhuman**: ~30-50 native + Composio kitchen-sink (1 = 1000+) + N MCP.
- **cli-printing-press**: 1 per HTTP endpoint. Opposite strategy: expand + provide discovery.

Two valid strategies emerge:
1. **Collapse upfront** (picobot, openhuman/composio): kitchen-sink action-enum, lose schema validation at the leaf.
2. **Expand + deferred discovery** (codex): keep typed schemas, hide behind a search tool.

Codex's pattern is better fit for Aura because (a) Aura has the embedding/RAG infra to power discovery, (b) user's complaint is about discovery, not tool count per se, (c) preserves per-tool schema validation.

### B. Description composition discipline (cli-printing-press)
`D:/tmp/cli-printing-press/internal/mcpdesc/compose.go:69-96`

```go
func Compose(in Input) string {
    desc := in.Endpoint.Description
    var composed string
    if hasStructuralOverride(desc) {
        composed = composeAction(desc)
    } else {
        var parts []string
        if action := composeAction(desc); action != "" { parts = append(parts, action) }
        if required := composeRequired(in.Endpoint); required != "" { parts = append(parts, required) }
        if optional := composeOptional(in.Endpoint); optional != "" { parts = append(parts, optional) }
        if !mentionsReturn(desc) {
            if returns := composeReturns(in.Endpoint); returns != "" { parts = append(parts, returns) }
        }
        composed = strings.Join(parts, " ")
    }
    composed = appendMethodMarker(composed, in.Endpoint.Method)
    return naming.MCPDescription(composed, in.NoAuth, in.AuthType, in.PublicCount, in.TotalCount)
}
```

Format: `<verb-led action>. Required: a, b. Optional: c, d (plus N more). Returns the X. Destructive.` Optional list capped at 3 (`optionalListMax = 3`). This discipline is what lets BM25 work well — descriptions are normalized prose, never freeform. Applicability 4/5; liftable independent of generator.

### C. Auto-update vs hardcoded — the user's complaint
User's pain is addressed by ONE pattern: **codex's BM25-over-registry-snapshot**. Every other system either:
- Ships full manifest in the prompt and doesn't need updating (openhuman, picobot)
- Generates the manifest at build time, not runtime (cli-printing-press)

Lift unambiguous: codex pattern, BM25 (cheaper) OR Aura's existing embedding stack (richer semantic match). Rebuild index whenever registry rebuilds (same trigger Aura already uses for wiki/Qdrant reconciler).

---

## Top patterns ranked by Aura applicability

| Pattern | Source | Rating | Effort | ROI |
|--------|-------|--------|-------|-----|
| Deferred tier + BM25 retrieval | codex `tool_search.rs:23-54`, `tool_search_entry.rs:7-56` | 5/5 | Medium | Very High |
| Permission gating with channel caps | openhuman `engine.rs:39-148`, `traits.rs:154-159` | 5/5 | Medium | High |
| Dynamic typed-tool expansion on intent narrowing | openhuman `ops.rs:526-540` | 5/5 | Medium | High |
| MCP description override sidecar | cli-printing-press `overrides.go:1-107` | 5/5 | Low | High |
| Per-tool `max_result_size_chars` | openhuman `traits.rs:241`, `shell.rs:128` | 5/5 | Low | Medium |
| Action-enum collapse for CRUD-on-resource | codex `automation_update`, picobot `filesystem.go:40-60` | 4/5 | Low | Medium |
| Description composition discipline | cli-printing-press `compose.go:69-96` | 4/5 | Low | Medium |
| `ToolExposure::Hidden` for internal tools | codex `tool_executor.rs:25-27` | 4/5 | Low | Low |
| Hook-based pre/post mutation | codex `registry.rs:9-12, 136-163` | 3/5 | High | Medium |
| Code-mode (JS isolate hosting nested tools) | codex `code-mode/lib.rs:36-38` | 2/5 | Very High | Speculative |

---

## Pattern interactions

- **Deferred + Description-override**: deferred tools rely on BM25 hits; bad descriptions waste deferred capacity. cli-printing-press's sidecar feeds codex's index. Composable.
- **Permission-gating + Channel-config**: openhuman's per-channel cap is the natural place for "Telegram = ReadOnly, web admin = Dangerous." Composable with deferred tiering: gating decides WHICH tools enter the index per session.
- **Dynamic typed expansion + Action-enum collapse**: composio is "collapsed at default scope, expanded when intent narrows." For Aura: collapse `create_xlsx/docx/pdf` → `create_document(format)`; expand to typed when `files_agent` spawned.

---

## What NOT to adopt

- **picobot's "all tools always visible"** — already Aura's state, biting it now.
- **cli-printing-press's "one tool per endpoint"** — Aura's MCP tools already do this and user just complained.
- **codex's hook-rewrite for every call** — over-engineered for Aura's threat model.
- **codex code-mode** — requires JS sandbox in Aura; not justified by marginal token win at current scale.

End of catalogue.
