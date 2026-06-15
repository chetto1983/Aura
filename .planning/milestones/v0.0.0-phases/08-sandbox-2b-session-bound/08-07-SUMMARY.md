---
phase: 08-sandbox-2b-session-bound
plan: 07
subsystem: sandbox
tags: [sandbox, sidecar, python, session, http, stdlib, runner, interpreter]

# Dependency graph
requires:
  - phase: 08-02
    provides: ErrSandboxUnreachable/ErrSandboxProtocol sentinels (errors.go) reused by the session exec path's taxonomy
  - phase: 05 (Slice 2a stateless)
    provides: sandbox/sidecar.py (run_code/_send_json/healthz/MAX_STREAM), internal/sandbox/{sandbox.go,docker.go} Runner+DockerRunner, the unit-tier goleak TestMain
provides:
  - "sidecar.py per-session interpreter: POST /session/{id}/exec/python execs into a long-lived namespace dict (x=42 survives across calls, D-01); POST /session/{id}/exec/shell re-applies a per-session API-managed cwd (D-02 asymmetric); guarded by threading.Lock (A6); stdlib-only (D-03)"
  - "Runner interface extended additively with RunPythonSession/RunShellSession(ctx, sessionID, code, timeoutSec)"
  - "DockerRunner.sessionExec → POST /session/{id}/exec/{lang} over HTTP (D-05), reusing the shared execPath timeout/decode/error-taxonomy"
affects: [08-08 wiring (execute tool session_id activation + advisory Result fields), 08-09 live persistence assertions]

# Tech tracking
tech-stack:
  added: []  # zero new modules — Python stdlib (io/threading/contextlib/traceback) + Go stdlib net/http
  patterns:
    - "exec(compile(code, '<session>', 'exec'), ns) into a kept dict for stdlib persistence (NO IPython/Jupyter/ZMQ — D-03)"
    - "sys.stdout/stderr redirected to io.StringIO around in-process exec (no subprocess, so persistence outlives the call)"
    - "asymmetric persistence: python vars persist; shell cd/export do NOT (only the tracked cwd is re-applied via subprocess.run(cwd=...))"
    - "additive interface extension — new methods + new HTTP path alongside the unchanged stateless ones; the 2a callers compile untouched"
    - "shared execPath: stateless exec + session sessionExec both delegate; post() takes a full path (no URL-build duplication)"

key-files:
  created:
    - sandbox/sidecar_test.py
  modified:
    - sandbox/sidecar.py
    - internal/sandbox/sandbox.go
    - internal/sandbox/docker.go
    - internal/sandbox/docker_test.go
    - internal/agent/tools/execute_test.go

key-decisions:
  - "Session persistence is exec() into a per-session dict, NOT an IPython kernel — keeps the sidecar Python-stdlib-only (D-03 invariant)"
  - "A user-code exception in the python session is captured into stderr (exit 1) via traceback, never crashing the server — one bad call must not kill the session"
  - "Result was NOT extended with advisory {risk_tier, gate_recommended} fields — those belong to 08-08 per the plan (scope control); only the Runner interface gained methods"
  - "post() now takes a full path string (not lang) so the stateless and session exec routes share one request/decode/error path — no duplication"
  - "Added a _drain_body() before the unknown-path 404 so an undrained request body does not leave a half-read keep-alive connection (client RST); correct HTTP hygiene in the container too"

patterns-established:
  - "Pattern: stdlib persistent interpreter — module-level SESSIONS dict (id -> {ns, cwd}) under a threading.Lock for the get-or-create, in-process exec into ns"
  - "Pattern: prefix-parse router (_parse_session_path) alongside the static INTERPRETERS map — session path falls through to 404 on unknown lang/empty id"

requirements-completed: [CAP-02]

# Metrics
duration: ~25min
completed: 2026-06-03
---

# Phase 8 Plan 07: Per-Session Interpreter + Session-Bound Runner Path Summary

Gave the sandbox sidecar a long-lived per-session Python interpreter (a kept namespace `dict` so `x=42` set in call 1 is readable as `x` in call 2) and an asymmetric per-session shell cwd (shell stays `subprocess.run` per call but re-applies the API-managed cwd — `cd`/`export` do NOT persist), then extended the Go `Runner`/`DockerRunner` additively with a session-bound exec HTTP path (`POST /session/{id}/exec/{lang}`). Execution stays HTTP-only (D-05) and the sidecar stays Python-stdlib-only (no IPython/Jupyter/ZMQ — D-03). Live persistence assertions are deferred to 08-09; this plan ships the sidecar + runner logic + the stdlib-only / httptest unit coverage.

## What Was Built

### Task 1 — Per-session interpreter in sidecar.py (stdlib-only) — commit a3fd6058
- Module-level `SESSIONS: dict[str, dict]` (`session_id -> {"ns": {}, "cwd": <path>}`) + `SESSIONS_LOCK = threading.Lock()`; `_get_session` does the get-or-create under the lock (defense against a 2nd concurrent Aura process — the Go container lock is Aura-local, RESEARCH A6).
- `run_session_python`: `exec(compile(code, "<session-{id}>", "exec"), ns)` into the kept `ns` dict (in-memory state survives — D-01); stdout/stderr captured via `contextlib.redirect_stdout/stderr` to `io.StringIO`; a `BaseException` in user code is captured into stderr (`exit_code=1`) via `traceback.print_exc`, never crashing the server. Reuses the 1 MiB `MAX_STREAM` truncation + the `run_code` JSON contract (stdout/stderr/exit_code/elapsed_ms/truncated/limit_hit).
- `run_session_shell`: still `subprocess.run` per call but `cwd=session["cwd"]` (D-02 asymmetric); reuses `run_code` via a new additive `cwd=None` param (the stateless path stays `cwd=None`, unchanged).
- Router: `_parse_session_path` prefix-parses `POST /session/{id}/exec/{lang}` (5-part split, `exec` literal, lang in {python,shell}, non-empty id) → falls through to 404 otherwise; `do_POST` tries the session route first, then the static `INTERPRETERS` map. Body read/validation extracted to `_read_exec_request` (shared by both routes); `_drain_body` before the unknown-path 404 avoids a client RST.
- `MAX_BODY_BYTES` / `do_GET /healthz` / timeout/oom/pids machinery all intact. The stateless `/exec/{lang}` path is byte-for-byte behaviourally unchanged.
- `sandbox/sidecar_test.py` (stdlib `unittest` + `urllib`, runs the real `Handler` on a loopback `ThreadingHTTPServer`): python namespace persistence across two same-session calls; session isolation; shell cwd re-application (`cd` does not persist); python exception captured + server survives the next call; unknown path (bad lang + empty id) → 404; stateless path unchanged + registers no session.

### Task 2 — DockerRunner session exec path + additive Runner/Result extension — commit 114dff27
- `Runner` interface (sandbox.go) gains `RunPythonSession`/`RunShellSession(ctx, sessionID, code string, timeoutSec int) (Result, error)`. The stateless `RunPython`/`RunShell` signatures and the four original `Result` fields (`Stdout/Stderr/ExitCode/ElapsedMs` + `Truncated/LimitHit`) are unchanged — the 2a callers (`execute.go`, `exec.go`) compile untouched.
- `DockerRunner.sessionExec(ctx, sessionID, lang, code, timeoutSec)` POSTs to `/session/{id}/exec/{lang}` (D-05 HTTP-only; the container lifecycle is the SessionManager's in 08-05, never driven here). Refactored the old `exec` + the new `sessionExec` to both delegate to a shared `execPath(ctx, path, code, timeoutSec)` carrying the timeout-clamp / `responseGrace` ctx / `ErrSandboxUnreachable` + `ErrSandboxProtocol` taxonomy; `post()` now takes a full path (no `/exec/`-prefix duplication).
- `docker_test.go` (httptest, no live container): `TestRunner_SessionExecPathAndWire` asserts the python+shell session URLs, the wire body (`code` + `timeout_sec`, `0` → config default), and the 1:1 `Result` decode; `TestRunner_SessionExecProtocolError` asserts the session path reuses `ErrSandboxProtocol` on a 502. The single unit-tier goleak `TestMain` is kept; new tests are added as functions.

## Verification Results

- `python -m py_compile sandbox/sidecar.py sandbox/sidecar_test.py` — exits 0.
- `python sandbox/sidecar_test.py` — 6 tests OK (persistence, isolation, shell cwd, exception capture + survival, 404, stateless-unchanged).
- sidecar grep: `/session/` route present; NO `IPython|ipykernel|jupyter|import zmq` (stdlib-only, D-03) — grep-clean.
- `go vet ./...` clean; `go build ./...` OK (2a callers `execute.go`, `exec.go` compile unchanged).
- `go test -run 'TestRunner' ./internal/sandbox/` — ok. `go test ./internal/sandbox/ ./internal/agent/tools/` — ok. `go test -race ./internal/sandbox/` — ok (goleak TestMain green).
- Unit-tier coverage `internal/sandbox` 66.7% (the live-sidecar tier behind `//go:build sandbox_integration` covers the container behaviour, same split as 2a; the session HTTP wire/timeout/taxonomy are all unit-covered here).
- All modified files ≤600 LOC (docker.go 223, sandbox.go 39, docker_test.go 332, sidecar.py 337, sidecar_test.py 119).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] execute_test.go fakeRunner must satisfy the extended Runner interface**
- **Found during:** Task 2 (`go vet` after extending the `Runner` interface)
- **Issue:** `*fakeRunner` (the `sandbox.Runner` test double in `internal/agent/tools/execute_test.go`) no longer implemented the interface (missing `RunPythonSession`/`RunShellSession`), breaking the build of the `tools` package.
- **Fix:** Added the two session methods to `fakeRunner` (recording a new `gotSession` field, replaying the canned Result). The 2a `execute` tool does not call them yet — `session_id` stays inert until 08-08; this is interface satisfaction only.
- **Files modified:** internal/agent/tools/execute_test.go
- **Commit:** 114dff27

**2. [Rule 2 - Missing hygiene] drain the request body before the unknown-path 404**
- **Found during:** Task 1 (the stdlib unittest's 404 case hit a `ConnectionResetError`)
- **Issue:** `do_POST` returned 404 for an unknown path without reading the (already-sent) request body, leaving the keep-alive connection half-read → a client-side RST. Pre-existing in the 2a static-map path too, but exercised for the first time by the new router's empty-id/bad-lang 404s.
- **Fix:** Added `_drain_body()` (reads + discards up to `MAX_BODY_BYTES`) before the 404. Correct HTTP hygiene; fixes the test and avoids RST in the real container.
- **Files modified:** sandbox/sidecar.py
- **Commit:** a3fd6058

### Scope notes (NOT deviations)
- `Result` was intentionally NOT extended with advisory `{risk_tier, gate_recommended}` fields — the plan assigns those to 08-08 (the execute-tool wiring). This plan only extended the `Runner` interface, per scope control.
- The stdlib unittest's shell-cwd case pins the session cwd to the test directory (an OS-portable existing dir) and asserts the second `pwd` equals the first after a `cd ..`, rather than a hardcoded Linux `/tmp` — so it is green on the Windows dev host as well as the Linux container. The asymmetric-persistence contract (D-02) is what is asserted, not a specific path.

## Known Stubs

None — the sidecar + runner logic is complete. Live container persistence (`x=42` survives a real `docker run` interpreter across two calls) is asserted in 08-09 (the live tier), as the plan specifies; that is a deferred validation, not a stub.

## Authentication Gates

None.

## Self-Check: PASSED

- Files: sandbox/sidecar.py, sandbox/sidecar_test.py, internal/sandbox/{sandbox.go,docker.go,docker_test.go}, 08-07-SUMMARY.md — all FOUND.
- Commits: a3fd6058 (Task 1), 114dff27 (Task 2) — both FOUND in git log.
