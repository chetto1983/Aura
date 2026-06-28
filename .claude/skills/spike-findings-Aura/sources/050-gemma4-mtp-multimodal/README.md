---
spike: 050
name: gemma4-mtp-multimodal
type: standard
validates: "Given the spike-048 GPU lane (gemma-4 E2B QAT Q4 + MTP draft) with the BF16 mmproj loaded, when an Italian OCR image, a known Italian audio clip, and a multi-frame turn are sent over /v1/chat/completions, then vision + audio + video(frame-sequence) all work in ONE 4 GB GPU sidecar and VRAM stays <= 4096 MiB"
verdict: VALIDATED
related: [048-gemma4-mtp-gpu-fit, 049-mtp-speedup-headtohead, 025-paddleocr-vl-local, 026-glm-ocr-local, 027-stt-half, 028-kokoro-tts]
tags: [gemma4, mtp, multimodal, vision, audio, video, mmproj, llama-cpp, gpu, slice-13]
---

# Spike 050: Gemma 4 MTP Multimodality (vision + audio + video on one 4 GB GPU)

## What This Validates

The frontier integration question after 048/049: the validated GPU text lane
also loads Gemma 4's **single vision+audio projector** (`mmproj`, 1411 tensors,
both encoders) — so can ONE 4 GB GPU sidecar do text + image + audio + video
with MTP still active? If yes, it collapses the **three CPU sidecars** the
9c design needs (spikes 025/026 OCR-VL, 027 STT) into one.

## Research

- **Gemma 4 audio merged into llama.cpp mtmd** via a USM-style Conformer
  encoder ([PR #21421](https://github.com/ggml-org/llama.cpp/pull/21421));
  audio + vision live in the **same** `mmproj` GGUF; `--mmproj` required for
  either. **BF16 mmproj recommended** — F16/Q8_0 cause repetitions
  ([HF discussion](https://huggingface.co/unsloth/gemma-4-E2B-it-GGUF/discussions/1)).
  Audio capped at **30 s**. `llama-server` exposes both over OpenAI-compat
  `/v1/chat/completions` (`image_url` + `input_audio`)
  ([multimodal.md](https://github.com/ggml-org/llama.cpp/blob/master/docs/multimodal.md)).
- **No native video decoder** in llama.cpp mtmd — video = client samples
  frames and sends them as multiple images in one turn.
- mmproj BF16 for E2B = 986,833,728 B (~941 MiB). Kept on **CPU**
  (`--no-mmproj-offload`) so GPU VRAM is unchanged from the 048 fit: weights
  2.5 GiB on GPU, projector encode on CPU.

## How to Run

```powershell
# assets: image = spike-025 OCR page (ground truth on disk); audio via Kokoro
docker run -d --name spike050-tts -p 8096:8880 ghcr.io/remsky/kokoro-fastapi-cpu:latest
# POST /v1/audio/speech {voice:if_sara, response_format:wav} → probe-it.wav
#   (rewrite the streaming WAV header to a canonical one — Kokoro emits a
#    placeholder nframes=2^31-1 that confuses the audio decoder)

docker run -d --name spike050 --gpus all -p 8095:8080 `
  -v D:\tmp\spike-048-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  --mmproj /models/mmproj-BF16.gguf --no-mmproj-offload `
  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
  --spec-type draft-mtp --spec-draft-n-max 2 `
  -ngl 99 --spec-draft-ngl 99 -c 8192 --temp 0 --host 0.0.0.0 --port 8080

go run ./.planning/spikes/050-gemma4-mtp-multimodal
docker rm -f spike050 spike050-tts
```

## What to Expect

- VRAM peak ≤ 4096 MiB (projector on CPU keeps GPU footprint = 048's).
- Image: ≥2/6 ground-truth OCR terms recalled (Analizzatore / Modbus / RS 485 / …).
- Audio: ≥2/5 known words from the synthesized sentence.
- Video: multi-image turn accepted, model frames the two as sequential.

## Observability

Forensic stdout (`[SETUP]/[IMAGE]/[AUDIO]/[VIDEO]/[VRAM]/[NOTE]/[SUMMARY]`),
2 s VRAM sampler, draft `draft_n`/`draft_n_accepted` per call. Exit 0 = all
modalities passed under the ceiling.

## Investigation Trail

- **mmproj download truncated** at 767 MiB (curl exit 255); `curl -C -` resume
  to the exact 986,833,728 B fixed it.
- **Kokoro WAV had a streaming placeholder header** (`nframes = 2^31-1`,
  dur reported 89478 s). Rewrote a canonical 44-byte RIFF/WAVE header over the
  PCM (real dur 7.0 s, 24 kHz mono) — Gemma's audio path needs a sane header.
  (MSYS gotcha: hardcoded `/d/tmp/...` paths fail under the Windows store
  `python3` — only argv paths get MSYS-converted; use `D:/...` in source.)
- **First run: IMAGE empty, AUDIO perfect, VIDEO ok.** Image hit
  `finish=length`, `predicted_n=1024`, **7029 reasoning chars, empty content**
  — NOT a vision failure: the **spike-048 thinking-model trap**. Gemma 4 E2B
  runs away in `reasoning_content` on dense OCR and never emits an answer.
  Raising max_tokens to 3072 just produced *more* thinking (still empty).
- **Fix 1 — `chat_template_kwargs:{enable_thinking:false}`**: image then
  recalled ground truth (Analizzatore di rete, Modbus RTU RS 485, TCP/IP,
  product code) — but fell into a **greedy repetition loop** ("Modbus RTU
  RS 485" ×90) at temp 0.
- **Fix 2 — `repeat_penalty:1.3`, `repeat_last_n:128`**: clean structured OCR,
  `finish=stop`, 81 tok/s. Both knobs are mandatory for a usable vision lane.
- Video: the two-frame turn is understood temporally ("Il primo fotogramma…
  il secondo…") — the frame-sequence mechanism is solid even though E2B's
  description of these specific images is impressionistic ("griglia a mosaico").

## Results

**VALIDATED — 3/3 modalities in one 4 GB GPU sidecar, MTP active, VRAM peak
3392 / 4096 MiB.**

| Modality | Verdict | Evidence | Speed |
|---|---|---|---|
| Audio (STT) | **PASS, excellent** | verbatim IT incl. proper nouns "Torino, Cuneo, Novara"; 5/5 known words | 142 tok/s, draft 19/22 |
| Vision (OCR) | **PASS, modest** | 3/6 ground-truth terms (Analizzatore, Modbus, RS 485); some garble | 61 tok/s, 10.6 s incl. CPU encode |
| Video (frame-seq) | **PASS, mechanism** | multi-image turn, temporal framing understood | 10.9 s / 2 frames |

Key findings:

1. **One GPU sidecar does text + vision + audio + (frame-sequence) video** with
   MTP draft active — VRAM peak 3392 MiB, projector encode on CPU
   (`--no-mmproj-offload`), no OOM. The three-CPU-sidecar 9c shape is
   collapsible to one GPU model, IF the quality trade is acceptable.
2. **Audio is the standout**: near-perfect Italian transcription, faster than
   the dedicated faster-whisper sidecar path and bidirectional with Kokoro
   already validated (spike 028). For voice, E2B-on-GPU is genuinely competitive.
3. **Vision is functional but below the dedicated OCR sidecars** (spikes 025
   PaddleOCR-VL / 026 GLM-OCR scored 7/7; E2B got the headline terms but
   garbled detail and dimensions). For high-fidelity OCR, keep GLM-OCR; for
   "glance at an image" E2B suffices.
4. **Two serving knobs are non-negotiable for the vision lane**:
   `enable_thinking:false` (else runaway reasoning → empty content) and
   `repeat_penalty≈1.3` (else greedy repetition on dense text). Audio needs
   neither but tolerates both.
5. **Image prompt-processing is the latency cost** (~6 s for 297 tokens on the
   CPU projector); generation itself stays fast. Offloading the projector to
   GPU would speed encode but breaks the 048 VRAM fit — CPU encode is the
   right trade on 4 GB.
6. **No native video** — the path is client-side frame sampling; mechanism
   proven, but real video ingest needs a frame extractor (ffmpeg) upstream.

Signal for Slice 13: a single `gemma-4-E2B + mmproj-BF16 + mtp` GPU sidecar is
a viable **unified local fallback for text + voice**, and a *low-fidelity*
vision option. It does NOT retire the dedicated GLM-OCR sidecar for
document-grade OCR. Audio quality alone makes the unified lane worth wiring.
