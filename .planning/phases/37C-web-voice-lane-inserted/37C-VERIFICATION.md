---
phase: 37C-web-voice-lane-inserted
verified: 2026-07-09T18:48:44Z
status: human_needed
score: 4/4 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: "Initial goal-backward verification — no prior VERIFICATION.md existed (phase was closed without one)."
human_verification:
  - test: "Live container: click a message speaker control; confirm the mp3 decodes and plays audibly and intelligibly in-browser (Chrome + Safari/iOS if available)."
    expected: "Assistant reply is spoken aloud, clearly and intelligibly."
    why_human: "Real audio decode/playback is a perceptual judgment no automated assert covers; the e2e stubs the Audio element for deterministic headless state. Tracked as Manual-Only in 37C-VALIDATION.md."
  - test: "Live container: dictate a spoken sentence into the Composer mic; confirm the inserted transcript is accurate and editable before send."
    expected: "The spoken words appear as an accurate, editable transcript in the input box; Send works."
    why_human: "Real STT accuracy against a live mic is perceptual; the e2e uses a fake media device (records silence) with a route-mocked transcript. Tracked as Manual-Only in 37C-VALIDATION.md."
---

# Phase 37C: Web Voice Lane (INSERTED) — Verification Report

**Phase Goal:** Cockpit-web voice parity with Telegram — (a) voice output (per-message speaker + auto-speak "voice mode"); (b) voice input (Composer Mic → in-place editable dictation). Local↔cloud selectable over the shipped `multimodal.TTSClient`/`STTClient`, graceful degrade when unconfigured.
**Verified:** 2026-07-09T18:48:44Z
**Status:** human_needed
**Re-verification:** No — initial verification (phase closed 2026-07-09 with all 6 plans done and WEBVOICE-01..04 `[x]`, but no VERIFICATION.md was produced).

## Goal Achievement

Every mechanical/code truth was independently re-derived from the codebase (not the SUMMARY prose) and confirmed with runnable tests on this Windows host. The full-matrix Go coverage, `-race`, and live-container Playwright tiers — which require the WSL/live stack — are confirmed green by CI (operator-confirmed) and documented in 37C-06-SUMMARY.md. Two perceptual audio-quality items remain genuinely human — hence `human_needed`, not `passed`.

### Observable Truths

| # | Truth (Success Criterion) | Status | Evidence |
|---|---------------------------|--------|----------|
| 1 | **WEBVOICE-01** — Each assistant message exposes a speaker control synthesizing its text via authenticated `POST /api/tts` (identity-scoped) over `multimodal.TTSClient`, played by in-page `<audio>`; "voice mode" enables auto-speak (parity `ShouldSpeak`). | ✓ VERIFIED | `internal/agui/voice_api.go` `handleTTS` returns `audio/mpeg` over the `ttsSynthesizer` seam (satisfied by `*multimodal.TTSClient`, mp3 web client built in `serve_voice.go:buildWebTTSClient`); 401 no-principal, 503 nil-client, 400 empty, `X-Aura-TTS-Truncated` cap header. Speaker control = `AssistantSpeakerControl` (`ExternalStoreChat_messages.tsx:201/236`) using `ActionBarPrimitive.Speak`/`StopSpeaking`, caps.tts-gated. `speechAdapter.ts` POSTs `/api/tts` → blob → `new Audio(objectURL)` with per-text cache + `dispose()` revoke. Auto-speak: `shouldSpeak(voiceMode\|\|turnWasDictated)` (direct port of `telegram.ShouldSpeak`), `useAutoSpeak.ts` calls `aui.thread().message({id}).speak()`, `VoiceModeToggle` mounted in `AppShell.tsx:514`. Tests: 18 Go `TestTTS*`/`TestVoiceCapabilities*`/`TestCapText` green; 48 web voice-unit + 15 speaker/runtime green. |
| 2 | **WEBVOICE-02** — Composer Mic becomes dictation: record → transcribe via STT → insert editable transcript; on failure falls back to today's attachment behavior. | ✓ VERIFIED | `handleSTT` (`voice_api.go:142`) reads multipart `audio`, maps MIME via `assets.AudioFormat`, transcribes over `sttTranscriber`, returns `{"text":...}` — **transcribe-and-discard**: no asset service, no Garage, no DB row (confirmed by reading the handler; `TestSTT_*` incl. empty-transcript → clean 200). `dictationAdapter.ts`: `getUserMedia` → `MediaRecorder` → on-stop POST to `/api/stt` → inserts via `onSpeech({transcript,isFinal:true})`. `Composer.tsx` is dictation-primary when `caps.stt`; `beginDictation` degrades to the KEPT `startRecording→uploads.addFiles('voice-note.webm')` attachment path on failure or `!caps.stt`. Composer test 11/11 green (31 dictation/attachment/caps assertions). |
| 3 | **WEBVOICE-03** — Selectable local↔cloud; default local aura-tts/aura-stt sidecars, switch to OpenRouter when `AURA_TTS_MODEL`/`AURA_STT_CLOUD_MODEL` set; graceful degrade (speaker hidden / mic in attachment mode) when neither, no errors. | ✓ VERIFIED | `buildWebTTSClient`/`buildWebSTTClient` (`serve_voice.go`) default `LocalBaseURL=cfg.TTSBaseURL`/`cfg.STTBaseURL`, cloud override on `CloudModel`; nil only when NEITHER set — **this is the 37C-06 operator-directed fix superseding cloud-only D-12**, present in code. Degrade: `handleVoiceCapabilities` ALWAYS 200 `{tts,stt}` (never 503); `useVoiceCapabilities` keeps `{false,false}` on any error; `useVoiceRuntime` omits adapters when caps false (native degrade); speaker hidden on `!caps.tts`, mic stays attachment on `!caps.stt`. Tests: `TestBuildWeb{TTS,STT}Client_LocalRoute`, `TestBuildWebSTTClient_Selectable`, `TestWireVoiceProviders_Branches` (6 branches: tts/stt/both × cloud/local), `TestWireVoiceProviders_Degraded` — all green. |
| 4 | **WEBVOICE-04** — No regression of the audio-attachment path; `RequireAuth` on TTS endpoint; React unit tests (speaker + dictation) + e2e; web + owned-surface Go coverage ≥85%. | ✓ VERIFIED (coverage/e2e via CI) | Attachment path kept intact in `Composer.tsx` (`startRecording→uploads.addFiles`, Paperclip independent). `RequireAuth`: whole origin wrapped `agui.RequireAuth(mux, auth)` (`serve_webui.go:531`); the two POSTs additionally `RequireCapability(agentRunCapability)`, capabilities read is RequireAuth-only (`serve_webui_voice.go:35`). Unit tests: speaker (`ExternalStoreChat_messages.speaker.test.tsx`) + dictation (`dictationAdapter.test.ts`, Composer) present + green. E2E: `web/e2e/voice.spec.ts` (4 tests: mount/speaker/dictation/degrade × chromium+mobile-chrome). **Web coverage independently reproduced on this host: 92.75% stmts / 86.7% branch / 93% funcs / 94.34% lines (1212/1212 tests pass) — ≥85% floor.** Go owned-surface 85.4% on the full `db_integration neo4j_integration` matrix + `-race` clean + 8/8 live e2e are CI-green (operator-confirmed; not reproducible on Windows without the live stack — sanctioned WSL/CI tier per CLAUDE.md). |

**Score:** 4/4 truths verified.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agui/voice_api.go` | 3 handlers + SetVoice narrow seams | ✓ VERIFIED | 191 LOC, substantive; `handleTTS`/`handleSTT`/`handleVoiceCapabilities` + `capText`; registered on `Server.Mux` via `registerVoiceRoutes` (`server.go:205`). |
| `cmd/aura/serve_webui_voice.go` | parent-mux mounts w/ RequireCapability/RequireAuth | ✓ VERIFIED | mounts 3 routes; wired at `serve_webui.go:503`. |
| `cmd/aura/serve_voice.go` | wireVoiceProviders + local↔cloud builders | ✓ VERIFIED | `wireVoiceProviders` called `serve.go:430`; local-default selectable builders (37C-06 fix). |
| `internal/config/config.go` | `AURA_TTS_MAX_CHARS` knob | ✓ VERIFIED | `TTSMaxChars` field + `envutil.IntDefault(...,4096)` + config_knobs registry. |
| `web/src/chat/voice/speechAdapter.ts` | custom SpeechSynthesisAdapter | ✓ VERIFIED | 136 LOC; fetch→blob→Audio, per-text cache, truncated header, dispose(). |
| `web/src/chat/voice/dictationAdapter.ts` | custom DictationAdapter | ✓ VERIFIED | 134 LOC; getUserMedia→MediaRecorder→/api/stt→onSpeech insert; error→degrade. |
| `web/src/chat/voice/{useVoiceRuntime,useVoiceCapabilities,useAutoSpeak,shouldSpeak,VoiceModeProvider,VoiceModeToggle}` | runtime wiring + gating + auto-speak | ✓ VERIFIED | all present, substantive, imported+used (see key links). |
| `web/e2e/voice.spec.ts` | live-container e2e | ✓ VERIFIED (present; CI-run) | 4 tests × 2 browsers. |
| `internal/webui/dist` | rebuilt bundle carrying voice surface | ⚠️ CI/build artifact | SUMMARY grep-confirms `/api/voice/capabilities` + `/api/tts` baked; a generated artifact — not independently re-diffed here. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| parent mux | `POST /api/tts`,`/api/stt` | `RequireCapability(agentRunCapability)` | ✓ WIRED | `serve_webui_voice.go:36-37`. |
| parent mux | `GET /api/voice/capabilities` | RequireAuth-only (inherited) | ✓ WIRED | `serve_webui_voice.go:38`. |
| whole origin | all voice routes | `agui.RequireAuth(mux, auth)` | ✓ WIRED | `serve_webui.go:531`. |
| `wireVoiceProviders` | `Server.SetVoice` | composition root | ✓ WIRED | `serve.go:430` → `serve_voice.go:37`. |
| `speechAdapter`/`dictationAdapter` | `useExternalStoreRuntime` | `adapters: voiceAdapters` | ✓ WIRED | `ExternalStoreChat.tsx:550` via `useVoiceRuntime` (caps-gated). |
| `AssistantSpeakerControl` | speech state | `ActionBarPrimitive.Speak`/`StopSpeaking` | ✓ WIRED | `ExternalStoreChat_messages.tsx:201/236-262`. |
| `useAutoSpeak` | runtime speak | `shouldSpeak` → `aui.thread().message().speak()` | ✓ WIRED | `useAutoSpeak.ts:51-52`; `AutoSpeak` mounted in runtime subtree. |
| `Composer` mic | `/api/stt` (primary) / attachment (fallback) | `caps.stt` branch | ✓ WIRED | `Composer.tsx:151-182`. |
| `useVoiceCapabilities` | `GET /api/voice/capabilities` | fetch same-origin | ✓ WIRED | `useVoiceCapabilities.ts`. |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Produces Real Data | Status |
|----------|------|--------|--------------------|--------|
| speaker control | `s.message.speech` | speechAdapter → real `POST /api/tts` → `multimodal.TTSClient.Synthesize` (local Kokoro / cloud OpenRouter) | Yes | ✓ FLOWING (e2e test #2 drives real audio/mpeg over live Kokoro per CI) |
| Composer transcript | inserted text | dictationAdapter → real `POST /api/stt` → `multimodal.STTClient.Transcribe` | Yes (mechanically; live accuracy is Manual-Only) | ✓ FLOWING |
| voice UI gating | `caps` | `GET /api/voice/capabilities` reflecting client presence | Yes | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Voice Go packages build | `go build ./internal/agui/... ./cmd/aura/...` | BUILD_OK | ✓ PASS |
| Voice Go packages vet | `go vet ./internal/agui/ ./cmd/aura/ ./internal/config/ ./internal/assets/` | clean | ✓ PASS |
| agui voice handler tests | `go test ./internal/agui/ -run 'TestTTS\|TestSTT\|TestVoice'` | ok 5.8s (18 funcs) | ✓ PASS |
| composition-root voice wiring | `go test ./cmd/aura/ -run 'Voice\|buildWeb'` | 7 funcs PASS (6 branches) | ✓ PASS |
| config knob + audio format | `go test ./internal/config/ ./internal/assets/` | ok | ✓ PASS |
| web voice unit modules | `npx vitest run src/chat/voice` | 7 files / 48 tests pass | ✓ PASS |
| web speaker/composer/runtime | `npx vitest run ExternalStoreChat_messages Composer ExternalStoreChat` | 4 files / 26 tests pass | ✓ PASS |
| **full web coverage** | `npx vitest run --coverage` | **1212/1212 pass; 92.75% stmts ≥85%** | ✓ PASS |
| Go owned-surface ≥85% full matrix | `bash scripts/coverage_gate.sh` (WSL/live stack) | 85.4% (CI-green, operator-confirmed) | ? SKIP (WSL/CI tier) |
| `-race` (agui/assets/config/cmd) | `go test -race` (WSL native CGO) | clean (CI-green) | ? SKIP (WSL/CI tier) |
| live-container `voice.spec.ts` | Playwright vs rebuilt container | 8/8 (CI-green, operator-confirmed) | ? SKIP (live-container tier) |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| WEBVOICE-01 | 37C-03/04 | speaker per message → POST /api/tts + auto-speak voice mode | ✓ SATISFIED | Truth #1 |
| WEBVOICE-02 | 37C-03/05 | Composer mic → editable dictation + attachment fallback | ✓ SATISFIED | Truth #2 |
| WEBVOICE-03 | 37C-02/03/06 | selectable local↔cloud + graceful degrade | ✓ SATISFIED | Truth #3 (incl. 37C-06 local-default fix) |
| WEBVOICE-04 | 37C-06 | no attachment regression + RequireAuth + tests/e2e + coverage ≥85% | ✓ SATISFIED | Truth #4 (web coverage reproduced 92.75%; Go/-race/e2e CI-green) |

No orphaned requirements — all four WEBVOICE IDs are claimed by phase plans and mapped to Phase 37C in REQUIREMENTS.md.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | none | — | Scanned `voice_api.go`, `serve_voice.go`, `serve_webui_voice.go`, `web/src/chat/voice/*`, `voice.spec.ts` for TODO/FIXME/XXX/HACK/placeholder/not-implemented/empty-returns — **NONE found**. No debt markers, no stubs. The e2e Audio-element stub + `/api/stt` route-mock are deterministic test seams for the perceptual surfaces (Manual-Only), not product stubs. |

### Human Verification Required

1. **Audible TTS playback intelligibility (WEBVOICE-01)**
   - **Test:** Live container — click a message speaker control (Chrome + Safari/iOS if available).
   - **Expected:** The assistant reply is spoken aloud, clearly and intelligibly (mp3 decodes and plays).
   - **Why human:** Real audio decode/playback is a perceptual judgment no automated assert covers; the e2e stubs the Audio element for deterministic headless state.

2. **Live dictation accuracy on a real mic (WEBVOICE-02)**
   - **Test:** Live container — dictate a spoken sentence into the Composer mic.
   - **Expected:** An accurate, editable transcript appears in the input box before send.
   - **Why human:** Real STT accuracy against a live mic is perceptual; the e2e uses a fake media device (records silence) with a route-mocked transcript.

Both are the exact items tracked as **Manual-Only** in `37C-VALIDATION.md` — sanctioned, expected human sign-off, not defects.

### Gaps Summary

**No gaps.** All four success criteria (WEBVOICE-01..04) are achieved in the codebase, independently re-derived from source (not SUMMARY prose): the three identity-scoped handlers are real and correctly gated (RequireAuth on the whole origin, RequireCapability on the two cost-bearing POSTs), transcribe-and-discard is confirmed by reading `handleSTT` (no asset/DB/Garage touch), the two custom assistant-ui adapters are substantive and wired into `useExternalStoreRuntime`, auto-speak genuinely calls `.speak()` via the `ShouldSpeak` port, and the attachment fallback is preserved. The 37C-06 operator-directed fix (web voice = local-default, cloud-selectable — superseding cloud-only D-12) is present in `buildWebTTSClient`/`buildWebSTTClient` and branch-tested.

Verification ran the full Go voice test suite + `go build`/`go vet` and the full web unit suite green on this Windows host, and **independently reproduced the web coverage floor (92.75% ≥85%)**. The Go owned-surface ≥85% full-matrix, `-race`, and live-container Playwright tiers require the WSL/live stack and are CI-green (operator-confirmed) — sanctioned CI/WSL deferrals per CLAUDE.md.

Status is **human_needed** (not `passed`) solely because two perceptual audio-quality judgments — audible TTS intelligibility and live-mic dictation accuracy — cannot be verified programmatically and remain for human sign-off. No item is FAILED; no code change is required to close them.

---

_Verified: 2026-07-09T18:48:44Z_
_Verifier: Claude (gsd-verifier)_
