---
phase: 37C
slug: web-voice-lane-inserted
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-09
---

# Phase 37C — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> **Source of truth for the detailed strategy:** `37C-RESEARCH.md` → `## Validation Architecture`.
> **Per-Task Verification Map materializes POST-planning** against the final plan/task set
> (mirrors 37B). This draft fills the planning-independent sections from RESEARCH; it does not
> fabricate task IDs the planner has not yet created.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Go framework** | stdlib `testing` + `net/http/httptest` + `go.uber.org/goleak`; run via `scripts/go_packages.sh` (never raw `./...`, SEC-07) |
| **Go handler harness** | `NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})` → `s.SetVoice(fakeTTS, fakeSTT, maxChars)` → identity path `withPrincipal(httptest.NewRequest(...), id)` + `s.Mux().ServeHTTP`; 401 path `RequireAuth(s.Mux(), testDeps("secret"))` + no cookie (precedent: `internal/agui/asset_download_test.go:25-129`) |
| **Web framework** | Vitest (`web/vitest.config.ts`, `npm test` = `vitest run --coverage`, ≥85% src/**) + Stryker (`web/stryker.config.json`, ≥70% killed) + Testing Library; new tests under `web/src/chat/voice/` |
| **E2E** | Playwright (`web/playwright.config.ts`, `npm run test:e2e`), live container, `web/e2e/voice.spec.ts` + `web/e2e/auth.ts`; precedent `web/e2e/artifacts.spec.ts` (WEBART-08, green vs real Authula) |
| **Quick run command** | `go test ./internal/agui/ ./internal/assets/ ./internal/config/` · `cd web && npx vitest run src/chat/voice src/chat/__tests__` |
| **Full suite command** | WSL `make quality-full` (owned-surface ≥85% gate) + `cd web && npm test` + `npm run test:e2e` |
| **Estimated runtime** | quick Go pkg ~10–20s · quick web unit ~5–15s · full web + coverage ~60s · Playwright e2e ~90s |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the task's package/test file (≤20s feedback)
- **After every plan wave:** Run the full web unit suite with coverage + the touched Go packages `-race`
- **Before `/gsd-verify-work`:** `make quality-full` green (owned-surface ≥85%) + `cd web && npm test` (≥85%) + Stryker ≥70% on the two adapters + Playwright `voice.spec.ts` green + `internal/webui/dist` rebuilt
- **Max feedback latency:** ~20 seconds (quick unit); ~90 seconds (full + e2e at wave boundaries)

---

## Per-Task Verification Map

> **Materializes post-planning.** The rows below are the requirement → test-class map lifted from
> `37C-RESEARCH.md § Validation Architecture → Phase Requirements → Test Map`. After planning, replace
> with one row per automated task (with concrete Task IDs, plan, wave, and `<automated>` command),
> as 37B-VALIDATION.md did against its final task set.

| Requirement | Behavior (proof) | Test class | Automated Command | File Exists |
|-------------|------------------|-----------|-------------------|-------------|
| WEBVOICE-01 | `POST /api/tts` → `audio/mpeg`; identity-scoped; 401 w/o cookie; 503 when `s.tts==nil`; char-cap prefix + "too long" signal beyond 4096; synthesize-and-discard (no asset/DB row) | Go unit | `go test ./internal/agui/ -run TestTTS` | ❌ W0 (`voice_api_test.go`) |
| WEBVOICE-01 | Speaker control idle→loading→playing→stop→error; `s.message.speech` toggle; auto-speak on `voiceMode \|\| turnWasDictated` | React unit | `cd web && npx vitest run src/chat/voice/speechAdapter` | ❌ W0 |
| WEBVOICE-02 | `POST /api/stt` transcribes, creates NO asset/Garage object/DB row; identity-scoped; 401; 503 when `s.stt==nil`; container→format map (webm;codecs / mp4;codecs / m4a) | Go unit | `go test ./internal/agui/ -run TestSTT` + `go test ./internal/assets/ -run TestAudioFormat` | ❌ W0 |
| WEBVOICE-02 | Dictation adapter: mocked `MediaRecorder` + mocked `/api/stt` → transcript inserted via `onSpeech(isFinal:true)` into composer, editable; `onSpeechEnd` cleans up | React unit | `cd web && npx vitest run src/chat/voice/dictationAdapter` | ❌ W0 |
| WEBVOICE-03 | `GET /api/voice/capabilities` → `{tts,stt}` (200 even unconfigured, both false); `!tts`⇒Speak hidden, `!stt`⇒mic stays attachment | Go unit + React unit | `go test ./internal/agui/ -run TestVoiceCapabilities` + `cd web && npx vitest run src/chat/voice/useVoiceCapabilities` | ❌ W0 |
| WEBVOICE-04 | No regression to attachment-record fallback (mic → `uploads.addFiles` when `!stt`/on error); `RequireAuth` present; speaker + dictation live e2e | React unit + Playwright | `cd web && npx vitest run src/chat/Composer` + `npx playwright test voice.spec.ts` | ⚠️ `Composer.test.tsx` exists; `voice.spec.ts` ❌ W0 |

*Status legend: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · ❌ W0 = file created during execution (Wave 0 scaffolding).*

---

## Wave 0 Requirements

- [ ] `internal/agui/voice_api_test.go` — three-handler suite (auth / identity / degrade / content-type / char-cap / no-persist)
- [ ] `internal/assets/audio_processor_test.go` — `TestAudioFormat` incl. `;codecs=` strip cases (`audio/webm;codecs=opus`→`webm`, `audio/mp4;codecs=mp4a.40.2`→`m4a`, unknown→`ogg`)
- [ ] `web/src/chat/voice/*.test.ts(x)` — speechAdapter, dictationAdapter, useVoiceCapabilities
- [ ] Web test infra: a shared `MediaRecorder` + `getUserMedia` + `Audio` mock helper (**none exists today** — `Composer.test.tsx` mocks only at the `uploads.addFiles` level)
- [ ] `web/e2e/voice.spec.ts` — speaker + dictation + degrade against the live container
- [ ] Fakes: `fakeTTS` / `fakeSTT` satisfying the new `ttsSynthesizer` / `sttTranscriber` seams + a recording `fakeAssetService` (assert-untouched, the no-persist proof)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| PRD-amendment pre-code gate (D-14) | WEBVOICE-01..04 | ROADMAP marks 37C "PRD-first: richiede PRD-amendment"; the amendment (WEBVOICE-01..04, the 3 routes, the 2 adapters, mp3-for-web vs opus, `AURA_TTS_MAX_CHARS`) is a required commit BEFORE any implementation code | Confirm a PRD-amendment commit lands first, covering the web voice surface; then implementation commits follow |
| Audible TTS playback + intelligibility | WEBVOICE-01 | Real audio decode/playback is a perceptual judgment no automated assert covers | Live container: click a message speaker; confirm mp3 decodes and plays in-browser (Chrome + Safari/iOS if available) |
| Live dictation accuracy on real mic | WEBVOICE-02 | Real STT quality against a live mic is perceptual | Live container: dictate a sentence; confirm the transcript is accurate + editable before send |

---

## Validation Sign-Off

- [ ] Per-Task Verification Map materialized against the final plan/task set (concrete Task IDs + `<automated>` commands)
- [ ] All automated tasks have a concrete `<automated>` verify command
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (test/source scaffolding created during execution)
- [ ] No watch-mode flags (all commands are `go test` / `vitest run` / `playwright test` one-shot)
- [ ] Feedback latency < 20s (quick unit) / < 90s (full + e2e)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** draft — planning-independent sections materialized from `37C-RESEARCH.md § Validation Architecture`. Per-Task Map + sign-off to be finalized post-planning.
