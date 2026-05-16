# Phase05 Plan - Consolidate Tools

Status: closed 2026-05-15 for the Phase-I metadata, visibility, and eval
slice. MCP-standard hint shape was adopted; US-I01..US-I06 shipped and all
benchmark rows are met.

## Goal

Tools become agent capabilities, not random global services. Every tool
carries deterministic metadata (visibility, capability, MCP behavioral hints)
so the runtime + dashboard can reason about safety without ad-hoc heuristics
AND so Aura's native tools are interchangeable with MCP-exported tools.

## Industry pattern source (D:/tmp curated)

- **MCP spec** (referenced in `D:/tmp/cli-printing-press/AGENTS.md:33`) defines
  four behavioral hints on every tool:
  - `readOnlyHint` — tool does not modify state
  - `destructiveHint` — tool may delete or overwrite data
  - `idempotentHint` — repeating the call produces the same effect
  - `openWorldHint` — tool interacts with the outside world (vs sandboxed)
  - Missing annotations default to "could write or delete" (fail closed)
  - HTTP method mirror: `GET` → read-only + open-world,
    `DELETE` → destructive + open-world,
    `POST/PUT/PATCH` → open-world.
- **Nanobot** (`D:/tmp/nanobot/nanobot/agent/tools/base.py:155`) uses a
  simpler `read_only(self) -> bool` + `exclusive` pair. Aura already has
  `ErrAwaitingUserInput` (Phase-D) as the exclusivity mechanism, so we do
  not need a separate `exclusive` flag.
- **Decision:** adopt the MCP 4-hint shape on ToolDefinition. Aura's
  internal tools + MCP-imported tools share one vocabulary. Visibility tier
  (always_on / active_turn / deferred) is Aura-specific (the MCP spec does
  not have one — MCP exposes everything; Aura's deferred-discovery is a
  prompt-budget optimization).

## Scope

The structural consolidation under `agent/tools/` is already shipped (Phase-A
work landed `internal/agent/tools/{registry,index,sets,swarm}` packages,
deterministic order, structured errors, capability per tool, examples,
deferred discovery, `AlwaysOnCore`). Phase-I closes the remaining metadata
+ eval gates from `prd.md` §6 Phase 5:

- add `ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`, `OpenWorldHint`
  bool fields on ToolDefinition (MCP standard);
- add `VisibilityTier` (`always_on` / `active_turn` / `deferred`) on
  ToolDefinition (Aura-specific, drives discovery budget);
- catalogue every registered tool with the 4 hints + tier;
- verify active-turn visibility cannot bypass authorization;
- add discovery top-k evals (queries → expected tool rank);
- add parameter accuracy evals for example-backed tools.

## Non-Goals

- No new tool registration / no flattening of subpackages.
- No new visibility model invention — three tiers + MCP 4-hints only.
- No authorization rewrite — Phase01B identity grants stay authoritative.
- No `exclusive` field — `ErrAwaitingUserInput` already serves that role.
- No secret value logging.

## What was already shipped (do not redo)

| PRD bullet | Status | Where |
|---|---|---|
| Move registry/index/toolsets/swarmtools under agent/tools | DONE | internal/agent/tools/{registry,index,sets,swarm} (Phase-A) |
| Preserve subpackage boundaries | DONE | 4 distinct packages, no god file |
| Stabilize tool order | DONE | registry.go:144 + :166 — `sort.Strings(names)` |
| Typed schemas | DONE | ToolDefinition.Parameters (JSONSchema map) |
| Structured errors | DONE | tools/registry/error.go (classifyToolError + FormatToolError) |
| Required capability per tool | DONE | ToolDefinition.RequiredCapability (identity.Capability) |
| Smallest control surface always loaded | DONE | agent.AlwaysOnCore + per-turn pool |
| Deferred discovery default | DONE | manifest.go pattern (deferred-tools rollout) |
| Curated examples for complex tools | DONE | ToolDefinition.Examples + examples.go |
| Secret-safe logging | DONE | memory-locked invariant + audit |

## Gap inventory (Phase-I)

| PRD bullet | Status | Needed |
|---|---|---|
| Visibility tier per tool | DONE | ToolDefinition.VisibilityTier plus per-tool assignment (US-I01/US-I02) |
| Risk class per tool | DONE as MCP hints | MCP-standard hint fields (ReadOnlyHint, DestructiveHint, IdempotentHint, OpenWorldHint) |
| Active-turn visibility never bypasses authorization | DONE | visibility/authz regression test (US-I03) |
| Discovery top-k evals | DONE | top-k fixture/eval test (US-I04) |
| Parameter accuracy evals | DONE | examples-vs-parameter schema eval (US-I05) |

## PRD Coverage (post-Phase-I)

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
|---|---|---|---|---|
| Deterministic tool list/order | this file | benchmark.md | registry.go | DONE |
| Typed schemas and structured errors | this file | benchmark.md | definition.go + error.go | DONE |
| Visibility/capability/risk class | this file | benchmark.md | (Phase-I US-I01..I02) | met |
| Deferred discovery | this file | benchmark.md | manifest.go + pool.go | DONE |
| Secret-safe logging | this file | benchmark.md | memory invariant | DONE |
| Discovery top-k evals | this file | benchmark.md | (Phase-I US-I04) | met |
| Parameter accuracy evals | this file | benchmark.md | (Phase-I US-I05) | met |
| Active-turn visibility doesn't bypass authz | this file | benchmark.md | (Phase-I US-I03) | met |

## Ralph queue (Phase-I, 6 stories)

- **US-I01** — Add MCP behavioral hints + visibility tier to ToolDefinition.
  Four new bool fields (`ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`,
  `OpenWorldHint`) defaulting to `false` so unmigrated tools fail closed
  (default = "could write or delete"). New `VisibilityTier` enum string field
  (`always_on` / `active_turn` / `deferred`); default `active_turn`. Test
  that defaults are conservative on a tool that does not provide a
  Definition(). MCP-imported tools inherit hints from the MCP server's
  declaration (no behavior change for MCP — they already pass these through).

- **US-I02** — Catalogue every registered native tool with the 4 MCP hints +
  tier. Edits each tool's `Definition()` method (or adds one). Bulk-style
  story; the gate is "every tool in registry.Definitions() has at least one
  non-default field". Recommended initial assignment (review in story):
  - `tool_search`, `request_dashboard_token`, `ask_user` → always_on
  - `read_skill`, `wiki_page`, `read_memory`, `list_memory` → active_turn,
    read_only=true, idempotent=true
  - `search_memory`, `web_search`, `web_fetch` → active_turn, read_only=true,
    open_world=true (note: NOT idempotent — remote state drifts)
  - `store_source`, `ingest_source`, `ocr_source` → active_turn,
    idempotent=true (SHA-256 dedup), open_world=true
  - `create_xlsx`, `create_docx`, `create_pdf` → deferred,
    open_world=false (sandbox), idempotent=false
  - `workspace_write`, `forget_memory`, `source_delete` → deferred,
    destructive=true, open_world=false
  - `schedule_task`, `cancel_task`, `list_tasks` → deferred,
    open_world=false
  - `execute_code` → deferred, open_world=false (sandbox),
    destructive=true (potential, sandboxed but still mutates files)

- **US-I03** — Authorization-vs-visibility test. A tool with
  `VisibilityTier=always_on` but a `RequiredCapability` the actor lacks
  must still fail `identity.Authorize`. Tests exercise both paths: visible
  + authorized → succeeds; visible + unauthorized → fails closed; not
  visible + authorized → tool_not_in_pool (not authz failure).

- **US-I04** — Discovery top-k eval. New fixture
  `internal/agent/tools/index/eval_topk_fixture.json` with 10-15 queries
  paired with their expected canonical tool name. Test drives
  `tool_search` (or the deferred-tools registry search) against the fixture
  and asserts ≥70% of queries rank the expected tool in top-3. Loud failure
  on regression — this is the eval the PRD §6 Phase 5 gate names.

- **US-I05** — Parameter accuracy eval. For each tool with curated
  `Examples`, parse every `Example.Arguments` through the tool's
  `Parameters` JSONSchema and assert 100% pass. Uses an off-the-shelf Go
  JSONSchema validator (only if a dependency is acceptable per Phase-G
  experience; otherwise hand-rolled check on the schema fields the tools
  actually use — type + required only).

- **US-I06** — Phase 5 closure. Append progress.md row with SHAs for
  US-I01..I05. Update benchmark.md actuals. Update INDEX.md and prd.md §6
  if needed (the gate criteria should now all read "met").

## Implementation Gate

Closed: every native registered tool is catalogued, active-turn visibility does
not bypass authorization, discovery and parameter evals pass, and benchmark.md
records the actuals. Future tool policy work belongs to a new bounded slice,
not to the closed Phase-I metadata pass.
