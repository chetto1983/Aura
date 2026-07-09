---
phase: 37C-web-voice-lane-inserted
plan: 06
type: execute
wave: 6
depends_on: ["37C-03", "37C-04", "37C-05"]
files_modified:
  - web/e2e/voice.spec.ts
  - internal/webui/dist
autonomous: true
requirements: [WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04]
must_haves:
  truths:
    - "a Playwright voice.spec.ts runs against the rebuilt live container: the assistant speaker triggers a POST /api/tts returning audio/mpeg and an <audio>/playback state"
    - "dictation with fake media (or a route-mocked /api/stt) inserts an editable transcript into the composer input, then Send works"
    - "with AURA_TTS_MODEL/AURA_STT_CLOUD_MODEL unset the speaker control is absent, the mic stays in attachment mode, GET /api/voice/capabilities returns {false,false}, and there are no console errors"
    - "web vitest coverage ≥85% and Stryker ≥70% killed on the two adapters; Go owned-surface ≥85% on the full db_integration+neo4j_integration matrix"
    - "internal/webui/dist is rebuilt from the current web/ sources before the e2e/coverage gate (the baked bundle carries the voice surface)"
  artifacts:
    - path: "web/e2e/voice.spec.ts"
      provides: "live-container e2e: speaker + dictation + degrade"
      contains: "/api/tts"
  key_links:
    - from: "web/e2e/voice.spec.ts"
      to: "web/e2e/auth.ts"
      via: "real Authula login before the voice probes"
      pattern: "auth"
  prohibitions:
    - "MUST NOT assert a passing gate off a skipped tier — the e2e must actually run against a rebuilt container (sub-second runtime is a skip tell); Go coverage runs the live db_integration+neo4j_integration matrix, not a compile-check"
    - "MUST NOT introduce a docker_integration-only Go surface (it contributes ZERO to the coverage gate) — the voice Go files are daemon-free unit-covered"
    - "MUST NOT ship without rebuilding internal/webui/dist — a stale bundle serves a cockpit without the voice surface"
---

<objective>
Terminal acceptance gate for Phase 37C: a live-container Playwright `voice.spec.ts` (speaker + dictation + degrade), the full coverage/mutation gate (web vitest ≥85% + Stryker ≥70% on the two adapters + Go owned-surface ≥85% on the full integration matrix), and an `internal/webui/dist` rebuild so the baked cockpit bundle carries the voice surface. Mirrors the 37B-08 terminal pattern.

Purpose: Prove WEBVOICE-01..04 end to end against a real rebuilt container and lock the coverage floors (WEBVOICE-04).
Output: `web/e2e/voice.spec.ts` green vs the live container + a rebuilt `internal/webui/dist` + green coverage/mutation gates (web AND Go).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md
@.planning/phases/37C-web-voice-lane-inserted/37C-VALIDATION.md
@web/e2e/artifacts.spec.ts
</context>

<artifacts_produced>
This plan produces:
- **`web/e2e/voice.spec.ts`** — the live-container e2e: (1) speaker → `POST /api/tts` (route interception) returns `audio/mpeg` + an `<audio>`/playback state; (2) dictation with fake media / route-mocked `/api/stt` → transcript in the composer input, editable, then Send; (3) degrade — TTS/STT unset → speaker absent, mic in attachment mode, `GET /api/voice/capabilities` → `{false,false}`, no console errors.
- **Rebuilt `internal/webui/dist`** — the cockpit bundle including the 37C voice surface.
</artifacts_produced>

<tasks>

<task type="auto">
  <name>Task 1: Rebuild internal/webui/dist + write web/e2e/voice.spec.ts against the live container</name>
  <files>internal/webui/dist, web/e2e/voice.spec.ts</files>
  <read_first>
    - web/e2e/artifacts.spec.ts + web/e2e/auth.ts — the WEBART-08 live-container harness (real Authula login, external-serve origin, route interception) to mirror for voice.spec.ts.
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Validation Architecture → E2E (Playwright, live container) — the three scenarios (speaker / dictation / degrade) + fake-media approach.
    - .planning/phases/37C-web-voice-lane-inserted/37C-VALIDATION.md — the WEBVOICE-04 e2e row + the Manual-Only perceptual checks (audible playback, dictation accuracy) that stay for /gsd-verify-work.
    - MEMORY reference "Web E2E against live container" — AURA_E2E_ORIGIN=:9080 external-serve; `docker compose build aura && up -d` FIRST (baked dist); `./node_modules/.bin/playwright` (not npx); page.evaluate fetch for authed probes.
  </read_first>
  <action>
    Rebuild the cockpit bundle so the voice surface is baked: `cd web && npm run build` (or the project's dist build) producing `internal/webui/dist`, then `docker compose build aura && docker compose up -d` so the container serves the rebuilt bundle. Create web/e2e/voice.spec.ts mirroring artifacts.spec.ts + auth.ts: after a real Authula login, (1) send a turn, click the assistant message speaker, intercept `POST /api/tts` and assert it returns `Content-Type: audio/mpeg` and the message enters a speaking/`<audio>` state; (2) enable fake media (`--use-fake-device-for-media-stream`) or route-mock `POST /api/stt` to return a fixed transcript, click the mic, assert the transcript appears in the composer input (editable), then Send; (3) a degrade case (documented run with TTS/STT unset, or a capabilities route-mock returning `{false,false}`) asserting the speaker control is absent, the mic is in attachment mode, and no console errors. Use `page.evaluate` fetch for the `/api/voice/capabilities` probe. Keep the spec real — no skip-as-green (a sub-second run is a skip tell).
  </action>
  <acceptance_criteria>
    - `grep -q "/api/tts" web/e2e/voice.spec.ts` AND `grep -q "/api/voice/capabilities" web/e2e/voice.spec.ts`.
    - `internal/webui/dist` is rebuilt from current sources (the bundle references the voice surface; not a stale artifact).
    - `cd web && ./node_modules/.bin/playwright test voice.spec.ts` passes against the rebuilt+running container (real, non-trivial runtime).
    - The three scenarios (speaker audio/mpeg, dictation transcript insert, degrade {false,false}) all assert.
  </acceptance_criteria>
  <verify>
    <automated>cd web && ./node_modules/.bin/playwright test voice.spec.ts --reporter=line && echo VOICE_E2E_OK</automated>
  </verify>
  <done>voice.spec.ts is green against a rebuilt live container across speaker + dictation + degrade; internal/webui/dist carries the voice surface.</done>
</task>

<task type="auto">
  <name>Task 2: Full coverage/mutation gate — web vitest ≥85% + Stryker ≥70% on the two adapters + Go owned-surface ≥85%</name>
  <files>web/e2e/voice.spec.ts</files>
  <read_first>
    - CLAUDE.md — the ≥85% owned-surface floor + the coverage-gate tag set (`db_integration neo4j_integration` only; no docker_integration) + the frontend gates (vitest ≥85% + Stryker ≥70%).
    - .planning/phases/37C-web-voice-lane-inserted/37C-VALIDATION.md → Coverage targets & owned-surface — the two adapters + the Go voice_api.go / AudioFormat surface must be daemon-free covered.
    - web/stryker.config.json + web/vitest.config.ts — the web coverage + mutation config (scope Stryker to src/chat/voice for the two adapters).
    - MEMORY references "make coverage .env leak breaks config tests" + "Coverage gate wipes shared PG" + "DB+knowledge integration test invocation" — the WSL invocation gotchas (unset AURA_WEB_AUTH_SECRET; GOFLAGS=-p=1; stack up).
  </read_first>
  <action>
    Run the full gates and fix any shortfall by adding daemon-free unit tests (never by weakening a gate). Web: `cd web && npm test` (vitest --coverage) must report ≥85% across the touched surface; run Stryker scoped to the two adapters (`src/chat/voice/speechAdapter.ts` + `src/chat/voice/dictationAdapter.ts`) and confirm ≥70% killed. Go: in WSL with the stack up (prepend `~/.local/bin:~/go/bin`, export the composed DSNs, unset AURA_WEB_AUTH_SECRET, GOFLAGS=-p=1), run `make quality-full` (or `bash scripts/coverage_gate.sh`) on the full `db_integration neo4j_integration` matrix and confirm owned-surface ≥85% with the new `internal/agui/voice_api.go` + `internal/assets` AudioFormat exercised by unit tests (no docker_integration-only surface). Run `-race` on the touched Go packages (`internal/agui`, `internal/assets`, `internal/config`, `cmd/aura`). Record the measured web + Go coverage numbers + the Stryker score in the SUMMARY. If any package dips below 85%, add daemon-free unit tests to lift it — do not lower a floor.
  </action>
  <acceptance_criteria>
    - `cd web && npm test` exits 0 with reported coverage ≥85% (Stmts/Branch/Funcs/Lines).
    - Stryker on the two adapters reports ≥70% killed.
    - Go: `make quality-full` (WSL, full matrix, stack up) green with owned-surface ≥85%; `internal/agui` + `internal/assets` + `internal/config` + `cmd/aura` each ≥85% and `-race` clean.
    - The measured web coverage %, Go owned-surface %, and Stryker % are recorded in 37C-06-SUMMARY.md.
  </acceptance_criteria>
  <verify>
    <automated>cd web && npm test 2>&1 | tail -20 && echo COVERAGE_GATE_WEB_OK</automated>
    <automated>bash scripts/coverage_gate.sh 2>&1 | tail -20 && echo COVERAGE_GATE_GO_OK</automated>
  </verify>
  <done>Web vitest ≥85% + Stryker ≥70% on the two adapters + Go owned-surface ≥85% on the full db_integration+neo4j_integration matrix, all measured live (both the web AND the WSL Go gate verified) and recorded; the phase closes at score >9.8 on the real speaker+dictation+degrade scenario.</done>
</task>

</tasks>

<verification>
- `cd web && ./node_modules/.bin/playwright test voice.spec.ts` green against the rebuilt live container (speaker + dictation + degrade).
- `cd web && npm test` ≥85% + Stryker ≥70% on the two adapters; WSL `make quality-full` / `bash scripts/coverage_gate.sh` owned-surface ≥85% on the `db_integration neo4j_integration` matrix; `-race` clean on the touched Go packages.
- `internal/webui/dist` rebuilt; no skip-as-green (real e2e + live integration coverage).
- Manual-Only (→ /gsd-verify-work): audible TTS playback intelligibility + live dictation accuracy (perceptual, per VALIDATION.md).
</verification>

<success_criteria>
- WEBVOICE-01..04 proven end to end: speaker → audio/mpeg, dictation → editable transcript, degrade → {false,false} with the mic in attachment mode and no console errors.
- Coverage floors met: web ≥85% + Stryker ≥70% on the adapters + Go owned-surface ≥85% (verified via a dedicated WSL Go gate command, not folded into the web run); internal/webui/dist rebuilt.
</success_criteria>

<output>
Create `.planning/phases/37C-web-voice-lane-inserted/37C-06-SUMMARY.md` when done.
</output>
