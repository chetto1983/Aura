# Cockpit Compact Chat UI — Reasoning Pill + Tool Card Disclosure Spec

**Date:** 2026-07-23
**Status:** DESIGN — implementation-ready. No code has been touched; this document is the
contract an implementer builds from.
**Scope:** `web/src/chat` (presentation + SSE reducer additive fields), `web/src/i18n`,
`web/src/styles/motion.css`. Backend untouched except where Amendment #91 already lands the
snapshot `reasoning` field (this spec consumes it; it does not implement it).
**Supersedes (explicitly):** two clauses of
`docs/superpowers/specs/2026-07-15-aura-calm-prism-chat-refinement-design.md` —
§6.2's frozen `aura.chat.reasoning.shown` preference contract and §6.3's tool "disclosure
state matrix" items 1–3 (auto-expand while running / auto-collapse on settle). Everything
else in Calm Prism (approval placement §6.4, color roles §6.7, footer §6.5) remains law and
this spec is designed inside it.
**Compatible with (in-flight):** SSE resume/detach design
(`docs/audit/sse-resume-design-1.3b-2026-07-23.md` §4 — parts are rebuilt from full-buffer
replay through the same reducer) and PRD Amendment #91 (reasoning persisted per assistant
turn, rehydrated into the snapshot, **no duration data**).

---

## 1. Problem statement + evidence (the space audit)

### 1.1 Operator problem (verbatim intent)

Tool cards and reasoning blocks take far too much vertical space. Live-deployment
screenshots show (a) a search tool card rendering a huge raw JSON payload inline
(`{"results":[{"title":…,"url":…,"content":…}` — dozens to hundreds of lines), and (b)
reasoning rendered as a full-width, fully-expanded block spanning multiple screens. A short
assistant answer ends up buried between walls of machinery. The operator wants it "come
Ollama": thinking collapsed into a one-line affordance ("Thought for 13.9 seconds") that
expands on demand, and compact tool activity.

### 1.2 Root causes in current code

| # | Cause | Evidence |
|---|---|---|
| C1 | The raw tool `<pre>` has **no vertical cap** — only `overflow-x-auto`. An expanded result renders at full height inline. | `web/src/chat/ToolActivityCard.tsx:139-147` |
| C2 | The tool card **auto-expands while running** (streamed args shown expanded), and re-expands when raw first arrives. | `web/src/chat/ToolActivityCard.tsx:81` (`useState(status === 'running' && hasRaw)`), `:86-93` (delayedRaw auto-expand) |
| C3 | The reasoning drawer's expand preference is a **global sticky localStorage flag**: one "Show reasoning" click makes every reasoning block in every conversation render fully expanded forever (including live streaming, where the wall grows in front of the operator). | `web/src/chat/ReasoningDrawer.tsx:21` (`useState(readReasoningPref)`), `web/src/chat/reasoningPref.ts:5-15` |
| C4 | The reasoning body is **unbounded** — `whitespace-pre-wrap` with no max-height. | `web/src/chat/ReasoningDrawer.tsx:49-55` |
| C5 | The collapsed reasoning affordance carries **no information** — a generic "Show reasoning" button, no duration, no streaming state beyond a body placeholder. | `web/src/chat/ReasoningDrawer.tsx:47`, `web/src/i18n/resources.ts:109-113` |
| C6 | Typed display cards (e.g. `WebResultDisplay`, 3 result rows/page + chrome ≈ 400px) always render at full height — there is no collapsed state at the tool-part level. | `web/src/chat/ExternalStoreChat_messages.tsx:340-354`, `web/src/chat/displays/WebResultDisplay.tsx:155-171`, `web/src/chat/displays/DisplayPagination.tsx:20-33` |
| C7 | Consecutive tool calls (deep-research runs fire 5–15 sequential searches) each render a full-width bordered card; there is no grouping. | `web/src/chat/ExternalStoreChat_messages.tsx:145-147` (one `ToolFallback` per part) |

### 1.3 Vertical-cost estimate (current vs proposed)

Line height of machinery text: Tailwind `text-xs` = 12px × `leading-relaxed` 1.625 ≈
**19.5px/line**. Reference viewport 900px tall.

| Scenario | Current | Proposed (collapsed default) |
|---|---|---|
| (a) Search tool, raw JSON result expanded (~25 results × 5–10 wrapped lines ≈ 150–250 lines) | ≈ 2,900–4,900px ≈ **3–5.5 screens** | **one 44px row**; expanded: ≤ 320px scroll container (≥ 89% cheaper even expanded) |
| (a′) Same tool while running (streamed args auto-expanded, C2) | args block grows live, ~100–400px churn | 44px row, summary text updates in place |
| (b) Reasoning, "Thought for 13.9s" (~600–1,500 words CoT ≈ 45–120 wrapped lines) | ≈ 880–2,340px ≈ **1–2.6 screens** (with pref stuck on, C3) | **one 44px pill** (+ ~20px optional last-line ticker while streaming); expanded: ≤ 320px scroll container |
| Full screenshot turn: reasoning + 2 search tools + 4-line answer | ≈ 5,000–9,000px; answer below fold ×4–9 | ≈ 44 + 2×44 + 8×3 gaps + ~120px answer ≈ **300px**; answer above the fold |

Target reduction ≥ 95% on the machinery share of a turn, with zero information removed
(premium bar: everything stays one interaction away).

---

## 2. Interaction grammar (the Ollama-derived pattern, adapted)

### 2.1 Reference grammar extracted

Common grammar across Ollama's app ("Thought for X seconds" one-liner, expand on demand),
Open WebUI (`<details>/<summary>` "Thought for X seconds", collapsed by default), ChatGPT
("Thought for Xs", auto-collapses when done, manual expand for details) and Claude.ai
(chevron + live timer while thinking):

1. **Collapsed by default** — always, including after streaming ends.
2. **One-line affordance** carrying live state: "Thinking…" while streaming → duration
   label when done.
3. **Chevron** expand/collapse; expansion is **per-block and ephemeral** (no product
   persists a global "always expanded" preference).
4. **Streaming cue** — an animated label (shimmer/pulse) while thinking.
5. **Expanded state** — visually muted, subordinate typography; the answer stays dominant.

Sources: Ollama thinking blog (ollama.com/blog/thinking), Open WebUI reasoning docs
(docs.openwebui.com/features/chat-conversations/chat-features/reasoning-models), the
ChevronUp/Down "Collapse/Expand thinking" convention in Ollama-ecosystem UIs.

### 2.2 Reasoning pill — state machine

One `ReasoningPill` per `{type:'reasoning'}` part (a turn with interleaved reasoning spans
renders one pill per span — the reducer already keys spans by `messageId`,
`web/src/chat/sseAdapter.ts:162-179`).

| State | Trigger | Collapsed row renders | Notes |
|---|---|---|---|
| **streaming** | part has no `finishedAt` AND message status is `running` | `◌ Thinking…` label with CSS shimmer + chevron; **last-line ticker** below (single `line-clamp-1` line of the newest reasoning text, `aria-hidden`) — RATIFIED IN SCOPE (§12 OQ-3 → KEEP) | No live region — the thread-level `role="status"` running row already announces (`web/src/chat/ExternalStoreChat.tsx:568-572`); a per-pill live region would double-announce |
| **done, duration known** | `finishedAt` set (live stream: REASONING_END timestamp) | `✓-less quiet row: "Thought for 13.9 s"` + chevron | duration = `finishedAt − startedAt`, formatted via the shared duration formatter (§5.3) |
| **done, duration unknown** | part settled but no timestamps (pre-migration rows: `reasoning_duration_ms` NULL) | `"Reasoning"` / `"Ragionamento"` label + chevron | The fallback label; identical affordance, identical expand behavior. `reasoning_duration_ms` persistence is RATIFIED (§12 OQ-1 → YES): rehydrated rows normally carry the duration and render the "done, duration known" state |
| **expanded** (from any state) | operator click | Row (label stays) + body: muted reasoning text in a **max-height 320px scroll container** | While still streaming, the body live-appends and stays scrolled to bottom (`scrollTop = scrollHeight` on delta if the user hasn't scrolled up) |
| **empty degrade** | rehydrated turn with no persisted reasoning (NULL column) | **nothing renders** | Amendment #91 point 4: "column NULL = drawer absent, correct degrade" |

### 2.3 Expanded reasoning typography — decision + justification

**Sans, not mono, not serif.** Body: `font-sans` (Atkinson Hyperlegible Next,
`web/tokens/tokens.json:77`) at `text-xs`/`leading-relaxed`, `text-text-muted`, behind a
`border-s border-border` inset rule (the existing drawer's rule,
`web/src/chat/ReasoningDrawer.tsx:52`, kept). Justification: CoT is prose, not machine
output — mono would inflate wrapped-line count ~15% (defeating the compactness goal) and
mis-signal "code"; Fraunces (the display serif) is reserved for headings
(`ExternalStoreChat.tsx:541`) and would glamorize what must read as subordinate machinery.
Muted-sans + inset rule matches Calm Prism §6.2 ("subtle inset rule and muted text rather
than a nested card").

### 2.4 Expand-preference policy — decision + justification

**Collapsed-by-default, always; per-part ephemeral expand state; the persisted preference
is retired.**

- `web/src/chat/reasoningPref.ts` is **deleted**; `ReasoningPill` holds a plain
  `useState(false)`. No localStorage read or write.
- Justification vs the current default-hidden pref: the pref's *default* was already
  hidden, but its *persistence* is precisely the wall-of-text root cause (C3): one expand
  poisons every future turn and every conversation, including live streams. None of the
  four reference products persists expansion globally; expansion is a per-block "let me
  look" gesture. The premium bar is preserved because access is one click and the streaming
  ticker keeps a live preview visible without expansion.
- This supersedes Calm Prism §6.2's frozen key contract. The orphaned
  `aura.chat.reasoning.shown` key is left in place (inert, harmless — no migration code;
  document in the phase notes).
- SSE-resume compatibility: reload-attach and resume rebuild parts through the same reducer
  (sse-resume design §4.2) — fresh component instances mean collapsed-by-default, which is
  now the *correct* invariant instead of a state-loss bug.

### 2.5 Tool row — state machine

| State | Trigger | Collapsed row renders |
|---|---|---|
| **running** | no `result`, `isError` ≠ true (`web/src/chat/toolStatus.ts:12-15`) | pulsing warning dot + tool name (mono) + live args summary (§3.2) + running elapsed ticker (aria-hidden, precedent `ToolActivityCard.tsx:109`) + chevron. **Never auto-expands** (deletes C2) |
| **done** | `result` present | success dot + name + args summary + result meta (§3.4) + settled duration + chevron |
| **error** | `isError === true` | danger dot + name + visible `Error` word in `text-danger` + duration + chevron. Stays collapsed (consistency; the danger tint is the signal — see OQ-2) |
| **expanded** | operator click | Row + body (§3.5): typed display or structured raw panel, in a capped scroll container |
| **approval-pending (HITL)** | run status `requires-action` (`sseAdapter.ts:317-323`) | **Not a tool-card state.** Approvals render as `ThreadApprovalCards` between the message list and the composer (`web/src/chat/ExternalStoreChat.tsx:574-580`) and are NEVER collapsed, never grouped, never behind a disclosure. This spec does not touch `web/src/approvals/*` (Calm Prism §6.4 remains law) |

The auto-expand/auto-collapse machinery (`userToggled`/`settledOnce`/`previous` refs,
`ToolActivityCard.tsx:78-93`) is deleted; the state machine collapses to a single
`useState(false)` per row.

---

## 3. Tool card redesign

### 3.1 Collapsed one-liner anatomy

```
[dot] [tool label] [summary…truncated] ············ [meta] [duration] [chevron]
```

- Whole row is ONE `<button>` (single 44px-min target, richer than a 44×44 chevron; the
  Button component already enforces the floor, `web/src/components/ui/button.tsx:22-25`).
- **dot**: 8px square `rounded-sm` (existing `DOT_CLASS`, `ToolActivityCard.tsx:17-21`) —
  `bg-warning` running (with pulse, §7), `bg-success` done, `bg-danger` error.
- **tool label**: `font-mono text-xs text-text` (existing, `ToolActivityCard.tsx:103`).
  MCP tools show `server.tool` verbatim (their registered name already carries the prefix).
- **summary**: `text-xs text-text-muted truncate min-w-0` — §3.2.
- **meta**: `text-[0.75rem] text-text-faint` — result meta when a typed display is
  attached (§3.4). Absent while running.
- **duration**: `font-mono text-[0.75rem] tabular-nums text-text-faint` (existing,
  `ToolActivityCard.tsx:111`); running ticks `aria-hidden` (existing precedent :109).
- **status text for AT**: `sr-only` span with `t('chat.tool.status.*')` (the visible
  status *word* is dropped from the row — the dot + duration carry it visually; `Error`
  stays visible).
- **cost**: per-tool cost does NOT exist on the wire — usage is turn-level only
  (`STATE_DELTA` usage keys, `web/src/chat/sseAdapter.ts:52-60`) and stays in the runtime
  footer (`web/src/i18n/resources.footer.ts:6`). Per-tool cost attribution is flagged as an
  OPTIONAL future data dependency (backend enrichment), explicitly out of scope (§11).

### 3.2 Args summarization rules (per tool family)

New pure module `web/src/chat/toolSummary.ts` — `summarizeArgs(toolName, argsText):
string`. Input is the streamed `argsText` (JSON, possibly partial mid-stream). Algorithm:
`JSON.parse` attempt; on failure (partial stream) retry after appending `"}` / `}`
best-effort closers, else return `''` (summary appears when parseable — no NLP, no regex on
natural language; this parses structured JSON only).

| Family (match on tool name) | Summary | Example |
|---|---|---|
| `web_search`, `document_search`, `*_search` | `args.query` | `"meteo domani Milano"` |
| `fs_read`, `fs_write`, `fs_list`, `fs_*` | `args.path` | `~/projects/aura/prd.md` |
| `shell_exec`, `sandbox_exec` | first line of `args.command`, max 64 chars + `…` | `git log --oneline -5` |
| `memory_*` | verb suffix + `args.query \|\| args.key \|\| ''` | `recall "docker cache"` |
| `send_file` | `args.filename \|\| args.path` | `results.xlsx` |
| MCP (`name contains a dot` or server-prefixed) | first string-valued property | `list_events "2026-07-24"` |
| generic fallback | first string-valued property, max 64 chars | — |

Values render as escaped React text (never markdown/HTML — HARDEN-08 posture,
`ToolActivityCard.tsx:8-15` applies to the summary too). Truncation: CSS `truncate` on the
span + hard 120-char cap in the function (defensive against pathological args).

### 3.3 Grouping — decision + spec

**Runs of ≥ 3 consecutive settled tool parts collapse into one `ToolGroup` stack.**
Deep-research turns fire 5–15 sequential searches (memory: deep-research = 109 agents);
grouping turns 10 × 44px rows into one 44px header.

- Membership: contiguous `tool-call` parts in `message.content` order, all settled
  (`result` present or `isError`), no non-tool part between them. The **currently running
  tool is never inside a group** — it renders as its own live row after the group.
- Threshold: `TOOL_GROUP_MIN = 3` (2 rows collapse to a header + nothing saved — not worth
  the indirection).
- Group header row (same anatomy as §3.1): rollup dot (danger if any member error, else
  success) + `t('chat.tool.group', {count})` ("5 tools") + wall-clock span (first member
  `startedAt` → last member `finishedAt`) + chevron. `aria-expanded`/`aria-controls`.
- Expanded group: the member rows render stacked (each still individually expandable —
  two-level disclosure). Collapse state per group lives in the group-leader component.
- Rendering seam: assistant-ui renders parts one at a time
  (`ExternalStoreChat_messages.tsx:131-148`), so grouping is computed per-part: the
  `ToolFallback` for part *i* reads the whole content via
  `useAuiState((s) => s.message.content)` (precedent: `AnswerSources`,
  `ExternalStoreChat_messages.tsx:297-309`), locates its contiguous run via the pure helper
  `toolRun(content, toolCallId)` (`web/src/chat/toolGrouping.ts`); the run's FIRST member
  renders the entire `ToolGroup`; subsequent members render `null`.
- Display-payload exceptions (§3.5 inline types) break a run (they are never members).

### 3.4 Result meta (collapsed hint for typed results)

When a `DisplayPayload` is attached (`web/src/chat/displays/types.ts:116-129`):

| `payload.type` | meta |
|---|---|
| `web_result` | `t('chat.tool.meta.results', {count: web_results.length})` |
| `table` | `t('display.table.rowCount', {count: rows.length})` (existing key, `resources.display.ts:31-32`) |
| `code` | `resolveLang(lang)` + `t('chat.tool.meta.lines', {count})` |
| `document` | `document.title ?? t('display.document.untitled')` |
| `chart` | `t('chat.tool.meta.points', {count: y_values.length})` |
| `swarm_report` | `t('chat.tool.meta.workers', {count: swarm.length})` |
| raw result (no payload) | `t('chat.tool.meta.chars', {count: result.length})` — cheap, honest |

### 3.5 Expanded body

- **Typed display attached, "evidence/data" types** (`web_result`, `document`, `code`,
  `table`, `chart`, `swarm_report`): the body renders the existing `DisplayRouter` card
  exactly as today (`web/src/chat/displays/DisplayRouter.tsx:50-69`) — pagination, copy,
  CSV export, citations, Source Explorer click-through all preserved (premium bar). The
  collapse boundary moves UP to the tool row; the cards themselves are unchanged.
- **Inline exceptions — always visible, never behind a disclosure**: `system_event`
  (classified errors/warnings — safety-relevant, one line,
  `web/src/chat/displays/SystemEventCard.tsx`) and `local_artifact` (the download chip —
  actionable, small, `web/src/chat/displays/LocalArtifactDisplay.tsx`). For these two
  types `ToolFallback` renders `DisplayRouter` directly with no row, exactly today's
  markup.
- **No/unknown payload → structured raw panel**, new `web/src/chat/ToolResultPanel.tsx`
  (replaces the bare `<pre>` C1):
  - Two labeled sections: **Request** (`argsText`, pretty-printed 2-space JSON when
    parseable, else verbatim) and **Result** (`result`; while running, Result is absent and
    Request shows the streaming args).
  - JSON results are pretty-printed and **syntax-highlighted via the existing lazy Shiki
    seam** (`highlightCode(body, 'json', isDarkTheme())`,
    `web/src/chat/displays/shiki.ts`; plain-escaped `<pre>` until/unless the chunk
    resolves — the exact `CodeDisplay` degrade, `web/src/chat/displays/CodeDisplay.tsx:46-60,97-116`).
    Non-JSON results render as escaped text. NEVER markdown/HTML (HARDEN-08,
    D-FALLBACK preserved).
  - Container: `max-h-80 overflow-y-auto overflow-x-auto` — the payload scrolls inside its
    own scrollbar; the page never grows (§4).
  - **Copy button** (top-right of the Result section): copies the raw untruncated
    `result` via the existing `useCopyAction` hook
    (`web/src/chat/displays/useCopyAction.ts:15-43`), labels
    `chat.tool.copy`/`chat.tool.copied`.
  - `DisplayRouter`'s `default:` fallback case changes from returning `ToolActivityCard`
    (`DisplayRouter.tsx:70-79`) to returning `ToolResultPanel` — required to avoid
    recursion now that `ToolActivityCard` hosts the router, and it strictly improves the
    unknown-payload degrade (escaped, capped, copyable).
- **Child activity** (swarm fan-out rows, `ToolActivityCard.tsx:243-257`): preserved as
  indented rows inside the expanded body, one nesting level, each child a §3.1-style row
  with its own disclosure.

---

## 4. Layout rules

- **Height caps**: every machinery body (reasoning body, ToolResultPanel, and the existing
  `CodeDisplay` cap it matches, `CodeDisplay.tsx:94`) uses `max-h-80` (320px ≈ ⅓ viewport)
  with `overflow-y-auto`. Typed display cards keep their intrinsic height (they are already
  internally paginated — `DisplayPagination.tsx:25,33` default 3/page — and are now behind
  the row).
- **Horizontal scroll containment**: wide content (`<pre>`, tables, highlighted code)
  scrolls inside its own `overflow-x-auto` container. The page body never
  horizontal-scrolls — the existing guards stay: `min-w-0` on message roots
  (`ExternalStoreChat_messages.tsx:129`), `overflow-x-auto` on `data-message-content`
  (`:130`), `[overflow-wrap:anywhere]` on prose.
- **Row height**: collapsed rows/pills `min-h-11` (44px — Button floor and icon-rail
  precedent; ≥ WCAG 2.5.8's 24px). Density tokens (`--row-h`,
  `web/src/styles/theme.css:86-88`) continue to govern inner padding rhythm, not the
  interactive floor.
- **Spacing rhythm**: parts within one assistant turn stack with `space-y-2` (8px) — add
  it to the parts wrapper (`data-message-content` div, `ExternalStoreChat_messages.tsx:130`);
  messages keep `space-y-4` in the viewport (`ExternalStoreChat.tsx:537`); group members
  inside an expanded `ToolGroup` stack with `space-y-1` (4px, tighter = visually one unit).
- **Width**: rows are full message width; no card border for the reasoning pill (it is a
  ghost row — Calm Prism §6.2 "no heavy filled card treatment"); tool rows keep ONE
  low-contrast boundary: `border border-border rounded-[var(--radius-md)]`, no fill
  (Calm Prism §6.3 refinement rules).

---

## 5. Component architecture

### 5.1 File plan (all ≤ 600 LOC; estimates)

| File | Action | Est. LOC | Content |
|---|---|---|---|
| `web/src/chat/ReasoningPill.tsx` | **new** (replaces `ReasoningDrawer.tsx`, which is deleted) | ~170 | Pill states §2.2, shimmer label, ticker, expanded body, focus management |
| `web/src/chat/reasoningPref.ts` | **delete** | −25 | §2.4 |
| `web/src/chat/durationFormat.ts` | **new** (extraction) | ~80 | `formatElapsed` + `useElapsed` moved verbatim from `ToolActivityCard.tsx:152-214` (now shared by tool rows, group header, reasoning pill — REUSABLE CODE rule) |
| `web/src/chat/toolSummary.ts` | **new**, pure | ~140 | `summarizeArgs` (§3.2), `resultMeta` (§3.4) |
| `web/src/chat/toolGrouping.ts` | **new**, pure | ~70 | `toolRun(content, toolCallId): {startIndex, ids[]} \| null` (§3.3) |
| `web/src/chat/ToolActivityCard.tsx` | **rewrite** | ~280 | Row (§3.1) + disclosure + body dispatch (DisplayRouter vs ToolResultPanel) + child rows. Keeps its name and its `childActivity` prop; gains `display?: DisplayPayload`, `onOpenSource?: (refId: string) => void` |
| `web/src/chat/ToolResultPanel.tsx` | **new** | ~160 | §3.5 raw panel |
| `web/src/chat/ToolGroup.tsx` | **new** | ~120 | §3.3 |
| `web/src/chat/ExternalStoreChat_messages.tsx` | edit (+~45, file 365→~410) | | `Reasoning` renderer → `ReasoningPillPart` (reads part fields via `useAuiState((s) => s.part)`, mirroring `ToolFallback`, `:326-338`); `ToolFallback` → group-aware dispatch (§3.3) + inline-exception branch (§3.5) |
| `web/src/chat/sseAdapter_frames.ts` | edit (+4) | | `ReasoningPart` gains `startedAt?: number; finishedAt?: number` (mirror `ToolPart`, `:147-149`) |
| `web/src/chat/sseAdapter.ts` | edit (+~18) | | §5.2 reducer additions |
| `web/src/chat/sseAdapter_snapshot.ts` | edit (+~12) | | §5.2 snapshot mapping |
| `web/src/chat/displays/DisplayRouter.tsx` | edit (~10) | | `default:` → `ToolResultPanel` (§3.5) |
| `web/src/i18n/resources.chatactivity.ts` | **new** | ~110 | `chat.reasoning` + `chat.tool` subtrees for BOTH langs, embedded into `resources.ts` exactly like `composerEffortEn` (`resources.ts:78,353`) — keeps `resources.ts` (573 LOC) under the 600 cap |
| `web/src/styles/motion.css` | edit (+~45) | | §7 keyframes inside the existing `prefers-reduced-motion: no-preference` block (`motion.css:1-37`) |

### 5.2 Reducer/data changes (what parts already provide vs what's missing)

Already provided: reasoning text accumulation per span (`sseAdapter.ts:262-270`), tool
`startedAt`/`finishedAt` from frame timestamps (`:274,281-287`), display payload attach by
`tool_call_id` (`:329-365`), status (`:317-328`).

Missing → added (all additive-optional; the wire already carries the data):

1. **Reasoning timestamps.** Every reasoning frame carries `timestamp` (verified in
   `internal/agui/testdata/golden-events.json` — REASONING_START/…/_END all stamp epoch-ms;
   the SDK's `events.NewBaseEvent` always stamps one, per
   `internal/agui/server_run_detach.go:206`). Reducer changes in `reduceFrame`:
   - `REASONING_START` / `REASONING_MESSAGE_START` (currently no-op/ensure,
     `sseAdapter.ts:262-264,367-370`): `ensureReasoning` + stamp `startedAt` from
     `frameTimestamp(frame)` if unset.
   - `REASONING_MESSAGE_END` / `REASONING_END`: stamp `finishedAt` (last-wins is fine —
     spans are keyed by messageId).
   - `updateReasoning` (`:174-179`) spreads existing fields (it already does
     `{...part, text}` — timestamps survive).
2. **Snapshot reasoning rehydration** (consumes Amendment #91 point 3): in
   `snapshotToThreadMessages`'s assistant branch (`sseAdapter_snapshot.ts:188-198`), read
   an optional `reasoning` string field off the snapshot message; when non-empty, prepend
   `{type: 'reasoning', text}` (NO timestamps → the pill's duration-unknown fallback) ahead
   of the tool parts. `SnapshotMessage` (`sseAdapter_frames.ts:184-190`) gains
   `readonly reasoning?: unknown`. Absent/NULL field → no part → no pill (correct degrade).
3. **Where collapse state lives**: per-part component state (`useState(false)` in
   `ReasoningPill` / row / group leader). NOT in the reducer, NOT in a store, NOT
   persisted. Rationale: the reducer replaces part objects on every delta
   (`writeTool`/`updateReasoning` produce new objects), so state-on-part would be clobbered;
   component identity is stable per position in the parts array across `setMessages`
   replacements (assistant-ui reconciles by index within one message), which is exactly the
   lifetime the UI state needs. Resume/reload rebuilds → collapsed default (§2.4).

### 5.3 Shared duration formatting

`durationFormat.ts` exports `formatElapsed(ms, language, t)` (moved from
`ToolActivityCard.tsx:152-165`; templates `chat.tool.duration.seconds/minutes`,
`resources.ts:117-120`) and `useElapsed(startedAt, finishedAt, settled)` (generalized from
`:167-214` — the `ToolStatus` param becomes a boolean `running` so the reasoning pill can
use it). The pill label composes `t('chat.reasoning.thought', {duration: formatElapsed(…)})`.

---

## 6. Design tokens applied (existing blue palette — no new colors)

All from `web/tokens/tokens.json` / `web/src/styles/theme.css` (both themes covered by the
semantic vars; `:root[data-theme]` swap is automatic).

| Element | Tokens |
|---|---|
| Reasoning pill row (ghost) | text `--color-text-muted`, hover `--color-text`; no border, no fill; focus ring `--color-ring` via `focus-visible:ring-2 ring-ring` (Button default) |
| Shimmer label | gradient `--color-text-muted` → `--color-accent-text` → `--color-text-muted` via `color-mix`, `background-clip: text`; static fallback `--color-text-muted` |
| Ticker line | `--color-text-faint` |
| Reasoning expanded body | text `--color-text-muted`, inset rule `--color-border`, scrollbar on `--color-surface` |
| Tool row card | border `--color-border`, radius `--radius-md`, bg transparent (on `--color-surface`) |
| Status dots | `--color-warning` (running), `--color-success` (done), `--color-danger` (error) — existing mapping `ToolActivityCard.tsx:17-21` |
| Tool name | `--color-text`, `--font-mono` |
| Summary / meta / duration | `--color-text-muted` / `--color-text-faint` / `--color-text-faint` + `--font-mono` tabular-nums |
| Error word | `--color-danger` |
| ToolResultPanel sections | labels `--color-text-faint`; body `--color-text-muted`, `--font-mono`; container border `--color-border`, bg `--color-surface` (matches `CodeDisplay.tsx:93`) |
| Group header | same as tool row; member indent rule `--color-border` (precedent `ToolActivityCard.tsx:244`) |
| Motion | `--motion-fast` (chevron), `--motion-base` (reveal), `--motion-ease-out` |

Accent discipline: accent stays for citations/actions (Calm Prism §6.7); the only accent
use here is the shimmer highlight (`--color-accent-text` — the readable-on-surface accent
tone) and the standard focus ring.

---

## 7. Motion spec (CSS-only)

Additions to `web/src/styles/motion.css` inside the `@media (prefers-reduced-motion:
no-preference)` block (`motion.css:1-37`); the existing global reduce override
(`motion.css:201-210`, animation/transition → 1ms) is the reduced-motion kill-switch, plus
`motion-reduce:transition-none` stays on chevrons (existing pattern
`ToolActivityCard.tsx:134`, `ReasoningDrawer.tsx:45`).

| Animation | Spec | Reduced-motion fallback |
|---|---|---|
| Chevron rotate | `transform var(--motion-fast) var(--motion-ease-out)`; `rotate-90` (pill, precedent `ReasoningDrawer.tsx:42-46`) / `rotate-180` (rows) | instant state change |
| Body reveal (the ONE high-impact moment) | `@keyframes aura-part-reveal { from { opacity: 0; transform: translateY(-4px) } to { opacity: 1; transform: none } }`, `var(--motion-base) var(--motion-ease-out)`, runs when the `hidden` attr is removed (display none→block restarts CSS animations — no JS height measurement). Collapse is instant (discipline: one clean transition, no exit choreography) | body appears instantly |
| Thinking shimmer | `@keyframes aura-thinking-shimmer { from { background-position: 140% 0 } to { background-position: -140% 0 } }` on the label (`background: linear-gradient(90deg, muted 0%, accent-text 50%, muted 100%); background-size: 260% 100%; -webkit-background-clip: text; color: transparent`), duration 2200ms linear infinite — cadence matches `--skeleton-duration` (`skeleton.css:7`) so all "working" cues breathe together | static `--color-text-muted` label (set `color` fallback before clip so the 1ms-killed animation leaves readable text) |
| Running dot pulse | `@keyframes aura-dot-pulse { from { opacity: .45 } to { opacity: 1 } }`, 1200ms `var(--motion-ease-in-out)` infinite alternate | static dot |

No other micro-animations. The ticker updates are content changes, not animations.

---

## 8. A11y spec (WCAG 2.2)

### 8.1 Roles/ARIA per state

| Element | ARIA |
|---|---|
| Pill / tool row / group header button | `<button type="button">` with `aria-expanded={expanded}`, `aria-controls={bodyId}` (`useId()`, precedents `ReasoningDrawer.tsx:36-39`, `ToolActivityCard.tsx:126-128`). Accessible name = visible label + sr-only status (tool rows: `aria-label={t('chat.tool.rowAria', {tool})}` so the name is stable while the summary churns) |
| Bodies | remain in DOM with `hidden` when collapsed (every `aria-controls` reference resolves — Calm Prism §6.2 requirement); expanded body wrapped in `role="region"` with `aria-label` (`chat.reasoning.regionAria` / `chat.tool.regionAria` with tool name) |
| Streaming cues | ticker + running elapsed + shimmer label are `aria-hidden="true"`; settled duration is NOT hidden (precedent `ToolActivityCard.tsx:109`); no per-part live regions — the thread `role="status"` row is the single announcer (`ExternalStoreChat.tsx:568-572`) |
| Error state | visible `Error` text (not color-only — 1.4.1); sr-only status word for done/running |
| Copy button | `aria-label={t('chat.tool.copyAria')}`; confirmation via label swap (existing `useCopyAction` pattern) |
| `aria-invalid` | n/a — no validation inputs added; the omit-when-valid convention (`web/src/a11y/aria.ts`) is untouched |

### 8.2 Keyboard interaction table

| Key | Context | Behavior |
|---|---|---|
| `Tab` | thread | reaches each pill/row/group button in DOM order; collapsed bodies (hidden) contribute no tab stops |
| `Enter` / `Space` | on a disclosure button | toggles (native button semantics; no keydown handlers) |
| `Tab` | inside expanded body | reaches inner actions (copy, pagination, citations, links) in order |
| `Shift+Tab` | from body | returns to the disclosure button |
| `Esc` | anywhere | no behavior added (Esc remains reserved for modal surfaces: Source Explorer sheet, cancel confirmation `InlineApprovalCard.tsx:64-68`) |

### 8.3 Focus management

- Expand: focus stays on the trigger button (no focus move — content below is reachable
  with Tab; moving focus would violate operator expectation on a toggle).
- Collapse while focus is INSIDE the body (e.g. after using Copy): focus returns to the
  trigger button (`document.activeElement` containment check on toggle — prevents focus
  loss to `<body>`).
- Targets: every interactive element ≥ 44×44 CSS px (`min-h-11` rows, Button floor);
  visible focus via the shared `focus-visible:ring-2 focus-visible:ring-ring`.

---

## 9. i18n key table (react-i18next; both languages mandatory; parity enforced by `web/src/i18n/__tests__/resources.parity.test.ts`)

New module `web/src/i18n/resources.chatactivity.ts`, embedded at `chat.reasoning` /
`chat.tool` in `resources.ts` (pattern: `composer.effort`, `resources.ts:78`). Existing keys
`chat.reasoning.show/hide` are retired with the drawer; `chat.tool.showRaw/hideRaw`
(`resources.ts:115-116`) are retired with the raw chevron; `chat.tool.duration.*` and
`chat.tool.status.*` (`resources.ts:117-126`) are kept unchanged.

| Key | en | it |
|---|---|---|
| `chat.reasoning.thinking` | `Thinking…` | `Sto ragionando…` |
| `chat.reasoning.thought` | `Thought for {{duration}}` | `Ha ragionato per {{duration}}` |
| `chat.reasoning.label` | `Reasoning` | `Ragionamento` |
| `chat.reasoning.expandAria` | `Show reasoning` | `Mostra il ragionamento` |
| `chat.reasoning.collapseAria` | `Hide reasoning` | `Nascondi il ragionamento` |
| `chat.reasoning.regionAria` | `Model reasoning` | `Ragionamento del modello` |
| `chat.reasoning.pending` (kept) | `Thinking...` | `Ragionamento in corso...` |
| `chat.tool.rowAria` | `{{tool}} activity` | `Attività di {{tool}}` |
| `chat.tool.expandAria` | `Show {{tool}} details` | `Mostra i dettagli di {{tool}}` |
| `chat.tool.collapseAria` | `Hide {{tool}} details` | `Nascondi i dettagli di {{tool}}` |
| `chat.tool.regionAria` | `{{tool}} result` | `Risultato di {{tool}}` |
| `chat.tool.request` | `Request` | `Richiesta` |
| `chat.tool.result` | `Result` | `Risultato` |
| `chat.tool.copy` | `Copy result` | `Copia risultato` |
| `chat.tool.copied` | `Copied` | `Copiato` |
| `chat.tool.copyAria` | `Copy the raw result` | `Copia il risultato grezzo` |
| `chat.tool.group_one` | `{{count}} tool` | `{{count}} strumento` |
| `chat.tool.group_other` | `{{count}} tools` | `{{count}} strumenti` |
| `chat.tool.groupAria` | `Grouped tool activity` | `Attività strumenti raggruppata` |
| `chat.tool.meta.results_one` | `{{count}} result` | `{{count}} risultato` |
| `chat.tool.meta.results_other` | `{{count}} results` | `{{count}} risultati` |
| `chat.tool.meta.lines_one` | `{{count}} line` | `{{count}} riga` |
| `chat.tool.meta.lines_other` | `{{count}} lines` | `{{count}} righe` |
| `chat.tool.meta.points_one` | `{{count}} point` | `{{count}} punto` |
| `chat.tool.meta.points_other` | `{{count}} points` | `{{count}} punti` |
| `chat.tool.meta.workers_one` | `{{count}} worker` | `{{count}} worker` |
| `chat.tool.meta.workers_other` | `{{count}} workers` | `{{count}} worker` |
| `chat.tool.meta.chars_one` | `{{count}} char` | `{{count}} carattere` |
| `chat.tool.meta.chars_other` | `{{count}} chars` | `{{count}} caratteri` |

(Duration values interpolate through the existing `chat.tool.duration.seconds/minutes`
templates via `formatElapsed` — e.g. en `Thought for 13.9 s`, it `Ha ragionato per 13,9 s`
with locale decimal comma from `Intl.NumberFormat`, `ToolActivityCard.tsx:153-165`.)

---

## 10. Acceptance criteria + test plan

### 10.1 Acceptance criteria (machine-checkable)

Stable test ids: `reasoning-pill`, `reasoning-body`, `reasoning-ticker`, `tool-row`,
`tool-body`, `tool-group`, `tool-group-body`, `tool-copy` (existing: `tool-elapsed`,
`tool-children`).

- **AC-1** A settled reasoning part renders `[data-testid="reasoning-pill"]` with
  `aria-expanded="false"` and a hidden `reasoning-body`; the CoT text is NOT visible.
- **AC-2** With `startedAt`/`finishedAt` on the part, the pill label matches
  `Thought for …` (en); without timestamps (rehydrated), it is exactly `Reasoning` — and
  behavior is otherwise identical (expand works the same).
- **AC-3** While the message is running and the part unfinished, the pill shows the
  `Thinking…` label with the shimmer class and an `aria-hidden` ticker containing the last
  reasoning line.
- **AC-4** Clicking the pill sets `aria-expanded="true"`, reveals the body
  (`role="region"` with accessible name), and the body container has both `max-h-80` and
  `overflow-y-auto`. Focus remains on the trigger.
- **AC-5** No code path reads or writes `localStorage['aura.chat.reasoning.shown']`
  (storage spy in vitest); `reasoningPref.ts` no longer exists.
- **AC-6** A running tool part renders a collapsed `tool-row` (no expanded args — the
  auto-expand of `ToolActivityCard.tsx:81` is gone), with pulsing dot class and the args
  summary (`query` text for `web_search`).
- **AC-7** A settled raw tool part stays collapsed; expanding reveals `tool-body`
  containing Request + Result sections, the Result JSON pretty-printed (highlighted OR
  plain-escaped fallback), inside a `max-h-80 overflow-y-auto` container; `tool-copy`
  writes the raw result string to the clipboard.
- **AC-8** A tool part with a `web_result` display payload renders the collapsed row with
  meta `N results`; expanding renders the existing `WebResultDisplay` (citations + Source
  Explorer click-through still fire `onOpenSource`).
- **AC-9** `system_event` and `local_artifact` payloads render inline with NO disclosure
  row (snapshot: markup unchanged vs today).
- **AC-10** Three consecutive settled tool parts render ONE `tool-group` header
  (`3 tools`, rollup dot, wall-clock duration) and no individual rows until expanded; a
  fourth, still-running tool renders as its own row outside the group.
- **AC-11** Error tool part: visible `Error` text with `text-danger`, row still
  collapsible, sr-only status present.
- **AC-12** Approval flow untouched: all existing `ExternalStoreChat.approvals` and
  `Composer_approvalCapture` tests pass unmodified (HITL never collapsed —
  `ThreadApprovalCards` placement unchanged).
- **AC-13** Snapshot rehydration: a `MESSAGES_SNAPSHOT` assistant message carrying a
  `reasoning` string yields a reasoning part (fallback label); absent field yields no pill.
- **AC-14** Reducer: folding the golden frame sequence produces reasoning parts with
  `startedAt`/`finishedAt` from the REASONING_* frame timestamps; all existing
  `sseAdapter` tests pass (additive fields only).
- **AC-15** Playwright: page `document.documentElement.scrollWidth <= innerWidth` after
  rendering a turn with a 200-line JSON tool result (no horizontal page scroll).
- **AC-16** Playwright with `emulateMedia({reducedMotion: 'reduce'})`: expanding is
  instant (no reveal animation), shimmer label is static text.

### 10.2 Test plan

**Vitest component tests** (colocated `web/src/chat/__tests__/`):
- `ReasoningPill.test.tsx` — AC-1..5 (streaming/done/fallback/expand/focus/no-storage);
  i18n both languages via the test i18n harness.
- `ToolActivityCard.test.tsx` — rewritten for AC-6..9, AC-11; XSS guard retained: a
  `<script>`-bearing result renders as inert text (ports the existing behavioral assertion,
  `ToolActivityCard.tsx:8-15`).
- `ToolResultPanel.test.tsx` — JSON pretty/highlight/fallback, copy, caps, streaming
  (Request-only) state.
- `toolSummary.test.ts` — table-driven per family incl. partial-JSON mid-stream, 120-char
  cap, missing keys.
- `toolGrouping.test.ts` — run detection: threshold, broken runs (text/reasoning between),
  running tail exclusion, inline-exception exclusion.
- `ToolGroup.test.tsx` — AC-10, rollup status, two-level disclosure, a11y attrs.
- `sseAdapter.test.ts` additions — AC-14; `sseAdapter_snapshot.test.ts` additions — AC-13.
- `resources.parity.test.ts` — passes automatically for the new keys (en/it parity).

**Playwright** (`web/e2e/chat-compact.spec.ts`, plus updating the reasoning/tool
assertions in `web/e2e/chat.spec.ts` that encode the old drawer/card behavior):
collapsed-by-default on a real streamed turn, expand reveals payload, a11y roles
(`aria-expanded`/`region`), AC-15 no-horizontal-scroll, AC-16 reduced-motion. Run against
the live container per the E2E memory recipe (`AURA_E2E_ORIGIN=:9080`, `docker compose
build aura && up -d` first — baked dist).

**Gates** (project law): vitest coverage **≥ 85%** on every new/rewritten module
(`web/` matches the Go floors); Stryker mutation **≥ 70%** on `toolSummary.ts`,
`toolGrouping.ts`, `durationFormat.ts`, `ReasoningPill.tsx`, `ToolActivityCard.tsx`
(`web/stryker.config.json` harness); `tsc`, eslint, prettier green; i18n parity test
green.

---

## 11. Rollout

- **Single phase, no feature flag.** The change is purely presentational plus two
  additive-optional reducer fields; the wire contract is untouched; the snapshot field is
  consumed only when present (Amendment #91 lands it independently — this UI degrades
  correctly both before and after that lands, AC-13). A flag would double the test matrix
  for zero rollback value: rollback = revert the commit.
- **Wave order (one phase, 3 waves):**
  1. Extraction + pure logic (`durationFormat`, `toolSummary`, `toolGrouping`, reducer
     fields, snapshot mapping) + tests.
  2. Components (`ReasoningPill`, `ToolActivityCard` rewrite, `ToolResultPanel`,
     `ToolGroup`, `ExternalStoreChat_messages` wiring, i18n, motion.css) + tests.
  3. E2E + quality gates + dist rebuild.
- **Dist rebuild rule:** the served UI is the baked `internal/webui/dist` — rebuild via
  Linux docker webbuild (`docker compose build aura && docker compose up -d`; npm install
  happens in the container — Windows lockfile lacks Linux WASM optional deps). Web gates
  (vitest/tsc/prettier/playwright) run on Windows Git Bash; Go tests (reducer-adjacent
  golden tests, if touched) in WSL.
- **Quality snapshot:** any row whose CI-gate-path glob matches `web/src/chat/**` must be
  re-attested at phase close (CLAUDE.md snapshot rule; verify locally with
  `scripts/quality_snapshot_gate.sh` before the push).
- **Explicitly out of scope:**
  - Per-tool cost attribution (§3.1) — optional data-dependency enhancement.
    (`reasoning_duration_ms` persistence moved IN scope of Amendment #91's backend
    half — §12 OQ-1 ratified YES.)
  - Amendment #91's backend half (migration, runner buffer, snapshot projection) — owned
    by its own phase; this spec only consumes the field.
  - `web/src/approvals/*` (HITL cards), the runtime footer, the Composer, Telegram
    rendering, Source Explorer internals, typed display card internals
    (`TableDisplay`/`ChartDisplay`/… bodies unchanged).
  - SSE resume client work (RS-07) — independent; this design is compatible by
    construction (§2.4, §5.2.3).
  - Any palette/token change (blue tokens ACCEPTED — none added).

---

## 12. Open questions (operator-level) — ALL RESOLVED 2026-07-23 (operator-ratified)

- **OQ-1 — Persist reasoning duration? → YES.** Amendment #91 extended: the same
  migration adds nullable `reasoning_duration_ms bigint` (first→last reasoning-delta wall
  time, computed at persist time) and the snapshot projection carries it, so rehydrated
  pills read "Thought for 13.9 s" identically to live. The "Reasoning"/"Ragionamento"
  fallback (§2.2 row 4) remains ONLY for pre-migration rows (NULL column).
- **OQ-2 — Error auto-expand? → NO (spec default stands).** Error tool cards stay
  collapsed; danger dot + visible "Error" word + danger tint are the failure signal
  (§2.5). No auto-expand.
- **OQ-3 — Streaming ticker: keep or cut? → KEEP.** The one-line live reasoning ticker
  under "Thinking…" (§2.2) is IN SCOPE — the premium-bar extra is wanted: aria-hidden,
  last-line only, dropped the instant the pill settles.
