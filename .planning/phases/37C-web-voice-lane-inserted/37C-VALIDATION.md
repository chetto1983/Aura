---
phase: 37C
slug: web-voice-lane-inserted
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-09
---

# Phase 37C — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> **Source of truth for the detailed strategy:** `37C-RESEARCH.md` → `## Validation Architecture`.
> **Per-Task Verification Map materialized post-planning** against the final 6-plan / 13-task set
> (mirrors 37B). Automated commands below are lifted verbatim from each plan's `<verify><automated>`
> block — the executor runs those blocks; this map is the roll-up.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Go framework** | stdlib `testing` + `net/http/httptest` + `go.uber.org/goleak`; run via `scripts/go_packages.sh` (never raw `./...`, SEC-07) |
| **Go handler harness** | `NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})` → `s.SetVoice(fakeTTS, fakeSTT, maxChars)` → identity path `withPrincipal(httptest.NewRequest(...), id)` + `s.Mux().ServeHTTP`; 401 path `RequireAuth(s.Mux(), testDeps("secret"))` + no cookie (precedent: `internal/agui/asset_download_test.go:25-129`) |
| **Web framework** | Vitest (`web/vitest.config.ts`, `npm test` = `vitest run --coverage`, ≥85% src/**) + Stryker (`web/stryker.config.json`, ≥70% killed) + Testing Library; new tests under `web/src/chat/voice/` |
| **E2E** | Playwright (`web/playwright.config.ts`, `./node_modules/.bin/playwright test`), live container, `web/e2e/voice.spec.ts` + `web/e2e/auth.ts`; precedent `web/e2e/artifacts.spec.ts` (WEBART-08, green vs real Authula) |
| **Quick run command** | `go test ./internal/agui/ ./internal/assets/ ./internal/config/` · `cd web && npx vitest run src/chat/voice src/chat/__tests__` |
| **Full suite command** | WSL `make quality-full` / `bash scripts/coverage_gate.sh` (owned-surface ≥85% gate) + `cd web && npm test` + `npm run test:e2e` |
| **Estimated runtime** | quick Go pkg ~10–20s · quick web unit ~5–15s · full web + coverage ~60s · Playwright e2e ~90s |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the task's package/test file (≤20s feedback)
- **After every plan wave:** Run the full web unit suite with coverage + the touched Go packages `-race`
- **Before `/gsd-verify-work`:** `make quality-full` / `coverage_gate.sh` green (owned-surface ≥85%) + `cd web && npm test` (≥85%) + Stryker ≥70% on the two adapters + Playwright `voice.spec.ts` green + `internal/webui/dist` rebuilt
- **Max feedback latency:** ~20 seconds (quick unit); ~90 seconds (full + e2e at wave boundaries)

---

## Per-Task Verification Map

*Materialized against the final 6-plan / 13-task set (commit `9b3f19642`). Requirement column = the
plan's `requirements` frontmatter. `Automated Command` = the task's `<verify><automated>` (abbreviated).*

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command (success token) | File Exists |
|---------|------|------|-------------|-----------|-----------------------------------|-------------|
| 37C-01-01 | 01 | 1 | WEBVOICE-01..04 | docs (PRD-amendment, BLOCKING gate) | `grep -q "WEBVOICE-01"…"AURA_TTS_MAX_CHARS"…"session-scoped" prd.md && only-prd-changed → PRD_AMENDMENT_OK` | ✅ `prd.md` |
| 37C-02-01 | 02 | 2 | WEBVOICE-01,02 | Go unit (tdd) | `go test ./internal/config/ -run 'TTSMaxChars\|Load\|Knob' && go vet → CONFIG_KNOB_OK` | ❌ W0 |
| 37C-02-02 | 02 | 2 | WEBVOICE-01,02 | Go unit (tdd) | `go test ./internal/assets/ -run TestAudioFormat && go build → AUDIOFORMAT_OK` | ❌ W0 |
| 37C-03-01 | 03 | 3 | WEBVOICE-01..04 | Go unit (tdd) | `go test ./internal/agui/ -run 'TestTTS\|TestSTT\|TestVoiceCapabilities' && go vet → VOICE_API_OK` | ❌ W0 (`voice_api_test.go`) |
| 37C-03-02 | 03 | 3 | WEBVOICE-01..04 | Go unit (tdd) | `go test ./cmd/aura/ -run 'WireVoice\|Voice' && go build && go vet → VOICE_WIRING_OK` | ❌ W0 |
| 37C-04-01 | 04 | 4 | WEBVOICE-01,03 | React unit (tdd) | `npx vitest run src/chat/voice/{speechAdapter,useVoiceCapabilities,shouldSpeak} && tsc → VOICE_MODULE_OK` | ❌ W0 |
| 37C-04-02 | 04 | 4 | WEBVOICE-01,03 | React unit (tdd) | `npx vitest run src/chat/voice/{VoiceModeProvider,useAutoSpeak} src/AppShell.voice && tsc → VOICEMODE_OK` | ❌ W0 |
| 37C-04-03 | 04 | 4 | WEBVOICE-01,03 | React unit (tdd) | `npx vitest run src/chat/ExternalStoreChat_messages.speaker && tsc → SPEAKER_CONTROL_OK` | ❌ W0 |
| 37C-05-01 | 05 | 5 | WEBVOICE-02,04 | React unit (tdd) | `npx vitest run src/chat/voice/dictationAdapter && tsc → DICTATION_ADAPTER_OK` | ❌ W0 |
| 37C-05-02 | 05 | 5 | WEBVOICE-02,04 | React unit (tdd) | `npx vitest run src/chat/Composer && tsc → COMPOSER_DICTATION_OK` | ⚠️ `Composer.test.tsx` exists |
| 37C-05-03 | 05 | 5 | WEBVOICE-02,04 | React unit (tdd) | `npx vitest run src/chat/ExternalStoreChat.voice && tsc && ExternalStoreChat.tsx ≤600 LOC → RUNTIME_WIRING_OK` | ❌ W0 |
| 37C-06-01 | 06 | 6 | WEBVOICE-01..04 | E2E (Playwright, live container) | `./node_modules/.bin/playwright test voice.spec.ts --reporter=line → VOICE_E2E_OK` | ❌ W0 (`voice.spec.ts`) |
| 37C-06-02 | 06 | 6 | WEBVOICE-01..04 | coverage (web + Go) | `cd web && npm test → COVERAGE_GATE_WEB_OK` **+** `bash scripts/coverage_gate.sh → COVERAGE_GATE_GO_OK` | ❌ W0 |

*Status legend: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · ❌ W0 = file created during execution (Wave 0 scaffolding).*

---

## Wave 0 Requirements

- [ ] `internal/agui/voice_api_test.go` — three-handler suite (auth / identity / degrade / content-type / char-cap / no-persist / STT-empty-transcript)
- [ ] `internal/assets/audio_processor_test.go` — `TestAudioFormat` incl. `;codecs=` strip cases (`audio/webm;codecs=opus`→`webm`, `audio/mp4;codecs=mp4a.40.2`→`m4a`, unknown→`ogg`)
- [ ] `web/src/chat/voice/*.test.ts(x)` — speechAdapter (incl. `truncated` flag + `dispose()`→revoke + concurrent-Speak-cancels), dictationAdapter (incl. empty-transcript), useVoiceCapabilities, shouldSpeak, VoiceModeProvider/useAutoSpeak
- [ ] Web test infra: a shared `MediaRecorder` + `getUserMedia` + `Audio` mock helper (**none exists today** — `Composer.test.tsx` mocks only at the `uploads.addFiles` level)
- [ ] `web/e2e/voice.spec.ts` — speaker + dictation + degrade against the live container
- [ ] Fakes: `fakeTTS` / `fakeSTT` satisfying the new `ttsSynthesizer` / `sttTranscriber` seams + a recording `fakeAssetService` (assert-untouched, the no-persist proof)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| PRD-amendment pre-code gate (D-14, task 37C-01-01) | WEBVOICE-01..04 | ROADMAP marks 37C "PRD-first: richiede PRD-amendment"; the amendment (WEBVOICE-01..04, the 3 routes, the 2 adapters, mp3-for-web vs opus, `AURA_TTS_MAX_CHARS`, session-scoped voice-mode) MUST land BEFORE any implementation code — its `<automated>` asserts only `prd.md` changed | Confirm the PRD-amendment commit lands first (37C-01, Wave 1); implementation commits follow only after |
| Audible TTS playback + intelligibility | WEBVOICE-01 | Real audio decode/playback is a perceptual judgment no automated assert covers | Live container: click a message speaker; confirm mp3 decodes and plays in-browser (Chrome + Safari/iOS if available) |
| Live dictation accuracy on real mic | WEBVOICE-02 | Real STT quality against a live mic is perceptual | Live container: dictate a sentence; confirm the transcript is accurate + editable before send |

---

## Validation Sign-Off

- [x] Per-Task Verification Map materialized against the final 6-plan / 13-task set (concrete Task IDs + `<automated>` commands)
- [x] All 13 automated tasks have a concrete `<automated>` verify command; the 3 perceptual items are tracked under Manual-Only
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (every task carries one)
- [ ] Wave 0 covers all MISSING references (test/source scaffolding created during execution)
- [x] No watch-mode flags (all commands are `go test` / `vitest run` / `playwright test` one-shot)
- [x] Feedback latency < 20s (quick unit) / < 90s (full + e2e)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ready — materialized against the final 6-plan / 13-task set (commit `9b3f19642`). Wave 0 scaffolding pending execution.
