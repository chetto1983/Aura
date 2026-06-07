---
spike: 028
name: kokoro-tts
type: standard
validates: "Given Kokoro-82M via Kokoro-FastAPI, when sent Italian text, then it produces natural Italian speech as Telegram-ready OGG/Opus, fast and GPU-free — the TTS (voice-out) half of Aura's voice loop"
verdict: VALIDATED
related: [027-stt-half, 029-voice-cloning]
tags: [multimodal, tts, kokoro, ogg-opus, phase-13]
---

# Spike 028: Kokoro-82M TTS — Aura Speaks Italian

## What This Validates

Operator directive 2026-06-07 ("prova kokoro per il TTS … così facciamo parlare Aura"). Given
Kokoro-82M served by Kokoro-FastAPI, when sent Italian text, then it produces natural Italian
speech as **Telegram-ready OGG/Opus**, fast and GPU-free. This is the **voice-out** half that,
with spike 027 (voice-in/STT), closes Aura's full voice loop on Telegram.

## Research

- **Kokoro-82M** (hexgrad): 82M params, **Apache-2.0**, 24kHz output. v1.0 = 8 languages /
  54 voices. **Italian supported**: `if_sara` (female), `im_nicola` (male) — confirmed live
  (67 voices total on the server; prefixes a/b=en, e=es, f=fr, h=hi, **i=it**, j=ja, p=pt, z=zh).
- **Kokoro-FastAPI** (remsky): Dockerized **OpenAI-compatible `/v1/audio/speech`** — mirrors
  the faster-whisper `/v1/audio/transcriptions` contract from spike 027. CPU + GPU images.
  `response_format` includes **opus** → direct Telegram voice-note format. Needs espeak-ng
  (bundled in the image) for G2P via misaki.

## How to Run

```bash
docker run -d --name spike028-kokoro -p 18093:8880 ghcr.io/remsky/kokoro-fastapi-cpu:latest
# Italian speech, Telegram-ready opus:
curl http://127.0.0.1:18093/v1/audio/speech -H 'Content-Type: application/json' \
  -d '{"model":"kokoro","input":"Ciao, sono Aura.","voice":"if_sara","response_format":"opus"}' \
  -o aura.opus
# deliver as a voice note:
curl "https://api.telegram.org/bot<TOKEN>/sendVoice" -F chat_id=<id> -F voice=@aura.opus
```

## Investigation Trail

- 2026-06-07: Kokoro-FastAPI CPU image pulled; first-run fetches hexgrad/Kokoro-82M + warms
  af_heart. Ready in seconds after pull.
- Generated the Aura intro line ("Ciao, sono Aura. Posso aiutarti con i tuoi documenti…",
  126 chars ≈ 8s speech) in both Italian voices, opus + wav.
- **Latency**: ~2.5-2.8s to synthesize ~8s of audio on **CPU** = **~0.3× realtime** (3× faster
  than real time). opus ≈ 135-156 KB, wav ≈ 354-383 KB. Output verified `Ogg data, Opus audio,
  mono, 24000 Hz`.
- Delivered both voices to the operator's Telegram via `sendVoice` (msg 3439+) — on-device
  listening verdict pending. **Caveat found**: `sendVoice` caption with emoji + parentheses +
  em-dash returned HTTP 400; plain ASCII captions send clean. (Telegram caption entity parsing
  — relevant to the channel's caption escaper, ties to spike 017 MarkdownV2 discipline.)

## Results

**VALIDATED.** Kokoro-82M gives Aura a fast, natural, GPU-free Italian voice:

- **Local, Apache-2.0, OpenAI-compat `/v1/audio/speech`** — same sidecar shape as faster-whisper.
- **Opus output = native Telegram voice notes**, no transcode. ~0.3× realtime on CPU, 0 VRAM.
- Two Italian preset voices (Sara / Nicola). This is **Aura's own voice** (a fixed identity);
  voice *cloning* from an arbitrary sample is a separate capability (spike 029 — Kokoro has
  fixed voicepacks, no zero-shot cloning).

**Voice loop now closed**: 027 faster-whisper (OGG/Opus in → text) + 028 Kokoro (text → OGG/Opus
out), both local CPU OpenAI-compat sidecars. The Telegram channel can both hear and speak.

**OPERATOR VERDICT (on-device, 2026-06-07): `if_sara` (female Italian) is THE Aura voice — "la
voce femminile di kokoro è perfetta".** Locked. This is Aura's identity voice; voice cloning
(spike 029) was descoped on the spot — the preset voice is enough.