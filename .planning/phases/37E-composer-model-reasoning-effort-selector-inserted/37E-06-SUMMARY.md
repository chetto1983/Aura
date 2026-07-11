---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 06
subsystem: api
tags: [reasoning-effort, governance, two-stage-validation, capability-gate, agui, composer-endpoint, override-seam, persistence, composition-root]

# Dependency graph
requires:
  - phase: 37E-03
    provides: "Store.UpdateReasoningEffortForIdentity + widened agui ConversationStore interface — handleRun persists the accepted symbol owner-scoped"
  - phase: 37E-04
    provides: "runner.WithReasoningOverride(ctx, llm.ReasoningEffort) — handleRun threads the validated fixed level into ctx before the runner builds the agent"
  - phase: 37E-05
    provides: "llm.ReasoningCapabilitySource (AllowedEfforts(ctx)) + llm.NewReasoningCapabilitySource(cfg,ttl) — the endpoint AND Stage-2 read one boot-warmed source; nil/detected=false => safe floor {auto,off}"
provides:
  - "aura run DTO `Effort` field (both request structs) + parseEffortSymbol(symbol)->(effort,isFixed,ok) — the 7-symbol enum decode mirroring aura.skill"
  - "Two-stage handleRun governance (owner-scope gate -> Stage-1 syntactic enum -> Stage-2 capability) with distinct 400 messages; absent/auto -> today's adaptive default (no regression)"
  - "Server.SetReasoningCapabilitySource(src, backend) — composition-root injector"
  - "GET /api/composer/reasoning-capabilities -> reasoningCapabilitiesDTO {allowed symbols, default, backend, detected}; nil source -> safe floor {auto,off} at 200"
  - "cmd/aura wireReasoningCapabilities(server, cfg) — boot wiring of NewReasoningCapabilitySource into the agui Server"
affects: [37E-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-stage server-side governance at a trust boundary: syntactic enum THEN capability, both -> 400 but with distinct bodies/logs; ordering owner-scope(404/401) -> Stage-1 -> Stage-2 proven so isolation always precedes governance"
    - "Symbol-only client contract (WEBMODEL-03): the client sends a SYMBOL, never a raw ReasoningConfig/budget; the server owns the symbol->config map (parseEffortSymbol) and the capability gate"
    - "Safe-floor degradation: a nil/detection-failed capability source yields {auto,off} at HTTP 200 (never 503) for both the endpoint and Stage-2 — availability of the capability probe never blocks a turn"
    - "Refactor-on-touch under the 600-LOC cap: adding the effort governance tipped server.go over 600, resolved by extracting the SSE stream helpers (streamSSE/pumpSend/bufferCap) into server_sse.go — same code, no behaviour change"

key-files:
  created:
    - internal/agui/server_reasoning_effort.go
    - internal/agui/server_reasoning_effort_test.go
    - internal/agui/composer_api_reasoning_test.go
    - internal/agui/server_sse.go
  modified:
    - internal/agui/server.go
    - internal/agui/server_run_request.go
    - internal/agui/server_run_request_test.go
    - internal/agui/composer_api.go
    - internal/runner/runner_reasoning.go
    - cmd/aura/serve.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve_webui_composer.go

key-decisions:
  - "aura.effort is decoded exactly like aura.skill (same DTO shape, both request structs carry the field) so the composer's existing symbol-passing convention is reused, not reinvented"
  - "Owner-scope isolation runs BEFORE governance: a cross-owner run returns 404 (isolation) not 400 (bad effort) — an attacker cannot use the effort error to probe conversation ownership (T-37E-06 ordering, tested)"
  - "Stage-1 (enum) and Stage-2 (capability) both return 400 but with distinct messages so the client/log can tell a typo from an unsupported level; the turn NEVER runs on a rejected effort (400 precedes the turn, tested)"
  - "The capability source is warmed ONCE at boot in wireReasoningCapabilities (non-blocking) and shared by the endpoint + Stage-2 from one TTL cache; an unrecognized backend leaves it nil -> both degrade to {auto,off}"
  - "The reasoning-capabilities route sits on registerComposerRoutes behind plain RequireAuth (mirror the 37D composer skills route), not a capability gate — reading the allowed UI symbols is not a privileged action"

patterns-established:
  - "Daemon-free HTTP governance tests: a fake ReasoningCapabilitySource + fake ConversationStore drive every two-stage branch (advertised/unadvertised/mandatory/detection-failed) with NO live network or DB"
  - "Endpoint body-leak assertion: the reasoning-capabilities response is asserted to NEVER contain the model id / base URL / API key — only UI-safe symbols"

requirements-completed: []  # WEBMODEL-02 (server /agent/run effort governance) and WEBMODEL-03 (no-bypass, real knob) are now server-observable, but the marks are deferred to terminal plan 37E-07 (the UI makes the whole vertical user-observable) per the 37E-01..05 precedent.

# Metrics
duration: ~35min (executor Tasks 1-2 + Task-3 code) + orchestrator close-out after a session-limit interruption
completed: 2026-07-11
---

# Phase 37E Plan 06: Server Governance + Capability Endpoint Summary

**The integration plan that binds all three Wave 2/3 verticals: `/agent/run` now decodes an optional `aura.effort` symbol and runs TWO-STAGE server governance — owner-scope isolation, then a syntactic 7-symbol enum gate (Stage-1, non-enum → 400), then a capability gate (Stage-2, a fixed level not in the active model's advertised set → 400; detection-failed collapses to the safe floor). An accepted fixed level is threaded via `runner.WithReasoningOverride` (37E-04) and persisted owner-scoped (37E-03). `GET /api/composer/reasoning-capabilities` exposes the allowed UI symbols + default + backend + detected (safe floor `{auto,off}` on a nil source), and `SetReasoningCapabilitySource` (37E-05) is wired once at the composition root. Symbol-only, no governance bypass (WEBMODEL-03).**

## Performance

- **Tasks:** 3/3 committed
- **Files:** 4 created, 8 modified
- **Completed:** 2026-07-11
- **Close-out note:** the executor subagent completed Tasks 1 & 2 and authored the Task-3 wiring, then was terminated mid-Task-3 by a provider session limit (before committing Task 3 / writing this SUMMARY / updating tracking). The orchestrator recovered per the safe-resume gate: verified the uncommitted Task-3 code was functionally complete (the `wireReasoningCapabilities` call site IS present at `serve.go:436`) and compiled clean, ran the full gate (build/vet/lint/file-size/tests/-race), then committed Task 3 (`953357e8`) and closed the plan. No work was redone or lost.

## Accomplishments
- **`aura.effort` decode** — an `Effort string \`json:"effort"\`` field on BOTH run-request DTO structs (`server_run_request.go`), decoded via `parseEffortSymbol(symbol) -> (effort llm.ReasoningEffort, isFixed, ok bool)`: the 7-symbol enum `auto·off·low·mid·high·extra·max`, `auto`/absent → `isFixed=false` (adaptive), a non-enum token → `ok=false` (Stage-1 400).
- **Two-stage `handleRun` governance** (after the owner-scope gate): Stage-1 rejects a non-enum symbol with a distinct 400 and the turn does NOT run; Stage-2 checks a fixed level against the active model's advertised set — unadvertised → 400 (distinct Stage-2 message), a mandatory model + `off` → 400; when detection failed, only `off`/`auto` pass (graduated → 400 safe floor). An accepted fixed level is threaded via `runner.WithReasoningOverride(ctx, effort)` and persisted owner-scoped via `Store.UpdateReasoningEffortForIdentity`.
- **`GET /api/composer/reasoning-capabilities`** (`handleReasoningCapabilities` + `reasoningCapabilitiesDTO`) — mirrors the 37D composer-skills route on `registerComposerRoutes` behind `RequireAuth`; returns only the allowed UI symbols (prepend `auto`, omit `off` when mandatory) + default + backend + `detected`; a nil/detection-failed source degrades to `{auto,off}` at HTTP 200 (never 503) and the body never leaks the model id / base URL / key.
- **Composition-root wiring** — `wireReasoningCapabilities(aguiServer, chat.cfg.LLM)` (`cmd/aura/serve.go:436`) builds `llm.NewReasoningCapabilitySource(cfg, reasoningCapsTTL)` (37E-05), warms it once at boot, and injects it via `Server.SetReasoningCapabilitySource(src, backend)`; an unrecognized backend leaves the source nil so both the endpoint and Stage-2 degrade to the safe floor.
- **Refactor-on-touch (600-LOC cap):** adding the governance pushed `server.go` over 600 LOC — the SSE stream helpers (`streamSSE`, `pumpSend`, `bufferCap`) were extracted verbatim into `server_sse.go` (125 LOC); `server.go` landed at 491 LOC.

## Task Commits

1. **Task 1: decode aura.effort + two-stage handleRun governance + thread + persist** — `051b569b` (feat)
2. **Task 2: GET /api/composer/reasoning-capabilities endpoint + safe fallback** — `1928d83a` (feat)
3. **Task 3: wire reasoning-capability source at the composition root** — `953357e8` (feat, committed by the orchestrator at close-out)

**Plan metadata:** (this docs commit)

## Files Created/Modified
- `internal/agui/server_reasoning_effort.go` (created, 79 LOC) — the effort governance helpers (Stage-1/Stage-2 evaluation).
- `internal/agui/server_reasoning_effort_test.go` (created, 314 LOC) — `TestHandleRunEffort` (Stage-1 + isolation-precedes-governance), `TestHandleRunEffortCapability` (Stage-2 advertised/unadvertised/mandatory/detection-failed), `TestParseEffortSymbol`.
- `internal/agui/composer_api_reasoning_test.go` (created, 128 LOC) — `TestReasoningCapabilitiesEndpoint` (auto-first symbols, extra/max mapping, mandatory omits off, nil source → safe floor at 200, detection failed → safe floor, no id/URL/key leak).
- `internal/agui/server_sse.go` (created, 125 LOC) — SSE stream helpers extracted from server.go (refactor-on-touch; no behaviour change).
- `internal/agui/server.go` (modified, 491 LOC) — `parseEffortSymbol`, `SetReasoningCapabilitySource`, two-stage `handleRun` governance; SSE helpers removed (now in server_sse.go).
- `internal/agui/server_run_request.go` (modified, +7) — `Effort` field on both run-request structs.
- `internal/agui/server_run_request_test.go` (modified, +10) — effort field decode coverage.
- `internal/agui/composer_api.go` (modified, +74, 108 LOC) — `reasoningCapabilitiesDTO`, `handleReasoningCapabilities`, route registration.
- `internal/runner/runner_reasoning.go` (modified, +9) — small helper bridging the override into the request path (consistent with 37E-04's seam).
- `cmd/aura/serve.go` (modified, +6) — `wireReasoningCapabilities` call site.
- `cmd/aura/serve_webui.go` (modified, +37, 593 LOC) — `wireReasoningCapabilities` func + `reasoningCapsTTL`.
- `cmd/aura/serve_webui_composer.go` (modified, +16/-4) — related composer wiring.

## Decisions Made
- **Isolation precedes governance.** The owner-scope gate runs first; a cross-owner conversation returns 404, not the 400 for a bad effort — the effort error can't be used to probe ownership. Proven by a dedicated subtest.
- **Two distinguishable 400s.** Stage-1 (enum) and Stage-2 (capability) both 400 but carry distinct bodies so a client typo vs. an unsupported level are separable in logs; the turn never runs on a rejected effort.
- **One boot-warmed source, two readers.** `wireReasoningCapabilities` warms the capability source once (non-blocking) and shares it between the endpoint and Stage-2 from a single TTL cache; nil backend → safe floor everywhere.
- **Route behind plain RequireAuth** (mirror the 37D skills route) — reading allowed UI symbols is not a privileged action, so no capability gate.
- **WEBMODEL-02/03 not marked complete** — server governance is in place, but the requirement becomes user-observable only with the UI (37E-07), which owns the mark (37E-01..05 precedent).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking / refactor-on-touch] SSE-helper extraction to keep server.go under the 600-LOC cap**
- **Found during:** Task 1 (adding two-stage governance to handleRun/server.go)
- **Issue:** The added governance pushed `server.go` over the CLAUDE.md 600-LOC hard cap; the pre-commit file-size hook would reject the commit.
- **Fix:** Extracted the pre-existing SSE stream helpers (`streamSSE`, `pumpSend`, `bufferCap`, `isLifecycleFrame`) verbatim into `internal/agui/server_sse.go`. No behaviour change — same code, moved. `server.go` → 491 LOC, `server_sse.go` → 125 LOC.
- **Verification:** file-size hook green (all within cap); `-race` on internal/agui clean (WSL) — the moved SSE goroutine code is unchanged and still race-safe.

### Orchestrator close-out (not a plan deviation)
The executor was cut off by a provider session limit after authoring (but before committing) Task 3. The orchestrator verified the uncommitted code was complete and compiling, then committed it and produced this SUMMARY + tracking. The delivered code matches the plan's `files_modified` exactly — no scope change.

**Total deviations:** 1 auto-fixed (Rule 3 — file-size-cap refactor). No architectural changes (Rule 4), no auth gates.

## Issues Encountered
- **Session-limit interruption mid-Task-3.** Recovered via the safe-resume gate (see close-out note) — Tasks 1 & 2 were already committed, Task-3 code was complete and compiling, so the orchestrator committed it after a full verification pass rather than re-executing.
- **`-race` needs CGO/gcc, absent on the Windows PATH.** All `-race` verification ran in WSL Ubuntu (go1.26.5, gcc, repo at `/mnt/d/Repo/Aura`) per CLAUDE.md; `internal/agui` `-race` clean (17s). Non-race tests, `go vet ./...`, `go build ./...`, `gofmt`, `golangci-lint`, and file-size ran on Windows via the pre-commit hook (0 issues).

## User Setup Required
None — no new env vars, deps, or migrations. `wireReasoningCapabilities` reuses the already-configured LLM backend config.

## Next Phase Readiness
Wave-5 plan 37E-07 (the Composer selector UI) links against:
- `GET /api/composer/reasoning-capabilities` → `{allowed, default, backend, detected}` — the selector renders ONLY the allowed symbols; safe floor `{auto,off}` when `detected=false`.
- The `aura.effort` run-body field — the UI folds the chosen fixed level in (omitting `auto`); server Stage-1/Stage-2 governance already enforces the enum + capability, and persistence (37E-03) round-trips it for reopen hydration (the `Conversation.ReasoningEffort` DTO field).

No blockers. Build/vet/lint/file-size clean; `internal/agui` tests + `-race` green; no file >600 LOC.

## Threat Flags
None new. The two trust boundaries (the `aura.effort` client symbol and the capability source) are exactly those in the plan's threat register and are mitigated as specified (symbol-only decode, two-stage server-owned validation, isolation-before-governance ordering, safe-floor degradation, no-leak endpoint body).

## Self-Check: PASSED

Files (created) verified present on disk:
- internal/agui/server_reasoning_effort.go — FOUND
- internal/agui/server_reasoning_effort_test.go — FOUND
- internal/agui/composer_api_reasoning_test.go — FOUND
- internal/agui/server_sse.go — FOUND

Commits verified in git log:
- 051b569b (Task 1) — FOUND
- 1928d83a (Task 2) — FOUND
- 953357e8 (Task 3) — FOUND

Gate: `go build ./...` clean, `go vet ./...` clean, `golangci-lint` 0 issues, file-size all within cap, `internal/agui` tests pass, `-race` clean (WSL).

---
*Phase: 37E-composer-model-reasoning-effort-selector-inserted*
*Completed: 2026-07-11*
