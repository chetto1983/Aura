# Phase 37E: Composer Model & Reasoning-Effort Selector - Context

**Gathered:** 2026-07-10
**Status:** Ready for planning

<domain>
## Phase Boundary

A **per-turn reasoning-effort ("thinking") selector in the web Composer** (`web/src/chat/Composer.tsx`) — parity with GPT's `off · low · mid · high · auto` control — that lets the user pick how hard the agent thinks on the next turn, carried on `POST /agent/run`, validated server-side, and mapped onto Aura's existing provider-neutral reasoning contract (`internal/llm` `ReasoningConfig`).

**This is NOT a model picker.** Model selection is already a shipped, operator-scoped concern in the **Settings page** (`internal/settings/settings.go` `AllowedKeys` → `AURA_LLM_MODEL`; served by `internal/agui/settings_api.go`). Per the user's explicit decision, 37E **drops the model selector** and delivers the reasoning-effort selector only. This is a scope reduction of the chartered WEBMODEL-01/02 requirements and is handled by the mandatory Wave-1 PRD-amendment (D-11).

Fourth cockpit-web parity area from the voice/artifact/skill audit (37B artifacts, 37C voice, 37D skills, 37E effort). We are clarifying HOW to implement the scoped WEBMODEL surface — a Composer model dropdown, per-message (vs per-conversation) overrides, and reasoning-budget/token knobs are out of scope.

</domain>

<decisions>
## Implementation Decisions

### Selector shape & levels (RESOLVED — user)
- **D-01:** **Effort-only selector; the model dropdown is dropped.** Model selection stays in the Settings page (global, operator-scoped, already shipped). The Composer exposes ONE control: the reasoning-effort selector. (WEBMODEL-01/02 "model selector" clause is removed — see D-11 PRD gate.)
- **D-02:** **Five levels, GPT-style: `off · low · mid · high · auto`.** These are symbolic UI values; the server maps them to the backend contract (D-03). The client NEVER sends a raw provider reasoning payload.

### Backend mapping (RESOLVED — grounded in `internal/llm` + `reasoning_policy.go`)
- **D-03:** UI level → `llm.ReasoningConfig` (server-side map, using the provider-neutral vocabulary in `internal/llm/client.go`):

  | UI level | `ReasoningConfig` |
  |---|---|
  | **off** | `Effort: ReasoningEffortNone` — the **only** live-verified off-switch on the DeepSeek/OpenRouter path (native `thinking:disabled` is dropped by OpenRouter; probe 2026-06-11). |
  | **low** | `Effort: ReasoningEffortLow` |
  | **mid** | `Effort: ReasoningEffortMedium` |
  | **high** | `Effort: ReasoningEffortHigh` |
  | **auto** | **no override** — defer to today's adaptive policy (D-04). |

  The `Exclude` field (CoT visibility) is set from `cfg.ShowReasoning` exactly as `ReasoningTier.reasoning()` does — the selector controls **effort, not visibility** (D-10).

- **D-04:** **"auto" = Aura's existing adaptive policy, unchanged** (user: "auto the model self-adapts like now"). When the user leaves the selector on `auto`, the Composer sends **no override** and the runtime runs `ApplyAdaptiveReasoning` (`internal/agent/prompt/reasoning_policy.go`, driven by `AURA_LLM_ADAPTIVE_REASONING`: greeting→none, search→low, code/proof→high) exactly as today. A **fixed** level (off/low/mid/high) **bypasses the classifier** and forces `req.Reasoning` directly. This makes `auto` a zero-regression default (D-07).

### Server-side governance (RESOLVED — WEBMODEL-03 spirit, effort-only)
- **D-05:** `POST /agent/run` accepts an **optional symbolic effort override** from a fixed enum (`off|low|mid|high|auto`). The **server** maps it to `ReasoningConfig`; a value outside the enum → **400** (never an arbitrary provider knob). Absent/`auto` → today's adaptive default (no regression). The client sends a symbol, not a `ReasoningConfig` — that closes the no-bypass requirement. (No model override exists at all, so model governance is untouched.)

### Persistence (RESOLVED — user: "Claude parity")
- **D-06:** **Persisted per-conversation, restored on reopen** (Claude-parity — not ephemeral). Recommended mechanism: write the chosen level into **`aura.conversations.metadata` jsonb** (the column already exists — `0005_conversations.up.sql`), so **no migration** is needed and the blast radius is minimal. Planner may instead add a typed `reasoning_effort` column if querying/indexing is wanted, but the jsonb path is the default. (This is the "per-conv preference store" that 37C noted was missing — 37E introduces the minimal form of it.)

### Default (RESOLVED — user)
- **D-07:** **New conversations default to `auto`** → identical to today's behavior (adaptive policy runs), so a user who never touches the selector sees zero change.

### Multi-backend coverage (RESOLVED — user: "add on llama.cpp too")
- **D-08:** The effort selector **must take effect on BOTH OpenRouter AND a local llama.cpp chat backend** — not just OpenRouter. Today `IsOpenRouterReasoningTarget` (`reasoning_policy.go:47`) gates `ApplyAdaptiveReasoning` to OpenRouter only, so on a local llama.cpp target `req.Reasoning` stays empty and the knob is a no-op. 37E must extend/generalize the reasoning-target recognition to include llama.cpp AND ensure the wire projection is correct for it. **Evidence-gated (see Architectural Verification below):** the exact llama.cpp per-request contract MUST be confirmed by the researcher before planning locks — the wire client currently emits OpenRouter's nested `reasoning:{effort}` object (`openai_compat/client.go:243 buildWireReasoning`), whereas llama-server may expect top-level `reasoning_effort` (OpenAI-standard) or `chat_template_kwargs:{enable_thinking}` (Qwen3-style toggle). A llama.cpp branch in the wire builder may be required.

### Honest backend reality (MUST surface — not a fork)
- **D-09:** On the **current primary model (DeepSeek-V4 via OpenRouter)**, `low/medium` collapse to `high` server-side (probe-verified), so `low·mid·high` are **effectively the same gear today**; only **off vs. on vs. auto** are genuinely distinct. The five labels are kept for **provider-neutrality + forward-compat** with models that DON'T collapse (and for the llama.cpp path, D-08). Plans/UAT must state this so no one is surprised the mid/high distinction is invisible on DeepSeek.

### Effort vs. visibility (constraint)
- **D-10:** The selector controls **reasoning effort only**. Reasoning **visibility** (whether the CoT text streams to the UI) stays governed by `AURA_SHOW_REASONING` / the `exclude` flag — the selector must not touch it.

### PRD-first gate (mandatory, blocks all code)
- **D-11:** 37E requires a **PRD-amendment BEFORE any code** (mirrors 37B-01 / 37C-01 / 37D-01, D-14/D-19). Wave 1 = PRD-amendment gate: (a) amend **WEBMODEL-01/02** to drop the model-selector clause (effort-only); (b) amend **WEBMODEL-03** to the effort-enum no-bypass form; (c) add the **llama.cpp coverage** requirement (D-08); (d) document the composer effort selector + the `/agent/run` effort field + the per-conversation persistence in `prd.md`. No implementation plan lands before it. The ROADMAP.md 37E entry (committed this session) still describes the model+effort scope as originally chartered — the amendment reconciles roadmap + REQUIREMENTS.md + prd.md.

### Claude's Discretion
- **UI widget/placement** — segmented control vs. small dropdown/pill near the send button (GPT shows a compact pill). Defer to the planner or a `/gsd-ui-phase` UI-SPEC; keep it accessible (ARIA) and non-disruptive to Composer paste/drop/Enter-send (37D D-08/D-09 precedent).
- **Persistence mechanism** — `conversations.metadata` jsonb (recommended, no migration) vs. a typed column (D-06).
- **Override wire seam** — set `req.Reasoning` directly post-build vs. thread a per-turn tier through `buildRequest`; the researcher/planner picks after reading `llm_agent.go` + `builder.go`.
- **Label wording / i18n** — en+it parity for the five levels (`off/low/mid/high/auto` → localized), CI-checked (37B/C/D precedent).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & amendment targets
- `.planning/ROADMAP.md` — Phase 37E section (Goal, WEBMODEL-01..03, Success Criteria, design forks) — added this session.
- `.planning/REQUIREMENTS.md` — WEBMODEL-01, WEBMODEL-02, WEBMODEL-03 (lines ~95-97) — **target of the Wave-1 amendment** (drop model-selector; add llama.cpp coverage).
- `prd.md` — target of the mandatory PRD-amendment (D-11); §Q&A revision protocol.

### Backend — reasoning contract (the heart of this phase)
- `internal/llm/client.go` — `ReasoningConfig{Effort,MaxTokens,Exclude,Enabled}` + `ReasoningEffort` vocabulary (`xhigh/high/medium/low/minimal/none`) + `Request.Reasoning`; `ReasoningConfig.Empty()` (the omit/"auto" path). **The provider-neutral contract the UI levels map onto (D-03).**
- `internal/agent/prompt/reasoning_policy.go` — `ApplyAdaptiveReasoning` (the "auto" path, D-04) + `IsOpenRouterReasoningTarget` (**the OpenRouter-only gate to extend for llama.cpp, D-08**) + `ReasoningTier.reasoning()` (effort→wire map + `exclude` handling, D-10) + the live DeepSeek probe notes (D-09).
- `internal/agent/prompt/builder.go` (~L96-101) — `BuildWithReasoningTier` → `ApplyAdaptiveReasoning(&req, provider, cfg, tier)`: **the injection point** for a per-turn override.
- `internal/agent/llm_agent.go` (~L200-260, L445 `buildRequest`) + `internal/agent/llm_agent_reasoning.go` (`adaptiveReasoningTier`, `ParseReasoningRouterTier`) — where the classifier tier is computed/threaded; the **override seam** (bypass classifier when a fixed level is set).
- `internal/agent/prompt/reasoning_classifier.go` — the 3-tier (none/low/high) LLM classifier that "auto" uses.
- `internal/llm/openai_compat/client.go` (L78/L84 `wireReasoning`, L236, L243 `buildWireReasoning`) + `internal/llm/openai_compat/sse.go` (L20-28 accept-both `reasoning`/`reasoning_content`) — **the wire projection**; a **llama.cpp branch may be needed** (D-08 evidence gate).

### Backend — run request + auth + model-selection-lives-here
- `internal/agui/server.go` — `handleRun` (the run DTO decode; add the `effort` field, mirror 37D's pinned-skill field); `mux.HandleFunc("POST /agent/run", s.handleRun)` (L186).
- `cmd/aura/serve_webui.go` (L70) — `/agent/run` auth mount (`RequireAuth`).
- `internal/settings/settings.go` (`AllowedKeys` → `AURA_LLM_MODEL`) + `internal/agui/settings_api.go` — **where MODEL selection already lives** (the reason the Composer drops it, D-01).

### Persistence
- `internal/db/migrations/0005_conversations.up.sql` — `aura.conversations` incl. the **`metadata jsonb`** column (D-06 persistence target) + the existing `model text` column.

### Frontend — Composer integration
- `web/src/chat/Composer.tsx` — the integration point; 37D's send-payload/pinned-field pattern is the model to mirror for carrying the effort on send.
- `web/src/chat/ExternalStoreChat.tsx` / `web/src/AppShell.tsx` — the send/submit path + conversation state (per-conversation restore of the level, D-06).
- `.planning/phases/37D-composer-skill-picker/37D-CONTEXT.md` — Composer send-payload wire path, ARIA, i18n en+it parity, ≥85% + Playwright conventions.
- `.planning/phases/37C-web-voice-lane-inserted/37C-CONTEXT.md` — the "no per-conv preference store exists" note (D-06 context) + setter-injection / degrade conventions.

### Live-proof harness (reasoning behavior ground truth)
- `scripts/deepseek_reasoning_probe.py` + `internal/agent/prompt/adaptive_reasoning_live_e2e_test.go` — the source of D-03/D-09 claims (effort:none off-switch; low/med→high collapse). The llama.cpp path needs its own equivalent evidence (D-08).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`llm.ReasoningConfig` + `ReasoningEffort` vocabulary** (`internal/llm/client.go`): the exact contract the five UI levels map onto — no new LLM type needed (D-03).
- **`ReasoningTier.reasoning(showReasoning)`** (`reasoning_policy.go:78`): the effort→wire projection + `exclude` handling to reuse for fixed overrides (keeps `exclude` parity, D-10).
- **`buildWireReasoning` + accept-both SSE** (`openai_compat/client.go`, `sse.go`): the wire already serializes `reasoning:{effort}` and reads local (`reasoning`) or remote (`reasoning_content`) reasoning — local reasoning chat is already a supported path (D-08 starting point).
- **37D Composer send-payload field**: the just-shipped pattern for carrying a new per-turn value (pinned skill) composer→run request — mirror it for `effort`.
- **`aura.conversations.metadata` jsonb**: an existing per-conversation store — the minimal persistence target (D-06), no migration.

### Established Patterns
- New authenticated routes/fields ride the existing `POST /agent/run` behind the whole-mux `RequireAuth` wrap (`serve_webui.go`); server-side validation before use (settings API precedent).
- Setter-injection at the composition root (`SetSettingsStore`/`SetVoice` precedent) if any new server dependency is needed.
- i18n keys in en+it with CI parity; web coverage ≥85% + vitest + Playwright e2e; Stryker on pure modules (37B/C/D precedent).

### Integration Points
- **Composer → run DTO → agent:** the selected symbolic level flows composer → `POST /agent/run` body → `handleRun` (validate) → Runner → `LlmAgent`. When fixed, it **bypasses `adaptiveReasoningTier`** and sets `req.Reasoning` via `builder.go`'s `ApplyAdaptiveReasoning` seam; when `auto`, the classifier runs as today.
- **Multi-backend gate:** extend/generalize `IsOpenRouterReasoningTarget` so a fixed override reaches a llama.cpp target, plus the correct wire projection (D-08 research).
- **Persistence:** on send/change, write the level to `conversations.metadata`; on thread open, hydrate the selector from it (D-06).

</code_context>

<specifics>
## Specific Ideas

- **"like GPT"** (user): the reference is GPT's compact reasoning-effort control with `off · low · mid · high · auto`. That exact vocabulary is locked (D-02); the visual is a small selector near the send affordance (Claude's discretion / UI-SPEC).
- **"auto the model self-adapts like now"** (user): `auto` must reuse the current adaptive reasoning policy, not a provider default (D-04).
- **"add on llama.cpp too"** (user): effort must work on a local llama.cpp chat backend, not only OpenRouter (D-08).
- **"model selection we do in settings"** (user): confirms the model dropdown is out; Settings owns it (D-01).

</specifics>

<deferred>
## Deferred Ideas

- **Composer model dropdown** — explicitly OUT (model lives in Settings, D-01). Not lost, just not here.
- **Per-message effort override** (GPT lets you change it mid-conversation per message) — 37E locks **per-conversation** (Claude parity, D-06). Per-message granularity could be a future refinement.
- **`xhigh` / `minimal` levels + reasoning-budget (MaxTokens) knob** — the `ReasoningConfig` vocabulary supports them; 37E exposes only the five agreed levels. Exposing the finer gears / a token budget is a future idea.
- **Surfacing model-specific effort behavior in the UI** (e.g., "this model collapses low→high") — D-09 documents the reality; an in-UI hint is a future nicety, not scoped.

### Reviewed Todos (not folded)
None — `todo.match-phase 37E` returned 0 matches.

</deferred>

---

*Phase: 37E-composer-model-reasoning-effort-selector*
*Context gathered: 2026-07-10*
