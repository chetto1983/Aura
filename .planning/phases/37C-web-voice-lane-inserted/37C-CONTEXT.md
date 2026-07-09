# Phase 37C: Web Voice Lane - Context

**Gathered:** 2026-07-09
**Status:** Ready for planning

<domain>
## Phase Boundary

Voice parity with Telegram in the web cockpit, in two lanes: **(a) voice output** — every assistant message gets a speaker control that synthesizes its text on demand, plus an auto-speak "voice mode"; **(b) voice input** — the Composer Mic becomes **in-place dictation** (an editable transcript in the input box, not a voice-note attachment). Both are **cloud-only via OpenRouter** (`AURA_TTS_MODEL` / `AURA_STT_CLOUD_MODEL`, no local sidecar — RAM constraint), reusing the already-shipped `multimodal.TTSClient` / `STTClient`. Second of the three cockpit-web parity areas from the voice/artifact/skill audit (37A/37B = artifacts, 37D = "/" picker).

**The spine (research-locked):** Aura runs `@assistant-ui/react@0.14.22`, which ships first-class **`adapters.speech` (`SpeechSynthesisAdapter`)** + **`adapters.dictation` (`DictationAdapter`)** seams. Both features are **custom server-backed adapters wired into the existing `useExternalStoreRuntime`** — NOT hand-rolled `<audio>`/MediaRecorder plumbing. The speaker button is `ActionBarPrimitive.Speak`/`StopSpeaking` in the assistant message's existing ActionBar; dictation's editable-transcript insertion into the composer is **native to the lib** (SC#2 comes for free).

**In scope:** three new authenticated identity-scoped endpoints (`POST /api/tts`, `POST /api/stt`, `GET /api/voice/capabilities`); a custom `SpeechSynthesisAdapter` (fetch → blob → `<audio>`, blob cached per message) + the speaker control in the assistant ActionBar; an ephemeral session "voice mode" header toggle with `ShouldSpeak` parity; a custom `DictationAdapter` (MediaRecorder → `/api/stt` → transcript) replacing the mic's attachment behavior **when STT is configured**, with the current attachment-record path KEPT as the degraded fallback; composition-root injection of the TTS/STT clients into the agui `Server`; graceful degrade when the cloud models are unconfigured.

**Out of scope (→ other phases / deferred):** **realtime full-duplex voice** (`RealtimeVoiceAdapter` / LiveKit / ElevenLabs Conversational — a future "hands-free voice mode" phase, NOT speaker+dictation parity); **persisted per-conversation voice-mode** (no per-conv preference store exists; deferred until one lands); **browser Web Speech API adapters** as an offline fallback (deliberately rejected for clean degrade — deferred); the "/" skill picker → **Phase 37D**; model/effort selector → **Phase 37E**; sharing/export → **Phase 37F**.

</domain>

<decisions>
## Implementation Decisions

Success criteria WEBVOICE-01..04 are effectively locked (see canonical refs). These are the HOW decisions the discussion resolved, grounded in the **assistant-ui** reference (the exact lib Aura runs) + the 2026 industrial STT/TTS research the user requested.

### Architecture — assistant-ui adapter seams (the spine)
- **D-01 — Both features are custom assistant-ui adapters wired into the runtime, NOT bespoke audio plumbing.** `@assistant-ui/react@0.14.22` exports `SpeechSynthesisAdapter`, `DictationAdapter`, `ActionBarPrimitive.Speak`/`StopSpeaking`, `useVoiceControls`/`useVoiceState`, and the `WebSpeech*` fallbacks. TTS = a custom `SpeechSynthesisAdapter` on `adapters.speech`; dictation = a custom `DictationAdapter` on `adapters.dictation`, both attached to the existing `useExternalStoreRuntime` (`ExternalStoreChat.tsx:540`). The ElevenLabs TTS + Scribe-STT example adapters (in the assistant-ui clone) are the exact templates. The composer inserting the editable transcript into the input box is **native** to `adapters.dictation` — do NOT re-implement it.

### TTS output / speaker control (WEBVOICE-01)
- **D-02 — New authenticated `POST /api/tts` over `multimodal.TTSClient.Synthesize`.** `RequireAuth` + `principalIdentityID`, registered alongside the other agui routes; responds **`audio/mpeg` (mp3)** for the web (`response_format=mp3`) — chosen over opus for universal `HTMLAudioElement` decoding incl. Safari/iOS. Telegram keeps opus end-to-end (unregressed — `TTSClient` already takes a per-call `Format`).
- **D-03 — Custom `SpeechSynthesisAdapter`: `speak(text)` → `fetch('/api/tts',{credentials:'same-origin'})` → `blob` → `new Audio(URL.createObjectURL(blob))`.** Returns the `{status, cancel, subscribe}` Utterance. **Cache the blob/objectURL per message** so re-clicking Speak never re-bills the API; `revokeObjectURL` on message unmount (the assistant-ui speech-guide + ElevenLabs-adapter pattern).
- **D-04 — Speaker = `ActionBarPrimitive.Speak`/`StopSpeaking` in the EXISTING assistant ActionBar.** Drop it into the assistant `ActionBarPrimitive.Root` next to Copy/Reload (`ExternalStoreChat_messages.tsx:186`), toggled by the `s.message.speech` state (guide pattern: `Speak` when `speech==null`, `StopSpeaking` otherwise). Per-message, hover-revealed like the existing row.
- **D-05 — Soft char cap on `/api/tts` input (`AURA_TTS_MAX_CHARS` knob).** Beyond the cap, synthesize the capped prefix + surface a subtle UI "message too long to read fully" hint. Bounds OpenRouter per-character cost + synth latency on long reports. Planner picks the default number after checking the TTS model's practical ceiling.

### Auto-speak "voice mode" (WEBVOICE-01)
- **D-06 — Ephemeral header toggle, session-scoped React state — NO persistence.** A "voice mode" toggle in the chat header, in-session only (resets on reload). There is **no per-conversation preference store** today (Telegram's `VoiceModePref` is a stub returning `false`), and this phase adds none. Smallest blast radius; matches "cloud-only, no new persistence." (Persisted per-conv voice-mode = deferred.)
- **D-07 — Auto-speak trigger = `ShouldSpeak(voiceMode || turnWasDictated)` — Telegram parity.** Auto-speak a new assistant reply when the toggle is ON **OR** the user dictated that turn (echo-modality parity with Telegram's `inboundWasVoice`). `turnWasDictated` is tracked client-side per submitted turn. Mirror the intent of `telegram.ShouldSpeak` (`internal/channels/telegram/tts.go:24`).

### Dictation input (WEBVOICE-02)
- **D-08 — New authenticated `POST /api/stt` — ephemeral transcribe-and-discard.** Audio blob in → `multimodal.STTClient.Transcribe` → `{text}` out. The audio is **NEVER persisted** — no `assets.Asset`, no Garage object, no DB row, no async poll (smallest surface, most private, lowest latency). `RequireAuth` + identity-scoped. Chosen over reusing the async `AudioProcessor.ProcessAsset` pipeline (which would persist an asset per dictation).
- **D-09 — Custom `DictationAdapter`: MediaRecorder → on-stop POST blob to `/api/stt` → `onSpeechEnd({transcript})`.** assistant-ui natively inserts the editable transcript into the composer input (SC#2's "editable before send" is free). The server maps the MediaRecorder container (`audio/webm`/opus on Chrome/Firefox, `audio/mp4` on Safari) to the STT `format` hint — mirror `assets.audioFormat` (`internal/assets/audio_processor.go:64`, already handles webm + m4a). The ElevenLabs-Scribe adapter is the structural template (session status + `onSpeech`/`onSpeechEnd`), minus the realtime WebSocket.
- **D-10 — Fallback = today's attachment-record behavior, KEPT not deleted (SC#2/#3/#4).** On dictation error OR when STT is unconfigured, the Mic reverts to the current `Composer.tsx` MediaRecorder → `uploads.addFiles([voice-note])` path — which stays as the degraded arm. No regression to the audio-attachment path (SC#4). The Paperclip continues to accept audio files independently.

### Cloud-only + graceful degrade (WEBVOICE-03)
- **D-11 — New authenticated `GET /api/voice/capabilities` → `{tts:bool, stt:bool}`.** SELF-scoped like `/api/me`, read once by the SPA on load; each flag reflects whether `AURA_TTS_MODEL` / `AURA_STT_CLOUD_MODEL` is configured. `!tts` → hide the speaker control; `!stt` → the Mic stays in attachment mode. Chosen over folding into `/api/me` (keeps feature-config out of the authz payload) and over attempt-and-degrade (no controls-flash / janky first-click).
- **D-12 — Cloud-only, no local sidecar; browser Web Speech deliberately NOT wired.** Both endpoints run `multimodal` clients with `CloudModel` set (OpenRouter). The lib's `WebSpeechSynthesisAdapter`/`WebSpeechDictationAdapter` are NOT used — 2026 research (OfflineTTS / AssemblyAI) confirms the Web Speech API is still draft, browser-inconsistent, and lower quality; clean degrade beats an inconsistent free fallback (browser Web Speech = deferred idea).

### Composition-root wiring
- **D-13 — Inject the TTS/STT clients into the agui `Server` via a setter, mirroring `SetSettingsStore`.** Add a `SetVoice`(-style) setter (`serve_settings.go:10` is the precedent); build `multimodal.TTSConfig`/`STTConfig` from `config.Config` exactly as `serve_channels.go:111` assembles the Telegram `MultimodalConfig` (`STTCloudModel`/`TTSModel` + the single OpenRouter key). Until set, the voice routes degrade (503 / capabilities=false). Config fields already exist (`config.go:194-201`) — no new env plumbing beyond `AURA_TTS_MAX_CHARS`.

### PRD-first (mandatory before code)
- **D-14 — A PRD-amendment commit is required before implementation** (ROADMAP marks 37C "PRD-first: richiede PRD-amendment"). The amendment must cover: the requirement group **WEBVOICE-01..04**, the **web voice surface** (undocumented in the PRD) — `POST /api/tts`, `POST /api/stt`, `GET /api/voice/capabilities`, the two assistant-ui adapters (`adapters.speech`/`adapters.dictation`), the ephemeral header toggle, **mp3-for-web** vs opus-for-Telegram, and the new **`AURA_TTS_MAX_CHARS`** knob. Planner writes the amendment first (see PRD §Q&A revision protocol).

### Claude's Discretion
- The `AURA_TTS_MAX_CHARS` default value; the speaker/toggle icon glyphs; the exact MediaRecorder mimeType→STT-format map; whether `/api/tts` streams the body vs buffers the (char-capped) blob; whether TTS+STT share one `SetVoice` setter or two; the exact `/api/voice/capabilities` compute location; and whether the custom adapters live under `web/src/chat/voice/` — all planner/executor discretion, provided the decisions above hold.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec (locked success criteria)
- `.planning/ROADMAP.md` §"Phase 37C: Web Voice Lane (INSERTED)" (lines 460-477) — goal, the 4 success criteria, and the three flagged design forks (a/b/c) with their steers.
- `.planning/REQUIREMENTS.md` §"Web Voice Lane (WEBVOICE)" (lines 74-81) — WEBVOICE-01..04 acceptance text (locked).

### Backend — reuse verbatim (already complete)
- `internal/multimodal/tts.go` — `TTSClient.Synthesize(ctx, text) []byte` (`:60`); cloud arm → OpenRouter `/audio/speech`; `AudioFormat()` (`:42`) + the per-call `Format` field the mp3-for-web arm sets (D-02).
- `internal/multimodal/stt.go` — `STTClient.Transcribe(ctx, audio, fileName, format) string` (`:52`); cloud arm → OpenRouter `/audio/transcriptions` JSON base64 (`:98`); `Cloud()` (`:45`). The `/api/stt` handler wraps this directly (D-08).
- `internal/channels/telegram/tts.go` — `ShouldSpeak(voiceMode, inboundWasVoice)` (`:24`) — the auto-speak parity predicate to mirror (D-07); `VoiceModePref` (`:32`) is the false stub proving no per-conv store exists (D-06).
- `internal/assets/audio_processor.go` — `audioFormat(mimeType)` (`:64`) — the MIME→STT-format map to reuse for the browser-recorder container (D-09); `ProcessAsset` (`:30`) is the async pipeline D-08 deliberately does NOT use.
- `internal/config/config.go` — the voice env fields already loaded (`:194-201`: `STTCloudModel`/`STTLanguage`/`TTSModel`/`TTSVoice`/`TTSFormat`) + `:475-485` mapping; add only `AURA_TTS_MAX_CHARS`.

### Backend — the surfaces to extend
- `internal/agui/assets_api.go` — `registerAssetRoutes` (`:13`) is the route-registration + `RequireAuth`/`principalIdentityID` pattern for the three new voice routes (add a `registerVoiceRoutes`).
- `internal/agui/audit_api.go` — `registerAuditRoutes` (`:62`) + `GET /api/me` handler (`:63`) — the SELF-scoped precedent for `GET /api/voice/capabilities` (D-11).
- `internal/agui/settings_api.go` — `SetSettingsStore` (`:53`) — the exact setter-injection precedent for `SetVoice` (D-13).
- `internal/agui/server.go` — `NewServer` (`:141`) — the `Server` struct the voice clients hang off.
- `cmd/aura/serve.go` — `agui.NewServer(...)` (`:338`) + the `SetIdentityAdmin`/setter calls (`:429`); `cmd/aura/serve_settings.go:10` (`SetSettingsStore`) — where to add the `SetVoice` wiring.
- `cmd/aura/serve_channels.go` — `telegram.MultimodalConfig{...}` assembled from `config.Config` (`:111`, incl. `STTCloudModel`/`TTSModel` + the OpenRouter key) — the exact config→multimodal mapping to reuse for the web voice clients (D-13).

### Web — the surfaces to build/extend
- `web/src/chat/ExternalStoreChat.tsx` — `useExternalStoreRuntime<ThreadMessageLike>({...})` (`:540`) — where `adapters.speech` + `adapters.dictation` attach (D-01); `AssistantRuntimeProvider` (`:551`).
- `web/src/chat/ExternalStoreChat_messages.tsx` — `AssistantMessage` (`:115`) + its `ActionBarPrimitive.Root` (`:186`, Copy/Reload) — where the `Speak`/`StopSpeaking` control lands (D-04).
- `web/src/chat/Composer.tsx` — the Mic handler (`startRecording`/`handleMic`, `:82`-`:122`) that currently records → `uploads.addFiles` (attachment); becomes dictation-primary with this as the KEPT fallback (D-09/D-10).
- `web/src/AppShell.tsx` — the chat-shell header (host for the ephemeral "voice mode" toggle, D-06) + the runtime/adapters wiring surface.
- `web/src/settings/settingsApi.ts` (`fetchSettings`, `:45`) + `internal/agui/audit_api.go` `GET /api/me` — the SPA fetch-on-load precedents for reading `/api/voice/capabilities` (D-11).
- `web/src/i18n/resources.ts` — `chat.attachments.mic`/`micStop` (`:81`, `:354`) — extend with dictation + speaker + voice-mode strings (en + it).

### External reference implementation (assistant-ui — the exact lib Aura runs)
- `D:\tmp\assistant-ui\packages\core\src\adapters\speech.ts` — `SpeechSynthesisAdapter` + `DictationAdapter` interfaces (`:21`, `:50`) + `WebSpeech*` reference impls (the fallbacks D-12 rejects).
- `D:\tmp\assistant-ui\examples\with-elevenlabs-scribe\lib\elevenlabs-scribe-adapter.ts` — the custom `DictationAdapter` template (session status + `onSpeech`/`onSpeechEnd`; strip the realtime WebSocket → one POST to `/api/stt`) (D-09).
- `D:\tmp\assistant-ui\examples\with-elevenlabs-conversational\lib\elevenlabs-voice-adapter.ts` — the `RealtimeVoiceAdapter` (the OUT-OF-SCOPE realtime lane, for the deferred hands-free phase).
- assistant-ui speech guide (`https://www.assistant-ui.com/docs/guides/speech`) — the `ActionBarPrimitive.Speak`/`StopSpeaking` + `adapters.speech` + custom-fetch-TTS-adapter reference (D-02/D-03/D-04).

### 2026 industrial research (user-requested)
- OfflineTTS "Browser Speech Recognition in 2026" (`https://offlinetts.com/blog/browser-speech-recognition-whisper-comparison/`) + AssemblyAI offline-Whisper — the evidence that Web Speech API is draft/inconsistent and server Whisper is the production STT choice (backs D-09/D-12).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `multimodal.TTSClient` / `STTClient` (`internal/multimodal/{tts,stt}.go`): complete, config-only local↔cloud swap; the `/api/tts` and `/api/stt` handlers are thin wrappers — no ML in Go.
- `telegram.ShouldSpeak` (`internal/channels/telegram/tts.go:24`): the exact auto-speak predicate to mirror for D-07 (`voiceMode || turnWasDictated`).
- `assets.audioFormat` (`internal/assets/audio_processor.go:64`): the MIME→STT-format map, reusable for the browser recorder container (D-09).
- assistant-ui `adapters.speech`/`adapters.dictation` + `ActionBarPrimitive.Speak`/`StopSpeaking` (lib `0.14.22`): both features' seams already exist — the composer's editable-transcript insertion (SC#2) is native.
- `Composer.tsx` MediaRecorder logic (`:82`-`:114`): reused as BOTH the dictation recorder (post to `/api/stt`) and the KEPT attachment fallback (D-10).

### Established Patterns
- **Route registration + auth:** `registerAssetRoutes` (`assets_api.go:13`) → `mux.HandleFunc` behind `RequireAuth` + `principalIdentityID` — the shape for `registerVoiceRoutes`.
- **Setter injection at the composition root:** `SetSettingsStore` (`settings_api.go:53`, wired `serve_settings.go:10`) — mirror for `SetVoice` (D-13).
- **SELF-scoped capability read:** `GET /api/me` (`audit_api.go:63`) — the precedent for `/api/voice/capabilities` (D-11).
- **config → multimodal mapping:** `serve_channels.go:111` builds the Telegram `MultimodalConfig` from `config.Config` — reuse verbatim for the web voice clients.
- **SPA fetch-on-load, same-origin cookie:** `fetchSettings` (`settingsApi.ts:45`) + `credentials:'same-origin'` — the blob fetch (D-03) and capabilities read (D-11) follow it.

### Integration Points
- `internal/agui/`: a new `voice_api.go` (`registerVoiceRoutes` + the 3 handlers) + a `SetVoice` setter on `Server`; `server.go` holds the clients.
- `cmd/aura/serve.go` + `serve_settings.go`: build the `multimodal` configs from `config.Config` and call `SetVoice` after `NewServer`.
- `web/src/chat/`: a new `voice/` module (the two custom adapters) attached to `useExternalStoreRuntime` in `ExternalStoreChat.tsx`; the `Speak` control in `ExternalStoreChat_messages.tsx`; the header toggle in `AppShell.tsx`; the mic rewrite in `Composer.tsx`; a capabilities hook gating both.

### Planning gotcha (record it)
- `useExternalStoreRuntime` may surface the speech/dictation adapters differently from `useChatRuntime` (the guide's example). Verify whether they attach via the runtime's `adapters` shared-option or via `RuntimeAdapterProvider`/`useRuntimeAdapters` (both exported by `0.14.22`) — resolve the exact plumbing before building the adapters.

</code_context>

<specifics>
## Specific Ideas

- The user explicitly asked to **"search online and on curated repos for industrial 2026 solutions"** — the decisions are anchored to the **assistant-ui** clone (the exact lib Aura runs, the strongest possible reference) + 2026 STT/TTS research. The key finding that shaped the whole phase: assistant-ui already models both features as adapter seams, so 37C is *wiring custom server-backed adapters*, not building audio/recorder infrastructure.
- Strong throughline (consistent with 37A/37B): **smallest-blast-radius, reference-backed reuse** — reuse `multimodal.TTSClient`/`STTClient` verbatim, mirror `ShouldSpeak`/`SetSettingsStore`/`serve_channels.go` config mapping, use the lib's native adapter seams, keep the attachment path as the fallback rather than replacing it.
- The user consistently picked the **ephemeral / no-new-persistence / universal-codec** options (header toggle over a DB pref; transcribe-and-discard over an asset; mp3 over opus for the web; a dedicated capabilities endpoint over overloading `/api/me`) — the pattern is *ship the parity feature with the thinnest durable surface*.

</specifics>

<deferred>
## Deferred Ideas

- **Realtime full-duplex "hands-free voice mode"** — assistant-ui's `RealtimeVoiceAdapter` (LiveKit / ElevenLabs Conversational; the `with-elevenlabs-conversational` example) — a live mic↔speaker conversation loop. A separate future phase; NOT speaker+dictation parity.
- **Persisted per-conversation voice-mode** — a real preference surviving reload/reopen (a `conversations.voice_mode` column or a per-thread store), wiring the `VoiceModePref` stub for real on both channels. Deferred until a per-conversation preference store lands.
- **Browser Web Speech API adapters as an offline fallback** — the lib's `WebSpeechSynthesisAdapter`/`WebSpeechDictationAdapter` when cloud is unconfigured (voice-without-a-key). Rejected now for clean degrade over inconsistent browser quality (2026 research); revisit if an offline/keyless mode becomes a goal.
- **Server-side TTS caching / prefetch** — caching synth audio beyond the per-message client blob (e.g. content-addressed audio store, or prefetching the latest reply under voice-mode). YAGNI until cost/latency proves it.
- **Streaming TTS playback** — chunked `<audio>` start-before-full-synth for long replies, instead of the D-05 char cap. A latency premium if the char cap proves too blunt.

### Reviewed Todos (not folded)
None — no pending-todo matches surfaced for this phase (`todo.match-phase 37C` → 0).

</deferred>

---

*Phase: 37C-Web Voice Lane*
*Context gathered: 2026-07-09*
