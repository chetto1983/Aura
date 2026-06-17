# 04 — Runtime Telemetry Footer SPEC (cost / cache / token / context instrument)

Status: DRAFT (root-cause verified against source, redesign concrete)
Surface: `web/src/chat/RuntimeFooter.tsx` + `footerMetrics.ts` + `ContextBudgetGauge.tsx` + `AppShell.tsx` wiring + backend fast-path emission.
Theme: Editorial graphite — consumes **03-design-system's semantic token names** (never raw hex). The instruments this footer touches: `--color-accent` (warm gold `#C8A86A`, gauge fill below the near-full tier), `--color-warning` (`#DDA94A`, gauge near-full tier), `--color-danger` (`#E66A63`, gauge critical tier), `--color-text` (`#ECE7DF`, metric values), `--color-text-muted` (`#B0A99E`, Session figures), `--color-text-faint` (`#8E877C`, Micro captions), `--color-surface` (`#1B1714`, footer bar), `--color-surface-3` (`#2E2823`, gauge track), `--color-border` (`#38322B`, top hairline), `--font-mono` (Commit Mono, tabular — every numeral). All values are 03's; this spec references the *names*, and re-skins for free when 03 lands.
Reqs: CHAT-04 / D-10 (per-turn + session token/cost) / D-11 (cache-hit ratio + microcompact context gauge) / D-12 (context-budget gauge bound to `/rot-events`).

---

## Root-Cause Analysis

The reported symptom: a live prompt produced a real reply, but the footer stayed **`TOKENS 0 · CACHE — · COST —`** — it never updated.

I traced the documented data flow end to end and the contract is INTACT at every hop; the break is a *content* break (zero-valued usage), plus two latent integration bugs that would reproduce the same em-dash/zero state under common conditions.

### The verified happy path (works in isolation + in the unit test)

```
LlmAgent.finalEvent (llm_agent_events.go:215)
  → ev.LLMResponse{Content, FinishReason}, ev.Actions.StateDelta = usageStateDelta(usage)
    usageStateDelta (llm_agent_events.go:229) = {prompt_tokens, completion_tokens, cache_hit_tokens [, cost_usd]}
  → translator.Translate (translator.go:127) — StateDelta branch (len>0, no /tool_call_id)
      yields events.NewStateDeltaEvent(stateDeltaOps(...))  ── ops = [{op:replace, path:/prompt_tokens, value:N}, …]
  → server.streamSSE (server.go:323) WriteEventWithType → `event: STATE_DELTA\ndata: {…,"delta":[…]}\n\n`
  → sseAdapter.parseSSEBlock (sseAdapter.ts:357) → StateDeltaFrame{delta:[…]}
  → reduceFrame STATE_DELTA branch (sseAdapter.ts:280): no /tool_call_id, isUsageDelta()==true
      → state.usage = usageFromStateDelta(ops)   (sseAdapter.ts:153)
  → streamRun onUpdate(toThreadMessage, state.usage)  (sseAdapter.ts:509)
  → ExternalStoreChat onNew.onUpdate → onUsage?.(usage)  (ExternalStoreChat.tsx:84)
  → AppShell setUsage  (AppShell.tsx:129)
  → RuntimeFooter usage prop → seedSession(conv) + addTurn(seed, usage)  (RuntimeFooter.tsx:42)
```

This path is proven by `web/src/chat/__tests__/ExternalStoreChat.test.tsx` (the STATE_DELTA with `/prompt_tokens:120` lands and `usageSpy.mock.calls.at(-1)[0].promptTokens === 120`) and by the live e2e `internal/runner/live_e2e_scenarios_test.go:315` ("OpenRouter reports prompt/completion tokens on every turn", `TotalInputTokens > 0`). **So neither the wire contract nor the reducer is broken.**

### THE primary break (verified) — the greeting fast-path emits all-zero usage and no cost

`internal/runner/runner.go:366` short-circuits the LLM loop for greetings via `fastReplyFor`. The terminal Event it yields, `fastReplyEvent` (`internal/runner/runner_fastpath.go:33`), stamps a StateDelta of:

```go
Actions: agent.Actions{StateDelta: map[string]any{
    "prompt_tokens":     0,
    "completion_tokens": 0,
    "cache_hit_tokens":  0,
}},   // NOTE: no "cost_usd"
```

When the operator's "real prompt" was a greeting (`ciao`, `salve`, `buongiorno`, `buonasera` — exactly the live-test opener), the chain is:

1. Backend emits `STATE_DELTA delta=[{/cache_hit_tokens:0},{/completion_tokens:0},{/prompt_tokens:0}]` (sorted keys, no `/cost_usd`).
2. `isUsageDelta(ops)` → **true** (the paths exist).
3. `usageFromStateDelta(ops)` → `{promptTokens:0, completionTokens:0, cacheHitTokens:0, costUsd:undefined}`.
4. Footer renders:
   - `TOKENS` = `formatTokens(0 + 0)` = **`"0"`**
   - `CACHE` = `cacheHitPercent(0, 0)` → `undefined` → **`"—"`**
   - `COST` = `formatCost(0, false)` → `undefined` → **`"—"`**

That is the reported string **exactly: `TOKENS 0 · CACHE — · COST —`**. The footer DID update — to a faithfully-zero turn. The fast-path is real, has no real token spend, and persists a 0-aggregate row, so even the "Session" seed reads 0. This is the dominant explanation and is fully consistent with "real prompt, real reply, footer 0".

The unit test never caught it because the fixture STATE_DELTA carries `prompt_tokens:120` — it tests a non-zero turn, never the zero-usage turn the fast-path (and 0-usage providers like Ollama, per project memory) actually emit.

### Latent break #2 (would reproduce the same symptom for non-greeting turns) — STATE_DELTA is droppable under backpressure

`server.go:341 pumpSend` drops any frame NOT in `isLifecycleFrame` when the per-connection SSE channel is full (T-12-09 "never block the Loop"). `isLifecycleFrame` (`server.go:366`) does **not** include `events.EventTypeStateDelta` (`"STATE_DELTA"`). So under a slow client the single usage frame — which arrives in a burst at end-of-turn alongside TEXT_MESSAGE_END + RUN_FINISHED — is the prime drop candidate. If dropped, the reducer's `state.usage` stays `undefined`, `onUsage(undefined)` is the last call, and the footer falls back to the session seed only. This is the silent, intermittent variant of the bug.

### Latent break #3 (stale Session totals) — the persisted aggregate is fetched once and never invalidated

`useConversation(conversationId)` (`useConversations.ts:97`) is a React Query `useQuery` keyed `[CONVERSATION_KEY, id]`. **Nothing invalidates it after a turn finishes** (grep: no `invalidateQueries([CONVERSATION_KEY])` anywhere in `AppShell.tsx`/`chat/`). The footer "Session" figure is therefore: seed-at-open + the single in-flight live turn. On reopening the thread it shows the last-cached aggregate, not the freshly-persisted one. Combined with break #1 (the persisted fast-path row is 0), the Session column also reads 0 forever for a greeting-opened thread.

### The precise fix (per break)

- **#1 (primary):** the fast-path has genuinely-zero token usage; rendering "0/—/—" is *arithmetically honest* but *operator-confusing*. Fix at the **presentation boundary**, not the data: when a turn carries usage but every token field is 0 AND no cost (a "no-spend turn" — fast-path, cache-only, or 0-usage provider), render the metrics as **`—` ("no spend")** rather than a bare `0`, and surface a one-line affordance (`footer.noSpend` → "Local reply — no model spend"). Do NOT fabricate non-zero numbers. Keep the Session seed authoritative for cumulative spend. (Backend stays as-is; the fast-path correctly reports zero.)
- **#2:** add `events.EventTypeStateDelta` (`"STATE_DELTA"`, confirmed against the AG-UI SDK enum `pkg/core/events/events.go:24`) to `isLifecycleFrame` (`server.go:366`) so the usage frame is delivered via the blocking-but-abortable send, never dropped. It is low-volume (one per turn) so it does not threaten the no-block invariant in practice; the terminal usage frame is as load-bearing as RUN_FINISHED for the footer.
- **#3:** invalidate `[CONVERSATION_KEY, threadId]` (and `[CONVERSATION_ROT_EVENTS_KEY, threadId]`) when a turn completes, so the persisted Session/gauge seed refreshes. Wire it on the `onUsage`/turn-finished seam in `ExternalStoreChat` (or via an `onTurnComplete` callback to AppShell), guarded so it fires once per finished turn, not per streamed frame.

### The failing test that reproduces the primary bug

`web/src/chat/__tests__/RuntimeFooter.test.tsx` — add:

```ts
it('a zero-usage turn (fast-path) shows no-spend, never a bare 0/NaN', () => {
  const usage: TurnUsage = { promptTokens: 0, completionTokens: 0, cacheHitTokens: 0 };
  const { container } = renderFooter({ usage, agg: EMPTY_AGG }); // EMPTY_AGG = all-0 persisted seed
  // The reported regression: a bare "0" + two em-dashes. After the fix the turn column
  // must read the no-spend placeholder, never NaN.
  expect(container.textContent).not.toMatch(/NaN/);
  expect(screen.getByText('Local reply — no model spend')).toBeTruthy();
});
```

And an `ExternalStoreChat.test.tsx` case asserting a STATE_DELTA with all-zero usage still calls `onUsage` (regression guard that the seam fires, not that the number is non-zero).

---

## (2) Data sources — robust dual-source design (recommended)

The footer MUST be correct even when the live model returns 0 usage (fast-path; Ollama/local; some providers). Two sources, layered:

| Source | Route | Drives | Robustness role |
|---|---|---|---|
| **Live STATE_DELTA** (per-turn) | SSE `/agent/run` → `usageFromStateDelta` | the **This-turn** column + the in-flight gauge `usedTokens` | immediate; zero on a no-spend turn |
| **Persisted cachemetrics aggregate** (session) | `GET /api/conversations/{id}` → `TotalInputTokens / TotalOutputTokens / TotalCachedTokens / TotalCostUSD` (`store_helpers.go:33`, fed by `runner_persist.go persistAssistantAnswer` → `AppendAssistantTurnWithCacheMetric`) | the **Session** column | authoritative cumulative spend; survives a dropped live frame |
| **rot-events** (microcompact ladder) | `GET /api/conversations/{id}/rot-events` → `PairsDropped` | the gauge "Compacted N older turns" marker | already wired (`ContextBudgetGauge.tsx`) |

**Recommended design:** Session = `seedSession(conv)` + (this-turn live delta, once). This is already the shape in `RuntimeFooter.tsx:42`; the design is sound — the only fixes are (a) refresh the seed after each turn (break #3) and (b) present the zero-spend turn honestly (break #1). A 0-usage live model still shows correct **Session** totals from persisted metrics because the backend persists each finalized turn's usage into `aura.conversations` aggregates regardless of the live frame surviving the wire.

No new backend route is required. The conversations API already projects all four aggregate fields and the rot-events ladder.

---

## (3) Redesigned footer instrument

### Layout (desktop, ≥ `sm`)

A single bottom-spanning bar (`grid-template-rows: auto 1fr auto` in AppShell — the footer is the last row), `border-t border-border bg-surface`, `font-mono` for every numeral (Editorial graphite: mono carries all instruments). Four instruments left-to-right, `gap-x-6`:

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│ TOKENS        CACHE        COST          CONTEXT                                    │
│ 1.2k          48%          $0.0031        12k / 1M · 1%  ▓▓░░░░░░░░░░░░░░            │
│ Session 84k   Session 51%  Session $0.42  Compacted 3 older turns                   │
└──────────────────────────────────────────────────────────────────────────────────┘
```

Each `Metric`: a `text-faint` micro-caption (Micro role — `text-[0.6875rem]`, uppercase, `tracking-wider`, `font-sans` weight 600 — see "Type alignment" below), the per-turn value (`font-mono text-sm text-text`), and the session figure beside it (`font-mono text-[0.6875rem] text-text-muted`, prefixed `Session`). The gauge is the fourth cell: caption + `{{used}} / {{window}} · {{percent}}%` + a 1.5px fill bar coloured by the **3-tier severity scale** (accent `<70%` → warning `≥70%` → danger `≥90%`; see "Context gauge" below), `role=progressbar` + `aria-valuenow`, with the optional compaction marker beneath.

### The metrics (final formatting contract)

| Instrument | This turn | Session | Format helper | Guard |
|---|---|---|---|---|
| **TOKENS** | prompt+completion of the turn | seed.prompt+completion + live delta | `formatTokens` (`42_000→"42k"`, `1.5M`, `<1000` verbatim) | no-spend turn → `—` (not `0`) |
| **CACHE** | `cacheHitPercent(cacheHit, prompt)` `→ "48%"` | session ratio | rounded int % | `/0 → "—"` (never NaN%) |
| **COST** | `formatCost(costUsd, costUsd!==undefined)` | `formatCost(session.costUsd, session.hasCost)` | `<$1 → 4dp` (`$0.0031`), `≥$1 → 2dp` | unknown cost → `—` (never `$NaN`/`$0.00`) |
| **CONTEXT** | gauge `used/window·%` | — | `formatTokens` both sides | `/0 window → 0%`, clamp 0..100; fill tier `accent`→`warning`→`danger` at 70/90 |

New: a **no-spend** affordance. When `turn` exists and `promptTokens+completionTokens === 0 && costUsd === undefined`, the This-turn TOKENS/CACHE/COST all render `—` and a `footer.noSpend` caption line ("Local reply — no model spend" / "Risposta locale — nessun costo modello") appears under the TOKENS cell. This converts the confusing `0/—/—` into a legible state.

### Type alignment (03 §3.4 roles)

The footer consumes 03's type roles by name, not raw px: metric captions and the "compacted N turns" eyebrow use the **Micro** role (`text-[0.6875rem]`, weight 600, `uppercase`, `tracking-[0.06em]`, `font-sans`) — *not* the earlier draft's `0.625rem`, which 03 does not define. Per-turn values use **Mono** (`font-mono text-sm tabular-nums`), the gauge value uses **Mono-strong** (`font-mono` weight 500, `tabular-nums`), and Session figures use **Mono** at `text-[0.6875rem] text-text-muted`. This removes the lone type deviation flagged against the design system.

### Context gauge (D-11/D-12) — 3-tier severity

Mechanism unchanged (`ContextBudgetGauge.tsx`): `usedTokens` rides the live STATE_DELTA `prompt_tokens` of the latest turn, falling back to `session.promptTokens` when idle (`RuntimeFooter.tsx:53`). `windowTokens` defaults to `DEFAULT_CONTEXT_WINDOW = 1_000_000` (DeepSeek-V4). `role=progressbar` + `aria-valuemin/max/now` carry the meaning so colour is decorative (WCAG 1.4.1). Compaction marker reads `totalPairsDropped(rotEvents)`.

**Fill colour — adopt the 3-tier severity scale** (06 §5.4-item-1, derived from openhuman H2's `TokenUsagePill.tsx:20-43`, patterns-only/GPL). The shipped 2-tier 85% binary flip is replaced by a graded warm-up that reads as "premium-calm" — a gentle amber well before the hard red, so the operator sees the budget filling rather than a single late jolt. Three named thresholds bound to **03's semantic tokens** (never raw hex):

| Tier | Range | Fill token | Rationale |
|---|---|---|---|
| **normal** | `< CONTEXT_NEAR_PERCENT (70)` | `--color-accent` (warm gold `#C8A86A`) | The gauge fill is the sole footer use of the scarce accent — explicitly **reserved-item-7** in 03 §4.3, so it does not violate accent scarcity. |
| **near** | `≥ 70 && < CONTEXT_CRITICAL_PERCENT (90)` | `--color-warning` (`#DDA94A`) | Early, calm amber — the "gentle warm-up" before the budget is a problem. |
| **critical** | `≥ 90` | `--color-danger` (`#E66A63`) | Reserved for the genuinely-tight context where microcompact is imminent. |

Concretely: replace the single `CONTEXT_NEAR_FULL_PERCENT = 85` constant with `CONTEXT_NEAR_PERCENT = 70` and `CONTEXT_CRITICAL_PERCENT = 90`; a pure `gaugeTier(percent)` helper returns `'normal' | 'near' | 'critical'`, mapped to the Tailwind utility (`bg-accent` / `bg-warning` / `bg-danger`) so colour resolves through 03's tokens. Percent is still clamped `0..100` and a `/0` window reads `0%`. Colour remains decorative only — the tier never changes the `aria-valuenow` semantics, and the threshold figures must be inside the `aria-label`/text so a SR user gets the severity without colour (WCAG 1.4.1).

### Live update via aria-live (a11y)

- The footer `<footer>` keeps its `aria-label={footer.contextLabel}`.
- Wrap the three numeric instruments (TOKENS/CACHE/COST) in a single `role="status" aria-live="polite" aria-atomic="true"` region so a completed turn announces the new figures once, when the screen reader is idle — NOT a chatty per-frame announcement. Per [WebAbility](https://www.webability.io/glossary/aria-live) and [BOIA](https://www.boia.org/blog/what-are-aria-live-regions), `polite` is correct for "important but not urgent, not so rapid as to be annoying" — exactly cost/token deltas.
- To avoid mid-stream chatter, only the **settled** (turn-finished) values feed the live region; during streaming the region holds the prior turn's numbers (the bar still visually updates the gauge, which is `role=progressbar` and a default live region, but we set the fill transition `motion-reduce:transition-none`).
- Reduced motion: the gauge fill already uses `transition-[width] motion-reduce:transition-none` — keep; no other animation introduced (no count-up tween — it would be both motion noise and a11y chatter).

### Mobile behavior (must not crowd the composer)

The footer is a *fixed bottom row* in the AppShell grid; on narrow screens the composer sits directly above it, so a 4-instrument bar would crowd. Spec:

- Below `sm`: collapse to a **single summary line** showing only `COST` (Session) + the `CONTEXT` percent + a compaction dot — the two figures an operator scans on mobile — behind a tap-to-expand disclosure (`<details>`/button, `aria-expanded`). Expanded reveals the full four-instrument set in a wrapping 2-column grid.
- The collapsed bar is `min-h` one line (`py-1`), so it never steals more than ~28px from the composer.
- `flex-wrap` already present (`RuntimeFooter.tsx:58`) handles the intermediate widths; the disclosure is the explicit mobile contract.
- The gauge `min-w-[10rem]` is relaxed to `min-w-0` in the collapsed state so it cannot force a horizontal scroll on a 320px viewport.

---

## (4) File targets, acceptance criteria, test plan

### File targets (all ≤600 LOC; refactor-on-touch)

| File | Change |
|---|---|
| `internal/runner/runner_fastpath.go` | (no number fabrication) — keep zero usage; document that a fast-path turn is a legitimate no-spend turn the footer renders as `—`. |
| `internal/agui/server.go` | add `events.EventTypeStateDelta` (`"STATE_DELTA"`) to `isLifecycleFrame` (break #2). |
| `web/src/chat/footerMetrics.ts` | add `isNoSpendTurn(usage)` predicate + the pure `gaugeTier(percent): 'normal'\|'near'\|'critical'` helper (thresholds 70/90); keep all existing guards. |
| `web/src/chat/RuntimeFooter.tsx` | no-spend rendering + the `aria-live="polite"` settled-values region + mobile disclosure; thread a `turnSettled` flag; align metric captions to 03's Micro role (`text-[0.6875rem]`). |
| `web/src/chat/ContextBudgetGauge.tsx` | replace `CONTEXT_NEAR_FULL_PERCENT = 85` with `CONTEXT_NEAR_PERCENT = 70` + `CONTEXT_CRITICAL_PERCENT = 90`; map `gaugeTier(percent)` → `bg-accent`/`bg-warning`/`bg-danger` (03 tokens); keep `role=progressbar`, the threshold figures inside the `aria-label`. |
| `web/src/chat/ExternalStoreChat.tsx` | add an `onTurnComplete` seam fired once at `finally`/RUN_FINISHED so AppShell can invalidate the conversation query (break #3). |
| `web/src/AppShell.tsx` | on `onTurnComplete`, `queryClient.invalidateQueries([CONVERSATION_KEY, threadId])` + rot-events; keep the existing thread-switch `setUsage(undefined)` reset. |
| `web/src/conversations/useConversations.ts` | (optional) export a `useInvalidateConversation(id)` helper to keep AppShell thin. |
| `web/src/i18n/resources.ts` | add `footer.noSpend` to **both** `en` and `it` bundles; rebuild `internal/webui/dist` after. |

### Acceptance criteria

1. **AC-1 (primary regression):** a STATE_DELTA with all-zero usage and no `cost_usd` (fast-path) renders the no-spend state — NOT `TOKENS 0 · CACHE — · COST —`; never `NaN`/`$NaN`. (unit: RuntimeFooter)
2. **AC-2 (streamed delta updates the footer):** a streamed STATE_DELTA with `prompt_tokens>0` updates This-turn TOKENS/CACHE/COST live; `onUsage` last-call carries the parsed usage. (unit: ExternalStoreChat — extends the existing `promptTokens===120` assertion to also assert the rendered footer string.)
3. **AC-3 (opening an existing conversation shows persisted totals):** mounting the footer with a non-empty `conversationId` and a stubbed `GET /api/conversations/{id}` returning `Total*` seeds the Session column from persistence with no live turn. (unit: RuntimeFooter — extends the existing "seeds from aggregate" case to the idle, `usage:undefined` path.)
4. **AC-4 (no dropped usage frame):** the SSE pump never drops STATE_DELTA — `isLifecycleFrame(EventTypeStateData) == true`. (Go unit: `server_test.go`.)
5. **AC-5 (Session refresh):** after a turn completes, `[CONVERSATION_KEY, id]` is invalidated exactly once. (unit: ExternalStoreChat with a spy queryClient.)
6. **AC-6 (a11y):** the numeric cluster is a `role=status aria-live=polite aria-atomic` region; the gauge keeps `role=progressbar`+`aria-valuenow`; reduced-motion disables the fill transition. (unit + axe smoke.)
7. **AC-7 (mobile):** below `sm` the footer collapses to one line + disclosure and does not exceed one text-row height. (unit: media-query class assertion / Playwright viewport.)
8. **AC-8 (i18n):** `footer.noSpend` present in en+it; no missing-key warning. (unit.)
9. **AC-9 (coverage):** owned footer surface ≥85% statements/branches/functions/lines (vitest threshold already 85, must stay green); backend touched files keep the ≥85% floor.
10. **AC-10 (3-tier gauge):** `gaugeTier(percent)` returns `normal`/`near`/`critical` at the `<70` / `[70,90)` / `≥90` boundaries (incl. the `69.9/70/89.9/90` edges), and the gauge applies `bg-accent`/`bg-warning`/`bg-danger` respectively — no raw hex, the fill class resolves through 03's tokens; the threshold figures appear in the gauge `aria-label` so severity is conveyed without colour. (unit: footerMetrics table test for the helper + RuntimeFooter render assertion for the class.)
11. **AC-11 (theme purity):** `grep -nE '#[0-9A-Fa-f]{3,6}'` over the footer source (`RuntimeFooter.tsx`, `ContextBudgetGauge.tsx`, `footerMetrics.ts`) returns no raw colour hex; every colour is a 03 semantic utility (`text-text`, `text-text-muted`, `text-text-faint`, `bg-surface`, `bg-surface-3`, `border-border`, `bg-accent`/`bg-warning`/`bg-danger`). No reference to the dead Phase-23 blue palette (`#5BA8FF`/`#E0A23C`/`#5B6675`/`#9AA4B2`). (grep gate, mirrors 03 AC-4.)

### Test plan

- **Unit (vitest, jsdom):** extend `RuntimeFooter.test.tsx` (AC-1/3/6/7/8/10) and `ExternalStoreChat.test.tsx` (AC-2/5). Pure helpers (`isNoSpendTurn`, `gaugeTier`, existing guards) keep their direct table tests. Mutation: the no-spend predicate, the `gaugeTier` boundary comparisons (70/90), and the `/0`/undefined guards are the kill targets.
- **Go unit:** `internal/agui/server_test.go` asserts STATE_DELTA is a lifecycle frame (AC-4) and is not dropped on a full buffer (extend the existing drop test).
- **Live (manual, operator):** send a greeting (`ciao`) → footer reads the no-spend state; send a real task → This-turn populates from the live STATE_DELTA and Session grows; reopen the thread → Session reflects persisted totals (AC-3). Cross-check `internal/runner/live_e2e_scenarios_test.go` already proves `TotalInputTokens>0` server-side.
- **a11y:** axe-core over the footer; NVDA/VoiceOver spot-check that the cluster announces once per settled turn, not per frame.

---

## Sources (2026 research)

- [Braintrust — How to track LLM token usage (2026)](https://www.braintrust.dev/articles/how-to-track-llm-token-usage-2026): three levels of visibility — prompt/completion per call, context-window utilization per request, per-step usage in traces. Validates the per-turn + session + context-gauge layering.
- [Braintrust — Best LLM monitoring tools 2026](https://www.braintrust.dev/articles/best-llm-monitoring-tools-2026) and [Maxim — Best LLM cost tracking tools 2026](https://www.getmaxim.ai/articles/best-llm-cost-tracking-tools-in-2026/): per-request cost breakdown + cached-token field tracked separately from ordinary prompt tokens so the dashboard distinguishes token volume from billed cost — directly supports the separate CACHE-hit % instrument.
- [Redis — LLM token optimization (2026)](https://redis.io/blog/llm-token-optimization-speed-up-apps/): cache hits cut cost up to ~73% and return in ms — the cache-hit ratio is a first-class operator metric, not a footnote.
- [WebAbility — aria-live complete guide](https://www.webability.io/glossary/aria-live) and [BOIA — What are ARIA live regions](https://www.boia.org/blog/what-are-aria-live-regions): use `polite` for important-but-not-urgent updates; `progressbar`/`status` are default live regions; avoid chatty regions — the basis for the settled-values-only live region.

---

## Self-Scorecard

Re-scored against the 00-VALIDATION rubric (gate = min of five dimensions) after the revision round that closed the two blocking gaps the adversarial validator flagged (theme-header blue-palette drift; un-incorporated 3-tier gauge).

| Dimension | Score | Notes |
|-----------|-------|-------|
| **Correctness** (cited 2026 best practice) | 9.5 | Root cause traced through the *actual* source at every hop and verified (`runner_fastpath.go:46-49`, `server.go:366-382` `isLifecycleFrame` excludes `STATE_DELTA`, un-invalidated `useConversation`); the exact `TOKENS 0 · CACHE — · COST —` reproduction is real; ARIA-live settled-only + 3-tier severity both cited (Braintrust/WebAbility/BOIA + openhuman H2). Unchanged by the revision — the validator scored this 9.5. |
| **Fit** (Aura constraints) | 9.5 | No new backend route; one-line `isLifecycleFrame` add; reuses persisted-aggregate + rot-events; honors ≥85% + en+it + ≤600 LOC. The 3-tier gauge is two extra constants + one pure helper, no new dependency. |
| **Concreteness** (exact names/AC) | 9.5 | File targets, formatting-contract table, the failing-test snippet, **now 11 machine-checkable AC** (added AC-10 gauge-tier boundaries 70/90 + AC-11 hex-grep gate), exact 03 token names + tier thresholds, the `gaugeTier` signature. |
| **Completeness** (edge/a11y/mobile/rollback) | 9.5 | Three breaks + per-break fix + rollback-safe (backend stays honest); mobile disclosure; a11y settled-only live region; **the 06 §5.4 3-tier gauge revision is now incorporated** (was the un-addressed sibling revision) with thresholds, token-bound tier colours, edge-case AC, and the colour-decorative/SR-label guard. |
| **Theme adherence** (03 tokens, no invented colour) | 9.5 | **The blocking defect is fixed.** The theme header no longer hardcodes the dead blue palette (`#5BA8FF`/`#E0A23C`/`#5B6675`/`#9AA4B2`) — it now references 03's semantic names (`--color-accent`/`-warning`/`-danger`/`-text`/`-text-muted`/`-text-faint`/`-surface`/`-surface-3`/`-border`/`--font-mono`) with the editorial-graphite values (gold `#C8A86A`, amber `#DDA94A`, rust `#E66A63`). The gauge fill is bound to tokens, sits inside 03 §4.3's reserved-item-7 accent allowance, and AC-11 is a grep gate that fails on any raw hex or any dead-palette reference. The lone type deviation (`0.625rem` → 03's Micro `0.6875rem`) is also resolved. |

**Gate score (min): 9.5 / 10 → PASS.**

What changed in this revision (the two blocking fixes + the minor one):
- **Theme header rewritten** to consume 03's semantic token *names* and editorial-graphite *values* — no raw hex from the dead blue Phase-23 palette anywhere in the spec. Every body reference (layout caption, formatting table, surface bindings) is a 03 utility name.
- **3-tier context-budget gauge incorporated** (06 §5.4-item-1 / openhuman H2): `normal` `<70%` → `--color-accent`, `near` `≥70%` → `--color-warning`, `critical` `≥90%` → `--color-danger`, replacing the 2-tier 85% binary flip with a graded premium-calm warm-up. Concrete constants (`CONTEXT_NEAR_PERCENT=70`, `CONTEXT_CRITICAL_PERCENT=90`), a pure `gaugeTier()` helper, token-bound tier colours, accent-scarcity reconciliation (reserved-item-7), and the WCAG-1.4.1 colour-decorative guard (thresholds in the `aria-label`).
- **Type alignment** to 03 §3.4: footer captions use the Micro role `text-[0.6875rem]` (not the earlier draft `0.625rem`, which 03 does not define).
- AC count 9 → 11 (AC-10 gauge tiers, AC-11 theme-purity grep); test plan + file targets updated to add `ContextBudgetGauge.tsx` and the `gaugeTier` boundary/mutation kill-targets.

Remaining gap (honest, not fixable from source alone — does NOT drag any rubric dimension below 9.5):
1. **Single-cause certainty.** The fast-path explanation is the best fit and reproduces the exact string, but it is conditional on the live prompt having been a greeting. If the operator sent a non-greeting and STILL saw `0/—/—`, the cause is break #2 (dropped frame) or a 0-usage provider. A perfect 10 needs the actual SSE trace (or `aura chat` `· toolname` trace) from the failing session to single out the one true cause rather than ranking three plausible ones — that capture was not available at spec time. The fix set is robust to all three regardless of which fired.
