---
spike: 025
name: paddleocr-vl-local
type: comparison
validates: "Given PaddleOCR-VL-1.5 (0.9B GGUF) on llama.cpp, when sent an Italian-text image, then it transcribes accurately and fits this hardware — local vision/OCR half of 9c after 4GB vLLM failed"
verdict: VALIDATED
related: [020-vllm-sidecar-4gb-fit, 024-openrouter-minimax-m3-vision, 026-glm-ocr-local]
tags: [multimodal, vision, ocr, llama-cpp, gguf, local, phase-13]
---

# Spike 025: PaddleOCR-VL-1.5 Local (llama.cpp)

## What This Validates

Operator pivot 2026-06-07 ("poi prova https://huggingface.co/PaddlePaddle/PaddleOCR-VL-1.5-GGUF;
audio può essere gestito da whisper o altri modelli 2026"). Given PaddleOCR-VL-1.5 — a 0.9B
OCR-specialized VLM in GGUF — served by llama.cpp, when sent an Italian-text image, then it
transcribes accurately and fits this hardware. This is the **local, private, GPU-free**
vision/OCR half of 9c. The voice half is separated to whisper/2026 audio (spike 027).

Comparison pair with spike 026 (GLM-OCR), same Italian fixtures, same engine.

## Research

- PaddleOCR-VL-1.5: 0.9B params, **SOTA 94.5% OmniDocBench v1.5**, document parsing (text,
  formula, table, chart, seal, text-spotting). 650k downloads.
- GGUF: `PaddleOCR-VL-1.5.gguf` 936 MB + `PaddleOCR-VL-1.5-mmproj.gguf` 882 MB = **~1.8 GB**
  total → fits the 4GB GPU with KV headroom, OR runs on CPU in ~1.2 GiB host RAM.
- Engine = **llama.cpp** (the PRD baseline — no vLLM, no CUDA-13 wrangling). Official run:
  `llama-server -m model.gguf --mmproj mmproj.gguf --temp 0`.
- Language: Baidu model, CN/EN-first; Italian (Latin script) verified empirically here.

## How to Run

```bash
# GGUFs: curl from HF into D:\tmp\spike-025 (see harness header)
# CPU (proven path — GPU entirely free):
docker run -d --name spike025-llama-ocr -p 18090:8080 -v D:\tmp\spike-025:/models:ro \
  ghcr.io/ggml-org/llama.cpp:server \
  -m /models/PaddleOCR-VL-1.5.gguf --mmproj /models/PaddleOCR-VL-1.5-mmproj.gguf \
  --host 0.0.0.0 --port 8080 --temp 0 -c 8192
# GPU: use compose.spike025.yaml (server-cuda image + WSL libcuda fix from spike 020)
go run -tags spike_multimodal ./.planning/spikes/025-paddleocr-vl-local
```

(Launch docker via **PowerShell**, not Git-Bash — MSYS rewrites `/models/...` to a Windows
path. `MSYS_NO_PATHCONV=1` in the Bash tool breaks docker resolution itself; PowerShell is clean.)

## Investigation Trail

- 2026-06-07: GGUF + mmproj downloaded (~1.8 GB). CUDA llama.cpp image slow to pull → ran on
  the already-present CPU `:server` image (188 MB) for the quality verdict; GPU latency rerun
  deferred to when `server-cuda` lands.
- 2026-06-07 CPU probe: server `model loaded` in 18s, projected 1242 MiB host memory.

## Results (CPU — `ghcr.io/ggml-org/llama.cpp:server`)

**VALIDATED.** Italian OCR recall **7/7** tokens.

- Reply: `Città Regione Abitanti / Cuneo Piemonte 56000 / Caraglio Piemonte 6800 /
  Mondovi Piemonte 22000` — all cities + populations + region read correctly.
- **cold = 10.3s** (first call, includes mmproj vision warmup), **warm p50 = 1.06s / p95 = 1.16s**.
- **VRAM peak = 0 MiB** (CPU-only; GPU completely free for other Aura sidecars).
- One blemish: `Mondovì` → `Mondovi` (accent dropped). Plain-text output (no table structure).

GPU latency rerun: PENDING (server-cuda image). Quality verdict holds regardless of device.

**vs 026 (GLM-OCR):** PaddleOCR is ~3× faster on CPU (1.06s vs 2.95s) but drops the `ì`
accent and emits plain text; GLM preserves the accent and reconstructs an HTML table. Speed
vs structural fidelity — see 026 + MANIFEST for the head-to-head verdict.