# Tool Lifecycle Patterns Across 4 Production Agents

Survey for Aura: how does each of picobot, openhuman, nanobot, hermes-agent handle
dynamic tool lifecycle — boot, MCP reconnect, runtime registration, removal,
config reload, embedding-dim change?

Sources (snapshot under D:/tmp): picobot (Go), nanobot (Python), hermes-agent
(Python), openhuman (Rust). All read-only, all from public repos.

---

## §1 Per-system lifecycle answers

### 1.1 picobot (Go)

| # | Question | Answer (file:line) | Notes |
|---|---|---|---|
| 1 | Boot-time registration order | `cmd/picobot/main.go:163-192` builds Hub → Provider → AgentLoop → `NewAgentLoop()` does the registration. Inside `internal/agent/loop.go:80-142` order is: built-ins (message, fs, exec, web, web_search, spawn, cron) → memory tools → skill tools → MCP. | Strictly sequential, single-threaded, MCP last. |
| 2 | MCP reconnect | NONE. `internal/mcp/client.go:35-50` connects once via `NewStdioClient` / `NewHTTPClient`, fails → server skipped for the life of the process. No reconnect, no backoff. | Restart = only recovery path. |
| 3 | Hot-reload signal | NONE. Config is read once at process start (`config.LoadConfig()` in `cmd/picobot/main.go:165`). No watcher, no SIGHUP handler. | |
| 4 | Index update trigger | N/A — no tool index. `internal/agent/tools/registry.go:25-62` is an in-memory `map[string]Tool` with a `Definitions()` getter the LLM consumes as a flat array. | The LLM sees the full ~25-tool list every turn. |
| 5 | Drift detection | NONE. No schema versioning, no embedding metadata, no health probe. A dead MCP server's tools stay in `Definitions()` until tool invocation fails (`MCPTool.Execute → CallTool` returns error). | |
| 6 | Failure tolerance | YES at boot only. `internal/agent/loop.go:133-135` logs and continues on per-server connect failure. Other servers + built-ins keep working. | No tool-discovery is independent of any embedding model — there's no embedder. |
| 7 | Registry persistence | In-memory only. `Registry.tools` is a plain map; rebuilt every boot. | |
| 8 | Operator API | NONE. Restart only. No `/mcp refresh`, no HTTP endpoint, no CLI subcommand. | |

Key snippet (picobot's MCP-at-boot in `internal/agent/loop.go:119-142`):

```go
// Connect to configured MCP servers and register their tools.
var mcpClients []*mcp.Client
for name, cfg := range mcpServers {
    var client *mcp.Client
    var err error
    switch {
    case cfg.Command != "":
        client, err = mcp.NewStdioClient(name, cfg.Command, cfg.Args)
    case cfg.URL != "":
        client, err = mcp.NewHTTPClient(name, cfg.URL, cfg.Headers)
    ...
    }
    if err != nil {
        log.Printf("MCP server %q: failed to connect: %v", name, err)
        continue
    }
    mcpClients = append(mcpClients, client)
    for _, tool := range client.Tools() {
        reg.Register(tools.NewMCPTool(client, name, tool))
    }
}
```

Translatability to Aura: **5/5** for the philosophy ("MCP register at boot is
fine for a single-user system"). But picobot has zero answers for any of Q2-Q8;
it's a baseline, not a model to lift from for the open gaps.

---

### 1.2 nanobot (Python)

| # | Question | Answer (file:line) | Notes |
|---|---|---|---|
| 1 | Boot order | `nanobot/agent/loop.py:450-476` registers built-ins via `ToolLoader.load(ctx, self.tools)` then optionally `MyTool`. MCP is LAZY: `_connect_mcp()` at line 478-498 runs on first user message, NOT at boot. | Lazy connect = fast cold start but first turn pays the connect latency. |
| 2 | MCP reconnect | On every message until success. `loop.py:480-498`: if `not self._mcp_connected and not self._mcp_connecting`, retry. Failure logs "will retry next message". No exponential backoff, just per-message gate. | |
| 3 | Hot-reload signal | NONE. mcp.json read once via `nanobot/config/loader.py`. No fsnotify, no SIGHUP. | |
| 4 | Index update trigger | N/A — no tool index. `nanobot/agent/tools/registry.py:48-71` returns sorted (`builtins`, `mcp_tools`) lists; cache invalidates only on `register/unregister` mutations. | |
| 5 | Drift detection | NONE. No schema check, no embedding dim awareness. Transport-level retry on tool call (`mcp.py:198-251` retries once on `ClosedResourceError`/`BrokenPipeError`/etc.) but the registry itself never re-syncs. | |
| 6 | Failure tolerance | YES, per-server isolated. `mcp.py:653-664` `connect_mcp_servers` iterates servers in sequence, each in its own `AsyncExitStack`. One server's `await session.initialize()` failure logs + continues to next (`mcp.py:631-651`). HTTP servers TCP-probed first (`_probe_http_url`) to avoid SDK cancel-scope explosions. | No embedding, so no embedding-down failure mode. |
| 7 | Registry persistence | In-memory only. `ToolRegistry._tools` is a dict, rebuilt each process. | |
| 8 | Operator API | NONE. Restart only. The only manual hook is `unregister(name)` in code (`registry.py:24-27`) — not exposed via HTTP/CLI. | |

Key snippet (nanobot's lazy + retry-next-message at `agent/loop.py:478-498`):

```python
async def _connect_mcp(self) -> None:
    """Connect to configured MCP servers (one-time, lazy)."""
    if self._mcp_connected or self._mcp_connecting or not self._mcp_servers:
        return
    self._mcp_connecting = True
    try:
        self._mcp_stacks = await connect_mcp_servers(self._mcp_servers, self.tools)
        if self._mcp_stacks:
            self._mcp_connected = True
        else:
            logger.warning("No MCP servers connected successfully (will retry next message)")
    except asyncio.CancelledError:
        logger.warning("MCP connection cancelled (will retry next message)")
        self._mcp_stacks.clear()
    except BaseException as e:
        logger.warning("Failed to connect MCP servers (will retry next message): {}", e)
        self._mcp_stacks.clear()
    finally:
        self._mcp_connecting = False
```

Translatability to Aura: **3/5**. The lazy+retry pattern is nice, but Aura
already does eager connect-at-boot, and a single-user Telegram bot doesn't
benefit much from cold-start optimization. The HTTP TCP-probe + sanitization
(`_normalize_schema_for_openai`, `_sanitize_name`) is small but reusable.

---

### 1.3 hermes-agent (Python) — RICHEST LIFECYCLE

Single 3500-line `tools/mcp_tool.py` contains the entire production MCP client
runtime. Hermes has the most complete answers across all 8 questions.

| # | Question | Answer (file:line) | Notes |
|---|---|---|---|
| 1 | Boot order | `discover_mcp_tools()` at `mcp_tool.py:3284` is called after built-in `discover_builtin_tools()`. It calls `register_mcp_servers(servers)` at `3189` which spawns one long-lived `MCPServerTask` per server **in parallel** via `asyncio.gather` (line 3237). | Parallel boot. Dedicated background asyncio loop in a daemon thread (`_ensure_mcp_loop` line 2142). |
| 2 | MCP reconnect | YES, full state machine. `MCPServerTask.run()` at `mcp_tool.py:1504-1660` runs forever; on transport exception: exponential backoff (1 → max), up to `_MAX_RECONNECT_RETRIES`. Distinguishes initial-connect retries from runtime reconnects. OAuth 401 path triggers `_reconnect_event` for cred refresh without burning backoff. Keepalive `list_tools` every 180s (`_wait_for_lifecycle_event` line 1193-1253) detects dead TCP idle. | The reference implementation. |
| 3 | Hot-reload signal | NONE for mcp.json file (config read at boot via `_load_mcp_config()` line 2237). BUT `register_mcp_servers(servers)` at `3189` is **idempotent** — re-callable with the full config; servers already in `_servers` are skipped, new ones are connected. So an operator CAN reload by calling this with a fresh config dict. | The mechanism exists; just no file watcher trigger. |
| 4 | Index update trigger | N/A — Hermes has no vector tool index. The "index" is the flat `tools/registry.py` registry that gets updated **on every `notifications/tools/list_changed` server push** (see Q5). | |
| 5 | Drift detection | YES — server-pushed `tools/list_changed`. `_make_message_handler` at `mcp_tool.py:1088-1129` receives MCP `ServerNotification.ToolListChangedNotification` and schedules `_refresh_tools_task()`. `_refresh_tools()` at `1131-1191` diffs old vs new tool names, deregisters stale, registers new, logs `added: X; removed: Y`. Circuit breaker (`_server_error_counts`, threshold 3, cooldown 60s, line 1728-1755) handles repeated transport failures. | Aura has none of this. |
| 6 | Failure tolerance | YES, per-server. Failed `MCPServerTask` doesn't poison others — each runs in its own asyncio Task, swallows exceptions in `run()`. Circuit breaker stops the model from burning 90 tool-call iterations on a dead server (`_CIRCUIT_BREAKER_THRESHOLD = 3`, line 1730). | |
| 7 | Registry persistence | In-memory only — `tools/registry.py` dict. But the server set is tracked in `_servers: Dict[str, MCPServerTask]` (module-global, `mcp_tool.py:1709`), which lets re-discovery be idempotent. | |
| 8 | Operator API | YES. `register_mcp_servers(servers)` (3189) is idempotent and re-callable. Slash command `/mcp` documented (`website/docs/.../mcp.md`). `get_mcp_status()` (3351) returns per-server status for banner display. `shutdown_mcp_servers()` (3457) does graceful parallel shutdown via `asyncio.gather` with timeout + orphan-PID reaper (`_kill_orphaned_mcp_children` 3503). | Most polished surface of the 4. |

Key snippet (hermes dynamic refresh on push, `tools/mcp_tool.py:1131-1191`):

```python
async def _refresh_tools(self):
    """Re-fetch tools from the server and update the registry.

    Called when the server sends ``notifications/tools/list_changed``.
    """
    from tools.registry import registry
    async with self._refresh_lock:
        old_tool_names = set(self._registered_tool_names)
        async with self._rpc_lock:
            tools_result = await self.session.list_tools()
        new_mcp_tools = tools_result.tools if hasattr(tools_result, "tools") else []
        stale_tool_names = old_tool_names - {
            f"mcp_{sanitize_mcp_name_component(self.name)}_"
            f"{sanitize_mcp_name_component(tool.name)}"
            for tool in new_mcp_tools
        }
        for tool_name in stale_tool_names:
            registry.deregister(tool_name)
            _forget_mcp_tool_server(tool_name)
        self._tools = new_mcp_tools
        self._registered_tool_names = _register_server_tools(self.name, self, self._config)
        added = set(self._registered_tool_names) - old_tool_names
        removed = old_tool_names - set(self._registered_tool_names)
        if added or removed:
            logger.warning("MCP server '%s': tools changed dynamically — %s", self.name, ...)
```

Key snippet (circuit breaker, `mcp_tool.py:1728-1755`):

```python
_server_error_counts: Dict[str, int] = {}
_server_breaker_opened_at: Dict[str, float] = {}
_CIRCUIT_BREAKER_THRESHOLD = 3
_CIRCUIT_BREAKER_COOLDOWN_SEC = 60.0

def _bump_server_error(server_name: str) -> None:
    n = _server_error_counts.get(server_name, 0) + 1
    _server_error_counts[server_name] = n
    if n >= _CIRCUIT_BREAKER_THRESHOLD:
        _server_breaker_opened_at[server_name] = time.monotonic()
```

Translatability to Aura: **4/5** for the dynamic-refresh + reconnect patterns;
**5/5** for the circuit breaker concept. Python→Go port effort is meaningful
(~300 LOC) but the structure maps cleanly to Aura's existing
`internal/mcp/Client` + a new `mcp.Supervisor`.

---

### 1.4 openhuman (Rust)

| # | Question | Answer (file:line) | Notes |
|---|---|---|---|
| 1 | Boot order | Two-track tool surface: (a) static native controllers compile-time-registered via `core/all.rs` macros, surfaced through `openhuman/tool_registry/ops.rs:registry_entries()`; (b) MCP client servers stored in SQLite (`openhuman/mcp_clients/store.rs`) and connected on demand via `connect()` at `openhuman/mcp_clients/connections.rs:31-68`. No "boot connect all" — operator-driven. | User-controlled connection model. |
| 2 | MCP reconnect | NONE. `connections.rs` exposes `connect`/`disconnect`/`client_for`. If the spawned stdio process dies, the client is still in the registry but tool calls fail. No automatic reconnect, no backoff. | Operator clicks "Reconnect" in UI. |
| 3 | Hot-reload signal | YES, but it's NOT file-watch. Servers live in SQLite (`installed_servers` table) + `mcp_clients_install` RPC. Operator installs → row added → next connect picks it up. The legacy `mcp_client/registry.rs:66-101` (`McpServerRegistry::from_config`) is config-driven but only used for the legacy "gitbooks" path. | DB-as-config, not file-as-config. |
| 4 | Index update trigger | The "index" is computed lazily at request time. `tool_registry/ops.rs:registry_entries()` (52-113) walks: static MCP server tools + controller schemas + `connections::all_connected_tools().await`. No cached index, no embedding. | Walking the live registry each RPC call is cheap because the registry is small (~50 tools). |
| 5 | Drift detection | Partial. `tools_snapshot()` at `mcp_clients/client/mod.rs:101-103` returns the cached tool list captured during initial `list_tools` handshake. If the upstream server's tools change, openhuman won't notice. No `tools/list_changed` handler. | |
| 6 | Failure tolerance | YES. Each server lives in `Arc<McpStdioClient>` in a `RwLock<HashMap>`. One server failing doesn't affect others. Static controllers are always-on (compile-time). | No embedding to worry about. |
| 7 | Registry persistence | YES — partial. Installed MCP servers persist in SQLite (`store::list_servers`). The live connection state (`CONNECTIONS` `OnceLock<RwLock<HashMap>>` at `connections.rs:21-25`) is in-memory but rebuildable: `for server in store::list_servers { connect(server) }`. | Static tools are compile-time so trivially "persistent". |
| 8 | Operator API | YES. JSON-RPC methods `openhuman.mcp_clients_connect` / `_disconnect` / `_list` / `_install` / `_uninstall` / `_tool_call` defined in `mcp_clients/ops.rs`. Full UI panel for connection management. | Most operator-friendly. |

Key snippet (openhuman's live-walk registry at `tool_registry/ops.rs:52-113`):

```rust
pub fn registry_entries() -> Vec<ToolRegistryEntry> {
    let mut entries = BTreeMap::new();

    for spec in crate::openhuman::mcp_server::tool_specs() {
        let entry = mcp_tool_entry(spec);
        insert_registry_entry(&mut entries, entry, "mcp_stdio");
    }

    for schema in crate::openhuman::tools::all_tools_controller_schemas() {
        let entry = controller_tool_entry(&schema);
        insert_registry_entry(&mut entries, entry, "controller");
    }

    let client_tools = {
        use crate::openhuman::mcp_clients::connections;
        match tokio::runtime::Handle::try_current() {
            Ok(handle) if handle.runtime_flavor() == MultiThread =>
                tokio::task::block_in_place(|| handle.block_on(connections::all_connected_tools())),
            _ => Vec::new(),
        }
    };
    ...
}
```

Translatability to Aura: **4/5** for the operator API design (DB-backed installed
servers + connect/disconnect RPCs); **2/5** for the live-walk-at-request-time
since Aura's tool count + LLM cache friendliness wants a cached `Definitions()`
list, not per-turn walks.

---

## §2 Aura's current state and gap analysis

### Aura current lifecycle (from `cmd/aura/app.go:507-543` and `cmd/aura/app_wire.go:60-141`)

| # | Question | Aura's answer |
|---|---|---|
| 1 | Boot order | `cmd/aura/app.go` `New()`: registers 22 native tools via `internal/agent/tools/registry` → loads `mcp.json` via `mcp.LoadServers(cfg.MCPServersPath)` at line 508 → connects each server SEQUENTIALLY at lines 513-542 (NOT parallel) → registers MCP-wrapped tools → in `app_wire.go:78-103` constructs `toolindex.Reconciler`, runs `Reconcile(ReasonBoot)` synchronously, starts `Run` goroutine. |
| 2 | MCP reconnect | NONE. `internal/mcp/client.go` connect-once-or-fail. A dead stdio MCP child = orphan tools in the registry until restart. |
| 3 | Hot-reload signal | PARTIAL. `internal/mcp/watcher.go` does fsnotify on `mcp.json` with 500ms debounce → fires `Reconciler.Notify(ReasonMCPConfig)`. BUT (commented in watcher.go:9-14) the watcher only triggers a re-INDEX, not a re-CONNECT. New MCP servers in mcp.json are NOT picked up until the process restarts. Open Wave 2.10.c. |
| 4 | Index update trigger | YES. Reconciler at `internal/agent/tools/index/reconciler.go:174-218` runs on: boot (sync), debounce-merged notify, periodic 10-min ticker. Manual via POST `/api/tools/reindex` (`internal/api/tools_reindex.go`). |
| 5 | Drift detection | PARTIAL. Reconciler detects: state/qdrant point-count mismatch → wipes state (lines 274-290). Hash-based content drift per tool. BUT it does NOT detect embedding-dimension change between the live `EmbeddingOutputDim` and the collection's actual dim — silently mismatches, every 10 min the periodic reconcile logs `embed %s: ...` errors. |
| 6 | Failure tolerance | YES at boot for MCP (per-server skip on connect fail, app.go:524). YES for Qdrant down (reconciler logs and returns Report with errors). NO for embedding model down — every reconcile pass fails noisily; agent can't add new tools but existing index keeps serving. |
| 7 | Registry persistence | Hybrid. In-memory `internal/agent/tools/registry.Registry` rebuilt at boot. `tool_search_state` SQLite table persists which tools were embedded (`internal/agent/tools/index/state.go`) — used for diff. Qdrant collection persists the vectors. |
| 8 | Operator API | PARTIAL. POST `/api/tools/reindex` exists. NO endpoint for "reconnect MCP server", NO "drop and rebuild collection", NO "switch embedding model and re-embed all", NO "list MCP servers with health". |

### Biggest gaps

1. **No MCP reconnect at all.** A flaky stdio child or HTTP server is a process-restart event. Hermes-grade `MCPServerTask` with exponential backoff would close this. Severity: HIGH (impacts every long-running deploy).
2. **No `tools/list_changed` consumer.** If an upstream MCP server adds a tool (e.g. user installs a new GitHub MCP plugin), Aura keeps the stale set until restart. Picking this up from Hermes is mechanical — Aura's `internal/mcp/client.go` already speaks JSON-RPC, just needs an inbound-notification path. Severity: MEDIUM.
3. **Embedding-dim drift is silent.** When `EmbeddingOutputDim` changes (256 → 768 swap), the reconciler tries to upsert vectors of the new dim into a 256-d collection and Qdrant rejects them — every 10 min, forever. No alert, no automatic collection-drop-and-recreate. Severity: HIGH (this is the reported bug).
4. **MCP-watcher only re-indexes, doesn't re-CONNECT.** Boot-time `mcp.LoadServers` + per-server `NewStdioClient` are not idempotent and not callable after boot. Wave 2.10.c was deferred for this reason. Severity: MEDIUM.
5. **No operator surface for "reconnect server X" / "drop tool index collection" / "show MCP health".** Severity: LOW-MEDIUM (debug pain, not user-facing).

---

## §3 Top 5 lift candidates

Ranked by ROI (operational pain reduced × ease of port). All 5 land in
`internal/mcp/` or `internal/agent/tools/index/`.

### LIFT-1: Embedding-dim drift detection + auto-recreate (FIXES THE BUG)

**Source:** New work. Inspired by Aura's existing state/qdrant drift recovery
(`reconciler.go:274-290`) and Hermes circuit-breaker pattern
(`mcp_tool.py:1728-1755`).

**Idea:** At Reconcile start, call `Qdrant.CollectionInfo` and compare
`info.VectorSize` against `r.cfg.VectorDim`. If mismatch: log loud + recreate
the collection + wipe SQLite state + treat all tools as new.

**File to add to:** `internal/agent/tools/index/reconciler.go` around line 344
(`info, err := r.cfg.Qdrant.CollectionInfo(ctx, ...)`). Already fetching the
collection info — just doesn't check the dim field.

```go
// Add after existing drift recovery, before upsert preflight:
if info.Status != "" && info.VectorSize != 0 && info.VectorSize != uint64(r.cfg.VectorDim) {
    r.cfg.Logger.Warn("toolindex: embedding-dim drift",
        "collection_dim", info.VectorSize, "live_dim", r.cfg.VectorDim,
        "action", "drop-and-recreate")
    if err := r.cfg.Qdrant.DeleteCollection(ctx, r.cfg.Collection); err == nil {
        // wipe state, force every tool to upsert path
        for name := range indexed { _ = r.cfg.State.Remove(ctx, name) }
        indexed = map[string]StateRow{}
    }
}
```

Pre-req: `qdrant.Client` needs a `DeleteCollection` method + `CollectionInfo`
needs to return the vector-size field (check `internal/storage/qdrant`). Effort:
~50 LOC + 1 test. Translatability: **5/5**.

### LIFT-2: MCP supervisor with exponential-backoff reconnect

**Source:** `D:/tmp/hermes-agent/tools/mcp_tool.py:1504-1660` (`MCPServerTask.run`).

**Idea:** Each MCP server becomes a long-lived goroutine, not a one-shot
`NewStdioClient` call. On transport close → exponential backoff (1s → 2s → 4s
→ ... cap 60s) → reconnect → `list_tools` → diff against current registry → swap.

**File:** new `internal/mcp/supervisor.go` wrapping the existing `Client` per
server. Boot wiring at `cmd/aura/app.go:512-543` changes to: create supervisor per
server, register tools on first-ready signal, supervisor stays alive forever.

**Pattern from Hermes (`mcp_tool.py:1638-1654`):**

```python
retries += 1
if retries > _MAX_RECONNECT_RETRIES:
    logger.warning("MCP server '%s' failed after %d reconnection attempts, giving up: %s", ...)
    return
logger.warning("MCP server '%s' connection lost (attempt %d/%d), reconnecting in %.0fs: %s", ...)
await asyncio.sleep(backoff)
backoff = min(backoff * 2, _MAX_BACKOFF_SECONDS)
```

Effort: ~250 LOC + tests. Touches `internal/mcp/client.go` (must split connect
from process lifecycle), `internal/telegram/deps.go` (consumers must accept a
supervisor not a static slice), `cmd/aura/app.go`. Translatability: **4/5** —
go routines + channels map well, but the `MCPClients []ConnectedClient` Deps
shape becomes a `Supervisor` interface and that ripples through 4-5 callers.

### LIFT-3: `tools/list_changed` consumer + dynamic tool diff

**Source:** `D:/tmp/hermes-agent/tools/mcp_tool.py:1070-1191` (`_make_message_handler` + `_refresh_tools`).

**Idea:** MCP servers can push `notifications/tools/list_changed`. Aura's
current `internal/mcp/client.go` reads response-shaped JSON-RPC frames (`probe.ID
!= nil`) and DROPS notifications (`stdioTransport.roundTrip` skip block in
picobot too). Wire a notification channel; on `tools/list_changed` re-fetch +
diff + register-add / deregister-remove + `Reconciler.Notify(ReasonMCPConfig)`.

**Bonus:** the diff log line from Hermes ("MCP server 'X': tools changed
dynamically — added: foo, bar; removed: baz") is exactly the kind of operator
visibility Aura wants.

Effort: ~120 LOC. Depends on LIFT-2 (supervisor owns the channel). Translatability: **4/5**.

### LIFT-4: Circuit breaker on MCP server failure (stop the iteration burn)

**Source:** `D:/tmp/hermes-agent/tools/mcp_tool.py:1711-1755` + Hermes issue #10447 ("90-iteration burn loop").

**Idea:** Per-MCP-server consecutive-error counter. After 3 consecutive failures
inside `MCPTool.Execute`, return a deterministic error string ("server X is
unreachable, do not retry") + open the breaker for 60s. Probe on cooldown
elapse, half-open → close on success.

**File:** `internal/agent/tools/registry/mcp.go` (the `MCPTool` wrapper). Wrap
`Execute` with breaker state held in a `sync.Map[string]*serverBreakerState`.

```go
// rough port
type breakerState struct {
    failures atomic.Int32
    openedAt atomic.Int64 // unix nanos
}
const breakerThreshold = 3
const breakerCooldown = 60 * time.Second
```

Effort: ~120 LOC + tests. Independent of LIFT-2/3 — can ship first. Translatability: **5/5**.

### LIFT-5: Idempotent re-registration API (`POST /api/mcp/refresh`)

**Source:** `D:/tmp/hermes-agent/tools/mcp_tool.py:3189-3281` (`register_mcp_servers`).

**Idea:** Make `mcp.LoadServers` + connect loop callable post-boot. Already-
connected servers are skipped (idempotent). New servers in mcp.json get
connected. Pair with the existing `internal/mcp/watcher.go` so the watcher
triggers a full refresh, not just a reindex.

The Hermes idempotency contract (`mcp_tool.py:3209-3225`):

```python
with _lock:
    new_servers = {
        k: v
        for k, v in servers.items()
        if k not in _servers and _parse_boolish(v.get("enabled", True), default=True)
    }
if not new_servers:
    return _existing_tool_names()
```

**File:** new `internal/mcp/refresh.go` + `internal/api/mcp_refresh.go`. Effort:
~200 LOC. Depends on LIFT-2 for graceful server removal. Translatability: **4/5**.

---

## §4 Honest gaps and caveats

- **None of the 4 systems has a vector index for tools.** Hermes, nanobot,
  picobot, openhuman all use flat list-of-definitions for the LLM. Aura's
  `aura_tool_search_v2` Qdrant collection is unique — there's no reference
  implementation for "tool index reconciler" to lift from. The patterns
  imported from Hermes (drift detection, supervisor, breaker) operate on the
  flat-registry layer; Aura keeps its tool-index layer and the lifts feed it
  via `Notify`.
- **mcp.json file-watching is unique to Aura.** Hermes reads config once.
  Openhuman uses DB-as-config. Nanobot reads once. Picobot reads once. So
  LIFT-5 (idempotent re-registration) has no production precedent; we'd be
  building on Hermes's idempotency contract without a battle-tested operator
  surface to copy from.
- **No system handles embedding-dim change because no system has an embedding
  layer over tools.** LIFT-1 is novel work, but the analogy is solid: Aura
  already has equivalent drift recovery for state/Qdrant points (lines 274-290
  of `reconciler.go`), just extend the pattern to vector_size.
- **Hermes is Python.** Porting 300 LOC of asyncio Task lifecycle into Go
  goroutines is mostly mechanical, but the `_reconnect_event` + `_shutdown_event`
  dual-channel pattern needs to map to a `context.Context` + `chan struct{}`
  combo. The `Anyio` cancel-scope warnings in Hermes comments don't apply.
- **Openhuman's "DB-as-config" for MCP servers** is the most operator-friendly
  pattern of the four but would be a bigger rewrite for Aura than is justified
  by the current ~5 MCP servers. Park for the post-Phase 8 MCP-UI work
  (referenced in memory note `project_mcp_productivity_roundup_milestone.md`).

---

## §5 Recommended sequencing

1. **LIFT-1 (embedding-dim drift)** — fixes the reported 10-min error bug.
   Smallest change, ships in one Ralph story (~80 LOC + 1 test). Land first.
2. **LIFT-4 (circuit breaker)** — independent, stops bad MCP from poisoning
   tool-call loops. ~120 LOC. Land second.
3. **LIFT-2 (supervisor with reconnect)** — biggest impact, biggest surface
   change. ~250 LOC + Deps refactor. Touches 4-5 packages. Schedule as a
   dedicated phase, not a single Ralph story.
4. **LIFT-3 (`tools/list_changed`)** — depends on supervisor (it owns the
   notification channel). Bundle with LIFT-2 or ship as a follow-up.
5. **LIFT-5 (`/api/mcp/refresh`)** — depends on LIFT-2. Wire the existing
   `internal/mcp/watcher.go` callback to the new refresh endpoint instead of
   just the reconciler.

This sequencing aligns with the user's per-module deep-refactor rule: each
lift touches a narrow set of files, has its own tests, and leaves
`internal/mcp/client.go` cleaner than it found it.
