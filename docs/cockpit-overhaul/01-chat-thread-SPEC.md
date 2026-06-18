---
doc: cockpit-overhaul/01
title: Chat Lane — assistant-ui canonical Thread rebuild (premium, mobile-aware) — Industrial SPEC
status: partially implemented (see Implementation Ledger 2026-06-18) — lighter in-place enhancement of ExternalStoreChat.tsx; canonical Thread split NOT built
created: 2026-06-17
owner: cockpit-overhaul milestone (v1.0.0 Deep Search Web Cockpit)
runtime: useExternalStoreRuntime over AG-UI/SSE (NOT AI SDK) — sseAdapter.ts is the backend contract, KEPT
component_library: "@assistant-ui/react" 0.14.22 + "@assistant-ui/react-markdown" 0.14.4 (pinned in web/package.json)
design_system: "Editorial graphite (premium-calm)" — tokens from sibling 03-design-system-SPEC.md (consumed, never re-invented)
shell_contract: composer lives in BottomDock under a shell-level AssistantRuntimeProvider — see sibling 02-shell-sidebar-SPEC.md §2.4
locked_inputs: .planning/phases/25-chat-approval-center/25-UI-SPEC.md (Copy/Edit/Reload only; reasoning drawer D-01; raw tool cards D-02; branch picker D-09)
constraints: assistant-ui 0.14.x · keep sseAdapter + runtime + continue-after-resume + inline approval cards · ≤600 LOC/TS file · vitest ≥85% · Stryker ≥70% · i18n en+it · WCAG 2.2 · mobile-first 390px
---

# 01 — Chat Lane: assistant-ui canonical Thread rebuild

> **Scope.** Rebuild the cockpit **chat lane presentation** on assistant-ui's *canonical*
> `Thread` patterns (the `packages/ui` reference + the Claude example), polished to the
> Editorial-Graphite design system and beautiful/usable at 390px — while preserving the
> *working backend wiring verbatim*: the `useExternalStoreRuntime` instance, the
> `sseAdapter.ts` AG-UI/SSE reducer, continue-after-resume, inline approval cards, the
> reasoning drawer, raw tool cards, and the path-aware branch picker. **This is a
> presentation swap, not a logic rewrite.** The runtime + reducer are the contract; only
> the component tree that renders them changes.

> **Token discipline.** Every color / space / radius / font / motion value below is a
> **named semantic token** from sibling `03-design-system-SPEC.md` (`--color-bg`,
> `--color-surface(-2/-3)`, `--color-border(-strong)`, `--color-text(-muted/-faint)`,
> `--color-accent(-text/-muted/-pressed)`, `--color-on-accent`, `--color-success|warning|danger`,
> `--radius-sm|md|lg|xl|pill`, `--font-display|sans|mono`, `--shadow-1..4`,
> `--motion-dur-*`/`--motion-ease-*`, density `--space-unit`/`--row-h`). **No new colors,
> no new fonts, no raw hex** (the design-system spec's AC-4 grep gate applies here too).
> Note: the shipped components still carry the literal `text-[#0B0E14]` on-accent value —
> this rebuild replaces it with **`text-on-accent`** (the design-system formalised token).

---

## Implementation Ledger (2026-06-18)

This section reconciles the forward-looking design above against the code actually on
`master`. It does **not** weaken any acceptance criterion — the original §1–§14 contract
remains the aspirational target; the ledger records what shipped, what did not, and the one
headline architectural deviation. All claims cite a real `file:line` read from the tree.

**Commit that implemented (the subset of) this spec:** `fc77e4cb feat(cockpit): overhaul
shell and chat lifecycle` (2026-06-17). The chat-lane part of that commit touched
`Composer.tsx` (+31/−), `ExternalStoreChat.tsx` (+93/−), `ToolActivityCard.tsx` (+31/−),
`MarkdownText.tsx` (new, 65 LOC), `markdownSanitize.ts` (new, 32 LOC), `ContextBudgetGauge.tsx`,
`RuntimeFooter.tsx`, `footerMetrics.ts`, `i18n/resources.ts` (+62), and the approval cards
(`InlineApprovalCard.tsx` +6, `ThreadApprovalCards.tsx` via shell mount).

### Headline deviation — canonical Thread split was NOT built

The central architectural move this SPEC proposed — splitting the runtime out of
`ExternalStoreChat.tsx` into a `useAuraChatRuntime` hook, hoisting the
`AssistantRuntimeProvider` to the shell, and rebuilding the presentation as a canonical
`Thread` tree (`ChatLane.tsx`, `messages/UserMessage.tsx`, `messages/AssistantMessage.tsx`,
`messages/EditComposer.tsx`, `messages/MessageActionBar.tsx`, `ThreadWelcome.tsx`,
`ThreadScrollToBottom.tsx`) — **did not happen.** Instead, `ExternalStoreChat.tsx` was
**kept and enhanced in place**: it still owns the runtime *and* renders the JSX, still mounts
its own `AssistantRuntimeProvider` (`ExternalStoreChat.tsx:318`), still renders its own
`<Composer/>` inside `ThreadPrimitive.Root` (`:344`), and `UserMessage`/`AssistantMessage`
remain **inline functions in the same file** (`:350`, `:409`) rather than extracted to
`messages/*`. The directory `web/src/chat/messages/` does not exist. What landed is a
**lighter, in-place enhancement** (markdown+sanitize security upgrade, a tool-card enrichment
*subset*, a token re-skin) — not the canonical presentation rebuild.

### Per-file-target status (against §1.4)

| Spec-proposed file | Status | Evidence |
|---|---|---|
| `useAuraChatRuntime.ts` (new, extract) | **NOT BUILT** | absent; runtime logic still inline in `ExternalStoreChat.tsx:71-315` |
| `ChatLane.tsx` (new, Thread tree) | **NOT BUILT** | absent; the Thread tree is inline `ExternalStoreChat.tsx:317-347` |
| `ExternalStoreChat.tsx` (→ thin shim) | **DEVIATED** | NOT thinned — kept as the full lane (runtime + JSX, 463 LOC) and enhanced in place; still the shell's chat component (`AppShell.tsx:124`) |
| `messages/UserMessage.tsx` (new) | **DEVIATED** | not extracted; inline `UserMessage()` `ExternalStoreChat.tsx:350-407` |
| `messages/AssistantMessage.tsx` (new) | **DEVIATED** | not extracted; inline `AssistantMessage()` `ExternalStoreChat.tsx:409-462` |
| `messages/EditComposer.tsx` (new) | **NOT BUILT** | absent; edit mode is still the inline `AuiIf composer.isEditing` branch `ExternalStoreChat.tsx:355-373` (the canonical "swap the whole message" pattern §3.1 was **not** adopted) |
| `messages/MessageActionBar.tsx` (new) | **NOT BUILT** | absent; action bars are hand-rolled `opacity-0 hover:opacity-100` `ExternalStoreChat.tsx:389,445` — the `autohide="not-last"`/`hideWhenRunning` semantics §3.1/§3.2 were **not** adopted |
| `ThreadWelcome.tsx` (new) | **NOT BUILT** | absent; empty state is the inline `AuiIf s.thread.isEmpty` block `ExternalStoreChat.tsx:321-330` (plain `font-display` heading, no `--motion-ease-expo` reveal, no suggestion chips) |
| `ThreadScrollToBottom.tsx` (new) | **NOT BUILT** | absent; viewport is still a bare `overflow-y-auto` `ExternalStoreChat.tsx:320` — no `ThreadPrimitive.ScrollToBottom`, no `ViewportFooter`, no `turnAnchor`/`autoScroll` opt-in (§4 not implemented) |
| `MarkdownText.tsx` (new) | **IMPLEMENTED** | `MarkdownText.tsx:1-65` — `MarkdownTextPrimitive` + `remarkGfm` + `rehypeSanitize` + styled table/pre/code/link |
| `markdownSanitize.ts` (new) | **IMPLEMENTED (lighter schema)** | `markdownSanitize.ts:1-32` — `rehype-sanitize` allowlist; see drift note below |
| `Composer.tsx` (edit + relocate) | **PARTIAL** | restyled + Send↔Stop kept (`Composer.tsx:12-41`); **NOT relocated** to `BottomDock` — still rendered inside the lane (`ExternalStoreChat.tsx:344`); the dock holds the mode selector + `RuntimeFooter` only (`AppShell.tsx:142-144`) |
| `BranchPicker.tsx` (edit, restyle) | **IMPLEMENTED** | `BranchPicker.tsx:16-46` — chevron buttons + `font-mono tabular-nums text-accent` Number/Count + `AuiIf branchCount>1` + `hideWhenSingleBranch` |
| `ReasoningDrawer.tsx` (token re-skin) | **IMPLEMENTED** | `ReasoningDrawer.tsx:30-65` — `border-border bg-surface-2`, `motion-reduce` chevron, persisted pref, `text.length===0→null` kept |
| `ToolActivityCard.tsx` (re-skin + enrich) | **DONE** | re-skin + status-tint + auto-expand in `fc77e4cb`; elapsed/auto-collapse-once/subagent-rows completed 2026-06-18 — see §3.5 status below |
| `approvals/InlineApprovalCard.tsx` (re-skin) | **PARTIAL** | `text-on-accent` used (`InlineApprovalCard.tsx:152`); but CardShell still `border-accent/40` (`:229`), **not** the §3.6 `border-l-2 border-l-accent` accent left-rule + `border-strong` |
| `approvals/ThreadApprovalCards.tsx` (reposition) | **DEVIATED** | kept; but mounted by the shell as a **sibling of the lane** (`AppShell.tsx:131`), NOT inside `ChatLane`'s reading-measure column after the message group (§3.6); no accent column alignment |
| `i18n/resources.ts` (new keys, en+it) | **IMPLEMENTED (with drift)** | `resources.ts:50,66,77,291,307,318` — `scrollToBottom`, `streaming`, `loading`, `markdown.copyCode/codeCopied`, `error.retry`, `empty.suggestionsLabel` added in en+it (several keys are unused given the un-built components) |
| `web/package.json` (add deps) | **IMPLEMENTED** | `package.json:33-34` — `rehype-sanitize ^6.0.0` + `remark-gfm ^4.0.1` added |

### What DID land (with evidence)

- **Markdown + sanitization security upgrade (§3.3).** `MarkdownText.tsx:13-15` wires
  `remarkPlugins={[remarkGfm]}` + `rehypePlugins={[[rehypeSanitize, markdownSanitizeSchema]]}`;
  the schema (`markdownSanitize.ts:5-32`) is a `rehype-sanitize` allowlist over `defaultSchema`
  (allow-not-deny model), restricting `href`/`cite` protocols to `http|https|mailto|tel`
  (`:8-12`), re-adding gfm table tags (`:14-23`), and `strip: ['script','style']` (`:31`).
  Links render with `rel="noreferrer" target="_blank"` (`MarkdownText.tsx:21-22`). This is the
  one real security upgrade the SPEC framed, and it shipped.
- **Tool-card enrichment (§3.5) — initial subset shipped in `fc77e4cb`; COMPLETED 2026-06-18
  (TDD pass, working tree).** `fc77e4cb` shipped: status-tinted left-rule (`RULE_CLASS`) + status
  pill (`PILL_CLASS`) keeping the dot — icon+text, never colour alone; and
  **auto-expand-while-running** with a `userToggled` intent-guard. The 2026-06-18 pass then folded
  in the three remaining §3.5 enrichments: **elapsed-time readout** (`useElapsed` hook,
  leak-safe `setInterval` cleared on settle+unmount, `aria-hidden`, graceful-absent when no
  timing), **auto-collapse-ONCE on the `running→done|error` settle edge** (one-shot via a
  `prevStatus` ref, still `userToggled`-guarded — replaces the old re-seed-every-transition), and
  **indented subagent child rows** (the body refactored into a reusable `ToolActivityRow` used by
  parent + children, `ps-4 border-l border-border`, graceful-absent). The raw blob stays escaped
  text in `<pre>` for parent AND children — D-02/XSS invariant intact (a child-level XSS test was
  added). Backed by the extended `ToolActivityCard.test.tsx`.
- **Composer restyle + Send↔Stop (§5, partial).** `Composer.tsx:24-38` keeps the
  `ComposerPrimitive.Cancel`(Stop)↔`ComposerPrimitive.Send` swap on `s.thread.isRunning`,
  `text-on-accent` on the accent CTA, IME-safe Enter handled by the primitive.
- **i18n keys + deps** as tabled above.
- **Footer / dock changes** (`RuntimeFooter.tsx`, `ContextBudgetGauge.tsx`, `footerMetrics.ts`,
  `BottomDock.tsx`, `AppShell.tsx`) — these are **02-shell-sidebar-SPEC.md** surface, not this
  spec's file targets; noted only because they share the commit.

### What did NOT land (still pending if the canonical rebuild is intended)

The entire §4 scroll-management contract (`ThreadPrimitive.Viewport turnAnchor/autoScroll`,
`ViewportFooter`, `ScrollToBottom`), the §3.2 streaming `●` in-progress cursor, the §3.2/§7
`.shine` streaming shimmer (**absent** — `grep shine web/src` returns nothing) + pre-first-token
`<Skeleton>`, the canonical
`EditComposer`/`MessageActionBar`/`ThreadWelcome` components, the provider hoist, and the
composer→`BottomDock` relocation. The §3.6 approval-card repositioning into the reading column
and its accent left-rule re-skin are also outstanding.

**Accepted shape vs pending rebuild.** Per project memory (cockpit = premium bar; minimal
in-place enhancement is the recurring pattern — "no atomic bombs"), the lighter in-place path
may be the accepted v1 shape. If so, the un-built canonical-Thread targets above are
**deliberately deferred**, not bugs. If the canonical rebuild is still intended, the §10
migration plan (extract hook → hoist provider → build `ChatLane` + `messages/*` → swap shell)
is the unchanged marching order. **Unverified:** which of the two is the operator's intent —
no decision record was found in the tree.

### Factual drift corrected inline

- §0.1 states `ExternalStoreChat.tsx` is "400 LOC" and user text "is **NOT markdown**". The
  shipped file is **463 LOC** and user text now **routes through `MarkdownText`**
  (`ExternalStoreChat.tsx:378-386`) — a deviation from the spec's "a prompt is literal"
  rationale (§3.1). Annotated at §0.1 and §3.1.
- §3.2/§3.5 narrative (cursor, `.shine`, elapsed, child rows) describes affordances that were
  not built; annotated in place rather than deleted (the design contract is retained).
- i18n: spec'd `chat.streaming = "Generating response"`; shipped value is `"Streaming"`
  (`resources.ts:51`) — minor copy drift, en+it both present.

---

## 0. The current chat lane — what we have, what is spartan, what is load-bearing

### 0.1 What exists today (read from `web/src/chat/`)

> **Impl note (2026-06-18):** `ExternalStoreChat.tsx` is now **463 LOC**, not 400, and was
> kept-and-enhanced in place (it was never split into `useAuraChatRuntime` + `ChatLane`). See
> the Implementation Ledger above for the per-file verdicts.

`ExternalStoreChat.tsx` (400 LOC) already does the hard part correctly and must NOT be
rewritten in its logic:

- Owns `messages: ThreadMessageLike[]` + `isRunning` + `abortRef` in React state and feeds
  them to `useExternalStoreRuntime<ThreadMessageLike>({ messages, isRunning, convertMessage,
  onNew, onEdit, onReload, onCancel })` (line 246–254).
- `onNew` POSTs `/agent/run` and folds the AG-UI SSE stream onto one assistant
  `ThreadMessageLike` via `streamRun` (sseAdapter). `onEdit`/`onReload` POST
  `/api/conversations/{id}/edit` with a `diverge_seq` and fold via `streamPost`; the
  external-store runtime keeps the prior version as a **sibling branch** automatically
  (no hand-rolled branch state machine).
- `onCancel` aborts the in-flight fetch (Stop / ctx-cancel on the server).
- `resumeNonce` continue-after-resume effect (line 151–190): when AppShell bumps the nonce
  after an inline approval resolves, re-drives a no-message `POST /agent/run` and folds the
  resumed stream into the lane.

The presentation pieces are spartan but each encodes a *locked decision*:

| File | LOC | What it renders | Verdict |
|------|-----|-----------------|---------|
| `ExternalStoreChat.tsx` | 400 | Thread root + viewport + inline `UserMessage`/`AssistantMessage` | **Keep logic; extract & restyle presentation** |
| `Composer.tsx` | 41 | `ComposerPrimitive` Send↔Stop swap, Enter/Shift+Enter | Keep API; restyle + relocate to dock |
| `ReasoningDrawer.tsx` | 66 | D-01 collapsible CoT, persisted pref | **Keep verbatim** (sound a11y) |
| `ToolActivityCard.tsx` | 94 | D-02 raw tool blob in `<pre>`, status dot, XSS-safe | **Keep verbatim** (security-load-bearing) |
| `BranchPicker.tsx` | 46 | `BranchPickerPrimitive` over path-aware backend | Keep API; restyle to canonical chevrons |
| `approvals/InlineApprovalCard.tsx` | 261 | D-03/05/06 in-thread HITL, 3 verbs, terminal states | **Keep verbatim** (security + locked verbs) |
| `approvals/ThreadApprovalCards.tsx` | 45 | Filters cross-thread poll → active thread | Keep; reposition in stream (§3.6) |

### 0.2 Why it reads "spartan" (the gap to assistant-ui-grade)

Concrete defects vs the canonical `packages/ui/thread.tsx`:

1. **No scroll management.** The viewport is a bare `overflow-y-auto` div (line 259). There
   is **no `ThreadPrimitive.ScrollToBottom`**, no auto-scroll anchoring, no "scroll up to
   read history without being yanked down" behavior. On a streaming turn the operator is
   either glued to the bottom or fighting the scroll. (Odysseus gets this right with a
   300px scroll-up bail — assistant-ui gets it right structurally via the viewport's
   `useThreadViewportAutoScroll`.)
2. **No streaming cursor.** Assistant text just grows; there is no in-progress indicator on
   the text part (the canonical `MessagePartPrimitiveInProgress` `●` cursor is unused
   because Aura renders text via a custom `Text` component, bypassing the default).
3. **No `MessagePrimitive.Parts` part-grouping.** Reasoning + tool parts render inline in
   arrival order with no visual grouping; the canonical tree groups consecutive
   reasoning/tool parts under a chain-of-thought container.
4. **User bubble is a flat `bg-surface-2` box** (line 314) — no tail, no max-width discipline
   beyond `max-w-[80%]`, no enter animation. Assistant message is `max-w-[90%]` with no
   reading measure.
5. **Action bar is always-dim-then-hover** with no `autohide="not-last"` / `hideWhenRunning`
   semantics (it hand-rolls `opacity-0 hover:opacity-100`), so on the *last* assistant
   message the actions stay hidden until hover even when idle.
6. **Edit composer is inline-conditional** (`AuiIf composer.isEditing`) rather than the
   canonical "swap the whole message for an `EditComposer`" pattern, which is cleaner and
   what the runtime expects.
7. **Markdown is `MarkdownTextPrimitive` raw** (no remark-gfm, no code-block chrome, no
   sanitization layer) — assistant prose with a table or fenced code renders unstyled, and
   there is **no XSS hardening on the markdown path** (only the tool blob is hardened). See
   §3.3 — this is the one real *security* upgrade in this rebuild.
8. **No empty-state polish, no error-part styling, no `requires-action` affordance** beyond
   what the approval card provides.
9. **Composer lives inside the (mobile-collapsing) chat section** — the shell spec §0 traces
   this to the chat-crushed-to-0 bug. This rebuild moves it to `BottomDock` (§5).

### 0.3 The hard contract that must survive untouched

- `sseAdapter.ts` (frame reducer + fetch pump) — **the backend contract**; not edited.
- The `useExternalStoreRuntime` call and its `onNew/onEdit/onReload/onCancel` callbacks +
  `divergeSeqAt` math + `foldReRun` + `resumeNonce` effect — **the runtime logic**; not
  rewritten, only *moved* into a hook so the JSX can be swapped (§10).
- `ReasoningDrawer`, `ToolActivityCard`, `InlineApprovalCard`, `ThreadApprovalCards` — kept;
  the first two are re-skinned in place (token swap only), the approval cards are kept
  verbatim and *repositioned* in the stream.
- The locked action-bar verb set: **Copy / Edit / Reload only** (no feedback ratings — that
  is Phase 26).

---

## 1. Target architecture — component tree & file targets

### 1.1 The canonical assistant-ui tree we adopt

We adopt the shape of `packages/ui/src/components/assistant-ui/thread.tsx` (the canonical
reference) and the `claude.tsx` example (for the HITL-adjacent bubble/action treatment),
mapped onto Aura's external-store runtime and AG-UI part shapes. The tree:

```
<AssistantRuntimeProvider runtime>          ← hoisted to the SHELL (wraps main + dock), 02-spec §2.4
  <Thread>                                    ← ThreadPrimitive.Root (lane presentation)
    <ThreadPrimitive.Viewport turnAnchor="bottom" autoScroll>   ← scroll owner (§4)
      <div max-w reading column>
        <AuiIf thread.isEmpty><ThreadWelcome/></AuiIf>           ← empty state (§7)
        <div message-group>
          <ThreadPrimitive.Messages>{() => <ThreadMessage/>}</ThreadPrimitive.Messages>
        </div>
        <ThreadApprovalCards/>                ← in-thread HITL, AFTER the message group (§3.6)
        <ThreadPrimitive.ViewportFooter>      ← sticky bottom (DESKTOP only; mobile → dock)
          <ThreadScrollToBottom/>             ← ThreadPrimitive.ScrollToBottom (§4)
        </ThreadPrimitive.ViewportFooter>
      </div>
    </ThreadPrimitive.Viewport>
  </Thread>
</AssistantRuntimeProvider>

ThreadMessage = useAuiState(s.message.role/composer.isEditing) →
  isEditing ? <EditComposer/> : role==='user' ? <UserMessage/> : <AssistantMessage/>
```

> **Composer placement (shell contract).** The canonical reference renders the `Composer`
> inside `ThreadPrimitive.ViewportFooter`. Aura's shell spec (`02-shell-sidebar-SPEC.md`
> §2.4) requires the composer in a viewport-pinned `BottomDock` so it survives the mobile
> keyboard. **Resolution:** the `AssistantRuntimeProvider` is hoisted to wrap *both* the
> chat lane and the dock (shell spec §5), so `ComposerPrimitive.Root` rendered in
> `BottomDock` still binds the same runtime (`Send`→`onNew`, `Cancel`→`onCancel`). The lane
> therefore renders **no composer of its own**; on desktop the lane's `ViewportFooter`
> holds only the `ScrollToBottom` button (and the dock sits below the lane as the grid's
> bottom track). This is the single deliberate divergence from the canonical tree, forced
> by mobile correctness, and is verified as a wiring move against 0.14.22 in §10.

### 1.2 Provider hoist & the shell contract

`ExternalStoreChat` today *is* the `AssistantRuntimeProvider`. The rebuild splits it:

- `useAuraChatRuntime(threadId, { onUsage, resumeNonce })` — a **hook** containing all the
  current `ExternalStoreChat` logic (state, onNew/onEdit/onReload/onCancel, foldReRun,
  divergeSeqAt, resumeNonce effect). Returns `{ runtime, isRunning, messages }`. Pure
  extraction; zero logic change.
- The shell (`AppShell.tsx`, per 02-spec §5) calls the hook and renders
  `<AssistantRuntimeProvider runtime>` around `<ChatLaneRegion>` + `<BottomDock>`.
- `ChatLane.tsx` renders the `Thread` tree (§3,§4). `BottomDock` (owned by 02-spec) renders
  `<Composer/>` inside the same provider.

This keeps a single source of truth for `isRunning` (the dock's Send↔Stop reads
`useAuiState(s.thread.isRunning)`) and a single runtime for branch nav, edit, cancel.

### 1.3 Current Aura component → its replacement (mapping)

| Today (`web/src/chat/`) | Canonical assistant-ui pattern | This rebuild |
|---|---|---|
| `ExternalStoreChat.tsx` (runtime + JSX mixed) | runtime in shell, `Thread` presentational | **Split**: `useAuraChatRuntime.ts` (logic) + `ChatLane.tsx` (`Thread` tree) |
| inline `<ThreadPrimitive.Root/Viewport>` | `ThreadPrimitive.Root` + `Viewport turnAnchor` + `ViewportFooter` | `ChatLane.tsx` adopts viewport + scroll + welcome |
| inline empty `<AuiIf thread.isEmpty>` block | `ThreadWelcome` (+ optional `Suggestions`) | `ThreadWelcome.tsx` (§7.1) |
| inline `UserMessage()` | canonical `UserMessage` (grid, bubble, `UserActionBar`, `EditComposer`) | `UserMessage.tsx` + `EditComposer.tsx` (§3.1) |
| inline `AssistantMessage()` | canonical `AssistantMessage` (`Parts`, action bar, branch picker, `MessageError`) | `AssistantMessage.tsx` (§3.2) |
| hand-rolled `opacity-0 hover` action bar | `ActionBarPrimitive.Root autohide="not-last" hideWhenRunning` | `MessageActionBar.tsx` (§3.1/3.2) |
| `Composer.tsx` | canonical `Composer` (`ComposerPrimitive` shell) | restyle + move to `BottomDock` (shell-owned), Send↔Stop kept |
| `BranchPicker.tsx` | `BranchPickerPrimitive` chevrons + `Number/Count` | restyle to canonical chevrons + accent indicator (§6) |
| `ReasoningDrawer.tsx` | (canonical uses `ReasoningRoot/Trigger/Content`) | **keep Aura's** (persisted pref) — token re-skin only (§3.4) |
| `ToolActivityCard.tsx` | (canonical uses `ToolFallback`/`ToolGroup`) | **keep Aura's raw card** (D-02 locked, XSS-safe) — token re-skin (§3.5) |
| (no markdown chrome) | `MarkdownText` (`MarkdownTextPrimitive` + remark-gfm + code header) | `MarkdownText.tsx` + **sanitization** (§3.3) |
| `ThreadApprovalCards.tsx` | (no analog — Aura-specific HITL) | **keep**, reposition in stream (§3.6) |
| (no streaming cursor) | `MessagePartPrimitiveInProgress` `●` | adopt in the `Text` part component (§3.2) |
| (no scroll button) | `ThreadPrimitive.ScrollToBottom` | `ThreadScrollToBottom.tsx` (§4) |

### 1.4 File targets (all ≤600 LOC; refactor-on-touch)

| File | Action | Notes |
|---|---|---|
| `web/src/chat/useAuraChatRuntime.ts` | **new (extract)** | All current `ExternalStoreChat` logic verbatim (state, onNew/onEdit/onReload/onCancel, foldReRun, divergeSeqAt, resumeNonce). Returns `{ runtime, isRunning, messages }`. ≤200 LOC. |
| `web/src/chat/ChatLane.tsx` | **new** | The `Thread` tree: `ThreadPrimitive.Root/Viewport/Messages/ViewportFooter` + welcome + `ThreadApprovalCards` mount + `ScrollToBottom`. Renders NO composer (dock owns it). ≤180 LOC. |
| `web/src/chat/ExternalStoreChat.tsx` | **rewrite (thin)** | Becomes a back-compat shim that wires `useAuraChatRuntime` + provider + `ChatLane` **for tests / desktop fallback**, OR is deleted once `AppShell` hoists the provider (shell spec §5). Keep its name+props so the existing test imports keep resolving during migration (§10). |
| `web/src/chat/messages/UserMessage.tsx` | **new** | Canonical user bubble (grid, tail, `UserActionBar`, branch picker). ≤120 LOC. |
| `web/src/chat/messages/AssistantMessage.tsx` | **new** | `MessagePrimitive.Parts` render-fn (text→markdown+cursor, reasoning→drawer, tool-call→raw card), `MessageError`, action bar, branch picker. ≤160 LOC. |
| `web/src/chat/messages/EditComposer.tsx` | **new** | Canonical edit-mode message composer (Cancel/Update). ≤80 LOC. |
| `web/src/chat/messages/MessageActionBar.tsx` | **new** | `ActionBarPrimitive` Copy/Edit (user) + Copy/Reload (assistant), `autohide`/`hideWhenRunning`. ≤120 LOC. |
| `web/src/chat/ThreadWelcome.tsx` | **new** | Empty-state hero (display serif) + optional suggestion chips. ≤100 LOC. |
| `web/src/chat/ThreadScrollToBottom.tsx` | **new** | `ThreadPrimitive.ScrollToBottom` button (accent, pill, disabled:invisible). ≤50 LOC. |
| `web/src/chat/MarkdownText.tsx` | **new** | `MarkdownTextPrimitive` + `remark-gfm` + sanitized code-block chrome + link allowlist. ≤200 LOC. |
| `web/src/chat/markdownSanitize.ts` | **new** | `rehype-sanitize` schema + URL allowlist (ported from Odysseus blocklists). Pure, heavily unit-tested. ≤120 LOC. |
| `web/src/chat/Composer.tsx` | **edit** | Restyle to canonical shell (Editorial-Graphite tokens); remove its own `border-t` (dock owns it, shell spec §9); add `!isComposing` IME guard note; keep Send↔Stop. ≤90 LOC. |
| `web/src/chat/BranchPicker.tsx` | **edit** | Restyle to canonical chevron buttons + accent `Number/Count`. Keep `AuiIf branchCount>1` + `hideWhenSingleBranch`. ≤70 LOC. |
| `web/src/chat/ReasoningDrawer.tsx` | **edit (token re-skin)** | Keep logic; swap to Editorial-Graphite tokens + `--motion-*` transitions. |
| `web/src/chat/ToolActivityCard.tsx` | **edit (re-skin + enrich)** | Keep XSS guard + raw `<pre>`; swap to tokens (`surface-3` blob well, `font-mono`); add status-tint left-rule + pill, auto-expand-running + intent-guarded auto-collapse, optional elapsed readout, indented subagent child rows (§3.5). Stays ≤120 LOC (split a `ToolActivityChildRow` sub-component if it approaches the budget). |
| `web/src/approvals/InlineApprovalCard.tsx` | **edit (token re-skin)** | Keep verbatim logic + verbs; swap `text-[#0B0E14]`→`text-on-accent`, `border-accent/40`→accent left-rule + `border-strong` (design-system §6 row). |
| `web/src/approvals/ThreadApprovalCards.tsx` | **edit** | Keep filter logic; reposition wrapper (§3.6). |
| `web/src/i18n/resources.ts` | **edit** | New keys §11 (en+it). |
| New deps in `web/package.json` | **add** | `rehype-sanitize`, `remark-gfm` (gfm may already transit via react-markdown — pin explicitly). MIT; run the supply-chain gate (25-UI-SPEC Registry Safety). |

No backend change. No `sseAdapter.ts` change. No runtime-logic change.

---

## 2. assistant-ui primitives & APIs (verified from clone + pinned 0.14.22)

Verified against `D:/tmp/assistant-ui` (the clone) and `web/package.json`
(`@assistant-ui/react` **0.14.22**, `@assistant-ui/react-markdown` **0.14.4**).

### 2.1 ThreadPrimitive

| API | Signature / props (verified) | Use here |
|---|---|---|
| `ThreadPrimitive.Root` | `Primitive.div`; container | Lane root, `flex h-full flex-col` |
| `ThreadPrimitive.Viewport` | props `turnAnchor?: "top"\|"bottom"` (default `"bottom"`), `autoScroll?: boolean` (default true unless `turnAnchor="top"`), `scrollToBottomOnRunStart?`/`OnInitialize?`/`OnThreadSwitch?` (all default true), `topAnchorMessageClamp?` (`ThreadViewport.tsx:30-86`) | The scroll owner (§4). Use default `turnAnchor="bottom"` (classic chat) |
| `ThreadPrimitive.Messages` | render-prop `{() => ReactNode}` or `{({message}) => …}` | Iterate turns → `ThreadMessage` |
| `ThreadPrimitive.ViewportFooter` | sticky footer slot inside viewport | Desktop scroll-to-bottom holder |
| `ThreadPrimitive.ScrollToBottom` | `createActionButton`; renders `null` when `isAtBottom` (the button auto-hides via `disabled:invisible`), opt `behavior?: ScrollBehavior` (`ThreadScrollToBottom.ts:14-44`) | The pill button (§4) |
| `ThreadPrimitive.Suggestions` + `SuggestionPrimitive.Trigger send` | welcome suggestion chips | Optional empty-state chips (§7.1) |
| `ThreadPrimitive.Empty` | conditional wrapper (legacy) | Prefer `AuiIf s.thread.isEmpty` (codemod-blessed pattern) |

### 2.2 MessagePrimitive

| API | Signature (verified) | Use here |
|---|---|---|
| `MessagePrimitive.Root` | `Primitive.div`; provides message context | Per-turn wrapper, `data-role` |
| `MessagePrimitive.Parts` | `{ components?: { Text, Image, Reasoning, Source, File, Empty, tools?, data?, ToolGroup?, ReasoningGroup?, … } }` OR render-prop `{({part}) => …}` (`MessageParts.tsx:34-73`) | Assistant body part router (§3.2) |
| `MessagePrimitive.GroupedParts` | `{ groupBy: groupPartByType({...}) }` render-prop `({part, children}) =>` with synthetic `group-reasoning`/`group-tool`/`group-chainOfThought`/`tool-call`/`text`/`reasoning` part types (`thread.tsx:244-286`) | **Optional** part-grouping (§3.2 note) |
| `MessagePrimitive.Error` | wraps `ErrorPrimitive` when message `status.type==='incomplete'`/error | Error part (§7.4) |
| Part component props (`MessagePartComponentTypes.ts`) | `TextMessagePartProps = MessagePartState & { type:'text'; text:string }`; `ReasoningMessagePartProps = MessagePartState & { type:'reasoning'; text:string }`; `ToolCallMessagePartProps = MessagePartState & ToolCallMessagePart<TArgs,TResult> & { addResult, resume, respondToApproval }` (`:24-88`) | Drives §3.2 render-fns |

`MessagePartState` carries the streaming `status: MessagePartStatus` (`running` | `complete`
| `incomplete` | `requires-action`) — the same union the message-level `status` uses, which
`sseAdapter.ts` already sets (`toThreadMessage` → `status: {type:'complete'|'incomplete'|
'requires-action'|'running'}`).

### 2.3 ComposerPrimitive / ActionBarPrimitive / BranchPickerPrimitive / ErrorPrimitive

| API | Verified shape | Use |
|---|---|---|
| `ComposerPrimitive.Root` | form container; `Input` (`rows`, auto-grows to `max-h`), `Send` (disabled when empty/running), `Cancel` (Stop) | Composer (§5) — kept |
| `ComposerPrimitive.Input` | textarea; Enter→send, Shift+Enter→newline, Esc→cancel built-in | Multiline (§5) |
| `ActionBarPrimitive.Root` | props `autohide?: "always"\|"not-last"\|"never"`, `hideWhenRunning?: boolean` (`thread.tsx:303-306`) | Action bars (§3.1/3.2) |
| `ActionBarPrimitive.Copy` | sets `s.message.isCopied` for 3s; gate icon with `AuiIf s.message.isCopied` | Copy verb |
| `ActionBarPrimitive.Edit` | enters edit mode → `composer.isEditing` true → `EditComposer` | Edit verb (user) |
| `ActionBarPrimitive.Reload` | calls `onReload(parentId)` → regenerate (fork sibling) | Reload verb (assistant) |
| `BranchPickerPrimitive` | `.Root hideWhenSingleBranch`, `.Previous`, `.Next`, `.Number`, `.Count` (`thread.tsx:417-444`) | Branch picker (§6) — kept |
| `ErrorPrimitive.Root` / `.Message` | render runtime/message error; `.Message` reads the error string (`thread.tsx:216-224`) | Error styling (§7.4) |
| `AuiIf` / `useAuiState` | `<AuiIf condition={(s)=>…}>` and `useAuiState((s)=>…)` selectors over `s.thread`/`s.message`/`s.composer`/`s.attachment` | Conditional rendering everywhere |

### 2.4 What we deliberately do NOT adopt

- `MessageTiming`, `ModelSelector`, attachments (`ComposerAddAttachment`/`Attachments`),
  `Quote`/`SelectionToolbar`, Lexical input, slash/mention adapters, `MessagePrimitive.Quote`,
  generative-UI `data` parts — all out of scope (Phase 26+; ux-spec Frame 07).
- `ActionBarPrimitive.FeedbackPositive/Negative`, `.ExportMarkdown`, `ActionBarMorePrimitive`
  — feedback ratings + export are **Phase 26** (25-UI-SPEC locks Copy/Edit/Reload only).
- `ToolFallback`/`ToolGroup` from the canonical UI — Aura keeps its own `ToolActivityCard`
  (D-02 raw view is locked and XSS-hardened; the canonical `ToolFallback` renders args/result
  generically without the security framing Aura needs).

---

## 3. Message rendering

### 3.1 User message (bubble, edit composer, action bar, branch picker)

> **Impl note (2026-06-18):** As-built, `UserMessage` is an **inline function** in
> `ExternalStoreChat.tsx:350-407`, not an extracted `messages/UserMessage.tsx`. The canonical
> "swap the whole message for an `EditComposer`" pattern was **not** adopted — edit mode is the
> inline `AuiIf composer.isEditing` branch (`:355-373`); there is no `EditComposer.tsx`. The
> action bar is hand-rolled `opacity-0 hover:opacity-100` (`:389`), not
> `ActionBarPrimitive autohide="not-last"`. And user text **is rendered through `MarkdownText`**
> (`:378-386`), contradicting the "a prompt is literal / NOT markdown" rule below.

Adopt the canonical `UserMessage` grid (`thread.tsx:349-373`) restyled to Editorial-Graphite.
The bubble is **right-aligned, `surface-2`, `radius-lg` with an asymmetric tail** (the tail
idea borrowed from Odysseus `style.css:1973` — `border-radius: var(--radius-lg) var(--radius-lg)
var(--radius-sm) var(--radius-lg)` so the bottom-right corner is tight), `shadow-2`, max-width
**80%** (design-system §6 "User message bubble" row). Enter animation
`fade-in slide-in-from-bottom-1 motion-safe:animate-in duration-150` (CSS-only; Aura has no
Framer Motion — design-system §5.4 motion tokens).

```tsx
// UserMessage.tsx (structure — tokens, not hex)
<MessagePrimitive.Root data-role="user"
  className="motion-safe:animate-in fade-in slide-in-from-bottom-1 grid grid-cols-[minmax(48px,1fr)_auto] gap-y-2 [&:where(>*)]:col-start-2">
  <div className="relative col-start-2 min-w-0">
    <div className="peer rounded-[var(--radius-lg)] rounded-br-[var(--radius-sm)] bg-surface-2 px-4 py-2.5 shadow-[var(--shadow-2)] wrap-break-word empty:hidden">
      <MessagePrimitive.Parts components={{ Text: UserText }} />
    </div>
    {/* Edit affordance sits to the LEFT of the bubble (canonical), touch-visible below lg */}
    <div className="absolute start-0 top-1/2 -translate-x-full -translate-y-1/2 pe-2 peer-empty:hidden">
      <UserActionBar/>     {/* Edit only (ActionBarPrimitive.Edit) */}
    </div>
  </div>
  <BranchPicker className="col-span-full col-start-1 row-start-2 justify-end" />
</MessagePrimitive.Root>
```

- `UserText` = `<div className="font-sans text-[0.9375rem] leading-[1.6] whitespace-pre-wrap text-text"><MessagePartPrimitiveText/></div>` (Body-L per design-system §3.4). User text is **NOT markdown** (a prompt is literal; preserves whitespace) — matches today's behavior.
- `UserActionBar` = `ActionBarPrimitive.Root autohide="not-last"` → `ActionBarPrimitive.Edit`
  (pencil icon, 44×44, `aria-label={t('chat.action.edit')}`). Copy stays in the locked verb
  set; the canonical places Copy on assistant only, but 25-UI-SPEC keeps **Copy on user too**
  (today's `UserMessage` has Copy+Edit) — so `UserActionBar` carries **Edit + Copy** (both
  44×44).
- **Edit mode** → `EditComposer` (§ below). `ThreadMessage` swaps the whole message:
  `if (composer.isEditing) return <EditComposer/>` (canonical `thread.tsx:90-97`). This is
  cleaner than today's `AuiIf composer.isEditing` inline branch and is what `onEdit` expects.
- `EditComposer.tsx` = a `ComposerPrimitive.Root` (bubble-shaped, `surface-2`, max-width 85%,
  right-aligned) with `ComposerPrimitive.Input autoFocus` + a footer row
  `ComposerPrimitive.Cancel` (ghost) + `ComposerPrimitive.Send` (accent, label
  `t('chat.edit.save')`). Send fires `onEdit` (fork + re-run). `aria-label={t('chat.edit.label')}`.

### 3.2 Assistant message (markdown, reasoning drawer, raw tool cards, error, streaming cursor)

> **Impl note (2026-06-18):** As-built, `AssistantMessage` is an **inline function**
> (`ExternalStoreChat.tsx:409-462`) with the markdown / reasoning-drawer / raw-tool-card part
> router (`:413-436`) and a hand-rolled hover action bar (`:445`). **Not shipped:** the
> streaming `●` in-progress cursor on the open text part, the `.shine` status-line shimmer, and
> the pre-first-token `<Skeleton>` (no `shine`/cursor/skeleton in `web/src/chat`). The
> running-status row is a plain static `role="status"` line (`:338-342`).

Adopt the canonical `AssistantMessage` (`thread.tsx:226-299`): a transparent block on `bg`
(NOT a bubble — assistant prose is full-measure editorial text, matching Claude/odysseus),
a `MessagePrimitive.Parts` router, a `MessageError`, then a footer row with the branch picker
+ action bar. Reading measure = `--thread-max-width` column (44rem desktop) + `text-[0.9375rem]
leading-[1.6]` Body-L.

**Part router (the core).** Use the `components` form of `MessagePrimitive.Parts` (Aura's
current approach — keeps the explicit per-type mapping), NOT the render-prop, because the
AG-UI part set is fixed (text / reasoning / tool-call) and the `components` form gives the
in-progress cursor for free on `Text`:

```tsx
<MessagePrimitive.Parts
  components={{
    Text: AssistantText,                 // markdown + streaming cursor (§3.3)
    Reasoning: ({ text }) => <ReasoningDrawer text={text} />,   // D-01 (§3.4)
    Empty: () => null,
    tools: {
      Fallback: ({ toolName, argsText, result, status }) => (
        <ToolActivityCard
          toolName={toolName}
          argsText={argsText}
          {...(typeof result === 'string' ? { result } : {})}
          isError={status?.type === 'incomplete'}
        />
      ),
    },
  }}
/>
```

- `AssistantText` renders `<MarkdownText/>` (§3.3) wrapped in the prose container, and
  appends the **streaming cursor** via `MessagePartPrimitiveInProgress` — the canonical
  `●` (`MessageParts.tsx:18-23`) tinted with `text-accent` (design-system §4.3 reserved item
  6 "in-flight streaming caret") and `motion-safe` pulse. This is the assistant-ui-grade
  streaming affordance the lane lacks today.
- **`.shine` streaming shimmer (06 §5.1-item-3, elysia E2).** *Alongside* the `●` caret (not
  replacing it), the **running-status line** — the `role="status"` row that reads
  `t('chat.streaming')` ("Generating response") / the active tool name (§5/§7) — carries a
  CSS-only gradient-sweep shimmer so the "thinking…" text shimmers while the model works.
  Reference: elysia `globals.css:114-130`. Concrete approach (CSS-only, no JS, no Motion lib):
  a `.shine` utility paints the text with a moving gradient via `background-clip: text` +
  `color: transparent`, animating `background-position`:
  ```css
  /* in web/src/index.css (or a chat-scoped layer) — token-pure, no raw hex */
  .shine {
    background: linear-gradient(100deg,
      var(--color-text-muted) 30%,
      var(--color-text) 50%,
      var(--color-text-muted) 70%);
    background-size: 200% 100%;
    -webkit-background-clip: text; background-clip: text;
    color: transparent;
    animation: shine-sweep 1.6s var(--motion-ease-standard) infinite;
  }
  @keyframes shine-sweep { from { background-position: 200% 0; } to { background-position: -200% 0; } }
  @media (prefers-reduced-motion: reduce) {
    .shine { animation: none; background: none; -webkit-background-clip: border-box;
             background-clip: border-box; color: var(--color-text-muted); }
  }
  ```
  It uses **named tokens only** (`--color-text(-muted)`, `--motion-ease-standard`) — no raw
  hex, so AC-TOKEN-1 still passes — and the reduced-motion branch falls back to flat
  `text-text-muted` (the §8 / 03 §8 global guard also covers it). The shimmer applies to the
  **status text only**, never to the streamed answer prose (the answer is read content, not a
  loading affordance) and never as a count-up tween (04 forbids count-ups for a11y).
- `Reasoning` → Aura's `ReasoningDrawer` (kept, §3.4). The render-fn receives
  `ReasoningMessagePartProps` = `{ type:'reasoning'; text:string; status }` (`MessagePartComponentTypes.ts:27`).
- `tools.Fallback` → Aura's `ToolActivityCard` (kept, §3.5). It receives
  `ToolCallMessagePartProps` (`toolName`, `args`/`argsText`, `result`, `status`, + the
  `addResult`/`resume`/`respondToApproval` callbacks we ignore — Aura's HITL is the separate
  approval card, NOT the per-tool approval gate). `isError` is derived from
  `status.type === 'incomplete'` (sseAdapter sets the tool result; an error tool shows the
  danger dot).
- **Optional part-grouping (note, not required).** The canonical tree wraps consecutive
  reasoning/tool parts in `MessagePrimitive.GroupedParts` + `groupPartByType` so a chain of
  tool calls collapses under one chevron. Aura's reducer emits **reasoning → text → tools**
  (one reasoning blob, then prose, then ordered tool cards), so grouping yields little today;
  **defer GroupedParts** to keep the router simple and the diff small. Documented so a
  reviewer doesn't "add it back" — revisit only if multi-step swarm runs produce long tool
  chains (Phase 26+).

**Footer (action bar + branch picker).** Canonical `thread.tsx:290-296`:
`<div className="ms-2 flex items-center"><BranchPicker/><AssistantActionBar/></div>` with the
`-mb`/`min-h` reservation trick (`thread.tsx:230-231`) so the autohiding action bar does not
shift layout. `AssistantActionBar` = `ActionBarPrimitive.Root hideWhenRunning autohide="not-last"`
→ Copy + Reload (44×44 `TooltipIconButton`-style ghost buttons, `text-text-muted` hover
`text-text`). **No feedback/export** (locked).

### 3.3 Markdown rendering (assistant prose) — the one security upgrade

Today assistant text is `MarkdownTextPrimitive` raw (no gfm, no chrome, **no sanitization**).
Build `MarkdownText.tsx` on the canonical `packages/ui` markdown component
(`markdown-text.tsx`), restyled to Editorial-Graphite, **plus an explicit sanitization layer**:

- `MarkdownTextPrimitive` (`@assistant-ui/react-markdown` 0.14.4) with
  `remarkPlugins={[remarkGfm]}` (tables, strikethrough, autolinks, task lists) and
  `rehypePlugins={[[rehypeSanitize, auraSchema]]}` — **this is the load-bearing add**.
- `auraSchema` (`markdownSanitize.ts`) = a `rehype-sanitize` schema derived from the
  GitHub default, hardened with the **Odysseus blocklist intelligence** (MIT — attribute in
  `THIRD_PARTY`): drop `script,iframe,object,embed,link,meta,style,base,form,svg,math`;
  strip all `on*` and `srcdoc` attributes; allow only `http(s)` + relative `#` URLs on `a[href]`
  (no `javascript:`/`vbscript:`/`data:`), with `target="_blank" rel="noopener noreferrer nofollow"`
  forced on external links. Rationale: assistant prose is **untrusted LLM output**; a model
  can be prompt-injected into emitting markdown with an HTML payload. assistant-ui's default
  markdown does NOT sanitize — react-markdown drops raw HTML by default, but remark-gfm +
  autolink + future `rehype-raw` reintroduce vectors, and a sanitize pass is the defensive
  floor (cf. odysseus `markdown.js:98-113` fixpoint sanitizer; we get fixpoint-equivalent
  safety from `rehype-sanitize`'s allowlist model, which is allow-not-deny).
- **Code blocks:** adopt the canonical `CodeHeader` (language label + Copy button), restyled:
  header `bg-surface-3 border-border`, `font-mono`, lowercase language; `<pre>` = `surface-3`
  well, `radius-md`, `overflow-x-auto`, `font-mono text-xs leading-relaxed`. No syntax
  highlighter dependency this phase (highlight.js is a perf/bundle cost — odysseus lazy-scan
  is O(n)/turn; defer to Phase 26 if desired). Inline `code` = `surface-3` chip, `font-mono`.
- **Links** render `text-accent-text underline underline-offset-2` (design-system §4.3
  reserved item 5).
- Tables/lists/blockquotes styled with tokens (the canonical class set, token-swapped).
- `memoizeMarkdownComponents` + `memo` (canonical) so streaming re-renders don't re-mount the
  whole markdown tree per delta (React-compiler is on; still memoize the components map).

> **Inline-citation hovercard — deliberate disagreement with 06 §5.1-item-1 (DEFER, not
> adopt).** Sibling spec `06-candidates-eval-SPEC.md` calls elysia's inline-citation pipeline
> (`MarkdownFormat.tsx:39-111`, `CitationBubble.tsx:25-56`) the "**top adopt**" for a
> Deep-Search cockpit. **This SPEC consciously declines to pull it forward, and the
> disagreement is intentional — not an oversight.** Rationale:
> - **Phase-26 boundary (25-UI-SPEC).** A citation is a *typed display* affordance: it needs a
>   source registry (id → title/url/snippet), a click-to-source target, and a hovercard chrome
>   — the same typed-payload machinery (`switch(payload.type)` web_result/document/…) that
>   25-UI-SPEC explicitly fences behind **Phase 26**. Building a half-citation now (a chip with
>   no backing source object) is the "half-built" anti-pattern this SPEC avoids; building the
>   full one means importing the Phase-26 typed-display layer early, which the milestone scope
>   forbids. The clean seam is: **raw tool cards now (D-02), typed displays + citations together
>   in Phase 26.**
> - **No backend contract for it today.** The AG-UI/SSE reducer (`sseAdapter.ts`, KEPT verbatim)
>   emits text / reasoning / tool-call parts only; there is **no `source`/citation part and no
>   source registry** in the stream. elysia's plugin works because Weaviate's backend annotates
>   `[n]` markers against a sources array; Aura has no such array on the wire. Adopting the
>   marker→chip transform now would chip *unbacked* `[n]` text — actively worse than leaving it
>   as prose.
> - **What this SPEC commits to instead.** The `auraSchema` sanitizer keeps `[n]` markers as
>   plain text (they pass through untouched), and §14 records the citation hovercard as
>   **explicitly in Phase-26 scope** so a planner reading 01 + 06 together has *one* marching
>   order: the elysia `rehypeCitations` plugin shape (positional splice, not end-append; do NOT
>   hide images — fixing elysia's two bugs) is the **chosen reference** for that Phase-26 work,
>   and the assistant-ui hovercard primitive (not raw Radix) is the chrome. Recorded here so it
>   is neither reinvented nor accidentally pulled forward.

### 3.4 Reasoning drawer (D-01)

**Keep `ReasoningDrawer.tsx` logic verbatim** — it is the locked D-01 affordance with a
persisted show/hide preference (`readReasoningPref`/`writeReasoningPref`), correct
`aria-expanded`/`aria-controls`/`aria-pressed`, a `motion-reduce:transition-none` chevron,
and `text.length===0 → null`. Token re-skin only:

- Container `border-border bg-surface radius-md` (was `surface-2`; design-system §6
  "Reasoning drawer" row = `surface`).
- Toggle label `text-accent-text` when expandable (design-system §6 "toggle text-accent-text").
- Body `font-sans text-xs leading-relaxed text-text-muted` (kept).
- Chevron rotation transition uses `--motion-dur-fast`/`--motion-ease-standard`.

It mounts as the **first** assistant part (sseAdapter orders reasoning before text), so the
drawer sits above the prose — the canonical "chain-of-thought then answer" reading order.

### 3.5 Raw tool-activity card (D-02) — kept raw/XSS-safe AND enriched

> **Impl note (2026-06-18):** A **subset** of this enrichment shipped in
> `ToolActivityCard.tsx`: status-tinted left-rule + pill + dot (`:15-31,63,67-76`) and
> **auto-expand-while-running** with a `userToggled` intent-guard (`:53-59,82`); the raw blob
> stays escaped text in `<pre>` (`:108-114`). **Not shipped:** the **elapsed-time readout**, the
> **indented subagent child rows**, and **auto-collapse-once-on-settle** (the card re-seeds
> `expanded` from status on each transition `:56-59` rather than collapsing exactly once when a
> running call settles). The base card stayed `bg-surface-2 border-border`, no flood-fill — as
> specified.

**Keep `ToolActivityCard.tsx` security model verbatim** — it is the locked D-02 raw view and
is **security-load-bearing**: the raw blob renders as TEXT inside `<pre>` (React escapes
children), never markdown, never `dangerouslySetInnerHTML`; the XSS guard is asserted in
`ToolActivityCard.test.tsx`. There is deliberately **no typed per-type routing** (that is
Phase 26). The card stays raw.

**Enrichment (06 §5.1-item-2, openhuman H1 — patterns only, GPL).** The raw-blob safety and
the openhuman timeline affordances are *not* mutually exclusive: the card keeps its escaped
`<pre>` body and *gains* status-tinting, auto-expand-while-running, elapsed time, and nested
subagent rows. This is a HIGH-VALUE 06 adopt aimed at this exact section; it was previously
dropped by a "keep verbatim" note — now folded in. Reference behavior:
`openhuman ToolTimelineBlock.tsx:38-114,117,140-161` (re-implemented from the description, no
code lifted — GPLv3).

**Status → semantic-token mapping (03 §4.2; icon + text label, never color alone — WCAG 1.4.1
/ 03 §4.3).** The status dot already maps correctly; extend it to a **status-tinted pill** and
a subtle card-surface tint:

| `ToolStatus` (`toolStatus.ts`) | Trigger | Dot + pill | Card tint (left-rule) | Status label |
|---|---|---|---|---|
| `running` | result undefined, no error | `bg-warning` dot, pill `text-warning` on `bg-warning/10` | `border-l-2 border-l-warning` | `t('chat.tool.status.running')` |
| `done` | result present, `!isError` | `bg-success` dot, pill `text-success` on `bg-success/10` | `border-l-2 border-l-success` | `t('chat.tool.status.done')` |
| `error` | `isError` (status `incomplete`) | `bg-danger` dot, pill `text-danger` on `bg-danger/10` | `border-l-2 border-l-danger` | `t('chat.tool.status.error')` |

The base card stays `bg-surface-2 border-border radius-md` (design-system §6 "Tool-activity
card"); the status-tint is the **left-rule + the pill background only** — the card body does
NOT flood-fill a status color (premium-calm; accent/status scarcity, 03 §4.3). Tints are
`/10`-alpha so they read as a quiet wash, not a banner.

**Auto-expand the running call; collapse settled ones.** The expander state seeds from status
instead of always-collapsed:
- A `running` card mounts **expanded** (`expanded` initial = `status === 'running'`), so the
  operator watches the streamed args/partial result live — this is the openhuman
  "auto-expand-running" behavior and the single biggest legibility win.
- When a card **settles** (`running → done|error`), it **auto-collapses once** to a one-line
  summary (so a long finished blob doesn't dominate the scroll), UNLESS the operator has
  manually toggled it — a `userToggled` ref gates the auto-collapse (the same intent-guard
  odysseus uses for the sidebar, 02-spec §3). Manual expand/collapse always wins thereafter.
- Implementation note: derive the auto-state in an effect that watches `status`; never fight a
  user toggle. `prefers-reduced-motion` removes the height/chevron transition only.

**Elapsed time (optional, shown when derivable).** When the part carries timing (a
`startedAt`/`finishedAt` the reducer can stamp from the AG-UI `TOOL_CALL_START`/`…_END`
frames), show a `font-mono tabular-nums text-text-faint text-[0.6875rem]` elapsed readout next
to the status pill: live `…s` ticking while `running` (a `setInterval` cleared on settle / on
unmount — no leak), frozen at the final duration when settled. **Graceful absence:** if the
reducer does not yet stamp timing, the readout is simply omitted (no placeholder, no `0s`) —
this keeps `sseAdapter.ts` un-edited for v1 and lets timing land later without a card rewrite.
The elapsed readout is `aria-hidden` while ticking (it must not spam `aria-live`).

**Nested subagent / child rows (swarm).** Aura runs swarms (ParallelAgent, PRD Slice 0.9/3),
so a tool call can spawn child activity. When the part exposes child tool entries, render them
as **indented rows inside the same card** (`ps-4 border-l border-border`), each its own
status-tinted line, so a swarm fan-out reads as one parent card with its children — not N
flat cards. Children inherit the same raw/escaped-text discipline. **Graceful absence:** with
no children the card is exactly today's single-row shape. (Deep nesting beyond one level is
Phase 26; one level covers the common swarm case.)

**Unchanged invariants.** Token re-skin: expandable blob `<pre>` background → `bg-surface-3`
(the "code-block well" token), `font-mono text-muted`. Expander button stays **44×44**. The
raw blob is still escaped text; no markdown, no HTML. The `toolStatus.ts` mapping and the
`chat.tool.status.*` i18n keys are reused.

### 3.6 Inline approval card placement in the Thread (D-03/D-05/D-06)

> **Impl note (2026-06-18):** As-built, `ThreadApprovalCards` is mounted by the shell as a
> **sibling of the lane** (`AppShell.tsx:131`), below `ExternalStoreChat` — not inside a
> `ChatLane` reading-measure column after the message group as specified here. The
> `InlineApprovalCard` CardShell re-skin is partial: `text-on-accent` is used
> (`InlineApprovalCard.tsx:152`) but the shell is still `border-accent/40` (`:229`), **not** the
> `border-l-2 border-l-accent` + `border-strong` accent left-rule below. Verbs/terminal states
> are unchanged (locked, as required).

`ThreadApprovalCards` must **stay** — it is the "perfectly like Claude Code" in-thread HITL.
Placement decision:

- The card renders **after the message group, before the viewport footer**, inside the same
  reading-measure column (so it aligns with assistant prose, not full-bleed). It is NOT a
  message part (it is sourced from the cross-thread approval poll filtered to the active
  thread, `ThreadApprovalCards.tsx`), so it cannot live inside `MessagePrimitive.Parts`.
- It is the **last thing in the scroll region** when a `requires-action` interrupt is pending,
  so the operator's eye lands on it after the (partial) assistant turn — exactly where Claude
  Code puts the approval prompt. `turnAnchor="bottom"` + autoScroll keeps it in view as it
  appears.
- Restyle the card (`InlineApprovalCard` CardShell) to design-system §6 "Approval card
  (inline)": `surface-2`, `border-strong` with an **accent left-rule** (`border-l-2
  border-l-accent`), `radius-lg`, `shadow-2`; Answer = `bg-accent text-on-accent`, Decline
  neutral, Cancel `text-danger`. Terminal states (§6 row): answered `success`, declined
  `success`, cancelled `danger`, expired `warning` — each with icon + label (kept).
- `isStreaming` (gates the inline Cancel-confirm) = the lane's `isRunning`; `onResolved` =
  the shell's resume-nonce bump (continue-after-resume). **Wiring unchanged.**

```tsx
// In ChatLane.tsx, inside the reading column, after <ThreadPrimitive.Messages>:
<div className="mx-auto w-full max-w-(--thread-max-width)">
  <ThreadApprovalCards
    conversationId={threadId}
    isStreaming={isRunning}
    onResolved={onApprovalResolved}   // → shell bumps resumeNonce
  />
</div>
```

---

## 4. Scroll management

> **Impl note (2026-06-18):** NOT implemented. The viewport is still a bare
> `overflow-y-auto` (`ExternalStoreChat.tsx:320`) — no `turnAnchor`/`autoScroll` opt-in, no
> `ViewportFooter`, no `ThreadPrimitive.ScrollToBottom`, no `ThreadScrollToBottom.tsx`. The
> entire §4 contract remains pending.

The current bare `overflow-y-auto` is replaced by the canonical viewport, which is the single
biggest "feels like a real chat" upgrade.

- `ThreadPrimitive.Viewport` (default `turnAnchor="bottom"`, `autoScroll` defaults true) owns
  scrolling and runs `useThreadViewportAutoScroll` internally: it **sticks to the bottom while
  streaming**, but **stops auto-scrolling the moment the operator scrolls up** to read history
  (and resumes when they scroll back to the bottom) — the behavior odysseus hand-rolls with a
  300px bail (`ui.js:461-487`), provided here for free. We do NOT hand-roll a scroll listener.
- `scrollToBottomOnRunStart`/`OnInitialize`/`OnThreadSwitch` all default true: switching
  conversations (the sidebar) and loading history both land at the latest turn; a new run
  jumps to the bottom. These match cockpit expectations; leave defaults.
- The viewport is `relative flex flex-1 flex-col overflow-x-auto overflow-y-scroll
  scroll-smooth` with `overscroll-contain` (so the body never rubber-bands behind it on
  mobile, shell spec §2.3). `min-h-0` on the lane section lets the viewport shrink-to-scroll
  (shell spec §0/§2.3 grid-collapse fix).
- `ThreadScrollToBottom` (`ThreadPrimitive.ScrollToBottom`) renders a pill button that is
  `disabled:invisible` and auto-disabled when `isAtBottom` (the hook returns `null`, so the
  button is non-functional/invisible at bottom). Style: `absolute -top-12 self-center
  rounded-[var(--radius-pill)] bg-surface-2 border-border shadow-3 p-3` with an
  `ArrowDown` SVG (24×24, `stroke=currentColor`, `aria-hidden`) and `aria-label=
  {t('chat.scrollToBottom')}`. It lives in the `ViewportFooter` (desktop) or just above the
  dock (mobile) so it floats over the last message. `motion-reduce` disables the
  `scroll-smooth`.
- **`scroll-margin`** on focusable message controls so the sticky footer/dock never covers a
  keyboard-focused element (25-UI-SPEC a11y).

---

## 5. Composer (multiline / send / stop / keyboard / mobile dock)

> **Impl note (2026-06-18):** The composer was restyled + Send↔Stop kept (`Composer.tsx`), but
> **not relocated** to `BottomDock` and **not** placed under a hoisted provider. It still renders
> inside the lane's `ThreadPrimitive.Root` (`ExternalStoreChat.tsx:344`) and the
> `AssistantRuntimeProvider` is still owned by `ExternalStoreChat` (`:318`). The shell's
> `BottomDock` holds the mode selector + `RuntimeFooter` only (`AppShell.tsx:142-144`).

The composer is **kept** (it already implements the locked behavior) and **relocated** to
`BottomDock` (shell spec §2.4/§6) under the hoisted provider. Restyle to canonical shell +
Editorial-Graphite:

- `ComposerPrimitive.Root` = the composer shell (`bg-surface-3` well, `border-strong`,
  `radius-lg`, `focus-within:ring-2 ring-ring`, design-system §6 "Composer" row). On focus
  the ring is the accent (`--color-ring`). The dock owns the top border (shell spec §9), so
  the composer drops its own `border-t`.
- `ComposerPrimitive.Input` = `rows={1}`, auto-grows to `max-h-32` (desktop) / **`40svh`
  cap on mobile** (shell spec §6.5 — a long draft never eats the screen), `min-h-[var(--row-h)]`,
  `resize-none`, `font-sans text-sm leading-relaxed text-text placeholder:text-text-faint`,
  `aria-label={t('chat.composer.placeholder')}` (placeholder `Ask Aura`, kept).
- **Keyboard (built into `ComposerPrimitive.Input`):** Enter → send, Shift+Enter → newline,
  Esc → cancel an active run. **IME safety:** the primitive guards on `isComposing`
  internally; we document this (odysseus' `!e.isComposing`, `chat.js:3431`) so a reviewer
  doesn't add a redundant handler. No custom `onKeyDown`.
- **Send↔Stop swap (kept):** `AuiIf !s.thread.isRunning` → `ComposerPrimitive.Send`
  (accent fill `bg-accent text-on-accent`, the ONE primary CTA, `radius-pill` icon button,
  `ArrowUp` SVG, 44×44, `disabled` when `composer.isEmpty`). `AuiIf s.thread.isRunning` →
  `ComposerPrimitive.Cancel` (a **stop square**, `SquareIcon`-equivalent SVG, neutral fill so
  accent stays scarce — design-system §4.3; the shipped Aura version uses a danger-tinted
  outline, keep that or the canonical neutral square — pick one in plan, both satisfy the
  contract). `aria-label`s: `chat.composer.sendAria` / `chat.composer.stopAria` (kept).
- A **`send-pending` micro-state** (odysseus `style.css:2390-2398`) is OPTIONAL: a brief pulse
  on the button between click and `RUN_STARTED` to cover the request latency. Nice-to-have;
  `motion-safe` only. Not required for acceptance.
- The composer's running affordance: a `role="status"` row above it announces
  `t('chat.running')` (+ active tool name when present, 25-UI-SPEC) — kept from today's
  `isRunning` row, moved into the dock.

---

## 6. Branch picker over the path-aware backend (D-09)

`BranchPicker.tsx` is **kept** (it binds `BranchPickerPrimitive` to the external-store
runtime's `getBranches`/`switchToBranch`, which the runtime derives from the DB-walked message
list `useAuraChatRuntime` feeds via `setMessages` — no hand-rolled state machine). Restyle to
the canonical chevron pattern (`thread.tsx:417-444`):

- `BranchPickerPrimitive.Root hideWhenSingleBranch` wrapped in `AuiIf message.branchCount>1`
  (kept — double-guard so a non-branched turn shows nothing).
- `.Previous` / `.Next` = chevron-left/right SVG icon buttons (24×24, `stroke=currentColor`,
  `aria-hidden`), `disabled:opacity-40`, 24×24 min target (inline secondary control), ghost
  `text-text-muted` hover `text-text`, focus ring.
- The `Number / Count` readout = `font-mono tabular-nums text-accent` (design-system §4.3
  reserved item 6 "branch-picker active indicator"), in an `aria-live="polite"` span so a
  branch switch is announced without moving focus (kept).
- Placement: assistant footer (after prose) and the user-message footer row (canonical
  `thread.tsx:367-370`) — both already wired by `onEdit`/`onReload`.

No backend change: edit (user turn) and regenerate (assistant turn) fork sibling branches via
the existing `/api/conversations/{id}/edit` `diverge_seq` path; the runtime models the tree;
the picker navigates it.

---

## 7. Every state — empty / loading / streaming / error / requires-action / mobile

| State | Trigger | Rendering |
|---|---|---|
| **Empty thread** | `s.thread.isEmpty` | `ThreadWelcome` (§7.1): centered hero in `font-display` Display-XL `t('chat.empty.thread.heading')` (`Ask Aura`) + `text-text-muted` body. Optional suggestion chips. |
| **Loading history** | conversation switch / first load | The viewport's `scrollToBottomOnInitialize` lands at the latest turn. While the snapshot fetch is in flight (if used), show the **chat skeleton** (`AppSkeletons`/`skeleton.css` — re-skins to graphite automatically, design-system §7.2): 2–3 ghost bubbles, `role=status aria-busy`, `sr-only` "Loading conversation". (Today the lane has no snapshot fetch — history arrives via the runtime; if a snapshot endpoint is wired later, this is its state.) |
| **Streaming** | `s.thread.isRunning` | Assistant text grows with the `●` in-progress cursor (`text-accent` pulse, §3.2); the `role="status"` running row shows `t('chat.running')` + active tool **with the `.shine` shimmer** (§3.2, 06 E2); **before the first token**, an `AppSkeletons`-style `<Skeleton>` shimmer line stands in for the not-yet-arrived assistant turn (E2 "pre-first-token skeleton", re-skins to graphite via 03 §7.2, `role=status aria-busy`); action bars `hideWhenRunning`; the composer shows Stop. Viewport sticks to bottom unless the operator scrolled up. All shimmer/pulse `motion-reduce:`-gated. |
| **Error (stream/turn failed)** | message `status.type==='incomplete' reason:'error'` (sseAdapter `RUN_ERROR`) | `MessagePrimitive.Error` → `ErrorPrimitive.Root` styled `border-danger bg-danger/10 text-danger radius-md p-3 text-sm` + `ErrorPrimitive.Message` (line-clamp-2). The reducer also routes `RUN_ERROR` into an error text part with the **sanitized** backend message (e.g. "thread already has an in-flight run", a 409/400 body, or a redacted 500) — so the operator sees the cause (sseAdapter `errorDetail`). Copy: `t('chat.error.stream')`. A **Retry** affordance = the assistant `ActionBarPrimitive.Reload` (regenerate the last turn). |
| **requires-action (interrupt)** | message `status.type==='requires-action'` (sseAdapter `RUN_FINISHED outcome:interrupt`) | The inline `ThreadApprovalCards` renders the pending approval (§3.6) as the last item; the composer is NOT disabled (the operator can still type, but the run is paused awaiting the verb). |
| **Mobile (390px)** | `<lg` | §9. |

### 7.1 ThreadWelcome (empty state)

Editorial, premium-calm (NOT the canonical "Hello there!"):

```tsx
<div className="my-auto flex grow flex-col items-center justify-center px-4 text-center">
  <h1 className="font-display text-[clamp(1.75rem,1.4rem+1.4vw,2.5rem)] leading-[1.08] tracking-[-0.02em] text-text
                 motion-safe:animate-in fade-in slide-in-from-bottom-1 [animation-duration:var(--motion-dur-slower)] [animation-timing-function:var(--motion-ease-expo)]">
    {t('chat.empty.thread.heading')}      {/* "Ask Aura" — Display-XL */}
  </h1>
  <p className="mt-2 max-w-sm font-sans text-sm text-text-muted">{t('chat.empty.thread.body')}</p>
  {/* Optional: ThreadPrimitive.Suggestions chips (deferred unless suggestions exist) */}
</div>
```

The reveal uses the design-system §5.4 `--motion-ease-expo` premium curve; `prefers-reduced-motion`
makes it static (design-system §8 global guard). Suggestion chips are **deferred** unless the
backend supplies starter prompts (none today) — documented so it isn't half-built.

---

## 8. Accessibility contract (WCAG 2.2)

Extends the shipped Phase-23 a11y floor (`eslint-plugin-jsx-a11y` recommended is a blocking
gate) and the 25-UI-SPEC contract. Net obligations for the chat lane:

- **1.3.1 / 4.1.2 Name/role/value:** assistant/user `MessagePrimitive.Root` carry `data-role`;
  every icon button (Send, Stop, Scroll-to-bottom, Edit, Copy, Reload, branch chevrons,
  reasoning/tool expanders) is a native `<button>` with `aria-label` and an `aria-hidden` SVG.
- **1.4.1 Use of color:** tool status + approval terminal states pair the color dot with a
  text label (kept); the streaming cursor is decorative (`aria-hidden`).
- **2.1.1 / 2.1.2 Keyboard / no trap:** composer Enter/Shift+Enter/Esc; action bars,
  branch picker, expanders all keyboard-operable; the edit composer's Cancel/Send are
  tab-ordered; the inline approval card's option/verb buttons are tab-ordered and reachable
  when the thread opens.
- **2.4.7 Focus visible:** `focus-visible:outline-2 focus-visible:outline-offset-2
  focus-visible:outline-accent` (the `--color-ring`) on every interactive element; never
  `outline:none` without replacement. `scroll-margin` so the sticky footer/dock never covers
  a focused element.
- **2.5.8 Target size (AA):** Send/Stop/Scroll-to-bottom/Edit/Copy/Reload **44×44**; inline
  secondary controls (branch chevrons) ≥24×24.
- **4.1.3 Status messages / live regions:** streaming assistant text in an
  `aria-live="polite"` region (the assistant message content); the running-status row
  `role="status"`; stream/resume errors `role="alert"`; the branch readout `aria-live="polite"`;
  the cross-thread approval count announces politely (must not interrupt). The `●` cursor is
  NOT a live region (decorative).
- **`aria-invalid` omit-when-valid:** `aria-invalid={hasError || undefined}` (kept in the
  approval free-text; applies to the edit composer if it ever validates).
- **2.3.3 Animation from interactions / reduced motion:** the message enter animation,
  streaming cursor pulse, scroll-smooth, welcome reveal, drawer/expander transitions all under
  `motion-safe:` / disabled by the design-system §8 `prefers-reduced-motion` global guard.
- **1.4.10 Reflow (320px):** the reading column is `max-w-(--thread-max-width)` but
  `min-w-0` + `wrap-break-word` + the markdown `<pre>` `overflow-x-auto` guarantee no
  horizontal page scroll at 320px (long tool blobs scroll *within* the card, not the page).
- **1.4.4 Resize text / 1.4.12 Text spacing:** all sizes via tokens/`rem`; the density model
  scales the base.
- **i18n:** every string via `t()`, present in en+it (§11).

---

## 9. Mobile-first (390px) specifics

Coordinated with shell spec §2.3/§2.4/§6 (the dock contract). The chat lane's own
mobile rules:

- **The lane fills the viewport** between header and `BottomDock` (shell spec §2.1). The
  composer is in the dock (pinned by the grid track, safe-area + keyboard handled there), so
  the lane is *just* the scroll viewport — it can never be crushed to 0 (shell spec AC-MOBILE-2:
  chat region ≥45% vh).
- **Bubbles:** user bubble `max-w-[80%]` (design-system) / **85%** is acceptable (odysseus
  precedent) — pick 80% to match the design-system row; tail on the bottom-right. Assistant
  prose is full reading-column width (no bubble), `text-[0.9375rem] leading-[1.6]` (premium
  reading measure holds on mobile).
- **Reading column:** `--thread-max-width` is 44rem on desktop; on mobile the column is
  `w-full px-3` (the viewport is narrower than 44rem so the max is a no-op). Horizontal padding
  `px-3` (mobile) → `px-4` (sm+).
- **Scroll-to-bottom button** floats just above the dock (`bottom-[calc(theme(spacing.2)+env(safe-area-inset-bottom))]`
  if rendered outside the viewport footer on mobile) so it clears the composer.
- **Tap targets:** all 44×44 (action bars are touch-visible below `lg` — they do NOT rely on
  hover; use `autohide="not-last"` which shows on the last message + on focus/tap, and force
  `opacity-100` below `lg` via a `lg:` hover gate so touch users see Edit/Copy/Reload).
  This is the chat-lane analog of the sidebar's "actions reachable on touch" fix (shell spec §4.2).
- **The reasoning drawer / tool card** expand inline (no off-canvas) — they are already
  collapse-in-place, which is correct on mobile.
- **No horizontal overflow** at 390px (shell spec AC-MOBILE-4) — guaranteed by `min-w-0` +
  `wrap-break-word` + in-card `<pre>` scroll.

---

## 10. Incremental migration plan (keep sseAdapter + runtime, swap presentation)

Sequenced so the lane stays green at every step and the working wiring is never at risk.

1. **Extract the runtime hook (no behavior change).** Move all of `ExternalStoreChat`'s logic
   into `useAuraChatRuntime.ts`. `ExternalStoreChat` becomes a thin wrapper that calls the
   hook, renders `<AssistantRuntimeProvider runtime>` + the *current* JSX. Run the existing
   `ExternalStoreChat.test.tsx` — must stay green (pure refactor). Commit.
2. **Verify the provider hoist against 0.14.22.** Spike: render `ComposerPrimitive.Root` in a
   sibling subtree of `ThreadPrimitive.Root` under one `AssistantRuntimeProvider`; confirm
   Send→`onNew` and `useAuiState(s.thread.isRunning)` work across the subtree boundary (they
   do via React context, but confirm — shell spec blocker #3). Commit the spike note.
3. **Build the new presentation tree behind the same runtime.** Add `ChatLane.tsx` +
   `messages/*` + `MarkdownText.tsx` + `markdownSanitize.ts` + `ThreadWelcome.tsx` +
   `ThreadScrollToBottom.tsx`. Re-skin `ReasoningDrawer`/`ToolActivityCard`/`BranchPicker`/
   `Composer`/`InlineApprovalCard` (token-only). Keep `ExternalStoreChat`'s old JSX importing
   the runtime hook so both presentations exist transiently.
4. **Swap the shell to the new tree.** Per shell spec §5, `AppShell` hoists the provider and
   renders `<ChatLane>` (lane, no composer) + `<BottomDock>` (composer). Point the shell at
   `ChatLane`; delete `ExternalStoreChat`'s old JSX (or keep the shim for the test until the
   test is migrated to mount `AssistantRuntimeProvider + ChatLane + Composer` directly).
5. **Migrate tests** to the new structure (the SSE-fixture streaming test asserts the same
   ground truth: reasoning drawer + tool card + assistant text render from the golden frames).
6. **Add the new tests** (§13) + sanitization tests + scroll/cursor tests.
7. **Run gates:** `tsc --noEmit`, `eslint --max-warnings=0` (incl. jsx-a11y), vitest ≥85%,
   Stryker ≥70% on `markdownSanitize.ts` (+ `useAuraChatRuntime` divergeSeq math), build,
   rebuild `web/dist` (i18n + embed). CI web-e2e (the shell spec's Playwright `mobile`
   project) exercises the lane at 390px.

**Invariant:** `sseAdapter.ts` is never edited; the runtime callbacks are never re-implemented;
the locked verbs/cards are never changed in behavior. If any step needs a reducer change, STOP
— it means the presentation swap has leaked into the contract (it should not).

---

## 11. New / changed i18n keys (en + it, same commit)

Existing `chat.*` keys (verified `resources.ts:39-87`) are reused: `composer.placeholder|send|
sendAria|stop|stopAria`, `running`, `empty.thread.heading|body`, `error.stream`,
`reasoning.show|hide`, `tool.showRaw|hideRaw|status.*`, `branch.label|previous|next`,
`action.copy|copied|edit|reload`, `edit.save|cancel|label`. The approval keys
(`approval.*`) are reused verbatim.

New keys (add under `chat.*` in both `en` and `it`):

```
chat.scrollToBottom        "Scroll to bottom"            / "Scorri in fondo"
chat.streaming             "Generating response"         / "Generazione in corso"   (sr-only on the ● cursor region)
chat.markdown.copyCode     "Copy code"                   / "Copia codice"
chat.markdown.codeCopied   "Copied"                      / "Copiato"
chat.loading               "Loading conversation"        / "Caricamento conversazione"  (skeleton sr-only)
chat.error.retry           "Retry"                       / "Riprova"
chat.empty.suggestionsLabel "Try asking"                 / "Prova a chiedere"      (only if suggestions ship)
```

(`chat.empty.thread.*`, `chat.running`, `chat.error.stream` already exist — reuse.) Rebuild
`web/dist` after the copy change (project i18n discipline).

> **Enrichments add no new i18n keys.** The tool-card status-tint/auto-expand/subagent rows
> reuse `chat.tool.status.{running,done,error}` + `chat.tool.{showRaw,hideRaw}`; the elapsed
> readout is a numeric `font-mono` value (no translatable string); the `.shine` shimmer wraps
> the existing `chat.streaming`/`chat.running` status text. (The deferred citation hovercard's
> bubble keys land in **Phase 26**, not here.)

---

## 12. Acceptance criteria

Machine-checkable. **AC-WIRE-* guard the "don't break the working backend" invariant.**

| # | Criterion | Verified by |
|---|---|---|
| **AC-WIRE-1** | `sseAdapter.ts` is byte-identical to pre-rebuild; no edit to the reducer or fetch pump. | `git diff` shows no change to `sseAdapter.ts`. |
| **AC-WIRE-2** | The runtime is still `useExternalStoreRuntime` with `onNew/onEdit/onReload/onCancel/convertMessage`; the golden-frame streaming test (reasoning drawer + tool card + assistant text) passes against the same `internal/agui/testdata/golden-events.json`-shaped fixtures. | vitest (migrated `ChatLane` streaming test). |
| **AC-WIRE-3** | Continue-after-resume still works: bumping `resumeNonce` re-drives a no-message `POST /agent/run` and folds the resumed turn into the lane. | vitest (resume-nonce test, ported). |
| **AC-WIRE-4** | Inline approval card still renders in-thread, with the 3 verbs (Answer/Decline/Cancel) and terminal states; `onResolved` fires the resume; Decline sends no content. | vitest (approval card tests kept). |
| **AC-RENDER-1** | Assistant text renders as **sanitized** markdown: a `[x](javascript:alert(1))` link is neutralized (no `javascript:` href), `<script>`/`<img onerror>` in prose do not execute, and a gfm table renders styled. | vitest (`markdownSanitize` unit + a render test). |
| **AC-RENDER-2** | The raw tool blob still renders as escaped text in `<pre>` (no HTML execution) — the D-02/XSS guard. | existing `ToolActivityCard.test.tsx` kept green. |
| **AC-RENDER-3** | A streaming assistant turn shows the in-progress `●` cursor (`text-accent`) on the open text part; it disappears on `complete`. | vitest (status `running`→`complete`). |
| **AC-RENDER-4** | The running-status line carries the `.shine` shimmer while streaming (gradient-clip text, named tokens only); under `prefers-reduced-motion: reduce` the shimmer is flat `text-text-muted` (no animation). A `<Skeleton>` stands in before the first token. | vitest (matchMedia + class assertion) / Playwright `reducedMotion`. |
| **AC-TOOL-1** | A `running` tool card mounts **expanded**; on settle (`running→done\|error`) it **auto-collapses once** to the summary row UNLESS manually toggled (the `userToggled` guard wins). | vitest (status transition + a user-toggle case). |
| **AC-TOOL-2** | The tool card is **status-tinted**: `running→warning`, `done→success`, `error→danger` as a left-rule + pill (icon + text label, never color alone); the card body is not flood-filled; the raw blob still renders as escaped text in `<pre>`. When the part carries timing, an elapsed `font-mono tabular-nums` readout shows; when it does not, the readout is omitted (no `0s`). Child subagent entries, when present, render as indented status-tinted rows inside the same card. | vitest (each status → tokens; elapsed present/absent; child rows). |
| **AC-SCROLL-1** | `ThreadPrimitive.ScrollToBottom` is present; the button is invisible/disabled when at bottom and appears when scrolled up. | vitest (mock viewport `isAtBottom`) + Playwright. |
| **AC-SCROLL-2** | While streaming, the viewport stays pinned to the bottom UNLESS the user scrolled up (no yank-down on manual scroll). | Playwright (scroll up during a mocked stream; assert scrollTop unchanged). |
| **AC-STATE-1** | Empty thread shows `ThreadWelcome` (`font-display` heading `Ask Aura`); a turn replaces it. | vitest. |
| **AC-STATE-2** | A `RUN_ERROR` turn renders `MessagePrimitive.Error` with the sanitized backend message + a Reload (retry) affordance. | vitest (error-frame fixture). |
| **AC-A11Y-1** | jsx-a11y clean; every icon button has `aria-label`, SVG `aria-hidden`; streaming text in `aria-live="polite"`; running row `role="status"`; errors `role="alert"`. | lint gate + vitest. |
| **AC-A11Y-2** | Reduced-motion: enter animation, `●` pulse, scroll-smooth, welcome reveal are static under `prefers-reduced-motion: reduce`. | vitest (matchMedia) / Playwright `reducedMotion:'reduce'`. |
| **AC-TOKEN-1** | No raw hex in the chat lane components (`grep -nE '#[0-9A-Fa-f]{3,6}'` in `web/src/chat/**/*.tsx` + `web/src/approvals/*.tsx` returns nothing except token files); `text-[#0B0E14]` is replaced by `text-on-accent`. | grep gate (design-system AC-4). |
| **AC-MOBILE-1** | At 390px, action bars (Edit/Copy/Reload) are reachable without hover (touch-visible below `lg`); bubbles ≤80% width; no horizontal page scroll. | Playwright `Pixel 7` (shell spec mobile project). |
| **AC-I18N-1** | Every new string in en+it; switching to `it` relabels scroll-to-bottom / streaming / retry. | vitest (language switch). |
| **AC-COV-1** | vitest ≥85% (statements/branches/functions/lines); Stryker ≥70% killed on `markdownSanitize.ts` + the `useAuraChatRuntime` divergeSeq math. | `npm test` + `npm run mutation`. |

---

## 13. Test plan

**Unit (vitest + @testing-library/react + jsdom):**

- `markdownSanitize.test.ts` — **highest-mutation-value file** (Stryker target). Table-driven:
  `javascript:`/`vbscript:`/`data:` href → stripped; `<script>`/`<iframe>`/`<svg onload>`/
  `<img onerror>` → removed; `on*` attrs → stripped; relative `#anchor` + `http(s)` → kept;
  external link gets `rel="noopener noreferrer nofollow" target="_blank"`; gfm table/strike/
  task-list pass through. Mutation-resistant assertions (exact href, exact tag presence/absence).
- `MarkdownText.test.tsx` — renders a fenced code block with the CodeHeader + Copy button
  (clipboard stub), inline code chip, a table, and an external link (sanitized + rel set).
- `useAuraChatRuntime.test.ts` — the extracted logic: `divergeSeqAt(index)` math (table-driven
  over indices incl. -1, 0, n), `onEdit` slices to parent + folds, `onReload` bails on
  `parentIndex<0` (WR-02 guard), `onCancel` aborts, `resumeNonce` re-drive. Stryker target.
- `ChatLane.test.tsx` — ports the current `ExternalStoreChat` streaming test (SSE fixture →
  reasoning drawer + tool card + assistant text), empty-state welcome, error-frame →
  `MessagePrimitive.Error`, `requires-action` → inline approval card present, `●` cursor on a
  running text part, branch picker hidden at branchCount 1.
- `UserMessage.test.tsx` / `AssistantMessage.test.tsx` / `EditComposer.test.tsx` — bubble
  structure + `data-role`; action bar verbs (user: Edit+Copy; assistant: Copy+Reload);
  `autohide`/`hideWhenRunning`; edit-mode swap → `EditComposer`, Send fires `onEdit`.
- `ThreadScrollToBottom.test.tsx` — present; invisible/disabled when `isAtBottom` (mock the
  viewport store); `aria-label` localized.
- `Composer.test.tsx` (extend) — Send↔Stop swap on `isRunning`; Enter sends, Shift+Enter
  newlines (the primitive's behavior); `aria-label`s; mobile `40svh` cap class present.
- `ToolActivityCard.test.tsx` (**extend** — AC-TOOL-1/2): keep the XSS-guard assertion green
  (raw blob escaped in `<pre>`); add status→token-tint per status; **auto-expand when
  `running` + auto-collapse-once on settle unless `userToggled`**; elapsed readout present when
  timing supplied / omitted when not; indented child subagent rows render status-tinted.
- `MarkdownText`/`AssistantMessage` streaming test (**extend** — AC-RENDER-4): the
  running-status line carries `.shine` (class assertion) and falls back to flat
  `text-text-muted` under `matchMedia('(prefers-reduced-motion: reduce)')`; pre-first-token
  `<Skeleton>` present with `role=status`.
- Kept green unchanged: `ReasoningDrawer.test.tsx`, `BranchPicker.test.tsx`, the approval-card
  tests.

**E2E (Playwright — reuse the shell spec's `chromium` + `mobile` projects):**

- `e2e/chat.spec.ts` (chromium) — mocked SSE (`page.route('/agent/run')`): send a prompt,
  assert streaming `●` cursor, assistant prose appears, tool card + reasoning drawer render,
  scroll-to-bottom appears when scrolled up and is hidden at bottom; error frame → error
  block + Reload; reduced-motion variant static.
- `e2e/chat-mobile.spec.ts` (mobile `Pixel 7`) — composer in the dock is reachable/focusable
  (shell AC-MOBILE-1); action bars touch-visible; no horizontal scroll; bubble width ≤80%.

**Gates (parity with backend, per project memory):** vitest coverage ≥85% (configured in
`vitest.config.ts`), Stryker ≥70% killed on the two logic files, `eslint --max-warnings=0`
(jsx-a11y blocking), `tsc --noEmit` clean, supply-chain gate on the new `rehype-sanitize`/
`remark-gfm` deps, CI runs the `mobile` Playwright project (no skip-as-green: it RUNS at 390px).

---

## 14. Out of scope (do not pull forward)

- Typed-display `switch(payload.type)` router (web_result/document/code/table/chart/…) →
  **Phase 26**. This rebuild ships the **raw tool card** (D-02) only.
- Feedback rating action group, Export-as-Markdown, `ActionBarMorePrimitive` → **Phase 26**
  (25-UI-SPEC locks Copy/Edit/Reload).
- Attachments (image/file upload), `ComposerAddAttachment`, the attachment dropzone → not in
  the AG-UI contract this milestone (multimodal in is Telegram/9c-side; cockpit attach is later).
- **Inline-citation hovercard + the `rehypeCitations`-style inline-token injection → Phase 26**
  (typed displays own citations; no source-array on the AG-UI wire today). This is a
  **deliberate disagreement with 06 §5.1-item-1's "top adopt" priority** (rationale in §3.3):
  the sanitizer keeps `[n]` markers as plain text now; the elysia `rehypeCitations` plugin
  *shape* (positional splice, do-not-hide-images — fixing elysia's two bugs) + an assistant-ui
  hovercard primitive is the **chosen reference** for the Phase-26 build. Recorded so 01 and 06
  give a planner one consistent marching order, not two.
- `MessagePrimitive.GroupedParts` part-grouping / chain-of-thought container → deferred (§3.2);
  Aura's reducer order makes it low-value today.
- Syntax highlighting (highlight.js) in code blocks → deferred (bundle/perf); CodeHeader +
  Copy ships now.
- Quote/selection toolbar, slash/mention/Lexical composer, model picker, `MessageTiming` →
  out (ux-spec Frame 07 / follow-up milestone).
- Suggestion chips on the welcome → deferred unless the backend supplies starter prompts.
- The shell layout, sidebar, runtime footer, drawers, mode switcher → **02-shell-sidebar-SPEC.md**.
- The token values, fonts, atmosphere, motion tokens → **03-design-system-SPEC.md** (consumed).

---

## 15. Citations (2026 best practice)

- **assistant-ui canonical Thread composition (Viewport + Messages + ViewportFooter +
  ScrollToBottom; MessagePrimitive.Parts components; ActionBar autohide/hideWhenRunning;
  BranchPickerPrimitive; external-store runtime onNew/onEdit/onReload/onCancel + branching).**
  Verified from the pinned clone `D:/tmp/assistant-ui` @ `packages/ui/src/components/
  assistant-ui/thread.tsx`, `templates/minimal/…/thread.tsx`, `apps/docs/components/examples/
  claude.tsx` + `shadcn.tsx`, `packages/react/src/primitives/thread/ThreadViewport.tsx`
  (turnAnchor/autoScroll props) + `ThreadScrollToBottom.ts`, `packages/core/src/react/types/
  MessagePartComponentTypes.ts` (part-component prop signatures). API index:
  https://www.assistant-ui.com/llms.txt (per-primitive pages under
  /docs/api-reference/primitives/*).
- **Auto-scroll that yields to the user (stick-to-bottom while streaming, release on
  scroll-up).** assistant-ui `useThreadViewportAutoScroll` (built into `ThreadPrimitive.Viewport`);
  industrial precedent: Odysseus `static/js/ui.js:461-487` (rAF lerp + 300px scroll-up bail +
  500ms throttle). Both MIT.
- **LLM-output markdown must be sanitized (untrusted output → mutation-XSS surface); drop
  script-capable tags incl. svg/math, strip `on*`/`srcdoc`, allowlist `http(s)`/relative
  URLs, force `rel=noopener` on external links.** Odysseus `static/js/markdown.js:52-113`
  (fixpoint sanitizer + `_safeHref`), adapted to `rehype-sanitize`'s allowlist model
  (rehype-sanitize is the maintained React/unified equivalent). react-markdown drops raw HTML
  by default — the sanitize pass is the defensive floor when gfm/autolink are enabled.
- **CSS-only message-enter motion (10px rise + fade) + reduced-motion guard.** Odysseus
  `style.css:1962,21319-21328` (`@keyframes msg-enter`); Elysia `itemVariants` spring config
  `{type:'spring',damping:20,stiffness:300}` (`TextDisplay.tsx:13-62`) as the reference feel —
  realized here with the design-system §5.4 `--motion-*` tokens (no Framer Motion in Aura web).
- **IME-safe Enter-to-send (`!isComposing`).** Odysseus `chat.js:3431` — handled internally by
  `ComposerPrimitive.Input`; documented to avoid a redundant handler.
- **Tool-activity enrichment: status-tinted pill + auto-expand-running + collapse-on-settle +
  elapsed time + nested subagent rows (§3.5).** openhuman `ToolTimelineBlock.tsx:38-114,117,
  140-161` (GPLv3 — *patterns only*, re-implemented from the behavior description, no source
  lifted), routed here by 06 §5.1-item-2; status→token mapping per design-system §4.2/§4.3.
- **CSS-only streaming shimmer (`.shine`) on the status line + pre-first-token `<Skeleton>`
  (§3.2/§7).** elysia `globals.css:114-130` + `RenderChat.tsx:294-303` (MIT), routed by
  06 §5.1-item-3; realized with `background-clip:text` + design-system `--motion-*` tokens and a
  `prefers-reduced-motion` flat fallback (no Framer Motion in Aura web).
- **Inline-token → React-component injection via a rehype plugin (citations).** Elysia
  `rehypeCitations` (`MarkdownFormat.tsx:39-111`) + `CitationBubble.tsx:25-56` — the clean
  pattern; **deliberately deferred to Phase 26** (§3.3/§14) against 06 §5.1-item-1's "top adopt"
  priority, because citations are a typed-display affordance with no AG-UI source-array on the
  wire today. The plugin *shape* (positional splice; do-not-hide-images) is the chosen Phase-26
  reference.
- **Editorial-Graphite tokens, WCAG 2.2 AA proof, motion + reduced-motion, mobile dock /
  svh / safe-area / keyboard.** Sibling specs `03-design-system-SPEC.md` (§4 color proof, §5
  motion, §3 type) and `02-shell-sidebar-SPEC.md` (§2.4 dock, §6 keyboard, §13 citations:
  svh-over-dvh, interactive-widget, native dialog, target-size).
- **Locked chat/HITL contract (reasoning drawer D-01, raw tool card D-02, branch picker D-09,
  Copy/Edit/Reload verbs, inline approval verbs/terminal states D-03/05/06, accent scarcity).**
  `.planning/phases/25-chat-approval-center/25-UI-SPEC.md`.

---

## Self-Scorecard

Rubric (target 9.5/10): concreteness (exact assistant-ui APIs verified from clone + version,
file targets, props, every state incl. streaming/empty/error/loading), correctness vs 2026
best practice (cited), fits Aura (keeps sseAdapter + runtime + locked decisions, tokens, i18n,
gates), a11y, mobile.

| Dimension | Score | Note |
|---|---|---|
| Concreteness (verified APIs, props, file targets) | 9.5 | Every primitive verified from the pinned 0.14.22 clone with file:line; 14 file targets ≤600 LOC; exact part-component prop signatures (`ToolCallMessagePartProps`, `ReasoningMessagePartProps`) quoted from source. |
| Current → replacement mapping | 9.5 | Per-file table; load-bearing pieces (sseAdapter, runtime, approval cards, tool-card XSS guard) explicitly kept; the spartan-gap list is concrete (file:line); enriched tool card stays raw + ≤120 LOC. |
| Every state (empty/loading/streaming/error/requires-action/mobile) | 9.5 | State table + per-state rendering; streaming cursor **+ `.shine` shimmer + pre-first-token skeleton (06 E2)**, error-via-MessagePrimitive.Error, requires-action→approval card, welcome all specified. |
| Message rendering (bubbles/markdown/reasoning/tools/cursor) | 9.5 | Canonical bubble grid; the one security upgrade (sanitized markdown) with the Odysseus blocklist ported; D-01 kept; **D-02 kept raw AND enriched (status-tint + auto-expand-running + elapsed + subagent rows, 06 H1)**; in-progress cursor; citation hovercard reconciled (deferred to Ph26 with explicit rationale vs 06). |
| Scroll + composer + branch picker | 9.5 | Viewport autoScroll/turnAnchor verified; ScrollToBottom behavior; composer Send↔Stop kept + dock relocation + mobile cap; branch picker over the path-aware backend kept. |
| 2026 correctness + citations | 9.5 | assistant-ui clone-verified, sanitization (rehype-sanitize), auto-scroll-yield, CSS motion (`.shine`), IME — all cited to source/clone; openhuman H1 + elysia E2 routed in with the GPL/MIT licensing noted; sibling specs cited for tokens/dock. |
| Fits Aura (wiring, tokens, i18n, gates, GSD shape) | 9.5 | AC-WIRE-* guard the invariant; token-pure incl. the `.shine` gradient (named tokens only → AC-TOKEN-1 grep still passes); enrichments add no new i18n keys; ≥85%/≥70% gates; incremental migration keeps green at each step; `sseAdapter.ts` un-edited (elapsed-time degrades gracefully if timing not stamped). |
| Accessibility + mobile | 9.5 | WCAG SCs mapped; touch-visible action bars; 320px reflow; live regions; status-tint pairs color with a text label (1.4.1); `.shine` + auto-expand transition `motion-reduce:`-gated; elapsed readout `aria-hidden` while ticking; coordinated with shell mobile ACs. |
| 06-incorporation (sibling-revision loop closed) | 9.5 | All three 06 §5.1 items aimed at this spec are resolved: tool-card enrichment (item 2) folded into §3.5 + AC-TOOL-1/2; `.shine` shimmer (item 3) into §3.2/§7 + AC-RENDER-4; citation priority (item 1) explicitly reconciled (deferred to Ph26, rationale in §3.3/§14). |

**Overall: 9.5 / 10.**

**Items that would block a clean 9.5 — and their resolution within this SPEC:**
1. **Provider-hoist confirmation (shell spec blocker #3).** This SPEC asserts the composer can
   live in `BottomDock` (sibling subtree) under one `AssistantRuntimeProvider`; §10 step 2
   makes it a *gated spike* before execution (it works via React context, but is verified, not
   assumed, against 0.14.22). Not a gap — a planned verification.
2. **`rehype-sanitize` schema completeness is enforced, not claimed.** AC-RENDER-1 +
   `markdownSanitize.test.ts` (Stryker ≥70%) recompute the sanitization on real payloads, so
   the blocklist is tested, not trusted. The Odysseus fixpoint→allowlist mapping is the one
   adaptation that must be reviewed in code review (`differential-review`).
3. **Tool-card elapsed time degrades gracefully (no contract change forced).** The enriched
   card *shows* elapsed time only when the reducer stamps `startedAt`/`finishedAt`; with no
   timing it omits the readout — so the enrichment lands without editing `sseAdapter.ts`
   (AC-WIRE-1 holds). Timing can be wired later behind the same component. Not a gap — a
   forward-compatible seam.
4. **History snapshot loading state is conditional.** The lane today has no snapshot fetch
   (history arrives via the runtime); the loading skeleton (§7) is specified for *if/when* a
   snapshot endpoint is wired (Phase 28/conversation deep-link). Marked conditional so it
   isn't half-built; the empty/streaming/error states are unconditional and fully specified.
5. **Citation hovercard deferred deliberately, not by omission.** §3.3/§14 record the explicit
   disagreement with 06's "top adopt" priority and the Phase-26 boundary + missing-source-array
   rationale, so 01 and 06 give a planner one consistent order. Not a gap — a documented
   sequencing decision.
6. **Live operator beauty sign-off** is a visual UAT after implementation (out of scope for a
   presentation SPEC) — the only thing this SPEC cannot self-certify, identical to the sibling
   specs' caveat.
