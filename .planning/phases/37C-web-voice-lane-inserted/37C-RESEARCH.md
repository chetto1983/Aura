# Phase 37C: Web Voice Lane - Research

**Researched:** 2026-07-09
**Domain:** assistant-ui adapter seams (speech/dictation) on an external-store runtime + thin Go voice endpoints over `internal/multimodal`
**Confidence:** HIGH (every answer read from installed library source or the local Aura tree; `file:line` cited throughout)

> Scope note: CONTEXT.md decisions **D-01..D-14** are LOCKED and are **not** restated here. This document ADDS the ground-truth that resolves the six open technical questions and produces the required `## Validation Architecture`. The planner MUST honor CONTEXT.md verbatim; this research only fills the gaps CONTEXT flagged (esp. the `useExternalStoreRuntime` adapter-plumbing gotcha at CONTEXT lines 122-123).

## Phase Requirements

| ID | Description (from REQUIREMENTS.md) | Research support (what this doc resolves) |
|----|-----------------------------------|-------------------------------------------|
| WEBVOICE-01 | Speaker control per assistant message + auto-speak "voice mode" | Q2 (`ActionBarPrimitive.Speak`/`StopSpeaking` + `s.message.speech` toggle) + Q5 (mp3 format, char cap) + Q7 `ShouldSpeak` parity |
| WEBVOICE-02 | Composer Mic → in-place editable dictation | Q1 (adapter wiring) + Q3 (custom `DictationAdapter`, native transcript insertion) + Q4 (container→format map) |
| WEBVOICE-03 | Cloud-only + graceful degrade (speaker hidden / mic attachment) | Q6 (`GET /api/voice/capabilities` + native capability gating) |
| WEBVOICE-04 | No attachment regression; `RequireAuth`; React unit + e2e; coverage ≥85% | Q6 (RequireAuth whole-mux wrap) + `## Validation Architecture` |

## Summary

- **Q1 is fully resolved and de-risks the phase.** In `@assistant-ui/react@0.14.22` (types live in `@assistant-ui/core@0.2.17`), an external-store runtime consumes speech/dictation adapters **directly on the `adapters` field of the object passed to `useExternalStoreRuntime`** — `useExternalStoreRuntime({ ..., adapters: { speech, dictation } })`. There is **no `RuntimeAdapterProvider` wiring required** for this path: `external-store-thread-runtime-core.ts:98` returns `this._store.adapters` verbatim, and `:157-162` derives `thread.capabilities.{speech,dictation}` from adapter *presence*. Passing `undefined` for an adapter is the native degrade switch.
- **The composer inserting the editable transcript IS native — but via `onSpeech(isFinal:true)`, NOT `onSpeechEnd`.** `base-composer-runtime-core.ts:424-457` appends `onSpeech` results into the composer's `_text`; `onSpeechEnd` (`:474-476`) only triggers cleanup and **its `{transcript}` payload is ignored**. A custom single-shot POST adapter that fires only `onSpeechEnd({transcript})` will insert *nothing*. This is the #1 landmine.
- **`multimodal.TTSClient.Synthesize(ctx, text)` has NO per-call format argument** (`tts.go:60`); `Format` is fixed at client construction (`tts.go:21/42`). CONTEXT's "TTSClient already takes a per-call Format" is imprecise. mp3-for-web is achieved by building a **dedicated web `TTSClient` instance with `TTSConfig.Format = "mp3"`**, leaving Telegram's opus client untouched — no signature change, matches Aura's smallest-blast-radius throughline.
- **`assets.audioFormat` (`audio_processor.go:64`) matches container MIME by EXACT string and is unexported** — MediaRecorder emits `audio/webm;codecs=opus` / `audio/mp4;codecs=...`, whose `;codecs=` suffix falls through to the `ogg` default. The `/api/stt` handler must strip the codecs parameter before mapping, and the map must be exported (`assets.AudioFormat`) or the handler will silently mis-tag every recording.
- **`AURA_TTS_MAX_CHARS` default = 4096.** OpenAI-compatible `/audio/speech` (which OpenRouter proxies) hard-caps input at 4096 chars and returns HTTP 400 above it ([OpenAI docs + community](https://platform.openai.com/docs/api-reference/audio)). 4096 is the provider ceiling that both prevents the 400 and bounds per-character cost; the planner may lower it for cost but should not exceed it.
- **Two razor-thin LOC ceilings** (CLAUDE.md hard 600 cap): `web/src/chat/ExternalStoreChat.tsx` is **595 LOC** (5 from the cap) and `cmd/aura/serve_webui.go` is **546**. The adapters MUST live in a new `web/src/chat/voice/` module and the route mounts in a new `cmd/aura/serve_webui_voice.go` (mirroring the `serve_webui_musr.go` extraction), or the phase breaks the pre-commit file-size hook.

**Primary recommendation:** Build two custom adapters in `web/src/chat/voice/`, attach them via one `adapters:{speech,dictation}` field on the existing `useExternalStoreRuntime` call, gate each on a capabilities probe, and back them with three thin identity-scoped handlers in a new `internal/agui/voice_api.go` (over narrow interface seams for testability) wired through a new `serve_webui_voice.go`. Everything hangs off patterns already in the tree.

## Open Questions Resolved

### Q1 — assistant-ui adapter plumbing on `useExternalStoreRuntime` (the CONTEXT gotcha)

**Answer: adapters attach directly on the `adapters` field of the external-store options object. No `RuntimeAdapterProvider` is needed for this runtime.**

Version reality: Aura's `@assistant-ui/react@0.14.22` re-exports its runtime types from `@assistant-ui/core@0.2.17` (`web/node_modules/@assistant-ui/react/src/legacy-runtime/runtime-cores/external-store/ExternalStoreAdapter.ts` re-exports `from "@assistant-ui/core"`). The `D:\tmp\assistant-ui` clone is `0.14.11` — **the `speech.ts` interface is byte-identical between the clone and the installed `0.2.17`**, so the clone is a faithful reference.

Ground truth (installed source):

- `@assistant-ui/core/src/runtimes/external-store/external-store-adapter.ts:128-134` — the external-store adapter's optional `adapters` object:
  ```ts
  adapters?: {
    attachments?: AttachmentAdapter | undefined;
    speech?: SpeechSynthesisAdapter | undefined;
    dictation?: DictationAdapter | undefined;
    voice?: RealtimeVoiceAdapter | undefined;   // ← the OUT-OF-SCOPE realtime lane; do NOT set
    feedback?: FeedbackAdapter | undefined;
    threadList?: ExternalStoreThreadListAdapter | undefined;
  } | undefined;
  ```
- `@assistant-ui/core/src/runtimes/external-store/external-store-thread-runtime-core.ts:98` — `get adapters() { return this._store.adapters; }` (the runtime reads adapters straight off the store you pass).
- `…:157-162` — capabilities are **derived from adapter presence**, which is the native degrade:
  ```ts
  speech:    this._store.adapters?.speech    !== undefined,
  dictation: this._store.adapters?.dictation !== undefined,
  ```
- `@assistant-ui/core/src/runtime/base/default-thread-composer-runtime-core.ts:44-46` — `getDictationAdapter() { return this.runtime.adapters?.dictation; }` (dictation resolves through the same `adapters` object; no provider indirection).

Concrete wiring the planner hands the executor — `web/src/chat/ExternalStoreChat.tsx:540`, add **one field** (keep the adapter factories in a separate module so this file stays ≤600 LOC):
```tsx
const runtime = useExternalStoreRuntime<ThreadMessageLike>({
  messages, isRunning, convertMessage: (m) => m,
  onNew, onEdit, onReload, onCancel,
  adapters: {
    ...(caps.tts ? { speech: speechAdapter } : {}),      // undefined ⇒ capabilities.speech=false ⇒ Speak control hidden (D-11)
    ...(caps.stt ? { dictation: dictationAdapter } : {}), // undefined ⇒ capabilities.dictation=false ⇒ native dictate disabled (D-11)
  },
});
```
`RuntimeAdapterProvider`/`useRuntimeAdapters` (also exported by `0.14.22`) is the mechanism used by *other* runtimes (`useChatRuntime`, remote-thread-list) — **not** this external-store path. Using it here would be redundant and is not required. **Confidence: HIGH.**

### Q2 — Speaker control API (`ActionBarPrimitive.Speak` / `StopSpeaking` + `s.message.speech`)

Both primitives exist and are exported: `@assistant-ui/react/src/primitives/actionBar/ActionBarSpeak.ts` (`ActionBarPrimitive.Speak`) and `ActionBarStopSpeaking.tsx` (`ActionBarPrimitive.StopSpeaking`). Speak lifecycle (installed `base-thread-runtime-core.ts:217-245`):
```ts
public speak(messageId) {
  const adapter = this.adapters?.speech;                 // your custom SpeechSynthesisAdapter
  const { message } = this.repository.getMessage(messageId);
  this._stopSpeaking?.();                                 // :223 cancels any prior utterance — only ONE plays at a time
  const utterance = adapter.speak(getThreadMessageText(message));  // :225 lib extracts the message text for you
  const unsub = utterance.subscribe(() => {
    if (utterance.status.type === "ended") { this.speech = undefined; ... }
    else this.speech = { messageId, status: utterance.status };
  });
  this.speech = { messageId, status: utterance.status };  // :236 → surfaces as s.message.speech on THIS message
}
```
State exposure: `thread.speech = { messageId, status }` is scoped per message, so `s.message.speech` is non-null only on the speaking message. Toggle semantics:
- `useActionBarStopSpeaking.ts:6` → `disabled = s.message.speech == null`.
- Both `Speak` and `StopSpeaking` render a **disabled `<button>` (not null)** when their hook returns disabled (`ActionBarStopSpeaking.tsx:38` `disabled={!callback}`). So to get the clean single-icon toggle (D-04), wrap each in Aura's existing `AuiIf` on the speech state rather than relying on auto-hide:
  ```tsx
  <AuiIf condition={(s) => s.message.speech == null}>
    <ActionBarPrimitive.Speak aria-label={t('chat.action.speak')} className="…">…</ActionBarPrimitive.Speak>
  </AuiIf>
  <AuiIf condition={(s) => s.message.speech != null}>
    <ActionBarPrimitive.StopSpeaking aria-label={t('chat.action.stopSpeaking')} className="…">…</ActionBarPrimitive.StopSpeaking>
  </AuiIf>
  ```
Placement: inside the assistant `ActionBarPrimitive.Root` at `web/src/chat/ExternalStoreChat_messages.tsx:186-200`, next to `Copy`/`Reload`/`BranchPicker`. Works with the external-store message — `speak()` takes the message id from the runtime; no per-message adapter needed. **Confidence: HIGH.**

### Q3 — DictationAdapter contract + native editable-transcript insertion

Interface (installed `@assistant-ui/core/src/adapters/speech.ts:25-53`, identical in the clone):
```ts
export type DictationAdapter = {
  listen: () => DictationAdapter.Session;
  disableInputDuringDictation?: boolean;
};
// Session = { status; stop(): Promise<void>; cancel(); onSpeechStart(cb); onSpeechEnd(cb:(r)=>void); onSpeech(cb:(r)=>void) }
// Result  = { transcript: string; isFinal?: boolean }
// Status  = {type:"starting"|"running"} | {type:"ended", reason:"stopped"|"cancelled"|"error"}
```

**Native insertion is confirmed, with a critical nuance.** `base-composer-runtime-core.ts:396-487` (`startDictation`):
- `:414` captures `_dictationBaseText = this._text` (preserves whatever is already typed).
- `:424-457` `onSpeech((result))`: on `isFinal` it commits `result.transcript` into the composer's `_text`; on interim it shows `base + interim`. **This is the code path that writes the editable transcript into the input box** — SC#2 "editable before send" is genuinely free.
- `:474-476` `onSpeechEnd(() => this._cleanupDictation(...))` — **discards its argument**; it only tears the session down.

⇒ A single-shot POST adapter MUST fire **`onSpeech({ transcript, isFinal: true })` to insert**, then `onSpeechEnd(...)` to clean up. Firing only `onSpeechEnd({transcript})` inserts nothing.

Minimal custom adapter = the Scribe template (`D:\tmp\assistant-ui\examples\with-elevenlabs-scribe\lib\elevenlabs-scribe-adapter.ts`) with the realtime WebSocket replaced by one `POST /api/stt`:
```ts
// web/src/chat/voice/dictationAdapter.ts
export function createDictationAdapter(): DictationAdapter {
  return {
    disableInputDuringDictation: true,     // brief record window; no interim stream to interleave with typing
    listen() {
      const cbs = { start: new Set<()=>void>(), end: new Set<(r:any)=>void>(), speech: new Set<(r:any)=>void>() };
      const chunks: Blob[] = []; let rec: MediaRecorder | undefined; let stream: MediaStream | undefined;
      const session: DictationAdapter.Session = {
        status: { type: 'starting' },
        stop:   async () => { rec?.stop(); },                 // onstop drives the POST + callbacks
        cancel: () => { rec?.stop(); stream?.getTracks().forEach(t=>t.stop()); session.status={type:'ended',reason:'cancelled'}; },
        onSpeechStart: (cb)=>{cbs.start.add(cb);return ()=>cbs.start.delete(cb);},
        onSpeechEnd:   (cb)=>{cbs.end.add(cb);return ()=>cbs.end.delete(cb);},
        onSpeech:      (cb)=>{cbs.speech.add(cb);return ()=>cbs.speech.delete(cb);},
      };
      (async () => {
        stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        rec = new MediaRecorder(stream);
        rec.ondataavailable = e => { if (e.data.size) chunks.push(e.data); };
        rec.onstop = async () => {
          stream?.getTracks().forEach(t=>t.stop());
          const blob = new Blob(chunks, { type: rec!.mimeType || 'audio/webm' });   // type carries the container (+codecs)
          const fd = new FormData(); fd.append('audio', blob, 'dictation');
          const res = await fetch('/api/stt', { method:'POST', credentials:'same-origin', body: fd });
          if (!res.ok) { session.status={type:'ended',reason:'error'}; return; }     // → Composer falls back (D-10)
          const { text } = await res.json();
          for (const cb of cbs.speech) cb({ transcript: text, isFinal: true });      // ★ inserts into composer
          session.status = { type:'ended', reason:'stopped' };
          for (const cb of cbs.end) cb({ transcript: text });                        // ★ cleanup (payload ignored by core)
        };
        rec.start(); session.status = { type:'running' }; for (const cb of cbs.start) cb();
      })().catch(() => { session.status = { type:'ended', reason:'error' }; });
      return session;
    },
  };
}
```
Trigger surface: the lib ships `ComposerPrimitive.Dictate` + `useComposerDictate` (`…/primitive-hooks/useComposerDictate.ts`) which calls `aui.composer().startDictation()` and auto-disables when `!s.thread.capabilities.dictation || !s.composer.isEditing`. Aura's Composer can either adopt `ComposerPrimitive.Dictate` (when `caps.stt`) or keep its custom mic Button and call `startDictation()`/`stopDictation()` via the runtime — see Landmines for the Composer integration options. **Confidence: HIGH.**

### Q4 — MediaRecorder container → STT format map

Real browser `MediaRecorder` blob `type` values (2026): Chrome/Edge/Firefox → `audio/webm;codecs=opus` (or bare `audio/webm`); Safari 14.1+ → `audio/mp4` (sometimes `audio/mp4;codecs=mp4a.40.2`). Aura's current Composer already reads `recorder.mimeType || 'audio/webm'` (`Composer.tsx:98`).

The reuse target, `assets.audioFormat(mimeType)` (`internal/assets/audio_processor.go:64`), maps by **exact string**:
```
audio/mpeg|audio/mp3 → mp3   audio/wav|audio/x-wav → wav   audio/webm → webm
audio/flac → flac            audio/mp4|audio/m4a|audio/x-m4a → m4a   default → ogg
```
Two problems for reuse:
1. **The `;codecs=` suffix is not stripped** — `audio/webm;codecs=opus` and `audio/mp4;codecs=…` both miss every case and fall to `ogg`, mis-tagging the container the OpenRouter STT JSON route needs (`stt.go:99` `format` field). The `/api/stt` handler MUST split on `;` (take the media-type before the first `;`) before mapping.
2. **`audioFormat` is unexported** (lowercase) with a single caller (`:48`) — the `internal/agui` handler can't call it. CLAUDE.md forbids duplication (refactor-on-touch), so **export it as `assets.AudioFormat(mimeType)`** and fold the codecs-strip into it (updating the one existing caller). The bare `audio/webm` and `audio/mp4` cases then cover Chrome/FF/Safari; **no new container entries are required** once the codecs param is stripped. **Confidence: HIGH** (map read directly; browser MIME values are stable/well-documented — MEDIUM only on the exact Safari `;codecs` string, which the strip makes irrelevant).

### Q5 — TTS format + char cap

`multimodal.TTSClient.Synthesize(ctx, text) ([]byte, error)` (`tts.go:60`) has **no format parameter**; the container comes from `TTSConfig.Format` at construction (`tts.go:21`) surfaced by `AudioFormat()` (`tts.go:42`, defaults `"opus"`). The cloud arm POSTs `{model, input, voice, response_format}` to OpenRouter `/audio/speech` (`tts.go:64-90`), so `response_format=mp3` is honored by setting `Format:"mp3"`. Telegram's client is built separately (`telegram/tts.go:51-65`, `Format: cfg.TTSFormat` = opus) and is **not** touched.

⇒ D-02 mp3-for-web is a **dedicated web `TTSClient` instance** with `TTSConfig.Format = "mp3"` — not a per-call flag. `audio/mpeg` is the wire content-type the handler sets. (`AudioFormat()` returns `"mp3"`; the HTTP header should be `audio/mpeg`, the canonical mp3 MIME, for `HTMLAudioElement` decoding incl. Safari/iOS.)

**`AURA_TTS_MAX_CHARS` default = 4096.** OpenAI-compatible `/audio/speech` (OpenRouter proxies OpenAI/Kokoro-family TTS on the same contract) hard-limits input to **4096 characters** and returns HTTP 400 above it — historically an undocumented cap, now in the official reference. Sources: [OpenAI Audio API reference](https://platform.openai.com/docs/api-reference/audio), [community confirmation of the hidden 4096 limit](https://community.openai.com/t/tts-model-has-a-hidden-4096-characters-limit/555925). 4096 is the defensible lock: it is the exact provider ceiling, so capping the prefix at 4096 guarantees the call never 400s on length and bounds per-character cost; the D-05 "message too long" hint fires only when the source text exceeds it. The planner may pick a lower number purely for cost (a long report at 4096 chars is a non-trivial synth bill), but should never exceed 4096. **Confidence: HIGH.**

### Q6 — Backend patterns to mirror (routes / capabilities / setter / config)

**Auth architecture (important):** `Server.Mux()` (`server.go:170`) registers routes on a bare mux; the auth gate is applied at the parent mux in `cmd/aura/serve_webui.go:newServeHandler`, which mounts each `/api/*` route explicitly and wraps the whole mux in `agui.RequireAuth(mux, auth)` at **`serve_webui.go:526`**. `/api/` itself is exclusion-only (`:91`), never a bare handler. A route mounted as bare `aguiHandler` gets **RequireAuth only**; `agui.RequireCapability(aguiHandler, auth, cap)` adds a capability. So WEBVOICE-04's "RequireAuth on the TTS endpoint" is satisfied automatically by the whole-mux wrap.

1. **`registerVoiceRoutes` on the AG-UI server** (mirror `registerAssetRoutes`, `assets_api.go:13`) — in a **new `internal/agui/voice_api.go`**:
   ```go
   func (s *Server) registerVoiceRoutes(mux *http.ServeMux) {
     mux.HandleFunc("POST /api/tts", s.handleTTS)
     mux.HandleFunc("POST /api/stt", s.handleSTT)
     mux.HandleFunc("GET /api/voice/capabilities", s.handleVoiceCapabilities)
   }
   ```
   Call it inside `Server.Mux()` (`server.go:170`, beside `s.registerAssetRoutes(mux)`). Each handler follows the asset shape: nil-guard → 503, `identityID, ok := principalIdentityID(r); if !ok { 401 }` (`assets_api.go:40-44,201-204`).
2. **SELF-scoped `GET /api/voice/capabilities`** (mirror `GET /api/me`, `audit_api.go:81`) returns `{ "tts": bool, "stt": bool }` reflecting client presence. It should answer **200 with `{false,false}` when unconfigured** (a probe the SPA reads once on load — never 503), whereas `POST /api/tts`/`/api/stt` return **503 when their client is nil** (D-11/D-13 "until set, the voice routes degrade").
3. **`SetVoice` setter** (mirror `SetSettingsStore`, `settings_api.go:53`; `SetAuditStore`/`SetIdentityAdmin`, `audit_api.go:55/60`). Depend on **narrow interface seams, not the concrete `*multimodal.TTSClient`**, so handlers are unit-testable with fakes (matching every other injected dep — `AssetService`, `ApprovalStore` are interfaces):
   ```go
   type ttsSynthesizer interface { Synthesize(ctx context.Context, text string) ([]byte, error); AudioFormat() string }
   type sttTranscriber interface { Transcribe(ctx context.Context, audio []byte, fileName, format string) (string, error) }
   func (s *Server) SetVoice(tts ttsSynthesizer, stt sttTranscriber, maxChars int) { s.tts, s.stt, s.ttsMaxChars = tts, stt, maxChars }
   ```
   `*multimodal.TTSClient`/`*multimodal.STTClient` satisfy these. Add `tts`, `stt`, `ttsMaxChars` fields to the `Server` struct (`server.go:98-129`).
4. **Composition-root wiring** — build the web voice clients from `config.Config` (D-13). The config fields already exist (`config.go:194-201`, loaded `:478-485`); the OpenRouter credential is `cfg.LLM.APIKey`/`cfg.LLM.BaseURL` (exactly as `serve_channels.go:117-118` maps for Telegram). Cloud-only (D-12): build clients **only when the cloud model is set**, so capabilities = presence:
   ```go
   // cmd/aura/serve_settings.go (or a new serve_voice.go) after agui.NewServer(...)
   if cfg.TTSModel != "" || cfg.STTCloudModel != "" {
     var tts *multimodal.TTSClient; var stt *multimodal.STTClient
     if cfg.TTSModel != "" {
       tts = multimodal.NewTTSClient(multimodal.TTSConfig{
         CloudModel: cfg.TTSModel, Voice: cfg.TTSVoice, Format: "mp3",            // ← web override (D-02)
         OpenRouterBaseURL: cfg.LLM.BaseURL, OpenRouterAPIKey: cfg.LLM.APIKey, TimeoutSec: cfg.MultimodalTimeoutSec,
       })
     }
     if cfg.STTCloudModel != "" {
       stt = multimodal.NewSTTClient(multimodal.STTConfig{
         CloudModel: cfg.STTCloudModel, Language: cfg.STTLanguage,
         OpenRouterBaseURL: cfg.LLM.BaseURL, OpenRouterAPIKey: cfg.LLM.APIKey, TimeoutSec: cfg.MultimodalTimeoutSec,
       })
     }
     server.SetVoice(tts, stt, cfg.TTSMaxChars)   // nil client ⇒ that capability=false ⇒ that POST route 503s
   }
   ```
   Only **`AURA_TTS_MAX_CHARS` is net-new env** (add a `TTSMaxChars int` field + `envutil.IntDefault("AURA_TTS_MAX_CHARS", 4096)` at `config.go:485` + a `KnobSpec` row).
5. **Parent-mux mounts** — in a **new `cmd/aura/serve_webui_voice.go`** (mirror `serve_webui_musr.go:38` exactly — the extraction keeps `serve_webui.go` under 600):
   ```go
   func registerVoiceRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
     mux.Handle(ttsRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))  // cost-bearing POST; or bare for RequireAuth-only
     mux.Handle(sttRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
     mux.Handle(voiceCapabilitiesRoute, aguiHandler)                                       // SELF-scoped read, RequireAuth only (like meRoute)
   }
   ```
   Call `registerVoiceRoutes(mux, aguiHandler, auth)` in `newServeHandler` beside `registerMUSRRoutes(...)` (`serve_webui.go:498`). Route constants (`ttsRoute = "POST /api/tts"`, etc.) go in the new file. Gating the two POSTs on `agentRunCapability` (the same grant `POST /agent/run` uses) is the recommended default since they consume OpenRouter budget; the seeded `local` identity holds `*` so dev is unaffected. **Confidence: HIGH.**

## Implementation Notes / Landmines

1. **`onSpeechEnd` payload is ignored — insert via `onSpeech(isFinal:true)`.** (Q3.) The single biggest executor trap. Encode it in the plan and a unit test that asserts the composer text changes after `onSpeech`, not after `onSpeechEnd`.
2. **`Synthesize` has no per-call format; build a dedicated mp3 web `TTSClient`.** (Q5.) Do not attempt to thread a format arg through the shared client — that regresses Telegram and is a wider blast radius than a second instance.
3. **Strip `;codecs=` and export `assets.AudioFormat`.** (Q4.) Without the strip, every Chrome/FF/Safari recording is mis-tagged `ogg`; the cloud STT may still decode webm-as-ogg inconsistently. Add a unit test with `audio/webm;codecs=opus` and `audio/mp4;codecs=mp4a.40.2` inputs.
4. **Two 600-LOC ceilings.** `ExternalStoreChat.tsx`=595, `serve_webui.go`=546 (verified `wc -l`). Adapters → `web/src/chat/voice/`; a capabilities hook (`useVoiceCapabilities`) → its own file; route mounts → `serve_webui_voice.go`; handlers → `internal/agui/voice_api.go`. Keep the `ExternalStoreChat.tsx` diff to the ~3-line `adapters:` field + imports. `server.go`=505 and `Composer.tsx`=201 have headroom but still get refactor-on-touch.
5. **Blob cache is thread-scoped, not per-row.** (D-03.) `adapter.speak(text)` only sees text — the adapter is one thread-level instance, so cache keyed on the text (or a hash) in the adapter closure; a re-click on the same message hits the cache and never re-bills. True per-message-row `revokeObjectURL` on unmount is not reachable from a thread-scoped adapter — revoke on chat/thread teardown (component unmount) and/or bound the cache (LRU). Plan the test around "second Speak on the same message issues no second fetch" rather than a per-row revoke.
6. **Only one utterance plays at a time.** `base-thread-runtime-core.ts:223` cancels the prior utterance when a second message's Speak fires. "Concurrent speak on two messages" is defined behavior (first cancels), not a bug — assert it, don't fight it.
7. **Composer integration = branch, keep the fallback (D-10).** Two viable shapes: (a) keep the custom mic Button and branch `handleMic` — `caps.stt` → `startDictation()` via the runtime; else → today's `startRecording()`→`uploads.addFiles` (`Composer.tsx:82-114`); or (b) render `ComposerPrimitive.Dictate` when `caps.stt` and the custom attachment mic when `!caps.stt`. (a) matches D-10's "the Mic reverts" language and keeps one control. On a dictation session ending `reason:"error"`, degrade to the attachment path. The attachment/Paperclip path and `chat.attachments.mic`/`micStop` i18n (`resources.ts:80-81` en / `:353-354` it) stay intact (WEBVOICE-04 no-regression).
8. **`ShouldSpeak` is client-side, no round-trip.** `telegram.ShouldSpeak(voiceMode, inboundWasVoice) = voiceMode || inboundWasVoice` (`telegram/tts.go:24`) is a pure boolean OR — re-implement it in TS as `voiceMode || turnWasDictated` (D-07). Track `turnWasDictated` per submitted turn client-side (set when the turn's text was produced via the dictation adapter). No server predicate call needed.
9. **`VoiceModePref` stays a `false` stub** (`telegram/tts.go:32`) — D-06 adds no persistence; the web toggle is ephemeral React state in the header. Do not wire a preference store.
10. **Capabilities probe fetch shape** = `fetchSettings` (`settingsApi.ts:45`): `fetch('/api/voice/capabilities', { headers:{Accept:'application/json'}, credentials:'same-origin' })`. The TTS blob fetch uses the same `credentials:'same-origin'` (D-03).
11. **i18n parity is enforced** — every new key (`chat.action.speak`/`stopSpeaking`, `chat.voiceMode.*`, dictation states) must land in BOTH `en` and `it` blocks of `resources.ts` (the file has a hard en/it mirror; a missing key fails the suite).
12. **PRD-amendment first (D-14).** ROADMAP flags 37C "PRD-first". The amendment (WEBVOICE-01..04, the three routes, the two adapters, mp3-for-web vs opus, `AURA_TTS_MAX_CHARS`) is a required pre-code commit.

## Validation Architecture

`.planning/config.json` → `workflow.nyquist_validation: true` (verified) — this section is REQUIRED and drives VALIDATION.md.

### Test Framework

| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + `net/http/httptest` + `go.uber.org/goleak`; run via `scripts/go_packages.sh` (never raw `./...`, SEC-07) |
| Go handler harness | `NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})` → `s.SetVoice(fakeTTS, fakeSTT, maxChars)` → identity path `withPrincipal(httptest.NewRequest(...), id)` + `s.Mux().ServeHTTP`; 401 path `RequireAuth(s.Mux(), testDeps("secret"))` + no cookie (pattern: `internal/agui/asset_download_test.go:25-70` happy / `:111-129` unauth) |
| Web framework | Vitest (`web/vitest.config.ts`, `npm test` = `vitest run --coverage`) + Stryker (`web/stryker.config.json`, ≥70% killed) + Testing Library; tests in `web/src/chat/__tests__/` |
| E2E | Playwright (`web/playwright.config.ts`, `npm run test:e2e`), live container, `web/e2e/*.spec.ts` + `web/e2e/auth.ts`; precedent `web/e2e/artifacts.spec.ts` (WEBART-08, 4/4 green vs real Authula) |
| Quick run | `go test ./internal/agui/ ./internal/assets/ ./internal/config/` · `cd web && npx vitest run src/chat/voice src/chat/__tests__` |
| Full suite | WSL `make quality-full` (owned-surface ≥85% gate) + `cd web && npm test` + `npm run test:e2e` |

### Phase Requirements → Test Map

| Req | Behavior | Test class | Automated command | Exists? |
|-----|----------|-----------|-------------------|---------|
| WEBVOICE-01 | `POST /api/tts` returns `audio/mpeg`; identity-scoped; 401 w/o cookie; 503 when `s.tts==nil`; char-cap prefix + "too long" signal beyond 4096; **transcribe/synthesize-and-discard** (no asset/DB row) | Go unit | `go test ./internal/agui/ -run TestTTS` | ❌ Wave 0 (`voice_api_test.go`) |
| WEBVOICE-01 | Speaker control state idle→loading→playing→stop→error; `s.message.speech` toggle; auto-speak on `voiceMode \|\| turnWasDictated` | React unit | `npx vitest run src/chat/voice/speechAdapter` | ❌ Wave 0 |
| WEBVOICE-02 | `POST /api/stt` transcribes and **creates NO asset/Garage object/DB row**; identity-scoped; 401; 503 when `s.stt==nil`; container→format map (webm;codecs / mp4;codecs / m4a) | Go unit | `go test ./internal/agui/ -run TestSTT` + `go test ./internal/assets/ -run TestAudioFormat` | ❌ Wave 0 |
| WEBVOICE-02 | Dictation adapter: mocked `MediaRecorder` + mocked `/api/stt` → transcript inserted via `onSpeech(isFinal)` into composer, editable; `onSpeechEnd` cleans up | React unit | `npx vitest run src/chat/voice/dictationAdapter` | ❌ Wave 0 |
| WEBVOICE-03 | `GET /api/voice/capabilities` → `{tts,stt}` (200 even unconfigured, both false); `!tts`⇒Speak hidden, `!stt`⇒mic stays attachment | Go unit + React unit | `go test ./internal/agui/ -run TestVoiceCapabilities` + `npx vitest run src/chat/voice/useVoiceCapabilities` | ❌ Wave 0 |
| WEBVOICE-04 | No regression to attachment-record fallback (mic → `uploads.addFiles` when `!stt`/on error); `RequireAuth` present; speaker + dictation live e2e | React unit + Playwright | `npx vitest run src/chat/Composer` + `npx playwright test voice.spec.ts` | ⚠️ Composer.test.tsx exists; `voice.spec.ts` ❌ Wave 0 |

### Go unit tests (the 3 handlers) — concrete assertions

- **`TestTTS_Owner`**: `withPrincipal(POST /api/tts {text})` → 200, `Content-Type: audio/mpeg`, body == fake synth bytes; assert `fakeTTS.gotText == text` and **no asset-service call** (inject a recording `fakeAssetService`, assert untouched).
- **`TestTTS_Unauth`**: `RequireAuth(s.Mux(), testDeps(...))` + no cookie → 401; `fakeTTS.calls == 0`.
- **`TestTTS_Degraded`**: no `SetVoice` (nil client) → 503.
- **`TestTTS_CharCap`**: text length > `ttsMaxChars` → synth receives the 4096-char prefix AND the response carries the "too long" signal (header or JSON field per planner's D-05 UX choice); text == cap → no signal; empty text → 400 or no-op (choose + assert).
- **`TestSTT_Owner`**: `withPrincipal(POST /api/stt, webm blob)` → 200 `{text}`; assert `fakeSTT.gotFormat == "webm"` for `audio/webm;codecs=opus` input (codecs stripped); **no asset row created**.
- **`TestSTT_Unauth`** / **`TestSTT_Degraded`**: 401 / 503 as above.
- **`TestVoiceCapabilities`**: table — {both set}→`{true,true}`; {tts only}→`{true,false}`; {neither/no SetVoice}→**200** `{false,false}` (never 503); 401 w/o cookie.
- **`assets.TestAudioFormat`** (in `internal/assets`): table incl. `audio/webm;codecs=opus`→`webm`, `audio/mp4;codecs=mp4a.40.2`→`m4a`, bare `audio/webm`→`webm`, unknown→`ogg`.
- All handler tests wrap in `goleak.VerifyNone` (agui convention).

### React unit tests (vitest)

- **speechAdapter**: mock `fetch('/api/tts')` (blob) + `HTMLAudioElement` (`play`/`onended`/`onerror` via `vi.stubGlobal('Audio', …)`); assert Utterance status `running→ended(finished)`, `cancel()` → `ended(cancelled)` + `audio.pause`, 4xx → `ended(error)`; **second `speak(sameText)` issues no second fetch** (blob cache).
- **dictationAdapter**: `vi.stubGlobal('MediaRecorder', FakeRecorder)` + stub `navigator.mediaDevices.getUserMedia`; drive `start→ondataavailable→stop`; assert one `POST /api/stt`, then `onSpeech({transcript,isFinal:true})` fired (transcript present) and `onSpeechEnd` fired after; `/api/stt` 4xx → session `ended(error)` and **no** `onSpeech`.
- **Speaker control**: render an assistant message with a fake runtime; assert `AuiIf` shows Speak when `speech==null`, StopSpeaking when non-null; click transitions.
- **Voice-mode toggle + parity**: unit-test the `shouldSpeak(voiceMode, turnWasDictated)` helper (both-false→false, either-true→true) and that a dictated turn sets `turnWasDictated`.
- **Degrade**: `caps={tts:false}` → no Speak control in the ActionBar; `caps={stt:false}` → mic renders the attachment path (`uploads.addFiles` called on stop), NOT dictation.
- **No-regression (D-10)**: with `caps.stt=false`, the existing `Composer.test.tsx` mic→attachment behavior still passes; add a case for dictation-error → attachment fallback.

### E2E (Playwright, live container)

Harness = the WEBART-08 pattern (`artifacts.spec.ts` + `auth.ts`): `docker compose build aura && up -d` (baked dist), external-serve origin, real Authula login. New `web/e2e/voice.spec.ts`:
- **Speaker**: send a turn, click the message speaker → assert an `<audio>`/playback state and a `POST /api/tts` (route interception) returns `audio/mpeg`.
- **Dictation**: with fake media (`--use-fake-device-for-media-stream` / route-mock `/api/stt`) → mic → transcript appears in the composer input, editable, then Send.
- **Degrade**: stack with `AURA_TTS_MODEL`/`AURA_STT_CLOUD_MODEL` unset → speaker control absent, mic in attachment mode, no console errors; `GET /api/voice/capabilities` → `{false,false}`.

### Nyquist edge / sampling cases (→ held-out / property tests)

empty text (TTS no-op / 400); text at 4096 vs 4097 (cap boundary — prefix + signal); unsupported/blank audio container (default `ogg` + STT still attempts); STT returns empty string (no insert, session ends clean); TTS 4xx from OpenRouter (Utterance `ended(error)`, control resets); blob cache reuse + revoke on thread unmount (no leak, no double-fetch); concurrent Speak on two messages (first cancels — assert single active utterance); mp3-for-web vs opus-for-Telegram both correct (assert the web `TTSClient` sends `response_format=mp3` while the Telegram client still sends `opus` — a table test over two constructed clients); `audio/webm;codecs=opus` and `audio/mp4;codecs=…` both map correctly (codecs-strip property).

### Coverage targets & owned-surface

- **Web ≥85%** (vitest, CLAUDE.md floor) + Stryker ≥70% killed on the two adapters — new files `web/src/chat/voice/{speechAdapter,dictationAdapter,useVoiceCapabilities}.ts` (+ Composer/ExternalStoreChat_messages deltas).
- **Go owned-surface ≥85%** — new files landing in the `db_integration neo4j_integration` coverage gate (`scripts/coverage_gate.sh`): `internal/agui/voice_api.go` and the `internal/assets` `AudioFormat` change. These are **pure request/response + a string map — fully unit-coverable with no daemon**, so they must be exercised by daemon-free unit tests (per CLAUDE.md: daemon-gated code contributes ZERO to the gate). No `docker_integration`-only surface is introduced.

### Wave 0 gaps

- [ ] `internal/agui/voice_api_test.go` — the three-handler suite (auth/identity/degrade/content-type/char-cap/no-persist).
- [ ] `internal/assets/audio_processor_test.go` — add `TestAudioFormat` incl. `;codecs=` cases (or wherever the export lands).
- [ ] `web/src/chat/voice/*.test.ts(x)` — speechAdapter, dictationAdapter, useVoiceCapabilities.
- [ ] Web test infra: a shared `MediaRecorder` + `getUserMedia` + `Audio` mock helper (none exists today — `Composer.test.tsx` mocks only at the `uploads.addFiles` level).
- [ ] `web/e2e/voice.spec.ts` — speaker + dictation + degrade against the live container.
- [ ] Fakes: `fakeTTS`/`fakeSTT` satisfying the new `ttsSynthesizer`/`sttTranscriber` seams + a recording `fakeAssetService` (assert-untouched for the no-persist proof).

## Sources

### Primary (HIGH confidence — read directly)
- Installed `@assistant-ui/core@0.2.17` (via `@assistant-ui/react@0.14.22`): `adapters/speech.ts:21-53`, `runtimes/external-store/external-store-adapter.ts:128-134`, `external-store-thread-runtime-core.ts:98,157-162`, `runtime/base/base-composer-runtime-core.ts:396-487`, `base-thread-runtime-core.ts:217-245`, `default-thread-composer-runtime-core.ts:44-46`, `react/primitive-hooks/{useComposerDictate,useActionBarSpeak,useActionBarStopSpeaking}.ts`, `primitives/actionBar/{ActionBarSpeak.ts,ActionBarStopSpeaking.tsx}`.
- Clone `D:\tmp\assistant-ui` (`0.14.11`): `packages/core/src/adapters/speech.ts` (identical), `examples/with-elevenlabs-scribe/lib/elevenlabs-scribe-adapter.ts` (DictationAdapter template).
- Aura tree: `internal/multimodal/{tts.go:21,42,60,95;stt.go:45,52,98-101}`, `internal/channels/telegram/tts.go:24,32,51-65`, `internal/assets/audio_processor.go:48,64`, `internal/config/config.go:194-201,478-485`, `internal/agui/{assets_api.go:13,40,201;audit_api.go:55,62,81;settings_api.go:53;server.go:98,141,170}`, `cmd/aura/{serve_webui.go:91,330,498,526;serve_webui_musr.go:38;serve_channels.go:110-129}`, `web/src/chat/{ExternalStoreChat.tsx:540;ExternalStoreChat_messages.tsx:186;Composer.tsx:82-172}`, `web/src/settings/settingsApi.ts:45`, `web/src/i18n/resources.ts:80-81,353-354`. Test harness: `internal/agui/asset_download_test.go:25-129`, `internal/agui/auth.go:307-316`. Infra: `.planning/config.json:19`, `web/{vitest,playwright,stryker}.config.*`, `web/e2e/artifacts.spec.ts`.

### Secondary (MEDIUM — verified)
- OpenAI Audio/Speech 4096-char input limit: [platform.openai.com/docs/api-reference/audio](https://platform.openai.com/docs/api-reference/audio), [community.openai.com hidden-4096-limit](https://community.openai.com/t/tts-model-has-a-hidden-4096-characters-limit/555925) — applies to the OpenAI-compatible contract OpenRouter proxies.

## Metadata

**Confidence breakdown:**
- Adapter wiring (Q1/Q2/Q3): HIGH — read from the exact installed runtime source; interface stable clone↔installed.
- Format map / char cap (Q4/Q5): HIGH — map + `Synthesize` signature read directly; 4096 cross-verified against provider docs.
- Backend patterns (Q6): HIGH — every mirror target read with `file:line`; auth wrap traced end-to-end.
- Validation Architecture: HIGH — harness + coverage gate rules read from existing tests and CLAUDE.md.

**Research date:** 2026-07-09
**Valid until:** ~2026-08-09 (stable; re-verify only if `@assistant-ui/react` is bumped past `0.14.x` or `internal/multimodal` signatures change).
