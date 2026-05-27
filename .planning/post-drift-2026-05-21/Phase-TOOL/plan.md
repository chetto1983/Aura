# Phase-TOOL — Tool Surface + Tool RAG + Collection Hygiene

**Status:** 🔴 next milestone after Phase-WIKI-FIX
**Provenance:** 4 parallel research scouts 2026-05-22 + live log analysis
**Estimated effort:** ~3-4 sessions Ralph atomic, 10 stories
**LOC delta:** **~-4500** net (cleanup + collapse + kill tool RAG) + ~+470 new (MCP supervisor + overrides + compact dim fix)

---

## Why this milestone

User mandate 2026-05-22 (verbatim, three iterations):
> "mettere a posto tutti i tool pulire la merda che ci gira attorno. il rag deve auto-aggiornare sui nuovi tool, non hardcoded alla cazzo"
> "poi metti a posto tutte le collection come abbiamo fatto nella memoria"
> "togli il rag dei tool, è una cagata che abbiamo fatto e consolida agli altri pattern"
> "alla fine basta il search tool come hai te"

**Locked direction:** kill the tool RAG entirely (no BM25 replacement, no semantic discovery layer). The unified `search` tool (US-LAT-05, already shipped) is the SINGLE canonical retrieval surface for wiki + sources — same shape Claude Code uses. All other "tool discovery" code (`tool_search`, `aura_tool_search_v2` collection, `internal/agent/tools/index/` reconciler) is deleted with no replacement. The LLM sees the full tool manifest in the system prompt every turn, like picobot / nanobot / openhuman.

Live evidence in logs 2026-05-22:

```
agent: invocation end elapsed_ms=179668 llm_calls=28 tool_calls=33 delivered=false
  error="context canceled"
toolindex reconcile errors:1 (recurring every 10 min)
  qdrant upsert: Vector dimension error: expected dim: 256, got 768
```

The substrate fixes from Phase-WIKI-FIX (FTS5 sync, dim-mismatch detect for `aura_memory_v1`) restored wiki retrieval — but the agent USER experience is still broken because:

1. **Tool surface is bloated and stale.** 29 registered tools + 18 orphan unregistered Tool types in code (~1500 LOC dead). Duplication clusters: 8. Estimated cleanup −2490 LOC.
2. **Tool RAG is gratuitous over-engineering.** `aura_tool_search_v2` Qdrant collection embeds tool descriptions and serves them via cosine — the dim drift bug is recurring because the collection is at 256d while embed sidecar is at 768d. Per 4-scout convergence (2026-05-22): **0/7 production agent systems use vector embedding for tool routing**. picobot/nanobot/openhuman use flat always-loaded manifests. codex has a `tool_search` handler but on BM25, not vectors. Aura is alone in this anti-pattern AND user 2026-05-22 made the call: kill the whole layer, no replacement.
3. **MCP layer can't hot-reload.** New MCP tools registered at runtime don't reach the index. Hermes has `notifications/tools/list_changed` listener; Aura doesn't.
4. **Other Qdrant collections (`aura_memory_v1_compact`, `aura_tool_search_v2`) still suffer the silent dim-mismatch + stale reconcile** that FIX-04 only fixed for `aura_memory_v1`.

Phase-TOOL is **Step 2 — ToolSurface** of the locked post-DRIFT sequence (memory `project_post_drift_phase_sequence_locked`): RAG+rerank → **ToolSurface** → TokenJuice → AgentLoop.

---

## Convergent patterns from 4-scout research (2026-05-22)

| Pattern | Sources | Translatability | Lifted? |
|---|---|---|---|
| **No tool RAG at all** — flat always-loaded manifest | picobot/nanobot/openhuman registries | 5/5 | Story US-TOOL-01 (DELETE the RAG, no replacement) |
| **Kitchen-sink action-enum tools** | picobot `internal/agent/tools/filesystem.go:40-60`, codex `automation_update`, openhuman composio | 5/5 | US-TOOL-05/06/07 |
| **MCP description override sidecar** | cli-printing-press `internal/mcpoverrides/overrides.go:73-100` | 5/5 | US-TOOL-08 |
| **MCP `tools/list_changed` listener** | Hermes `tools/mcp_tool.py:1088-1129` | 4/5 | US-TOOL-09 |
| **Per-MCP-server circuit breaker** | Hermes `tools/mcp_tool.py:1711-1755` | 5/5 | folded into US-TOOL-09 |
| **Mechanical description composition** (action + Required + Optional + Returns) | cli-printing-press `internal/mcpdesc/compose.go:69-96` | 5/5 | US-TOOL-04 |

Anti-pattern reaffirmed by 4/4 systems: **vector embedding of tool descriptions for routing** is wrong shape. User-confirmed 2026-05-22: also wrong shape are middle-grounds like BM25 retrieval-as-tool — for a registry of <50 tools the always-loaded manifest is the right answer; the retrieval layer is gratuitous.

Full scout artifacts:
- `docs/research-2026-05-22/tool-rag-patterns.md` (7 systems × 8 bullets)
- `docs/research-2026-05-22/tool-lifecycle-patterns.md` (4 systems × 8 bullets)
- `docs/research-2026-05-22/aura-tool-surface-audit.md` (full inventory, duplication clusters, god files)
- `docs/research-2026-05-22/tool-collapse-namespace-patterns.md` (kitchen-sink + deferred tier patterns)

---

## Stories

### US-TOOL-01 — KILL the tool RAG entirely; all tools always-on in the manifest

- **Scope:** User mandate 2026-05-22 verbatim: "togli il rag dei tool, è una cagata che abbiamo fatto". The 4-scout convergence is unanimous: 0/7 production agent systems use vector embedding for tool routing. picobot, nanobot, openhuman, codex (after its `tool_search` handler) all expose the full tool manifest to the LLM every turn, with no semantic discovery layer. Aura today over-engineered this with Qdrant + reconciler + drift detection + dim handling. The whole stack goes.
  - DELETE `internal/agent/tools/index/` package entirely (reconciler, qdrant adapter, fsnotify watcher, the lot).
  - DELETE the `tool_search` tool from the registry (it returns from a manifest the LLM already has — round-trip waste).
  - DELETE the `aura_tool_search_v2` Qdrant collection.
  - DELETE any code that pushes tool descriptions to vector storage.
  - All registered tools (native + MCP) appear in the system-prompt manifest every turn. After Phase-TOOL collapses (US-TOOL-05/06/07) the catalog is ~12-15 tools × ~180 char description = ~2700 char ≈ ~700 prompt tokens. Within budget for always-loaded.
- **Pattern source (DELETE, not lift):** picobot `internal/agent/tools/registry.go` (flat map[name]Tool, no discovery), nanobot `agent/runner.py` (all tools surfaced via `_build_tools_for_request`), openhuman `agent/harness/tools` (TOML allowlist per agent, no runtime discovery).
- **Files:** DELETE [internal/agent/tools/index/](internal/agent/tools/index/) entire package; DELETE [internal/agent/tools/registry/tool_search.go](internal/agent/tools/registry/tool_search.go) + tests; MODIFY [cmd/aura/app.go](cmd/aura/app.go) + [cmd/aura/app_wire.go](cmd/aura/app_wire.go) (drop reconciler init + tool_search registration); manual via Qdrant API: DELETE collection `aura_tool_search_v2`; AUDIT system-prompt manifest size after collapses to confirm it stays under 1k tokens.
- **LOC delta:** **-700** (the index package alone is ~500 LOC; tool_search tool ~80; tests + adapters ~120).
- **Acceptance:**
  - `grep -rn aura_tool_search_v2 internal/ cmd/` returns 0 results.
  - `grep -rn 'internal/agent/tools/index' internal/ cmd/` returns 0 results (the package is gone).
  - `grep -rn 'tool_search' internal/ cmd/` returns 0 results in production code (or only historical comments).
  - `docker logs aura-aura-1 --since 30m | grep -i 'toolindex reconcile'` shows ZERO entries 30 min after restart.
  - `docker logs aura-aura-1 --since 30m | grep -i 'Vector dimension error'` shows ZERO entries.
  - Qdrant `GET /collections` does not list `aura_tool_search_v2`.
  - System prompt manifest is auto-generated from the live registry; manifest token count logged at boot (<1000 tokens after Phase-TOOL collapses ship).
- **Single atomic commit:** `feat(tools): kill tool RAG entirely; all tools always-on in manifest (US-TOOL-01)`
- **Priority:** P0 — the architectural fix; closes the recurring log error AND removes a whole substrate the agent never needed.

### US-TOOL-02 — Delete 18 unregistered orphan Tool types (+ tests)

- **Scope:** Scout 3 found 18 `Tool`-implementing types registered in code but never wired in the production composition root (`cmd/aura/app.go` / `app_wire.go`). Most are the 11 verb-tools behind `source` / `file` / `web` / `doc` dispatchers — they're internal-only, dispatched by name from their parent dispatcher, never LLM-facing. Their LLM contracts (`Name()`, `Description()`, `Parameters()`) are dead code. Delete them + their tests.
- **Files:** DELETE the orphan Tool files (audit list in [docs/research-2026-05-22/aura-tool-surface-audit.md](docs/research-2026-05-22/aura-tool-surface-audit.md) §6); MODIFY their dispatcher parents to inline the verb logic.
- **LOC delta:** **-1500** (prod ~-1000 + tests ~-500).
- **Acceptance:**
  - Every type implementing `Tool` interface in `internal/agent/tools/registry/` has at least one registration site in `cmd/aura/` (proven by grep).
  - `go test ./internal/agent/tools/... -count=1` green.
  - Probe suite passes (no probe relies on a deleted tool).
- **Single atomic commit:** `chore(tools): delete 18 unregistered orphan Tool types (US-TOOL-02)`

### US-TOOL-03 — Fix `text_response` registration + remove `search_memory` zombie

- **Scope:** Two registry hygiene fixes:
  - **`text_response`** is referenced by name in `internal/agent/terminal.go:222` (hard-coded terminal detection) but NEVER registered in the production composition root (Scout 3 P0). Either properly register it OR remove the hard-coded reference and detect terminal via the `IsTerminal()` method. Pick the cleaner path after reading the surrounding terminal logic.
  - **`search_memory`** self-declares `DEPRECATED` in its Description() but is still registered alongside the unified `search` tool that replaced it. Remove the registration; remove the tool file; update any test that still references it.
- **Files:** MODIFY [cmd/aura/app.go](cmd/aura/app.go) (or wherever tool registration happens) + [internal/agent/terminal.go](internal/agent/terminal.go); DELETE [internal/agent/tools/registry/search_memory.go](internal/agent/tools/registry/search_memory.go) + its tests.
- **LOC delta:** ~+10 / -200 = **-190**.
- **Acceptance:**
  - `text_response` either registered properly and the agent loop calls it via the standard tool path, OR removed from hard-coded references entirely.
  - Tool catalog returns `search` only, no `search_memory`.
  - All probe cases still pass.
- **Single atomic commit:** `fix(tools): wire text_response + drop deprecated search_memory (US-TOOL-03)`

### US-TOOL-04 — Description quality audit: EN-only, ≤200 char, structural markers

- **Scope:** Three audit dimensions in one story:
  - (a) **EN-only**: `text_response` and `ask_user_clarification` carry IT paragraphs in their descriptions (Scout 3 P1). Per memory `feedback_all_prompts_in_english_only`, all instructional text in the system prompt must be EN; output in IT is via explicit directive. Rewrite to EN.
  - (b) **≤200 char**: Extend `description_audit_test.go` to assert `len(tool.Description()) <= 200`. Sweep all catalogued tools; trim verbose ones.
  - (c) **Structural markers**: Apply cli-printing-press `internal/mcpdesc/compose.go:69-96` shape: first sentence is action verb + Returns clause; followed by `Required:` / `Optional:` argument hints. Auto-suffix `Destructive.` on DELETE-class, `Partial update.` on PATCH-class.
- **Files:** MODIFY [internal/agent/tools/registry/description_audit_test.go](internal/agent/tools/registry/description_audit_test.go); MODIFY every catalogued tool's `Description()` that violates.
- **LOC delta:** ~-50 (descriptions get shorter) + 30 LOC test helpers = -20.
- **Acceptance:**
  - `go test ./internal/agent/tools/registry/...` green with new ≤200 char assertion.
  - No `Description()` returns text containing common IT words (`gli`, `agli`, `nella`, `dei`, `delle` — quick lint).
  - Bench delta: per-turn prompt token count drops 500-1500 tokens (Anthropic 2026 doc).
- **Single atomic commit:** `chore(tools): description audit — EN-only + 200char cap + structural markers (US-TOOL-04)`

### US-TOOL-05 — Collapse `schedule_task` / `list_tasks` / `cancel_task` → `task(mode=create|list|cancel)`

- **Scope:** Kitchen-sink action-enum collapse. picobot's `cron.go` shape (`internal/agent/tools/cron.go:25-62`) and codex `automation_update(mode)` both prove the shape. Three tools become one with `mode` enum + per-mode argument schema. The internal dispatch goes to the existing three handler functions; only the LLM-facing surface collapses.
- **Files:** NEW or MODIFY [internal/agent/tools/registry/task.go](internal/agent/tools/registry/task.go) (single tool); DELETE leaf scheduler files; MODIFY composition root.
- **LOC delta:** **-150**.
- **Acceptance:**
  - Registry contains ONE `task` tool with `mode` enum {create, list, cancel}.
  - Probe: each mode works; reply text references mode in user-facing copy.
  - Audit log via [scheduler_audit_test.go](internal/agent/tools/registry/scheduler_audit_test.go) (if exists) updated.
- **Single atomic commit:** `feat(tools): collapse schedule_task/list/cancel → task(mode=) (US-TOOL-05)`

### US-TOOL-06 — Collapse `create_xlsx` / `create_docx` / `create_pdf` → `create_document(format=xlsx|docx|pdf)`

- **Scope:** Same kitchen-sink shape. The three file-generation tools have 90%+ identical scaffolding (per codebase-cleanup-audit 2026-05-21 §3.1 row 2). Collapse to one tool with `format` enum + per-format builder dispatch.
- **Files:** MODIFY [internal/agent/tools/registry/files_*.go](internal/agent/tools/registry/) → fold to one `create_document` tool + per-format builder; DELETE the three leaf files.
- **LOC delta:** **-300** (largest file-level clone in the repo).
- **Acceptance:**
  - Registry contains ONE `create_document` tool.
  - Probe: artifact-level verification per `feedback_inspect_artifact_visually_not_just_pass_status` — open produced xlsx/docx/pdf, assert structure.
- **Single atomic commit:** `feat(tools): collapse create_xlsx/docx/pdf → create_document(format=) (US-TOOL-06)`

### US-TOOL-07 — Collapse `workspace_write` / `workspace_read` / `list_memory` / `read_memory` → `workspace(action=read|write|list)`

- **Scope:** Picobot `FilesystemTool` shape (`internal/agent/tools/filesystem.go:40-60`). Four read/write/list-shaped tools sharing the `path` schema collapse to one. **Note**: `edit_memory` and `delete_memory` (and `forget_memory`) keep separate identities — their schemas diverge enough that collapsing them obscures intent. Only the cleanly-overlapping four collapse.
- **Files:** NEW or MODIFY [internal/agent/tools/registry/workspace.go](internal/agent/tools/registry/workspace.go); DELETE leaf files.
- **LOC delta:** **-250**.
- **Acceptance:**
  - Registry contains ONE `workspace` tool with action enum.
  - LLM tool-selection accuracy probe: queries that previously called `workspace_write` now call `workspace(action=write)` consistently.
- **Single atomic commit:** `feat(tools): collapse workspace_* + memory_* readers → workspace(action=) (US-TOOL-07)`

### US-TOOL-08 — MCP description override sidecar (`tools-overrides.json`)

- **Scope:** Lift cli-printing-press `internal/mcpoverrides/overrides.go:73-100`. A user-editable `runtime-workspace/tools-overrides.json` (or extend `mcp.json`) provides per-tool-name description overrides. Loaded once at boot + on `tools-overrides.json` write (fsnotify). Unmatched override keys SURFACE as warnings so typos don't silently no-op. Override format: `{"server_tool_name": "new description text"}`.
- **Files:** NEW [internal/mcp/overrides.go](internal/mcp/overrides.go) + tests; MODIFY [internal/agent/tools/registry/mcp.go](internal/agent/tools/registry/mcp.go) (apply at registration time).
- **LOC delta:** **+120**.
- **Acceptance:**
  - Writing an override entry for an existing MCP tool name → next-boot description reflects the override.
  - Typo override key (no matching tool name) → warning logged at boot.
  - fsnotify on the file → live reload without restart.
- **Single atomic commit:** `feat(mcp): tools-overrides.json sidecar for description tuning (US-TOOL-08)`

### US-TOOL-09 — MCP supervisor: `tools/list_changed` listener + per-server circuit breaker + reconnect

- **Scope:** Three Hermes patterns folded into one MCP-layer story (all updates apply to the in-memory tool registry — there is no tool RAG to push to after US-TOOL-01):
  - (a) **`notifications/tools/list_changed` listener** (`mcp_tool.py:1088-1129`). When an MCP server pushes a list-changed notification, re-fetch its tools list, diff against the in-memory registry, upsert/remove tools so the system-prompt manifest picks them up on the next turn without restart.
  - (b) **Per-server circuit breaker** (`mcp_tool.py:1711-1755`). N consecutive failures from one MCP server → server marked unhealthy, its tools temporarily removed from the registry until manual or scheduled retry. Other MCP servers and native tools keep working.
  - (c) **Exponential-backoff reconnect** (`mcp_tool.py:1504-1660`). On stdio child crash or HTTP server unreachable, automatic reconnect with backoff.
- **Files:** MODIFY [internal/mcp/client.go](internal/mcp/client.go) + [internal/mcp/](internal/mcp/) (supervisor logic); MODIFY [internal/agent/tools/registry/mcp.go](internal/agent/tools/registry/mcp.go) (handle list-changed events into in-memory registry).
- **LOC delta:** **+350**.
- **Acceptance:**
  - Kill an MCP stdio child mid-conversation → next iter, child auto-restarted with backoff; agent doesn't crash.
  - MCP server pushes `tools/list_changed` → new tool visible in next-turn manifest within 5 sec without restart.
  - 3 consecutive failures from server X → circuit opens, logged warning; manifest stops listing server X tools; other tools still callable.
- **Single atomic commit:** `feat(mcp): supervisor with list_changed + circuit breaker + reconnect (US-TOOL-09)`

### US-TOOL-10 — Apply Phase-WIKI-FIX hygiene to `aura_memory_v1_compact` collection

- **Scope:** Phase-WIKI-FIX FIX-04 added dim-mismatch detection + auto-rebuild for `aura_memory_v1` (the wiki collection). The compact memory collection `aura_memory_v1_compact` still suffers the same class of bug. Replicate the fix:
  - Boot-time dim probe vs `cfg.EmbeddingOutputDim`; auto-rebuild on mismatch unless `AURA_NO_REBUILD_ON_DIM_MISMATCH` set.
  - Reuse the rebuild skip-and-continue logic (Phase-WIKI-FIX FIX-03).
  - Extend `POST /api/wiki/reindex` (or add `POST /api/compact/reindex` if separate semantics).
  - `aura_tool_search_v2` is killed entirely by US-TOOL-01; no fix needed for it.
- **Files:** MODIFY [internal/storage/memoryindex/](internal/storage/memoryindex/) (compact collection owner); MODIFY [internal/api/](internal/api/) reindex endpoints.
- **LOC delta:** **+80** (reuse pattern from FIX-04).
- **Acceptance:**
  - Boot-time: change `EMBEDDING_OUTPUT_DIM` setting → `aura_memory_v1_compact` auto-rebuilt at new dim.
  - `POST /api/compact/reindex` (or unified endpoint) drops + recreates with skip-continue + report.
  - No silent dim mismatch in logs for 24h after change.
- **Single atomic commit:** `fix(compact): dim-mismatch auto-detect + rebuild for compact collection (US-TOOL-10)`

---

## Sequencing

| # | Story | Depends on | Why this order |
|---|---|---|---|
| 1 | US-TOOL-01 (BM25 tool RAG) | nothing | THE architectural fix; kills recurring log error |
| 2 | US-TOOL-10 (compact dim fix) | nothing | Parallel — different collection, no overlap |
| 3 | US-TOOL-02 (orphan deletion) | nothing | Independent cleanup |
| 4 | US-TOOL-03 (text_response + zombie) | nothing | Independent registry hygiene |
| 5 | US-TOOL-04 (description audit) | US-TOOL-02, 03 | Audit after orphans gone, after wrong refs fixed |
| 6 | US-TOOL-05 (task collapse) | US-TOOL-04 | Collapse after descriptions cleaned (new tool description must pass new audit) |
| 7 | US-TOOL-06 (create_document collapse) | US-TOOL-04 | Same |
| 8 | US-TOOL-07 (workspace collapse) | US-TOOL-04 | Same |
| 9 | US-TOOL-08 (MCP overrides) | nothing | Independent; can ship parallel with 5/6/7 |
| 10 | US-TOOL-09 (MCP supervisor) | US-TOOL-01 | Supervisor pushes to BM25; needs BM25 first |

**One story = one commit per [`feedback_one_module_per_slice`].**

---

## Verification — phase exit criteria

After all 10 stories ship:

1. `docker logs aura-aura-1 --since 1h | grep -i 'dim'` returns zero entries (no dim-mismatch noise).
2. Tool count in registry: from 29 native + 18 orphans → ~15-18 native + 0 orphans.
3. `len(tool.Description())` ≤ 200 for every catalogued tool (verified by test).
4. Tool RAG: `tool_search(query)` returns top-5 in <5ms via BM25, no Qdrant calls.
5. New MCP tool registered → appears in BM25 within 5 sec.
6. Total LOC delta: **~-4000** (cleanup) **+~600** (BM25 + MCP supervisor) = **net -3400**.
7. Bench rerun (substrate + e2e probe_chat): improvements on tool-selection accuracy + no degradation on retrieval Recall@5.

---

## Risks

- **R1 (US-TOOL-01):** Killing the tool RAG means the LLM sees the full manifest every turn. After kitchen-sink collapses (US-TOOL-05/06/07) the manifest is ~12-15 tools × ~180 char description = ~2700 char ≈ ~700 prompt tokens. Well within budget. If the registry ever crosses 50+ tools (unlikely with Aura's scope), a deferred-loading tier can be reconsidered — but it would be lexical/manifest-based, not vector-RAG.
- **R2 (US-TOOL-02):** Some "orphan" tools may be registered through a path the audit missed (e.g. swarm composition). Mitigation: probe suite + golangci-lint clean + manual smoke test before merging the deletion.
- **R3 (US-TOOL-05/06/07):** LLM tool-selection accuracy on enum-dispatched tools depends on description quality. Mitigation: ship US-TOOL-04 (description audit) FIRST so the collapsed tools' descriptions are sharp.
- **R4 (US-TOOL-09):** MCP supervisor is non-trivial (~350 LOC). Risk of subtle race conditions in reconnect path. Mitigation: write the supervisor test FIRST (TDD), simulate stdio crash + http unreachable in unit tests before integration.
- **R5 (US-TOOL-08 + US-TOOL-09):** Both touch MCP layer. Ship US-TOOL-08 first (smaller, isolated to override-config); US-TOOL-09 builds on the layer that 08 stabilizes.

---

*Updated 2026-05-22. Per CLAUDE.md DEEP REFACTOR ON TOUCH: every story commit must include golangci-lint clean + dupl clean + LOC ≤600 + dead code removed on touched files.*
