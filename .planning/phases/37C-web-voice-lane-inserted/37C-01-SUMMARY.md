---
phase: 37C-web-voice-lane-inserted
plan: 01
subsystem: docs/prd
tags: [prd-amendment, pre-code-gate, web-voice, WEBVOICE, docs-only]
type: execute
wave: 1
autonomous: true
dependency_graph:
  requires: []
  provides:
    - "prd.md Amendment #79 — the architectural record every 37C code plan (02..06) depends_on"
  affects:
    - "37C-02..06 (all gated behind this PRD-first commit, D-14)"
tech_stack:
  added: []
  patterns:
    - "PRD-amendment blockquote (mirrors WEBART Amendment #78) — git-log ordering is the gate"
key_files:
  created:
    - ".planning/phases/37C-web-voice-lane-inserted/37C-01-SUMMARY.md"
  modified:
    - "prd.md (Amendment #79 — Web Voice Lane, +19 lines, commit a02332a36)"
    - ".planning/STATE.md (tracking)"
    - ".planning/ROADMAP.md (37C-01 checkbox)"
decisions:
  - "Amendment #79 corrects CONTEXT D-02: multimodal.TTSClient.Synthesize has NO per-call format → mp3-for-web = a dedicated web TTSClient (TTSConfig.Format=\"mp3\"); Telegram opus untouched"
  - "Amendment #79 corrects CONTEXT D-09: dictation transcript insertion is native via onSpeech(isFinal:true), NOT onSpeechEnd (core ignores its payload)"
  - "Scope reconciliation: ROADMAP SC#1 / WEBVOICE-01 'per-conversation voice mode' delivered as an EPHEMERAL session-scoped toggle; persisted per-conversation preference (conversations.voice_mode) DEFERRED"
  - "AURA_TTS_MAX_CHARS default 4096 (OpenAI-compatible /audio/speech ceiling OpenRouter proxies) — documented here, wired in 37C-02"
metrics:
  tasks_completed: 1
  duration: "~25 min (docs-only)"
  completed: "2026-07-09"
  files_changed: 1
  commit: "a02332a36"
requirements_touched: [WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04]
requirements_completed: []
---

# Phase 37C Plan 01: Web Voice Lane PRD-Amendment Pre-Code Gate Summary

**One-liner:** Landed prd.md Amendment #79 documenting the web voice surface (WEBVOICE-01..04, the three `/api/tts` · `/api/stt` · `/api/voice/capabilities` routes, the two assistant-ui `adapters.speech`/`adapters.dictation` seams, mp3-for-web vs opus-for-Telegram, and the net-new `AURA_TTS_MAX_CHARS` knob) as the mandatory PRD-first (D-14) commit that gates every 37C code plan.

## What Was Built

A single docs-only task: a new **"Web Voice Lane (Voce Web) — 37C"** amendment (numbered **#79**) appended to `prd.md` immediately after the WEBART Amendment #78 block (near line 2948), mirroring its blockquote/heading/format conventions. The amendment records, at the PRD level:

1. **WEBVOICE-01..04 requirement group** — transcribed faithfully from `REQUIREMENTS.md:78-81`.
2. **Three authenticated, identity-scoped web voice routes** (`internal/agui`, mirroring `registerAssetRoutes` + SELF-scoped `GET /api/me`):
   - `POST /api/tts` over `multimodal.TTSClient.Synthesize` → `audio/mpeg` (mp3), soft `AURA_TTS_MAX_CHARS` cap + `X-Aura-TTS-Truncated` signal, 503 when unconfigured.
   - `POST /api/stt` over `multimodal.STTClient.Transcribe` — **transcribe-and-discard** (NO `assets.Asset` / Garage object / DB row / async `ProcessAsset` poll); container MIME mapped via the exported `assets.AudioFormat` with `;codecs=` stripped; 503 when unconfigured.
   - `GET /api/voice/capabilities` — SELF-scoped, returns `{tts,stt}` **200 even when unconfigured (never 503)** so the SPA gates controls without a first-click flash.
   - `SetVoice` composition-root setter (mirrors `SetSettingsStore`), fed from `config.Config` like `serve_channels.go`; `RequireAuth` via whole-mux wrap, the two POSTs additionally `agent.run`-capability-gated (cost-bearing).
3. **The assistant-ui adapter spine** — `adapters.speech` (`SpeechSynthesisAdapter`, fetch→blob→`<audio>`, blob cached per message) + `adapters.dictation` (`DictationAdapter`, `MediaRecorder`→`POST /api/stt`) attached directly on `useExternalStoreRuntime`; **transcript insertion native via `onSpeech({transcript,isFinal:true})`, NOT `onSpeechEnd`**; `undefined` adapter = native degrade; speaker control = `ActionBarPrimitive.Speak`/`StopSpeaking`; attachment-record fallback (`uploads.addFiles`) KEPT for `!stt`/error.
4. **mp3-for-web vs opus-for-Telegram** — `Synthesize` has no per-call format, so mp3-for-web = a dedicated web `TTSClient` with `TTSConfig.Format="mp3"`; Telegram's opus client untouched.
5. **Ephemeral session-scoped voice-mode toggle** + explicit reconciliation of the "per-conversation voice mode" wording (delivered session-scoped; `VoiceModePref` stays a `false` stub; persisted per-conversation preference DEFERRED) + `ShouldSpeak(voiceMode || turnWasDictated)` parity.
6. **Net-new `AURA_TTS_MAX_CHARS` knob** — default 4096.
7. **Scope guard** — realtime full-duplex voice, persisted per-conv voice-mode, browser Web Speech adapters, server-side TTS caching/prefetch, and streaming TTS playback recorded out-of-scope.

## Verification

The plan's `<automated>` verify block passed, printing **`PRD_AMENDMENT_OK`**:
- All 9 required grep tokens present: `WEBVOICE-01`, `WEBVOICE-04`, `/api/tts`, `/api/stt`, `/api/voice/capabilities`, `AURA_TTS_MAX_CHARS`, `adapters.dictation`, `audio/mpeg`, `session-scoped`.
- `git diff --name-only` == `prd.md` only (no Go, no web/, no test file) — confirmed after neutralizing two unrelated pre-existing working-tree mods (see Deviations).
- Additional acceptance tokens confirmed present: `WEBVOICE-02/03`, `adapters.speech`, `onSpeech(`, `onSpeechEnd`, `opus`, `transcribe-and-discard`, `VoiceModePref`, `conversations.voice_mode`, `TTSConfig.Format`, `ShouldSpeak`, `uploads.addFiles`, `X-Aura-TTS-Truncated`, `4096`.
- Pre-commit hooks green (vet 5.3s + whole-tree file-size 74.7s), no `--no-verify`; commit `a02332a36` = 1 file / +19 / 0 deletions.

## Deviations from Plan

**None to the plan's task** — the amendment was written exactly as specified, including the two RESEARCH corrections (no per-call TTS format → dedicated mp3 web client; `onSpeech` not `onSpeechEnd`) and the per-conversation↔session-scoped reconciliation. All four prohibitions were respected: no Go/web/test file touched; no per-call TTS Format recorded; `onSpeech(isFinal:true)` recorded as the insertion path; no new persistence recorded (persisted per-conv voice-mode = DEFERRED).

### Working-tree hygiene (execution-mechanics, not a plan deviation)

Two unrelated files were dirty in the working tree at start and would have broken the plan's strict `git diff --name-only == prd.md` verify:
- `.planning/graphs/.last-build-status.json` — a generated graph-build-status artifact (timestamp + HEAD-hash + exit code). Restored to HEAD via a sanctioned specific-file `git checkout --` (regenerated on the next graph build; no work lost).
- `.planning/STATE.md` — carried the orchestrator's "37C executing" update. Staged (not reverted) to preserve that content and remove it from the unstaged diff; the prd.md task commit used an explicit pathspec (`git commit prd.md`) so only prd.md landed in `a02332a36`; STATE.md was then further updated and committed in the tracking commit.

## Requirements

`WEBVOICE-01..04` are **phase-spanning** — this plan only DOCUMENTS them; they are delivered by the code plans 37C-02..06. They remain `[ ]` and `requirements mark-complete` was intentionally NOT run (matching the 37B plan precedent).

## Known Stubs

None introduced. (The amendment *records* that Telegram's `VoiceModePref` remains a pre-existing `false` stub and that a persisted per-conversation voice-mode store is DEFERRED — this is documented intent, not a new stub created by this plan.)

## Next

Wave 2 — **37C-02** (backend foundation: the `AURA_TTS_MAX_CHARS` knob default 4096 + the exported `;codecs=`-safe `assets.AudioFormat`), which `depends_on` this commit.

## Self-Check: PASSED

- FOUND: `.planning/phases/37C-web-voice-lane-inserted/37C-01-SUMMARY.md`
- FOUND: `prd.md` with `Amendment #79` present in commit `a02332a36`
- FOUND: commit `a02332a36` (prd.md amendment, 1 file / +19 / 0 deletions)
- FOUND: commit `dc2d7d696` (SUMMARY + STATE + ROADMAP tracking)
- Verify token `PRD_AMENDMENT_OK` printed; all 9 required grep tokens present; `git diff --name-only` == `prd.md` only at task-commit time.
- No unintended deletions; no Go/web/test file touched.
