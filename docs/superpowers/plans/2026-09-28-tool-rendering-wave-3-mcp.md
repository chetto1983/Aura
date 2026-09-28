# MCP and Evidence Tool Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show trusted memory, calendar and WhatsApp read results in useful cards while preserving MCP Apps, Source Explorer and the intentional row-only actions.

**Architecture:** At MCP execution, stamp a host-owned recipe source/tool/action marker into result metadata and the append-only tool ledger. Live display projection consumes that marker; replay reads the owner-scoped ledger before re-deriving the same payload. Strict parsers admit only known result shapes; read actions can add tables, stats or references, while server-supplied MCP Apps views and Aura's evidence/worker owners remain intact.

**Tech Stack:** Go MCP bridge, `toolinvocations.Store`, `internal/agent/display`, AG-UI snapshot, React/TypeScript, Elements Data table from Wave 1, Tool UI Stats display and citation/link-preview source.

**Spec:** `docs/superpowers/specs/2026-09-28-tool-rendering-tool-ui-elements-design.md` (Wave 3 result rendering). Editable `send_email` and `send_message` review is covered by its own authorization plan.

## Global Constraints

- A model tool name or server-returned JSON does not prove a trusted source. The host marker is set only by the bridge after selecting a trusted managed recipe; the server cannot write it.
- A replay snapshot may consult only the authenticated conversation's append-only invocation facts. If the marker is absent or inconsistent, render escaped raw text. Never infer trust from today's mounts when replaying an old call.
- Bound rows/bytes, validate exact action/tool schema and URL. Keep `aura.mcp_view` as the calendar and WhatsApp browse surface, `WebResultDisplay`/`DocumentDisplay`/Source Explorer for evidence, and `WorkerPane`/`SwarmReportTable` for worker detail.
- Preserve the Wave 1 router fallback, English/Italian copy, blue tokens, responsive tables, and files under 600 lines. Commit one tested deliverable per task and leave unrelated worktree edits alone.
- The 29 calendar actions, 15 WhatsApp tools and 13 memory tools have dispositions in the spec. Only `send_email` and `send_message` move to the separate draft plan; other destructive actions keep current approvals.

## Review Focus

1. A renamed or untrusted server advertising `memory_search` cannot gain a rich trusted card; Task 1 tests this.
2. A historical call must not change presentation when a server mount is removed or reused; Task 1 tests this.
3. A memory fact with markup or an oversized object is rendered as escaped text or bounded cell text; Task 2 tests this.
4. A calendar/WhatsApp MCP Apps view still opens after an additional compact result card is attached; Task 3 tests this.
5. A web citation with `ref_id` still opens the same Source Explorer entry; Task 3 tests this.

---

### Task 1: Host-owned MCP provenance in live and replay

**Files:**
- Modify: `internal/agent/mcptools/bridge_call.go`, its tests
- Modify: `internal/agent/llm_agent_events.go`, `llm_agent_display.go`, event tests
- Modify: `internal/agent/display/preview.go`, `normalize.go`, tests
- Modify: `internal/agui/server_messages.go`, `server_display.go`, their focused tests
- Modify: `internal/agui/server.go` and composition root in `cmd/aura/serve_webui.go` only to inject the existing `toolinvocations.Store` read interface.

**Interfaces:**
- Consumes: bridge `policy.recipeSource`, actual raw MCP tool name and validated calendar action; ledger `ListByConversation(ctx, conversationID)` end rows.
- Produces: runtime-only `tools.Spec.TrustedRecipeSource` set by the trusted bridge, plus host-only result metadata `aura_display_source` with exact recipe source, tool and action. Live `PreviewInput.TrustedMCP *TrustedMCP` comes from that bridge-owned marker; replay obtains it only when the marker matches the call and an owner-scoped ledger end fact. `NormalizeToolPreview` uses it for MCP cases; native cases are unchanged.

- [ ] **Step 1: Write failing trust and parity tests.** A trusted memory call gets a marker; an arbitrary mounted server using the same tool name does not. A trusted calendar action marker contains the actual action, and a client-supplied `aura_display_source` argument cannot change it. For snapshot, pair assistant/tool turns with an end ledger fact and compare live/replay display JSON; remove/mismatch the fact and require raw fallback.

```go
fact := toolinvocations.Event{ToolCallID:"m1", ToolName:"memory__memory_search", Event:toolinvocations.EventEnd,
  Meta:map[string]any{"aura_display_source":map[string]any{"recipe":"recipe:memory","tool":"memory_search"}}}
// projectDisplaySnapshotWithFacts(history, []toolinvocations.Event{fact}) may enrich m1;
// the same history with nil facts must leave m1.display absent.
```

- [ ] **Step 2: Run `go test ./internal/agent/mcptools ./internal/agent/display ./internal/agui -run 'Test.*(MCPDisplay|DisplaySnapshot)' -count=1`; confirm failures.**
- [ ] **Step 3: Implement metadata and the owner-scoped read.** Set `Spec.TrustedRecipeSource` only from the bridge's verified policy, never from an advertised schema. Set the result marker after `CallTool` succeeds, never from payload content. Stamp it into `ToolInvocation.Meta` through the existing result metadata path. In `handleMessages`, list invocation facts only after `GetForIdentity` succeeds; the existing `toolinvocations.Store.ListByConversation` is the read source. Match conversation, tool-call ID, tool name, event=end and marker; ignore duplicate/inconsistent facts. Pass the marker into the same normalizer used live. If the ledger read fails, serve the transcript without MCP rich displays rather than failing the thread.

```go
type TrustedMCP struct { Recipe, Tool, Action string }
type PreviewInput struct { ToolCallID, ToolName, Arguments, ResultPreview string; TrustedMCP *TrustedMCP }
if in.TrustedMCP != nil && in.TrustedMCP.Recipe == "recipe:memory" && in.TrustedMCP.Tool == "memory_search" {
    return normalizeMemorySearch(in)
}
```

- [ ] **Step 4: Run focused tests, `go test ./internal/agui ./internal/agent/display ./internal/agent/mcptools -count=1`, and a replay fixture with a changed mount list.**
- [ ] **Step 5: Commit only provenance/rehydration files.** Use `feat(display): preserve trusted MCP source across replay`.

### Task 2: Memory facts, diagnostics and graph references

**Files:**
- Create: `internal/agent/display/memory.go`, `memory_test.go`
- Modify: display payload/normalize/preview tests and TS mirror/router tests
- Create: `web/src/chat/displays/MemoryFactsDisplay.tsx`, `MemoryStatsDisplay.tsx`, focused tests
- Add only copied Tool UI `stats-display` and `code-block` sources; reuse Elements Data table from Wave 1.
- Modify: `web/src/i18n/resources.display.ts`.

**Interfaces:**
- Consumes: Task 1 `TrustedMCP` with `recipe:memory`; source shapes from `cmd/arcadedb-mcp` for `memory_search`, `memory_recall`, `memory_facts_about`, `memory_entities`, `graph_diagnostics`, `graph_schema`, `graph_path`.
- Produces: bounded `KindTable`, `KindStats`, `KindCode`, or graph workspace reference; mutation calls and `memory_digest` retain row/raw receipt unless their result matches a documented bounded summary.

- [ ] **Step 1: Write failing parser/card tests from the served tool schemas and result builders in `cmd/arcadedb-mcp`.** Fixtures include a fact with subject/predicate/object/validity, empty list, duplicate entity, diagnostic numeric counts, schema JSON, path with a valid graph reference, an oversized object, and a write tool whose text imitates a read result. Only exact trusted read names may produce a typed card.

```go
in := PreviewInput{ToolCallID:"m1", ToolName:"memory__memory_search", ResultPreview:`{"facts":[{"statement":"A knows B","subject":"A","predicate":"knows","object":"B","sources":[]}],"retrieval":{"path":"lexical","abstained":false}}`,
  TrustedMCP:&TrustedMCP{Recipe:"recipe:memory",Tool:"memory_search"}}
p, ok := NormalizeToolPreview(in, NewRegistry())
if !ok || p.Table == nil || len(p.Table.Rows) != 1 { t.Fatalf("memory table = %+v, %v", p, ok) }
```

- [ ] **Step 2: Run `go test ./internal/agent/display -run TestMemory -count=1` and the new Vitest files; confirm failures.**
- [ ] **Step 3: Implement exact parsers and cards.** Read the local server result constructors before matching each shape; do not assume all tools return the same JSON wrapper. Limit rows and cell lengths, retain validity/source fields, and show an explicit truncation count. Project diagnostics as finite numeric stats, schema as escaped JSON code, and a path only to the existing graph workspace if its reference is validated. Reuse Wave 1 DataTable adapter with Aura filter/export controls; never render fact values as Markdown/HTML.

```go
if len(rows) > maxDisplayRows { rows = rows[:maxDisplayRows]; truncated = true }
for _, count := range diagnostics { if count < 0 { return Payload{}, false } }
```

- [ ] **Step 4: Run Go/Vitest/typecheck and a real memory MCP read through the cockpit, then reopen the thread.**
- [ ] **Step 5: Commit only memory projection/card files.** Use `feat(chat): render trusted memory reads`.

### Task 3: Calendar/WhatsApp reads and existing evidence owners

**Files:**
- Create: `internal/agent/display/sidecars.go`, `sidecars_test.go`
- Modify: display payload/normalize/preview tests and TS router/tests/i18n
- Modify: `web/src/chat/displays/WebResultDisplay.tsx`, `DocumentDisplay.tsx`, their tests only for Tool UI citation/link-preview presentation
- Modify: `web/src/chat/displays/SwarmReportTable.tsx` or `web/src/chat/workers/WorkerPane.tsx` only if a compact Elements Subagent list demonstrably preserves their navigation; otherwise keep both intact.
- Add only selected Tool UI citation/link-preview source. Add a focused cockpit Playwright scenario under `web/e2e/`.

**Interfaces:**
- Consumes: Task 1 `TrustedMCP` for calendar's `action` and WhatsApp's exact tool name; existing `mcp_view` descriptor, `ref_id` / Source Explorer callback and trusted artifact URLs.
- Produces: a bounded row/table/document reference for read actions where the actual result builder supports it; no new authorization or draft behavior. Current MCP Apps view remains present and clickable.

- [ ] **Step 1: Write failing fixtures for the calendar fork's 29-action enum and WhatsApp fork's 15 tool names at the pinned revisions in the spec mapping.** Capture each read result shape from its server result builder before asserting a typed projection. Test a read list, an empty read, attachment path without an Aura asset, a view-bound read, an unknown action, and a destructive action with a success-shaped body. Test a web result with `ref_id` and a click opening the same Source Explorer entry.

```go
in := PreviewInput{ToolCallID:"w1", ToolName:"whatsapp__send_message", ResultPreview:`{"chats":[]}`,
  TrustedMCP:&TrustedMCP{Recipe:"recipe:whatsapp",Tool:"send_message"}}
if _, ok := NormalizeToolPreview(in, NewRegistry()); ok { t.Fatal("send result promoted to read table") }
```

- [ ] **Step 2: Run focused Go/Vitest and confirm failures.**
- [ ] **Step 3: Implement only shape-backed compact read displays.** Use the exact trusted action/tool allowlist, strict bounded projection, and raw fallback for unsupported shapes. Preserve `aura.mcp_view` attachment and source routing. Port Tool UI citation/link-preview markup into current evidence cards without dropping `ref_id`, `onOpenSource`, link safety, or fetched-document Markdown sanitization. Keep worker report/pane unless an Elements list passes their existing navigation/error tests.

```tsx
<CitationBubble source={source} onOpenSource={() => onOpenSource(source.ref_id)} />
// The copied frame can wrap this owner callback; it cannot replace ref_id with href.
```

- [ ] **Step 4: Run Go/Vitest/typecheck and cockpit Playwright for memory, calendar view, WhatsApp view, web citation and a malformed MCP fallback, live then reloaded.** Verify row-only dispositions against the spec inventory and record actual UX measurements before PRD edits.
- [ ] **Step 5: Commit only sidecar/evidence files.** Use `feat(chat): render trusted sidecar reads beside MCP Apps`.

### Task 4: Remaining native dispositions and the whole-wave gate

**Files:**
- Modify: `internal/agent/display/preview.go`, `normalize.go`, tests for `task`, `skill`, `plugin_pack`, `swarm_status`, `document_search` and `document_open` when their producer shape supports a typed result
- Modify: `web/src/chat/displays/SwarmReportTable.tsx`, `web/src/chat/workers/WorkerPane.tsx`, their focused tests only if the optional Elements Subagent list keeps existing navigation/errors
- Modify: `web/src/chat/displays/ChartDisplay.tsx`, `DisplayRouter.tsx`, `types.ts`, `internal/agent/display/payload.go` and their tests only if the `chart` kind is still producerless at implementation time; remove the dead kind and component together.
- Add a whole-wave Playwright scenario under `web/e2e/`.

**Interfaces:**
- Consumes: Wave 1 `KindTable`/row fallback, existing worker status/report, current source registry and artifact descriptors.
- Produces: table for verified list-shaped `task`, `skill` or `plugin_pack` results; existing worker/evidence behavior for other calls. No read-only result becomes an action control.

- [ ] **Step 1: Write failing tests for each list/result mode the spec names.** A `task list` with two jobs makes a bounded table; create/run/cancel are rows. `skill list` and `plugin_pack` list render only their documented fields; `skill_manage` and other mutations remain receipts. `swarm_status` preserves worker IDs, errors and navigation. `document_search` retains `ref_id`; `document_open` shows an authorized artifact link only when an asset ID exists. A response that only names a container path is raw text. `send_file` retains its `LocalArtifactDisplay` controls, and WhatsApp `get_media_data`/`download_media` without an Aura asset ID remain receipts. Inventory all Go producers of `KindChart` and `KindTable` before changing those declarations.

```go
if _, ok := NormalizeToolPreview(PreviewInput{ToolCallID:"t1",ToolName:"task",Arguments:`{"action":"cancel"}`,ResultPreview:"cancelled"}, NewRegistry()); ok {
    t.Fatal("task cancellation must remain a receipt, not a fabricated table")
}
```

- [ ] **Step 2: Run focused display/worker/evidence tests and confirm failures.**
- [ ] **Step 3: Implement only verified list projections and preserve owners.** Reuse Elements DataTable for actual structured rows. Keep row/raw for ambiguous schemas and action receipts. Keep existing worker pane/report unless an Elements Subagent list passes the same ID/error/navigation fixtures. Route document links through the existing source registry and asset route. If `rg 'KindChart' internal -g '*.go'` still shows definitions/tests but no producer, remove `KindChart`, the TS kind/router arm and `ChartDisplay`/its tests in the same commit; if a producer now exists, retain and test it.

```tsx
return artifact?.asset_id
  ? <LocalArtifactDisplay payload={{ artifact }} />
  : <ToolResultPanel argsText={rawArgs} result={rawResult} />;
```

- [ ] **Step 4: Run `go test ./internal/agent/display ./internal/agui -count=1`, focused Vitest, web typecheck and whole-wave Playwright.** Exercise memory, calendar/WhatsApp MCP Apps, web/document source clicks, workers, table and malformed raw fallback live and after reload. Record observations before PRD changes.
- [ ] **Step 5: Commit only these disposition/E2E files and perform whole-wave review.** Use `feat(chat): complete remaining tool result dispositions`.
