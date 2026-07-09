---
phase: 37D-composer-skill-picker
plan: 05
type: execute
wave: 4
depends_on: ["37D-04"]
files_modified:
  - web/e2e/composer-skills.spec.ts
  - internal/webui/dist
autonomous: true
requirements: [WEBSKILL-03]
must_haves:
  truths:
    - "composer-skills.spec.ts drives the full flow against the built app (golden-replay, no live agent loop): mock GET /api/composer/skills (non-empty) + the /agent/run SSE; open the / menu → filter → select a skill → the removable pill appears → send; the intercepted /agent/run POST body carries aura.skill === the selected skill name (the WEBSKILL-02 end-to-end proof)"
    - "the spec also proves the quick actions: new-chat starts a new conversation (route/effect), clear resets the composer input + pinned pill; each is a pure client action (no /agent/run POST fired for the action itself)"
    - "every assertion is a COUNTED DOM/route fact guarded > 0 (no-skip-as-green, CLAUDE.md); the shared gotoAuthenticated harness throws when neither a live serve nor the auth stack is reachable"
    - "internal/webui/dist is rebuilt from the 37D-03/04 web changes so the shipped binary + the Playwright webServer serve the composer picker"
    - "the web unit coverage gate (vitest v8 statements/branches/functions/lines ≥85) and the owned-surface Go coverage gate (≥85 on the db_integration neo4j_integration matrix) both pass with the 37D surface included"
  artifacts:
    - path: "web/e2e/composer-skills.spec.ts"
      provides: "Playwright golden-replay e2e: open→filter→select→pill→send-carries-aura.skill + new-chat/clear"
      contains: "composer/skills"
  key_links:
    - from: "web/e2e/composer-skills.spec.ts"
      to: "/agent/run"
      via: "intercept POST + assert postData aura.skill"
      pattern: "aura"
  prohibitions:
    - "MUST NOT drive a live agent turn — mock /api/composer/skills + the /agent/run SSE at the page-network layer (golden-replay, mirroring artifacts.spec.ts / replay.spec.ts sseFromFrames); the spec exercises the REAL in-browser picker + sseAdapter without a backend agent loop"
    - "MUST NOT assert on an uncounted condition — every check counts DOM/route facts and guards > 0 so a no-op run FAILS (no-skip-as-green)"
    - "MUST NOT close the phase below 85% — if the web vitest gate or the owned-surface Go gate reports <85, add daemon-free unit tests to close the gap before sign-off (do NOT lower a threshold)"
    - "MUST NOT hand-edit internal/webui/dist — regenerate it via the web build (npm run build); it is a build artifact"
---

<objective>
Prove the 37D feature end-to-end and hold the coverage floor: a Playwright golden-replay spec that opens the `/` picker, filters, selects a skill, sees the pill, sends, and asserts the intercepted `/agent/run` POST carries `aura.skill` (the WEBSKILL-02 wire proof) plus the new-chat/clear quick-action behavior — all with counted, no-skip-as-green assertions against the freshly-built app. Then close the phase by rebuilding the `internal/webui/dist` embed and verifying both coverage gates (web vitest ≥85 + owned-surface Go ≥85) include the new surface.

Purpose: The terminal gate (mirrors 37C-06) — deliver the SC3 e2e + the ≥85% coverage sign-off that `/gsd-verify-work` builds on.
Output: `web/e2e/composer-skills.spec.ts`, a rebuilt `internal/webui/dist`, and green web + Go coverage gates.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37D-composer-skill-picker/37D-RESEARCH.md
@.planning/phases/37D-composer-skill-picker/37D-VALIDATION.md
@web/e2e/artifacts.spec.ts
</context>

<artifacts_produced>
This plan produces:
- `web/e2e/composer-skills.spec.ts` — a Playwright golden-replay spec: `gotoAuthenticated` (from `./auth`), a mocked `GET /api/composer/skills` returning a non-empty `{skills:[...]}`, a mocked `/agent/run` SSE (`sseFromFrames` idiom), and a POST interceptor capturing the run body. Flow assertions (each counted, `> 0`): the `/` menu opens with the mocked skills; typing filters; selecting a skill renders the removable pill; sending fires a `/agent/run` whose body `aura.skill` equals the selected name; `new-chat` starts a new conversation; `clear` resets the composer input + pill.
- A regenerated `internal/webui/dist` (via `cd web && npm run build`) embedding the 37D composer picker.
- The phase-close coverage verification (web vitest ≥85 + owned-surface Go ≥85).
</artifacts_produced>

<tasks>

<task type="auto">
  <name>Task 1: composer-skills.spec.ts golden-replay e2e (open→filter→select→pill→send carries aura.skill; new-chat/clear) + dist rebuild</name>
  <files>web/e2e/composer-skills.spec.ts, internal/webui/dist</files>
  <read_first>
    - web/e2e/artifacts.spec.ts (whole file) — the golden-replay harness to mirror: gotoAuthenticated from ./auth, page.route mocks at the network layer, the no-skip-as-green counted-assertion discipline (domAssertions/hits guarded > 0), the chromium + mobile-chrome projects.
    - web/e2e/replay.spec.ts — the `sseFromFrames` idiom for mocking the `/agent/run` SSE response (so no live agent loop runs).
    - web/e2e/voice.spec.ts — a sibling web-parity e2e (open a composer control, drive it, assert a counted outcome) for structure.
    - web/e2e/auth.ts — gotoAuthenticated (the shared live-or-throw harness) + how a conversation id / thread is seeded for the run.
    - web/playwright.config.ts — the webServer (`aura serve`) that serves the built app + the project list; confirm the e2e runs against the embedded internal/webui/dist (hence the rebuild).
    - .planning/phases/37D-composer-skill-picker/37D-RESEARCH.md § Validation Architecture (SC3 e2e row) + .planning/phases/37D-composer-skill-picker/37D-VALIDATION.md (the SC→test map) — the exact flow to cover.
  </read_first>
  <action>
    First rebuild the web embed: `cd web && npm run build` (regenerates internal/webui/dist with the 37D composer picker so the Playwright webServer serves it). Create web/e2e/composer-skills.spec.ts mirroring artifacts.spec.ts: `gotoAuthenticated`, then `page.route('**/api/composer/skills', ...)` returning a non-empty `{skills:[{name:'skill-creator',description:'...',type:'instruction'}, ...]}`, and mock the `/agent/run` SSE via the sseFromFrames idiom (a minimal assistant text turn) while INTERCEPTING the `/agent/run` POST to capture its body. Drive the flow with counted assertions guarded `> 0`: (1) focus the composer, type `/`, assert the picker listbox + the mocked skill option render; (2) type a filter fragment, assert the option list narrows; (3) select the skill (click or ArrowDown+Enter), assert the removable pill renders; (4) type a message + send, assert exactly one `/agent/run` POST fired AND its parsed body `aura.skill === 'skill-creator'`; (5) `new-chat`: open `/`, pick new-chat, assert a new conversation route/effect occurred AND no `/agent/run` fired for the action; (6) `clear`: with text + a pinned pill, pick clear, assert the composer input is empty and the pill is gone AND no `/agent/run` fired. Keep every assertion counted (guard `> 0` / exact equality) so a no-op FAILS. Do NOT drive a live agent turn.
  </action>
  <acceptance_criteria>
    - `web/e2e/composer-skills.spec.ts` exists, uses `gotoAuthenticated`, mocks `/api/composer/skills` + the `/agent/run` SSE, and intercepts the run POST to assert `aura.skill`.
    - The spec contains at least one exact-equality assertion that the run body's `aura.skill` equals the selected skill name (the WEBSKILL-02 wire proof) and counted `> 0` DOM assertions for the menu/pill.
    - `internal/webui/dist` is regenerated (the build ran) so the served app includes the composer picker.
    - `cd D:/Repo/Aura/web && npm run test:e2e -- composer-skills` passes (chromium; and mobile-chrome if the sibling specs run it).
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npm run build && npm run test:e2e -- composer-skills && echo COMPOSER_E2E_OK</automated>
  </verify>
  <done>The e2e proves open→filter→select→pill→send-carries-aura.skill and the new-chat/clear pure-client actions against the freshly-built app, with counted no-skip-as-green assertions; internal/webui/dist is rebuilt.</done>
</task>

<task type="auto">
  <name>Task 2: Phase-close coverage gate — web vitest ≥85 + owned-surface Go ≥85 (close residual gaps)</name>
  <files>web/e2e/composer-skills.spec.ts</files>
  <read_first>
    - web/vitest.config.ts:28-32 — the v8 coverage thresholds (statements/branches/functions/lines = 85) the web gate enforces; confirm the 37D web files are included in the coverage scope.
    - scripts/coverage_gate.sh — the owned-surface Go gate (internal/* minus generated/skeleton, floor via AURA_COVERAGE_MIN, tags db_integration neo4j_integration) that CLAUDE.md pins at ≥85; note internal/agui (composer_api.go + the handleRun edits) is owned surface exercised by the 37D-02 daemon-free tests.
    - CLAUDE.md § Quality tooling & gates — the coverage floor 85% rule, the tag set (db_integration neo4j_integration ONLY — no docker_integration job), and the "verify locally before pushing with scripts/coverage_docker.sh" guidance.
    - .planning/phases/37D-composer-skill-picker/37D-VALIDATION.md § Sampling Rate / Validation Sign-Off — the phase-gate checklist.
  </read_first>
  <action>
    Run the two coverage gates with the full 37D surface included and confirm both clear 85%. Web: `cd web && npm test` (vitest run --coverage) — the v8 thresholds (85) fail the run if any 37D file drags a metric below; if a metric is short, add daemon-free unit tests (extend the 37D-03/04 suites or the SkillPicker/model tests) to close the gap — do NOT lower a threshold. Go owned-surface: run `bash scripts/coverage_gate.sh` under the db_integration neo4j_integration matrix (WSL/CI with the stack up, or `bash scripts/coverage_docker.sh` per CLAUDE.md), confirming internal/agui (composer_api.go + the pinned-skill handleRun path) is covered by the 37D-02 unit suite and the aggregate owned-surface floor stays ≥85; if internal/agui dropped, add daemon-free unit tests for the uncovered branch (e.g. the nil-provider 503, the unknown-name no-op). Record the measured web + owned-surface percentages in the plan SUMMARY. This task adds tests only where a gate is short; if both gates are already green, it is a verification-only pass.
  </action>
  <acceptance_criteria>
    - `cd D:/Repo/Aura/web && npm test` exits 0 (vitest v8 ≥85 statements/branches/functions/lines with the 37D files included).
    - The owned-surface Go gate (`bash scripts/coverage_gate.sh`, db_integration neo4j_integration matrix) reports the aggregate ≥85 with internal/agui's composer surface covered (measured value recorded in the SUMMARY).
    - No coverage threshold was lowered to pass; any shortfall was closed by adding daemon-free unit tests.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npm test && echo WEB_COVERAGE_85_OK</automated>
  </verify>
  <done>Both coverage gates pass with the 37D surface included (web vitest ≥85 automated here; owned-surface Go ≥85 on the db+neo4j matrix recorded in the SUMMARY); no threshold lowered.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| (test-only) | No new runtime trust boundary; this plan adds an e2e spec + rebuilds the web embed. Its job is to PROVE the mitigations built in 37D-02/03/04 hold end-to-end. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37D-09 | Repudiation (falsely-green e2e / coverage) | composer-skills.spec.ts + the coverage gates | mitigate | Every e2e assertion is a counted DOM/route fact guarded > 0 (no-skip-as-green); the run-body interceptor asserts aura.skill by exact equality; the coverage gates fail below 85 (no threshold lowered) — a no-op run FAILS rather than passing |
| T-37D-10 | Tampering (stale embed hides the feature) | internal/webui/dist | mitigate | internal/webui/dist is rebuilt from source via npm run build so the served/shipped app matches the 37D source (the e2e runs against the rebuilt embed) |
| T-37D-SC | Tampering | npm/pip/cargo installs | accept | 37D installs NO external packages (RESEARCH § Package Legitimacy Audit: N/A) |
</threat_model>

<verification>
- `cd web && npm run build && npm run test:e2e -- composer-skills` green (the full flow + quick actions against the rebuilt app).
- `cd web && npm test` green (vitest v8 ≥85 with the 37D files included).
- `bash scripts/coverage_gate.sh` (db_integration neo4j_integration, WSL/CI stack up) reports the owned-surface aggregate ≥85 with internal/agui's composer surface covered (recorded in the SUMMARY).
</verification>

<success_criteria>
- The composer-skills e2e proves open→filter→select→pill→send-carries-aura.skill and the new-chat/clear pure-client actions, golden-replay, no-skip-as-green.
- internal/webui/dist is rebuilt; the web vitest gate (≥85) and the owned-surface Go gate (≥85, db+neo4j matrix) both pass with the 37D surface included; no threshold lowered.
</success_criteria>

<output>
Create `.planning/phases/37D-composer-skill-picker/37D-05-SUMMARY.md` when done.
</output>
