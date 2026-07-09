---
phase: 37C-web-voice-lane-inserted
plan: 04
type: execute
wave: 4
depends_on: ["37C-03"]
files_modified:
  - web/src/chat/voice/useVoiceCapabilities.ts
  - web/src/chat/voice/useVoiceCapabilities.test.ts
  - web/src/chat/voice/speechAdapter.ts
  - web/src/chat/voice/speechAdapter.test.ts
  - web/src/chat/voice/shouldSpeak.ts
  - web/src/chat/voice/shouldSpeak.test.ts
  - web/src/chat/voice/voiceMocks.ts
  - web/src/chat/voice/VoiceModeProvider.tsx
  - web/src/chat/voice/VoiceModeProvider.test.tsx
  - web/src/chat/voice/useAutoSpeak.ts
  - web/src/chat/voice/useAutoSpeak.test.tsx
  - web/src/AppShell.tsx
  - web/src/AppShell.voice.test.tsx
  - web/src/chat/ExternalStoreChat_messages.tsx
  - web/src/chat/ExternalStoreChat_messages.speaker.test.tsx
  - web/src/i18n/resources.ts
autonomous: true
requirements: [WEBVOICE-01, WEBVOICE-03]
must_haves:
  truths:
    - "useVoiceCapabilities reads GET /api/voice/capabilities once (credentials:same-origin) and returns {tts,stt}, defaulting to {false,false} on error/while loading"
    - "speechAdapter.speak(text) fetches /api/tts → blob → new Audio(objectURL); running→ended(finished); cancel → ended(cancelled)+pause; 4xx → ended(error); a repeat speak(sameText) issues NO second fetch (per-text blob cache); object URLs revoked on teardown"
    - "the assistant ActionBar shows Speak when s.message.speech==null, StopSpeaking when non-null, and is ABSENT when caps.tts is false (D-04)"
    - "VoiceModeProvider exposes {caps, voiceMode, toggleVoiceMode, turnWasDictated, markTurnDictated}; the header toggle flips ephemeral session state (no persistence); useAutoSpeak speaks a new assistant reply exactly when shouldSpeak(voiceMode || turnWasDictated) (D-07 auto-speak parity)"
    - "shouldSpeak(voiceMode, turnWasDictated) === (voiceMode || turnWasDictated) — Telegram ShouldSpeak parity"
  artifacts:
    - path: "web/src/chat/voice/speechAdapter.ts"
      provides: "custom SpeechSynthesisAdapter (fetch→blob→Audio, per-text cache, X-Aura-TTS-Truncated hint)"
      min_lines: 40
    - path: "web/src/chat/voice/VoiceModeProvider.tsx"
      provides: "ephemeral voice-mode + turnWasDictated + caps context"
      contains: "turnWasDictated"
    - path: "web/src/chat/voice/useAutoSpeak.ts"
      provides: "auto-speak effect gated by shouldSpeak"
      contains: "shouldSpeak"
    - path: "web/src/chat/voice/voiceMocks.ts"
      provides: "shared test doubles (Audio/MediaRecorder/getUserMedia/fetch) reused by 37C-05"
      contains: "MediaRecorder"
  key_links:
    - from: "web/src/chat/voice/speechAdapter.ts"
      to: "/api/tts"
      via: "fetch(credentials:same-origin) → blob → Audio"
      pattern: "/api/tts"
    - from: "web/src/chat/ExternalStoreChat_messages.tsx"
      to: "ActionBarPrimitive.StopSpeaking"
      via: "AuiIf on s.message.speech, gated by caps.tts"
      pattern: "StopSpeaking"
    - from: "web/src/chat/voice/useAutoSpeak.ts"
      to: "shouldSpeak"
      via: "auto-speak gate on a new assistant reply"
      pattern: "shouldSpeak"
  prohibitions:
    - "MUST NOT re-fetch /api/tts on a repeat Speak of the same message text — per-text blob cache in the adapter closure (D-03)"
    - "MUST NOT persist voice mode — VoiceModeProvider is ephemeral session React state (D-06); no localStorage, no server pref"
    - "MUST NOT wire adapters.voice (RealtimeVoiceAdapter) — out-of-scope realtime lane"
    - "MUST NOT add an i18n key to only one of the en/it blocks — every new key lands in BOTH (parity suite enforced)"
    - "MUST NOT edit ExternalStoreChat.tsx here — the runtime adapters field + AutoSpeak mount are owned by 37C-05 (that near-cap 595-LOC file gets a single consolidated edit)"
---

<objective>
Build the voice-OUTPUT lane in a new `web/src/chat/voice/` module: the one-shot `useVoiceCapabilities` probe, the custom `SpeechSynthesisAdapter` (fetch → blob → `<audio>`, per-text cache, truncation hint), the `shouldSpeak` parity helper, a shared `voiceMocks.ts` (reused by 37C-05), the ephemeral `VoiceModeProvider` (voice mode + `turnWasDictated` + caps) with a header toggle in AppShell, the `useAutoSpeak` effect, and the caps-gated `Speak`/`StopSpeaking` control in the assistant ActionBar.

Purpose: Deliver WEBVOICE-01 speaker output + auto-speak voice-mode, and WEBVOICE-03 degrade (Speaker absent when `!tts`), against the fixed backend contract from 37C-03.
Output: the `voice/` module + provider + auto-speak + Speaker control + AppShell toggle + en/it i18n, all unit-tested. Does NOT touch ExternalStoreChat.tsx (37C-05 owns that).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md
@web/src/chat/ExternalStoreChat_messages.tsx
@web/src/AppShell.tsx
@web/src/settings/settingsApi.ts
@web/src/i18n/resources.ts
</context>

<artifacts_produced>
This plan produces:
- **`voice/useVoiceCapabilities.ts`** — `useVoiceCapabilities(): {tts,stt}` (one-shot `fetch('/api/voice/capabilities', {headers:{Accept:'application/json'}, credentials:'same-origin'})`, default `{false,false}`).
- **`voice/speechAdapter.ts`** — `createSpeechAdapter(): SpeechSynthesisAdapter` (fetch → blob → `new Audio(URL.createObjectURL(blob))`, per-text blob cache, `X-Aura-TTS-Truncated` hint, `dispose()` revokes URLs).
- **`voice/shouldSpeak.ts`** — `shouldSpeak(voiceMode, turnWasDictated) => voiceMode || turnWasDictated`.
- **`voice/voiceMocks.ts`** — shared vitest doubles (`stubAudio`, `stubMediaRecorder`, `stubGetUserMedia`, `mockTtsFetch`) — reused by 37C-05.
- **`voice/VoiceModeProvider.tsx`** — context `{caps, voiceMode, toggleVoiceMode, turnWasDictated, markTurnDictated}` + `useVoiceMode()` hook (ephemeral session state).
- **`voice/useAutoSpeak.ts`** — `useAutoSpeak()` effect that speaks a newly-completed assistant reply when `shouldSpeak(voiceMode, turnWasDictated)`.
- **Speaker control** in `ExternalStoreChat_messages.tsx` (caps.tts-gated `Speak`/`StopSpeaking`).
- **Voice-mode header toggle** in `AppShell.tsx` + `VoiceModeProvider` wrapping the chat surface.
- **i18n keys (en+it):** `chat.action.speak`, `chat.action.stopSpeaking`, `chat.action.tooLong`, `chat.voiceMode.on`, `chat.voiceMode.off`.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: voice/ module — useVoiceCapabilities + speechAdapter + shouldSpeak + shared voiceMocks + tests</name>
  <files>web/src/chat/voice/useVoiceCapabilities.ts, web/src/chat/voice/useVoiceCapabilities.test.ts, web/src/chat/voice/speechAdapter.ts, web/src/chat/voice/speechAdapter.test.ts, web/src/chat/voice/shouldSpeak.ts, web/src/chat/voice/shouldSpeak.test.ts, web/src/chat/voice/voiceMocks.ts</files>
  <behavior>
    - useVoiceCapabilities: resolves {tts:true,stt:false} from a mocked 200; returns {false,false} on reject/non-2xx; fetches exactly once.
    - speechAdapter: speak('hello') → running then ended(finished) after Audio.onended; cancel() → ended(cancelled) + audio.pause called; 4xx /api/tts → ended(error); second speak('hello') issues NO second fetch; X-Aura-TTS-Truncated:true surfaces the truncation hint.
    - shouldSpeak: (false,false)→false; (true,false)→true; (false,true)→true; (true,true)→true.
  </behavior>
  <read_first>
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q1 (adapter attach), Q2 (speak lifecycle), Q5 (audio/mpeg + truncation), Landmines #5 (thread-scoped cache), #6 (single utterance), #8 (shouldSpeak), #10 (fetch shape).
    - web/src/settings/settingsApi.ts:45 — the fetchSettings shape (Accept json + credentials same-origin) to mirror.
    - web/node_modules/@assistant-ui/react adapters/speech.ts (or @assistant-ui/core) — the SpeechSynthesisAdapter + DictationAdapter interfaces (the mocks must model both; 37C-05 reuses the DictationAdapter mocks).
    - an existing web *.test.tsx under web/src/chat — the vitest + Testing Library conventions (vi.stubGlobal, vi.fn, fetch mocking).
    - internal/channels/telegram/tts.go:24 — ShouldSpeak = voiceMode || inboundWasVoice (the exact predicate).
  </read_first>
  <action>
    Create the `web/src/chat/voice/` module. `useVoiceCapabilities.ts`: fetch the capabilities endpoint once (useEffect, same-origin cookie), store `{tts,stt}`, default `{false,false}` while loading/on error. `speechAdapter.ts`: `createSpeechAdapter()` → `SpeechSynthesisAdapter` whose `speak(text)` checks a `Map<string,{url}>` cache keyed on text; on miss `POST /api/tts` (`credentials:'same-origin'`, JSON `{text}`), read `X-Aura-TTS-Truncated`, make + cache an object URL, play via `new Audio(url)`; the Utterance status goes running→ended(finished) on `onended`, ended(error) on failure/`onerror`, ended(cancelled) on `cancel()` (calls `audio.pause()`); a `dispose()` revokes all cached URLs. `shouldSpeak.ts`: the pure OR predicate. `voiceMocks.ts`: `stubAudio` (controllable onended/onerror + pause spy), `stubMediaRecorder` + `stubGetUserMedia` (for 37C-05), `mockTtsFetch(blob,{truncated?})`. Write the three co-located tests for the `<behavior>`. eslint `--max-warnings=0` + prettier clean; each file ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `grep -q "/api/voice/capabilities" web/src/chat/voice/useVoiceCapabilities.ts` AND `grep -q "same-origin" web/src/chat/voice/useVoiceCapabilities.ts`.
    - `grep -q "/api/tts" web/src/chat/voice/speechAdapter.ts` AND `grep -q "X-Aura-TTS-Truncated" web/src/chat/voice/speechAdapter.ts`.
    - `grep -q "voiceMode || turnWasDictated" web/src/chat/voice/shouldSpeak.ts`.
    - `cd web && npx vitest run src/chat/voice/speechAdapter src/chat/voice/useVoiceCapabilities src/chat/voice/shouldSpeak` green (incl. the no-second-fetch cache + cancel/error transitions).
    - `cd web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npx vitest run src/chat/voice/speechAdapter src/chat/voice/useVoiceCapabilities src/chat/voice/shouldSpeak && npx tsc --noEmit && echo VOICE_MODULE_OK</automated>
  </verify>
  <done>The voice/ module has a one-shot capabilities probe, a cached fetch→blob→Audio speech adapter with truncation hint + error/cancel transitions, the shouldSpeak parity helper, and a shared mock helper — all green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: VoiceModeProvider + header toggle (AppShell) + useAutoSpeak + voiceMode i18n</name>
  <files>web/src/chat/voice/VoiceModeProvider.tsx, web/src/chat/voice/VoiceModeProvider.test.tsx, web/src/chat/voice/useAutoSpeak.ts, web/src/chat/voice/useAutoSpeak.test.tsx, web/src/AppShell.tsx, web/src/AppShell.voice.test.tsx, web/src/i18n/resources.ts</files>
  <behavior>
    - VoiceModeProvider: default voiceMode=false, turnWasDictated=false; toggleVoiceMode flips voiceMode; markTurnDictated sets turnWasDictated=true; caps is fed from useVoiceCapabilities; state resets on remount (no persistence).
    - Header toggle: rendered only when caps.tts (or caps.tts||caps.stt per discretion); clicking it flips voiceMode and updates the aria-pressed/label (chat.voiceMode.on/off).
    - useAutoSpeak: when a NEW assistant reply completes and shouldSpeak(voiceMode, turnWasDictated) is true → the runtime speak is invoked once for that message; when shouldSpeak is false → speak NOT invoked; a dictated turn (turnWasDictated=true) triggers auto-speak even with voiceMode off; turnWasDictated is consumed/reset per turn so it does not auto-speak subsequent typed turns.
  </behavior>
  <read_first>
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q2 (speak API) + Landmines #8 (shouldSpeak client-side) + #9 (VoiceModePref stays a stub, no persistence).
    - web/src/AppShell.tsx:420-470 — the chat-surface render (ExternalStoreChat at :426) + the ShellHeader block; where to host the voice-mode toggle (chat header/ShellHeader) and wrap the chat surface in VoiceModeProvider; how AppShell already threads props (onUsage/onArtifact) so caps/voiceMode can flow to ExternalStoreChat in 37C-05.
    - web/node_modules/@assistant-ui/react — the runtime speak API (useAssistantRuntime()/thread runtime .speak(messageId) or an ActionBar hook) useAutoSpeak calls; confirm the exact export.
    - web/src/i18n/resources.ts:80-82 (en) + the it mirror (~:353) — add chat.voiceMode.on/off to BOTH blocks.
    - web/src/chat/voice/shouldSpeak.ts (Task 1) — the predicate useAutoSpeak gates on.
  </read_first>
  <action>
    Create `voice/VoiceModeProvider.tsx`: a React context + `VoiceModeProvider` holding ephemeral `voiceMode` (default false), `turnWasDictated` (default false), a `toggleVoiceMode`, a `markTurnDictated`, and `caps` (from `useVoiceCapabilities()`); export `useVoiceMode()`. NO persistence. Create `voice/useAutoSpeak.ts`: a hook (mounted later inside the runtime subtree by 37C-05) that tracks the latest completed assistant message id and, when it changes and `shouldSpeak(voiceMode, turnWasDictated)` is true, calls the runtime speak once for that message, then clears `turnWasDictated` for the next turn. In AppShell.tsx wrap the chat surface in `VoiceModeProvider` and add a voice-mode toggle button in the chat header (rendered when `caps.tts`), aria-pressed reflecting voiceMode, label `t(voiceMode ? 'chat.voiceMode.on' : 'chat.voiceMode.off')`, a lucide glyph (discretion). Add `chat.voiceMode.on`/`chat.voiceMode.off` to BOTH en+it blocks of resources.ts. Write VoiceModeProvider.test.tsx (state transitions + no-persistence), useAutoSpeak.test.tsx (shouldSpeak-gated speak with a fake runtime + turnWasDictated reset), AppShell.voice.test.tsx (toggle present when caps.tts, flips voiceMode, absent when !caps.tts). Refactor-on-touch: keep AppShell.tsx ≤600 LOC (extract a small VoiceModeToggle sibling if the addition risks the cap).
  </action>
  <acceptance_criteria>
    - `grep -q "turnWasDictated" web/src/chat/voice/VoiceModeProvider.tsx` AND `grep -q "markTurnDictated" web/src/chat/voice/VoiceModeProvider.tsx`.
    - `grep -q "shouldSpeak" web/src/chat/voice/useAutoSpeak.ts`.
    - resources.ts contains `voiceMode` in BOTH en and it blocks.
    - VoiceModeProvider.tsx contains NO `localStorage` and NO fetch-to-persist (ephemeral only).
    - `cd web && npx vitest run src/chat/voice/VoiceModeProvider src/chat/voice/useAutoSpeak src/AppShell.voice` green.
    - `cd web && npx tsc --noEmit` clean; AppShell.tsx ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npx vitest run src/chat/voice/VoiceModeProvider src/chat/voice/useAutoSpeak src/AppShell.voice && npx tsc --noEmit && echo VOICEMODE_OK</automated>
  </verify>
  <done>An ephemeral VoiceModeProvider (voiceMode + turnWasDictated + caps), a header toggle in AppShell, and a shouldSpeak-gated useAutoSpeak hook exist with en+it voiceMode i18n — unit-tested, no persistence.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: caps.tts-gated Speak/StopSpeaking control in the assistant ActionBar + speaker i18n</name>
  <files>web/src/chat/ExternalStoreChat_messages.tsx, web/src/chat/ExternalStoreChat_messages.speaker.test.tsx, web/src/i18n/resources.ts</files>
  <behavior>
    - caps.tts=true, s.message.speech==null → ActionBarPrimitive.Speak (aria-label chat.action.speak) renders; s.message.speech!=null → StopSpeaking (aria-label chat.action.stopSpeaking) renders instead.
    - caps.tts=false → neither Speak nor StopSpeaking is in the ActionBar.
    - resources.ts en+it both contain chat.action.speak, chat.action.stopSpeaking, chat.action.tooLong.
  </behavior>
  <read_first>
    - web/src/chat/ExternalStoreChat_messages.tsx:183-204 — the assistant ActionBarPrimitive.Root (Copy/Reload/BranchPicker) where the Speak/StopSpeaking pair lands; the AuiIf/`s.message.*` condition style used in the file.
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q2 — the AuiIf-on-s.message.speech pattern (both primitives render a disabled button, not null, so gate with AuiIf).
    - web/src/chat/voice/VoiceModeProvider.tsx (Task 2) — `useVoiceMode().caps.tts` is the gate source (no per-row re-fetch).
    - web/src/i18n/resources.ts:80-82 (en) + it mirror — add chat.action.speak/stopSpeaking/tooLong to BOTH.
    - web/node_modules/@assistant-ui/react primitives/actionBar/{ActionBarSpeak,ActionBarStopSpeaking} — confirm the exported primitive names.
  </read_first>
  <action>
    In ExternalStoreChat_messages.tsx add the caps.tts-gated Speaker control into the assistant `ActionBarPrimitive.Root` beside Copy/Reload: two `AuiIf`-wrapped primitives — `ActionBarPrimitive.Speak` (aria-label `t('chat.action.speak')`, shown when `s.message.speech == null`) and `ActionBarPrimitive.StopSpeaking` (aria-label `t('chat.action.stopSpeaking')`, shown when `s.message.speech != null`) — the whole pair rendered only when `useVoiceMode().caps.tts` is true. Hover-reveal styling matching the row; a lucide speaker/volume glyph (discretion). Add `chat.action.speak`, `chat.action.stopSpeaking`, `chat.action.tooLong` to BOTH en+it blocks of resources.ts. Create ExternalStoreChat_messages.speaker.test.tsx asserting the `<behavior>` with a fake runtime + a VoiceMode context wrapper. Keep the file ≤600 LOC (extract a `SpeakerControl` sibling if needed).
  </action>
  <acceptance_criteria>
    - `grep -q "StopSpeaking" web/src/chat/ExternalStoreChat_messages.tsx` AND `grep -q "chat.action.speak" web/src/chat/ExternalStoreChat_messages.tsx`.
    - resources.ts contains `stopSpeaking` and `tooLong` in BOTH en and it blocks.
    - `cd web && npx vitest run src/chat/ExternalStoreChat_messages.speaker` green (toggle + caps-gated absence).
    - `cd web && npm test` passes with coverage ≥85% (no regression) and the i18n parity suite green.
    - `cd web && npx tsc --noEmit` + eslint `--max-warnings=0` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npx vitest run src/chat/ExternalStoreChat_messages.speaker && npx tsc --noEmit && echo SPEAKER_CONTROL_OK</automated>
  </verify>
  <done>The assistant ActionBar shows a caps.tts-gated Speak/StopSpeaking toggle keyed on s.message.speech, with en+it i18n parity for the speaker keys.</done>
</task>

</tasks>

<verification>
- `cd web && npx vitest run src/chat/voice src/chat/ExternalStoreChat_messages.speaker src/AppShell.voice` all green.
- `cd web && npm test` (full suite) green with coverage ≥85% and no i18n-parity failure.
- `cd web && npx tsc --noEmit` clean; every new/edited file ≤600 LOC (AppShell.tsx checked).
</verification>

<success_criteria>
- The voice-output surface exists: capabilities probe, cached fetch→blob→Audio adapter, ephemeral VoiceModeProvider + header toggle, shouldSpeak-gated auto-speak, and a caps.tts-gated Speak/StopSpeaking control — all unit-tested.
- Degrade: with caps.tts=false the Speaker control + toggle are absent (WEBVOICE-03).
</success_criteria>

<output>
Create `.planning/phases/37C-web-voice-lane-inserted/37C-04-SUMMARY.md` when done.
</output>
