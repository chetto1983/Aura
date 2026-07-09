# Phase 37C: Web Voice Lane - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-09
**Phase:** 37C-Web Voice Lane
**Areas discussed:** Auto-speak model, Dictation transport, TTS delivery & format, Voice availability signal (+ user-requested industrial research)

---

## Research pass (user-requested: "search online and on curated repos for industrial 2026 solutions")

Before the decision questions, a research pass established the industrial baseline:
- **assistant-ui** (`@assistant-ui/react@0.14.22`, the exact lib Aura runs) ships first-class `adapters.speech` (`SpeechSynthesisAdapter`) + `adapters.dictation` (`DictationAdapter`) seams, `ActionBarPrimitive.Speak`/`StopSpeaking`, and `WebSpeech*` fallbacks. The ElevenLabs TTS + Scribe-STT example adapters are direct templates. **Consequence:** 37C wires custom server-backed adapters, not bespoke audio/recorder code; the editable-transcript insertion (SC#2) is native.
- **2026 STT/TTS consensus** (OfflineTTS, AssemblyAI): the browser Web Speech API is still draft, inconsistent across browsers, and lower quality — server Whisper is the production choice. Confirms the roadmap's server-STT steer + Telegram parity.
- **Realtime full-duplex voice** (`RealtimeVoiceAdapter`/LiveKit) exists in the lib but is out of scope for speaker+dictation parity → deferred.

---

## Auto-speak model

| Option | Description | Selected |
|--------|-------------|----------|
| Ephemeral header toggle | In-session React state, no DB, resets on reload; `ShouldSpeak(voiceMode \|\| turnWasDictated)` | ✓ |
| Persisted per-conversation | New persistence (a `conversations.voice_mode` column or per-thread localStorage); truest parity | |
| Global setting | One voice-mode for all conversations via the operator Settings surface | |

**User's choice:** Ephemeral header toggle.
**Notes:** No per-conversation preference store exists today (Telegram's `VoiceModePref` is a false stub); the user consistently favored the thinnest durable surface. Persisted per-conv voice-mode → deferred.

## Dictation transport

| Option | Description | Selected |
|--------|-------------|----------|
| Ephemeral POST /api/stt | Synchronous transcribe-and-discard over `STTClient.Transcribe`; no asset, no DB row | ✓ |
| Reuse asset pipeline | Persist audio as an `assets.Asset` + async `AudioProcessor.ProcessAsset` + poll | |

**User's choice:** Ephemeral POST /api/stt.
**Notes:** Smallest surface, most private, lowest latency (no async poll). Wired as a custom `DictationAdapter` (MediaRecorder → POST). Attachment-record path KEPT as the degraded fallback (SC#2/#4).

## TTS delivery & format

| Option | Description | Selected |
|--------|-------------|----------|
| mp3 for web | `/api/tts` → `audio/mpeg`; universal `HTMLAudioElement` decode incl. Safari/iOS; Telegram keeps opus | ✓ |
| opus (Telegram-consistent) | One format end-to-end; but Safari/iOS opus support is spotty → risk of silent no-play | |
| You decide | Planner picks after a codec-support check | |

**User's choice:** mp3 for web.
**Notes:** `TTSClient` already takes a per-call `Format`, so the web arm sets `response_format=mp3` while Telegram stays opus (unregressed). Locked-by-research: custom `SpeechSynthesisAdapter` (fetch → blob → `<audio>`), blob cached per message.

## Voice availability signal (SC#3 degrade)

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated capabilities endpoint | `GET /api/voice/capabilities` → `{tts,stt}`, SELF-scoped, read once on load | ✓ |
| Fold into /api/me | Extend the existing self-scoped payload with `voice:{tts,stt}` | |
| Attempt-and-degrade | Render optimistically; hide on 501/error at first use | |

**User's choice:** Dedicated capabilities endpoint.
**Notes:** Keeps feature-config out of the authz payload; avoids the controls-flash of attempt-and-degrade. `!tts` → hide speaker; `!stt` → mic stays attachment.

## Follow-up — auto-speak echo of dictation

| Option | Description | Selected |
|--------|-------------|----------|
| Echo dictation (parity) | `ShouldSpeak = voiceMode \|\| turnWasDictated`; a dictated turn gets a spoken reply | ✓ |
| Voice-mode only | `ShouldSpeak = voiceMode`; dictation never triggers audio | |

**User's choice:** Echo dictation (parity) — matches Telegram `inboundWasVoice`.

## Follow-up — TTS on very long replies

| Option | Description | Selected |
|--------|-------------|----------|
| Soft char cap + guard | Cap synth text (`AURA_TTS_MAX_CHARS`); speak the prefix + a "too long" hint | ✓ |
| Synthesize whole | No cap; rely on the request timeout | |
| You decide | Planner picks after checking the model's input ceiling | |

**User's choice:** Soft char cap + guard. Bounds OpenRouter per-character cost + latency.

---

## Claude's Discretion

- `AURA_TTS_MAX_CHARS` default; speaker/toggle icon glyphs; the MediaRecorder-mimeType→STT-format map; `/api/tts` stream-vs-buffer; one `SetVoice` setter vs two; the `/api/voice/capabilities` compute location; the `web/src/chat/voice/` module layout.

## Deferred Ideas

- Realtime full-duplex "hands-free voice mode" (`RealtimeVoiceAdapter`/LiveKit) — future phase.
- Persisted per-conversation voice-mode (a `conversations.voice_mode` column / per-thread store).
- Browser Web Speech API adapters as an offline/keyless fallback.
- Server-side TTS caching / prefetch beyond the per-message client blob.
- Streaming TTS playback (chunked `<audio>`) as an alternative to the char cap.
