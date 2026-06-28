---
spike: 071
name: fc270m-baseline-and-slot
type: standard
validates: "Given BASE (un-finetuned) FunctionGemma-270m served locally + Aura's real tool defs (manifest.go) + grounded queries (bitcoin/meteo/mail/doc-search), when it emits tool_calls, then measure valid-JSON %, top-1 tool %, arg-correctness %, CPU & GPU latency, VRAM-fit beside the primary model, and the honest production slot vs the shipped embedding ranker"
verdict: PARTIAL
related: [052-reasoning-tier-embed-classifier, 054-semantic-toolsearch-vs-bm25, 057-toolselection-oracle-signal, 048-gemma4-mtp-gpu-fit, 058-unified-embedding-index]
tags: [functiongemma, local-llm, function-calling, tool-calling, gemma, slice-13, baseline, latency]
---

# Spike 071: FunctionGemma-270m baseline + production slot

## What This Validates

The kill-gate for the "finetune FunctionGemma on Aura tools" idea. Before spending
hours on a finetune toolchain (072/074), answer cheaply: does the **base** model
emit valid tool calls against Aura's real schemas at all, at what latency on this
hardware, does it even fit VRAM beside the primary model, and — given embeddings
already solved tool *selection* for free — what is the defensible production slot?

## Research

**Model.** `unsloth/functiongemma-270m-it-GGUF` — Google FunctionGemma, a Gemma-3
270M variant trained for **text-only function calling**, 32K ctx. GGUF quants
180 MB (IQ2) → 543 MB (BF16); Q8_0 = 292 MB. Runs in ~550 MB RAM on CPU.

**Two premise corrections (factual):**
1. **You can't finetune the `-GGUF` repo.** GGUF is inference-only. LoRA/QLoRA
   trains the **safetensors** base (`unsloth/functiongemma-270m-it`) or the
   `…-unsloth-bnb-4bit` variant, then *exports* GGUF (→ spike 072).
2. **Embeddings already own tool *selection*** (spikes 054-058, SHIPPED). A 270M
   decoder will not beat ~85 µs granite cosine at "which tool?". The only thing
   FunctionGemma uniquely adds is **generating the call + arguments** with no
   cloud → its home is the future **Slice 13 local/offline fallback**.

**Non-standard call format** (Unsloth docs):
- Developer preamble: `<start_of_turn>developer\nYou are a model that can do function calling with the following functions`
- Declarations: `<start_function_declaration>declaration:NAME{description:<escape>…<escape>,parameters:{…}}<end_function_declaration>`
- Calls: `<start_function_call>call:web_search{query: "…"}<end_function_call>`, optional `<think>…</think>` CoT, `<escape>` delimits string values.
- Google sampling rec: `temp=1.0, top_k=64, top_p=0.95`.

| Approach | How tools reach the model | Status |
|---|---|---|
| llama.cpp `/v1/chat/completions` + `--jinja` + OpenAI `tools[]` | GGUF jinja template renders the `<start_function_declaration>` block | **Chosen** — production-shape; declarations confirmed injected (model used my exact `category` enum + `path` keys) |
| Raw `/completion` with hand-rendered special-token prompt | manual | fallback, not needed |

**Chosen approach:** serve Q8_0 with `--jinja`, send Aura's real ToolSpecs as the
OpenAI `tools[]` array, parse calls from BOTH `message.tool_calls[]` and (since
llama.cpp does **not** parse FunctionGemma's format) the raw `<start_function_call>`
tokens via regex.

## How to Run

```bash
# CPU server (Q8, ~292 MB pull, --jinja for tool support)
docker run -d --name fc270m-cpu -p 127.0.0.1:8099:8099 -v fc270m-cache:/root/.cache/llama.cpp \
  ghcr.io/ggml-org/llama.cpp:server \
  --hf-repo unsloth/functiongemma-270m-it-GGUF --hf-file functiongemma-270m-it-Q8_0.gguf \
  --jinja --host 0.0.0.0 --port 8099 --ctx-size 8192 -t 4
# GPU server (server-cuda image, -ngl 99, port 8098)
docker run -d --name fc270m-gpu --gpus all -p 127.0.0.1:8098:8098 -v fc270m-cache:/root/.cache/llama.cpp \
  ghcr.io/ggml-org/llama.cpp:server-cuda \
  --hf-repo unsloth/functiongemma-270m-it-GGUF --hf-file functiongemma-270m-it-Q8_0.gguf \
  --jinja --host 0.0.0.0 --port 8098 --ctx-size 8192 -ngl 99

# Harness (env knobs: FC_BASE_URL, FC_TAG, FC_LANG=en, FC_TEMP=1.0, FC_SYS=1)
FC_BASE_URL=http://127.0.0.1:8099 FC_TAG=cpu-q8 go run ./.planning/spikes/071-fc270m-baseline-and-slot
```

Launch docker from **PowerShell**, not Git-Bash (MSYS path mangling — CONVENTIONS).

## What to Expect

15-tool corpus (11 real Aura ToolSpecs + a 4-tool `mail__*` gravity-well cluster),
12 grounded queries. Per query: emitted-call? top-1 tool correct? required arg
present? Plus per-call latency and parse-route (`tool_calls` vs `raw`).

## Observability

Forensic log: ISO-stamped `[CASE]` rows (verdict OK/TOOL-ONLY/WRONG-TOOL/NO-CALL +
the emitted call or the refusal text) → `[SCORE]` / `[PARSE-VIA]` / `[LATENCY]` /
`[SUMMARY]`.

## Investigation Trail

1. **First run (CPU, IT, temp 0):** emitted-call **2/12**, top1 **1/12**. The
   model **refuses** most turns ("I am sorry, but I cannot assist… My current
   capabilities are limited to…") instead of calling. All calls came via the
   **raw** parser (`tool_calls=0 raw=2`) — llama.cpp's `--jinja` does NOT parse
   FunctionGemma's `<start_function_call>` into OpenAI `tool_calls`.
2. **Is the refusal a config bug?** Checked: the model emitted `web_search` with
   `category:<escape>news<escape>` and `fs_read` with `path:…` — **my exact schema
   enum + key names**. So declarations ARE injected; the refusal is the model, not
   a missing-tools artifact.
3. **temp=1.0 (Google rec sampling):** unchanged — 2/12. Not a greedy-collapse
   artifact.
4. **English eval set:** 3/12 emitted (marginally better), top1 still 1/12. The
   one repeatable success (`news → web_search`) writes the query in English even
   for the IT prompt. English-centric, but English does **not** rescue it.
5. **Correct developer preamble (`FC_SYS=1`):** byte-identical to default — the
   GGUF template already injects the function-calling preamble when `tools[]` is
   present. Refusal is **intrinsic to the base model**.
6. **GPU footprint + latency:** Q8 full-offload (`-ngl 99`, ctx 8192) = **455 MiB
   VRAM** idle. Latency drops ~2.5× vs CPU.

## Results

**PARTIAL ⚠ — gate PASSED (proceed to finetune), but the slot is narrow.** Base
FunctionGemma-270m is **unusable on Aura's tools out-of-box** — which *motivates*
the finetune rather than killing it: the machinery is right (reads real JSON
schemas, emits the correct `<escape>` format, picks plausible tools when it fires),
only the alignment to actually-fire-on-these-tools is missing — the exact thing a
LoRA targets.

### Accuracy (base, zero-shot, deterministic at temp 0)

| Variant | emitted-call | top1-tool | arg-correct |
|---|---|---|---|
| CPU IT temp0 | 2/12 | 1/12 | 1/12 |
| CPU IT temp1.0 | 2/12 | 1/12 | 0/12 |
| CPU IT + correct preamble | 2/12 | 1/12 | 1/12 |
| CPU/GPU **EN** temp0 | 3/12 | 1/12 | 1/12 |

~**80% refusal**, top1 ≈ **8%**. IT ≈ EN. The only stable win is `news → web_search`.
Failure modes: refuse-instead-of-call (dominant); wrong tool (`fs_glob → fs_read`);
**hallucinated** tool name (`search(query:TODO)` — not in corpus).

### Latency (per call; refusals are the fast end, full calls the slow end)

| Host | p50 | p95 | max (full call) |
|---|---|---|---|
| **GPU** Q8 `-ngl 99` | ~300 ms | ~1.1 s | ~1.6 s |
| **CPU** Q8 `-t 4` | ~750 ms | ~3.5 s | ~3.8 s |

A real tool-call generation costs **~1.6 s GPU / ~3.5 s CPU** (decode-bound on the
verbose `<escape>` arg format). CPU is at/over the memory `minillm_cpu_not_viable`
edge; GPU is usable.

### VRAM coexistence (the real production constraint)

- FunctionGemma Q8 full-offload = **455 MiB** (ctx 8192; ~350 MiB at ctx 2048).
- Primary Gemma-4 E2B sidecar = **~3705 MiB** peak (spike 048).
- 3705 + 455 = **4160 MiB > 4096** → permanent GPU coexistence is **right at/over
  the ceiling**, with zero desktop headroom. Practical options: FunctionGemma runs
  **CPU** (slow), **time-shares** the card, or replaces the primary on the offline
  path.

### Integration finding

Aura needs a **custom FunctionGemma call parser** — llama.cpp `--jinja` returns the
`<start_function_call>…` block in `message.content`, NOT in `tool_calls[]`. The
~5-line regex (`rawCallRe` here) is the parser; the `<escape>`-delimited args need
a small decoder. This is real glue Slice 13 must own.

### Slot analysis (vs the shipped embedding ranker)

| Capability | Embedding ranker (shipped, 054-058) | FunctionGemma-270m |
|---|---|---|
| Tool **selection** ("which tool?") | ~85 µs, free, top1 ~8/15 | slower, top1 1/12 base → **loses** |
| **Argument generation** ("with what args?") | ✗ cannot | ✓ (its reason to exist) |
| Offline / no-cloud | ✓ | ✓ |

**The only defensible slot is offline call+arg generation (Slice 13 fallback).** It
must NOT be put on the hot path for selection — embeddings already win there.

## Signal for the Build

- **Proceed to 072-074, eyes open.** Base is unusable (~8% top1) → a finetune is
  *necessary*, not optional, for any viability. The format/latency/footprint are
  workable; the machinery is sound.
- **Target the refusal prior + Italian + Aura's exact tool names** — that is what
  the dataset (073) and finetune (074) must move.
- **Latency budget = GPU** (~1.6 s/call). CPU (~3.5 s) is fallback-only.
- **VRAM forces a choice**: FunctionGemma can't permanently co-reside with the
  primary 4 GB model — slate it CPU or time-shared, or as the offline-mode primary.
- **Ship a FunctionGemma call parser** regardless of finetune outcome — the wire
  format is non-OpenAI.
- **Keep it off the selection hot path.** Its value is generation, narrow to the
  offline fallback. If 074 can't beat "embedding-selects + cloud-fills-args" on a
  real metric, the slot may not justify the build.
