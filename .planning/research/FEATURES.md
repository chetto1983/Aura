# Feature Research

**Domain:** Operator web cockpit ("Aura Deep Search") over an already-shipped Go agentic substrate
**Researched:** 2026-06-15
**Confidence:** HIGH (design package is the truth-source; every backend-existence claim cross-checked against the cited Go file/CLI)

> **Scope note for the roadmapper.** This milestone does NOT invent features. The 8-frame design package
> (`docs/design/aura-deep-search-figma/`) already specifies the cockpit in depth. This file CATEGORIZES the
> designed surfaces into buildable groups, marks each as table-stakes / differentiator / anti-feature,
> rates complexity (S/M/L), and — critically — states **backend dependency: exists** (with the
> package/CLI/endpoint) or **needs-building** (with the specific gap). The two cross-cutting gaps that gate
> almost everything are (1) the **richer AG-UI typed-display protocol** and (2) **web auth**. Read those two
> rows first.

---

## The Two Gating Dependencies (read first)

### GAP-1 — Richer AG-UI typed-display protocol  (needs-building, L, blocks ~6 frames)

The shipped AG-UI gateway (`internal/agui/server.go`, `internal/agui/translator.go`, mounted by
`cmd/aura/serve.go` at `chat.cfg.AGUIBind`) emits **standard AG-UI events only**: `RUN_STARTED`,
`TEXT_MESSAGE_*`, `REASONING_*`, `TOOL_CALL_START/ARGS/END/RESULT`, `STATE_DELTA`, `RUN_FINISHED`,
`RUN_ERROR`, plus **one** custom event `aura.artifact` (`ArtifactEventName`, `translator.go:19`). The
agent `Event` (`internal/agent/event.go`) carries forward-compat slots (`Actions.StateDelta`,
`Actions.ArtifactDelta`) but has **no concept of a typed display** (`web_result`, `graph_chunk`,
`swarm_report`, `mcp_*`, `skill_*`, `ui_control`, `system_event`).

The design's whole premise (ux-spec §"Implementation Model", display-type mapping lines 420-438) is a
**display classifier → typed renderer** lane. Today a tool result reaches the frontend only as an opaque
`TOOL_CALL_RESULT` text preview (`translator.go:103`, `emitToolInvocation`). To render typed cards the
backend must emit a structured payload (CUSTOM events keyed by display type, or a typed `tool_call_result`
envelope). This is the single biggest backend build of the milestone and most frames depend on it.

### GAP-2 — Web auth  (needs-building, M, blocks any non-loopback exposure)

The gateway is **deliberately unauthenticated** — the loopback bind IS the compensating control
(`server.go:71`, amendment #35; `serve.go:221`). `.planning/PROJECT.md` §Out of Scope explicitly lists
"Multi-user con auth/RBAC reale … Niente login, niente sessioni HTTP autenticate, niente OAuth in v1." This
milestone adds web auth, which is a **scope expansion of PROJECT.md** and must be in an early phase before
the cockpit is reachable off-loopback or before any mutating governance action is exposed over HTTP. The
existing `identity` + `capability_grants` scaffolding (`cmd/aura identity`, PROJECT.md CORE-03) is the seam
to build session auth on, but no HTTP login/session/cookie/token layer exists today.

---

## Feature Landscape

### Table Stakes (Users Expect These)

Without these the cockpit "feels broken" as a frontend over the substrate. All map to shipped backend.

| Feature | Why Expected | Complexity | Backend dependency (evidence) |
|---------|--------------|------------|-------------------------------|
| `aura serve` + reachable web app shell | A cockpit needs a server + page to load | M | **Exists (serve, partial).** `cmd/aura/serve.go` mounts the AG-UI gateway + `/healthz` `/readyz` `/metrics` `/debug/vars` (`server.go:90-98`). **Needs-building:** static asset serving / SPA host + (GAP-2) auth + TLS (`serve+TLS` per milestone). |
| Chat stream (send message, stream tokens) | This is the core agent loop surface; PROJECT.md Core Value | S (frontend) | **Exists.** `POST /agent/run` → SSE `TEXT_MESSAGE_*` via `Translate` (`translator.go:47`); `GET /threads/{id}/messages` snapshot (`server.go:220`). |
| Conversation/investigation list + history + rename/archive/delete | "Claude.ai-style" multi-thread is shipped (CORE-04) | S (frontend) | **Exists.** `internal/conversations/store.go`: `List` (`:173`), `Rename` (`:204`), `Delete` (`:364`), `SearchConversationTurns` FTS (`:310`); `aura chat {list,new,resume,archive,unarchive,delete,rename,search}` (`main.go:14`). No HTTP route yet → thin REST/SSE adapter needed. |
| FTS conversation search | Spec Frame 01 + capability map "Conversations" | S | **Exists.** `SearchConversationTurns` (FTS migration 0006). Needs an HTTP endpoint. |
| Approval center (ask_user pause/resume) | HITL is a first-class substrate primitive (CORE-02) | M | **Exists (data) + partial (protocol).** `internal/askuser/store.go` (FIFO `paused_states`, accept/decline/cancel `:34-38`); AG-UI already emits `RUN_FINISHED` with `Interrupt` + `ResponseSchema` on a pause (`translator.go:72-79`, `interruptFrom`) and accepts `Resume[]` on `POST /agent/run` (`server.go:197`, `resumeAnswers`). **Needs-building:** the approval-center *display component* + a list-pending HTTP view (`aura paused-states list` exists CLI-side, `main.go:13`). |
| Tool activity stream (start/args/end/result) | Operator must see what the agent ran | S | **Exists.** `TOOL_CALL_*` events (`emitToolInvocation`, `translator.go:303`); ledger in `internal/toolinvocations`. |
| Reasoning drawer (CoT) | Spec Frame 02; substrate streams reasoning | S | **Exists (gated).** `REASONING_*` lifecycle emitted; delta redacted by default (`showReasoning=false`, `server.go:214`; real CoT is a Telegram opt-in). Web needs a deliberate `showReasoning` policy decision. |
| System-event cards (web safety) | Spec Frame 04; SSRF/blocked must be safe, typed | M | **Exists (data) / needs typed emit.** Stable enum `internal/web/errors.go` (`blocked_url`, `unsupported_scheme`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`, `web_search_unavailable`; safe reasons `:24-30`). Today these reach the UI only as tool-result text → **needs GAP-1** to render as `system_event` cards. |
| Run status / daemon health | Spec capability map "Runtime shell" | S | **Exists.** `/healthz` (PG ping + scheduler last-tick, `serve.go:225-237`), `/readyz` (PG + Neo4j probes, `serveReadinessProbes`), `/metrics`, `/debug/vars`. |
| MCP server list + status (read-only) | Governance surface; MCP manager shipped (CAP-09) | M | **Exists (data).** `internal/mcp/manager/status.go` `SnapshotStatus` (trust/runtime/startup/auth, redaction `RedactSecrets`); `aura mcp {recipes,list,status,logs,doctor,tools,...}` (`mcp.go:38-67`). **Needs-building:** HTTP read endpoints + the server-row component. |
| Skills library (active/pending/archived/audit, read-only) | Governance surface; CAP-07 | M | **Exists (data).** `aura skills {list,info,audit,...}` (`skills.go:32`); append-only `aura.skill_audit`. **Needs-building:** HTTP read endpoints + library-tabs component. |
| Scheduler board (tasks/runs/doctor, read-only) | CAP-06 shipped | M | **Exists (data).** `aura task {schedule,list,cancel,run_now,approve,runs,doctor}` (`task.go:33`); `cron.Store`. **Needs-building:** HTTP read endpoints + scheduler-board component. |
| Mobile single-column layout | Spec Frame 05 | M | **Frontend-only.** No backend dependency. |
| Theme/density boot (no flash) | Spec Frame 07 Odysseus pattern | S | **Frontend-only.** Local-storage paint-before-boot. |

### Differentiators (Competitive Advantage)

Where the cockpit earns its "operator-OS" identity. These align with PROJECT.md Core Value (a *visible,
auditable, governed* substrate). Most depend on GAP-1.

| Feature | Value Proposition | Complexity | Backend dependency (evidence) |
|---------|-------------------|------------|-------------------------------|
| Typed display router (web_result / document / code / table / chart / local_artifact) | Turns opaque tool output into inspectable, paginated, raw/source-viewable evidence — the headline Elysia-informed feature | L | **Needs-building (GAP-1).** Requires backend typed-display emit per `web_search`/`web_fetch`/`sandbox_exec` result; today only `aura.artifact` custom event + text preview exist. |
| Neo4j Graph Explorer (node-link canvas, Cypher preview, path strip, node inspector) | Evidence-bearing graph, not a hairball; unique to Aura's graph-memory backbone (INFRA-02) | L | **Needs-building (GAP-1 + new contract).** Graph DATA exists via `internal/knowledge/client.go` (`read_neo4j_cypher`/`write_neo4j_cypher` over `mcp-neo4j-cypher`; APOC `get-schema` per neo4j-mcp-skill); `aura neo4j` CLI. **Gap:** a graph-normalizer that emits the `nodes/edges/paths/schema/query` payload (ux-spec lines 446-454) + the canvas renderer. Read-only Cypher (`NEO4J_READ_ONLY`) is the safe default per the skill. |
| Swarm worker-report table (minimal, no inter-agent chat) | Industrial swarm visibility (CAP-03) without "swarm talk" theater | M | **Exists (data) / needs typed emit.** `swarm.ChildReport` (`internal/swarm/report.go:32`: goal_index, child_id, status, summary, error, question, options); transcripts dumped to `<runDir>/<convID>/swarm/<childID>.jsonl`. **Needs GAP-1** for a `swarm_report` display. |
| MCP governance: doctor/tools/allowlist + redacted env + recipe install | Operator changes what tools the agent has, with provenance + risk visible | L | **Exists (data) + needs mutating HTTP + auth.** Read side per status.go/catalog/policy; `aura mcp doctor/tools` start the server + list tools. **Needs-building:** mutating endpoints (install/add/enable/disable/remove) behind GAP-2 auth + confirmation/approval UI. |
| Skills governance: install/create/update/delete approval gates + risk tiers | Supply-chain-safe self-extension; pending skills never auto-run | L | **Exists (data) + needs mutating HTTP + auth.** Pending under `~/.aura/skills/pending/`, CLI-only activation (`skills.go` "approve" = human-only path the model can't reach); risk tiers via `scoring`. **Needs-building:** approval-queue component + mutating endpoints behind GAP-2. RISKY/DESTRUCTIVE actions need explicit approval UI. |
| Approval center as a dedicated surface (priority, resume-token, accept/decline/cancel) | HITL becomes an operator queue, not a chat bubble | M | **Exists (data).** `askuser.Store` FIFO + priority; protocol `Interrupt`. **Needs-building:** the queue UI + a "list all pending across threads" endpoint. |
| AI `ui_control` events (open_panel, highlight_source, set_mode, focus_query, show_job, set_density, theme_preview) | Agent can safely drive the UI, allowlisted + audited + replayable | L | **Needs-building (GAP-1 + allowlist).** No `ui_control` exists. Requires a new CUSTOM event family + a strict allowlist validator (ux-spec lines 464-475: "never raw CSS/scripts/URLs/unbounded DOM"; events replayable from run log). High abuse risk → must be designed defensively. |
| Operator-OS shell mechanics (adaptive icon rail, dockable tool windows, dock chips, command palette, slash actions, background-job feedback) | The "cockpit" feel; state survives minimize/restore | L | **Mostly frontend.** `background_job` state can ride GAP-1 / existing `STATE_DELTA`; command palette indexes runs/sources/tools client-side. Backend only needs to expose job state (research/compare progress). |
| Source Explorer (Table / Metadata / Configuration views, mapping editor) | Evidence preflight + display-mapping control | M | **Partial.** Source rows come from web/knowledge results; display mappings are a frontend concern. Backend dependency is GAP-1 (typed source payloads). |
| Cache/cost footer + context-budget meter | Shows the KV-cache + cost story (CAP-04) | S | **Exists (data).** `aura cache-stats` (`main.go:77`); `STATE_DELTA` already carries usage/cost (`translator.go:127`, `stateDeltaOps`); context L1/L2/L2.5 rotation in `internal/conversations/context.go`. |
| Full setup wizard (web) | PROJECT.md UX-03; today loopback setup server `:9081` | M | **Exists (partial).** `serve.go` mounts a setup-wizard HTTP server (`setupSrv`, `:9081`). **Needs-building:** richer web onboarding + missing-key states (Phase 14/17 surfaces are labelled "Planned"). |

### Anti-Features (Commonly Requested, Often Problematic)

Captured **verbatim from the design's "Important Non-Goals" (ux-spec lines 521-540)** plus PROJECT.md
out-of-scope. The roadmapper must keep these OUT.

| Feature | Why Requested | Why Problematic | Alternative (from spec) |
|---------|---------------|-----------------|-------------------------|
| Swarm talk / join / mailbox / sibling-chat UI | "Show the agents collaborating" | Theater, not evidence; no backend for it; spec explicitly bans it | Minimal worker-report table only (`swarm.ChildReport`) |
| Generic cards for every payload | Simpler one renderer | Defeats the whole point — typed displays ARE the product | Typed display router (GAP-1) |
| Neo4j rendered as a decorative hairball | "Looks impressive" | Answers no evidence/path question; loses provenance | Filtered evidence paths (20-80 nodes), table fallback, inspector |
| Arbitrary AI-driven frontend automation | "Let the agent do anything in the UI" | Security catastrophe; raw CSS/scripts/DOM mutation | Allowlisted, scoped, logged, reversible `ui_control` only |
| Showing raw saved MCP secrets | "Let me see the value I set" | Secret leak | Redacted chips after save (`RedactSecrets`); required/optional/missing/redacted states |
| Silently mounting destructive MCP tools | "Just give the agent the tools" | Bypasses allowlist; destructive without consent | Explicit denied/destructive policy display (`mcp/manager/policy`) |
| Activating skills directly from a model tool call | "Faster self-extension" | Supply-chain compromise; model gates itself | CLI/`ask_user`-resume human approval only; pending skills can't run/inject |
| Treating `--ignore-scripts` as "safe install" | "We sandboxed it" | Still RISKY supply-chain input | Surface risk tier + validation checklist regardless |
| Exposing raw SearXNG backend params | "Power-user search tuning" | Leaks/abuses the self-hosted search backend | Keep SearXNG params off the operator/model surface |
| Copying Elysia's Weaviate collection model / abstract sphere | "Match the inspiration" | Wrong data model; decorative not operational | Aura trust model = source state + provenance + execution structure |
| Copying Odysseus personal-workspace sprawl | "More features" | Scope creep, off-mission | Adopt dock/window mechanics ONLY where they improve investigation |
| Public skills marketplace / multi-user RBAC in v1 | "Real product needs it" | PROJECT.md out-of-scope; local-first by definition | Web auth this milestone is single-operator session auth, not RBAC |
| Multimodal user input on `/agent/run` | "Send images in chat" | Endpoint rejects it explicitly today (`server.go:33`, `errUnsupportedUserMessageContent`) | Text-only on the AG-UI HTTP endpoint; multimodal stays a channel concern |

---

## Feature Dependencies

```
GAP-2 Web Auth + TLS + static host
    └──gates──> ALL off-loopback exposure
    └──gates──> every MUTATING governance endpoint (MCP/skills/scheduler writes)

GAP-1 Richer AG-UI typed-display protocol  (backend emit + frontend classifier)
    ├──requires──> typed payloads from web/sandbox/swarm/knowledge tool results
    └──enables──> Typed display router
                      ├──enables──> Source Explorer (typed source payloads)
                      ├──enables──> System-event cards (web safety as typed events)
                      ├──enables──> Swarm worker-report display
                      └──enables──> Neo4j Graph Explorer (graph_chunk/path/schema)
    └──enables──> ui_control event family (+ allowlist validator)
                      └──enables──> Operator-OS shell (rail/dock/job feedback driven by events)

Chat stream + Conversation list  (exists, thin HTTP adapter)
    └──independent of GAP-1; ships first as a usable vertical slice

Approval center
    └──requires──> Chat stream (pauses arrive on the run stream)
    └──requires──> "list pending across threads" endpoint (new) + askuser.Store (exists)

MCP governance (write) ──requires──> GAP-2
Skills governance (write) ──requires──> GAP-2
Neo4j Graph Explorer ──requires──> GAP-1 + graph-normalizer contract (new)
```

### Dependency Notes

- **GAP-1 is the spine.** Six of eight frames (Display Workspace, Source Explorer, System Events, Graph
  Explorer, Swarm report, ui_control/operator-OS) cannot render their intended typed experience until the
  backend emits structured display payloads. The chat/reasoning/tool-activity primitives already exist, so
  a *degraded* (text-preview) cockpit is shippable before GAP-1 — but the differentiators are not.
- **GAP-2 gates exposure and all writes.** Read-only governance views can be built on loopback first;
  every *mutating* governance action (install/enable/remove MCP, approve/delete skills, schedule/cancel
  tasks over HTTP) must wait for auth, because exposing them unauthenticated re-introduces exactly the risk
  amendment #35 avoided. This is a deliberate PROJECT.md scope expansion — flag it.
- **Approval center enhances the chat stream**, it does not replace it — pauses already arrive via the
  run-finished-with-interrupt frame; the center is a cross-thread aggregation view.
- **ui_control conflicts with "no arbitrary automation"** unless the allowlist validator ships *with* it.
  Never land `ui_control` emit without its validator + run-log replay in the same phase.

---

## MVP Definition (vertical slices, not all-backend-then-all-frontend)

The downstream consumer asked for vertical slices that ship a usable surface end-to-end. Grouping:

### Launch With (v1) — usable cockpit, loopback, read-mostly

- [ ] **Slice A — Serve foundation + auth + shell.** Static SPA host on `aura serve`, TLS, **GAP-2 web
  auth (session over identity/capability_grants)**, app shell + health/readyz wiring. *Essential: nothing
  is reachable or safe without it.* (M+M, gates everything.)
- [ ] **Slice B — Chat + conversations vertical.** SSE chat stream, conversation list/search/rename/
  archive/delete via thin HTTP adapters over `conversations.Store`, reasoning drawer, tool-activity stream,
  cost/cache footer. *Essential: this is the Core Value loop, and it's almost entirely shipped backend.*
- [ ] **Slice C — Approval center.** Cross-thread pending list + accept/decline/cancel resume over the
  existing `Interrupt`/`Resume[]` protocol. *Essential: HITL is a substrate primitive; chat is incomplete
  without it.*

### Add After Validation (v1.x) — the typed-display differentiators

- [ ] **Slice D — GAP-1 typed-display protocol + display router.** Backend typed emit for web_result /
  document / code / local_artifact + frontend classifier, raw/source views, pagination, citations. *Trigger:
  Slices A-C validate the transport; D is the headline differentiator.*
- [ ] **Slice E — System-events + Source Explorer.** Web-safety enum rendered as typed `system_event`
  cards; Source Explorer table/metadata/config. *Trigger: rides D's classifier.*
- [ ] **Slice F — Neo4j Graph Explorer.** Graph-normalizer contract + canvas + Cypher preview + node
  inspector (read-only Cypher default). *Trigger: D done; this is a large self-contained surface.*
- [ ] **Slice G — Swarm report display.** `swarm_report` typed card over `ChildReport`. *Trigger: D done.*

### Future Consideration (v2+)

- [ ] **Slice H — Governance write surfaces (MCP + Skills + Scheduler mutations).** Mutating endpoints +
  approval/confirmation UI + audit. *Defer: needs GAP-2 hardened; read-only governance views can ship in
  v1.x; writes are higher-risk and benefit from a settled auth model.*
- [ ] **Slice I — ui_control + operator-OS shell.** Allowlisted `ui_control` family + validator + rail/
  dock/command-palette/background-job mechanics. *Defer: highest abuse surface; only valuable once typed
  displays + multiple tool windows exist to control.*
- [ ] **Slice J — Full web setup/onboarding wizard.** Beyond the `:9081` loopback setup. *Defer: overlaps
  Phase 14/17 "Planned" surfaces; sequence after the cockpit core.*
- [ ] **Observability surface (richer).** `/metrics` + `/debug/vars` exist; a built-in dashboard view is
  v2 polish. *Defer.*

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Serve foundation + web auth + TLS + shell (A) | HIGH | MEDIUM | P1 |
| Chat + conversations + reasoning + tool stream (B) | HIGH | LOW (backend exists) | P1 |
| Approval center (C) | HIGH | MEDIUM | P1 |
| GAP-1 typed-display protocol + router (D) | HIGH | HIGH | P1/P2 boundary |
| System events + Source Explorer (E) | MEDIUM | MEDIUM | P2 |
| Neo4j Graph Explorer (F) | HIGH | HIGH | P2 |
| Swarm report display (G) | MEDIUM | LOW (rides D) | P2 |
| MCP/Skills/Scheduler read-only views | MEDIUM | MEDIUM | P2 |
| Governance write surfaces (H) | MEDIUM | HIGH | P3 |
| ui_control + operator-OS shell (I) | MEDIUM | HIGH | P3 |
| Full web setup wizard (J) | MEDIUM | MEDIUM | P3 |

**Priority key:** P1 = must-have for a usable cockpit; P2 = the differentiators; P3 = high-risk/high-cost,
defer until core validated.

---

## Frame → Feature-Group Mapping (coverage check)

| Design frame | Primary feature group | Backend status |
|--------------|-----------------------|----------------|
| 01 Chat + Display Workspace | Chat stream (B) + Typed display router (D) | B exists; D needs-building (GAP-1) |
| 02 Tree View + Reasoning Drawer | Reasoning drawer (B) + decision-tree canvas | Reasoning exists; tree canvas frontend; node-detail needs structured node payload |
| 03 Source Explorer | Source Explorer (E) | Needs GAP-1 typed source payloads |
| 04 System Events | System-event cards (E) | Enum exists (`web/errors.go`); needs GAP-1 typed emit |
| 05 Mobile | Responsive layout | Frontend-only |
| 06 Neo4j Graph Explorer | Graph Explorer (F) | Data exists (knowledge/MCP); needs normalizer + GAP-1 |
| 07 Odysseus Operator-OS | Shell mechanics + ui_control (I) | Mostly frontend; ui_control needs-building (GAP-1 + allowlist) |
| 08 MCP Config + Skills Install | Governance read (P2) + write (H) | Read data exists (manager/skills/cron); writes need GAP-2 |

All 8 frames + all governance surfaces (MCP, skills, scheduler, approval, web-safety) covered.

---

## Competitor / Inspiration Feature Analysis

| Feature | weaviate/elysia-frontend | pewdiepie-archdaemon/odysseus | Aura's approach |
|---------|--------------------------|-------------------------------|-----------------|
| Result rendering | Typed payload router, merged tabs, raw/code views | Chat renderer modules | Adopt typed router (GAP-1); reject Weaviate collection model + abstract sphere |
| Workspace shell | Chat-page mode switch | Adaptive rail, dockable windows, command palette, background jobs | Adopt operator-OS mechanics selectively (Slice I); reject personal-workspace sprawl |
| AI-driven UI | — | UI events | Allowlisted, audited, reversible `ui_control` only |
| Graph | Source tab only | — | Dedicated evidence-bearing Neo4j Graph Explorer (Aura-unique) |
| Governance | — | — | First-class MCP + skills governance with risk tiers + audit (Aura-unique) |

---

## Sources

- `docs/design/aura-deep-search-figma/ux-spec.md` — 8 frames, component inventory, copy contract, display-type mapping, non-goals (truth-source).
- `docs/design/aura-deep-search-figma/BACKEND_CAPABILITY_MAP.md` — capability-group → Figma-surface mapping.
- `docs/design/aura-deep-search-figma/README.md`, `FIGMA_PROJECT.md` — design stance + screen-additions list.
- `.planning/PROJECT.md` — Core Value, Out of Scope (auth/multi-user, marketplace).
- Backend cross-check (existence evidence): `internal/agui/server.go`, `internal/agui/translator.go`,
  `cmd/aura/serve.go`, `internal/agent/event.go`, `internal/web/errors.go`, `internal/mcp/manager/status.go`,
  `cmd/aura/mcp.go`, `internal/knowledge/client.go`, `internal/askuser/store.go`, `cmd/aura/skills.go`,
  `cmd/aura/task.go`, `internal/swarm/report.go`, `internal/conversations/store.go`, `cmd/aura/main.go`.
- `.claude/skills/neo4j-mcp-skill/SKILL.md` — Neo4j MCP tools (get-schema/read-cypher/write-cypher/list-gds-procedures), `NEO4J_READ_ONLY` safe default, APOC requirement for schema introspection.

---
*Feature research for: Aura operator web cockpit ("Aura Deep Search")*
*Researched: 2026-06-15*
