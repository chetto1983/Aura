---
spike: 024
name: openrouter-minimax-m3-vision
type: standard
validates: "Given OpenRouter minimax/minimax-m3, when sent an Italian-text image, then it recalls the content (OCR) accurately at low latency/cost — cloud option for the 9c vision half after local 4GB vLLM failed"
verdict: VALIDATED
related: [020-vllm-sidecar-4gb-fit, 025-paddleocr-vl-local, 026-glm-ocr-local]
tags: [multimodal, vision, openrouter, cloud, phase-13]
---

# Spike 024: OpenRouter minimax/minimax-m3 Vision

## What This Validates

Operator pivot 2026-06-07 after spike 020 invalidated local 4GB vLLM ("prova con openrouter
minimax/minimax-m3"). Given `minimax/minimax-m3` via OpenRouter, when sent an Italian-text
image, then it recalls the content accurately — a **cloud option for the vision/OCR half**
of 9c that needs no local GPU.

## Research

OpenRouter model catalog probe (`GET /api/v1/models`, free):

| Model | input modalities | ctx | $/M prompt | $/M completion |
|-------|------------------|-----|-----------|----------------|
| **minimax/minimax-m3** | text, **image, video** | 1M | $0.30 | $1.20 |
| minimax/minimax-01 | text, image | 1M | $0.20 | $1.10 |
| minimax/minimax-m2.x, m1 | text only | — | — | — |

**Key structural finding: minimax-m3 has NO audio input** → it covers the *photo* half of 9c,
not the *voice/STT* half. Models on OpenRouter that DO accept audio (for the STT half,
spike 027): `google/gemini-*-flash*` (image+audio+video, $0.10-1.5/M), `openai/gpt-audio-mini`
($0.60/M), `mistralai/voxtral-small-24b` ($0.10/M), `nvidia/nemotron-3-nano-omni` (free).

## How to Run

```bash
cd /d/Aura
set -a; source <(tr -d '\r' < .env); set +a   # OPENROUTER_API_KEY
go run -tags spike_multimodal ./.planning/spikes/024-openrouter-minimax-m3-vision
```

## What to Expect

Color sanity PASS, Italian OCR table recalled, sub-2s latency, total cost ~$0.001.

## Investigation Trail

- 2026-06-07: catalog confirmed m3 = text+image+video, 1M ctx, $0.30/$1.20 — very cheap.
- 2026-06-07: live probe (PAID, operator-authorized). Color: PASS ("Red, green, blue").
  Italian OCR table: model returned a clean reconstructed markdown table —
  `Cuneo 56.000 / Caraglio 6.800 / Mondovì 22.000`, all correct, **accents preserved**, even
  inferred "tutte e tre nella regione Piemonte". The harness scored it PARTIAL only because
  the assertion looked for raw `56000` while m3 used Italian digit grouping `56.000` — a
  false negative; the OCR is effectively 100%.
- p50 ≈ 1.9s, 312-397 prompt / 53-180 completion tokens, **$0.00104 for 2 probes**.

## Results

**VALIDATED (vision half).** minimax-m3 reads Italian-text images accurately with correct
accents and even regional inference, ~1.9s p50, ~$0.0005/call. Strong, cheap cloud option
for 9c photo description / OCR. **But it cannot do STT** (no audio modality) and it is a
**cloud dependency** — contrary to the PRD's local-sidecar privacy/offline design. Positioned
as the cloud fallback / high-quality option; the local OCR-VL models (025/026) are the
privacy-preserving primary. The STT half is unsolved here and moves to spike 027.

Assertion bug to carry forward: any Italian-number OCR check must normalize `.`/space digit
grouping before substring matching.