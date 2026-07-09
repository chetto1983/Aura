---
phase: 37D-composer-skill-picker
plan: 02
subsystem: api
tags: [agui, composer, skills, http-route, require-auth, mechanism-a, authority-frame, WEBSKILL]
type: execute
wave: 2
autonomous: true
dependency_graph:
  requires:
    - phase: 37D-01
      provides: "prd.md Amendment #81 — the RequireAuth-only GET /api/composer/skills contract, the aura.skill envelope, and Mechanism A (the PRD-first gate)"
  provides:
    - "GET /api/composer/skills — the RequireAuth-only global active-skills snapshot the picker UI + e2e build against (WEBSKILL-01 backend)"
    - "aura.skill run-envelope field + the server-side Mechanism-A pinned-skill application in handleRun (WEBSKILL-02 runtime contract)"
    - "tools.UseAuthorityFrame — the exported authority-frame literal reused verbatim across the tool + the agui server"
    - "SkillsBoardProvider.SkillBody(name) — one-loader-snapshot pinned-skill body resolution (list ⊆ resolvable guard)"
  affects:
    - "37D-03 (skills fetch client + SkillPicker consume GET /api/composer/skills)"
    - "37D-04 (Composer carries aura.skill on send into this decode + Mechanism-A path)"
    - "37D-05 (composer-skills e2e asserts the endpoint + the pinned-skill run)"
tech_stack:
  added: []
  patterns:
    - "Bare-aguiHandler RequireAuth-only parent-mux mount (serve_webui_composer.go mirrors serve_webui_voice.go) — the D-03 anti-403 route class, distinct from the governance.read board sibling"
    - "Mechanism A: server-side prepend of the reused tools.UseAuthorityFrame + resolved body to the MODEL message via the existing TurnWithModelUserMessage split (zero runner change, no new tool, no new skills store)"
    - "One-loader-snapshot provider seam (ActiveSkills + SkillBody read the same Loader.List/Get) → list ⊆ resolvable divergence guard (Pitfall 2)"
key_files:
  created:
    - "internal/agui/composer_api.go — handleComposerSkills + registerComposerRoutes"
    - "internal/agui/composer_api_test.go — endpoint suite (active/RequireAuth-not-capability/401/503)"
    - "cmd/aura/serve_webui_composer.go — bare RequireAuth-only parent-mux mount"
    - "internal/agui/server_run_request_test.go — aura.skill decode suite"
    - "internal/agui/server_skill_run_test.go — pinned-skill run suite + subset guard (goleak)"
  modified:
    - "internal/agui/server.go — registerComposerRoutes in Mux() + Mechanism-A prepend in handleRun (540 LOC)"
    - "cmd/aura/serve_webui.go — one-line registerComposerRoutes call (556 LOC)"
    - "internal/agui/server_run_request.go — Skill field on both decode structs"
    - "internal/agui/governance_seam.go — SkillBody on SkillsBoardProvider"
    - "internal/agui/governance_api_test.go — scriptedSkillsBoard.SkillBody"
    - "internal/agui/governance_seam_test.go — fakeSkillsBoard.SkillBody"
    - "internal/agent/tools/skill_read.go — useAuthorityFrame → exported UseAuthorityFrame"
    - "internal/agent/tools/skill_test.go — white-box assertion renamed to UseAuthorityFrame"
    - "cmd/aura/serve_governance.go — skillsBoardAdapter.SkillBody via loader.Get"
decisions:
  - "GET /api/composer/skills mounted BARE (mux.Handle(composerSkillsRoute, aguiHandler)) so it inherits the whole-mux RequireAuth ONLY — NOT RequireCapability(governance.read); proven by TestComposerSkills_RequireAuthNotCapability (a non-admin identity gets 200 non-empty where the governance gate 403s the SAME principal), the D-03 403 trap avoided"
  - "activeSkillRows reused VERBATIM from governance_api.go so the picker, the governance board, and the runtime skill action=use read ONE loader.List() snapshot (D-04 one source of truth); nil provider → 503 (client degrades to empty, D-09), never a 500/403"
  - "Pinned skill applied via Mechanism A: handleRun prepends tools.UseAuthorityFrame + body + separator to the MODEL message BEFORE the existing *userMsg != *modelUserMsg TurnWithModelUserMessage split, so the model sees the framed skill first while the raw user text stays the visible/persisted turn — zero runner change, no new tool, no new skills store"
  - "useAuthorityFrame exported to tools.UseAuthorityFrame and reused verbatim (never re-declared in agui) — the literal IS the contract (DRY); grep useAuthorityFrame internal/ → 0 after the rename"
  - "The client-supplied aura.skill is a loader KEY resolved via s.governance.Skills.SkillBody (loader.Get against the validated snapshot) — never joined to a filesystem path; an unknown/absent name is a clean no-op (never a 5xx, never a passthrough of client text into the model message) — T-37D-02/03"
  - "SkillBody added to SkillsBoardProvider + ALL THREE existing implementers (skillsBoardAdapter, scriptedSkillsBoard, fakeSkillsBoard) in one task so the agui package compiles (Go builds all _test.go before -run filtering)"
patterns_established:
  - "Pattern: a lean authenticated read route as a bare-aguiHandler sibling of a capability-gated board (serve_webui_composer.go ∥ serve_webui_voice.go) — the same projection, a different auth tier"
  - "Pattern: server-side authority-framed pinned-skill prepend (Mechanism A) over the TurnWithModelUserMessage visible/model split"
requirements_touched: [WEBSKILL-01, WEBSKILL-02]
requirements_completed: []
metrics:
  tasks_completed: 2
  duration: "~50 min (2 TDD tasks, incl. WSL -race + coverage)"
  completed: "2026-07-10"
  files_changed: 14
  commits: ["e4a09a95", "07184186"]
---

# Phase 37D Plan 02: Composer Skills API + Pinned-Skill Wire Path Summary

**RequireAuth-only `GET /api/composer/skills` (the global active-skills snapshot, projected with the governance board's `activeSkillRows` verbatim) plus the `aura.skill` run-envelope field applied server-side via Mechanism A — the reused `tools.UseAuthorityFrame` + resolved body prepended to the model message through the existing `TurnWithModelUserMessage` split, with an unknown name a clean no-op and the listed set a proven subset of the resolvable set.**

## Performance

- **Duration:** ~50 min (2 TDD tasks)
- **Completed:** 2026-07-10
- **Tasks:** 2
- **Files modified/created:** 14 (5 created, 9 modified)

## Accomplishments

- **WEBSKILL-01 backend:** `GET /api/composer/skills` returns `200 {skills:[{name,description,type}]}` for any authenticated identity behind plain `RequireAuth` (401 unauth, 503 nil-provider), mounted BARE — proven NOT to require `governance.read` (the D-03 403 trap the governance board sibling sets).
- **WEBSKILL-02 runtime contract:** the pinned skill rides one `skill` field on the existing `aura` envelope and is applied server-side via Mechanism A — the reused `tools.UseAuthorityFrame` + resolved body prepended to the model message, raw visible turn persisted, unknown name a clean no-op, no runner change / no new tool / no new skills source of truth.
- **One source of truth:** `activeSkillRows` reused verbatim + the new `SkillsBoardProvider.SkillBody` served from the SAME loader snapshot `ActiveSkills` lists — the WEBSKILL-02 list ⊆ resolvable divergence guard is green.
- Daemon-free suite carries the coverage: `handleComposerSkills`/`registerComposerRoutes` 100%, `handleRun` 92.3%, `internal/agui` package 85.7% on the untagged unit run alone (≥85% owned-surface floor).

## Task Commits

Each task was committed atomically (test + implementation together, per the project's one-task-one-commit discipline + Co-Authored-By trailer):

1. **Task 1: GET /api/composer/skills — handler + RequireAuth-only mount + daemon-free suite** — `e4a09a95` (feat)
2. **Task 2: Pinned-skill wire path — aura.skill decode + SkillBody seam + Mechanism-A prepend + exported UseAuthorityFrame** — `07184186` (feat)

**Plan metadata:** this SUMMARY + STATE.md + ROADMAP.md (docs commit).

_TDD note: both tasks are `tdd="true"`; tests were written first and observed failing (compile-RED for the new symbols; behavior-RED for the handleRun prepend) before the implementation, but each landed as a single atomic `feat(...)` commit (impl+test) per CLAUDE.md "one slice = one commit" — matching the 37C/37B sequential-executor precedent._

## Files Created/Modified

**Created:**
- `internal/agui/composer_api.go` — `handleComposerSkills` (nil provider → 503; else `writeJSON(w, {"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})`) + `registerComposerRoutes(mux)` mounting `GET /api/composer/skills` on the agui `Server.Mux`.
- `internal/agui/composer_api_test.go` — active rows, the RequireAuth-not-capability differential, 401 unauth, 503 nil-provider.
- `cmd/aura/serve_webui_composer.go` — `composerSkillsRoute` + `registerComposerRoutes(mux, aguiHandler, auth)` bare `mux.Handle` (RequireAuth-only, the 600-LOC split mirror of `serve_webui_voice.go`).
- `internal/agui/server_run_request_test.go` — `aura.skill` decode present + absent.
- `internal/agui/server_skill_run_test.go` — pinned-skill applied / unknown-name no-op / no-skill unchanged / list-subset-of-resolvable guard (goleak).

**Modified:**
- `internal/agui/server.go` — `s.registerComposerRoutes(mux)` in `Mux()`; the Mechanism-A prepend in `handleRun` (after `buildTurnUserMessage`, before the split). 540 LOC.
- `cmd/aura/serve_webui.go` — one-line `registerComposerRoutes(mux, aguiHandler, auth)` in `newServeHandler`. 556 LOC.
- `internal/agui/server_run_request.go` — `Skill string json:"skill"` on BOTH the typed `Aura` struct and the ext-decode struct.
- `internal/agui/governance_seam.go` — `SkillBody(name string) (string, bool)` on `SkillsBoardProvider`.
- `internal/agui/governance_api_test.go` + `internal/agui/governance_seam_test.go` — `SkillBody` on the `scriptedSkillsBoard` + `fakeSkillsBoard` fakes (interface completeness).
- `internal/agent/tools/skill_read.go` — `useAuthorityFrame` → exported `UseAuthorityFrame` (const + doc + two in-file uses).
- `internal/agent/tools/skill_test.go` — the white-box assertion renamed to `UseAuthorityFrame`.
- `cmd/aura/serve_governance.go` — `skillsBoardAdapter.SkillBody` via `a.loader.Get(name)`.

## Verification

- `go test ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` — full package suites green (daemon-free).
- `go build ./...` + `go vet ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` clean.
- Acceptance guards: `COMPOSER_ENDPOINT_OK`, `PINNED_SKILL_OK`; interface-completeness `ActiveSkills=3 / SkillBody=3`; `grep useAuthorityFrame internal/` → 0; LOC composer_api.go 38 / serve_webui_composer.go 34 / server.go 540 / serve_webui.go 556 (all ≤600).
- **`-race` (WSL, `CGO_ENABLED=1`, go1.26.5 + gcc 15.2.0 — this Windows host has `CGO_ENABLED=0` / no C compiler): PASS** on `internal/agui` (18.1s) + `internal/agent/tools` (3.0s), goleak-clean (no goroutine leaks).
- Coverage (daemon-free): `handleComposerSkills`/`registerComposerRoutes` 100%, `handleRun` 92.3%, `internal/agui` package 85.7% ≥ the 85% owned-surface floor. The CI `db_integration neo4j_integration` matrix only raises it; NO daemon-gated (`docker_integration`) surface was added, so this plan only lifts the aggregate.
- Pre-commit hooks (gofmt + vet + lint + whole-tree file-size) green on both task commits — no `--no-verify`.
- `go.mod`/`go.sum` byte-unchanged; no new deps, migrations, or env vars.

## Threat Model Coverage

- **T-37D-01 (DoS/self, endpoint mount):** mitigated — bare `mux.Handle(composerSkillsRoute, aguiHandler)` inherits whole-mux `RequireAuth` only; `TestComposerSkills_RequireAuthNotCapability` proves a non-admin gets 200 non-empty (not 403).
- **T-37D-02 (path traversal via skill name):** mitigated — the name is resolved via `s.governance.Skills.SkillBody(name)` → `loader.Get(name)` against the validated snapshot key set; an unknown name is a no-op, never joined to a path (`TestRun_PinnedSkill_UnknownName_NoOp`).
- **T-37D-03 (prompt-injection via skill body):** mitigated — the body is delivered through the SAME `tools.UseAuthorityFrame` frame `action=use` emits (loader load-time blocklist unchanged); no new trust surface.
- **T-37D-04 (oversized payload):** mitigated — `handleRun` caps the body via `http.MaxBytesReader` before decode (unchanged); the field is a short bounded name.
- **T-37D-SC (package installs):** N/A — 37D-02 installs NO external packages; `go.mod`/`go.sum` byte-unchanged.

## Deviations from Plan

**None — plan executed exactly as written.** All five must-have truths delivered, all six prohibitions honored (RequireAuth-only mount not `governance.read`; one loader not a second store; name-is-a-loader-key not a filesystem path; `tools.UseAuthorityFrame` reused verbatim not re-declared; no `NewSkillToolForIdentity`/per-identity filtering; `composer_api.go`/`serve_webui.go` ≤600 LOC). No auto-fixes were required (no Rule 1/2/3 triggers surfaced).

## Requirements

`WEBSKILL-01` and `WEBSKILL-02` are **phase-spanning** and remain `[ ]`: 37D-02 delivers the backend contract (the API + the runtime wire path), but WEBSKILL-01's `/`-triggered keyboard-filterable menu is delivered by the frontend (37D-03/04) and WEBSKILL-02's "selecting an entry" is the composer integration (37D-04); the a11y + e2e + coverage gate is 37D-05 (WEBSKILL-03). Both IDs are carried by 37D-03/04 too, so `requirements mark-complete` was intentionally NOT run here — matching the 37D-01 / 37C / 37B gate-plan precedent (the terminal plan marks them).

## Known Stubs

None introduced. (The plan records that the per-identity rooting primitive `NewSkillToolForIdentity` remains a pre-existing DORMANT primitive with zero production callers and that persisted per-identity skill scoping is DEFERRED — documented intent from 37D-01, not a new stub created here.)

## Next Phase Readiness

- The API contract (`GET /api/composer/skills` shape + auth tier) and the `aura.skill` envelope are frozen for **37D-03** (skills fetch client + `SkillPicker` combobox) and **37D-04** (Composer `/` trigger + send carrying `aura.skill`).
- **37D-05** e2e can assert both the endpoint (200 rows, 401 unauth) and the pinned-skill run (framed model message).
- No blockers; no external service configuration required.

## Self-Check: PASSED

- FOUND: `internal/agui/composer_api.go`, `internal/agui/composer_api_test.go`, `cmd/aura/serve_webui_composer.go`, `internal/agui/server_run_request_test.go`, `internal/agui/server_skill_run_test.go`
- FOUND: commit `e4a09a95` (Task 1, feat), commit `07184186` (Task 2, feat)
- Interface-completeness `ActiveSkills=3 / SkillBody=3`; `grep useAuthorityFrame internal/` → 0; all touched files ≤600 LOC.
- No unintended file deletions (the 6 line-deletions in Task 2 are the const/use-site renames, not files).

---
*Phase: 37D-composer-skill-picker*
*Completed: 2026-07-10*
