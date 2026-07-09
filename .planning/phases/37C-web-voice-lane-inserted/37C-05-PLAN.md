---
phase: 37C-web-voice-lane-inserted
plan: 05
type: execute
wave: 5
depends_on: ["37C-03", "37C-04"]
files_modified:
  - web/src/chat/voice/dictationAdapter.ts
  - web/src/chat/voice/dictationAdapter.test.ts
  - web/src/chat/Composer.tsx
  - web/src/chat/Composer.test.tsx
  - web/src/chat/ExternalStoreChat.tsx
  - web/src/chat/ExternalStoreChat.voice.test.tsx
  - web/src/i18n/resources.ts
autonomous: true
requirements: [WEBVOICE-02, WEBVOICE-04]
must_haves:
  truths:
    - "dictationAdapter (D-09) records via MediaRecorder, POSTs the blob to /api/stt on stop, and inserts the transcript via onSpeech({transcript, isFinal:true}) (NOT onSpeechEnd); onSpeechEnd fires after for cleanup; a /api/stt 4xx ends the session reason:error with NO onSpeech"
    - "dictationAdapter handles an EMPTY transcript (200 {text:''}) by firing NO onSpeech insert and ending the session clean (reason:stopped) — RESEARCH Nyquist edge 'STT returns empty string, session ends clean'"
    - "the Composer Mic is dictation-primary when caps.stt: it starts/stops dictation via the runtime and inserts an editable transcript; a dictated turn calls markTurnDictated; dictation state changes (listening/transcribing/error) are announced via an aria-live region for screen-reader users"
    - "when caps.stt is false OR dictation errors, the Mic reverts to today's MediaRecorder → uploads.addFiles attachment behavior (no regression, WEBVOICE-04); the Paperclip attachment path is unchanged"
    - "both adapters attach directly on useExternalStoreRuntime via adapters:{speech,dictation} — undefined when the matching cap is false — and AutoSpeak is mounted inside the runtime provider"
    - "unmounting the chat/runtime subtree invokes speechAdapter.dispose(), revoking all cached TTS object URLs (no blob-URL leak — RESEARCH Landmine #5 revoke-on-thread-teardown)"
    - "ExternalStoreChat.tsx stays ≤600 LOC (the diff is the ~adapters field + AutoSpeak mount + dispose()-on-unmount cleanup + caps/voice imports)"
  artifacts:
    - path: "web/src/chat/voice/dictationAdapter.ts"
      provides: "custom DictationAdapter (MediaRecorder → POST /api/stt → onSpeech(isFinal:true))"
      min_lines: 40
      contains: "onSpeech"
    - path: "web/src/chat/Composer.tsx"
      provides: "dictation-primary mic with kept attachment fallback + aria-live dictation state"
      contains: "caps"
    - path: "web/src/chat/ExternalStoreChat.tsx"
      provides: "adapters:{speech,dictation} on useExternalStoreRuntime + AutoSpeak mount + speechAdapter.dispose()-on-unmount"
      contains: "dictation"
  key_links:
    - from: "web/src/chat/voice/dictationAdapter.ts"
      to: "/api/stt"
      via: "MediaRecorder onstop → POST blob → onSpeech(isFinal:true)"
      pattern: "/api/stt"
    - from: "web/src/chat/ExternalStoreChat.tsx"
      to: "useExternalStoreRuntime adapters"
      via: "adapters:{speech,dictation} gated on caps"
      pattern: "dictation"
    - from: "web/src/chat/ExternalStoreChat.tsx"
      to: "speechAdapter.dispose"
      via: "useEffect cleanup on chat/thread unmount (revoke object URLs)"
      pattern: "dispose"
    - from: "web/src/chat/Composer.tsx"
      to: "uploads.addFiles"
      via: "kept attachment fallback when !caps.stt / on error"
      pattern: "addFiles"
  prohibitions:
    - "MUST NOT insert the transcript via onSpeechEnd — the core ignores its payload; insertion is onSpeech({transcript, isFinal:true}) (RESEARCH Landmine #1)"
    - "MUST NOT delete or regress the MediaRecorder → uploads.addFiles attachment path — it stays as the degraded fallback (D-10, WEBVOICE-04)"
    - "MUST NOT leave speechAdapter.dispose() uncalled — a useEffect cleanup on the chat/runtime subtree invokes it on unmount so cached blob URLs are revoked (RESEARCH Landmine #5); per-message unmount is not reachable"
    - "MUST NOT wire adapters.voice (RealtimeVoiceAdapter)"
    - "MUST NOT push ExternalStoreChat.tsx over 600 LOC — keep the diff minimal (adapters field + AutoSpeak mount + dispose cleanup + imports); extract if needed"
    - "MUST NOT add an i18n key to only one of the en/it blocks (parity suite enforced)"
---

<objective>
Build the voice-INPUT lane: the custom `DictationAdapter` (MediaRecorder → POST `/api/stt` → transcript inserted natively via `onSpeech(isFinal:true)`), the Composer Mic rewrite (dictation-primary when `caps.stt`, with today's attachment-record path KEPT as the degraded fallback and an aria-live dictation-state announcement), and the final runtime wiring that attaches BOTH adapters on `useExternalStoreRuntime`, mounts `AutoSpeak`, and revokes the speechAdapter's cached object URLs on unmount. This is the last web plan and the sole owner of the near-cap `ExternalStoreChat.tsx` edit.

Purpose: Deliver WEBVOICE-02 dictation (editable transcript before send) + WEBVOICE-04 no-regression of the audio-attachment path, light up the whole voice surface end to end, and close the blob-URL leak by invoking `speechAdapter.dispose()` from a component lifecycle.
Output: `dictationAdapter.ts` (+test), the Composer branch (+test, +aria-live), the `adapters:{speech,dictation}` + `AutoSpeak` + `dispose()`-on-unmount wiring in ExternalStoreChat (+test), and dictation-state i18n.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md
@.claude/skills/accessibility/SKILL.md
@web/src/chat/Composer.tsx
@web/src/chat/ExternalStoreChat.tsx
@web/src/i18n/resources.ts
</context>

<artifacts_produced>
This plan produces:
- **`voice/dictationAdapter.ts`** — `createDictationAdapter(): DictationAdapter` (`disableInputDuringDictation:true`; `listen()` starts MediaRecorder, on stop POSTs the blob to `/api/stt`, fires `onSpeech({transcript, isFinal:true})` to insert, then `onSpeechEnd` for cleanup; a 4xx → `status ended(error)` and no insert; an empty transcript → no insert, clean end).
- **Composer dictation branch** — `handleMic` routes to runtime `startDictation`/`stopDictation` when `caps.stt`, else to the existing `startRecording`→`uploads.addFiles`; on dictation error it degrades to the attachment path; a dictated transcript calls `markTurnDictated()`; an aria-live region announces the dictation state (listening/transcribing/error).
- **Runtime wiring** in `ExternalStoreChat.tsx` — `adapters:{ speech: caps.tts?speechAdapter:undefined, dictation: caps.stt?dictationAdapter:undefined }` on `useExternalStoreRuntime`, `<AutoSpeak/>` mounted inside `AssistantRuntimeProvider`, a `useEffect` cleanup invoking `speechAdapter.dispose()` on unmount, and caps threaded from `useVoiceMode()`.
- **i18n keys (en+it):** dictation states — `chat.dictation.listening`, `chat.dictation.transcribing`, `chat.dictation.error` (names at discretion; both locales).
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: dictationAdapter.ts (MediaRecorder → POST /api/stt → onSpeech(isFinal:true)) + test</name>
  <files>web/src/chat/voice/dictationAdapter.ts, web/src/chat/voice/dictationAdapter.test.ts</files>
  <behavior>
    - listen() → getUserMedia + MediaRecorder start; session status starting→running; onSpeechStart fires.
    - stop() → recorder.onstop POSTs a Blob (type = recorder.mimeType) as multipart 'audio' to /api/stt; on 200 {text} → onSpeech({transcript:text, isFinal:true}) fires FIRST (inserts), then onSpeechEnd fires (cleanup); status ended(stopped).
    - empty transcript: on 200 {text:''} → NO onSpeech insert (nothing to insert), session ends clean (reason:stopped); onSpeechEnd may still fire for cleanup — RESEARCH Nyquist edge "STT returns empty string, session ends clean".
    - /api/stt 4xx → status ended(error); NO onSpeech fired (Composer degrades).
    - cancel() → tracks stopped, status ended(cancelled), no POST.
  </behavior>
  <read_first>
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q3 (the DictationAdapter contract + the concrete adapter sketch) + Landmine #1 (onSpeech NOT onSpeechEnd inserts) — THE critical trap; + § "Nyquist edge / sampling cases" (STT returns empty string → no insert, clean end).
    - web/src/chat/voice/voiceMocks.ts (37C-04) — stubMediaRecorder + stubGetUserMedia + fetch mocks to drive the test.
    - web/node_modules/@assistant-ui/react adapters/speech.ts — the DictationAdapter.Session shape (status; stop; cancel; onSpeechStart/onSpeechEnd/onSpeech) the adapter must implement.
    - web/src/chat/Composer.tsx:82-114 — the existing MediaRecorder recording logic (mimeType read, chunk collection, track cleanup) to mirror in the adapter.
  </read_first>
  <action>
    Create `voice/dictationAdapter.ts` per RESEARCH § Q3: `createDictationAdapter()` returns a `DictationAdapter` with `disableInputDuringDictation:true` and a `listen()` that opens `getUserMedia({audio:true})`, starts a `MediaRecorder`, collects chunks, and on `onstop` builds a `Blob({type: rec.mimeType||'audio/webm'})`, appends it as multipart form field `audio` (filename 'dictation'), and `fetch('/api/stt', {method:'POST', credentials:'same-origin', body: fd})`. On a non-ok response set `status = {type:'ended', reason:'error'}` and fire nothing else. On 200, read `{text}`: if `text` is a non-empty string fire `onSpeech({transcript:text, isFinal:true})` FIRST (the insert path); if `text` is empty, fire NO `onSpeech` (nothing to insert). In both cases set `status={type:'ended',reason:'stopped'}`, then fire `onSpeechEnd({transcript:text})` (cleanup, payload ignored by core). `cancel()` stops tracks + sets ended(cancelled) with no POST. Write dictationAdapter.test.ts using the shared voiceMocks: assert the POST fires once, `onSpeech(isFinal:true)` fires with the transcript, `onSpeechEnd` fires after, a 4xx ends the session error with NO onSpeech, AND an empty-transcript 200 fires NO onSpeech and ends clean (reason:stopped). eslint/prettier clean; file ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `grep -q "/api/stt" web/src/chat/voice/dictationAdapter.ts` AND `grep -q "isFinal: *true" web/src/chat/voice/dictationAdapter.ts`.
    - dictationAdapter.ts fires `onSpeech` before `onSpeechEnd` (insertion via onSpeech, not onSpeechEnd) and skips `onSpeech` on an empty transcript.
    - `cd web && npx vitest run src/chat/voice/dictationAdapter` green (POST-once, onSpeech-inserts, onSpeechEnd-after, 4xx→error-no-insert, empty-transcript→no-insert-clean-end).
    - `cd web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npx vitest run src/chat/voice/dictationAdapter && npx tsc --noEmit && echo DICTATION_ADAPTER_OK</automated>
  </verify>
  <done>The DictationAdapter records, POSTs to /api/stt on stop, inserts the transcript via onSpeech(isFinal:true), cleans up via onSpeechEnd, ends error (no insert) on a 4xx, and on an empty transcript inserts nothing and ends clean — proven with the shared mocks.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Composer Mic rewrite — dictation-primary + aria-live state + kept attachment fallback + markTurnDictated + dictation i18n</name>
  <files>web/src/chat/Composer.tsx, web/src/chat/Composer.test.tsx, web/src/i18n/resources.ts</files>
  <behavior>
    - caps.stt=true → handleMic starts/stops dictation via the runtime (startDictation/stopDictation); on a dictated transcript insert, markTurnDictated() is called; the mic button shows a listening/transcribing state.
    - dictation state a11y: state changes (listening / transcribing / error) are announced via an aria-live="polite" (or role="status") region so screen-reader users hear the transitions; the mic <button> carries an aria-label/aria-pressed reflecting the listening state (accessibility skill § Live regions 4.1.3 + § Icon buttons need accessible names).
    - caps.stt=false → handleMic runs the EXISTING startRecording()→uploads.addFiles attachment path unchanged (no regression); the existing Composer.test.tsx mic→attachment assertions still pass.
    - dictation session ending reason:error → the Composer degrades to the attachment path (or surfaces chat.dictation.error via the aria-live region and leaves the mic usable).
    - The Paperclip file-attachment path is untouched.
  </behavior>
  <read_first>
    - web/src/chat/Composer.tsx:78-174 — the current startRecording/stopRecording/handleMic + the Mic/Paperclip buttons (the attachment path to KEEP + branch).
    - .claude/skills/accessibility/SKILL.md § Robust → Live regions (4.1.3) + § Perceivable → Icon buttons need accessible names — the aria-live announcement + aria-label guidance for the dictation-state region and the mic button.
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q3 (trigger surface: ComposerPrimitive.Dictate / useComposerDictate / runtime startDictation) + Landmine #7 (Composer integration = branch, keep the fallback) + Landmine #11 (i18n parity).
    - web/src/chat/voice/VoiceModeProvider.tsx (37C-04) — useVoiceMode() gives caps.stt + markTurnDictated.
    - web/node_modules/@assistant-ui/react — useComposerRuntime()/thread composer startDictation()/stopDictation() (or ComposerPrimitive.Dictate + useComposerDictate) — confirm the exact API used to trigger dictation.
    - web/src/i18n/resources.ts:80-82 (en `chat.attachments.mic`/`micStop`) + it mirror — KEEP these; add the dictation-state keys to BOTH blocks.
    - web/src/chat/Composer.test.tsx — the existing mic→uploads.addFiles test to preserve; add the dictation-branch + error-fallback + aria-live cases.
  </read_first>
  <action>
    Rewrite the Mic handling in Composer.tsx to branch on `useVoiceMode().caps.stt`: when true, `handleMic` toggles runtime dictation (`startDictation()`/`stopDictation()` — the adapter's onSpeech inserts the editable transcript natively) and calls `markTurnDictated()` when a dictation transcript is inserted; render a listening/transcribing mic state using the new dictation-state i18n. Add an `aria-live="polite"` (or `role="status"`) region that announces the current dictation state text (`t('chat.dictation.listening')` / `transcribing` / `error`) on each transition per the accessibility skill's live-regions guidance, and give the mic `<button>` an accurate `aria-label` (+ `aria-pressed` reflecting the listening state) so it has an accessible name in both dictation and attachment modes. When `caps.stt` is false OR a dictation session ends `reason:'error'`, fall back to the EXISTING `startRecording()`→`uploads.addFiles(['voice-note'])` attachment path — do not delete or alter it (an error announces `chat.dictation.error` via the live region and leaves the mic usable). Leave the Paperclip file path untouched. Add dictation-state keys (`chat.dictation.listening`, `chat.dictation.transcribing`, `chat.dictation.error`; names at discretion) to BOTH en+it blocks of resources.ts; keep the existing `chat.attachments.mic`/`micStop` keys. Extend Composer.test.tsx: the existing mic→attachment case must still pass with caps.stt=false; add caps.stt=true → dictation-branch (startDictation called, markTurnDictated called on insert), dictation-error → attachment fallback, AND an assertion that an aria-live/`role=status` region announces the dictation state (listening/transcribing/error). Refactor-on-touch: keep Composer.tsx ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `grep -q "addFiles" web/src/chat/Composer.tsx` (attachment fallback KEPT) AND `grep -q "caps" web/src/chat/Composer.tsx` (dictation branch on caps.stt).
    - `grep -q "markTurnDictated" web/src/chat/Composer.tsx`.
    - `grep -qE "aria-live|role=\"status\"" web/src/chat/Composer.tsx` (dictation-state announcement region present).
    - resources.ts still contains `mic`/`micStop` AND now contains the dictation-state keys in BOTH en and it blocks.
    - `cd web && npx vitest run src/chat/Composer` green — the pre-existing mic→attachment test still passes AND the new dictation-branch + error-fallback + aria-live announcement cases pass.
    - `cd web && npx tsc --noEmit` + eslint `--max-warnings=0` clean; Composer.tsx ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npx vitest run src/chat/Composer && npx tsc --noEmit && echo COMPOSER_DICTATION_OK</automated>
  </verify>
  <done>The Mic is dictation-primary when caps.stt (runtime dictation + markTurnDictated + an aria-live dictation-state announcement), reverts to the unchanged uploads.addFiles attachment path when !caps.stt or on error, with en+it dictation-state i18n and no attachment regression.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Wire both adapters into useExternalStoreRuntime + mount AutoSpeak + speechAdapter.dispose() on unmount (ExternalStoreChat.tsx)</name>
  <files>web/src/chat/ExternalStoreChat.tsx, web/src/chat/ExternalStoreChat.voice.test.tsx</files>
  <behavior>
    - useExternalStoreRuntime receives adapters:{speech, dictation} where speech is the speechAdapter only when caps.tts (else undefined) and dictation is the dictationAdapter only when caps.stt (else undefined).
    - With caps={tts:false,stt:false} both adapters are undefined → runtime capabilities.speech/dictation false (native degrade) → Speaker + dictation inert; no console errors.
    - AutoSpeak is mounted inside AssistantRuntimeProvider so auto-speak has runtime access.
    - dispose-on-unmount: a useEffect cleanup invokes speechAdapter.dispose() when the chat/runtime subtree unmounts, revoking all cached TTS object URLs (RESEARCH Landmine #5 — revoke on thread teardown; per-message unmount isn't reachable from a thread-scoped adapter). Unmounting the component calls dispose()/URL.revokeObjectURL.
    - ExternalStoreChat.tsx remains ≤600 LOC.
  </behavior>
  <read_first>
    - web/src/chat/ExternalStoreChat.tsx:540-593 — the useExternalStoreRuntime({...}) call (add the `adapters` field) + the AssistantRuntimeProvider subtree (mount <AutoSpeak/>); note the file is ~595 LOC — keep the diff minimal (RESEARCH Landmine #4).
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q1 — the exact adapters:{...(caps.tts?{speech}:{}), ...(caps.stt?{dictation}:{})} wiring on the external-store runtime (NO RuntimeAdapterProvider); capabilities derive from adapter presence; Landmine #5 — revoke object URLs on chat/thread teardown.
    - web/src/chat/voice/{speechAdapter,dictationAdapter,useAutoSpeak,VoiceModeProvider}.ts(x) (37C-04/05) — the factories + useVoiceMode() caps + <AutoSpeak/> to import and mount; speechAdapter.dispose() is the teardown-revoke call.
    - web/src/chat/ExternalStoreChat.tsx:150-183 — the ExternalStoreChatProps + component signature (thread caps/adapters in via useVoiceMode() context so the props stay unchanged; the adapters are created once via useMemo).
  </read_first>
  <action>
    In ExternalStoreChat.tsx add the `adapters` field to the existing `useExternalStoreRuntime<ThreadMessageLike>({...})` call: `adapters: { ...(caps.tts ? { speech: speechAdapter } : {}), ...(caps.stt ? { dictation: dictationAdapter } : {}) }`, where `caps` comes from `useVoiceMode()` and `speechAdapter`/`dictationAdapter` are created once with `useMemo(() => createSpeechAdapter(), [])` / `useMemo(() => createDictationAdapter(), [])`. Add a `useEffect(() => () => speechAdapter.dispose(), [speechAdapter])` cleanup so the memoized speech adapter revokes its cached object URLs when the chat/runtime subtree unmounts (RESEARCH Landmine #5 — per-message unmount isn't reachable, so revoke on thread teardown). Mount `<AutoSpeak />` inside `AssistantRuntimeProvider` (one line) so the auto-speak effect has runtime access. Keep the diff to the adapters field + the AutoSpeak mount + the dispose cleanup + the imports (RESEARCH Landmine #4) — if the file would exceed 600 LOC, extract the adapter-memo + caps read + dispose effect into a tiny `useVoiceAdapters()` helper in voice/ and import it. Create ExternalStoreChat.voice.test.tsx: with caps={false,false} the runtime gets no speech/dictation adapter (both undefined) and renders with no console error; with caps={true,true} both adapters are present and AutoSpeak is mounted; unmounting the component invokes speechAdapter.dispose() (assert via a spy on the adapter's dispose / URL.revokeObjectURL). eslint/prettier + tsc clean.
  </action>
  <acceptance_criteria>
    - `grep -q "adapters:" web/src/chat/ExternalStoreChat.tsx` AND `grep -q "dictation" web/src/chat/ExternalStoreChat.tsx` AND `grep -q "AutoSpeak" web/src/chat/ExternalStoreChat.tsx` AND `grep -q "dispose" web/src/chat/ExternalStoreChat.tsx`.
    - ExternalStoreChat.tsx does NOT reference `RuntimeAdapterProvider` (the external-store path attaches adapters directly).
    - ExternalStoreChat.voice.test.tsx asserts speechAdapter.dispose() (URL.revokeObjectURL) fires on unmount.
    - `cd web && npx vitest run src/chat/ExternalStoreChat.voice` green (both-false → undefined adapters, both-true → present + AutoSpeak mounted, unmount → dispose invoked).
    - `cd web && npm test` full suite green with coverage ≥85% (no regression).
    - `cd web && npx tsc --noEmit` clean; `wc -l web/src/chat/ExternalStoreChat.tsx` ≤ 600.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npx vitest run src/chat/ExternalStoreChat.voice && npx tsc --noEmit && test "$(wc -l < src/chat/ExternalStoreChat.tsx)" -le 600 && echo RUNTIME_WIRING_OK</automated>
  </verify>
  <done>Both custom adapters attach directly on useExternalStoreRuntime (undefined = native degrade), AutoSpeak is mounted inside the runtime provider, and speechAdapter.dispose() is invoked on unmount (cached object URLs revoked — no blob-URL leak); ExternalStoreChat.tsx stays ≤600 LOC.</done>
</task>

</tasks>

<verification>
- `cd web && npx vitest run src/chat/voice/dictationAdapter src/chat/Composer src/chat/ExternalStoreChat.voice` all green.
- `cd web && npm test` (full suite) green with coverage ≥85% and no i18n-parity failure; the pre-existing Composer mic→attachment test still passes (no regression).
- `cd web && npx tsc --noEmit` clean; ExternalStoreChat.tsx + Composer.tsx ≤600 LOC.
</verification>

<success_criteria>
- Dictation works: record → /api/stt → editable transcript inserted via onSpeech(isFinal:true) (WEBVOICE-02); an empty transcript inserts nothing and ends clean; a dictated turn marks turnWasDictated for auto-speak parity; dictation state is announced via aria-live.
- No regression: when !caps.stt or on dictation error the Mic reverts to uploads.addFiles; the Paperclip path is untouched (WEBVOICE-04).
- Both adapters attach directly on the external-store runtime with undefined-as-degrade; AutoSpeak is live inside the provider; speechAdapter.dispose() revokes cached object URLs on unmount (no blob-URL leak).
</success_criteria>

<output>
Create `.planning/phases/37C-web-voice-lane-inserted/37C-05-SUMMARY.md` when done.
</output>
