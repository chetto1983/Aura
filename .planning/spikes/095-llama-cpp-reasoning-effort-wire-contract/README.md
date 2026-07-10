---
spike: 095
name: llama-cpp-reasoning-effort-wire-contract
type: standard
validates: "Given llama-server + gemma-4-E2B-it-qat, when the composer effort (off/low/mid/high/auto) is sent as each candidate per-request wire shape, then observe which field toggles/graduates reasoning_content → the exact wire branch Aura's openai_compat client needs for the llama.cpp path (Phase 37E D-08)"
verdict: VALIDATED
related: [048-gemma4-mtp-gpu-fit, 050-gemma4-mtp-multimodal, 040-adaptive-reasoning-source-truth, 042-adaptive-budget-policy-shim, 052-reasoning-tier-embed-classifier]
tags: [gemma4, llama-cpp, reasoning, reasoning-effort, phase-37E, wire-contract, local-llm]
---

# Spike 095: llama.cpp reasoning-effort wire contract (Phase 37E D-08)

## What This Validates

Given llama.cpp `server-cuda` serving `gemma-4-E2B-it-qat` (the validated local
thinking model from spikes 048/050) on the RTX A2000 4 GB, when the same
reasoning-inducing prompt is sent under each candidate **per-request** wire
shape, then observe which field actually toggles/graduates the
`reasoning_content` channel. The output is the exact wire branch Aura's
`internal/llm/openai_compat` client needs so the **Phase 37E** composer effort
selector (`off · low · mid · high · auto`, CONTEXT.md D-02/D-03) works on a
local llama.cpp backend, not just OpenRouter (D-08).

The per-turn constraint is the point: 37E changes effort **per conversation**,
so the knob MUST be a request-body field, not a server launch flag (no restart).

## Research

Prior art already settles the model-feasibility half — this spike does NOT re-run it:

- **Spike 048 (VALIDATED):** `gemma-4-E2B-it-qat` UD-Q4_K_XL (2.44 GB) fully GPU-offloaded fits the 4 GB A2000 (VRAM 3705/4096); Gemma 4 **is a thinking model** — `reasoning_content` consumes `max_tokens`, so a client must budget for it or disable it.
- **Spike 050 (VALIDATED):** the disable knob is `chat_template_kwargs:{enable_thinking:false}` (else runaway reasoning → empty content).
- **Spikes 040/042 (VALIDATED):** Aura's adaptive-reasoning policy + OpenRouter `reasoning`-object mapping = 37E's "auto".

llama.cpp server reasoning surface ([server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md), [discussion #20408](https://github.com/ggml-org/llama.cpp/discussions/20408)):

| Knob | Kind | Notes |
|------|------|-------|
| `reasoning: on/off/auto` | field/flag | default auto (detect from template) |
| thinking token budget `-1/0/N` | `--reasoning-budget` (+ maybe request) | 0 = immediate end; N = a real budget → potential low/mid/high gradation |
| `chat_template_kwargs:{enable_thinking}` | request body | model-template toggle (spike 050 knob); needs `--jinja` |
| `reasoning_effort` | via chat_template_kwargs only | only if the model was TRAINED with effort levels |
| `--reasoning-format` | server flag | parse `<think>` → `reasoning_content`; `none` = raw |

**Aura today** (`internal/llm/openai_compat/client.go` `buildWireReasoning`) emits OpenRouter's **nested `reasoning:{effort,exclude,…}` object** — NOT any of the above. Hypothesis: llama-server ignores it → 37E needs a llama.cpp branch. This spike proves/refutes that empirically.

**User's linked file caveat:** `mradermacher/gemma-4-E2B` Q4_K_S (3.37 GB) is the **base** `google/gemma-4-E2B` (no thinking template advertised) and a worse 4 GB fit; the spike uses the validated unsloth `-it-qat` instead (operator-approved at the align checkpoint).

## How to Run

```powershell
# 1. serve (PowerShell — never Git-Bash; MSYS mangles /models. --jinja REQUIRED
#    for chat_template_kwargs + reasoning parsing; no MTP draft — irrelevant here)
docker run -d --name spike095 --gpus all -p 8096:8080 `
  -v D:\tmp\spike-095-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  -ngl 99 -c 4096 --temp 0 --jinja --reasoning-format auto `
  --host 0.0.0.0 --port 8080

# 2. probe (host go run; pure stdlib HTTP to 127.0.0.1:8096)
go run ./.planning/spikes/095-llama-cpp-reasoning-effort-wire-contract

# 3. teardown
docker rm -f spike095
```

## What to Expect

- `baseline` (no reasoning field) → `reasoning_content` **> 0** (thinking ON by default on Gemma-4-it).
- Exactly which of the 10 variants drives `reasoning_content` → 0 (with a non-empty answer) identifies the **OFF switch**; whether `reasoning_budget_64` yields a *reduced* (but non-zero) reasoning identifies real **gradation**.
- If `aura_nested_none` / `aura_nested_high` match the baseline → llama-server **ignores** Aura's current OpenRouter shape → 37E MUST add a llama.cpp wire branch.

## Investigation Trail

1. **Model fit re-confirmed.** Downloaded unsloth `gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf` (2,620,368,960 B, `GGUF` magic verified) → `D:\tmp\spike-095-models`. Served on GPU (`-ngl 99 -c 4096 --jinja --reasoning-format auto`). VRAM **3606 / 4096 MiB** with the desktop coexisting — matches spike 048 (3705), holds on the newer image.
2. **First probe matrix (10 per-request shapes).** Only `chat_template_kwargs:{enable_thinking:false}` turned thinking OFF (`reasoning_content` 1410 B → **0 B**, answered directly). Every other shape — including Aura's CURRENT `reasoning:{effort}` object, `reasoning:"off"`, `reasoning_effort:"low"`, and (wrongly-named) `reasoning_budget` — left reasoning at full length. Interim conclusion: "on/off only, no gradation."
3. **Operator correction (screenshot + "search online, don't suppose").** The operator showed llama.cpp's OWN webui serving this exact model with a graduated **Off / Low (512) / Medium (2048) / High (8192) / Max (unlimited)** "Reasoning effort" selector — so gradation IS possible; my field name was wrong.
4. **Authoritative source** ([llama.cpp discussion #21445](https://github.com/ggml-org/llama.cpp/discussions/21445)): the per-request field is **`thinking_budget_tokens`** (int), gated by `if (reasoning_budget == -1 && body.contains("thinking_budget_tokens"))` — it only applies when the server is started WITHOUT `--reasoning-budget` (my launch was, so it works).
5. **Second probe matrix — gradation proven.** Swapped in `thinking_budget_tokens`: reasoning_content scales monotonically with the budget — **64→214 B, 128→347 B, 256→612 B, 1024→full 1411 B**. The webui's Low/Medium/High/Max are exactly `thinking_budget_tokens` 512/2048/8192/-1; Off is `enable_thinking:false`.

## Results

**VALIDATED.** The Phase 37E llama.cpp reasoning wire contract is settled, and gemma-4-E2B-it-qat is a viable local reasoning backend for it.

### Reasoning-toggle matrix (reasoning_content bytes; baseline ≈ 528 reasoning tokens)

| Per-request field | reasoning | Effect |
|---|---|---|
| *(baseline, none)* | 1410 | thinking ON (default) |
| `reasoning:{effort:"none",exclude:true}` **(Aura today)** | 1410 | **IGNORED** |
| `reasoning:{effort:"high"}` | 1410 | IGNORED |
| `reasoning:"off"` / `reasoning:"on"` (llama string) | 1410 | IGNORED (per-request) |
| `reasoning_effort:"low"` (OpenAI top-level) | 1411 | IGNORED (Gemma template has no effort levels) |
| `chat_template_kwargs:{enable_thinking:false}` | **0** | **OFF** ✓ |
| `thinking_budget_tokens:64 / 128 / 256 / 1024` | **214 / 347 / 612 / 1411** | **GRADUATED** ✓ (monotonic cap) |

### The 37E llama.cpp wire contract (for `internal/llm/openai_compat`)

| 37E level | llama.cpp per-request field | llama.cpp webui equivalent |
|---|---|---|
| **off** | `chat_template_kwargs:{enable_thinking:false}` | Off |
| **low** | `thinking_budget_tokens: 512` | Low |
| **mid** | `thinking_budget_tokens: 2048` | Medium |
| **high** | `thinking_budget_tokens: 8192` | High |
| **max** | `thinking_budget_tokens: -1` (or omit) | Max (Unlimited) |
| **auto** | *(no override)* → Aura's adaptive policy maps to one of the above per tier | — |

**Server requirements (must be documented for 37E):** launch llama-server WITH `--jinja` (else `chat_template_kwargs` is ignored) and WITHOUT `--reasoning-budget` (else per-request `thinking_budget_tokens` is locked out, per #21445).

### Key findings

- **Aura's current OpenRouter `reasoning:{effort}` object is a NO-OP on llama-server** → 37E MUST add a llama.cpp branch to `buildWireReasoning` (confirms CONTEXT.md D-08's hypothesis empirically).
- **The token-budget model UNIFIES both backends.** llama.cpp's Low/Med/High/Max token budgets (512/2048/8192/∞) also express cleanly on OpenRouter via `reasoning.max_tokens` — which gives DeepSeek REAL gradation, sidestepping the effort-label collapse noted in CONTEXT.md D-09. Recommend 37E define the levels by token budget, not just effort string.
- **The operator's screenshot = the 37E reference UI.** llama.cpp's own selector is Off/Low/Medium/High/**Max** (Max = unlimited). Worth reconciling with the earlier `off/low/mid/high/auto`: "Max" (unlimited budget) and "auto" (Aura adaptive) are different axes — 37E can offer both, or adopt the llama.cpp reference set.
- **Correctness signal:** thinking-OFF made the little model answer WRONG ("first train faster by 15 km/h") while thinking-ON answered RIGHT (both 60 km/h, equal). The effort selector has real quality impact.
- **GPU fit holds:** 3606/4096 MiB, ~consistent with spike 048.

### The operator's linked file — NOT used, and why

`mradermacher/gemma-4-E2B` Q4_K_S = 3.37 GB is quants of the **base** `google/gemma-4-E2B` (no thinking template) and a worse 4 GB fit. The validated unsloth `-it-qat` UD-Q4_K_XL (2.44 GB) is the correct local pick (operator-approved at the align checkpoint).
