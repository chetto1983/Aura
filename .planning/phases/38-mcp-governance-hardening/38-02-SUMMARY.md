---
phase: 38-mcp-governance-hardening
plan: 02
subsystem: infra
tags: [go, bufio, syscall, process-management, mcp, stdio, dos-mitigation]

# Dependency graph
requires: []
provides:
  - "Shared internal/procgroup package (SetProcessGroup/KillProcessGroup, per-OS: Unix Setpgid+Kill(-pid,SIGKILL), Windows CREATE_NEW_PROCESS_GROUP+taskkill /F /T), reused by internal/agent/tools and internal/mcp"
  - "Bounded stdio JSON-RPC frame reads in internal/mcp/client.go: bufio.Scanner + .Buffer(_, AURA_MCP_STDIO_MAX_FRAME) replaces the unbounded bufio.Reader.ReadBytes('\\n') (F-034 closed)"
  - "Deterministic over-cap-frame transport abort: ErrStdioFrameTooLarge wraps ErrTransport (IsTransportError true), no resync, no large allocation"
  - "Process-tree kill on Close/abort: mcp.Open calls procgroup.SetProcessGroup before spawn; killProcess delegates to procgroup.KillProcessGroup (F-035 closed)"
affects: ["38-05 (mount-timeout two-context split builds on the setProcessGroup(cmd) wiring installed here in mcp.Open)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "bufio.Scanner + .Buffer(initialBuf, maxFrame+1) for a bounded line read with a deterministic bufio.ErrTooLong abort (the +1 accounts for the trailing delimiter byte the scanner must additionally buffer to find at the exact cap boundary — verified empirically against go1.26.5's bufio/scan.go, not just read from the illustrative research sketch)"
    - "Shared per-OS process-group-kill package (internal/procgroup), reused verbatim by internal/agent/tools and internal/mcp instead of duplicating or introducing Windows Job Objects"

key-files:
  created:
    - internal/procgroup/procgroup_unix.go
    - internal/procgroup/procgroup_windows.go
    - internal/procgroup/procgroup_test.go
    - internal/procgroup/procgroup_unix_test.go
    - internal/procgroup/procgroup_windows_test.go
    - internal/mcp/client_frame_test.go
  modified:
    - internal/mcp/client.go
    - internal/mcp/client_test.go
    - internal/mcp/client_open_test.go
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/shell_bg.go

key-decisions:
  - "scanner.Buffer's max-token-size parameter must be maxFrame+1, not maxFrame — the Scanner's cap bounds the whole buffered chunk needed to FIND the newline delimiter (one byte past the frame content), so maxFrame alone would spuriously reject a frame of exactly the configured cap (verified empirically, not assumed from the research sketch)."
  - "internal/agent/tools/shell_bg.go (not listed in the plan's files_modified) also called the local setProcessGroup/killProcessGroup being deleted — updated it too, else the build breaks."
  - "Cross-OS SysProcAttr field assertions cannot live in one untagged test file (Setpgid vs CreationFlags are different, OS-conditional struct fields) — split into procgroup_unix_test.go/procgroup_windows_test.go alongside the plan-specified procgroup_test.go."
  - "Grandchild-survival test proves the whole tree via a heartbeat file, not PID-liveness syscalls — fully portable across OSes with no new build-tagged production code."

patterns-established:
  - "internal/procgroup as the single shared process-group/tree lifecycle helper for any package that spawns a long-lived subprocess it must be able to fully reap."

requirements-completed: [MCPH-05, MCPH-06]

coverage:
  - id: D1
    description: "Stdio frames are capped at AURA_MCP_STDIO_MAX_FRAME (default 1 MiB, envutil.IntDefault); the exact boundary (1048575/1048576 accepted, 1048577 rejected) is proven, and an over-cap frame aborts the whole transport deterministically as an IsTransportError with no resync for the next call."
    requirement: MCPH-05
    verification:
      - kind: unit
        ref: "internal/mcp/client_frame_test.go#TestReadResponseBlockingAcceptsFrameAtCap"
        status: pass
      - kind: unit
        ref: "internal/mcp/client_frame_test.go#TestReadResponseBlockingAcceptsFrameJustUnderCap"
        status: pass
      - kind: unit
        ref: "internal/mcp/client_frame_test.go#TestReadResponseBlockingAbortsOversizedFrame"
        status: pass
      - kind: unit
        ref: "internal/mcp/client_frame_test.go#TestReadResponseBlockingNoResyncAfterOversizedFrame"
        status: pass
      - kind: unit
        ref: "internal/mcp/client_frame_test.go#TestReadResponseBlockingClosedStdoutIsTransportError"
        status: pass
    human_judgment: false
  - id: D2
    description: "Close/killProcess terminates the WHOLE stdio process tree (Unix process-group SIGKILL, Windows taskkill /F /T) via the shared internal/procgroup package — a spawned grandchild does not survive Close."
    requirement: MCPH-06
    verification:
      - kind: unit
        ref: "internal/mcp/client_open_test.go#TestCloseKillsGrandchildProcessTree"
        status: pass
      - kind: unit
        ref: "internal/procgroup/procgroup_test.go, procgroup_unix_test.go, procgroup_windows_test.go"
        status: pass
    human_judgment: false

duration: ~35min
completed: 2026-07-18
status: complete
---

# Phase 38 Plan 02: Bounded Stdio Frame Cap + Process-Tree Kill Summary

**Replaced the MCP stdio client's unbounded `bufio.Reader.ReadBytes('\n')` with a `bufio.Scanner`-based 1 MiB frame cap that aborts the transport deterministically on overflow, and replaced its single-PID `cmd.Process.Kill()` with a shared `internal/procgroup` process-tree kill reused from `internal/agent/tools`.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-07-18T10:18:00Z
- **Tasks:** 3
- **Files modified:** 13 (6 created, 5 modified, 2 deleted)

## Accomplishments
- Extracted the already-proven, CI-shipped per-OS process-group-kill pair out of `internal/agent/tools` into a new shared `internal/procgroup` package (`SetProcessGroup`/`KillProcessGroup`), deleting the duplicate local copies; both `shell_exec.go` and `shell_bg.go` now call it.
- Closed F-034: `internal/mcp/client.go`'s `readResponseBlocking` now reads through a `bufio.Scanner` bounded by `AURA_MCP_STDIO_MAX_FRAME` (default 1 MiB, `envutil.IntDefault`); a hostile/misbehaving stdio MCP server can no longer force unbounded memory growth with an unterminated line.
- Closed the D-09 "never resync" requirement: an over-cap frame surfaces as `ErrStdioFrameTooLarge` (wraps `ErrTransport`, so `IsTransportError` is true), tears the transport down via the existing `abortTransport()` (kill+close), and a subsequent read on the same client also errors — proven against a well-formed frame queued right behind the oversized one.
- Closed Pitfall #4 (Scanner's EOF-swallowing default): a clean `Scan()==false, Err()==nil` is now explicitly synthesized into a transport error wrapping `io.ErrUnexpectedEOF`, never a silent `(nil, nil)`.
- Closed F-035: `mcp.Open` now calls `procgroup.SetProcessGroup(cmd)` before `cmd.Start()`; `killProcess` delegates to `procgroup.KillProcessGroup(c.cmd)` — a spawned grandchild process no longer survives `Close()`. Proven with a new "grandchild" `TestHelperProcess` mode that spawns a real OS-level grandchild and confirms (via a heartbeat file) it stops running the instant `Close()` returns.

## Task Commits

Each task was committed atomically (Tasks 2 and 3 are TDD — RED then GREEN):

1. **Task 1: Extract the per-OS process-group-kill pair into a shared internal/procgroup package** - `201c3d0f` (feat)
2. **Task 2: Bounded stdio frame cap with deterministic transport abort (D-08/D-09)**
   - RED - `c536f9ff` (test)
   - GREEN - `e0af16a4` (feat)
3. **Task 3: Process-tree kill in Open+killProcess (D-10)**
   - RED - `9668c17e` (test)
   - GREEN - `0dba2dc8` (feat)

_No refactor commits were needed — each GREEN implementation landed clean on first pass with no follow-up cleanup._

## Files Created/Modified
- `internal/procgroup/procgroup_unix.go` - `SetProcessGroup`/`KillProcessGroup` (Unix: Setpgid + Kill(-pid,SIGKILL)), moved verbatim from `internal/agent/tools/shell_exec_unix.go`
- `internal/procgroup/procgroup_windows.go` - `SetProcessGroup`/`KillProcessGroup`/`taskkillProcessMissing` (Windows: CREATE_NEW_PROCESS_GROUP + taskkill /F /T), moved verbatim from `internal/agent/tools/shell_exec_windows.go`
- `internal/procgroup/procgroup_test.go` - cross-OS smoke tests (nil-Process no-op, SysProcAttr non-nil)
- `internal/procgroup/procgroup_unix_test.go` - Unix-specific SysProcAttr.Setpgid assertion
- `internal/procgroup/procgroup_windows_test.go` - Windows-specific CreationFlags + taskkillProcessMissing table test
- `internal/agent/tools/shell_exec.go` - calls `procgroup.SetProcessGroup`/`procgroup.KillProcessGroup` instead of local funcs
- `internal/agent/tools/shell_bg.go` - same call-site update (background shell spawn path)
- `internal/agent/tools/shell_exec_unix.go` - **deleted** (moved to internal/procgroup)
- `internal/agent/tools/shell_exec_windows.go` - **deleted** (moved to internal/procgroup)
- `internal/mcp/client.go` - `Client.stdout` is now `*bufio.Scanner`; `readResponseBlocking` bounded-read + abort; `Open` calls `procgroup.SetProcessGroup`; `killProcess` delegates to `procgroup.KillProcessGroup`; new `defaultStdioMaxFrame`/`ErrStdioFrameTooLarge`/`newStdioScanner`
- `internal/mcp/client_test.go` - `newClientForTest` builds the scanner instead of `bufio.NewReader`
- `internal/mcp/client_frame_test.go` - new: exact 1 MiB boundary table test, no-resync test, closed-stdout test
- `internal/mcp/client_open_test.go` - new `TestHelperProcess` "grandchild"/"grandchild-child" modes + `TestCloseKillsGrandchildProcessTree`

## Decisions Made
- **`.Buffer()`'s max parameter is `maxFrame+1`, not `maxFrame`.** Empirically verified (a standalone scratch Go program against this exact toolchain's `bufio/scan.go`, then confirmed by the production test suite) that passing `maxFrame` directly would reject a frame of exactly the configured cap one byte early, because `bufio.Scanner`'s max-token-size bounds the whole buffered chunk needed to *find* the delimiter — which is one byte past the frame content itself. The plan's own acceptance criteria (1048575/1048576 accepted, 1048577 rejected) require the `+1`; RESEARCH.md's Code Example #3 was explicitly marked "illustrative... not final code" and would have gotten this off-by-one wrong if implemented literally.
- **Grandchild-survival test uses a heartbeat file, not a PID-liveness syscall.** Avoids introducing new OS-specific production *or test* syscall surface beyond what Task 1 already established; the grandchild continuously appends to a file while alive, and the test asserts the file stops growing the instant `Close()` returns — fully portable, verified on both Windows (native) and Linux (WSL, `-race`).
- **Cross-OS `SysProcAttr` field assertions split into build-tagged test files.** `syscall.SysProcAttr`'s fields differ per OS (`Setpgid` vs `CreationFlags`), so a single untagged test file referencing both would fail to compile on either platform; `procgroup_unix_test.go`/`procgroup_windows_test.go` each assert their own OS's field.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `internal/agent/tools/shell_bg.go` also referenced the deleted local process-group functions**
- **Found during:** Task 1
- **Issue:** The plan's `files_modified` list only named `shell_exec.go` for the caller-side update, but `shell_bg.go` (the background-shell spawn path) also calls the same unexported `setProcessGroup`/`killProcessGroup` functions being deleted from `shell_exec_unix.go`/`shell_exec_windows.go`. Leaving it untouched would have broken the build (`undefined: setProcessGroup`/`killProcessGroup`).
- **Fix:** Updated `shell_bg.go`'s two call sites to `procgroup.SetProcessGroup`/`procgroup.KillProcessGroup`, identically to `shell_exec.go`.
- **Files modified:** internal/agent/tools/shell_bg.go
- **Verification:** `go build ./...` clean; `go test ./internal/agent/tools/... -race` (WSL) green.
- **Committed in:** 201c3d0f (Task 1 commit)

**2. [Rule 3 - Blocking] Cross-platform `SysProcAttr` structural test assertions require build-tagged files**
- **Found during:** Task 1
- **Issue:** The plan named only `internal/procgroup/procgroup_test.go`, but asserting `SetProcessGroup`'s exact per-OS `SysProcAttr` wiring (`Setpgid` on Unix, `CreationFlags` on Windows) in one untagged file cannot compile cross-platform — those are different, OS-conditional struct fields in Go's stdlib `syscall` package.
- **Fix:** Added `internal/procgroup/procgroup_unix_test.go` (`//go:build !windows`) and `internal/procgroup/procgroup_windows_test.go` (`//go:build windows`) alongside the plan-specified cross-platform `procgroup_test.go` (which covers the OS-agnostic nil-Process and non-nil-SysProcAttr assertions).
- **Files modified:** internal/procgroup/procgroup_unix_test.go, internal/procgroup/procgroup_windows_test.go (new)
- **Verification:** `go test ./internal/procgroup/... -race` green on both Windows (native) and Linux (WSL).
- **Committed in:** 201c3d0f (Task 1 commit)

**3. [Rule 1 - Bug] gosec G204 on the moved `taskkill` subprocess call**
- **Found during:** Task 1 (post-move lint pass)
- **Issue:** `golangci-lint` flagged `exec.Command("taskkill", "/F", "/T", "/PID", pid)` (G204: subprocess launched with a variable). This was invisible before the move because `.golangci.yml` excludes the *entire* `internal/agent/tools` package from linting ("pre-rewrite skeleton"); the new `internal/procgroup` package has no such exclusion.
- **Fix:** Added a `//nolint:gosec` with a one-line justification comment, matching the exact style already established in `internal/mcp/client.go` for the same class of finding (the PID is `strconv.Itoa` of a PID this package itself spawned, never external input).
- **Files modified:** internal/procgroup/procgroup_windows.go
- **Verification:** `golangci-lint run ./internal/procgroup/...` reports 0 issues on both Windows and WSL Linux.
- **Committed in:** 201c3d0f (Task 1 commit)

**4. [Rule 1 - Bug, in the reference sketch, not the plan] `.Buffer()`'s max parameter off-by-one**
- **Found during:** Task 2 (GREEN implementation)
- **Issue:** RESEARCH.md's Code Example #3 (explicitly marked "illustrative... not final code") sketched `c.scanner.Buffer(make([]byte, 0, 4096), maxFrame)`. Empirical verification (a scratch Go program run against this repo's exact go1.26.5 toolchain, then cross-checked by reading `bufio/scan.go` directly) showed this rejects a frame of exactly `maxFrame` content bytes — contradicting the plan's own explicit acceptance criteria ("a 1048576-byte frame is accepted").
- **Fix:** Pass `maxFrame+1` to `.Buffer()`, with a comment documenting why (the scanner's max token size must additionally cover the trailing delimiter byte).
- **Files modified:** internal/mcp/client.go
- **Verification:** `TestReadResponseBlockingAcceptsFrameAtCap`/`...JustUnderCap`/`...AbortsOversizedFrame` all pass, pinning the exact 1048575/1048576/1048577 boundary from the plan's acceptance criteria.
- **Committed in:** e0af16a4 (Task 2 GREEN commit)

---

**Total deviations:** 4 auto-fixed (2 Rule 3 blocking, 1 Rule 1 lint bug, 1 Rule 1 reference-sketch bug)
**Impact on plan:** All four were necessary for a green build/lint or for correctly satisfying the plan's own acceptance criteria. No scope creep — no unrequested features were added.

## Issues Encountered
- **`go test -race` requires cgo; this Windows session has no gcc/w64devkit toolchain on PATH.** Resolved by running the full `-race` matrix under WSL Ubuntu (which has a working gcc 15 + `CGO_ENABLED=1`) against the same worktree files via the `/mnt/d/...` mount, in addition to native (non-race) Windows runs — both platforms' build-tagged files (`_unix`/`_windows`) got real compiler + test coverage this way, not just the host GOOS. Documented per this plan's `<project_validation>` fallback instruction.
- **A RED-phase verification run of `TestCloseKillsGrandchildProcessTree` correctly left a leaked helper process running** (expected: the pre-fix single-PID kill cannot reap the grandchild by construction). Found and force-killed it via `Get-CimInstance Win32_Process` / `Stop-Process` before proceeding to GREEN; confirmed no leaked `mcp.test.exe` processes remained on either Windows or WSL afterward.
- **A test-authoring bug** (the no-resync test's second, well-formed frame was sized too small for its own JSON envelope) surfaced immediately as a `t.Fatalf` from the test's own builder helper during the RED run; fixed before drawing any conclusions from the RED result.

## User Setup Required

None - no external service configuration required. `AURA_MCP_STDIO_MAX_FRAME` is an optional, silently-defaulted env knob (1 MiB) per this phase's Tier C convention (RESEARCH Assumption A3) — no action needed unless an operator wants to change it.

## Next Phase Readiness
- `internal/procgroup`'s `SetProcessGroup`/`KillProcessGroup` are now available for 38-05's two-context mount-timeout work to reuse for the handshake-timeout kill path (Pitfall #2's fix) without any further extraction.
- `mcp.Open`'s `procgroup.SetProcessGroup(cmd)` call (installed by this plan, right before `cmd.Start()`) is the exact wiring point 38-05's `cmd.Cancel` idiom will attach to — this plan deliberately did not add `cmd.Cancel` itself (that is 38-05's concern, per Pitfall #2).
- No blockers. `AURA_MCP_STDIO_MAX_FRAME`/`AURA_MCP_MOUNT_TIMEOUT`/`AURA_MCP_PROBE_TIMEOUT` remain Tier C (read inside `internal/mcp`, not registered in `config_knobs.go`) per RESEARCH Assumption A3 — this plan only consumed the first of the three.

---
*Phase: 38-mcp-governance-hardening*
*Completed: 2026-07-18*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: internal/procgroup/procgroup_unix.go
- FOUND: internal/procgroup/procgroup_windows.go
- FOUND: internal/procgroup/procgroup_test.go
- FOUND: internal/procgroup/procgroup_unix_test.go
- FOUND: internal/procgroup/procgroup_windows_test.go
- FOUND: internal/mcp/client_frame_test.go
- FOUND: internal/mcp/client.go
- FOUND: internal/mcp/client_test.go
- FOUND: internal/mcp/client_open_test.go
- FOUND: internal/agent/tools/shell_exec.go
- FOUND: internal/agent/tools/shell_bg.go
- CONFIRMED DELETED (intentional): internal/agent/tools/shell_exec_unix.go
- CONFIRMED DELETED (intentional): internal/agent/tools/shell_exec_windows.go

**Commits verified to exist (`git log --oneline --all`):**
- FOUND: 201c3d0f (feat: extract internal/procgroup)
- FOUND: c536f9ff (test: RED, bounded stdio frame cap)
- FOUND: e0af16a4 (feat: GREEN, bounded stdio frame cap)
- FOUND: 9668c17e (test: RED, grandchild process-tree kill)
- FOUND: 0dba2dc8 (feat: GREEN, grandchild process-tree kill)

**Plan-level verification re-confirmed:**
- `go build ./...` clean (Windows native).
- `go vet ./...` clean (Windows native).
- `bash scripts/check-file-size.sh` — all 2009 tracked source files within the 600-LOC cap.
- `go test ./internal/mcp/... ./internal/procgroup/... ./internal/agent/tools/... ./internal/agent/mcptools/... -race` green under WSL Linux (cgo available there); native Windows run green without `-race` (no cgo toolchain on PATH in this session — documented under Issues Encountered).
- goleak (`internal/mcp/main_test.go` TestMain) reported no leaked goroutines in either run.
- grep checks: no `ReadBytes` in client.go, no `cmd.Process.Kill()` in client.go, no Job Object import anywhere, no duplicate process-group funcs remaining in internal/agent/tools.
- No leaked OS processes remained on Windows or WSL after the grandchild-kill test runs (verified via `Get-CimInstance Win32_Process` / `ps aux`).
