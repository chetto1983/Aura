# Phase 37E: Composer Model & Reasoning-Effort Selector - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-10
**Phase:** 37E-composer-model-reasoning-effort-selector
**Areas discussed:** Roadmap gap (37E had no heading), Model-vs-effort scope, "auto" semantics, Persistence, Default level, Multi-backend (llama.cpp)

---

## Pre-req: Roadmap gap

Phase 37E was defined in REQUIREMENTS.md (WEBMODEL-01..03), forward-referenced by 37C, and tracked in the v2.0.0 milestone audit as unbuilt — but ROADMAP.md skipped 37D→37F, so `gsd query init.phase-op 37E` returned `phase_found:false` and blocked discuss-phase.

| Option | Description | Selected |
|--------|-------------|----------|
| Add 37E entry, then discuss | Insert `#### Phase 37E:` heading into ROADMAP.md (goal + WEBMODEL-01..03 + success criteria + design forks), then run discuss-phase | ✓ |
| Add entry, then stop for review | Insert heading, stop for user review before discussing | |
| Show me the draft first | Draft without writing | |

**User's choice:** Add 37E entry, then discuss.
**Notes:** Roadmap entry committed separately (`docs(roadmap): add Phase 37E heading`). The entry describes the model+effort scope as originally chartered; the scope reductions below are reconciled by the Wave-1 PRD-amendment.

---

## Model selector vs. effort selector

| Option | Description | Selected |
|--------|-------------|----------|
| Model dropdown + effort selector (as chartered) | Composer exposes both, per WEBMODEL-01 | |
| Effort selector only | Drop the model dropdown; model stays a Settings-page concern | ✓ |

**User's choice:** "delete model selection we do in settings!" → effort selector only.
**Notes:** Confirmed model selection already lives in the Settings page (`internal/settings/settings.go` `AURA_LLM_MODEL`, served by `settings_api.go`). Central architectural blocker dissolved: there is no multi-model allowlist in the codebase (`Request.Model` is a bare string), so a Composer model picker would have needed a net-new allowlist concept — now moot.

---

## Effort levels

**User's choice (unprompted):** "we must add thinking exposed in frontend (off, low, mid, high; auto) like gpt" → five levels `off · low · mid · high · auto`, GPT-style.
**Notes:** Mapped server-side onto the existing `llm.ReasoningConfig` vocabulary (off→none, low→low, mid→medium, high→high, auto→omit/adaptive). The client sends a symbol; the server does the mapping (governance).

---

## "auto" semantics

| Option | Description | Selected |
|--------|-------------|----------|
| = Aura's adaptive policy | "auto" sends no override; today's `AURA_LLM_ADAPTIVE_REASONING` policy decides per message | ✓ |
| = provider/model default | "auto" omits the knob and lets OpenRouter/the model pick, bypassing the adaptive policy | |

**User's choice:** "auto il model self adapt like now! read backend reasoning effort" → adaptive policy.
**Notes:** Grounded in `internal/agent/prompt/reasoning_policy.go` (`ApplyAdaptiveReasoning`, greeting→none / search→low / code→high). Fixed levels bypass the classifier; `auto` runs it as today → zero regression.

---

## Persistence

| Option | Description | Selected |
|--------|-------------|----------|
| Per-conv via `metadata` jsonb | Persist in `conversations.metadata` (no migration), restore on reopen | ✓ (recommended mechanism) |
| Per-conv via new column | Add typed `reasoning_effort` column (a migration) | |
| Ephemeral (no persistence) | Session-scoped React state; resets on reload | |

**User's choice:** "claude parity" → persisted per-conversation, restored on reopen (not ephemeral).
**Notes:** Interpreted as Claude-like sticky-per-conversation behavior. Recommended the `conversations.metadata` jsonb path (no migration, smallest blast radius); this is the "per-conv preference store" 37C noted was missing.

---

## Default level

| Option | Description | Selected |
|--------|-------------|----------|
| auto | New conversations start at "auto" — identical to today | ✓ |
| mid | New conversations start at a fixed medium effort | |

**User's choice:** auto.
**Notes:** Zero regression for users who never touch the selector.

---

## Multi-backend (llama.cpp)

**User's choice (unprompted):** "we must add on llama.cpp too" → the effort selector must work on a local llama.cpp chat backend, not only OpenRouter.
**Notes:** `IsOpenRouterReasoningTarget` currently gates the reasoning projection to OpenRouter only, so llama.cpp is a no-op today. The wire client is already provider-neutral (emits `reasoning:{effort}`, accept-both SSE), but the exact llama.cpp per-request contract (top-level `reasoning_effort` vs `chat_template_kwargs.enable_thinking` vs nested `reasoning{}`) must be verified by the researcher before planning locks (evidence gate, CONTEXT.md D-08).

---

## Claude's Discretion
- UI widget/placement of the selector (segmented control vs. compact pill near send) — defer to planner / `/gsd-ui-phase`.
- Persistence mechanism (`metadata` jsonb recommended vs. typed column).
- The override wire seam (direct `req.Reasoning` post-build vs. threaded tier).
- Label wording + en/it i18n for the five levels.

## Deferred Ideas
- Composer model dropdown (explicitly out — Settings owns it).
- Per-message (vs per-conversation) effort override — GPT-style per-message granularity.
- `xhigh`/`minimal` levels + a reasoning-budget (MaxTokens) knob.
- An in-UI hint that a given model collapses low→high (D-09 reality).

## Honest backend reality captured (CONTEXT.md D-09)
On the current primary model (DeepSeek-V4 via OpenRouter), `low/medium` collapse to `high` server-side — so today only off/on/auto are visibly distinct. The five labels are kept for provider-neutrality + forward-compat + the llama.cpp path.
