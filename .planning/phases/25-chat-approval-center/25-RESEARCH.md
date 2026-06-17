# Phase 25: Chat + Approval Center - Research

**Researched:** 2026-06-17
**Domain:** Embedded React cockpit chat lane (assistant-ui) over the existing AG-UI/SSE gateway + thin HTTP adapters over `conversations.Store` / `askuser.Store` + cross-thread HITL approval center + conversation branch trees (D-09)
**Confidence:** HIGH (every backend claim grounded in a read file:line; runtime-adapter + npm versions verified this session)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 — Reasoning drawer ON by default.** Flip the cockpit SSE path to `Translate(…, true)` so `REASONING_*` events stream into a collapsible reasoning drawer every turn. Deliberate operator override of the conservative redacted web default (`internal/agui/server.go:214`, `Translate(…, false)`). Drawer collapsible with a persisted show/hide preference. Reasoning *trace* still does not persist verbatim by default (HARDEN-05) — D-01 is the live cockpit stream, not trace persistence.
- **D-02 — Tool activity = name + collapsed raw result.** Live tool-activity stream shows each tool call's name + status + an expandable raw text/JSON result blob. Typed rendering is Phase 26 (DISP-01..05). Phase 25 ships the lightweight raw view only.
- **D-03 — Resolution is inline, in-thread (Claude Code interrupt model).** `ask_user`/HITL interrupt renders as a card in that conversation's message stream; operator answers in place; run resumes over the existing `Resume[]` protocol. No heavyweight separate dashboard.
- **D-04 — Cross-thread awareness via a header badge + lightweight list.** Persistent pending-count badge + a lightweight list lets the operator jump to the inline card. `askuser.Store` has NO cross-thread pending query today (`ListPending` is per-conversation); planner adds a `status='pending'` aggregate read.
- **D-05 — Verb semantics map to Claude Code allow/deny/interrupt.** Answer → `MarkResumed` (accept); Deny → resume with an explicit "declined" answer (agent continues informed of refusal); Cancel run (esc) → abort whole run + auto-resolve (`AutoResolveForConversation`).
- **D-06 — Stale / auto-terminated render explicit terminal state (APRV-03).** Expired/auto-resolved interrupt shows its terminal state inline AND in the badge list — never silently disappears.
- **D-07 — Archive-first; hard-delete behind a confirm dialog.** Primary list action archives (`Store.UpdateStatus`); a separate explicit "Delete permanently" with confirm calls `Store.Delete`.
- **D-08 — FTS search = snippet result list → open thread at match.** `SearchConversationTurns` hits as a panel of matching turns with highlighted snippet + conversation title; click opens that thread scrolled to the match. Uses the `SearchResult` projection as-is.
- **Builder defaults:** inline rename (`Store.Rename`), auto-title from first turn (`Store.SetTitleIfNull`, editable), recent-first sidebar, `Store.List(includeArchived)` drives the list.
- **D-09 — FULL assistant-ui branch trees, built IN Phase 25 as an explicit sub-slice.** Message branching/versioning + edit/regenerate, NOT just linear chat. Requires backend that does not exist today: a schema migration adding parent/branch pointers to `aura.conversation_turns` (next migration slot — verify per PROJECT.md), path-aware history loading (`LoadManagedHistory` walks the *selected* branch path), edit/re-run-from-a-point semantics, and KV-cache stable-prefix care (CAP-04 `messages[0]` invariant). Baseline streaming + a stop/interrupt button (ctx-cancel) are included. Planner records the ROADMAP/REQUIREMENTS amendment FIRST (PRD-first).
- **D-10 — Runtime footer = per-turn + session totals.** Latest turn's tokens + cache-hit % + estimated $, plus a running session cumulative. Estimated $ via OpenRouter's native `cost` field. Cache metrics already persisted (`AppendAssistantTurnWithCacheMetric`). Data source (additive turn-complete SSE signal vs thin REST read of persisted `cache_metrics`) is a planner decision — keep it OFF the Phase-26 `aura.display` namespace.
- **D-11 — Context-budget indicator = fill gauge + microcompact event markers.** Gauge of tokens-in-context vs model window ("42k / 128k · 33%") + a marker when the microcompact ladder fires ("compacted N older turns"). Read-only projection of `LoadManagedHistory` + microcompact-ladder state (Phase 1.8b).
- **D-12 — Lives in the footer alongside cost + cache.** ONE runtime instrument cluster (cost + cache-hit + context-budget).

### Claude's Discretion

- assistant-ui **runtime adapter** choice (`@assistant-ui/react-ag-ui` vs `useExternalStoreRuntime`/`useLocalRuntime`). **RESOLVED in 25-UI-SPEC.md → `useExternalStoreRuntime`.** This research independently confirms that choice (see Standard Stack + Pitfall 1).
- The **cross-thread pending-query** shape on `askuser.Store` (new `status='pending'` aggregate read) + its thin HTTP adapter.
- The **conversation HTTP adapter** surface (`GET/POST /api/conversations…`) — thin REST over `conversations.Store`, under the Phase-24 `/api/` carve-out, behind `RequireAuth`.
- **Footer data source** (turn-complete SSE signal vs REST read of persisted `cache_metrics`) — D-10.
- **Empty/error/loading states**, mobile/responsive behavior, conversation-list navigation chrome — standard, follow Phase 23 tokens.

### Deferred Ideas (OUT OF SCOPE)

- **Typed-display protocol + `switch(payload.type)` router** (web_result/document/code/table/chart/system_event/swarm_report) → **Phase 26**. Phase 25's raw view (D-02) is the placeholder.
- **Neo4j Graph Explorer** → **Phase 27**.
- **Read-only governance boards + web onboarding wizard** → **Phase 28**.
- **Governance WRITE surfaces (MCP config, skills install/approval queue UI)** → **Phase 29** (reuses the SAME `Interrupt`/`Resume[]` protocol built here).
- **`ui_control` operator-OS shell** (dock windows, icon rail, command palette, AI UI-control events) + scheduler write surfaces → follow-up milestone.
- **Estimated-$ pricing beyond OpenRouter's native `cost`** — only if a non-OpenRouter provider becomes primary.
- **Scope note (NOT deferred):** Full conversation branch trees (D-09) are folded INTO Phase 25 by operator decision; the ROADMAP/REQUIREMENTS amendment is recorded first.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CHAT-01 | Send a prompt and watch the streamed assistant response over `POST /agent/run` (SSE) in an assistant-ui chat lane | `useExternalStoreRuntime` maps the existing SSE (RUN_STARTED → TEXT_MESSAGE_* → RUN_FINISHED) onto `ThreadMessage[]`; `/agent/run` + `/threads/{id}/messages` already exist (`server.go:96-97`). Stop = ctx-cancel via `cancelRun()` aborting the fetch (`streamSSE` unwinds on `ctx.Done()`, `server.go:275`). |
| CHAT-02 | Browse, FTS-search, rename, archive, delete conversations (thin HTTP adapters over `conversations.Store`) | Store is COMPLETE: `List(includeArchived)` `store.go:173`, `SearchConversationTurns` `store.go:310`, `Rename` `store.go:204`, `UpdateStatus` (archive) `store.go:187`, `Delete` (hard) `store.go:364`, `SetTitleIfNull` `store.go:221`. New `/api/conversations…` REST adapter behind `RequireAuth`. |
| CHAT-03 | Reasoning drawer (CoT) + live tool-activity stream, governed by explicit `showReasoning` policy | D-01 flips `Translate(…, false)` → `true` at `server.go:214`; the `REASONING_*` lifecycle is already emitted (translator `reasoningRunState`, `translator.go:228`), only the delta text is redacted. Tool activity rides `TOOL_CALL_START/ARGS/END/RESULT` (`translator.go:303`). |
| CHAT-04 | Per-turn cost + cache-hit metrics in a footer | The final Event already carries usage in `Actions.StateDelta` (`usageStateDelta` `llm_agent_events.go:229`: `prompt_tokens`/`completion_tokens`/`cache_hit_tokens`/`cost_usd`) → translator emits it as a `STATE_DELTA` event (`translator.go:127`). The cockpit reads cost/cache off the SSE stream — NO new endpoint required for the live turn. |
| APRV-01 | Cross-thread list of pending `ask_user`/HITL interrupts (question, options, priority, source) | `askuser.Pending` carries Question/Options/Priority/Kind/ConversationID/ProxiedFromChildID (store.go:68). New cross-thread `status='pending'` aggregate query + thin HTTP read. |
| APRV-02 | Accept / decline / cancel a pending interrupt + resume over `Interrupt`/`Resume[]` | Accept/Cancel ride the AG-UI `Resume[]` on `POST /agent/run` (`resumeAnswers` `server.go:360`). **Decline is NOT expressible over AG-UI `Resume[]` today** (the SDK `ResumeStatus` enum has only `resolved`/`cancelled` — see Pitfall 4). Planner must bridge it. |
| APRV-03 | Stale / auto-terminated approvals render their terminal state (no silent loss) | `AutoResolveForConversation` writes the auto-terminated marker (`store.go:297`, content `<auto-terminated: conversation ended>`); `ListRecent` exposes `Resumed`+`ResumedAnswer` for terminal-state rendering (`store.go:193`). |
</phase_requirements>

---

## Summary

Phase 25 is overwhelmingly a **wiring + thin-adapter** phase on top of an already-complete backend, with exactly ONE genuinely novel, high-risk sub-slice: the **D-09 conversation branch trees**, which need a new schema migration, path-aware history loading, re-run-from-a-point semantics, and KV-cache-invariant care. Everything else (chat streaming, reasoning drawer, tool activity, footer cost/cache, conversation management, approval resume) is carried by seams that already exist and are tested.

The chat data plane is the existing AG-UI/SSE transport: `POST /agent/run` (SSE) drives streaming, `GET /threads/{id}/messages` returns the MESSAGES_SNAPSHOT for history rehydration. The reasoning drawer is a single-line backend flip (`Translate(…, false)` → `true` at `server.go:214`) plus a frontend collapsible part renderer — the `REASONING_*` lifecycle frames are *already emitted* regardless; only the delta payload is redacted today. The footer's cost/cache data is **already on the wire**: the agent stamps per-turn usage (`prompt_tokens`/`completion_tokens`/`cache_hit_tokens`/`cost_usd`) into the final Event's `StateDelta`, which the translator emits as a `STATE_DELTA` AG-UI event. The cockpit reads it off the live stream — no new endpoint is needed for the per-turn footer (a thin REST read of persisted `cache_metrics` is the fallback only for session-cumulative on reload).

The runtime-adapter decision (Claude's Discretion, already resolved in the UI-SPEC) is independently confirmed here: **`useExternalStoreRuntime`** is the correct carrier because (a) Aura's SSE is a custom AG-UI event shape, not the AI-SDK Data Stream wire format, and (b) D-09's branch/edit/regenerate + stop affordances are first-class on the external-store runtime (`onCancel`, `setMessages`, `onEdit`, `onReload`, branch navigation). `@assistant-ui/react-ag-ui` exists (v0.0.41) but is young (30k weekly downloads) and would couple Aura to its event-shape assumptions; it is the documented fallback only.

Two backend gaps the planner MUST close before claiming the requirements: (1) **`askuser.Store` has no cross-thread pending read** (D-04 / APRV-01) — `ListPending` is per-conversation; (2) **the AG-UI `Resume[]` path cannot express "decline"** (D-05 / APRV-02) — the SDK `ResumeStatus` enum is only `resolved`/`cancelled`, so the HTTP resume maps to accept/cancel only, while the Runner's internal `SubmitAnswers` supports the full accept/decline/cancel model.

**Primary recommendation:** Build five thin-adapter work items (`/api/conversations…` CRUD, `/api/approvals` cross-thread pending read + resume, footer SSE consumption) + the `Translate(…, true)` flip + the React chat lane on `useExternalStoreRuntime`, and treat **D-09 branch trees as a separate, sequenced, schema-migration sub-slice (migration 0017)** gated behind a REQUIREMENTS amendment (CHAT-05) and the `scripts/cache_invariant_audit.sh` CI gate.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Token streaming / reasoning / tool activity | Backend (AG-UI SSE gateway, `internal/agui`) | Browser (assistant-ui external-store runtime) | The runner produces the event stream; the cockpit only renders it. Inventing a new chat protocol is forbidden (CONTEXT "existing SSE/AG-UI transport is the chat data plane"). |
| Conversation CRUD (list/search/rename/archive/delete) | Backend (`conversations.Store` via thin `/api/` HTTP adapter) | Browser (React Query cache) | Store is complete; the handler is a thin REST shell with no business logic (CONTEXT "thin HTTP adapters over stores"). |
| Cross-thread pending approvals | Backend (`askuser.Store` new aggregate read + `/api/` adapter) | Browser (header badge + popover list) | The pending set is server state; the badge is a projection. Resolution still happens inline (D-03). |
| Approval resume (accept/decline/cancel) | Backend (AG-UI `Resume[]` + a decline bridge) | Browser (inline card verbs) | Resume orchestration is the Runner's job (`SubmitAnswers`); the card only collects the verb + payload. |
| Per-turn cost / cache footer | Backend (already-emitted `STATE_DELTA` usage) | Browser (footer cluster) | Data is on the wire; the footer is pure presentation. Session-cumulative on reload may read persisted `cache_metrics`. |
| Context-budget gauge | Backend (`LoadManagedHistory` + `context_rot_events`) | Browser (fill gauge) | The ladder state is server-owned; the gauge is read-only projection. |
| Branch trees (D-09) | Backend (migration 0017 + path-aware `LoadManagedHistory` + re-run wiring) | Browser (`BranchPickerPrimitive` + external-store branch nav) | The tree topology is durable DB state; the UI navigates it. The KV-cache invariant is a backend concern. |
| Auth / origin gate | Backend (`RequireAuth`, Phase 24) | — | Every Phase-25 route mounts behind the existing whole-origin gate; no new auth this phase. |

---

## Standard Stack

### Core (frontend — net-new this phase)

| Library | Version (verified npm 2026-06-17) | Purpose | Why Standard |
|---------|-----------------------------------|---------|--------------|
| `@assistant-ui/react` | `0.14.22` | Chat primitives + `useExternalStoreRuntime` + `BranchPickerPrimitive` + `ActionBarPrimitive` | The chosen cockpit chat library (PROJECT.md / UI-SPEC); external-store runtime carries custom SSE + branching + stop. React 19 compatible (`react@^18 || ^19`). |
| `@assistant-ui/react-markdown` | `0.14.4` | `MarkdownTextPrimitive` for assistant text parts | Renders streamed markdown answers; the raw tool-result blob stays mono/plain (D-02). |
| `assistant-stream` | `0.3.23` | `PlainTextEncoder/Decoder`, streaming abstractions | Helper for mapping a custom stream into runtime updates; pulled in transitively by `@assistant-ui/react`. |

### Supporting (already in `web/package.json` — reuse, do not re-add)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `@tanstack/react-query` | `^5.101.0` | `/api/conversations` + `/api/approvals` reads, polling the pending badge | Every non-SSE REST read (mirror `useRuntimeHealth.ts`). |
| `react-i18next` + `i18next` | `^17.0.8` / `^26.3.1` | en+it copy bundles | All new copy goes in BOTH `en`+`it` in `web/src/i18n/resources.ts` (memory `reference_cockpit_i18n_react_i18next`). |
| `react-router-dom` | `^7.18.0` | `/c/:conversationId` deep links (open thread at FTS match, D-08) | Conversation routing + the cross-thread "Open" jump. |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `useExternalStoreRuntime` | `@assistant-ui/react-ag-ui` `0.0.41` (`useAgUiRuntime`) | The AG-UI adapter EXISTS and matches Aura's transport name, but it is young (v0.0.x, ~30k weekly downloads vs 1.2M for core), couples Aura to its assumed event-shape mapping, and its branch-tree + stop coverage over a custom server is unproven. **External-store keeps Aura in control of the SSE→message mapping and exposes `onCancel`/`onEdit`/`onReload`/branch-nav as first-class.** Keep `react-ag-ui` as the documented fallback ONLY (UI-SPEC §Runtime adapter). |
| `useExternalStoreRuntime` | `useLocalRuntime` | Local runtime owns its own message state via a `ChatModelAdapter.run()` async generator. Workable for linear streaming, but branch/edit reconstruction is harder to align with the *server-owned* branch topology D-09 introduces; external-store lets Aura drive `setMessages` from the authoritative DB walk. |
| OpenRouter `cost` on SSE | Bespoke pricing table | Already solved: the runner prefers the wire `cost` and falls back to the seeded price table (`runner_persist.go:255`, `llm.CostUSDValue`). The footer reads the same `cost_usd` the persistence layer uses — no second source of truth. |

**Installation:**
```bash
# in web/ — pin exact versions; the Phase-23 supply-chain gate + allowScripts allow-list governs new deps
npm install @assistant-ui/react@0.14.22 @assistant-ui/react-markdown@0.14.4 assistant-stream@0.3.23
```

**Version verification:** Confirmed live via `npm view` on 2026-06-17 (see Package Legitimacy Audit). `@assistant-ui/react` created 2024-05-07, repo `github.com/assistant-ui/assistant-ui`, MIT.

---

## Package Legitimacy Audit

> slopcheck 0.6.1 available this session. **Important footgun:** slopcheck auto-detects ecosystem from project files; run from a dir with `go.mod` present it defaulted to the **Go** registry and false-flagged all four packages `[SLOP]` ("does not exist on go"). Re-run with `--ecosystem npm` → all clean. **The planner/executor MUST pass `--ecosystem npm` (or run inside `web/`).**

| Package | Registry | Age | Downloads/wk | Source Repo | slopcheck (`-e npm`) | Disposition |
|---------|----------|-----|--------------|-------------|----------------------|-------------|
| `@assistant-ui/react` | npm | ~2 yrs (2024-05-07) | 1,232,500 | github.com/assistant-ui/assistant-ui | [OK] | Approved |
| `@assistant-ui/react-markdown` | npm | ~2 yrs (2024-06-26) | 509,415 | github.com/assistant-ui/assistant-ui | [OK] | Approved |
| `assistant-stream` | npm | ~1.7 yrs (2024-09-29) | 1,152,307 | github.com/assistant-ui/assistant-ui | [OK] | Approved |
| `@assistant-ui/react-ag-ui` | npm | young (v0.0.41) | 30,184 | github.com/assistant-ui/assistant-ui | [OK] | Fallback only (not installed unless external-store is rejected) |
| `@assistant-ui/react-data-stream` | npm | mature | 26,138 | github.com/assistant-ui/assistant-ui | [OK] | Not needed (AI-SDK data-stream format — Aura is custom SSE) |

**Packages removed due to slopcheck [SLOP] verdict:** none (the [SLOP] verdicts were a wrong-ecosystem false positive; all clean on npm).
**Packages flagged as suspicious [SUS]:** none. `@assistant-ui/react-ag-ui` is low-download but same-org/same-monorepo as the approved core — not a slopsquat; it is simply a newer adapter the phase does not need.
**Postinstall check:** none of the three required packages declare a network/filesystem `postinstall` (npm view scripts.postinstall empty). The existing `allowScripts` allow-list in `web/package.json` governs script execution.

---

## Architecture Patterns

### System Architecture Diagram

```
                          ┌──────────────────────────────────────────────────┐
  Operator types prompt   │  Embedded React cockpit (internal/webui/dist)    │
  ───────────────────────▶│  AppShell → chatRegion                           │
                          │   ┌──────────────┐  ┌───────────────────────┐    │
                          │   │ ThreadPrim   │  │ Footer cluster        │    │
                          │   │ Composer     │  │ Tokens·Cache·Cost·Ctx │    │
                          │   │ Branch picker│  └───────────▲───────────┘    │
                          │   │ Reasoning dr │              │                │
                          │   │ Tool card    │  ┌───────────┴───────────┐    │
                          │   │ Inline APRV  │  │ Cross-thread badge+list│   │
                          │   └──────┬───────┘  └───────────▲───────────┘    │
                          └──────────┼──────────────────────┼────────────────┘
                                     │ useExternalStoreRuntime│
        ┌────────────────────────────┼──────────────────────┼──────────────────────┐
        │  RequireAuth (Phase 24, whole-origin gate) — every route below            │
        ├────────────────────────────┼──────────────────────┼──────────────────────┤
        │  POST /agent/run (SSE)      │   GET/POST /api/conversations…  /api/approvals
        │  GET /threads/{id}/messages │   (NEW thin adapters, D-04/CHAT-02)         │
        └──────────┬──────────────────┴───────────┬──────────┴─────────┬────────────┘
                   │ handleRun → streamSSE         │                    │
                   │ Translate(…, TRUE ← D-01)     │ conversations.Store │ askuser.Store
                   ▼                               ▼                    ▼ (+ new
        ┌──────────────────────┐      ┌─────────────────────────┐   status='pending'
        │ runner.Turn(ctx,…)   │      │ List/Search/Rename/     │    aggregate read)
        │  → LlmAgent.Run      │      │ UpdateStatus/Delete/    │   ┌─────────────────┐
        │  emits *agent.Event  │      │ LoadManagedHistory      │   │ Insert/MarkResumed
        │  (text/reasoning/    │      │ AppendAssistantTurn-    │   │ AutoResolveForConv
        │   tool/usage+cost)   │      │  WithCacheMetric        │   │ CleanupResumed   │
        └──────────┬───────────┘      └────────────┬────────────┘   └────────┬─────────┘
                   │                               │ aura.conversations       │ aura.paused_states
                   │ ctx-cancel = stop button      │ aura.conversation_turns  │
                   ▼                               │  (+ migration 0017 D-09) ▼
        ┌──────────────────────┐                  ▼                  Postgres aura.*
        │ STATE_DELTA usage →   │      aura.cache_metrics  aura.context_rot_events
        │ footer (cost/cache)   │      (footer fallback)   (microcompact markers, D-11)
        └──────────────────────┘
```

Trace the primary use case: operator types → composer `append` → external-store runtime POSTs `/agent/run` → SSE frames decode into `ThreadMessage[]` parts (text → markdown, reasoning → drawer, tool → raw card, STATE_DELTA usage → footer) → RUN_FINISHED closes the turn; a RUN_FINISHED *with interrupt outcome* surfaces an inline approval card; resolving it re-POSTs `/agent/run` with a `Resume[]` entry.

### Recommended Project Structure

```
internal/
├── agui/
│   ├── server.go            # FLIP server.go:214 Translate(…,false→true) for cockpit (D-01)
│   ├── conversations_api.go # NEW: GET/POST /api/conversations… thin adapters (CHAT-02)
│   └── approvals_api.go     # NEW: GET /api/approvals (cross-thread pending) + resume bridge (D-04/D-05)
├── conversations/
│   ├── store_branch.go      # NEW: branch-pointer writes + path-aware loader (D-09)
│   └── context.go           # EXTEND LoadManagedHistory to walk a selected branch path (D-09)
├── askuser/
│   └── store.go             # ADD ListPendingAll(limit) cross-thread aggregate read (D-04)
└── db/
    ├── migrations/0017_conversation_turn_branches.up.sql  # NEW (D-09)
    └── queries/{paused_states,conversation_turns}.sql      # ADD the two queries

web/src/
├── chat/
│   ├── ExternalStoreChat.tsx   # AssistantRuntimeProvider + useExternalStoreRuntime wiring
│   ├── sseAdapter.ts           # SSE frame → ThreadMessage[] reducer (text/reasoning/tool/usage)
│   ├── Composer.tsx            # ComposerPrimitive + Send↔Stop swap
│   ├── ReasoningDrawer.tsx     # collapsible reasoning part (persisted pref)
│   ├── ToolActivityCard.tsx    # name + status dot + expandable mono raw blob (D-02)
│   ├── BranchPicker.tsx        # BranchPickerPrimitive bound to the tree backend (D-09)
│   └── RuntimeFooter.tsx       # Tokens·Cache·Cost·Context cluster (D-10/D-11/D-12)
├── conversations/
│   ├── ConversationSidebar.tsx # list/rename/archive/delete over /api/conversations
│   ├── SearchPanel.tsx         # SearchConversationTurns snippet rows → open at match (D-08)
│   └── useConversations.ts     # React Query hooks
└── approvals/
    ├── ApprovalBadge.tsx       # header pending-count pill (D-04)
    ├── ApprovalList.tsx        # cross-thread popover list → Open (D-04)
    ├── InlineApprovalCard.tsx  # in-thread card; Answer/Decline/Cancel verbs (D-03/D-05/D-06)
    └── useApprovals.ts         # React Query poll of /api/approvals
```

### Pattern 1: External-store SSE → ThreadMessage reducer
**What:** A pure reducer maps the AG-UI SSE frame sequence onto assistant-ui's `MessagePart[]` model.
**When to use:** The chat lane's data plane (CHAT-01/03/04).
**Frame → part mapping (verified against `translator.go`):**
```
RUN_STARTED                          → start a new assistant message (status: running)
TEXT_MESSAGE_START/CONTENT/END       → { type: "text", text } (markdown rendered)
REASONING_START/MESSAGE_*/END        → { type: "reasoning", text } → drawer (D-01)
TOOL_CALL_START (name) / ARGS        → { type: "tool-call", toolName, argsText } card start
TOOL_CALL_END / RESULT (preview)     → tool-call.result = raw preview (D-02 raw view)
STATE_DELTA {prompt_tokens,          → footer cost/cache (NOT a message part) — read off
   completion_tokens, cache_hit_       the JSONPatch ops at /prompt_tokens etc.
   tokens, cost_usd}
STATE_DELTA {tool_call_id}           → correlation marker for a tool-result preview (Pitfall 2)
RUN_FINISHED (success)               → status: complete
RUN_FINISHED (interrupt outcome)     → status: requires-action → inline approval card
RUN_ERROR (sanitized)                → status: incomplete + error part
```

### Pattern 2: Thin HTTP adapter over a store (the established Aura pattern)
**What:** A handler that parses the request, calls exactly one `Store` method, projects the result to JSON. No business logic.
**When to use:** Every `/api/conversations…` and `/api/approvals` route.
**Precedent:** `serve_adapters.go` shows the composition-root adapter discipline; `handleMessages` (`server.go:220`) is the canonical thin read (resolve 404 → `Store.LoadHistory` → JSON). Mount the new routes the same way `serve_webui.go` mounts the AG-UI prefixes, adding `/api/conversations/` and `/api/approvals` to the parent mux ahead of `/` (they are currently exclusion-only in `fallbackExcludedPrefixes`).

### Pattern 3: Stop button = ctx-cancel
**What:** `ComposerPrimitive.Cancel` → `api.thread().cancelRun()` → the external-store `onCancel` aborts the in-flight `fetch` (`AbortController`). The server's `streamSSE` already unwinds cleanly on client disconnect (`ctx.Done()` at `server.go:275`, goleak-clean per the comment).
**When to use:** D-09 core loop "stop/interrupt the active turn."

### Pattern 4: Approval resume re-POST
**What:** Resolving an inline card re-POSTs `/agent/run` with the SAME `threadId` + a `Resume[]` entry. `resumeAnswers` (`server.go:360`) maps `resolved`→accept (payload as content), `cancelled`→cancel. The run resumes over the rehydrated history (the answer is injected as a RoleTool turn before the fresh agent run).

### Anti-Patterns to Avoid
- **Inventing a new chat protocol / websocket.** The SSE/AG-UI transport is the data plane (CONTEXT). Map onto it; do not replace it.
- **Putting business logic in the `/api/` handlers.** Thin adapters only — the Store owns the logic (CONTEXT "no business logic in the handler").
- **Reading `r.Reply`/the prose to detect a tool result.** The `tool_call_id` StateDelta marker is the disambiguator (Pitfall 2, `translator.go:329`); a raw tool preview overloads `LLMResponse.Content`.
- **Re-emitting the final Event's Content as a CONTENT delta.** The translator treats the final Event as END-only (`translator.go:142`); double-streaming double-prints the answer.
- **Mutating `messages[0]` during branch-path reconstruction (D-09).** The CAP-04 stable-prefix invariant is CI-gated (`scripts/cache_invariant_audit.sh`). See Pitfall 3.
- **A new listener/port.** Single-binary invariant — mount additively on the existing loopback `http.Server`.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Chat message tree / branch navigation UI | A bespoke branch state machine | `@assistant-ui/react` `BranchPickerPrimitive` + external-store branch nav | The runtime models the branch tree; you only supply the DB-walked message list + `setMessages`. |
| Streaming markdown render | A custom markdown streamer | `@assistant-ui/react-markdown` `MarkdownTextPrimitive` | Handles incremental markdown + code blocks safely. |
| SSE parsing | A from-scratch `EventSource` loop | The external-store runtime + a thin frame reducer (`fetch` + `ReadableStream` for POST SSE) | `EventSource` cannot POST a body; use `fetch` streaming (the existing AG-UI client pattern). |
| Cost estimation | A pricing table | OpenRouter `cost` on the SSE `STATE_DELTA` (`usageStateDelta` `llm_agent_events.go:235`) + the seeded `llm.CostUSDValue` fallback | Already the single source of truth in persistence; don't fork it. |
| Cross-thread pending FIFO ordering | App-side sort | A SQL `ORDER BY priority DESC, created_at ASC, token ASC` aggregate (mirror `ListPendingPausedStates` `paused_states.sql:14`) | The total-order tiebreaker (`token ASC`) is mandatory — tx-batched rows share `created_at` (askuser store.go:158 comment). |
| Auth on the new routes | A new auth check | `RequireAuth` wrapping the whole origin (Phase 24, `auth.go:169`) | Already protects every route mounted on the parent mux; the new `/api/` reads inherit it for free. |
| Error redaction on the wire | Re-spell redaction | `sanitizeErr`/`SanitizeString`/`redactEvent` (`server.go:475-516`) | DSN/token redaction belt-and-suspenders already covers RUN_ERROR + 4xx bodies. |

**Key insight:** Phase 25's value is in the *wiring discipline*, not new mechanism. The only place to build genuinely new backend is D-09 (branch tree + path walk + migration), and even there the assistant-ui runtime supplies the entire UI tree model — the new code is the DB schema + deterministic path reconstruction.

---

## Runtime State Inventory

> This is NOT a rename/refactor phase. Skipped — no stored data, live-service config, OS-registered state, secrets, or build artifacts carry a renamed string. The one new durable schema object (migration 0017 branch pointers) is a greenfield additive column on `aura.conversation_turns`, covered under D-09 below, not a migration of existing runtime state.
>
> **One adjacent durable-state note (not a rename):** D-09's migration 0017 adds columns to a table that ALREADY has rows in any live deployment. The migration MUST default the new branch-pointer columns so existing turns form a single canonical linear branch (parent = previous seq, branch_id = a default), or the path walk will see orphaned turns. Verified explicitly — see Pitfall 3 / D-09 detail.

---

## Common Pitfalls

### Pitfall 1: Choosing the wrong runtime (or the AG-UI adapter prematurely)
**What goes wrong:** Reaching for `@assistant-ui/react-ag-ui` because the name matches "AG-UI" — then discovering its event-shape assumptions don't match Aura's custom translator output, or that its branch/stop coverage over a custom server is unproven.
**Why it happens:** Name-matching beats reading the runtime contract.
**How to avoid:** Use `useExternalStoreRuntime` (UI-SPEC §Runtime adapter, confirmed here). It hands Aura the full SSE→message mapping and exposes `onCancel`/`onEdit`/`onReload`/branch nav as the carrier for D-09. Keep `react-ag-ui` as a documented fallback only.
**Warning signs:** A branch-tree or stop affordance that "almost works"; fighting the adapter's assumed wire format.

### Pitfall 2: Mis-rendering a tool-result preview as assistant prose
**What goes wrong:** A tool-result preview overloads `LLMResponse.Content` with no `FinishReason`, identical on the wire to a streamed assistant chunk. Rendered naively it double-prints into the answer.
**Why it happens:** The disambiguator is a `StateDelta["tool_call_id"]` marker, not the content shape (`translator.go:329`, `toolResultCallID`).
**How to avoid:** In the SSE reducer, treat a `TOOL_CALL_RESULT` event (and any `STATE_DELTA` carrying `tool_call_id`) as a tool part, never an assistant text delta. The translator already routes these to `TOOL_CALL_RESULT`; trust the event TYPE, not the content.
**Warning signs:** The raw tool output appears in the answer bubble.

### Pitfall 3: D-09 branch reconstruction poisons the KV-cache stable prefix (CAP-04)
**What goes wrong:** Walking a selected branch path produces a non-deterministic or differently-ordered `messages[0]`, breaking the `scripts/cache_invariant_audit.sh` gate and the OpenRouter prompt-cache discount.
**Why it happens:** `messages[0]` (system L0) and `messages[1]` (always-block) are protected by the ladder (`context.go:236` `applyL1` never touches seq=1; `dropOldestPairs` protects the head). A branch walk that re-orders or rebuilds the head, or that injects a branch-specific system turn, mutates the hashed prefix.
**How to avoid:** (1) The branch path walk must produce a *deterministic* ordered turn list whose head (system + always-block) is byte-identical to the linear case — only the *body* turns differ per branch. (2) `messages[0]` is rebuilt per turn from loader state, NOT from a persisted branch turn, so it is branch-independent by construction (`context.go:38-47` comment) — preserve that. (3) Run `make` cache-invariant gate (22 byte-stable `request NN:` hashes) after wiring the path walk; it is Postgres-free and CI-blocking. (4) The new migration 0017 must default existing rows into ONE canonical branch so `LoadHistory`/`LoadManagedHistory` stay byte-identical for non-branched conversations (the `LoadHistory` byte-identity contract, `store.go:250`).
**Warning signs:** `scripts/cache_invariant_audit.sh` reports "messages[0] mutated at request N"; cache-hit % drops in the footer after a branch switch.

### Pitfall 4: "Decline" is not expressible over the AG-UI `Resume[]` path
**What goes wrong:** D-05 requires Decline to resume with an explicit "declined" answer so the agent continues *informed of the refusal*. But the SDK `types.ResumeStatus` enum has ONLY `resolved` and `cancelled` (verified in the vendored SDK `core/types/types.go:302-309`). The server's `resumeAnswers` maps `resolved`→`ActionAccept`, `cancelled`→`ActionCancel` (`server.go:360-370`) — **there is no path to `ActionDecline` over `POST /agent/run`**. The Runner's internal `SubmitAnswers` DOES support decline (`runner_resume.go:153`, `declinedContent = "user declined to answer"`), but the HTTP gateway can't reach it.
**Why it happens:** The AG-UI protocol's resume model is two-state; Aura's HITL model is three-state (accept/decline/cancel, `askuser/store.go:34`).
**How to avoid — planner decision (pick ONE, document it):**
- **Option A (recommended): a thin `/api/approvals/{token}/resolve` adapter** that calls `Runner.SubmitAnswers` directly with `{Action: "decline"}` (full three-action model), bypassing the AG-UI `Resume[]` two-state limitation. Accept/cancel can still ride `/agent/run` Resume[] (to also re-drive the turn), OR all three go through the new adapter and the resumed run is re-triggered by a follow-up `/agent/run` with no Resume (continue-after-resume, `Turn(…, userMsg=nil)` `runner.go:354`). This keeps the decline semantics intact and is a thin adapter consistent with the phase pattern.
- **Option B: a payload convention on `resolved`** — send `status:"resolved"` with a sentinel payload the server's `resumeAnswers` recognizes as decline and maps to `ActionDecline`. Lighter wire change but overloads the protocol; less clean.
**Warning signs:** A "Decline" button that actually injects the operator's text as an accepted answer (mapping to `ActionAccept`), so the agent treats the refusal as the answer.

### Pitfall 5: CONCURRENTLY index in a multi-statement migration
**What goes wrong:** If D-09's migration 0017 adds an index on the new branch column with `CREATE INDEX CONCURRENTLY`, it cannot run inside golang-migrate's implicit transaction block (the same constraint that forced the 0005/0006 split, `0006_conversation_turns_fts.up.sql:2`).
**Why it happens:** golang-migrate v4 wraps a multi-statement migration in one tx; `CONCURRENTLY` is forbidden in a tx.
**How to avoid:** On a table with existing rows, a plain `CREATE INDEX` in the multi-statement migration is fine for a fresh/small table; if the conversation_turns table is large in production, split the concurrent index into its OWN single-statement migration file (the 0006 precedent). For a default-backfill of branch columns, an `ALTER TABLE … ADD COLUMN … DEFAULT …` is tx-safe.
**Warning signs:** `pq: CREATE INDEX CONCURRENTLY cannot run inside a transaction block` on migrate up.

### Pitfall 6: Mounting `/api/` on the mux collides with the integrations subtree
**What goes wrong:** Registering `mux.Handle("/api/", …)` shadows the already-mounted `/api/integrations/` proxy (`serve_webui.go:21-25` warns explicitly).
**Why it happens:** `/api/` is an EXCLUSION prefix only today, never a mux registration.
**How to avoid:** Register the SPECIFIC new subtrees — `mux.Handle("/api/conversations/", …)` and `mux.Handle("/api/approvals", …)` — NOT a bare `/api/`. They are already covered by the `/api/` SPA-fallback exclusion, so no `fallbackExcludedPrefixes` change is needed (it already returns `/api/`). Add the routes to `aguiRoutePrefixes` (or a new prefix list) so Go 1.22 longest-pattern precedence keeps them authoritative over `/`.
**Warning signs:** `/api/integrations/...` starts 404ing or hitting the wrong handler after the new routes land.

---

## Code Examples

### Conversation HTTP adapter (CHAT-02) — thin read, mirrors handleMessages
```go
// Source: pattern from internal/agui/server.go:220 (handleMessages) + store.go methods
// GET /api/conversations → list; behind RequireAuth (Phase 24, no new auth)
func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
    includeArchived := r.URL.Query().Get("archived") == "true"
    convs, err := s.conv.List(r.Context(), includeArchived) // store.go:173
    if err != nil {
        http.Error(w, sanitizeErr(err), http.StatusInternalServerError) // server.go:475
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(convs)
}
```
> The `ConversationStore` interface the server consumes today is narrower than `*conversations.Store` — the planner widens the consumer-side interface (server.go declares it) to add `List`/`SearchConversationTurns`/`UpdateStatus`/`Rename`/`Delete`, keeping "accept interfaces, return structs" (D-A2-02). `*conversations.Store` satisfies it implicitly.

### Cross-thread pending read (D-04 / APRV-01) — new aggregate query
```sql
-- Source: new query, mirrors ListPendingPausedStates (paused_states.sql:14) but
-- WITHOUT the per-conversation WHERE; the total order tiebreaker is mandatory.
-- name: ListAllPendingPausedStates :many
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer
FROM aura.paused_states
WHERE resumed_at IS NULL
ORDER BY priority DESC, created_at ASC, token ASC
LIMIT $1;
```
```go
// askuser/store.go — new method beside ListPending (store.go:161)
func (s *Store) ListPendingAll(ctx context.Context, limit int) ([]Pending, error) {
    if limit <= 0 { limit = 100 }
    rows, err := s.q.ListAllPendingPausedStates(ctx, int32(limit))
    if err != nil { return nil, fmt.Errorf("list all pending: %w", err) }
    out := make([]Pending, 0, len(rows))
    for _, r := range rows { out = append(out, fromRow(r)) } // store.go:327
    return out, nil
}
```
> The existing partial index `paused_states_pending_idx ON (conversation_id, resumed_at) WHERE resumed_at IS NULL` (`0003_paused_states.up.sql:30`) is keyed conversation-first, so it does NOT optimally serve a cross-thread `WHERE resumed_at IS NULL ORDER BY priority DESC`. For a single operator the pending set is tiny; the planner may add a `(resumed_at, priority DESC) WHERE resumed_at IS NULL` index if needed, but it is likely premature.

### Footer cost/cache from the SSE STATE_DELTA (D-10 / CHAT-04)
```typescript
// Source: usageStateDelta (internal/agent/llm_agent_events.go:229) emitted as a
// STATE_DELTA JSONPatch by the translator (translator.go:127/341).
// The final Event's StateDelta is rendered as JSONPatch ops: replace /prompt_tokens etc.
interface TurnUsage { promptTokens: number; completionTokens: number; cacheHitTokens: number; costUsd?: number; }
function usageFromStateDelta(ops: Array<{op: string; path: string; value: unknown}>): Partial<TurnUsage> {
  const byPath = Object.fromEntries(ops.map(o => [o.path, o.value]));
  return {
    promptTokens: Number(byPath['/prompt_tokens'] ?? 0),
    completionTokens: Number(byPath['/completion_tokens'] ?? 0),
    cacheHitTokens: Number(byPath['/cache_hit_tokens'] ?? 0),
    costUsd: byPath['/cost_usd'] !== undefined ? Number(byPath['/cost_usd']) : undefined,
  };
}
// cache-hit % = cacheHitTokens / promptTokens (guard /0 at the presentation boundary,
// matching cachemetrics.Aggregate's "ratio left to the caller" comment, store.go:79).
```

### Context-budget gauge data source (D-11)
The gauge reads tokens-in-context vs window. Two sources, both server-owned:
- **Live (preferred):** the same `STATE_DELTA` usage carries `prompt_tokens` per turn = the current context size; the model window is a constant the cockpit knows (or a thin `/api/runtime` read of `llm.Config.ContextWindow`).
- **Compaction markers:** the L2.5 ladder writes one `aura.context_rot_events` row per pair-drop (`context.go:117`, action `hard_drop_pairs`, with `pairs_dropped`/`tokens_before`/`tokens_after`). Surface a "Compacted N older turns" marker either via a thin `/api/conversations/{id}/rot-events` read OR an additive SSE custom event. The marker data already exists; no new computation.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `EventSource` for SSE | `fetch` + `ReadableStream` (POST SSE needs a body) | n/a (always) | `/agent/run` is a POST with a JSON body; `EventSource` can't carry it. Use streaming fetch, as the existing AG-UI client does. |
| AI-SDK Data Stream wire format | Custom AG-UI event stream + external-store runtime | this phase | Aura's SSE is AG-UI events, not the AI-SDK data-stream protocol, so `useChatRuntime`/`@assistant-ui/react-ai-sdk` do NOT fit. |
| Bespoke pricing tables | Provider-native `cost` field (OpenRouter) | Phase 6 (D-18) | Footer reads `cost_usd` already on the wire; fallback to seeded price table only for non-OpenRouter. |

**Deprecated/outdated:**
- `useChatRuntime` (AI-SDK) — wrong wire format for Aura; do not use.
- Reading the model reply text to detect tool results — superseded by the `tool_call_id` StateDelta marker (Pitfall 2).

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `@assistant-ui/react` 0.14.x's `useExternalStoreRuntime` exposes `onCancel`/`onEdit`/`onReload` + branch navigation as documented in the architecture reference (skill) and llms.txt | Standard Stack / Pitfall 1 | If a method's exact name/shape differs in 0.14.22, the planner consults `assistant-ui.com/llms.txt` (the SKILL.md mandates this) at plan time; mapping intent is unchanged. The runtime API surface in the skill reference (`thread.append`, `thread.cancelRun`, `message.edit/reload`) is stable across 0.x. |
| A2 | The `STATE_DELTA` usage event arrives ONCE per turn at finalize (not per chunk) | Code Examples (footer) | Confirmed: usage is stamped only on `finalEvent`/`finalizeEvent` (`llm_agent_events.go:215`, `llm_agent_finalize.go:118`), not on chunk events. Low risk. |
| A3 | Existing `conversation_turns` rows can be defaulted into one canonical branch by migration 0017 without breaking `LoadHistory` byte-identity | Pitfall 3 / D-09 | If the default scheme is wrong, non-branched conversations could change their loaded history. Mitigated by the `LoadHistory` byte-identity test (`store.go:250`) + the cache-invariant gate. |
| A4 | A single-operator deployment's pending-approval set is small enough that the cross-thread `ListPendingAll` needs no new index | Don't Hand-Roll / Code Examples | If background/scheduled threads accumulate many pendings, add a `(resumed_at, priority DESC) WHERE resumed_at IS NULL` index. Low risk for v1.0.0 single operator. |

**Note:** A1 is the only `[ASSUMED]`-tagged claim about an external API; it is bounded by the SKILL.md directive to consult llms.txt for the latest API at implementation time. All backend claims are `[VERIFIED: codebase]` against the read file:line.

---

## Open Questions

1. **Decline-over-HTTP bridge (D-05)** — Option A (new `/api/approvals/{token}/resolve` calling `Runner.SubmitAnswers` with full three actions) vs Option B (payload convention on AG-UI `resolved`).
   - What we know: The Runner supports decline internally (`runner_resume.go:153`); the AG-UI `Resume[]` path does not (`ResumeStatus` is two-state).
   - What's unclear: Whether the planner wants ALL resume verbs to flow through one new adapter (cleaner, one resume path) or keep accept/cancel on `/agent/run` Resume[] and only decline on the new adapter.
   - Recommendation: **Option A, all three verbs through `/api/approvals/{token}/resolve`** for a single, testable resume surface; re-drive the run with a no-Resume `/agent/run` (continue-after-resume). Keeps the AG-UI Resume[] path untouched and the three-action model intact.

2. **Footer session-cumulative on reload (D-10)** — the per-turn footer reads the live SSE; the *session cumulative* on a fresh page load needs the persisted total.
   - What we know: `aura.conversations` already aggregates `total_input_tokens`/`total_output_tokens`/`total_cached_tokens`/`total_cost_usd` per thread (`0005_conversations.up.sql:16-19`), exposed on the `Conversation` projection (`store.go:104-107`).
   - Recommendation: The `/api/conversations/{id}` GET returns those aggregates → the footer seeds session-cumulative from them, then adds live SSE deltas. No new table or query — the data is already on the conversation row.

3. **D-09 edit/regenerate persistence semantics** — when the operator edits a user turn or regenerates an assistant turn, does the old branch stay queryable (full tree) or is it a soft-overwrite?
   - What we know: assistant-ui's branch model keeps all branches (architecture ref §Branching Model); the operator chose "FULL branch trees" (D-09).
   - Recommendation: Persist every branch (full tree). The migration's parent/branch pointers must support N siblings under one parent. The `LoadManagedHistory` walk takes a *selected leaf* and reconstructs the root→leaf path. This is the highest-design-effort item — flag as its own plan/wave.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| npm registry (assistant-ui pkgs) | Chat lane | ✓ | see audit | — |
| Node 24 (`web/` build) | Embedded dist rebuild | ✓ (package.json engines `>=24.16.0 <25`) | — | — |
| Postgres (`aura.*`) | conversation/approval stores + migration 0017 | ✓ (live stack) | 15.x | — |
| `slopcheck` | Package legitimacy gate | ✓ | 0.6.1 | mark `[ASSUMED]` (not needed — ran clean) |
| OpenRouter `cost` field | Footer $ (D-10) | ✓ (provider feature, verified Phase 6 D-18) | — | seeded `llm` price table (`runner_persist.go:255`) |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** OpenRouter `cost` → seeded price table (already wired).

---

## Validation Architecture

> `workflow.nyquist_validation: true` in `.planning/config.json` — this section is REQUIRED.

### Test Framework
| Property | Value |
|----------|-------|
| Backend framework | Go `testing` + table-driven + `testify` (where adopted) + `goleak` + `-race`; build tags `db_integration`/`neo4j_integration` for live tiers |
| Backend quick run | `go test ./internal/agui/ ./internal/conversations/ ./internal/askuser/` |
| Backend full (DB) | `go test -tags 'db_integration neo4j_integration' ./internal/...` (derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`; stack up) |
| Frontend framework | Vitest 4 + @testing-library/react 16 + jsdom; Playwright for E2E; Stryker 9 for mutation |
| Frontend quick run | `cd web && npm run test` (vitest run --coverage) |
| Frontend mutation | `cd web && npm run mutation` (stryker, ≥70% killed) |
| Cache-invariant gate | `scripts/cache_invariant_audit.sh` (Postgres-free; 22 byte-stable hashes) — D-09 gate |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CHAT-01 | SSE frames map to ThreadMessage parts; stop aborts the fetch | Vitest unit (reducer) + Playwright E2E (stream a real turn) | `cd web && npm run test -- sseAdapter` | ❌ Wave 0 |
| CHAT-01 | `Translate(…, true)` streams real reasoning deltas (D-01) | Go unit (translator golden) | `go test ./internal/agui/ -run TestTranslate` | ✅ extend `translator_reasoning_test.go` |
| CHAT-02 | `/api/conversations` list/search/rename/archive/delete map to the right Store method | Go integration (`db_integration`) | `go test -tags db_integration ./internal/agui/ -run TestConversationsAPI` | ❌ Wave 0 |
| CHAT-02 | Sidebar renders list, archive/delete confirm, FTS panel opens at match | Vitest + Testing-Library | `cd web && npm run test -- ConversationSidebar SearchPanel` | ❌ Wave 0 |
| CHAT-03 | Reasoning drawer collapse + persisted pref; tool card raw expand | Vitest | `cd web && npm run test -- ReasoningDrawer ToolActivityCard` | ❌ Wave 0 |
| CHAT-04 | Footer reads cost/cache off STATE_DELTA; cache-% /0 guard | Vitest unit | `cd web && npm run test -- RuntimeFooter usageFromStateDelta` | ❌ Wave 0 |
| APRV-01 | `ListPendingAll` returns cross-thread pendings in total order | Go integration | `go test -tags db_integration ./internal/askuser/ -run TestListPendingAll` | ❌ Wave 0 |
| APRV-02 | accept/decline/cancel resume map to the right `ResponseInput.Action` | Go integration (Runner) | `go test -tags db_integration ./internal/runner/ -run TestResolve` | ✅ extend `runner_resume*_test.go` |
| APRV-02 | Inline card verbs + the decline bridge | Vitest + Go adapter test | `cd web && npm run test -- InlineApprovalCard` | ❌ Wave 0 |
| APRV-03 | Auto-terminated/expired renders terminal state | Go integration + Vitest | `go test -tags db_integration ./internal/askuser/ -run TestAutoResolve` + `npm run test -- ApprovalList` | ✅ (Go) / ❌ (web) |
| D-09 | Branch path walk keeps `messages[0]` byte-stable | Go cache-invariant gate | `bash scripts/cache_invariant_audit.sh` | ✅ (gate exists; D-09 must keep it green) |
| D-09 | Path-aware `LoadManagedHistory` reconstructs root→leaf | Go unit + integration | `go test ./internal/conversations/ -run TestBranchPath` | ❌ Wave 0 |
| D-09 | migration 0017 up/down + existing-row default backfill | Go DB migration test | `go test -tags db_integration ./internal/db/ -run TestMigrate` | ✅ extend migration tests |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/<pkg>/ -race` for touched Go packages; `cd web && npm run lint && npm run typecheck && npm run test` for touched web.
- **Per wave merge:** full `go test -tags 'db_integration neo4j_integration' ./internal/...` on the live stack + `cd web && npm run test` (coverage ≥85%) + `bash scripts/cache_invariant_audit.sh` (after any D-09 touch).
- **Phase gate:** full suite green + `npm run mutation` (≥70% killed, frontend per memory `feedback_frontend_quality_gates_coverage_mutation`) + Go mutation spot-check (≥70%) on the new branch-path loader + cross-thread query + the `/api/` adapters, documented in the phase VALIDATION.md.

### Wave 0 Gaps
- [ ] `internal/agui/conversations_api_test.go` — covers CHAT-02 (`db_integration`)
- [ ] `internal/agui/approvals_api_test.go` — covers APRV-01/02/03 + the decline bridge
- [ ] `internal/askuser/store_test.go` (extend) — `ListPendingAll` (APRV-01)
- [ ] `internal/conversations/store_branch_test.go` — branch path walk + byte-identity (D-09)
- [ ] `internal/db/migrations/0017_conversation_turn_branches.{up,down}.sql` + migration test
- [ ] `web/src/chat/__tests__/sseAdapter.test.ts` — SSE frame → parts reducer (CHAT-01/03/04)
- [ ] `web/src/chat/__tests__/RuntimeFooter.test.tsx` — usage parse + /0 guard (CHAT-04)
- [ ] `web/src/conversations/__tests__/ConversationSidebar.test.tsx` — list/archive/delete/rename
- [ ] `web/src/approvals/__tests__/InlineApprovalCard.test.tsx` — verbs + terminal states
- [ ] `web/e2e/chat.spec.ts` — Playwright: type prompt → stream → resolve inline card

**No-skip-as-green discipline:** integration tiers `t.Fatal` under `$CI` when their env is unset (CLAUDE.md); a sub-second "integration" run is a skip tell. The cache-invariant gate hard-fails on an empty/short hash stream. Frontend coverage gate ≥85% blocks CI (Stryker ≥70%). Realistic fixtures only — the SSE reducer test uses captured real translator output (golden frames from `internal/agui/testdata/golden-events.json`), not synthetic shapes.

---

## Security Domain

> `security_enforcement` absent in config → treated as enabled. Phase 25 adds HTTP read/resume surfaces + a migration; the threat surface is narrow because every route inherits the Phase-24 whole-origin gate.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (inherited) | `RequireAuth` whole-origin gate (Phase 24, `auth.go:169`); no new auth — new `/api/` routes mount behind it. |
| V3 Session Management | yes (inherited) | HMAC-signed HttpOnly+Secure+SameSite=Strict cookie (Phase 24, `auth_cookie.go`); same-origin fetch with `credentials: 'same-origin'` (existing pattern, `useRuntimeHealth.ts:31`). |
| V4 Access Control | yes | `RequireCapability` on `POST /agent/run` (`auth.go:222`). The new `/api/` reads are read-only; resume (`/api/approvals/.../resolve`) is mutating — gate it with the same `capability_grants` check (or document why a read-only operator surface needs no extra capability). |
| V5 Input Validation | yes | Parse + validate path/body: conversation ids via `uuid.Parse` BEFORE the store round-trip (mirror `server.go:167/226` — a malformed id is a clean 404, never a 500); body size-cap like `maxRunBodyBytes`/`maxLoginBodyBytes`. |
| V6 Cryptography | no (no new crypto) | Reuses the Phase-24 HMAC cookie; never hand-roll. |
| V7 Error Handling / Logging | yes | `sanitizeErr`/`SanitizeString`/`redactEvent` redact DSN/token/key from RUN_ERROR + 4xx bodies (`server.go:475-516`). The cross-thread pending list MUST sanitize the question text if it could echo a secret — apply `SanitizeString` to surfaced `question`/`resumed_answer` if there's any chance of credential leakage. |

### Known Threat Patterns for {React SPA + Go single-binary HTTP gateway + Postgres}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SQL injection on FTS query / new aggregate | Tampering | Parameterized sqlc queries only; the FTS query is the LOCKED `content % $1` contract (`conversation_turns.sql:33`) — never rewrite/interpolate. The new `ListAllPendingPausedStates` binds `$1` limit. |
| Path/UUID confusion → 500 leak | Information Disclosure | `uuid.Parse` the id before the store call → clean 404 (`server.go:167`). |
| XSS via streamed assistant text or tool raw blob | Tampering/Injection | The raw tool blob (D-02) renders as text/mono, NEVER `dangerouslySetInnerHTML`; markdown goes through `MarkdownTextPrimitive` (sanitized). Untrusted tool/swarm output is default-untrusted (HARDEN-08). |
| CSRF on the resume mutation | Spoofing | SameSite=Strict cookie (Phase 24 posture, `auth.go:14-18`); same-origin SPA, no cross-origin write path. Re-evaluate only if a cross-origin surface is added (it is not here). |
| Reasoning CoT leak (D-01 flips redaction on) | Information Disclosure | Justified: whole-origin-private single operator (Phase 24 D-03) is the only viewer; the trace still does NOT persist verbatim by default (HARDEN-05). The drawer is a live stream, not durable history. |
| DoS via giant SSE/raw blob | Denial of Service | Existing `maxRunBodyBytes` body cap + the SSE pump's drop-on-full backpressure (`server.go:301`); tool-result spillover already capped (sidecar). |
| Pending-list enumeration across threads | Information Disclosure | Single-operator deployment; the operator owns all threads. No multi-tenant scoping needed this milestone (RBAC explicitly out of scope). |

---

## Sources

### Primary (HIGH confidence — read this session)
- `internal/agui/server.go` — handleRun/handleMessages/streamSSE/Translate(…,false→true at :214)/maxRunBodyBytes/sanitizeErr/redactEvent + the `Resume[]`→`resumeAnswers` map (:360)
- `internal/agui/translator.go` — full SSE event sequence: REASONING_* lifecycle, TOOL_CALL_*, STATE_DELTA (usage/cost), tool_call_id marker, interrupt outcome
- `internal/agui/auth.go` — RequireAuth/RequireCapability/whole-origin gate (Phase 24)
- `internal/conversations/store.go` — List/Get/UpdateStatus/Rename/SetTitleIfNull/CountTurns/LoadHistory/SearchConversationTurns/Delete + the LoadHistory byte-identity contract
- `internal/conversations/context.go` — LoadManagedHistory + the L1/L2/L2.5 microcompact ladder + the messages[0]/messages[1] protection (D-11 + Pitfall 3)
- `internal/conversations/store_append.go` — AppendTurn + AppendAssistantTurnWithCacheMetric (D-10 cache-metric source)
- `internal/askuser/store.go` — Insert/GetByToken/ListPending(per-conv)/ListRecent/MarkResumed/MarkResumedBatch/AutoResolveForConversation/CleanupResumedOlderThan + the accept/decline/cancel three-action model
- `internal/agent/llm_agent_events.go` — finalEvent + usageStateDelta (cost_usd/cache_hit_tokens) — the footer SSE source
- `internal/runner/runner_persist.go` + `runner_resume.go` — usageFromStateDelta, cost precedence, SubmitAnswers, declinedContent, cancelConversation
- `internal/db/migrations/0003/0005/0006/0007*.up.sql` — paused_states / conversations / conversation_turns / cache_metrics schema; latest migration is 0016 → D-09 lands at **0017**
- `internal/db/queries/{paused_states,conversation_turns,cache_metrics}.sql` — the sqlc query contracts
- `internal/webui/embed.go` + `cmd/aura/serve_webui.go` — embed host, `/api/` carve-out, route precedence, the `/api/` mux-collision warning
- `web/package.json` + `web/src/{AppShell.tsx,i18n/resources.ts,health/useRuntimeHealth.ts,health/RuntimeHealthPanel.tsx}` — the React Query/i18n/token patterns to mirror
- `scripts/cache_invariant_audit.sh` — the CAP-04 messages[0]/[1] stable-prefix CI gate (D-09 risk)
- vendored SDK `…/ag-ui/sdks/community/go/pkg/core/types/types.go:253-323` — Interrupt + ResumeEntry + ResumeStatus (the two-state `resolved`/`cancelled` enum → the decline gap, Pitfall 4)
- `.claude/skills/assistant-ui/{SKILL.md,references/architecture.md,references/packages.md}` — runtime selection, external-store API surface, branch model, package list
- npm registry (`npm view`, downloads API) — versions + ages + downloads (2026-06-17)

### Secondary (MEDIUM confidence)
- slopcheck 0.6.1 (`--ecosystem npm`) — package legitimacy [OK] (after correcting the auto-detected Go-ecosystem false positive)

### Tertiary (LOW confidence)
- A1 (external-store runtime method names exact in 0.14.22) — bounded by the SKILL.md directive to consult assistant-ui.com/llms.txt at implementation time.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions verified via npm; runtime adapter confirmed against the skill + the actual SSE shape.
- Architecture: HIGH — every seam read at file:line; the SSE→part mapping verified against `translator.go`.
- Pitfalls: HIGH — the decline gap (Pitfall 4) and the cache-invariant gate (Pitfall 3) are both verified against source, not inferred.
- D-09 design: MEDIUM — the *shape* is clear (migration 0017 + path walk + invariant gate), but the exact column model + edit/regenerate persistence is a design item the planner sequences (Open Questions 3).

**Research date:** 2026-06-17
**Valid until:** ~2026-07-17 (assistant-ui is fast-moving; re-verify versions + the external-store API against llms.txt at plan time. Backend claims are stable until the next conversations/askuser refactor.)
