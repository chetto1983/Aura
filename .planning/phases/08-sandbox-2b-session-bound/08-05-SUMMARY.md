---
phase: 08-sandbox-2b-session-bound
plan: 05
subsystem: sandbox
tags: [session-control-plane, gvisor, reaper, os.Root, openat2, symlink-escape, boot-recovery]
requires:
  - "08-02: errors.go sentinels (ErrSessionCapReached, ErrWorkspaceQuotaExceeded), config fields, sqlc sandbox_sessions queries + migration 0008"
provides:
  - "sandbox.SessionManager: Acquire/Release, docker-CLI lifecycle (D-05), per-session lock (D-07), idle-TTL reaper (D-03), hard cap (D-12), boot recovery (D-06)"
  - "sandbox.WorkspaceManager: EnsureDir, WalkSize (os.Root quota), CheckQuota, PurgeConversationDir (manual no-follow openat cascade, D-13/D-14)"
  - "sandbox.ConversationCleaner shape + *WorkspaceManager impl (wired in main.go 08-08; avoids sandbox->conversations import cycle)"
  - "sandbox.NewDockerCLI() production dockerClient; sandbox.ErrPrivacyModeEgressDenied / ErrDockerCLIAbsent sentinels"
affects:
  - "08-07: per-session HTTP exec path consumes SessionManager (Acquire/Release)"
  - "08-08: main.go wires WorkspaceManager as conversations.Delete's cleaner + session seccomp profile + RecoverOnBoot"
  - "08-09: live integration assertions (container reuse, TTL reap, boot recovery against real docker+PG)"
tech-stack:
  added: []
  patterns:
    - "sync.Map per-conversation + per-session mutex control plane (RESEARCH Pattern 1)"
    - "ctx-bound reaper goroutine, Close waits on done channel (goleak-clean)"
    - "testing/synctest virtual-clock TTL test (GA in go1.26.4)"
    - "os.OpenRoot/openat2 RESOLVE_BENEATH walks; manual post-order no-follow cascade (RESEARCH Pattern 4)"
    - "narrow injected interfaces (dockerClient, sessionStore) so unit tier runs with no daemon and no Postgres"
key-files:
  created:
    - internal/sandbox/sessions.go
    - internal/sandbox/sessions_reaper.go
    - internal/sandbox/sessions_recovery.go
    - internal/sandbox/sessions_docker.go
    - internal/sandbox/sessions_test.go
    - internal/sandbox/sessions_integration_test.go
    - internal/sandbox/workspace.go
    - internal/sandbox/workspace_test.go
    - internal/sandbox/cleanup.go
  modified: []
decisions:
  - "Privacy error ErrPrivacyModeEgressDenied + ErrDockerCLIAbsent defined in sessions_docker.go (a NEW file) rather than the 08-02-owned errors.go, to keep parallel-wave 08-07 conflict-free; still errors.Is-friendly."
  - "sessions.go split into sessions.go + sessions_reaper.go + sessions_recovery.go + sessions_docker.go (each <600 LOC, deep-refactor-on-touch ceiling)."
  - "os.Root has no ReadDir in go1.26 (RESEARCH example assumed it); used root.Open(dir)+Readdirnames(-1) instead. Same openat confinement."
  - "Reaper always starts (it is the only idle-eviction path); removed the speculative StartReaper opt-out field as dead surface."
metrics:
  duration: "~50m"
  completed: 2026-06-03
  tasks: 2
  files: 9
---

# Phase 8 Plan 05: Session Control Plane + Symlink-Safe Workspace Walkers Summary

Per-`conversation_id` gVisor container control plane (`SessionManager`) with docker-CLI lifecycle (never the socket), a per-session lock, a goleak-clean idle-TTL reaper, a hard concurrency cap, and boot recovery — plus `os.Root`/openat2 host walkers (`WorkspaceManager`) that quota-sum regular files and cascade-delete an attacker-planted symlink as a link, never following it to the target.

## What Shipped

### Task 1 — SessionManager control plane (`feat 184c6a8a`)
- `Acquire(ctx, sessionID)` parses the conv-id string → uuid at the boundary (landmine #1), reuses-or-creates a container, and LOCKS the per-session mutex (D-07). `Release` unlocks.
- Container creation shells `docker run` with **all 2a hardening flags replicated** (Pitfall 6 — gVisor + flags are NOT inherited by an ad-hoc `docker run`): `--runtime=<cfg.SandboxRuntime> --user 65532:65532 --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges --security-opt seccomp=<profile> --pids-limit 64 --memory 512m --cpus 1.0 --ulimit nofile=64`. Lifecycle is `exec.CommandContext`, `exec.LookPath("docker")`-gated, fixed argv, `//nolint:gosec // fixed argv, no socket` — **no `/var/run/docker.sock` anywhere** (D-05).
- Hard cap under `capMu`: a 6th concurrent **distinct** session → `ErrSessionCapReached`, **no silent LRU** (D-12). Cap check + count increment + map store are atomic so two racing distinct-conv Acquires cannot both slip past `maxN-1`.
- Reaper (`sessions_reaper.go`) is a `time.Ticker` sweep bound to the ctx passed to `NewSessionManager`; `Close()` cancels and waits on a `done` channel (goleak-clean). Idle > TTL → `docker stop/rm` + `MarkTerminated` + map delete + count decrement. Eviction `TryLock`s the session so it never races an in-flight exec.
- Boot recovery (`sessions_recovery.go`): `ListActive` → `MarkTerminated` each (lazy recreate next Acquire, D-06) + a `docker ps -aq --filter name=aura-sandbox-sess-` stray sweep that `docker rm -f`s crash leftovers (A7).
- Privacy-mode fail-fast (D-10): `local-only` + non-empty allowlist → `ErrPrivacyModeEgressDenied` at Acquire.
- Narrow injected `dockerClient` + `sessionStore` interfaces (`*sqlc.Queries` satisfies the latter) so the unit tier runs with **no Docker daemon and no Postgres**.

### Task 2 — WorkspaceManager os.Root walks + cleanup interface (`feat 3f37c0bf`)
- `WalkSize` opens the workspace with `os.OpenRoot` (openat2 `RESOLVE_BENEATH`), recurses via `root.Open`+`Readdirnames`, `Lstat`s each entry and sums **regular files only** — a symlinked dir pointing outside contributes 0 (D-13). `CheckQuota` → `ErrWorkspaceQuotaExceeded`.
- `PurgeConversationDir` is a **manual post-order no-follow openat cascade** over the whole `<id>/` dir (workspace/ AND `.result` spillover — co-tenancy landmine #4). `root.Remove` unlinks the LINK, never the target. **No `os.RemoveAll`** on the attacker-controlled subtree (D-14; `os.Root` has no `RemoveAll` per golang/go#67002, and `os.RemoveAll` is TOCTOU-symlink-susceptible per #52745).
- `cleanup.go` declares the `ConversationCleaner` shape + a compile-time `var _ ConversationCleaner = (*WorkspaceManager)(nil)` assertion. The interface is DEFINED in `conversations` and wired by `main.go` (08-08), so **`sandbox` does NOT import `conversations`** (import-cycle guard). `go list -deps ./internal/sandbox | grep conversations` → 0.
- Conv-id validated locally (duplicated guard — sandbox cannot import `conversations.validateID`).

## Tests (what ran vs deferred)

| Test | Tier | Status |
|------|------|--------|
| `TestSessions_HardCapEnforced` (6th → ErrSessionCapReached, no LRU) | unit -race | **PASS** |
| `TestSessions_ReuseSameConversation` (1 docker run, not recreate) | unit -race | **PASS** |
| `TestSessions_ConcurrentSerialized` (8 goroutines, max 1 in crit) | unit -race | **PASS** |
| `TestReaper_EvictsAfterTTL` (testing/synctest virtual clock) | unit -race | **PASS** |
| `TestSessions_RunArgvHardeningFlags` (all gVisor+hardening flags; no socket) | unit -race | **PASS** |
| `TestSessions_PrivacyModeFailFast` (local-only+allowlist → typed err) | unit -race | **PASS** |
| `TestWorkspace_SymlinkEscapeCascade` (host secret survives, link removed) | unit -race | **PASS on Linux/WSL (incl. CI=1)** |
| `TestWorkspace_QuotaWalkSize` (sums 30B, no symlink traversal) | unit -race | **PASS on Linux/WSL (incl. CI=1)** |
| `TestWorkspace_QuotaExceeded` (32B > 16B quota → err) | unit -race | **PASS (all platforms)** |
| `TestSessions_BootRecoveryMarksTerminated` | db_integration | **compiles + skips locally (no live PG here); runs in CI/WSL** |

- Full `internal/sandbox` unit tier is **green under `-race` and goleak-clean** on both Windows and Linux (verified `CI=1 go test -race ./internal/sandbox/` under WSL).
- **No new `TestMain`** — the new tests share the existing unit-tier goleak `TestMain` in `docker_test.go` (`//go:build !sandbox_integration`).

## Deviations from Plan

### Auto-fixed / adjusted

**1. [Rule 3 - Blocking] `os.Root` has no `ReadDir` in go1.26**
- **Found during:** Task 2 — the RESEARCH code example used `root.ReadDir(dir)`, but `go doc os.Root` confirms no such method in go1.26.4.
- **Fix:** Used `root.Open(dir)` + `*File.Readdirnames(-1)` (still openat-confined under the root). Same RESOLVE_BENEATH guarantee.
- **Files:** internal/sandbox/workspace.go

**2. [Rule 3 - Blocking] Windows symlink privilege blocks the escape gate locally**
- **Found during:** Task 2 — `os.Symlink` on Windows needs `SeCreateSymbolicLinkPrivilege`; the test failed on the dev host.
- **Fix:** Added a `mustSymlink` helper that `t.Skip`s when symlink creation is unsupported BUT `t.Fatal`s under `$CI` (no-skip-as-green). The deployment + CI target is Linux where the gate runs — verified PASS under WSL even with `CI=1`.
- **Files:** internal/sandbox/workspace_test.go

**3. Privacy + docker-absent sentinels placed in a new file, not the 08-02 errors.go**
- **Reason:** `errors.go` is referenced by the parallel 08-07 plan; defining `ErrPrivacyModeEgressDenied`/`ErrDockerCLIAbsent` in the new `sessions_docker.go` keeps parallel-wave files conflict-free. Still `errors.Is`-friendly. (Scope-faithful — D-10 names a typed sentinel, not a specific file.)

**4. Removed a speculative `StartReaper` opt-out field**
- During Task 1 cleanup the reaper-start branch was always-true (dead logic). The reaper is the only idle-eviction path, so it always starts; the field was removed to avoid dead surface (CLAUDE.md no-dead-code-on-touch).

## Known Stubs

**`runArgv` seccomp default `"unconfined"`** (`sessions.go`): when `SeccompProfile` is empty the argv falls back to `seccomp=unconfined`. This is an **intentional placeholder** — the connect-allowed session seccomp profile is shipped by 08-07/08-08 (compose + main.go wiring pass the real `SeccompProfile` into `SessionDeps`). The fallback is documented inline; it is not reached in production once 08-08 wires the profile. No data-flow-to-UI stub; no hardcoded empty result.

## Threat Flags

None. All security surface (docker-lifecycle carve-out, gVisor flag replication, symlink-escape cascade, privacy fail-fast, session/quota DoS caps) is enumerated in the plan's `<threat_model>` STRIDE register (T-08-05-EOP-SOCKET/RUNTIME/SYMLINK, DOS-CAP/QUOTA, INFO-PRIVACY) and implemented as specified.

## Self-Check: PASSED
- All 9 created files exist on disk (verified).
- Both task commits exist: `184c6a8a` (SessionManager), `3f37c0bf` (WorkspaceManager).
- No new TestMain; no edits to docker.go/sandbox.go; no `docker.sock`; `sandbox` does not import `conversations` (`go list -deps` → 0).
- reaper/cap/serialization + symlink-escape tiers run green under `-race`, goleak-clean; db-tier boot-recovery deferral noted honestly.
