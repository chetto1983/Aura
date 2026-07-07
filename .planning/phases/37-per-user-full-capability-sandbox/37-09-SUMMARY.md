---
phase: 37-per-user-full-capability-sandbox
plan: 09
subsystem: agent-tools
tags: [sandbox, tool-routing, shell_bg, background-exec, fail-closed, docker, exec-stream, d-02, go]

# Dependency graph
requires:
  - phase: 37-per-user-full-capability-sandbox
    provides: "SandboxRouter.Route(ctx)/Strict() + the sandboxUnavailableResult fail-CLOSED deny shape + the route-then-branch pattern (37-07); DockerBackend.Exec (secret-scrubbed, ExecCreate/ExecAttach/stdcopy/ExecInspect) (37-04)"
provides:
  - "A CONCRETE streamed/detached background-exec verb on *DockerBackend (ExecStream + ExecStreamHandle): ExecCreate/ExecAttach stream demuxed stdout/stderr into a registry buffer, Wait blocks for the exit code via ExecInspect, Kill signals the job's process group from a SEPARATE box exec — NOT a 6th Backend verb (D-02 keeps the seam at 5)"
  - "SandboxRouter.ExecStream — the tool-facing streaming passthrough, resolved structurally via backendAs[backgroundExecStreamer] (off the core Backend interface)"
  - "BackgroundShells holds a box exec-stream handle (bgShell.box) under strict vs a host *exec.Cmd under lenient; startBox is the routed analog of start, sharing newShell/register so the owner/authority layer is byte-for-byte identical"
  - "shell_exec background branch routes: routed ⇒ startBox (box), routed-but-box-failed START ⇒ sandbox_unavailable deny (never a host process), !routed ⇒ host start unchanged"
  - "cmd/aura wires sandboxRouter into NewBackgroundShells (nil for pool-free/CLI = host-direct)"
affects: [sandbox-tool-routing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Streamed/detached box exec via a pump goroutine that demuxes stdcopy into the registry buffer and, on Kill, signals the job's process group from a separate box exec then joins the copy goroutine (goleak-clean, one owned goroutine tree)"
    - "PID-file box kill: the wrapper sh writes $$ before running the (compound/pipeline) command; Kill reads it and sends SIGTERM to the process group AND the process (best-effort tree kill) — Docker has no kill-exec verb"
    - "Non-blocking Kill: closes a once-guarded channel; the actual box-side SIGTERM runs in the pump goroutine, so sh.cancel() stays non-blocking even under the registry lock (sweepExpiredLocked)"
    - "Optional streaming capability via structural type-assertion (backendAs) keeps ExecStream OFF the core Backend interface (D-02) — the DGX agent-sandbox E2B impl stays a valid Backend unmodified"
    - "Shared newShell/register between the host start and the routed startBox — the owner/authority binding, TTL, cap, and random-id machinery are identical on both paths; only the process handle changes"

key-files:
  created:
    - internal/sandbox/usersandbox/docker_backend_stream_test.go
    - internal/agent/tools/shell_bg_sandbox_test.go
    - internal/agent/tools/shell_bg_sandbox_docker_test.go
  modified:
    - internal/sandbox/usersandbox/docker_backend_exec.go
    - internal/sandbox/usersandbox/router_tools.go
    - internal/agent/tools/shell_bg.go
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/shell_bg_owner_test.go
    - internal/agent/tools/shell_bg_test.go
    - internal/agent/tools/shell_bg_ttl_test.go
    - internal/agent/tools/shell_exec_approval_test.go
    - internal/agent/tools/tool_hardening_test.go
    - cmd/aura/main.go

key-decisions:
  - "The streamed/detached verb is a CONCRETE *DockerBackend method (ExecStream), surfaced through SandboxRouter.ExecStream via backendAs[backgroundExecStreamer] — NEVER added to the Backend interface (D-02 locks it at Resolve/Exec/Suspend/Resume/Stop). TestBackend_StreamVerbNotOnInterface asserts a Backend value cannot call ExecStream while *DockerBackend can."
  - "The box kill uses a PID-file + a SEPARATE box exec sending SIGTERM to the process group AND the process (best-effort tree kill) because the Docker exec API has NO kill-exec verb and detaching the stream alone leaves the process running (37-RESEARCH A5). The wrapper is `echo $$ > pidfile; <command>` (NO `exec` prefix) so compound lines / builtins / pipelines run as children in the same group; runc starts each exec as a session leader, so $$ is the PGID."
  - "Kill is non-blocking (closes a once-guarded channel); the actual box-side SIGTERM runs in the pump goroutine. This preserves the registry invariant that sh.cancel() is safe to call under b.mu (sweepExpiredLocked) — a slow box teardown never wedges the lock."
  - "startBox shares newShell + register with the host start (DEEP REFACTOR ON TOUCH) so the owner/authority model (random ids, (identity,session) binding, TTL, authorizeCaller, localOwnerID) is byte-for-byte unchanged — only the process handle behind bgShell (host *exec.Cmd → bgShell.box exec-stream) differs."
  - "Liveness/exit is derived from the streamed-EOF + reaper done-flag (mirroring the host cmd.Wait model) with ExecInspect supplying the exit code in Wait(); a polled Alive() method was intentionally omitted to stay deadcode-clean — the status the model sees flips to 'killed' the instant shell_kill sets sh.killed, exactly as on the host path."

patterns-established:
  - "Routed background job: shell_exec routes at the top (37-07's Route), and on the background+routed branch calls BackgroundShells.startBox(boxHandle,...) which runs the job via Router.ExecStream and stores the box handle in the registry entry; a box START failure returns sandboxUnavailableResult (never a host process)."

requirements-completed: []

# Coverage metadata
coverage:
  - id: D1
    description: "A routed-but-box-failed background START denies (sandbox_unavailable) with NO host process spawned and no half-registered job — fail-CLOSED (T-37-09-FAILOPEN / D-09 / GATE-01)."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_sandbox_test.go#TestShellBg_FailClosedStart"
        status: pass
    human_judgment: false
  - id: D2
    description: "The streamed/detached exec is a CONCRETE DockerBackend verb; the Backend interface still declares EXACTLY Resolve/Exec/Suspend/Resume/Stop (a Backend value cannot call ExecStream; *DockerBackend can) — D-02 preserved (T-37-09-D02WIDEN)."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/docker_backend_stream_test.go#TestBackend_StreamVerbNotOnInterface,TestRouterExecStream_UnsupportedBackendErrors"
        status: pass
    human_judgment: false
  - id: D3
    description: "The owner/authority layer (random unguessable 128-bit ids, (identity,session) binding, per-job TTL, owner-only authorizeCaller with nil caps) is IDENTICAL after the box-routing change — startBox and start share newShell (T-37-09-AUTHREG / MUSR-03/04)."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_sandbox_test.go#TestShellBg_OwnerModelUnchanged"
        status: pass
    human_judgment: false
  - id: D4
    description: "Under a live daemon a strict-profile background shell_exec job runs INSIDE the box as a streamed box exec: shell_poll returns the box's streamed output and shell_kill stops the box exec; Shutdown joins every reaper/pump goroutine (no box exec / waiter leak, T-37-09-LEAK)."
    requirement: "SBX-01"
    verification:
      - kind: integration
        ref: "internal/agent/tools/shell_bg_sandbox_docker_test.go#TestShellBg_RunsInBox (docker_integration; compiles + skips locally, t.Fatal under $CI)"
        status: unknown
    human_judgment: true
    rationale: "Compiles + skips cleanly locally (dockerd unreachable in the Windows worktree via npipe). The live box-background + poll/kill + goleak pass runs on native-Linux dockerd at phase validation (WSL/CI)."

# Metrics
duration: ~1h15m
completed: 2026-07-07
status: complete
---

# Phase 37 Plan 09: Route background shell jobs into the per-identity box Summary

**Under a strict profile a background `shell_exec` job (`background:true` → `shell_poll` → `shell_kill`) now runs INSIDE the per-identity box as a streamed box exec, never a host `*exec.Cmd` with a process group: `shell_exec`'s background branch mirrors 37-07's route-then-branch — `routed` calls `BackgroundShells.startBox`, which runs the job via a CONCRETE streamed/detached verb on the DockerBackend (`ExecStream` → `ExecStreamHandle`: `ExecCreate`/`ExecAttach` stream the demuxed stdout/stderr into the registry buffer, `Wait` blocks for the exit code via `ExecInspect`, `Kill` signals the job's process group from a SEPARATE box exec because the Docker exec API has no kill-exec verb), stores the box handle in `bgShell.box`, and returns a `shell_id`; a routed-but-box-failed START returns the fail-CLOSED `sandbox_unavailable` deny (never a host process); `!routed` (dev/local_trusted, CLI/manifest) keeps the host path byte-for-byte. The streaming verb is NOT a 6th `Backend` verb — D-02 locks the seam at Resolve/Exec/Suspend/Resume/Stop so the DGX `agent-sandbox` E2B impl drops in unmodified; the router surfaces it structurally via `backendAs[backgroundExecStreamer]`. The owner/authority layer (random unguessable ids, `(identity, session)` binding, TTL reaper, `authorizeCaller`, `localOwnerID`) is byte-for-byte unchanged — `startBox` and the host `start` share `newShell`/`register`; only the process handle behind `bgShell` changed. SBX-01's full routed tool set (all five, D-11) is now complete.**

## Performance

- **Duration:** ~1h15m
- **Completed:** 2026-07-07
- **Tasks:** 2
- **Files created:** 3 | **Files modified:** 10

## Accomplishments

- **The streamed/detached box-exec verb (D-02-preserving).** `*DockerBackend.ExecStream(ctx, h, ExecRequest, io.Writer) (*ExecStreamHandle, error)` (docker_backend_exec.go): `ExecCreate` (AttachStdout/Stderr) + `ExecAttach`, a `pump` goroutine demuxes `stdcopy.StdCopy` into the caller's writer (the `bgShell`), `Wait` blocks on the stream end then reads the exit code via `ExecInspect`, `Kill` requests termination. The verb is CONCRETE-only — `var _ Backend = (*fakeBackend)(nil)` still holds and a `Backend` value cannot type-assert to it (`TestBackend_StreamVerbNotOnInterface`).
- **The box kill mechanism (T-37-09-LEAK).** Docker has no kill-exec API, and detaching the stream leaves the process running. So the wrapper shell writes its PID (`echo $$ > /tmp/.aura-bg-<token>.pid; <command>` — no `exec` prefix, so compound/builtin/pipeline commands run as children in the same group), and `Kill` fires a SEPARATE box exec sending `SIGTERM` to the process group `-$P` AND the process `$P` (best-effort tree kill). `Kill` is non-blocking (closes a once-guarded channel); the SIGTERM runs in the `pump` goroutine, so `sh.cancel()` stays safe to call under the registry lock. The `pump` owns its one nested copy goroutine and joins it on kill — goleak-clean.
- **The routed background registry (SBX-01 core).** `BackgroundShells` gained `Router` (the same instance the synchronous tools carry) and `bgShell.box` (the streamed handle under strict). `startBox` mints the crypto id, builds the shell via the SHARED `newShell`, `register`s it atomically (prune + TTL-sweep + cap), then runs `Router.ExecStream` and stores the handle — a box START failure `remove`s the entry and returns an error the tool maps to `sandboxUnavailableResult`. `shell_poll` reads the streamed buffer + status; `shell_kill` flips `sh.killed` and fires `sh.cancel()` → the box handle's `Kill`.
- **Owner/authority preserved byte-for-byte (T-37-09-AUTHREG).** `newShell`/`register` are shared by `start` and `startBox`, so a routed job binds `(identity, session)` and TTL exactly as a host job does; `authorizeCaller`, `localOwnerID`, and the random-id minting are untouched (`TestShellBg_OwnerModelUnchanged`).
- **Composition wiring.** `cmd/aura` threads `sandboxRouter` into `tools.NewBackgroundShells(sandboxRouter)`; a nil router (pool-free/CLI manifest, dev/local_trusted) keeps every background job host-direct. 37-07's `ShellExec.Router` wiring and the `shell_poll`/`shell_kill` registrations (same registry) are preserved.

## Task Commits

1. **Task 1: concrete streamed/detached box-exec verb (D-02-preserving) + route background jobs (start/poll/kill) + fail-CLOSED start** — `648b911a` (feat)
2. **Task 2: wire the sandbox router into NewBackgroundShells at the composition root** — `7d52c1b5` (feat)

## Interface Handoff

```
// Concrete streamed background-exec verb (NOT on the Backend interface — D-02):
func (b *DockerBackend) ExecStream(ctx, h BoxHandle, req ExecRequest, out io.Writer) (*ExecStreamHandle, error)
func (s *ExecStreamHandle) Wait() (exitCode int, err error) // blocks on stream end, exit via ExecInspect
func (s *ExecStreamHandle) Kill()                            // non-blocking; box-side SIGTERM in the pump goroutine

// Tool-facing router passthrough (resolved structurally, off the core Backend seam):
func (r *SandboxRouter) ExecStream(ctx, h BoxHandle, req ExecRequest, out io.Writer) (*ExecStreamHandle, error)

// Routed background registry:
func NewBackgroundShells(router *usersandbox.SandboxRouter) *BackgroundShells
func (b *BackgroundShells) startBox(callerCtx, h BoxHandle, command, dir string, env []string) (string, error)
```

## Deviations from Plan

### Auto-fixed / scoped adjustments

**1. [Rule 3 - Blocking] `NewBackgroundShells` signature change forced mechanical caller updates**
- **Found during:** Task 1 / Task 2.
- **Issue:** Threading the router into `NewBackgroundShells` (the plan's Task 2 intent) changed its signature, breaking every existing `NewBackgroundShells()` caller in the tools test suite.
- **Fix:** Updated the 11 call sites in `shell_bg_owner_test.go`, `shell_bg_test.go`, `shell_bg_ttl_test.go`, `shell_exec_approval_test.go`, `tool_hardening_test.go` to `NewBackgroundShells(nil)` (host path — exactly what those tests exercise). Mechanical; no test behavior changed.
- **Files modified:** the 5 test files above (not in the plan's `files_modified`).
- **Committed in:** `648b911a` (Task 1).

**2. [Rule 3 - Blocking] Split the docker_integration test into its own build-tagged file**
- **Found during:** Task 1.
- **Issue:** The plan lists a single `shell_bg_sandbox_test.go`, but `TestShellBg_RunsInBox` needs `//go:build docker_integration` while `TestShellBg_FailClosedStart`/`TestShellBg_OwnerModelUnchanged` must run locally (untagged) — one file can carry only one build constraint.
- **Fix:** `shell_bg_sandbox_test.go` (untagged: fail-closed + owner-model) + `shell_bg_sandbox_docker_test.go` (tagged: runs-in-box). Mirrors 37-07's `shell_exec_sandbox_test.go` / `shell_exec_sandbox_docker_test.go` split.
- **Committed in:** `648b911a` (Task 1).

**3. [Design choice, documented] `Alive()` omitted; liveness via streamed-EOF + reaper**
- **Found during:** Task 1.
- **Issue:** The must-have suggested `Alive()/ExitCode()` via `ExecInspect`. A polled `Alive()` would be dead code (the tools-package status is derived from the reaper's done-flag, not a poll of the box), and the `deadcode` gate flags unused exported methods.
- **Fix:** `Wait()` reads the exit code via `ExecInspect` (satisfying "exit via ExecInspect"); liveness is the streamed-EOF + `sh.done` model — IDENTICAL to how the host `cmd.Wait` path derives status, and the status the model sees flips to "killed" the instant `shell_kill` sets `sh.killed`. No `Alive()` added → deadcode-clean.

**4. [Refactor on touch] Extracted `newShell`/`register`/`remove` from `start`**
- **Found during:** Task 1.
- **Issue:** `startBox` and `start` need identical owner-binding + atomic cap/prune/sweep registration.
- **Fix:** Factored `newShell` (owner/TTL/bufcap binding) + `register` (atomic prune+sweep+cap+insert) + `remove` out of `start`; both paths share them, so the authority model cannot diverge. `shell_bg.go` stays at 528 LOC (≤600).

**Total:** 2 blocking (mechanical/tagging), 1 documented design choice, 1 refactor-on-touch. **Impact:** No change to the plan's intent (route background jobs into the box via a concrete D-02-preserving streamed verb + fail-CLOSED start + composition wiring).

## Requirements Status

- **SBX-01** (full-capability box; host tools ACTUALLY executing inside the box) — this plan delivers the FIFTH and most-invasive routed tool (background shell jobs), completing D-11's routed five (the four non-background tools + send_file landed in 37-07). `requirements-completed: []` — REQUIREMENTS.md is left for the orchestrator/verifier to reconcile the multi-plan SBX-01 after the wave (consistent with 37-04/37-05/37-07).

## Known Stubs

None. The box kill is a real box-side SIGTERM to the job's process group via a separate exec (not a placeholder); `ExecStream`/`ExecStreamHandle` are fully implemented and exercised by the docker_integration tier.

## Threat Flags

None — no new trust-boundary surface beyond the plan's `<threat_model>`. The four registered threats each have a mitigation test: T-37-09-FAILOPEN (FailClosedStart), T-37-09-AUTHREG (OwnerModelUnchanged), T-37-09-D02WIDEN (StreamVerbNotOnInterface), T-37-09-LEAK / T-37-09-HOSTBG (RunsInBox, docker_integration).

## Issues Encountered

- **Live docker_integration + `-race` run deferred to CI/WSL.** dockerd is unreachable in this Windows worktree (npipe is not stdlib-dialable), so `TestShellBg_RunsInBox` skips locally (the sanctioned local skip; `t.Fatal` under `$CI`). `go test -race` needs CGO/gcc (absent locally; WSL/CI per CLAUDE.md). The suite compiles under `-tags docker_integration`; the live box-background + poll/kill + goleak pass runs at phase validation on the native-Linux stack — consistent with 37-04/37-07.

## Next Phase Readiness

- SBX-01's routed tool set is complete (all five, D-11). 37-08 (docs/ADR/compose/bench-soak/validation) is unaffected (no file overlap).
- Blockers: none. The live docker_integration + `-race` + goleak run is a phase-validation (WSL/CI) step, not a code blocker.

## Self-Check: PASSED

- Created files exist: `docker_backend_stream_test.go`, `shell_bg_sandbox_test.go`, `shell_bg_sandbox_docker_test.go`, `37-09-SUMMARY.md` — all FOUND on disk.
- Task commits exist: `648b911a`, `7d52c1b5` — both FOUND in `git log`.
- Plan `<verification>` re-run: `go build ./...` green; `go vet ./...` clean; `go test ./internal/agent/tools/` green (incl. TestShellBg_FailClosedStart / TestShellBg_OwnerModelUnchanged); `go test ./internal/sandbox/usersandbox/` green (incl. TestBackend_StreamVerbNotOnInterface / TestRouterExecStream_UnsupportedBackendErrors); `go test ./cmd/aura/` green; `go build ./cmd/aura/` green (NewBackgroundShells wired). `-tags docker_integration` suite compiles + skips locally (dockerd unreachable). No touched file > 600 LOC (shell_bg.go 528, shell_exec.go 585, docker_backend_exec.go 215).

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-07*
