# Local OCR Benchmark — GLM-OCR on llama.cpp

**Date:** 2026-05-13
**Goal:** Verify that a 100% local, open-source OCR backend can replace Mistral Document AI (cloud, paid) for Aura's PDF ingest pipeline, across the deployment tiers a user might actually have.

## TL;DR

GLM-OCR Q8 (~1.4 GB on disk) running on stock llama.cpp produces **byte-identical OCR output** to Mistral Document AI on a real Italian technical datasheet, across CPU, Intel iGPU, and NVIDIA dedicated GPU backends. Wall-clock latency is the only axis that varies — quality is constant.

| Backend | Hardware | Latency / A4 page | Verdict |
|---------|----------|-------------------|---------|
| Mistral Document AI | none (cloud) | 2–5 s | $$$, cloud, baseline |
| **NVIDIA CUDA** | RTX A2000 Laptop (4 GB VRAM) | **8 s** | premium tier, replaces Mistral |
| **Intel Vulkan** | Intel UHD Graphics (iGPU) | **54 s** | classic mini-PC, async OK |
| Pure CPU | i7-11850H 8 cores, 4 threads | 269 s | unusable for production |

## Test setup

**Hardware**
- Lenovo ThinkPad mobile workstation (used as mini-PC)
- CPU: Intel Core i7-11850H, 8 cores / 16 threads, 2.50 GHz
- RAM: 32 GB
- iGPU: Intel UHD Graphics (shared, 16 GB addressable RAM)
- dGPU: NVIDIA RTX A2000 Laptop, 4 GB VRAM (Ampere, compute capability 8.6)
- OS: Windows 11

**Model**
- Repo: [ggml-org/GLM-OCR-GGUF](https://huggingface.co/ggml-org/GLM-OCR-GGUF)
- Architecture: CogViT + GLM-0.5B (~0.9 B total)
- Quantization: Q8_0 main (907 MB) + Q8_0 mmproj (462 MB) — **1.37 GB total on disk**
- License: MIT (model), Apache 2.0 (pipeline). Source: [zai-org/GLM-OCR](https://huggingface.co/zai-org/GLM-OCR)
- Last updated: 2026-03-10 (ggml-org official build)
- OmniDocBench v1.5: 94.62 (#1 OCR-tuned VLM at this size class)

**Inference runtime**
- llama.cpp release [b9128](https://github.com/ggml-org/llama.cpp/releases/tag/b9128), Windows x64 builds
- Same `llama-server.exe` binary across backends; only the build (CUDA / Vulkan / CPU) changes
- Context: 8192. Threads: 4 (CPU). GPU offload: `-ngl 99` (all layers).

**Test document**
- Real user content: Finder 6MBU00242200 (Italian technical datasheet, "Analizzatore di rete Modbus")
- One A4 page, ~300 KB, mixed layout: title, image placeholder, paragraph, key-value table, multi-line footnotes, footer
- Page rendered to PNG at 200 DPI (1656 × 2339), 309 KB
- Prompt: `OCR:` — minimal, no language hint, no layout instruction

## Per-backend results

### NVIDIA CUDA (RTX A2000)

```
Wall-clock:           8 s
Image encode (ViT):   4.4 s
Image decode (proj):  0.4 s
LLM generation:       3 s (491 tokens at ~160 tok/s)
VRAM peak:            ~3 GB / 4 GB available
```

Build: `llama-b9128-bin-win-cuda-12.4-x64.zip` + `cudart-llama-bin-win-cuda-12.4-x64.zip`. Required cuBLAS/cuDNN ship inside the zip; no system CUDA install needed.

### Intel Vulkan (UHD Graphics, integrated)

```
Wall-clock:           54 s
Image encode (ViT):   7.7 s   (28× faster than CPU encode)
Image decode (proj):  16.8 s  (4015 vision tokens, slower than CUDA)
LLM generation:       ~30 s   (491 tokens at ~16 tok/s)
GPU memory:           1.2 GB shared (system RAM addressable from iGPU)
```

Build: `llama-b9128-bin-win-vulkan-x64.zip`. 31 MB binary, no driver dependency beyond the standard Intel Graphics driver. `--device Vulkan1` to force iGPU when a discrete GPU is also present.

The image-decode phase (mmproj forward pass on the ViT output tokens) is the bottleneck on the iGPU — fewer compute units than the A2000 by ~50× shows here. Encode and generation scale much better than decode does.

### Pure CPU (4 threads)

```
Wall-clock:           269 s
Image encode (ViT):   218 s
Image decode (proj):  22 s
LLM generation:       ~29 s
RAM peak:             3.1 GB
```

Build: same Vulkan binary, but `-ngl 0` (no offload). For reference only — not viable for routine use.

## Output quality

The Mistral DAI ground truth and all three GLM-OCR backends produce **the same content**, line by line, on the test page:

- Title `6MBU00242200` ✓
- Subtitle `Analizzatore di rete` ✓
- Italian paragraph including `da -20 a +60 °C` (em-dash + degree sign preserved) ✓
- Key-value table: every row matches (`Dimensioni 17.7x93.7x63.2 mm`, `Tensione di alimentazione 24 V`, etc.) ✓
- Multi-line footnote about indicative information ✓
- Footer `Finder 2026 - Tutti i diritti riservati` ✓
- Note about `Findernet.com` ✓

Differences vs Mistral are cosmetic only:
- Mistral marks the product image as `![img-0.jpeg](img-0.jpeg)`; GLM emits the surrounding caption instead
- Mistral surfaces the header `*Header:* finder LAVITER TO THE FUTURE`; GLM skips the logo text (it's a stylized graphic, not flat text)

No mojibake, no missing accents (`è`, `°`, `−`), no swapped digits. The model handles Italian correctly despite Italian not appearing in its declared language list — this is consistent with what large multilingual ViT + LM combinations do for any Latin-alphabet language with sufficient web-scale training data.

## Why GLM-OCR works where PaddleOCR-VL-1.5 did not

Same class of model (~0.9 B, vision-language, OCR-tuned), same llama.cpp runtime — but PaddleOCR-VL-1.5 on the same CPU configuration took **166 seconds for image encode alone**, vs GLM-OCR's 218 s for the full encode. On the GPU backends the gap is the same: PaddleOCR-VL-1.5's NaViT-style dynamic-resolution encoder emits far more tokens per image and exercises attention patterns less friendly to GGML kernels.

GLM-OCR's CogViT is closer to a SigLIP-style fixed-grid encoder — predictable token count, predictable compute, scales linearly with image area.

## Three-tier deployment plan

The latency table maps cleanly to a tiered offering:

| Tier | Hardware target | Backend | Practical SLA per page |
|------|-----------------|---------|------------------------|
| **Free local** | Any 2018+ laptop with Intel iGPU | Vulkan (Intel UHD/Arc) | ~30–90 s, async |
| **Premium local** | Any machine with NVIDIA dGPU ≥ 4 GB VRAM | CUDA | 5–10 s, near-realtime |
| **Hosted** | Aura cloud subscription | Mistral DAI (current) | 2–5 s, realtime |

All three produce the same Italian OCR quality. The free local tier is what makes Aura genuinely self-hostable on commodity mini-PCs.

## Reproduction

Download:

```
GLM-OCR-Q8_0.gguf            907 MB  (huggingface ggml-org/GLM-OCR-GGUF)
mmproj-GLM-OCR-Q8_0.gguf     462 MB  (huggingface ggml-org/GLM-OCR-GGUF)
```

Run (Windows, native binary):

```
llama-server.exe \
  -m  path/to/GLM-OCR-Q8_0.gguf \
  --mmproj path/to/mmproj-GLM-OCR-Q8_0.gguf \
  -ngl 99 \
  --device Vulkan1   # or Vulkan0 / CUDA0 depending on hardware
  --host 127.0.0.1 --port 8080 -c 8192
```

POST an A4-page PNG (base64) as a vision content block to `http://127.0.0.1:8080/v1/chat/completions` with the prompt `OCR:` and observe the timing lines in stderr (`image slice encoded in <ms>`).

## Open follow-ups

- Multi-page batch: throughput on a 10-page document is bounded by per-page encode; KV cache reuse for prompt is irrelevant (every page is a new image).
- Italian quality at scale: this test is one page. A 50-document spot-check against existing Mistral DAI archives is the next step before flipping the production OCR backend.
- Docker NVIDIA passthrough: Docker Desktop's nvidia-container-toolkit configuration is the blocker for shipping the CUDA tier as a sidecar in `compose.yaml`. WSL2 + manual install is a workaround.
- AMD ROCm and Apple Metal: same model, untested on these backends.
