---
spike: 026
name: glm-ocr-local
type: comparison
validates: "Given GLM-OCR (Q8 GGUF) on llama.cpp, when sent an Italian-text image, then it transcribes accurately with table structure — head-to-head vs PaddleOCR-VL for the 9c vision half"
verdict: VALIDATED
related: [025-paddleocr-vl-local, 024-openrouter-minimax-m3-vision, 020-vllm-sidecar-4gb-fit]
tags: [multimodal, vision, ocr, llama-cpp, gguf, local, phase-13]
---

# Spike 026: GLM-OCR Local (llama.cpp)

## What This Validates

Operator pivot 2026-06-07 ("poi prova https://huggingface.co/ggml-org/GLM-OCR-GGUF"). Given
GLM-OCR (Zhipu OCR VLM) served by llama.cpp, when sent an Italian-text image, then it
transcribes accurately. Head-to-head with spike 025 (PaddleOCR-VL) — same Italian fixtures,
same engine, same port — to pick the 9c local vision/OCR engine.

## Research

- GLM-OCR from **`ggml-org`** itself (the llama.cpp org) → first-class llama.cpp support guaranteed.
- GGUF: `GLM-OCR-Q8_0.gguf` 950 MB + `mmproj-GLM-OCR-Q8_0.gguf` 484 MB = **~1.4 GB** (smaller
  than PaddleOCR-VL). f16 variant (1.8 GB) also available. 23k downloads.
- Same engine/run shape as 025: `llama-server -m model --mmproj mmproj --temp 0`.

## How to Run

```bash
# GGUFs in D:\tmp\spike-026; launch via PowerShell (MSYS path caveat — see 025)
docker run -d --name spike026-glm-ocr -p 18090:8080 -v D:\tmp\spike-026:/models:ro \
  ghcr.io/ggml-org/llama.cpp:server \
  -m /models/GLM-OCR-Q8_0.gguf --mmproj /models/mmproj-GLM-OCR-Q8_0.gguf \
  --host 0.0.0.0 --port 8080 --temp 0 -c 8192
go run -tags spike_multimodal ./.planning/spikes/025-paddleocr-vl-local   # same probe, port 18090
```

## Investigation Trail

- 2026-06-07: GGUFs downloaded (~1.4 GB). Ran on CPU `:server` image, same port as 025 (Paddle
  container stopped first) so the probe harness compares apples-to-apples.

## Results (CPU — `ghcr.io/ggml-org/llama.cpp:server`)

**VALIDATED.** Italian OCR recall **7/7** tokens.

- Reply: a well-formed **HTML table** —
  `<table border="1"><tr><td>Città</td>…<td>Cuneo</td><td>Piemonte</td><td>56000</td>…
  <td>Mondovì</td>…</table>` — all content correct, **`Mondovì` accent preserved**, and the
  **table structure reconstructed** (rows/cells), not just flat text.
- **cold = 8.7s**, **warm p50 = 2.95s / p95 = 3.32s** (CPU) — ~3× slower than PaddleOCR,
  consistent with the richer structured output + Q8 weights.
- **VRAM peak = 0 MiB** (CPU-only).

GPU latency rerun: PENDING (server-cuda image).

## Head-to-Head Verdict (CPU)

| | PaddleOCR-VL-1.5 (025) | GLM-OCR (026) |
|---|---|---|
| Italian OCR recall | 7/7 | 7/7 |
| Accent `ì` (Mondovì) | dropped → `Mondovi` | **preserved** |
| Output shape | plain text | **HTML table structure** |
| warm p50 / p95 (CPU) | **1.06s / 1.16s** | 2.95s / 3.32s |
| Footprint | ~1.8 GB | ~1.4 GB |

Both nail the content. **GLM-OCR wins on fidelity** (accents + table/document structure —
directly relevant to 9c document ingestion and the Slice-11 markitdown-replacement angle);
**PaddleOCR-VL wins on speed** (3× faster, better for quick photo-to-text turns). Final 9c
pick is a quality-vs-latency call for the planner — both are GPU-free, private, and free,
which is the decisive win over both cloud minimax-m3 (024) and local 4GB vLLM (020,
invalidated). Confirm with the GPU rerun + a real-document fixture before locking.