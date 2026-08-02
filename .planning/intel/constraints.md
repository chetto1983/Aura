# Constraints

## Responsive App Shell and Sidebar Contract
- source: D:\Aura\docs\cockpit-overhaul\02-shell-sidebar-SPEC.md
- type: nfr
- content:
  DATA_A7F2C91D_START
  Layout-level region orchestration = media queries (`sm:`/`lg:`). The shell is a page layout responding to the viewport. Component-internal adaptation = container queries where a region must look right at any width it is given. If reserving sidebar and/or right-panel width would push the chat lane below `--chat-lane-min` (380px), those regions are not laid out in flow; they collapse to overlay drawers/sheets instead.
  DATA_A7F2C91D_END

## Aura Cockpit Design System — Editorial Graphite
- source: D:\Aura\docs\cockpit-overhaul\03-design-system-SPEC.md
- type: nfr
- content:
  DATA_3B8E5A04_START
  Aesthetic (locked): Editorial graphite, premium-calm. A warm graphite dark base, a distinctive editorial display serif for headings, a calm technical grotesque for body/UI, a tabular monospace for all numeric/runtime instruments, generous spacing, and one restrained warm-gold accent. Hard constraints include a single binary, reuse of `web/tokens/tokens.json` and `generate-theme.mjs`, whole-cockpit theming, WCAG 2.2 AA minimum, and no file over 600 LOC.
  DATA_3B8E5A04_END

## Authula Web-Auth Integration
- source: D:\Aura\docs\cockpit-overhaul\05-authula-auth-SPEC.md
- type: protocol
- content:
  DATA_D1C6F7B2_START
  Verdict: ADOPT as an embedded Go library (Option A2). Mount `auth.Handler()` under `/auth/*` for credential flows; replace only the cookie-validation core inside Aura's `RequireAuth`, leaving `RequireCapability` / `capability_grants` / `agent.run` gating untouched. Three mandatory hardenings: dedicated `authula` Postgres schema via `search_path`; `__Host-` / `Secure` / `SameSite=Strict` cookie; CSRF enabled.
  DATA_D1C6F7B2_END

## Candidate Reference-Project Evaluation
- source: D:\Aura\docs\cockpit-overhaul\06-candidates-eval-SPEC.md
- type: nfr
- content:
  DATA_9E24A8C5_START
  All three repos earn study; only odysseus earns deep study. The single highest-value takeaway is that a panel that would crush the chat lane is removed from the chat lane entirely on mobile, becomes an overlay/bottom-sheet, gets out of the way the moment another surface opens, and restores afterward. No candidate displaces an existing Aura architectural decision.
  DATA_9E24A8C5_END

## Aura Deep Search Event and Display Contracts
- source: D:\Aura\docs\design\aura-deep-search-figma\ux-spec.md
- type: api-contract
- content:
  DATA_6F0D3B97_START
  Aura event -> display classifier -> chat renderer | display renderer | code/raw view | tree node | system event. Operator or agent request -> risk classifier -> pending config/skill state -> approval queue -> activation + audit. The model must never emit raw CSS selectors, scripts, URLs to execute, or unbounded DOM mutations. UI-control events should be replayable from the run log so debugging a session reconstructs what the operator saw.
  DATA_6F0D3B97_END

## aura-pim-mcp Interop Gate
- source: D:\Aura\docs\superpowers\plans\2026-06-16-aura-pim-mcp-fork.md
- type: protocol
- content:
  DATA_C4A19E62_START
  Aura's Go MCP client is proven against the .NET server over stdio and against Python FastMCP over streamable-HTTP, but never against .NET over HTTP. This gate decides whether the agent mounts over HTTP (preferred) or must fall back to stdio. GATE DECISION: GREEN -> Phase 3 mounts the agent over HTTP. RED -> Phase 3 mounts the agent over stdio, while the REST admin API still runs over HTTP for the cockpit.
  DATA_C4A19E62_END

## Industrial Multimodal Asset Pipeline
- source: D:\Aura\docs\superpowers\plans\2026-06-18-industrial-multimodal-asset-pipeline.md
- type: schema
- content:
  DATA_8D57F0AC_START
  The shared substrate must land before web and Telegram can converge: asset metadata and object storage; web presign/finalize/status APIs; document processor and protected prompt context; web attachment UX; generic image/audio processors; Telegram adapter refactor. Each wave is independently testable and should be committed separately.
  DATA_8D57F0AC_END

## OpenClaw Plugin Compatibility Host
- source: D:\Aura\docs\superpowers\specs\2026-06-02-openclaw-plugin-compatibility-design.md
- type: protocol
- content:
  DATA_B2E6049F_START
  Make OpenClaw plugins installable and usable in Aura without turning Aura into a wrapper around the OpenClaw Gateway. Aura should gain the leverage of the OpenClaw plugin ecosystem, including tools, providers, channels, hooks, and services. Aura must still own the agent loop, configuration policy, audit trail, human approval gates, context management, and runtime governance.
  DATA_B2E6049F_END

## Aura Plugins — Unified Extension Model
- source: D:\Aura\docs\superpowers\specs\2026-06-14-aura-plugins-unified-extension-design.md
- type: protocol
- content:
  DATA_F9A13C76_START
  The shared unit is a declarative manifest that composes primitives Aura already has (MCP servers + skills + hooks), fanned out by one installer to existing machinery. No code-loading plugin host, no Node ESM sidecar, no dynamic native-code ABI. No OpenClaw binary/manifest compatibility. Command and manifest: `aura plugins` and `aura.plugin.json`; hook authoring uses both in-process Go and out-of-process command programs; provider extension is deferred.
  DATA_F9A13C76_END

## Aura Orchestrator Control Flow
- source: D:\Aura\docs\superpowers\specs\2026-06-15-aura-orchestrator-design.md
- type: protocol
- content:
  DATA_5C8D2E41_START
  An LLM authors the plan, a deterministic Go engine executes it with typed agent slots. The LLM decides what (the plan and each task's answer); the Go executor guarantees how (ordering, concurrency, budget, verification, failure isolation).
  DATA_5C8D2E41_END

## aura-pim-mcp Sidecar
- source: D:\Aura\docs\superpowers\specs\2026-06-16-calendar-pim-mcp-fork-design.md
- type: protocol
- content:
  DATA_E703B6A9_START
  Unified PIM: one server for mail + calendar + contacts; retire the standalone mail-mcp. All connect/management UI is in Aura's own frontend. Keep a thin C# fork that tracks upstream. Use a REST admin API for the cockpit plus MCP-over-HTTP for the agent, with per-deployment OAuth configured through Aura's admin UI.
  DATA_E703B6A9_END

## Durable Swarm Messaging
- source: D:\Aura\docs\superpowers\specs\2026-06-29-durable-swarm-messaging-design.md
- type: protocol
- content:
  DATA_1A9F4D83_START
  Use Postgres as the source of truth for tasks, messages, idempotency, retries, leases, and channel-thread mapping. Treat in-process events and Postgres `NOTIFY` as wakeup optimizations only. Keep the substrate channel-agnostic. Delivery semantics are at-least-once, never exactly-once; everything outside DB-local effects relies on idempotency keys.
  DATA_1A9F4D83_END

## Aura Calm Prism Chat Refinement
- source: D:\Aura\docs\superpowers\specs\2026-07-15-aura-calm-prism-chat-refinement-design.md
- type: nfr
- content:
  DATA_7C2E58B0_START
  Aura's chat should feel quieter during conversation, clearer while work is running, and richer when a result deserves structure. Quiet conversation lets prose and user messages carry the reading experience; expressive progress reveals reasoning, tools, and approvals through concise semantic rows; rich results retain stronger visual treatment for typed displays and artifacts. This is a client-side interaction and presentation refinement and does not change backend fields, event protocols, persistence schemas, approval authorization, or artifact routing.
  DATA_7C2E58B0_END

## Unified Cloud Routing
- source: D:\Aura\docs\superpowers\specs\2026-07-17-unified-cloud-routing-design.md
- type: protocol
- content:
  DATA_4F6A0C91_START
  One override, `compose.cloud.yaml`, is the cloud-appliance file: it plumbs six modalities to OpenRouter on the running image and removes GPU-only inference sidecars from the dependency graph. Embed/rerank bases are set without `/v1` because those clients append `/v1/embeddings|/v1/rerank`; STT/TTS use `LLM.BaseURL` with `/v1` because they append `/audio/...`.
  DATA_4F6A0C91_END

## Document Search and Ingestion Consolidation
- source: D:\Aura\docs\superpowers\specs\2026-07-22-document-search-consolidation-design.md
- type: protocol
- content:
  DATA_D8B317E5_START
  Consolidate the agent's document surface so it stops confusing uploaded/indexed documents with files the agent creates in `/workspace`, and give it a deliberate bridge between the two. Add a static `<documents>` doctrine block with aligned tool descriptions and a `document_index` tool that indexes a `/workspace` file into the identity's knowledge base on demand. Auto-indexing and retrieval re-architecture are out of scope.
  DATA_D8B317E5_END

## Cockpit Compact Chat UI
- source: D:\Aura\docs\superpowers\specs\2026-07-23-cockpit-compact-chat-ui-spec.md
- type: nfr
- content:
  DATA_2E94C6FA_START
  Collapsed-by-default, always; per-part ephemeral expand state; the persisted preference is retired. `web/src/chat/reasoningPref.ts` is deleted; `ReasoningPill` holds a plain `useState(false)`. No localStorage read or write. Approvals render between the message list and the composer and are never collapsed, grouped, or placed behind a disclosure.
  DATA_2E94C6FA_END

## Mid-Turn Steering
- source: D:\Aura\docs\superpowers\specs\2026-07-23-mid-turn-steering-design.md
- type: protocol
- content:
  DATA_A06D8B3C_START
  A steer is additive user input, queued FIFO per run and injected as one plain `user` message per steer at the top of the agent loop, before the budget gate and after the previous round's tool results. It never interrupts a tool mid-execution, never extends the Budget, is echoed on the wire as a custom AG-UI frame, is persisted at drain time, and is bounced back to the client if the run ends before injection.
  DATA_A06D8B3C_END
