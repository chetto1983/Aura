# Runtime Workspace Bootstrap And Graph Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura fast and intelligent by giving the runtime agent a Picobot-style narrow workspace, automatic first-run layout bootstrap, and a materialized wiki graph/search context that avoids repeated broad file reads.

**Architecture:** Keep the repository as the developer workspace, but expose a separate runtime workspace to Aura's LLM tools. The runtime workspace contains only the files Aura needs to operate: `AGENT.md`, wiki, skills, MCP config, heartbeat/runtime notes, and optional inbox/source mounts. Wiki graph data becomes a materialized artifact updated by domain services after wiki writes and reindex jobs, so both dashboard and prompt preloading can read compact node/edge/context summaries quickly.

**Tech Stack:** Go, Docker Compose, SQLite, existing `internal/workspace`, `internal/wiki`, `internal/search`, `internal/telegram`, `internal/settings`, bounded workspace file tools, Qdrant/SQLite search, Garage backup/export.

---

## Current Evidence

- Aura currently exposes `AURA_WORKSPACE_ROOT=/app`, which is the whole repository.
- Live wiki shape on 2026-05-08:
  - 22 wiki pages
  - 53 wiki graph edges
  - 0 broken refs
  - 4 graph orphans
  - 32 `wiki/raw/*` source directories
  - 3 local skill directories
- Dashboard graph is built by scanning wiki pages in `internal/api/wiki.go`.
- Search graph documents are built by `internal/search/graph_documents.go` as `graph:node:*` and `graph:index:*`.
- Picobot pattern from `D:\tmp\picobot`:
  - onboarding creates a dedicated workspace;
  - context reads only bootstrap files and memory from that workspace;
  - filesystem tool is rooted to the workspace;
  - memory is file-backed and small;
  - system triggers stay stateless.

## Non-Goals

- Do not move live SQLite, wiki, or skills into Garage in this phase.
- Do not make Garage the source of truth until restore/import/sync is tested.
- Do not remove dashboard graph, source/OCR ingest, scheduler, auth, Telegram, MCP, Qdrant, or backups.
- Do not expose raw `git`, raw shell, or live DB mutation to Aura runtime tools.
- Do not rewrite the entire wiki storage layer.

## Target Runtime Layout

Default local/dev workspace remains the repository for Codex and developers.

Aura runtime workspace should become:

```text
runtime-workspace/
  AGENT.md
  HEARTBEAT.md
  mcp.json
  wiki/
    SCHEMA.md
    index.md
    log.md
    raw/
    graph/
      graph.json
      context.md
  skills/
  inbox/
```

Docker can mount this as `/workspace` while still mounting implementation code at `/app` for the binary/container image. If bind-mount migration is too disruptive in the first slice, keep `/app` mounted but set `AURA_WORKSPACE_ROOT=/workspace` and mount the narrow directories explicitly.

## Task 1: Add Runtime Workspace Config And Bootstrap Paths

**Files:**
- Modify: `internal/config/config.go`
- Modify: `.env.example`
- Modify: `compose.yaml`
- Test: `internal/config/config_test.go`

- [x] **Step 1: Add config defaults**

Add or confirm these config fields:

```go
RuntimeWorkspacePath string `envconfig:"AURA_RUNTIME_WORKSPACE_PATH" default:"./runtime-workspace"`
WorkspaceRoot        string `envconfig:"AURA_WORKSPACE_ROOT" default:"."`
```

Docker target:

```yaml
AURA_RUNTIME_WORKSPACE_PATH: "/workspace"
AURA_WORKSPACE_ROOT: "/workspace"
PROMPT_OVERLAY_PATH: "/workspace"
```

- [x] **Step 2: Write config tests**

Add coverage proving:

```go
t.Setenv("AURA_RUNTIME_WORKSPACE_PATH", "/workspace")
t.Setenv("AURA_WORKSPACE_ROOT", "/workspace")
t.Setenv("PROMPT_OVERLAY_PATH", "/workspace")
cfg := Load()
```

Expected:

```text
RuntimeWorkspacePath == "/workspace"
WorkspaceRoot == "/workspace"
PromptOverlayPath == "/workspace"
```

- [x] **Step 3: Run focused tests**

Run:

```powershell
go test ./internal/config -count=1
```

Expected: PASS.

- [x] **Step 4: Commit**

Stage only the config, compose, and env template files.

```powershell
git add internal/config/config.go internal/config/config_test.go compose.yaml .env.example
git commit -m "slice 07: add runtime workspace config"
```

## Task 2: Bootstrap The Local Runtime Layout Before DB Open

**Files:**
- Create: `internal/runtimebootstrap/bootstrap.go`
- Create: `internal/runtimebootstrap/bootstrap_test.go`
- Modify: `cmd/aura/main.go`
- Modify: `docs/container.md`
- Modify: `docs/implementation-tracker.md`

- [x] **Step 1: Write failing bootstrap tests**

Create tests for:

```go
func TestEnsureLayoutCreatesRuntimeWorkspace(t *testing.T)
func TestEnsureLayoutDoesNotOverwriteExistingFiles(t *testing.T)
func TestEnsureLayoutCreatesParentDirsForEnvDBLogsSkillsAndMCP(t *testing.T)
```

The expected created paths:

```text
runtime-workspace/AGENT.md
runtime-workspace/HEARTBEAT.md
runtime-workspace/mcp.json
runtime-workspace/wiki/
runtime-workspace/wiki/raw/
runtime-workspace/wiki/index.md
runtime-workspace/wiki/log.md
runtime-workspace/wiki/graph/
runtime-workspace/skills/
runtime-workspace/inbox/
```

- [x] **Step 2: Implement `EnsureLayout`**

Function shape:

```go
type LayoutConfig struct {
	RuntimeWorkspacePath string
	EnvPath              string
	DBPath               string
	LogDir               string
	WikiPath             string
	SkillsPath           string
	MCPServersPath       string
	PromptOverlayPath    string
}

func EnsureLayout(cfg LayoutConfig) error
```

Rules:

- create directories with `0755`;
- create files only when missing;
- never overwrite `.env`, `AGENT.md`, `index.md`, `log.md`, or `mcp.json`;
- `mcp.json` default content is `{}`;
- `HEARTBEAT.md` default content is a short disabled template;
- `AGENT.md` seed can copy root `AGENT.md` when present, else use a minimal runtime schema;
- do not touch database files except creating parent directories.

- [x] **Step 3: Wire bootstrap before DB open**

In `cmd/aura/main.go`, call `runtimebootstrap.EnsureLayout(...)` after config load and before SQLite open/setup wizard.

Expected behavior:

- first run no longer requires manual `New-Item data,wiki,skills,garage`;
- Docker bind mounts get their missing host directories/files created by startup;
- setup wizard can write `data/.env` because parent directories exist.

- [x] **Step 4: Run focused tests**

Run:

```powershell
go test ./internal/runtimebootstrap ./cmd/aura -count=1
```

Expected: PASS.

- [x] **Step 5: Commit**

```powershell
git add internal/runtimebootstrap cmd/aura/main.go docs/container.md docs/implementation-tracker.md
git commit -m "slice 07: bootstrap runtime workspace layout"
```

## Task 3: Narrow Docker Workspace Mounts

**Files:**
- Modify: `compose.yaml`
- Modify: `Dockerfile` if directory ownership needs adjustment
- Test: Docker smoke commands

- [ ] **Step 1: Change Compose mounts**

Target shape:

```yaml
environment:
  AURA_RUNTIME_WORKSPACE_PATH: "/workspace"
  AURA_WORKSPACE_ROOT: "/workspace"
  PROMPT_OVERLAY_PATH: "/workspace"
volumes:
  - ./runtime-workspace:/workspace
  - ./data:/data
  - ./wiki:/workspace/wiki
  - ./skills:/workspace/skills
```

Keep source code at `/app` only if needed for bind-mounted development. Do not expose `/app` through `AURA_WORKSPACE_ROOT`.

- [ ] **Step 2: Verify container sees the narrow root**

Run:

```powershell
docker compose config --quiet
docker compose up -d --build aura
docker compose exec -T aura sh -lc 'echo $AURA_WORKSPACE_ROOT; find /workspace -maxdepth 2 -type f | sort | head -40'
```

Expected:

```text
/workspace
```

The listing must include runtime files and must not list `internal/`, `cmd/`, `web/`, `.git/`, `data/aura.db`, or build artifacts.

- [ ] **Step 3: Verify bounded file tools root**

Run existing debug or Telegram sandbox smoke with a prompt equivalent to:

```text
lista i file disponibili nel tuo workspace e dimmi cosa puoi modificare
```

Expected:

- tool output lists runtime workspace files only;
- no repo source tree appears;
- model does not try to read `AGENTS.md`.

- [ ] **Step 4: Commit**

```powershell
git add compose.yaml Dockerfile docs/implementation-tracker.md
git commit -m "slice 07: narrow Aura runtime workspace"
```

## Task 4: Materialize Wiki Graph Files

**Files:**
- Create: `internal/wiki/graph.go`
- Create: `internal/wiki/graph_test.go`
- Modify: `internal/wiki/store.go`
- Modify: `internal/api/wiki.go`
- Modify: `internal/search/search.go` or the reindex path that calls graph document builders

- [ ] **Step 1: Write graph builder tests**

Test inputs:

```markdown
---
title: Alpha
category: test
related:
  - beta
schema_version: 2
prompt_version: ingest_v1
created_at: "2026-05-08T00:00:00Z"
updated_at: "2026-05-08T00:00:00Z"
---

# Alpha

See [[beta]] and [[missing]].
```

Expected graph:

```json
{
  "nodes": [{"id":"alpha","title":"Alpha","category":"test"}],
  "edges": [{"source":"alpha","target":"beta","type":"wikilink"}],
  "broken_refs": [{"source":"alpha","target":"missing"}]
}
```

- [ ] **Step 2: Implement graph materializer**

Function shape:

```go
func BuildGraph(pages map[string]*Page) Graph
func WriteGraphFiles(dir string, graph Graph) error
```

Outputs:

```text
wiki/graph/graph.json
wiki/graph/context.md
```

`context.md` should include:

```markdown
# Wiki Graph Context

- Pages: N
- Edges: N
- Orphans: N
- Broken refs: N

## Hubs
- [[davide]] degree=13
```

- [ ] **Step 3: Update after wiki writes and migrations**

Where `Store.WritePage`, `MigrateYAMLToMD`, reindex/debug cleanup, or explicit index rebuild currently updates `index.md`, also refresh `wiki/graph/*`.

- [ ] **Step 4: Switch dashboard graph to materialized graph**

`GET /api/wiki/graph` should prefer `wiki/graph/graph.json`; if missing or invalid, rebuild from pages and write it.

- [ ] **Step 5: Run tests**

```powershell
go test ./internal/wiki ./internal/api ./internal/search -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/wiki internal/api/wiki.go internal/search docs/implementation-tracker.md
git commit -m "slice 07: materialize wiki graph cache"
```

## Task 5: Add A Compact Memory Pack Before LLM Turns

**Files:**
- Create: `internal/conversation/memory_pack.go`
- Create: `internal/conversation/memory_pack_test.go`
- Modify: `internal/telegram/conversation_context.go` or current speculative search injection point
- Modify: `internal/conversation/system_prompt.go`

- [ ] **Step 1: Write tests for pack composition**

Given:

- user text: `come posso migliorare Aura?`
- search hits: `aura-operating-memory`, `piano-di-miglioramento`
- graph context with hubs
- recent log entries

Expected pack:

```markdown
## Memory Pack

### Relevant Pages
- [[aura-operating-memory]]
- [[piano-di-miglioramento]]

### Graph Context
...

### Recent Wiki Log
...
```

Pack must stay under a byte/token budget.

- [ ] **Step 2: Implement pack budget**

Function shape:

```go
type MemoryPackInput struct {
	UserText       string
	SearchContext string
	GraphContext  string
	RecentLog      string
	MaxBytes       int
}

func ComposeMemoryPack(input MemoryPackInput) string
```

Default max: 8-12 KB.

- [ ] **Step 3: Inject pack once per turn**

Replace scattered broad prompt additions with a single compact pack after overlay/skills and before tool profile instructions.

- [ ] **Step 4: Run focused tests**

```powershell
go test ./internal/conversation ./internal/telegram -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/conversation internal/telegram docs/implementation-tracker.md
git commit -m "slice 07: inject compact memory pack"
```

## Task 6: Benchmark And Live Smoke The New Runtime

**Files:**
- Modify: `cmd/debug_telegram_sandbox/main.go` if it needs expectation flags for workspace listing or memory pack evidence
- Create or modify: targeted tests under `cmd/debug_telegram_sandbox`
- Modify: `docs/implementation-tracker.md`

- [ ] **Step 1: Add debug expectations**

Add flags if missing:

```text
-expect-workspace-root /workspace
-forbid-path-fragment internal/
-forbid-path-fragment .git/
-expect-memory-pack
```

- [ ] **Step 2: Run live benchmark prompts**

Run three prompts against the live Docker runtime:

```powershell
go run ./cmd/debug_telegram_sandbox -prompt "che cartelle puoi vedere nel tuo workspace?" -expect-workspace-root /workspace
go run ./cmd/debug_telegram_sandbox -prompt "come posso migliorare Aura usando la wiki e il grafo?" -expect-swarm -expect-loop-steps-max 4
go run ./cmd/debug_telegram_sandbox -prompt "trova una skill utile e dimmi cosa fa" -expect-loop-steps-max 4
```

Targets:

- no repo source tree exposure;
- no repeated broad file loops;
- under 30 seconds for ordinary memory/wiki questions;
- token count lower than the previous 174k-token bad turn;
- tool calls start from search/swarm or compact workspace reads.

- [ ] **Step 3: Run full verification**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
docker compose config --quiet
docker compose up -d --build aura
```

Expected: PASS, `/status` ok.

- [ ] **Step 4: Commit closure**

```powershell
git add cmd/debug_telegram_sandbox docs/implementation-tracker.md .planning/phases/07-runtime-workspace-bootstrap-graph-cache/PLAN.md
git commit -m "slice 07: verify narrow workspace runtime"
```

## Acceptance Criteria

- Aura runtime file tools can no longer see the whole repo by default.
- First-run startup creates required local layout without manual folder creation.
- `AGENT.md` is loaded from the runtime workspace.
- `wiki/graph/graph.json` and `wiki/graph/context.md` exist and refresh after wiki writes/reindex.
- Dashboard graph reads the materialized graph or rebuilds it safely.
- LLM turns receive a compact memory pack instead of repeatedly scanning broad files.
- Live Docker `/status` remains ok.
- Live debug prompt for workspace listing does not expose `internal/`, `.git/`, `web/`, `data/aura.db`, or build artifacts.
- Garage remains backup/artifact-only until restore/import/sync exists.

## Execution Order

1. Runtime config.
2. Local layout bootstrap.
3. Narrow Docker workspace.
4. Materialized wiki graph.
5. Compact memory pack.
6. Benchmark and live verification.

Do not combine tasks 2-5 into one commit. Each task should be independently testable and revertible.

## Open Questions For The User

- Runtime workspace folder name: `runtime-workspace/` or `.aura/workspace/`?
- Should `HEARTBEAT.md` be enabled in this phase, or only seeded as a disabled placeholder?
- Should `wiki/graph/context.md` be committed as durable wiki infrastructure, or treated as generated cache?
