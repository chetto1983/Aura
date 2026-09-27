# Video Studio audio — spike findings

Spec: `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md`. Probes run in headless
Chromium (Playwright 1.62.1, chromium-headless-shell 1234, WSL) against the probe's own Vite server.

## Fixtures

The VM's TTS (`/api/voice/capabilities` → `{"tts":true,"stt":true}`) answered each phrase as a
24 kHz mono MP3 (48–50 kB). `verify-fixtures.mjs` on the committed bytes:

```
music RMS -18.24 dBFS (want -18.24)
speech.wav: speech windows -18.02, -18.18, -18.35 dBFS; gaps -200.00, -200.00, -200.00 dBFS
speech-noisy.wav: speech windows -17.63, -17.74, -17.92 dBFS; gaps -27.63, -27.95, -29.00 dBFS
```

Ground truth: `{"sampleRate":16000,"snrDb":10,"speechRmsDb":-18.18,"windows":[[1,3.662],[5.162,7.857],[9.357,11.905]]}`.
