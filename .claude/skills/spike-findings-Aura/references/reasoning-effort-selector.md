# reasoning-effort selector (composer per-turn "thinking" control)

Build recipe for a per-turn reasoning-effort selector in the cockpit Composer
(`off · low · mid · high · auto`, GPT/llama.cpp-style) that drives BOTH Aura's
cloud backend (OpenRouter/DeepSeek-V4) and a local llama.cpp backend. This is
the empirically-settled wire contract for **Phase 37E** (WEBMODEL). It is the
per-turn USER-facing control — distinct from the automatic tier classifier in
`adaptive-reasoning.md` (spikes 052/053), which is what `auto` delegates to.

Verified live on `gemma-4-E2B-it-qat` (llama.cpp, GPU) and
`deepseek/deepseek-v4-flash:nitro` (OpenRouter) — spikes 095 + 096.

## Requirements

- **Effort-only in the Composer; NOT a model picker.** Model selection already
  lives in the Settings page (`internal/settings/settings.go` `AllowedKeys` →
  `AURA_LLM_MODEL`). 37E adds ONLY the effort control.
- **`auto` = Aura's existing adaptive policy** (`internal/agent/prompt/reasoning_policy.go`,
  `AURA_LLM_ADAPTIVE_REASONING`). Selecting `auto` sends NO override; a fixed
  level bypasses the classifier. New conversations default to `auto` (zero regression).
- **Server-side governance:** the client sends a symbolic level from a fixed enum;
  the SERVER maps it to the wire shape. Never accept a raw provider reasoning
  payload from the browser. A non-enum value → 400.
- **The selector controls EFFORT, not VISIBILITY.** Reasoning visibility stays
  governed by `AURA_SHOW_REASONING` / the `exclude` flag. Verified: `exclude:true`
  hides the CoT text but reasoning still runs+bills (spike 096: 295 tok, 0 bytes).
- **Gradation fidelity is backend-dependent — do NOT promise uniform low/mid/high.**
  (See "What to Avoid".)

## How to Build It

### The per-backend wire contract (the whole point)

Aura's `internal/llm/openai_compat/client.go` `buildWireReasoning` currently emits
OpenRouter's nested `reasoning:{effort,exclude,enabled,max_tokens}` object. That
object is **correct for OpenRouter but IGNORED by llama-server**. Branch by target.

| Level | OpenRouter / DeepSeek (spike 096) | llama.cpp / Gemma-4 (spike 095) |
|-------|-----------------------------------|----------------------------------|
| **off** | `reasoning:{effort:"none"}` **or** `reasoning:{enabled:false}` | `chat_template_kwargs:{enable_thinking:false}` |
| **low** | `reasoning:{effort:"low"}` (cosmetic) | `thinking_budget_tokens:512` |
| **mid** | `reasoning:{effort:"medium"}` (cosmetic) | `thinking_budget_tokens:2048` |
| **high** | `reasoning:{effort:"high"}` (cosmetic) | `thinking_budget_tokens:8192` |
| **max** | (no reliable "unlimited" distinct from on) | `thinking_budget_tokens:-1` (or omit) |
| **auto** | omit override → adaptive policy | omit override → adaptive policy |

- **Aura's existing OpenRouter path already does OFF correctly** — no OpenRouter
  wire change needed. The **net-new work is the llama.cpp branch** in
  `buildWireReasoning` emitting `chat_template_kwargs.enable_thinking` +
  `thinking_budget_tokens`.
- The wire client is already OpenAI-compat and accept-both on the response
  (`openai_compat/sse.go` decodes `reasoning` [local] or `reasoning_content`
  [DeepSeek]). Only the REQUEST projection needs the branch.

### The override seam in the agent loop

- Injection point: `internal/agent/prompt/builder.go` `BuildWithReasoningTier`
  calls `ApplyAdaptiveReasoning(&req, provider, cfg, tier)`.
- The tier is computed by `LlmAgent.adaptiveReasoningTier()` (`llm_agent_reasoning.go`)
  and threaded via `LlmAgent.buildRequest()` (`llm_agent.go`).
- **Fixed level → bypass `adaptiveReasoningTier` and set `req.Reasoning` directly.
  `auto` → run the classifier as today.**
- `IsOpenRouterReasoningTarget` (`reasoning_policy.go:47`) gates the adaptive
  policy to OpenRouter only — **generalize it** so a fixed override also reaches
  a llama.cpp target.

### Server config for a local llama.cpp reasoning backend

Launch llama-server WITH `--jinja` (else `chat_template_kwargs` is ignored) and
WITHOUT `--reasoning-budget` (else per-request `thinking_budget_tokens` is locked
out — llama.cpp discussion #21445). Model: unsloth `gemma-4-E2B-it-qat`
UD-Q4_K_XL (2.44 GB, GPU-fit 3606/4096 on the A2000).

```
docker run -d --gpus all -p <port>:8080 -v <dir>:/models:ro \
  ghcr.io/ggml-org/llama.cpp:server-cuda \
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf \
  -ngl 99 -c 4096 --temp 0 --jinja --reasoning-format auto --host 0.0.0.0 --port 8080
```

### Persistence + UI (37E)

- Persist the chosen level per-conversation (Claude parity) in
  `aura.conversations.metadata` jsonb (the column exists — no migration).
- UI: a compact selector near send (ARIA), must not break Composer
  paste/drop/Enter-send. Reference visual = llama.cpp's own webui
  (Off/Low/Medium/High/Max with token budgets).

## What to Avoid

- **Do NOT send Aura's OpenRouter `reasoning:{effort}` object to llama-server** —
  it is silently ignored (spike 095: reasoning stayed full). Branch the wire.
- **Do NOT promise clean low/mid/high on DeepSeek-V4-Flash.** Spike 096: the
  `effort` labels don't track (low 404 > high 303 > med 264 tok) and
  `reasoning.max_tokens` is **NOT a hard cap** (256 budget → 330 reasoning tokens).
  The model self-scales; the cloud path is effectively **on/off**. Real gradation
  exists on llama.cpp (`thinking_budget_tokens`, proven monotonic) and on cloud
  models trained with effort levels (o-series/GPT-OSS) — not DeepSeek.
- **Do NOT reach for `reasoning:"off"` / top-level `reasoning_effort` on llama-server** —
  both ignored per-request (spike 095). Only `enable_thinking` / `thinking_budget_tokens` work.
- **Do NOT conflate effort with visibility.** `exclude:true` only hides CoT text.
- **Do NOT use the base `mradermacher/gemma-4-E2B` Q4_K_S (3.37 GB)** — base model,
  no thinking template, worse 4 GB fit. Use unsloth `-it-qat`.
- **Do NOT cap the response `max_tokens` by tier** — it truncated tool-call JSON
  (the 203-turn disaster, see `reasoning_policy.go`). Budget reasoning via the
  reasoning-specific knob, never the answer budget.

## Constraints

- Off is reliable on both backends; gradation is not uniform (see above).
- llama.cpp per-request thinking budget requires `--jinja` + no `--reasoning-budget`.
- `usage.completion_tokens_details.reasoning_tokens` is the ground-truth metric
  (survives `exclude:true`); llama.cpp exposes `reasoning_content` bytes.
- Small local models answer WORSE with thinking off (spike 095: Gemma got the
  arithmetic wrong off, right on) — effort has real quality impact, not cosmetic.
- Cost of the OpenRouter probe: 11 DeepSeek-Flash calls, « $0.01.

## Origin

Synthesized from spikes: 095 (llama.cpp wire contract, VALIDATED), 096 (OpenRouter
wire contract, VALIDATED — corrected the token-budget-unifies hypothesis).
Builds on 040/042/052/053 (see `adaptive-reasoning.md`).
Source files available in: `sources/095-llama-cpp-reasoning-effort-wire-contract/`,
`sources/096-openrouter-reasoning-effort-wire-contract/`.
Feeds Phase 37E — see `.planning/phases/37E-*/37E-CONTEXT.md` (D-03/D-08/D-09/D-10).
