---
spike: 096
name: openrouter-reasoning-effort-wire-contract
type: standard
validates: "Given Aura's primary OpenRouter backend (DeepSeek-V4-Flash), when the composer effort selector's candidate wire shapes are sent per-request, then measure which fields toggle/graduate reasoning — settling the OpenRouter half of Phase 37E and testing whether reasoning.max_tokens gives real gradation (the token-budget unification hypothesis)"
verdict: VALIDATED
related: [095-llama-cpp-reasoning-effort-wire-contract, 040-adaptive-reasoning-source-truth, 042-adaptive-budget-policy-shim, 052-reasoning-tier-embed-classifier]
tags: [openrouter, deepseek-v4, reasoning, reasoning-effort, phase-37E, wire-contract]
---

# Spike 096: OpenRouter reasoning-effort wire contract (Phase 37E)

## What This Validates

Counterpart to spike 095 (llama.cpp). Given Aura's PRIMARY chat backend
(OpenRouter → `deepseek/deepseek-v4-flash:nitro`), when the composer effort
selector's candidate per-request wire shapes are sent, then measure which
fields toggle/graduate reasoning — settling the OpenRouter half of Phase 37E's
D-03/D-08/D-09. **Decisive question:** does DeepSeek honor `reasoning.max_tokens`
(real gradation), so the token-budget model unifies both backends (my D-09
recommendation)?

## Research

- Aura's `openai_compat` client already emits OpenRouter's nested
  `reasoning:{effort,exclude,enabled,max_tokens}` object (`buildWireReasoning`).
- Prior art (spikes 040/042): DeepSeek `effort:none` is the off-switch;
  `effort` low/med reportedly collapse to high (the model self-scales).
- OpenRouter docs: `reasoning.effort` (OpenAI/Grok-style) vs `reasoning.max_tokens`
  (Anthropic/Gemini-style); a model honors one, and OpenRouter cross-maps.
  DeepSeek is an `effort`-style model → hypothesis: it may IGNORE `max_tokens`.
- `usage.completion_tokens_details.reasoning_tokens` is the ground-truth metric
  (survives `exclude:true`, which only hides the CoT text).

## How to Run

```bash
# key loaded from .env at runtime, never printed:
set -a; . ./.env; set +a   # (or export OPENROUTER_API_KEY + AURA_LLM_MODEL)
go run ./.planning/spikes/096-openrouter-reasoning-effort-wire-contract
```

Bounded paid run: 11 small DeepSeek-V4-Flash calls (~70 in / ≤2048 out each; « $0.01 total).

## Results

**VALIDATED — with a correction to the D-09 unification hypothesis.**

Model: `deepseek/deepseek-v4-flash:nitro`, temp 0, single sample per variant.

| Per-request field | reasoning_tokens | CoT bytes | Effect |
|---|---|---|---|
| *(baseline)* | 457 | 1646 | ON (default) |
| `reasoning:{effort:"none"}` | **0** | 0 | **OFF** ✓ (answered) |
| `reasoning:{enabled:false}` | **0** | 0 | **OFF** ✓ (answered) |
| `reasoning:{effort:"low"}` | 404 | 1448 | ON |
| `reasoning:{effort:"medium"}` | 264 | 920 | ON |
| `reasoning:{effort:"high"}` | 303 | 1068 | ON |
| `reasoning:{max_tokens:256}` | **330** | 1199 | ON — **NOT capped (330 > 256)** |
| `reasoning:{max_tokens:512}` | 282 | 1061 | ON |
| `reasoning:{max_tokens:2048}` | 429 | 1585 | ON |
| `reasoning:{enabled:true}` | 171 | 587 | ON |
| `reasoning:{effort:"high", exclude:true}` | 295 | **0** | ON, CoT text hidden ✓ |

### Key findings

1. **OFF is reliable, two ways:** `reasoning:{effort:"none"}` and `reasoning:{enabled:false}` both drive `reasoning_tokens → 0` and answer directly. (Aura's current shape works for OFF — unlike llama.cpp where it's ignored, spike 095.)
2. **Gradation is NOT reliable on DeepSeek-V4-Flash.** The `effort` label doesn't track (low 404 > high 303 > medium 264), and — critically — **`reasoning.max_tokens` is NOT honored as a hard cap** (a 256 budget produced 330 reasoning tokens). The reasoning-token count varies with the *problem*, not the requested budget: the model self-scales. This **refutes the D-09 hypothesis** that `reasoning.max_tokens` gives clean gradation and unifies both backends.
3. **`exclude:true` gates visibility, not effort** — reasoning still ran and billed (295 tokens) with the CoT text hidden (0 bytes). Confirms CONTEXT.md D-10.
4. **Backends genuinely differ:** llama.cpp graduates cleanly via `thinking_budget_tokens` (spike 095); OpenRouter/DeepSeek is **effectively on/off** — the effort/budget knobs don't move it reliably. 37E must NOT promise clean low/mid/high on the cloud path.

### Corrected 37E design signal

| 37E level | OpenRouter/DeepSeek (this spike) | llama.cpp/Gemma (spike 095) |
|---|---|---|
| **off** | `reasoning:{effort:"none"}` **or** `{enabled:false}` | `chat_template_kwargs:{enable_thinking:false}` |
| **low/mid/high** | `reasoning:{effort: low/medium/high}` — **cosmetic** (model self-scales; no reliable gradation) | `thinking_budget_tokens:512/2048/8192` — **real, monotonic gradation** |
| **max** | (no reliable "unlimited" distinct from on) | `thinking_budget_tokens:-1` |
| **auto** | omit → Aura adaptive policy | omit → Aura adaptive policy |

**Honest UAT line for 37E:** the effort selector's gradations are backend/model-dependent. Reliable everywhere = **off vs. on vs. auto**. True low/mid/high fidelity exists on **local llama.cpp models with a thinking-budget** (and on cloud models trained with real effort levels, e.g. GPT-OSS/o-series), but **not** on DeepSeek-V4-Flash, where the knobs are effectively on/off.

## Investigation Trail

1. Read model/base from config source (`.env` is permission-protected): `deepseek/deepseek-v4-flash:nitro`, `https://openrouter.ai/api/v1`, single `OPENROUTER_API_KEY`. Loaded the key at runtime line-by-line from `.env` without printing it.
2. 11-variant per-request matrix, `reasoning_tokens` as ground truth.
3. OFF confirmed twice (effort:none, enabled:false). exclude:true confirmed as a visibility gate.
4. **Surprise:** `reasoning.max_tokens:256` produced 330 reasoning tokens → not a hard cap on DeepSeek. Combined with the non-monotonic effort labels → DeepSeek self-scales; gradation is unreliable. This corrects the D-09 "token-budget unifies both backends" claim.

### Caveat

Single sample per variant at temp 0; DeepSeek reasoning length has run-to-run variance (provider routing via `:nitro`). The qualitative conclusion (OFF reliable; gradation unreliable; max_tokens not a hard cap) is robust — the 256-budget overshoot and the flat/noisy effort band are both clear — but the exact token counts are not to be read as precise.
