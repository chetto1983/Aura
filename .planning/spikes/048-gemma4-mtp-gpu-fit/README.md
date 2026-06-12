---
spike: 048
name: gemma4-mtp-gpu-fit
type: standard
validates: "Given llama.cpp server-cuda built 2026-06-11 (post-MTP-merge) + gemma-4 E2B QAT UD-Q4_K_XL (2.44 GB) + mtp draft (56.5 MB), when served with full offload on the 4 GB RTX A2000 alongside the desktop, then boot OK, /v1/chat/completions answers, and VRAM stays <= 4096 MiB"
verdict: VALIDATED
related: [020-vllm-sidecar-4gb-fit, 021-survey-2026-shortlist, 049-mtp-speedup-headtohead]
tags: [gemma4, mtp, llama-cpp, gpu, local-llm, slice-13]
---

# Spike 048: Gemma 4 MTP GPU Fit (best model for 4 GB VRAM)

## What This Validates

Given a llama.cpp CUDA build carrying the June-2026 Gemma 4 MTP merges and the
best-fit Gemma 4 variant for 4 GB of VRAM (E2B QAT UD-Q4_K_XL + its multi-token
prediction draft head), when served with full GPU offload on the RTX A2000
Laptop (4096 MiB) while the Windows desktop keeps its ~1.16 GiB residency, then
the server boots, answers an Italian chat completion over the OpenAI-compat
wire, and VRAM never exceeds the card.

Strategic slot: **Slice 13 (local LLM fallback)** — the lane where vLLM died
(spike 020, KV-cache arithmetic wall). llama.cpp + MTP is the candidate
replacement engine for a local GPU text lane.

## Research

- **MTP merge timeline:** Gemma 4 MTP merged into llama.cpp **2026-06-07**
  ([PR #23398](https://github.com/ggml-org/llama.cpp/pull/23398)); compact
  E2B/E4B assistant support **2026-06-08**
  ([PR #24282](https://github.com/ggml-org/llama.cpp/pull/24282)). Builds older
  than that cannot load arch `gemma4-assistant`.
- **Engine image:** `ghcr.io/ggml-org/llama.cpp:server-cuda`, locally pulled,
  **created 2026-06-11T07:16Z** (after both merges), digest
  `sha256:e502860c8aa147e74e7cf42568fa2a8407c578dd291c1b231f698a55dd83fef6`.
  `--help` confirms `--spec-type ... draft-mtp ...`, `--spec-draft-n-max`,
  `--spec-draft-model`, `--spec-draft-ngl`.
- **Model files** (unsloth/gemma-4-E2B-it-qat-GGUF):
  - `gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf` = 2,620,368,960 B (~2.44 GiB)
  - `mtp-gemma-4-E2B-it.gguf` = 59,234,176 B (~56.5 MiB) — Gemma 4 ships the
    MTP head as a separate small draft GGUF in the same repo, wired via
    `--model-draft` (unlike Qwen3.6 where a full separate MTP GGUF exists).
  - vision `mmproj` deliberately NOT mounted — this is a text-lane probe.
- **Size ladder ruled out:** E4B QAT UD-Q4_K_XL = 4,215,693,760 B (~4.21 GiB)
  exceeds the entire card before KV/CUDA context — partial-offload only, out of
  scope for "best model that FITS 4 GB". 12B+ are far out.
- **Fit math (spike-020 discipline — prove it before pulling):** free VRAM at
  probe start ≈ 4096 − 1164 (desktop) ≈ **2.9 GiB**; weights 2.44 + 0.056 GiB
  ≈ 2.5 GiB; llama.cpp CUDA context is far lighter than vLLM's 0.8 GiB and E2B
  KV at `-c 4096` is small (MatFormer per-layer sharing). Expected to fit
  full-offload, tight; fallback knobs if OOM: `-c 2048`, `-ctk q8_0 -ctv q8_0`,
  or `-ngl` partial.
- **Unsloth guidance** ([MTP guide](https://unsloth.ai/docs/models/mtp)):
  `--spec-type draft-mtp --spec-draft-n-max 2` (test 1–6), claimed 1.4–2.2×
  GGUF speedup, ~2 GB headroom advised, Gemma 4 four-bit total footprint 5–21
  GB E2B→31B (RAM+VRAM combined).

## How to Run

```powershell
# 1. serve (PowerShell, never Git-Bash — MSYS mangles /models)
docker run -d --name spike048 --gpus all -p 8095:8080 `
  -v D:\tmp\spike-048-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
  --spec-type draft-mtp --spec-draft-n-max 2 `
  -ngl 99 --spec-draft-ngl 99 -c 4096 --temp 0 --host 0.0.0.0 --port 8080

# 2. probe
go run ./.planning/spikes/048-gemma4-mtp-gpu-fit

# 3. teardown
docker rm -f spike048
```

## What to Expect

- `/health` 200 within the 240 s boot deadline (first load reads 2.5 GiB).
- A non-empty Italian reply from `/v1/chat/completions`.
- `[VRAM]` peak ≤ 4096 MiB across generation.
- `/completion` timings JSON logged — includes draft acceptance stats when
  MTP is active (first speed signal before the 049 head-to-head).

## Observability

Forensic stdout log: ISO-timestamped `[SETUP]/[VRAM]/[PROBE]/[SUMMARY]` lines;
a goroutine samples `nvidia-smi memory.used` every 2 s and logs new peaks.
Exit 0 = VALIDATED, 1 = failure.

## Investigation Trail

- Pre-build: spike 021 had measured gemma4 e2b at 7.2 GB on the **Ollama
  registry** — that was a heavier default quant; unsloth's QAT UD-Q4_K_XL at
  2.44 GiB re-opens the full-offload question that 021 closed.
- Image freshness was the first kill-risk: the MTP merges are 4–5 days old.
  The locally pulled `server-cuda` happens to be built 2026-06-11 — no rebuild
  needed. Flag surface verified via `--help` before any model download.
- First probe run FAILED with an empty `content` — not a serving bug:
  **Gemma 4 is a thinking model**. `reasoning_content` consumed the whole
  `max_tokens: 160` budget before any visible answer (`finish_reason: length`).
  Raised to 1024 and the reply appeared after ~1.6k chars of reasoning. Any
  client of this lane must budget max_tokens for thinking or disable it via
  chat-template kwargs.
- Boot is fast: 2.5 GiB of weights healthy in ~17–25 s from `docker run`.

## Results

**VALIDATED.** Best model for 4 GB VRAM = gemma-4 **E2B QAT UD-Q4_K_XL +
mtp draft, fully GPU-offloaded** (`-ngl 99 --spec-draft-ngl 99`, `-c 4096`),
coexisting with the Windows desktop:

- VRAM peak during generation: **3705 / 4096 MiB** (server share ≈ 2.5 GiB;
  desktop held ~1.2 GiB). No OOM, no partial-offload fallback needed.
- `/v1/chat/completions` answered the Italian probe correctly
  (469 tokens incl. reasoning at **89.6 tok/s**, draft acceptance 265/406 ≈ 65%).
- Native `/completion` timings expose `draft_n` / `draft_n_accepted` —
  the ground-truth counters the 049 head-to-head uses.
- `system_fingerprint: b9592-ac4cddeb0` (image digest pinned in Research).
- E4B Q4 (4.21 GiB) confirmed over-ceiling for full offload — E2B is the
  right "best fit" call, not a compromise.

Spike 020's vLLM wall does NOT apply to llama.cpp: weights + CUDA context +
KV for E2B at 4k context all fit inside the free ~2.9 GiB.
