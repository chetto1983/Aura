---
spike: 020
name: vllm-sidecar-4gb-fit
type: standard
validates: "Given the vLLM sidecar (Docker nvidia runtime, RTX A2000 Laptop 4096MiB), when it serves a quantized multimodal model ≤4GB, then boot OK + /v1 endpoints answer + VRAM steady ≤4096MiB without OOM"
verdict: INVALIDATED
related: [024-openrouter-minimax-m3-vision, 025-paddleocr-vl-local, 026-glm-ocr-local, 027-stt-half]
tags: [multimodal, vllm, gpu, phase-13]
---

# Spike 020: vLLM Sidecar Fit on 4GB GPU

## What This Validates

Given the vLLM sidecar (Docker Desktop nvidia runtime, RTX A2000 Laptop 4096MiB, driver 573.91),
when it serves a quantized multimodal model ≤4GB, then the server boots, `/v1/chat/completions`
(vision) and `/v1/audio/transcriptions` (STT) answer correctly, and VRAM steady stays ≤4096MiB
without OOM. **Kill-risk for the D-13 premise "serviti con vLLM"**: if nothing useful fits,
the 9c engine reverts to llama.cpp by default and D-13 gets amended.

Three phases:
- **A (vision alone)**: Qwen3-VL-2B-Instruct-FP8, full GPU budget (0.85)
- **B (STT alone)**: whisper-large-v3-turbo, full GPU budget — proves the exact
  `/v1/audio/transcriptions` contract PRD voice.go assumes
- **C (dual resident)**: both at 0.42 budget simultaneously — THE architectural question:
  vLLM is one-model-per-instance, so 9c "STT+vision via vLLM" requires both resident at once

## Research

Checked 2026-06-07 (web — knowledge cutoff predates the 2026 model wave):

| Approach | Tool | Pros | Cons | Status |
|----------|------|------|------|--------|
| vLLM + Gemma 4 E2B/E4B | [official recipe](https://docs.vllm.ai/projects/recipes/en/latest/Google/Gemma4.html) | PRD model parity, audio+vision unified | **official requirement 24GB+ VRAM**; E4B raw 7.5B params; known hybrid-attention perf issue (~9 tok/s on a 4090) | DOA on 4GB |
| vLLM + Qwen3-VL-2B FP8 | `Qwen/Qwen3-VL-2B-Instruct-FP8` (verified exists) | ~2.2GB W8A16 weights via Marlin fallback on Ampere; vLLM ≥0.11 native support | vision-only; no official AWQ for 2B (verified 401) | **probe A** |
| vLLM + whisper-large-v3-turbo | `openai/whisper-large-v3-turbo` (verified exists) | 809M params ~1.6GB fp16; exposes `/v1/audio/transcriptions` = the PRD voice.go contract verbatim; `RedHatAI/whisper-large-v3-turbo-FP8-dynamic` as smaller fallback | STT-only | **probe B** |
| llama.cpp + Gemma 4 E4B Q4 | PRD baseline | GGUF has BOTH vision+audio encoders (`clip_model_loader: has audio encoder`, [issue #21325](https://github.com/ggml-org/llama.cpp/issues/21325)) | server-level audio input unverified (#21325 is webui-level; no maintainer response) | baseline, measured in 022/023 |

Key facts driving the design:
- **vLLM = one model per instance.** No omni model fits 4GB (Gemma 4 official: 24GB+;
  Qwen3-Omni 30B-A3B: way out). Simultaneous STT+vision via vLLM ⇒ two instances with split
  `gpu-memory-utilization` (~0.42 each ≈ 1.7GB budgets) — Phase C measures exactly this.
- **A2000 = Ampere sm_86**: no native FP8 — vLLM auto-falls back to Marlin W8A16 for FP8
  checkpoints (weight-only, ~1 byte/param).
- vLLM audio models with the transcriptions endpoint: Whisper, Voxtral
  ([docs](https://docs.vllm.ai/en/latest/contributing/model/transcription/)); Gemma-style
  audio goes through chat completions `audio_url` instead — if 9c serves Gemma-family via
  vLLM the voice.go endpoint design changes.
- Voxtral Mini (3B/4B, [Transcribe V2 Feb 2026](https://developers.redhat.com/articles/2026/02/06/run-voxtral-mini-4b-realtime-vllm-red-hat-ai))
  is bf16 ~9.5GB — out for 4GB unless a quant ships; candidate for 021 survey notes only.
- `--enforce-eager` trades throughput for the CUDA-graph memory we don't have.
- Latency measured host→127.0.0.1 is **production-shaped**: aura.exe also reaches sidecars
  through the Docker Desktop port forward (memory `hyperv-port-forwarding-lie` applies to
  absolute numbers vs in-container, but the host path IS the production path).

## How to Run

```bash
cd /d/Aura
# Phase A — vision fit (first boot pulls vllm/vllm-openai ~10-20GB + model ~2.2GB)
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 up -d vllm-vision
go run ./.planning/spikes/020-vllm-sidecar-4gb-fit -mode=vision
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 stop vllm-vision

# Phase B — STT fit
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 up -d vllm-stt
go run ./.planning/spikes/020-vllm-sidecar-4gb-fit -mode=stt
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 stop vllm-stt

# Phase C — dual residency
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 up -d vllm-vision-half vllm-stt-half
go run ./.planning/spikes/020-vllm-sidecar-4gb-fit -mode=dual
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 down
```

## What to Expect

- Phase A: server ready in minutes (after one-time pulls), reply names red+green+blue,
  `[VRAM] max≤4096MiB`, `[SUMMARY] FIT PROVEN`.
- Phase B: `/v1/audio/transcriptions` answers 200 with JSON `{text}` on a synthetic sine WAV
  (content not asserted — WER is spike 022's job).
- Phase C: either both halves serve (dual-resident architecture viable) or one OOMs at boot —
  both outcomes are findings; OOM ⇒ 9c needs swap-on-demand or GPU-vision+CPU-STT hybrid.

## Observability

Forensic log on stdout: ISO-timestamped `[CATEGORY]` lines (`READY`/`VISION`/`STT`/`VRAM`/
`CRASH`/`SUMMARY`), exit 0 = fit proven. VRAM sampled via `nvidia-smi` every 2s for the whole
run; on readiness timeout the harness dumps the container log tail.

## Investigation Trail

- 2026-06-07: probes pre-flight — host GPU = RTX A2000 Laptop 4096MiB (driver 573.91);
  WSL sees the GPU; `docker info` lists the `nvidia` runtime; Ollama 0.30.6 host with zero
  models; whisper.cpp NOT built on this machine (stale memory); disk C: 89GB / D: 259GB free.
- 2026-06-07: HF repo existence verified via API: `Qwen/Qwen3-VL-2B-Instruct-FP8` 200,
  `openai/whisper-large-v3-turbo` 200, all three AWQ-2B candidates 401 (don't exist).
- 2026-06-07: harness + compose written; Phase A launching.
- 2026-06-07: **FINDING — `vllm/vllm-openai:latest` is CUDA 13.0-based and the host driver
  (573.91, Windows) tops out at CUDA 12.x**: `nvidia-container-cli: requirement error:
  unsatisfied condition: cuda>=13.0`. Docker Hub publishes parallel `-cu129` variants —
  repinned to `v0.22.1-cu129` (current stable, 2026-06-05, 11GB). Production note for the
  Phase-13 amendment: the vLLM sidecar MUST pin a `cuXY` tag matching the host driver
  ceiling, never `latest`.
- 2026-06-07: compose YAML gotcha — `--limit-mm-per-prompt={"image": 1}` parses as a YAML
  mapping inside a command list; must be quoted as a string.
- 2026-06-07: ffmpeg absent on host AND WSL — relevant for 022/9c voice.go: Telegram
  delivers OGG/Opus; vLLM transcriptions claims OGG intake (libsndfile), llama.cpp mtmd
  (miniaudio) does NOT decode Opus → a conversion step may be load-bearing for the
  llama.cpp baseline path.

## Results

**INVALIDATED for the 4GB constraint** (operator killed the path mid-run 2026-06-07:
"lascia stare non può funzionare"). vLLM cannot host a useful multimodal model on this
card. The wall, measured precisely:

1. **KV cache starvation.** Qwen3-VL-2B-Instruct-FP8 weights = 2.95 GiB (FP8 has no native
   Ampere support → Marlin W8A16 weight-only, ~1 byte/param). Plus ~0.8 GiB CUDA context +
   torch allocator + activation reserved before vLLM even profiles. On a 4096 MiB card that
   leaves **0.09 GiB for KV cache while 4096-token context needs 0.44 GiB** →
   `ValueError: Free memory ... less than desired`. Dropping to `max-model-len=2048` +
   `gpu-memory-utilization=0.92` is the only config that even attempts to fit, and it leaves
   zero headroom for the vision encoder cache.
2. **WSL RAM starvation.** The WSL2 VM is capped at **7 GiB total**; with the Aura stack
   running (postgres/neo4j/searxng/sandbox/embed) only ~3.3 GiB is free, and the 3.23 GiB
   FP8 checkpoint loads off a **9P filesystem with no mmap prefetch** → 100% CPU for minutes,
   swap thrash, ~17 min just to reach the (failing) KV-alloc step.
3. **Dual-residency (Phase C) is structurally impossible.** vLLM is one-model-per-instance;
   STT+vision simultaneously needs two instances each wanting ~3 GiB on a 4 GiB card. Never
   attempted — phase 1 already proves one model barely fits.

**Infra findings worth keeping for any future vLLM-on-this-host attempt:**
- `vllm/vllm-openai:latest` is CUDA-13 based; host driver 573.91 caps at CUDA 12.x →
  `nvidia-container-cli: cuda>=13.0` refused. Even `:v0.22.1-cu129` needs `cuda>=12.9` vs the
  12.8 driver → bypass with `NVIDIA_DISABLE_REQUIRE=1`.
- **WSL libcuda shadow**: the image's `/usr/local/cuda-12.9/compat/libcuda.so*` shadows the
  WSL-injected driver lib → `torch.cuda.is_available()==False` ("No CUDA GPUs are available")
  while `nvidia-smi`/NVML works. Fix: `rm` the compat stub + `ldconfig` in the entrypoint
  before `exec vllm serve`. (Carried into the spike 025 llama.cpp compose.)
- compose YAML: `--limit-mm-per-prompt={"image": 1}` must be a quoted string, else parsed as
  a mapping.

**Verdict consequence**: the D-13 premise "9c serviti con vLLM" is dead on this hardware.
Two replacement directions emerged from the operator, both spiked: cloud vision via
OpenRouter (024) and — the winner — **dedicated small OCR-VL GGUF models on llama.cpp**
(025 PaddleOCR-VL, 026 GLM-OCR), which run the vision/OCR half on **CPU with the GPU
entirely free**. STT half tracked separately (027).
