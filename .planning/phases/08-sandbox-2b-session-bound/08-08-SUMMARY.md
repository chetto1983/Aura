---
phase: 08-sandbox-2b-session-bound
plan: 08
subsystem: infra
tags: [sandbox, session-bound, seccomp, forward-proxy, scoring, cli, conversations, docker]

# Dependency graph
requires:
  - phase: 08-sandbox-2b-session-bound (08-03)
    provides: scoring.ComputeSandboxTier + GateRecommended (advisory risk tier)
  - phase: 08-sandbox-2b-session-bound (08-05)
    provides: SessionManager (Acquire/Release/RecoverOnBoot) + WorkspaceManager.PurgeConversationDir (os.Root cascade)
  - phase: 08-sandbox-2b-session-bound (08-06)
    provides: host CONNECT forward proxy (deny-wins glob allowlist + resolve-then-pin)
  - phase: 08-sandbox-2b-session-bound (08-07)
    provides: DockerRunner.RunPythonSession/RunShellSession + sidecar /session/{id}/exec routes
provides:
  - "execute tool: active session_id (defaults to conversation id), session-bound routing, advisory scoring annotation"
  - "aura sandbox sessions {list|terminate|prune} operator CLI + aura exec --session live path"
  - "Conversations.Delete cascade via the ConversationCleaner interface (no import cycle)"
  - "session-container network/seccomp posture (connect-allowing variant + egress bridge + proxy env), extends AR-05-01"
affects: [08-09, sandbox, security-audit, conversations]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "consumer-declared narrow interface (tools.SessionAcquirer, conversations.ConversationCleaner) to break import cycles"
    - "reaper-free one-shot operator control plane (sandbox.SessionControl) distinct from the long-lived SessionManager"
    - "allowlist-gated docker run argv (egress bridge + proxy env appended only for a non-empty allowlist)"

key-files:
  created:
    - cmd/aura/sandbox.go
    - internal/sandbox/sessions_control.go
    - internal/sandbox/sessions_control_test.go
    - sandbox/seccomp-session.json
  modified:
    - internal/agent/tools/execute.go
    - internal/agent/tools/execute_test.go
    - internal/conversations/store.go
    - cmd/aura/exec.go
    - cmd/aura/exec_test.go
    - cmd/aura/main.go
    - cmd/aura/chat.go
    - internal/sandbox/sessions.go
    - internal/sandbox/sessions_test.go
    - compose.yaml

key-decisions:
  - "execute always routes through the session-bound Runner (no stateless branch in the tool); an omitted session_id defaults to the ctx conversation id (D-26)"
  - "operator terminate/prune use a new reaper-free SessionControl, not the SessionManager, so a CLI one-shot starts no goroutine"
  - "seccomp-session.json derives from seccomp.json by ADDING connect ONLY; the egressless case keeps the 2a profile (connect denied)"
  - "egress bridge + HTTP(S)_PROXY env are appended to the session docker run argv ONLY when an allowlist is configured; empty allowlist keeps the 2a egressless argv"
  - "ErrSessionCapReached maps to a distinct aura exec exit code (75, EX_TEMPFAIL)"

patterns-established:
  - "tools.SessionAcquirer: optional Acquire/Release seam on the Execute tool (nil in the unit tier, *sandbox.SessionManager in production)"
  - "sandbox.SessionControl: list/terminate/prune over the sqlc querier + LookPath-gated dockerCLI (D-05), injectable docker for tests"

requirements-completed: []  # CAP-02 stays OPEN — the live container integration tier + 08-SECURITY re-audit run in 08-09

# Metrics
duration: ~30min
completed: 2026-06-03
---

# Phase 8 Plan 08: Wave-3 Live-Surface Wiring Summary

**Activated the session-bound sandbox end-to-end — execute tool routes through the per-conversation session runner with an advisory risk tier, `aura sandbox sessions` + `aura exec --session` are live, the conversation delete cascade goes through the os.Root cleaner via an interface, and the session container gets a connect-allowing seccomp variant + host-proxy egress posture (extends AR-05-01).**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-03T11:24Z (approx)
- **Completed:** 2026-06-03T11:53Z
- **Tasks:** 2
- **Files modified:** 14 (4 created, 10 modified)

## Accomplishments
- The `execute` tool's `session_id` is ACTIVE: empty defaults to the conversation id (WithToolCallContext, D-26), validated by the traversal guard, routed through `RunPythonSession`/`RunShellSession`, with optional `Acquire`/`Release` serialization (D-07) when a SessionManager is wired. The inert-reject branch is gone and the Spec() now documents the D-02 asymmetric persistence contract (python state persists; shell cd/export do not).
- A non-empty `network_allow` appends an advisory `[advisory] risk_tier: …, gate_recommended: …` line (`scoring.ComputeSandboxTier`) to the lean preview — advisory only, no pending-state persistence (D-12).
- `aura sandbox sessions {list|terminate|prune}` operator CLI over a new reaper-free `sandbox.SessionControl`; `aura exec --session <id>` routes live through the session runner with `ErrSessionCapReached` → exit 75.
- `Conversations.Delete` tears down the per-conversation tree via the injected `ConversationCleaner.PurgeConversationDir` (os.Root no-follow cascade) — `go list -deps ./internal/conversations` confirms NO `internal/sandbox` dependency.
- `sandbox/seccomp-session.json` (connect added) + an allowlist-gated egress posture (bridge + proxy env in the session docker run argv) + a compose.yaml posture block documenting the bridge-gateway-reachable proxy / empty-allowlist-egressless split EXTENDING AR-05-01.
- Boot composition (`bootChat`) wires WorkspaceManager (as the Store cleaner) + SessionManager (reaper started) + RecoverOnBoot + the session-bound Execute, and Closes the manager on teardown (goleak-clean).

## Task Commits

1. **Task 1: Activate execute session_id + scoring advisory + Conversations.Delete cascade** — `78100acd` (feat) — built on the resumed partial working-tree edits (the unused-import build break is now resolved with the real ComputeSandboxTier call + session routing + inert-reject removal).
2. **Task 2: aura sandbox sessions CLI + exec --session + main.go composition + session network/seccomp posture** — `96723ad3` (feat)

**Plan metadata:** _(this docs commit)_

## Files Created/Modified
- `internal/agent/tools/execute.go` — active session_id, session-bound routing, SessionAcquirer seam, advisory annotation, D-02 Spec() doc.
- `internal/agent/tools/execute_test.go` — rewrote the obsolete 2a session tests for the new contract (default-id, explicit-id, traversal-reject, advisory, Acquire/Release, SessionAcquirer fake).
- `internal/conversations/store.go` — ConversationCleaner interface + nil-safe cleaner field + Delete cascade (partial from prior session, verified + retained).
- `cmd/aura/sandbox.go` — `runSandbox` dispatcher (NEW).
- `internal/sandbox/sessions_control.go` + `_test.go` — reaper-free operator control plane (NEW).
- `sandbox/seccomp-session.json` — 2b connect-allowing seccomp variant (NEW).
- `cmd/aura/exec.go` + `_test.go` — live `--session` path, ErrSessionCapReached exit mapping, rewrote the reserved-session test.
- `cmd/aura/main.go` — `case "sandbox"` + usage.
- `cmd/aura/chat.go` — boot composition (WorkspaceManager cleaner + SessionManager + RecoverOnBoot + session-bound registry + Close).
- `internal/sandbox/sessions.go` + `_test.go` — allowlist-gated egress argv (EgressNetwork + ProxyEnv on SessionDeps).
- `compose.yaml` — session-container network/seccomp posture comment (extends AR-05-01).

## Decisions Made
See `key-decisions` frontmatter. The most consequential: the execute tool has no stateless branch anymore — every call is session-bound (defaulting to the conversation id), which is the 2b contract; and operator terminate/prune deliberately do NOT instantiate a SessionManager (which would start a reaper goroutine) — a focused `SessionControl` keeps the CLI a clean one-shot.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Completed the resumed partial Task-1 edits (unused-import build break)**
- **Found during:** Task 1
- **Issue:** The working tree carried a prior session's partial edits — the `internal/scoring` import was added to execute.go but unused, so `go build` failed ("scoring imported and not used").
- **Fix:** Implemented the real `scoring.ComputeSandboxTier` advisory, removed the inert-reject branch, added the session-bound routing + default-session-id + Acquire/Release seam, updated Spec().
- **Files modified:** internal/agent/tools/execute.go
- **Verification:** `go build ./...` clean; `go test ./internal/agent/tools/` green.
- **Committed in:** 78100acd

**2. [Rule 2 - Missing Critical] Added SessionDeps EgressNetwork + ProxyEnv for the egress posture**
- **Found during:** Task 2
- **Issue:** The plan requires the session container to reach the host proxy at the bridge gateway IP for a non-empty allowlist, but `runArgv` had no seam to attach a bridge or inject HTTP(S)_PROXY env — only a single seccomp profile string.
- **Fix:** Added `EgressNetwork` + `ProxyEnv` to SessionDeps and an allowlist-gated branch in `runArgv`; the composition root selects the session seccomp variant + bridge for a non-empty allowlist, else the egressless floor.
- **Files modified:** internal/sandbox/sessions.go, internal/sandbox/sessions_test.go, cmd/aura/chat.go
- **Verification:** `go test ./internal/sandbox/` green incl. the new egress-gated argv test; race-clean.
- **Committed in:** 96723ad3

**3. [Rule 1 - Bug / obsolete test] Rewrote two tests encoding the removed 2a contract**
- **Found during:** Tasks 1 & 2
- **Issue:** `TestExecute_SessionIdReserved` and `TestRunExec_SessionExitUsage` asserted the inert-reject behavior the plan explicitly removes; they failed against the new live contract.
- **Fix:** Rewrote them as `TestExecute_SessionIdDefaultsToConversation`/`TestExecute_ExplicitSessionId`/etc. and `TestRunExec_SessionLive` (now asserts the live session runner reaches a dead sidecar → exit 70). Per CLAUDE.md, the tests were broken by an intended contract change, justified here.
- **Files modified:** internal/agent/tools/execute_test.go, cmd/aura/exec_test.go
- **Verification:** `go test ./cmd/aura/ ./internal/agent/tools/` green.
- **Committed in:** 78100acd, 96723ad3

---

**Total deviations:** 3 auto-fixed (1 blocking, 1 missing-critical, 1 obsolete-test). No scope creep — all confined to the plan's named files + the SessionDeps seam needed to honour the documented egress posture.
**Impact on plan:** All necessary for correctness/posture. CAP-02 intentionally NOT marked complete (live container integration + 08-SECURITY re-audit are 08-09).

## Issues Encountered
- The per-session forward-proxy start (NewSessionProxy bind per conversation) is wired conceptually (config + seccomp + bridge + proxy-env seam) but the live bind/reachability is deferred to 08-09 per the plan; the Go side carries the posture deterministically and the compose comment flags the live spike.

## Post-edit Validation (Gate 2)
- `go vet ./...` — clean
- `go build ./...` — clean
- `go test ./internal/agent/tools/ ./internal/conversations/ ./cmd/aura/ ./internal/sandbox/` — all PASS
- `go test -race ./internal/sandbox/ ./internal/agent/tools/` — PASS (race-clean)
- `go list -deps ./internal/conversations | grep internal/sandbox` — empty (no import cycle)
- `grep -c ComputeSandboxTier internal/agent/tools/execute.go` = 3; `grep -c PurgeConversationDir internal/conversations/store.go` = 3
- `grep "reserved for Phase 8"` in execute.go + exec.go = 0 (inert-reject gone)
- `grep -c '"connect"' sandbox/seccomp-session.json` = 1; JSON validates (409 syscalls)
- `gofmt -l` on all touched files — clean; all files ≤600 LOC (pre-commit file-size gate green)

## User Setup Required
None — no external service configuration required for this wiring plan. (Live container exercise + the egress reachability spike are 08-09.)

## Next Phase Readiness
- 08-09 can author the live container integration tier (tool + CLI session exec against a real sidecar), the bridge-gateway proxy reachability spike (pip → pypi over the allowlist), and the 08-SECURITY re-audit of the AR-05-01 extension. CAP-02 closes there.
- Open seam for 08-09: per-session `NewSessionProxy` bind/lifecycle (start on session create, stop on reap) — the config + posture are in place; only the live bind is deferred.

## Self-Check: PASSED
- Created files verified on disk: cmd/aura/sandbox.go, internal/sandbox/sessions_control.go(+_test), sandbox/seccomp-session.json, 08-08-SUMMARY.md.
- Commits verified in git log: 78100acd (Task 1), 96723ad3 (Task 2).

---
*Phase: 08-sandbox-2b-session-bound*
*Completed: 2026-06-03*
