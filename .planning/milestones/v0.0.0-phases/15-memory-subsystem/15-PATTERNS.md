# Phase 15: Memory Subsystem - Pattern Map

**Mapped:** 2026-06-11
**Files analyzed:** 8 (1 new Go cmd, 1 catalog edit, 1 new default-on seam, 1 new Dockerfile, 1 compose edit, 1 doc amendment, 1 audit-run, test tier)
**Analogs found:** 8 / 8 (every owned-surface file has an exact in-repo analog; RESEARCH pre-located the wiring chain)

> **Framing reminder for the planner:** Phase 15 is an *adoption*, not a build. The memory engine is a black box behind MCP. Aura's owned Go surface is a few hundred LOC of WIRING. The single genuine design gap is **default-on** (no existing recipe auto-mounts). Everything else is copy-an-analog or REUSE-as-is.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `prd.md` / `REQUIREMENTS.md` / `ROADMAP.md` (amendment #62) | config (doc) | transform (re-scope) | `prd.md` §Slice 11 Amendment #61 (line 3662) | exact |
| `internal/mcp/manager/catalog.go` (edit `BuiltInCatalog`) | config (recipe registry) | request-response | the `calculator`/`whatsapp` recipe entries in the same func (catalog.go:26–111) | exact |
| default-on seam (`internal/config/config.go` `loadMCPServers` **or** `internal/mcp/manager/runtime.go` `RunnableManagedServers`) | config (load path) | request-response | `loadMCPServers` (config.go:314–351) / `RunnableManagedServers` (runtime.go:49–78) | role-match (NEW behavior — no existing auto-include) |
| `cmd/aura/memory.go` (NEW) | route (operator CLI) | request-response | `cmd/aura/mcp_tools.go` `openAndListManagedMCPTools` + `cmd/aura/mcp.go` `runMCPCommand` dispatch | exact |
| `docker/agent-memory/Dockerfile` (NEW) | config (build) | file-I/O | `docker/markitdown/Dockerfile` | exact (pinned `pip install` sidecar) |
| `compose.yaml` (swap `image:`→`build:` on `aura-agent-memory-mcp`) | config | — | `markitdown` service `build:`/`image:` stanza (compose.yaml:278–282) | exact |
| 16-tool Deferred + `memory__*` exposure | service (tool bridge) | request-response | `internal/agent/mcptools` `MountManagedServer`/`Bridge` | **REUSE-as-is (zero new code)** |
| fail-soft mount loop | service (boot) | event-driven | `cmd/aura/main.go` `buildRegistryWithMCP` (main.go:185–203) | **REUSE-as-is (memory flows through `cfg.MCPPolicies`)** |
| KV-cache reconciliation | test (audit) | batch | `scripts/cache_invariant_audit.sh` | **RUN-only (no edit)** |
| `cmd/aura/memory_test.go` + `*_integration` tier | test | request-response | `internal/mcp/calculator_integration_test.go` + `internal/agent/mcptools/managed_mount_test.go` | exact |

---

## Pattern Assignments

### `internal/mcp/manager/catalog.go` — add the `memory` recipe (EDIT)

**Analog:** the existing recipe entries in `BuiltInCatalog()` (same file, calculator at line 26, whatsapp at 95). This is the recipe registry CONTEXT flagged as "not under an obvious `recipe` symbol".

**Pattern to replicate — a `streamable_http` `trusted_recipe` entry** (the existing entries are all stdio `Command`-based; memory is the first HTTP recipe, so combine the recipe *shape* below with the `ManagedServer{Type/URL}` from `MountManagedServer`'s HTTP branch, mount.go:36–37):

```go
// EXISTING shape to copy (catalog.go:26-46, calculator) — name/summary/source/trust/runtime + Server:
{
    Name:       "calculator",
    Summary:    "calculator-mcp-server over stdio via uvx",
    Source:     "recipe:calculator",
    TrustClass: mcp.TrustTrustedRecipe,
    Runtime:    "local",
    Server: mcp.ManagedServer{
        Command: "uvx",
        Args:    []string{ /* ... */ },
        Source:  "recipe:calculator",
        Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
        Runtime: mcp.ManagedRuntime{Kind: "local"},
    },
},
```

**What the planner must produce instead** — an HTTP variant (A1: `trusted_recipe`, not `remote_http`):
```go
{
    Name:       "memory",
    Summary:    "neo4j-labs agent-memory (POLE+O + reasoning traces) over streamable-HTTP",
    Source:     "recipe:memory",
    TrustClass: mcp.TrustTrustedRecipe,
    Runtime:    "local",  // (HTTP — no launch command; trust class only affects policy display, A1)
    Server: mcp.ManagedServer{
        Type:   mcp.ServerTypeStreamableHTTP,   // "streamable_http" — gates the HTTP path in RunnableManagedServers/MountManagedServer
        URL:    "http://127.0.0.1:8091/mcp/",   // AURA_AGENT_MEMORY_MCP_PORT default 8091
        Source: "recipe:memory",
        Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
    },
},
```
Note: `BuiltInCatalog()` ends with `sort.Slice(... entries[i].Name ...)` (catalog.go:113) — append anywhere, it self-sorts. `LookupCatalog` (catalog.go:119) auto-covers the new entry. `aura mcp recipes` golden (`mcpRecipes`, mcp.go:70–92) will list it as `memory  trusted_recipe  local  recipe:memory  ...`.

**Env-var naming:** RESEARCH proposes the URL carry `AURA_AGENT_MEMORY_MCP_PORT` (already in compose.yaml:154, `AURA_<DOMAIN>_<UNIT>` convention). If the planner makes the recipe URL env-driven, keep that exact name; do **not** invent a new var.

---

### default-on mount seam (NEW behavior — THE gap)

**Analogs (the two load paths, pick ONE to special-case):**

1. `internal/config/config.go` `loadMCPServers` (config.go:314–351) — composes `MCPServers` (stdio launch configs) + `policies` (HTTP/managed) that flow into `cfg.MCPPolicies`, consumed by `buildRegistryWithMCP`. **Returns empty when the config file is absent.**
   ```go
   // config.go:327-334 — the exact spot policies are assembled from the managed config file
   runnableManaged, err := mcpmanager.RunnableManagedServers(managed)
   if err != nil { return nil, nil, err }
   policies := make(map[string]mcp.ManagedServer, len(runnableManaged))
   for name, server := range runnableManaged {
       policies[name] = server
   }
   ```
2. `internal/mcp/manager/runtime.go` `RunnableManagedServers` (runtime.go:49–78) — iterates `doc.ProfileServerNames(activeProfile)`; **a recipe is only present if `aura mcp install` wrote it into `servers.json` AND it's in the active profile**. There is no auto-include.

**Why this is NEW:** every existing recipe (calculator/calendar/mail/whatsapp) is opt-in via `mcpInstall` (mcp.go:94–123 writes `recipe.Server` into `doc.MCPServers` + `ensureProfileMembership`). `LoadManagedConfig` returns an empty registry on a fresh machine → catalog entry alone mounts NOTHING.

**Pattern the planner must DESIGN (Open Q1 — gate with an empty-`AURA_MCP_CONFIG` test):**
- **Option (a) seed-on-first-run:** when `LoadManagedConfig` creates a fresh file, inject the `memory` `CatalogEntry.Server` into the default profile (reuse `mcpInstall`'s `ensureProfileMembership` mechanics). Low blast radius; `rm servers.json` loses memory.
- **Option (b) inject-unless-disabled:** special-case `memory` in `loadMCPServers` (or `RunnableManagedServers`) so it's always added to `policies` unless the operator explicitly `disable`d it. Survives a deleted config; touches a shared load path. RESEARCH leans (b) for "core capability → on out of the box".

Either way the injected value is `mcpmanager.LookupCatalog("memory").Server`. **Respect the existing disable check** (runtime.go:53 `if server.Enabled != nil && !*server.Enabled { continue }`) so `aura mcp disable memory` still works.

---

### `cmd/aura/memory.go` — operator CLI (NEW)

**Analog (exact reuse pattern):** `cmd/aura/mcp_tools.go` `openAndListManagedMCPTools` (mcp_tools.go:92–117) for opening the managed HTTP server, plus the `effectiveManagedMCPServer` resolver (mcp_tools.go:56–63), plus the `runMCPCommand` switch dispatch (mcp.go:34–68) for the verb router shape.

**Server resolution + open pattern** (mcp_tools.go:56–63, 99–100):
```go
func effectiveManagedMCPServer(name string) (mcp.ManagedServer, bool, error) {
    cfg := config.LoadDB()
    if cfg.MCPServersErr != nil { return mcp.ManagedServer{}, false, cfg.MCPServersErr }
    server, ok := cfg.MCPPolicies[name]   // memory lives here after default-on
    return server, ok, nil
}
// open streamable-HTTP:
cli, err := mcp.OpenServer(ctx, name, server)   // server.Type==streamable_http || server.URL!=""
```

**Direct CallTool (the RAW wire name — NO `memory__` namespace at the wire layer; namespacing is only the agent registry's, bridge.go:122–131):**
```go
// mcp.OpenServer returns mcp.Transport; CallTool signature (http_client.go:101 / client.go:234):
//   CallTool(ctx, name string, args map[string]any) (string, error)
text, err := cli.CallTool(ctx, "memory_search", map[string]any{"query": q})
defer func() { _ = cli.Close() }()
```

**Verb-router shape to copy** (mcp.go:26–68): a top-level `runMemory(args)` that prints `usage` + exits non-zero on error, dispatching to `runMemoryCommand(ctx, args, out io.Writer) error` with a `switch args[0]`. Wire it into `main.go`'s top-level switch (main.go:43–87) as `case "memory": runMemory(os.Args[2:])` and add it to `usage()` (main.go:90–92). Reuse the package-local `writef`/`writeln`/`firstMCPDescriptionLine` helpers (mcp.go:469, mcp_tools.go:119).

**Verb→tool mapping (Claude's Discretion D, planner decides; ground-truth 16-tool list from spike 032):**
| `aura memory <verb>` | raw MCP tool | note |
|---|---|---|
| `search <q>` | `memory_search` | the spike-035 recall tool |
| `context` | `memory_get_context` | |
| `add-entity` / `add-fact` / `add-preference` | `memory_add_entity` / `memory_add_fact` / `memory_add_preference` | the 3 long-term writes |
| `sessions` | `memory_list_sessions` | |
| `trace ...` | `memory_start_trace`/`memory_record_step`/`memory_complete_trace`/`memory_get_observations` | reasoning-trace surface (D-06) |
| facts read | `graph_query` or `memory_search` | **no standalone `memory_get_facts`** (Open Q4 — live `tools/list` lacks it; spike 033 read facts via `graph_query`) |

**Timeout pattern** (mcp_tools.go:93): `ctx, cancel := context.WithTimeout(ctx, 20*time.Second)`.

---

### `docker/agent-memory/Dockerfile` (NEW) + `compose.yaml` edit

**Analog:** `docker/markitdown/Dockerfile` (the canonical in-repo pinned-`pip install` sidecar) + the compose `build:`/`image:` stanza at compose.yaml:278–282.

**Dockerfile pattern to replicate** (markitdown/Dockerfile:4–20):
```dockerfile
FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir "markitdown[all]==0.1.6" "fastapi==..." ...
WORKDIR /app
COPY app.py .
EXPOSE 8080
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8080"]
```
**Adapt for memory** (RESEARCH Code Examples; vendor the fork at pinned `c1c2d65`, NOT PyPI — Pitfall 5):
```dockerfile
FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends gcc && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY pyproject.toml README.md README-pypi.md ./
COPY src/ ./src/
RUN pip install --no-cache-dir -e ".[mcp,google,openai]"
EXPOSE 8080
# compose `command:` already overrides with --profile extended --persistent --384d flags (compose.yaml:143-152)
```

**compose.yaml edit** (mirror markitdown, compose.yaml:279–282) — swap the single `image:` line (compose.yaml:101) for:
```yaml
aura-agent-memory-mcp:
  build:
    context: ./docker/agent-memory   # vendored fork src/ pinned at c1c2d65 (A2)
  image: ${AURA_AGENT_MEMORY_MCP_IMAGE:-aura-agent-memory-mcp:local}
  pull_policy: never   # markitdown carries this on the built image (compose.yaml:282)
  # ALL existing environment/command/healthcheck/ports/depends_on (compose.yaml:102-160) stay verbatim
```
Keep the env block exactly as-is (compose.yaml:109–122) — `AURA_<DOMAIN>_<UNIT>` Aura vars + canonical upstream `NAM_*`/`NEO4J_*`/`OPENAI_*` (the third-party-sidecar naming exception, per CLAUDE.md §Env vars).

---

### PRD amendment #62 (Wave 0, doc-only) — FIRST plan

**Analog:** `prd.md` §Slice 11 **Amendment #61** (line 3662) — the immediately-preceding superseding amendment, same block-quote `> **▶ Amendment #NN (title — context, date) — ...**` format with: what it does, what it supersedes (`[SUPERSEDED #NN]` tags), and the open nodes. Also P8 #44 (line 2442, the sandbox→sandbox-agent supersede) for the "kill bespoke, adopt off-the-shelf" precedent.

**What #62 records (D-12 verbatim):** UX-06 → deferred; UX-07 → already deferred (#27, unchanged); UX-08 → recall@5/p95 become *advisory snapshots* vs the package (amendment #20 snapshot gate still applies); UX-09 → agent-written reasoning/insight recalled on demand (no `messages[2]`, no journal cron). Re-state the four UX-06..09 checkboxes in `REQUIREMENTS.md` (lines 51–54) + `ROADMAP.md` Phase 15 (lines 510–526). Also catalog `AURA_AGENT_MEMORY_MCP_*` in the PRD env index. **The amendment commit must precede any Go commit** (PRD-first; `git log` ordering is the gate).

---

## Shared Patterns

### Tool bridging — REUSE-AS-IS (zero new code)
**Source:** `internal/agent/mcptools` `MountManagedServer` (mount.go:35–64) → `Mount`/`Bridge` (bridge.go:100–159).
**Apply to:** memory's 16-tool exposure (D-06/D-07).
The HTTP branch of `MountManagedServer` already does exactly what memory needs:
```go
// mount.go:36-46
if isStreamableHTTPManagedServer(server) {   // server.Type==streamable_http || server.URL!=""
    srv, err := mcp.OpenServer(ctx, name, server)
    names, err = Mount(ctx, reg, name, srv)   // namespaces memory__*, marks Deferred
    return srv.Close, names, nil
}
```
`Bridge` sets `Deferred: true` on every spec (bridge.go:48, 129) and `namespacedName(namespace, tool)` produces `memory__memory_search` (name.go:49, `nsDelimiter = "__"`). Results are tagged `Trust: TrustUntrusted, Source: "mcp:memory"` (bridge.go:81–91). **Mount ALL 16 (plain `MountManagedServer`, NO `DenyRisk=write`)** — Pitfall 2: spike-035's `mounted=13 blocked=3` was an exploration, NOT D-06.

### Fail-soft + reconnect-on-use — REUSE-AS-IS
**Source:** `cmd/aura/main.go` `buildRegistryWithMCP` (main.go:185–203).
**Apply to:** memory boot (D-09).
```go
// main.go:191-202 — a single dead/misconfigured server WARN-and-drops; boot continues:
if _, managed := cfg.MCPPolicies[name]; managed {
    closer, _, err = mcptools.MountManagedServer(ctx, reg, name, cfg.MCPPolicies[name])
}
if err != nil {
    slog.Warn("mcp mount failed", "server", name, "err", err)
    continue   // already-mounted servers stay; non-deferred built-ins keep registry valid
}
```
A down memory sidecar yields a structured `error: ...` tool result the model self-corrects (bridge.go:74–78 — tool-level failures return inline, never loop-fatal). `newReconnectingServer` (mount.go:57) re-lists tools on reconnect. **No supervisor/ping** (locked Phase 9 lifecycle).

### KV-cache invariant — RUN-ONLY (no edit)
**Source:** `scripts/cache_invariant_audit.sh` (Postgres-free, 22 request hashes over `messages[0]`/`messages[1]`/`skillman`).
**Apply to:** D-04 confirmation. Pull-on-demand never touches the cacheable prefix → the invariant holds trivially. **Anti-pattern:** do NOT add a `messages[2]` insight stream — there is no `messages[2]` by design, and the audit only covers the three existing streams. Run it, assert unchanged.

### Env-var naming
`AURA_<DOMAIN>_<UNIT>` for Aura-owned vars (`AURA_AGENT_MEMORY_MCP_PORT`, `AURA_AGENT_MEMORY_MCP_IMAGE`, `AURA_AGENT_MEMORY_MCP_URL` for the test tier). Third-party sidecar vars keep upstream canonical naming (`NAM_*`, `NEO4J_*`, `OPENAI_*`, `OPENROUTER_API_KEY`) — the documented exception. **No new secret** this phase.

---

## Tests

### Unit (per-commit, no stack)
**Analogs:** `internal/agent/mcptools/managed_mount_test.go` `httpMCPServer` (managed_mount_test.go:18–49) — an `httptest` streamable-HTTP MCP fake that completes `initialize`/`tools/list`, driving the real `*mcp.HTTPClient` transport. Use this for:
- `cmd/aura/memory_test.go` — verb→tool mapping with the fake transport (no live sidecar).
- `cmd/aura/mcp_test.go` extension — `memory` recipe golden (`mcpRecipes` output) + **default-on assertion** (set `AURA_MCP_CONFIG` to a temp empty path; assert `memory` lands in `cfg.MCPPolicies` / `memory__memory_search` registered with NO prior `install`).
- fail-soft extension — point the recipe URL at a dead port; assert boot WARNs + continues (extend the existing `buildRegistryWithMCP` fail-soft path).

### Integration tier `memory_integration` (per-wave, stack up)
**Analog:** `internal/mcp/calculator_integration_test.go` — the exact build-tag-gated live-MCP tier shape:
```go
//go:build memory_integration   // mirror calculator_integration_test.go:1

// gate on AURA_AGENT_MEMORY_MCP_URL; t.Fatal (not t.Skip) under $CI — no-skip-as-green (CLAUDE.md)
url := os.Getenv("AURA_AGENT_MEMORY_MCP_URL")
// ... mcp.OpenServer → ListTools (assert 16, all Deferred, memory__* names) → CallTool round-trips
```
Three live tests (port spikes 032/033/035): `MemoryLiveMount` (16-tool surface), `MemoryCLI` (seed+read via `aura memory`), `MemoryLoopRecall` (`tool_search`→`memory__memory_search`→`text_response`, reuse `agenttest.FakeClient`), `MemoryReasoningTrace` (trace round-trip recall). **CI must export `AURA_AGENT_MEMORY_MCP_URL` + bring the stack up** so the tier runs, never skip-greens.

> **Note on the build-tag convention:** the repo uses **one tag per live tier** (`db_integration`, `neo4j_integration`, `web_integration`, `calculator_integration`, `whatsapp_integration`, `telegram_integration`, `multimodal_integration`). Add `memory_integration` to that family — do NOT overload an existing tag.

---

## No Analog Found

None. Every owned-surface file maps to an exact or role-match in-repo analog. The only NEW *behavior* (not new file-type) is the **default-on seam** — its load-path analogs exist (`loadMCPServers`/`RunnableManagedServers`), but the auto-include logic itself is unprecedented (all current recipes are opt-in `install`). The planner designs that logic per Open Q1; everything around it is copy-an-analog.

## Metadata

**Analog search scope:** `cmd/aura/` (mcp*.go, main.go), `internal/mcp/` (manager/catalog.go, manager/runtime.go, transport.go, http_client.go, client.go), `internal/config/config.go`, `internal/agent/mcptools/` (mount.go, bridge.go, name.go, managed_mount_test.go), `docker/markitdown/`, `compose.yaml`, `scripts/`, `prd.md` §Slice 11.
**Files scanned:** ~16.
**Pattern extraction date:** 2026-06-11.
