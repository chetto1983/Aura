---
phase: 08-sandbox-2b-session-bound
plan: 10
subsystem: infra
tags: [sandbox, docker, session-routing, egress-proxy, bind-mount, gvisor, security]

# Dependency graph
requires:
  - phase: 08-08
    provides: "per-conv SessionManager (runArgv hardening + egress-gated bridge/proxy-env), DockerRunner HTTP exec, WorkspaceManager os.Root cascade"
  - phase: 08-09
    provides: "live sandbox_integration tier (sessions_live/workspace/network criteria tests) + NewSessionProxy CONNECT logic; the per-session proxy bind/lifecycle open seam this plan closes"
provides:
  - "Process-scoped shared SessionEndpoint resolver (package-level defaultSessionEndpoint) routing 2b session exec to each per-conv container's OWN loopback port (HTTP, D-05) — the byte-identical integrationRunner+liveManager pair routes by construction"
  - "Socket-free dockerClient.port() 5th lifecycle verb + 127.0.0.1-line parse filter (parsePublishedHostPort)"
  - "Per-conv workspace bind-mount at /workspace RW with nosuid,nodev,noexec via the runc-compatible local-volume o=bind form (verified 0x100e mount)"
  - "Shell-capable session image with a valid /workspace cwd (Dockerfile fallback dir + sidecar missing-cwd degrade)"
  - "Boot lazy-recreate fix + reaper/recovery endpoint teardown (no stale URL after reap/recover)"
  - "Boot-started egress forward proxy at the bridge gateway (cmd/aura/sandbox_proxy.go) with goleak-clean shutdown"
affects: [08-verify, 08-secure, sandbox, CAP-02]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Process-scoped shared package-level resolver singleton (defaultSessionEndpoint) — lets two independently-constructed objects share routing state by construction without an injection seam"
    - "runc-compatible nosuid/nodev/noexec bind-mount via the local-volume driver o=bind,<flags> form (Docker -v / --mount type=bind reject the kernel flags; the local-volume o= form reaches the daemon mount syscall with MS_NOSUID|MS_NODEV|MS_NOEXEC|MS_BIND)"

key-files:
  created:
    - internal/sandbox/sessions_endpoint.go
    - internal/sandbox/sessions_endpoint_test.go
    - cmd/aura/sandbox_proxy.go
  modified:
    - internal/sandbox/docker.go
    - internal/sandbox/docker_test.go
    - internal/sandbox/sessions.go
    - internal/sandbox/sessions_docker.go
    - internal/sandbox/sessions_test.go
    - internal/sandbox/sessions_reaper.go
    - internal/sandbox/sessions_recovery.go
    - internal/sandbox/sessions_integration_test.go
    - sandbox/Dockerfile
    - sandbox/sidecar.py
    - cmd/aura/chat.go
    - cmd/aura/exec.go
    - compose.yaml

key-decisions:
  - "BLOCKER-1 fix: a PROCESS-SCOPED SHARED package-level defaultSessionEndpoint (not composition-root injection) so the unmodified integrationRunner+liveManager pair routes by construction"
  - "nosuid,nodev,noexec implemented via the runc-compatible local-volume o=bind form (verified live: daemon issues a 0x100e mount) — NOT deferred"
  - "Single boot proxy shares one convID-scoped DNS-pin cache for all conversations (FLAG 3 / AR-08-02, v1 deviation from D-25); per-Acquire proxies deferred"
  - "ErrSessionEndpointUnknown from the standalone `aura exec` CLI maps to exit 70 (unreachable env fault), not 71"

patterns-established:
  - "Pattern: process-scoped shared resolver singleton for cross-object routing by construction"
  - "Pattern: runc-compatible security-flag bind-mount via local-volume o= driver"

requirements-completed: []  # CAP-02 closes only at the Task-6 human Gate-3 sign-off (not by this code-only SUMMARY)

# Metrics
duration: ~75 min
completed: 2026-06-03
---

# Phase 8 Plan 10: CAP-02 Gap-Closure — Per-Conv-Container Session Exec Routing Summary

**Process-scoped shared SessionEndpoint routes 2b session exec to each per-conversation container's own loopback port (HTTP per D-05), with a nosuid,nodev,noexec workspace bind-mount, a shell-capable /workspace cwd, boot lazy-recreate + reaper endpoint teardown, and a boot-started egress proxy — closing the gap a live Gate-3 run exposed without touching any integration-criteria test.**

## Performance

- **Duration:** ~75 min
- **Completed:** 2026-06-03
- **Tasks:** 5 of 6 (Task 6 is a blocking-human live Gate-3 checkpoint — PENDING)
- **Files modified:** 15 (3 created, 12 modified)

## Accomplishments
- **Per-conv exec routing (Task 1):** `sessions_endpoint.go` defines the `SessionEndpoint` resolver (Register/URLFor/Unregister, sync.Map, -race-safe), `ErrSessionEndpointUnknown`, and the package-level `defaultSessionEndpoint` singleton. `NewDockerRunner` defaults its resolver to that var; `sessionExec` resolves the per-conv URL (miss → typed error, no fallthrough to the shared r.url); the stateless path is byte-identical against r.url. `create` resolves the published loopback port via the new socket-free `dockerClient.port()` verb and registers the per-conv URL; `runArgv` publishes `127.0.0.1:0:18901`. BLOCKER-1 proven at unit tier: a bare runner + a nil-Endpoint manager round-trip through the shared default.
- **Workspace bind-mount (Task 2):** `WorkspaceEnsurer` seam threaded into `create`; `runArgv` mounts the host workspace at `/workspace` RW with **nosuid,nodev,noexec** via the runc-compatible local-volume `o=bind` form (verified live — daemon issues a 0x100e mount).
- **Shell + /workspace cwd (Task 3):** Dockerfile creates an owned-by-65532 `/workspace` fallback mount-point before the USER switch; `sidecar.py` degrades a missing cwd to `/tmp` with a stderr note instead of an opaque exit 127 (a genuinely missing interpreter still maps to 127). Stays stdlib-only + ast.parse-clean.
- **Boot lazy-recreate + teardown (Task 4):** `evict` Unregisters the endpoint after LoadAndDelete (no stale URL after reap); `RecoverOnBoot` clears m.sessions + zeroes m.count (clamped) + Unregisters under capMu so the next Acquire lazy-recreates.
- **Boot composition (Task 5):** the conversations-Cleaner WorkspaceManager is shared into the SessionManager; `startSandboxProxy` starts one boot-scoped egress proxy at the bridge-gateway addr (parsed from `AURA_SANDBOX_PROXY_ENV`) when the allowlist is non-empty, goleak-clean; egress env contract + the BLOCKER-2 "egressless bridge is NOT internal:true" note documented in `sandbox_proxy.go` + `compose.yaml`.

## Task Commits

1. **Task 1: per-conv exec routing + shared SessionEndpoint + published port** — `4d1ec1f1` (feat)
2. **Task 2: workspace bind-mount nosuid,nodev,noexec** — `cff34d38` (feat)
3. **Task 3: session /workspace cwd (Dockerfile + sidecar degrade)** — `bbffdd94` (fix)
4. **Task 4: boot lazy-recreate + reaper/recovery endpoint teardown** — `4fa53d64` (fix)
5. **Task 5: boot composition (shared WorkspaceManager + egress proxy)** — `b221fd43` (feat)

**Task 6:** PENDING — blocking-human live Gate-3 checkpoint (no code; doc closeout happens only after the operator types "approved").

## Files Created/Modified
- `internal/sandbox/sessions_endpoint.go` — SessionEndpoint resolver + defaultSessionEndpoint singleton + parsePublishedHostPort 127.0.0.1 filter.
- `internal/sandbox/sessions_endpoint_test.go` — resolver round-trip/race, BLOCKER-1 shared-default proof, lookup-miss typed error, IPv6-multiline parse, stateless-path-unchanged.
- `internal/sandbox/docker.go` — SessionEndpoint field + default; sessionExec resolves per-conv URL; execPath/post take an explicit base URL; NewDockerRunnerWithEndpoint.
- `internal/sandbox/docker_test.go` — session wire-contract tests register the convID endpoint at the test server (session path now resolves via the resolver, not r.url).
- `internal/sandbox/sessions.go` — dockerClient.port() interface verb; WorkspaceEnsurer seam; create resolves port + registers endpoint + EnsureDir; runArgv publishes the loopback port + mounts the workspace; workspaceMountSpec.
- `internal/sandbox/sessions_docker.go` — dockerCLI.port() impl (socket-free, LookPath-gated fixed argv).
- `internal/sandbox/sessions_test.go` — fakeDocker.port(); workspace-mount-argv unit test.
- `internal/sandbox/sessions_reaper.go` — evict Unregisters the endpoint after LoadAndDelete.
- `internal/sandbox/sessions_recovery.go` — RecoverOnBoot clears in-memory sessions + count + Unregisters endpoints (lazy-recreate).
- `internal/sandbox/sessions_integration_test.go` — recoveryDocker.port() interface-satisfaction stub (no criteria-logic change).
- `sandbox/Dockerfile` — /workspace fallback mount-point owned by 65532 before USER.
- `sandbox/sidecar.py` — missing-cwd degrade to /tmp.
- `cmd/aura/chat.go` — share WorkspaceManager into SessionManager; start egress proxy; ProxyEnv from env.
- `cmd/aura/exec.go` — ErrSessionEndpointUnknown → exit 70.
- `cmd/aura/sandbox_proxy.go` — startSandboxProxy + parseSandboxProxyEnv + proxyListenAddr.
- `compose.yaml` — egress env contract + BLOCKER-2 + FLAG-3/AR-08-02 notes in the session-posture comment.

## Decisions Made
- **BLOCKER-1 (process-scoped shared resolver):** the live tier builds the runner (`integrationRunner`) and manager (`liveManager`) as two independent do-not-touch objects with no injection seam. A package-level `defaultSessionEndpoint` that `create` registers into (when Deps.Endpoint is nil) and `NewDockerRunner` defaults to makes them share routing state by construction. Composition-root-only injection would regress the currently-passing 1a/1c via ErrSessionEndpointUnknown.
- **nosuid,nodev,noexec — implemented, NOT deferred (FLAG 4):** Docker `-v` and `--mount type=bind` reject the kernel flags ("invalid mode" / "must be a key=value pair"). The runc-compatible local-volume driver `o=bind,nosuid,nodev,noexec` form reaches the daemon mount syscall with flags `0x100e` (MS_NOSUID|MS_NODEV|MS_NOEXEC|MS_BIND) — verified via a Go `exec.Command` probe in WSL (the daemon issued the 0x100e mount; the only failure was the Docker-Desktop-on-Windows host-path-sharing layer, which does not apply to the Linux/CI Gate-3 host). The inner `o=` commas are CSV-quoted so the `--mount` tokenizer keeps them as one option.
- **Single boot proxy DNS-pin scope (FLAG 3 / AR-08-02):** one convID-scoped pin cache for all conversations, a v1 deviation from D-25 per-conversation rebind isolation, accepted because the pin TTL is short and the allowlist is operator-opt-in. Documented in code; the **08-SECURITY register entry is a Task-6 post-approval closeout**.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] recoveryDocker.port() interface-satisfaction stub in sessions_integration_test.go**
- **Found during:** Task 1 (dockerClient grew the port() verb)
- **Issue:** The `dockerClient` interface gained `port()`; the `recoveryDocker` no-op fake in the `db_integration`-tagged `sessions_integration_test.go` must implement it or that tier fails to compile.
- **Fix:** Added a 3-line no-op `port()` stub returning a canned loopback addr — the same category as the plan-sanctioned `fakeDocker.port()` edit (a fake satisfying the interface), with NO change to any integration-criteria assertion/logic.
- **Files modified:** internal/sandbox/sessions_integration_test.go
- **Verification:** `go test -tags db_integration` + `go test -tags 'sandbox_integration db_integration'` both compile; the three byte-identical criteria test files (sessions_live/workspace/network) are untouched (git diff confirmed).
- **Committed in:** 4d1ec1f1 (Task 1 commit)

**2. [Rule 1 - Bug] docker_test.go session wire-contract tests register the endpoint**
- **Found during:** Task 1 (the session path now resolves via the resolver, not r.url)
- **Issue:** Two untagged unit tests (TestRunner_SessionExecPathAndWire / _ProtocolError) POSTed against r.url, an assumption obsolete under the D-05 per-conv routing.
- **Fix:** Each test now registers its convID at the test server via a PRIVATE NewSessionEndpoint + NewDockerRunnerWithEndpoint, exercising the new correct routing (not modifying a criteria test; updating obsolete unit assumptions with justification).
- **Files modified:** internal/sandbox/docker_test.go
- **Verification:** go test -race ./internal/sandbox/ green.
- **Committed in:** 4d1ec1f1 (Task 1 commit)

**3. [Rule 1 - Bug] exec.go ErrSessionEndpointUnknown → exit 70**
- **Found during:** Task 5 (TestRunExec_SessionLive expected exit 70, got 71)
- **Issue:** `aura exec --session <id>` from the standalone CLI (no SessionManager Acquire) can never have a registered endpoint; the new ErrSessionEndpointUnknown fell to the default exit 71 (protocol fault).
- **Fix:** Mapped ErrSessionEndpointUnknown to exit 70 (unreachable env fault) alongside ErrSandboxUnreachable — accurate classification preserving the test's intent (the flag drives the session runner).
- **Files modified:** cmd/aura/exec.go
- **Verification:** go test -race ./cmd/aura/ green.
- **Committed in:** b221fd43 (Task 5 commit)

**4. [Scope/ordering] AR-08-02 + 08-SECURITY register edits deferred to Task-6 post-approval**
- **Found during:** Task 5
- **Issue:** Task 5 acceptance names "record AR-08-02 in 08-SECURITY"; the orchestrator's blocking-human constraint reserves ALL 08-SECURITY register edits (AR-08-02, the 5th-verb socket carve-out, threats_open flip) for the Task-6 post-"approved" closeout.
- **Fix:** AR-08-02 + the BLOCKER-2 fact are documented in code (sandbox_proxy.go header + compose.yaml comment) now; the 08-SECURITY register entry is surfaced for the human at Task 6 and edited only after sign-off. No silent scope reduction.
- **Files modified:** cmd/aura/sandbox_proxy.go, compose.yaml (code-side docs); 08-SECURITY deferred.
- **Verification:** N/A (doc ordering).
- **Committed in:** b221fd43 (Task 5 commit)

---

**Total deviations:** 4 (2 bug, 1 blocking, 1 scope/ordering). **Impact on plan:** All necessary for compile/correctness; no scope creep. The integration-criteria test files remain byte-identical (the BLOCKER-1 by-construction routing held).

## Issues Encountered
- The nosuid/nodev/noexec mount form required runtime probing (Docker rejects the flags on `-v` and `--mount type=bind`). The runc-compatible local-volume `o=bind` form was verified live (0x100e mount). No runtime-compat blocker — the flags were ACCEPTED, not deferred. (The Windows-Docker-Desktop path-sharing layer failed the probe's actual mount, but that layer is absent on the Linux/CI Gate-3 host.)

## User Setup Required
**The live egress criterion needs operator-exported env + a host proxy started before the egress tests** (the live tests do NOT boot the composition root). See the Task-6 verification steps below. Env contract: AURA_SANDBOX_EGRESS_NETWORK, AURA_SANDBOX_PROXY_ENV, AURA_SANDBOX_SESSION_SECCOMP, AURA_SANDBOX_NETWORK_ALLOW_HOSTS, AURA_RUN_DIR, AURA_SANDBOX_SESSION_IMAGE.

## Next Phase Readiness
- **Tasks 1-5 code-complete; unit tier green (`go test -race ./internal/sandbox/ ./cmd/aura/`).**
- **Task 6 PENDING — blocking-human live Gate-3:** the operator must run the live `sandbox_integration db_integration` tier + `make quality-full` in WSL with the stack up (rebuild the session image + repoint AURA_SANDBOX_SESSION_IMAGE; start the host egress proxy at the bridge gateway). On "approved", the doc closeout runs: extend T-08-05-EOP-SOCKET for the 5th verb, record AR-08-02, confirm the nosuid/nodev/noexec status, flip 08-SECURITY threats_open → 0, mark CAP-02 complete in ROADMAP.md + REQUIREMENTS.md.
- **CAP-02 is NOT closed by this SUMMARY** — it closes only at the human Gate-3 sign-off.

---
*Phase: 08-sandbox-2b-session-bound*
*Completed (code, Tasks 1-5): 2026-06-03*

## Self-Check: PASSED
- Created files exist on disk: sessions_endpoint.go, sessions_endpoint_test.go, sandbox_proxy.go, 08-10-SUMMARY.md.
- All 5 task commits present in git log: 4d1ec1f1, cff34d38, bbffdd94, 4fa53d64, b221fd43.
- Unit tier green: `go test -race ./internal/sandbox/ ./cmd/aura/`.
- Integration-criteria test files byte-identical (sessions_live/workspace/network); sessions_integration_test.go carries only the recoveryDocker.port() interface stub.
- Task 6 (live Gate-3 + CAP-02 doc closeout) PENDING human sign-off — STATE/ROADMAP/REQUIREMENTS NOT advanced.
