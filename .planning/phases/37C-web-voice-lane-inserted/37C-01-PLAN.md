---
phase: 37C-web-voice-lane-inserted
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - prd.md
autonomous: true
requirements: [WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04]
must_haves:
  truths:
    - "prd.md documents the WEBVOICE-01..04 requirement group as a named subsection (transcribed from REQUIREMENTS.md)"
    - "prd.md documents the three new authenticated identity-scoped web voice routes: POST /api/tts, POST /api/stt, GET /api/voice/capabilities"
    - "prd.md documents the two assistant-ui adapter seams (adapters.speech / adapters.dictation) as the wiring spine, and that dictation transcript insertion is native via onSpeech(isFinal:true)"
    - "prd.md records mp3-for-web (a dedicated web TTSClient with Format=mp3 → audio/mpeg) vs opus-for-Telegram (untouched), and the net-new AURA_TTS_MAX_CHARS knob (default 4096)"
    - "prd.md records the ephemeral session-scoped voice-mode toggle (no new persistence) and transcribe-and-discard STT (no asset/DB row)"
    - "prd.md reconciles the 'per-conversation voice mode' wording (ROADMAP SC#1 / REQUIREMENTS WEBVOICE-01) with the delivered ephemeral session-scoped toggle: for 37C the voice-mode toggle is session-scoped (resets on reload; VoiceModePref stays a false stub) and the PERSISTED per-conversation voice-mode preference is explicitly DEFERRED to a future phase"
  artifacts:
    - path: "prd.md"
      provides: "37C PRD-amendment covering WEBVOICE-01..04 + web voice surface + adapters + mp3-vs-opus + AURA_TTS_MAX_CHARS + the per-conversation-vs-ephemeral scope reconciliation"
      contains: "WEBVOICE-01"
  key_links: []
  prohibitions:
    - "MUST NOT write any Go source, web/ source, or test file in this plan — this is a docs-only PRD-first gate (D-14)"
    - "MUST NOT record a per-call TTS Format — RESEARCH corrects CONTEXT: Synthesize has NO per-call format; mp3-for-web is a dedicated web TTSClient instance with TTSConfig.Format=mp3"
    - "MUST NOT record onSpeechEnd as the transcript-insertion path — the core ignores its payload; insertion is via onSpeech(isFinal:true)"
    - "MUST NOT record any new persistence (voice-mode column, per-conversation store, dictation asset) — the phase adds none; the persisted per-conversation voice-mode is recorded as DEFERRED, not delivered"
---

<objective>
Land the mandatory PRD-amendment commit that gates every 37C code plan (D-14, PRD-first absolute — CLAUDE.md "Senza PRD completo non si scrive una riga di codice"). The web voice surface is currently undocumented in prd.md: the WEBVOICE-01..04 requirement group, the three new routes, the two assistant-ui adapter seams, mp3-for-web vs opus-for-Telegram, and the net-new AURA_TTS_MAX_CHARS knob. This plan documents them BEFORE any implementation code is written, and reconciles the "per-conversation voice mode" phrasing in ROADMAP SC#1 / WEBVOICE-01 with the ephemeral session-scoped scope D-06 actually delivers.

Purpose: Satisfy the PRD-first principle and D-14; establish the architectural record every downstream 37C plan (02–06) builds against and depends_on.
Output: An amended prd.md committed as a standalone PRD-amendment.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@.planning/phases/37C-web-voice-lane-inserted/37C-CONTEXT.md
@.planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md
</context>

<artifacts_produced>
This plan produces:
- **prd.md amendment** — a new "Web Voice Lane (Voce Web) — 37C" subsection adjacent to the WEBART content (near prd.md:2938) covering: (1) the WEBVOICE-01..04 requirement group with acceptance text; (2) the three authenticated identity-scoped routes `POST /api/tts` (returns `audio/mpeg`), `POST /api/stt` (transcribe-and-discard, no asset persisted), `GET /api/voice/capabilities` (SELF-scoped `{tts,stt}`, 200 even when unconfigured); (3) the two assistant-ui adapter seams `adapters.speech` (`SpeechSynthesisAdapter`) + `adapters.dictation` (`DictationAdapter`) attached directly on `useExternalStoreRuntime`, with native transcript insertion via `onSpeech(isFinal:true)`; (4) mp3-for-web (a dedicated web `TTSClient` built with `TTSConfig.Format="mp3"`, `response_format=mp3`, wire `audio/mpeg`) vs opus-for-Telegram (untouched); (5) the ephemeral session-scoped voice-mode header toggle (no new persistence) with an explicit reconciliation of the "per-conversation" wording (persisted per-conv voice-mode = DEFERRED), the `ShouldSpeak(voiceMode || turnWasDictated)` parity predicate, the attachment-record fallback kept for `!stt`/error, and the net-new `AURA_TTS_MAX_CHARS` knob (default 4096).

No code symbols are produced by this plan.
</artifacts_produced>

<tasks>

<task type="auto">
  <name>Task 1: Amend prd.md with the 37C Web Voice Lane (WEBVOICE-01..04 + routes + adapters + mp3-vs-opus + AURA_TTS_MAX_CHARS + per-conv/ephemeral reconciliation)</name>
  <files>prd.md</files>
  <read_first>
    - prd.md around line 2938 — the existing WEBART-05..08 amendment block (the "Requirement group" PRD-record shape) to mirror and append the WEBVOICE amendment adjacently; match the existing PRD heading/blockquote/format conventions.
    - .planning/REQUIREMENTS.md:74-81 — the locked WEBVOICE-01..04 acceptance text to transcribe faithfully (note WEBVOICE-01's "per-conversation voice mode" phrasing — the reconciliation target).
    - .planning/ROADMAP.md §"Phase 37C" SC#1 — the "per-conversation voice mode" success-criterion wording to reconcile at the PRD level.
    - .planning/phases/37C-web-voice-lane-inserted/37C-CONTEXT.md D-01..D-14 — the decision record the amendment must reflect (esp. D-02 mp3-for-web, D-06 ephemeral no-persistence toggle + persisted per-conv DEFERRED, D-08 transcribe-and-discard, D-11 capabilities endpoint, D-14 PRD-first) and the Deferred Ideas block ("Persisted per-conversation voice-mode").
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Summary + § Open Questions Resolved — the corrections to carry: no per-call TTS Format (dedicated web client Format=mp3); insertion via onSpeech not onSpeechEnd; AURA_TTS_MAX_CHARS default 4096 (OpenAI /audio/speech 4096-char ceiling); AudioFormat export + ;codecs strip.
  </read_first>
  <action>
    Add a "Web Voice Lane (Voce Web) — 37C" subsection to prd.md adjacent to the existing WEBART amendment (near line 2938). Document: (1) the four requirements WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04 with their acceptance text; (2) the three new authenticated identity-scoped routes — `POST /api/tts` over `multimodal.TTSClient.Synthesize` returning `audio/mpeg` (mp3) with a soft char cap and an `X-Aura-TTS-Truncated` signal beyond the cap; `POST /api/stt` over `multimodal.STTClient.Transcribe` that is transcribe-and-discard (NO `assets.Asset`, NO Garage object, NO DB row, NO async poll); `GET /api/voice/capabilities` SELF-scoped like `/api/me` returning `{tts:bool, stt:bool}` (200 even when unconfigured, both false, never 503), each flag reflecting whether `AURA_TTS_MODEL` / `AURA_STT_CLOUD_MODEL` is configured; note `RequireAuth` gates all three via the whole-mux wrap and the two POSTs additionally carry `agent.run` capability (cost-bearing); (3) the assistant-ui adapter spine — a custom `SpeechSynthesisAdapter` on `adapters.speech` (fetch → blob → `<audio>`, blob cached per message) and a custom `DictationAdapter` on `adapters.dictation` (MediaRecorder → POST `/api/stt` → transcript inserted natively into the composer via `onSpeech({transcript, isFinal:true})`, editable before send), both attached directly on the existing `useExternalStoreRuntime` call — record explicitly that `onSpeechEnd` only cleans up and its payload is ignored, and that passing `undefined` for an adapter is the native degrade switch (capabilities derive from adapter presence); the speaker control is `ActionBarPrimitive.Speak`/`StopSpeaking` in the assistant ActionBar; (4) mp3-for-web vs opus-for-Telegram — record that `Synthesize` has NO per-call format argument, so mp3-for-web is achieved by constructing a DEDICATED web `TTSClient` instance with `TTSConfig.Format="mp3"` (wire content-type `audio/mpeg`, `response_format=mp3`) while Telegram's opus `TTSClient` is untouched; (5) the ephemeral session-scoped voice-mode header toggle — and here explicitly RECONCILE the scope: ROADMAP SC#1 and REQUIREMENTS WEBVOICE-01 phrase this as "per-conversation voice mode", but for 37C the delivered scope is an EPHEMERAL / session-scoped React toggle that resets on reload (there is NO per-conversation preference store today — `VoiceModePref` stays a `false` stub, no new persistence), and a PERSISTED per-conversation voice-mode preference (a `conversations.voice_mode` column or per-thread store) is explicitly DEFERRED to a future phase (when such a store lands); state this reconciliation in the amendment so the delivered vs. deferred scope is unambiguous at the PRD level. Also record the `ShouldSpeak(voiceMode || turnWasDictated)` Telegram-parity predicate, the kept attachment-record fallback (mic reverts to `uploads.addFiles` when `!stt` or on dictation error — no regression), and the net-new `AURA_TTS_MAX_CHARS` knob with default 4096 (the OpenAI-compatible `/audio/speech` hard input ceiling OpenRouter proxies). Reference the env-var/knob catalog conventions already in the PRD (add `AURA_TTS_MAX_CHARS` to the env index if one exists).
  </action>
  <acceptance_criteria>
    - `grep -q "WEBVOICE-01" prd.md` AND `grep -q "WEBVOICE-04" prd.md` succeed.
    - prd.md contains the literal strings `/api/tts`, `/api/stt`, `/api/voice/capabilities`, `AURA_TTS_MAX_CHARS`.
    - prd.md contains `adapters.speech` and `adapters.dictation` and `onSpeech` (the corrected insertion path) in the WEBVOICE subsection.
    - prd.md contains `audio/mpeg` (mp3-for-web) and mentions opus for Telegram in the same subsection.
    - prd.md contains a reference to the transcribe-and-discard / no-persist STT behavior and the ephemeral (no-persistence) voice-mode toggle.
    - prd.md's WEBVOICE subsection explicitly documents the voice-mode toggle as `session-scoped` for 37C AND records the persisted per-conversation voice-mode preference as deferred (the "per-conversation" wording is reconciled) — `grep -q "session-scoped" prd.md` succeeds and the subsection names the deferred per-conversation preference.
    - `git diff --name-only` shows ONLY `prd.md` changed (no Go, no web/, no test file).
  </acceptance_criteria>
  <verify>
    <automated>grep -q "WEBVOICE-01" prd.md && grep -q "WEBVOICE-04" prd.md && grep -q "/api/tts" prd.md && grep -q "/api/stt" prd.md && grep -q "/api/voice/capabilities" prd.md && grep -q "AURA_TTS_MAX_CHARS" prd.md && grep -q "adapters.dictation" prd.md && grep -q "audio/mpeg" prd.md && grep -q "session-scoped" prd.md && test "$(git diff --name-only | tr -d ' ')" = "prd.md" && echo PRD_AMENDMENT_OK</automated>
  </verify>
  <done>prd.md documents the WEBVOICE-01..04 group, the three voice routes, the two assistant-ui adapter seams (with the onSpeech correction), mp3-for-web vs opus-for-Telegram, transcribe-and-discard STT, the ephemeral voice-mode toggle with the per-conversation-vs-ephemeral scope reconciliation (persisted per-conv DEFERRED), and the AURA_TTS_MAX_CHARS knob; only prd.md changed.</done>
</task>

</tasks>

<verification>
- `grep -q "WEBVOICE-01" prd.md` and `grep -q "AURA_TTS_MAX_CHARS" prd.md` and `grep -q "session-scoped" prd.md` succeed.
- The PRD-amendment is a standalone commit landing BEFORE any 37C code commit (D-14). Every code plan (37C-02..06) declares `depends_on` this plan.
</verification>

<success_criteria>
- prd.md records the WEBVOICE-01..04 group, the three routes, the two adapter seams, mp3-vs-opus, transcribe-and-discard STT, the ephemeral voice-mode toggle (with the per-conversation wording reconciled to session-scoped + persisted-per-conv deferred), and AURA_TTS_MAX_CHARS.
- Docs-only: no Go source, web source, package.json, or test file touched.
</success_criteria>

<output>
Create `.planning/phases/37C-web-voice-lane-inserted/37C-01-SUMMARY.md` when done.
</output>
