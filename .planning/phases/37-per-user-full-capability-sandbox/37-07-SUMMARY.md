---
phase: 37-per-user-full-capability-sandbox
plan: 07
subsystem: agent-tools
tags: [sandbox, tool-routing, shell_exec, fs_read, fs_write, send_file, skill-snippet, fail-closed, docker, go]

# Dependency graph
requires:
  - phase: 37-per-user-full-capability-sandbox
    provides: "SandboxRouter.Route(ctx)/Strict() seam + chatEnv.sandboxRouter handle (37-05); DockerBackend.Exec (secret-scrubbed) + MaterializeIn (/skills root) + CopyArtifactsOut (37-04)"
  - phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs
    provides: "skills.SnippetSandboxPath (/skills/<name>/<name>.<ext>) + SnippetInvocation + SnippetHostInvocation"
provides:
  - "Routed shell_exec/fs_read/fs_write/send_file + skill action=use: under a strict profile the four non-background host tools execute INSIDE the per-identity box via the SandboxRouter; nil router (dev/local_trusted, CLI/manifest) keeps the host path byte-for-byte (SC-4)"
  - "sandboxUnavailableResult — the fail-CLOSED deny ToolResult (error:\"sandbox_unavailable\") every routed tool returns on a box failure, mirroring shellApprovalRequiredResult (D-09/GATE-01) — NEVER a host os/exec fallback"
  - "SandboxRouter.Exec / CopyArtifactOut / WriteFile — the tool-facing box passthroughs (router_tools.go); DockerBackend.CopyArtifactsOut / CopyFileIn (materialize.go)"
  - "skillLoader.Snippet extended to (instructions, hostPath, sandboxPath, interpreter, ok); renderSnippetUse picks SandboxPath under SandboxRouter.Strict() else HostPath — action=use executes NOTHING"
  - "send_file box-aware delivery: box /workspace prefix-check BEFORE CopyArtifactsOut, then the host checkWorkspace symlink/size fence re-run over the STAGED copy AFTER copy-out (D-10)"
  - "cmd/aura composition: buildSandboxRouter(cfg) built ONCE in assembleChatEnv (WithMaterializeSources over the skills export dir), threaded onto the 5 tools + backing the reaper; web_* left unrouted (D-11)"
affects: [37-09, sandbox-tool-routing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Route-then-branch at each tool: routed=false ⇒ host path unchanged; routed+err ⇒ fail-CLOSED deny; routed ⇒ box exec/read/write/deliver (never host)"
    - "Host-only pre-checks skipped/re-targeted on the routed branch: shell_exec AG-018 os.Stat(cwd) skipped; fs_read statSizeWithinCap → bounded `head -c <cap+1>` on returned bytes; fs_write deniedSkillsWrite → box-relative deniedBoxSkillsWrite; content-length cap kept (content-based)"
    - "Optional backend capabilities via structural type-assertion (backendAs[T]): CopyArtifactsOut/CopyFileIn stay OFF the core Backend interface so an E2B gateway opts in by method set, keeping backend.go minimal"
    - "tar copy-in for fs_write (CopyToContainer) instead of an in-box heredoc — sidesteps ARG_MAX + quoting/binary limits for large content"
    - "single-router composition: buildSandboxRouter built once in assembleChatEnv so the SAME instance backs the tools AND the idle reaper (no divergent box-handle maps)"

key-files:
  created:
    - internal/agent/tools/sandbox_route.go
    - internal/agent/tools/shell_exec_cwd.go
    - internal/agent/tools/shell_exec_sandbox.go
    - internal/agent/tools/send_file_sandbox.go
    - internal/agent/tools/shell_exec_sandbox_test.go
    - internal/agent/tools/shell_exec_sandbox_docker_test.go
    - internal/sandbox/usersandbox/router_tools.go
  modified:
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/fs_read.go
    - internal/agent/tools/fs_write.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_read.go
    - internal/agent/tools/send_file.go
    - internal/agent/tools/skill_test.go
    - internal/skilladapters/skilladapters.go
    - internal/skilladapters/skilladapters_loader_test.go
    - internal/sandbox/usersandbox/materialize.go
    - cmd/aura/main.go
    - cmd/aura/chat_boot.go
    - cmd/aura/serve_dispatch.go
    - cmd/aura/serve_adapters.go
    - cmd/aura/cache_audit.go
    - cmd/aura/main_test.go
    - cmd/aura/registry_test.go

key-decisions:
  - "Added the router→box passthroughs (SandboxRouter.Exec/CopyArtifactOut/WriteFile) + DockerBackend.CopyArtifactsOut/CopyFileIn (Rule 3 — the tools reach the box ONLY through the router, which did not expose exec/copy). Copy-out/copy-in are resolved structurally (backendAs[T]) rather than widened onto the core Backend interface, keeping backend.go minimal and the router_test fakeBackend untouched."
  - "Corrected the RESEARCH/PATTERNS claim that skill action=use already returns SandboxPath (FALSE): extended skillLoader.Snippet to ALSO return sandboxPath; renderSnippetUse now selects SandboxPath under Strict() vs HostPath. action=use still calls NO backend.Exec."
  - "Built buildSandboxRouter ONCE in assembleChatEnv (signature changed *chatEnv → *config.Config) and threaded it through buildRegistryWithMCP/buildBaseRegistryWithHandles/newSkillTool, rather than the plan's main.go-only Task 2 scope — the registry is constructed before buildDispatch, so the router must exist earlier; one instance must back both the tools and the reaper. Also routes `aura chat` under a strict profile (previously serve-only) — a correct containment extension."
  - "buildSandboxRouter now wires WithMaterializeSources over cfg.SkillExportDir (Dest /skills) — 37-05 explicitly deferred this to 37-07 and the snippet-exec E2E requires it; egress-image wiring is deliberately NOT added here (a separate follow-up, out of lane)."
  - "shell_exec cwd-marker helpers split into shell_exec_cwd.go (refactor-on-touch): shell_exec.go was at 597 LOC and the Router field would breach the 600 cap; the box variant wrapForCwdTrackingBox uses PLAIN pwd (never `pwd -W`, Pitfall 6)."

patterns-established:
  - "Per-tool box routing: hold *usersandbox.SandboxRouter, call Route(ctx) at the top, branch on (routed, err); host-only pre-checks skipped/re-targeted on the routed branch; a box failure returns sandboxUnavailableResult (never host)."

requirements-completed: []

# Coverage metadata
coverage:
  - id: D1
    description: "A routed-but-box-failed shell_exec returns the sandbox_unavailable deny result and the host os/exec path is provably never reached (backend.Exec uncalled) — fail-CLOSED (D-09/GATE-01)."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_sandbox_test.go#TestShellExec_FailClosedNoHostFallback"
        status: pass
    human_judgment: false
  - id: D2
    description: "A routed shell_exec runs the command inside the box via backend.Exec using POSIX /bin/sh with PLAIN pwd (never `pwd -W`), and the box $PWD is tracked from the cwd marker onto the footer/meta; a nil router keeps the host path byte-for-byte."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_sandbox_test.go#TestShellExec_RoutedRunsInBox,TestShellExec_NilRouterHostUnchanged"
        status: pass
    human_judgment: false
  - id: D3
    description: "skill action=use renders the in-box SandboxPath under a strict profile / the HostPath under lenient, and calls NO backend.Exec (skillLoader.Snippet returns sandboxPath sourced from skills.SnippetInvocation)."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_sandbox_test.go#TestSnippetUse_StrictRendersSandboxPath"
        status: pass
    human_judgment: false
  - id: D4
    description: "Routed send_file stages a box /workspace artifact out via CopyArtifactsOut and delivers the staged host-side copy; a box path OUTSIDE /workspace is rejected by the pre-copy prefix-check (never copied out); a box copy-out failure denies."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_sandbox_test.go#TestSendFile_StrictCopiesArtifactOut,TestSendFile_RoutedWorkspaceFence"
        status: pass
    human_judgment: false
  - id: D5
    description: "Routed fs_read reads via a bounded `head -c` box exec (cap on returned bytes, no host stat of a box path) and denies on a box failure; routed fs_write tar-copies content into the box and refuses a /skills write."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_sandbox_test.go#TestFSRead_RoutedReadsInBoxAndFailsClosed,TestFSWrite_RoutedWritesInBoxAndSkillsFence"
        status: pass
    human_judgment: false
  - id: D6
    description: "Under a live daemon a strict-profile shell_exec runs inside the box (host fs untouched), and a snippet MaterializeIn'd at /skills/<name>/... is EXECUTED by the routed shell_exec at the exact SandboxPath action=use rendered — the rendered path and the materialized path agree at RUNTIME."
    requirement: "SBX-01"
    verification:
      - kind: integration
        ref: "internal/agent/tools/shell_exec_sandbox_docker_test.go#TestRoute_StrictExecInBox,TestSnippetExec_RoutedEndToEnd (docker_integration; compiles + skips locally, t.Fatal under $CI)"
        status: unknown
    human_judgment: true
    rationale: "Compiles + skips cleanly locally (dockerd unreachable in the Windows worktree via npipe). The live strict-exec-in-box + snippet-E2E pass runs on native-Linux dockerd at phase validation (WSL/CI)."
  - id: D7
    description: "The router is wired onto shell_exec/fs_read/fs_write/send_file (Router) and the skill tool (SandboxRouter) at the composition root; web_fetch/web_search receive neither (D-11); the pool-free manifest path passes a nil router (host-direct)."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "cmd/aura (buildBaseRegistryWithHandles/newSkillTool threading; grep gate Router:/SandboxRouter: present, web tools unrouted; go build ./cmd/aura + go vet clean)"
        status: pass
    human_judgment: false

# Metrics
duration: ~2h
completed: 2026-07-07
status: complete
---

# Phase 37 Plan 07: Route the non-background host tools into the per-identity box Summary

**Under a strict profile the four non-background host tools — `shell_exec`, `fs_read`, `fs_write`, and skill-snippet execution (`skill` `action=use`) — plus artifact delivery (`send_file`) now execute INSIDE the per-identity box via the `SandboxRouter`, never host `os/exec` / `os.ReadFile`: each tool calls `Route(ctx)` at the top and branches — `routed=false` keeps the host path byte-for-byte (dev/local_trusted, SC-4), `routed+err` returns the fail-CLOSED `sandbox_unavailable` deny `ToolResult` (mirroring `shellApprovalRequiredResult`, D-09/GATE-01), and `routed` runs the command/read/write/deliver in the box via `backend.Exec` / tar `CopyToContainer` / `CopyArtifactsOut`. The host-only pre-checks (shell_exec AG-018 `os.Stat(cwd)`, fs_read `statSizeWithinCap`, fs_write `deniedSkillsWrite`) are skipped or re-targeted on the routed branch so a `/workspace/...` box path is never falsely host-stat'd; `send_file` prefix-checks the box path against the literal `/workspace` root BEFORE copy-out then re-runs the host symlink/size fence over the STAGED copy (D-10). `skill action=use` renders a snippet's in-box `SandboxPath` under `SandboxRouter.Strict()` (the routed `shell_exec` then runs the snippet the box materialized at `/skills/<name>/...`) and the host path otherwise — it executes NOTHING itself. The router is built ONCE at the composition root and threaded onto the 5 tools; `web_fetch`/`web_search` are deliberately left host-side (D-11).**

## Performance

- **Duration:** ~2h (across a session-quota interruption mid-Task 2)
- **Completed:** 2026-07-07
- **Tasks:** 2
- **Files created:** 7 | **Files modified:** 17

## Accomplishments

- **The routed tool branches (SBX-01 core).** Each of `ShellExec`/`FSRead`/`FSWrite`/`SendFile` gained a `Router *usersandbox.SandboxRouter`; the `SkillTool` gained a distinctly-named `SandboxRouter` (W6 — not `Router`, which collides with the unexported `router *ActionRouter`). `shell_exec` runs the cwd-marker-wrapped command via `Router.Exec` (POSIX `/bin/sh`, PLAIN `pwd`); `fs_read` reads via a bounded `head -c <cap+1>` exec (the size cap on returned bytes, no host stat); `fs_write` tar-copies content in via `Router.WriteFile` (CopyToContainer, sidestepping ARG_MAX/quoting) with a box-relative `/skills` fence; `send_file` stages a box `/workspace` artifact out. All fail CLOSED on a box error.
- **The fail-CLOSED deny shape (GATE-01 at the tool layer).** `sandboxUnavailableResult(tool, cause)` mirrors `shellApprovalRequiredResult`: an inline `{error:"sandbox_unavailable", tool, message, detail}` the model self-corrects on — returned whenever `routed=true && err!=nil` (or a box exec/copy infra failure), so the host `os/exec` / `os.ReadFile` path is provably unreachable when routed.
- **Corrected snippet path selection.** The RESEARCH/PATTERNS claim that `action=use` already returns a SandboxPath was FALSE. `skillLoader.Snippet` was extended to `(instructions, hostPath, sandboxPath, interpreter, ok)`; the `skilladapters.Loader` sources `sandboxPath` from `skills.SnippetInvocation` (the `/skills/<name>/<name>.<ext>` root `MaterializeIn` lands the snippet at); `renderSnippetUse` picks `SandboxPath` under `SandboxRouter.Strict()` else `HostPath`. `action=use` calls NO `backend.Exec` — the subsequent routed `shell_exec` runs the materialized snippet.
- **Box-aware send_file (D-10).** The routed branch fences in two steps because `checkWorkspace` is host-only (a box `/workspace/...` path is not a host path): (1) a literal `/workspace` prefix-check BEFORE `CopyArtifactsOut`, then (2) stage the tar stream to a host-readable dir under the run dir and re-run the host `checkWorkspace` symlink/size fence over the STAGED copy before delivery. `checkWorkspace` was refactored to a root-parameterized `fenceWithinRoot` reused by both the host path and the staged copy; the delivery tail extracted to `emitDelivery`.
- **The router→box passthroughs.** `SandboxRouter.Exec`/`CopyArtifactOut`/`WriteFile` (`router_tools.go`) are the tool-facing seam; copy-out/copy-in are resolved structurally (`backendAs[T]`) off `DockerBackend.CopyArtifactsOut`/`CopyFileIn` (`materialize.go`, with `tarSingleFile`), keeping the core `Backend` interface and the 37-05 `fakeBackend` untouched.
- **Composition-root wiring.** `buildSandboxRouter` (now `*config.Config`, `WithMaterializeSources` over `cfg.SkillExportDir`) is built ONCE in `assembleChatEnv` and threaded via `buildRegistryWithMCP → buildBaseRegistryWithHandles → newSkillTool` onto the 5 tools — the SAME instance the idle reaper uses. `web_*` unrouted (D-11); pool-free/manifest paths pass a nil router (host-direct).

## Task Commits

1. **Task 1: route shell_exec/fs_read/fs_write/skill-snippet into the box + box-aware send_file (fail-CLOSED)** — `c6b1a97a` (feat)
2. **Task 2: wire the SandboxRouter onto the 5 box-capable tools at the composition root** — `b560072c` (feat)

## Interface Handoff (for 37-09 / shell_bg)

```
// The deny shape shell_bg* mirrors on a routed-but-box-failed call:
sandboxUnavailableResult(tool string, cause error) ToolResult   // {error:"sandbox_unavailable", ...}

// The router box passthroughs a routed tool calls after Route returns a live handle:
(r *SandboxRouter) Exec(ctx, h, ExecRequest) (ExecResult, error)             // shell_exec / fs_read
(r *SandboxRouter) CopyArtifactOut(ctx, h, boxPath) (io.ReadCloser, error)   // send_file
(r *SandboxRouter) WriteFile(ctx, h, boxPath, content) error                 // fs_write

// The route-then-branch pattern every box-capable tool uses:
h, routed, err := s.Router.Route(ctx)
if routed && err != nil { return sandboxUnavailableResult(name, err), nil }  // fail-CLOSED
if routed { /* run in box via Router.Exec/WriteFile/CopyArtifactOut */ }
// else: host path, byte-for-byte
```

- **37-09 (shell_bg / shell_poll / shell_kill):** background box jobs are OUT OF SCOPE here — `shell_exec`'s `a.Background` branch was left on its existing host path. 37-09 routes the background lifecycle and mirrors `sandboxUnavailableResult` on failure.
- **Snippet path selection:** `skillLoader.Snippet` now returns `sandboxPath`; any future `skillLoader` implementer must satisfy the 5-return signature.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added the router→box passthroughs + DockerBackend copy methods (usersandbox)**
- **Found during:** Task 1.
- **Issue:** The plan's design has each tool call `Route(ctx)` then `backend.Exec` / `CopyArtifactsOut` / a copy-in, but the `SandboxRouter` (37-05) exposed only `Route`/`Strict`/`SuspendIdle` — the private `backend` was unreachable, and the `Backend` interface had no copy-out/copy-in. The tools cannot route without these seams.
- **Fix:** Added `SandboxRouter.Exec`/`CopyArtifactOut`/`WriteFile` (`internal/sandbox/usersandbox/router_tools.go`) — copy-out/copy-in resolved structurally via `backendAs[T]` so the core `Backend` interface stays minimal and the 37-05 `fakeBackend` is untouched — plus `DockerBackend.CopyArtifactsOut`/`CopyFileIn` + `tarSingleFile` in `materialize.go`.
- **Files modified:** internal/sandbox/usersandbox/router_tools.go (new), internal/sandbox/usersandbox/materialize.go.
- **Verification:** `go build`/`go vet ./internal/sandbox/usersandbox/` green; `go test ./internal/sandbox/usersandbox/` green (existing suite unaffected).
- **Committed in:** `c6b1a97a` (Task 1).

**2. [Rule 3 - Blocking] Broadened Task 2 beyond main.go (composition-root ordering)**
- **Found during:** Task 2.
- **Issue:** The plan scoped Task 2 to `cmd/aura/main.go`, but the tool registry is constructed in `assembleChatEnv` BEFORE `buildDispatch` (where 37-05 built the router), so the router does not yet exist at registry-build time; and one instance must back both the tools AND the reaper.
- **Fix:** Changed `buildSandboxRouter(*chatEnv)` → `buildSandboxRouter(*config.Config)`, built it once in `assembleChatEnv` (chat_boot.go), removed the re-build from `buildDispatch` (serve_dispatch.go), and threaded the router param through `buildRegistryWithMCP`/`buildBaseRegistryWithHandles`/`newSkillTool` (main.go, serve_adapters.go), updating the callers (cache_audit.go, main_test.go, registry_test.go). This also routes `aura chat` under a strict profile (previously serve-only) — a correct containment extension.
- **Files modified:** cmd/aura/{main.go, chat_boot.go, serve_dispatch.go, serve_adapters.go, cache_audit.go, main_test.go, registry_test.go}.
- **Verification:** `go build ./...`, `go vet ./...`, `go build ./cmd/aura/` green; `go test ./cmd/aura/` green; grep gate (Router:/SandboxRouter: present, web tools unrouted) passes.
- **Committed in:** `b560072c` (Task 2).

**3. [Refactor on touch] Split shell_exec cwd-marker helpers into shell_exec_cwd.go**
- **Found during:** Task 1.
- **Issue:** shell_exec.go was at 597 LOC; adding the `Router` field would breach the 600-LOC cap (NO GOD CLASS).
- **Fix:** Moved `cwdMarker`/`wrapForCwdTracking`/`extractCwdMarker`/`removeCwdMarkerLine` to `shell_exec_cwd.go` and added the box variant `wrapForCwdTrackingBox` (PLAIN `pwd`, never `pwd -W` — Pitfall 6). shell_exec.go is now 571 LOC; the box exec logic lives in `shell_exec_sandbox.go`.
- **Files modified:** internal/agent/tools/shell_exec.go, internal/agent/tools/shell_exec_cwd.go (new), internal/agent/tools/shell_exec_sandbox.go (new).
- **Verification:** `wc -l` shell_exec.go = 571 (≤600); full tools suite green.
- **Committed in:** `c6b1a97a` (Task 1).

**4. [In-scope, noted] buildSandboxRouter wires WithMaterializeSources**
- **Found during:** Task 2.
- **Issue:** 37-05 explicitly deferred `WithMaterializeSources` to 37-07 (the tool-adjacent wiring), and the snippet-exec E2E requires the box to materialize the skills export dir at `/skills`.
- **Fix:** `buildSandboxRouter` now passes `WithMaterializeSources(sandboxMaterializeSources(cfg))` sourcing `cfg.SkillExportDir → /skills`. The egress-image wiring was deliberately NOT added (a separate follow-up, out of lane, per the coordinator).
- **Files modified:** cmd/aura/serve_dispatch.go.
- **Committed in:** `b560072c` (Task 2).

---

**Total deviations:** 3 auto-fixed (2× Rule 3 blocking, 1× refactor-on-touch) + 1 in-scope note. **Impact:** No scope change to the plan's intent (route the 5 tools + fail-CLOSED deny + composition wiring); the plan's `files_modified` should additionally list `internal/sandbox/usersandbox/{router_tools.go,materialize.go}`, `internal/agent/tools/{shell_exec_cwd.go,shell_exec_sandbox.go,sandbox_route.go,send_file_sandbox.go}`, and `cmd/aura/{chat_boot.go,serve_dispatch.go,serve_adapters.go,cache_audit.go,main_test.go,registry_test.go}`.

## Requirements Status

- **SBX-01** (full-capability box; host tools ACTUALLY executing inside the box) — this plan delivers the tool-interposition core for the four NON-background tools + box-aware send_file + the corrected snippet-path selection + the fail-CLOSED deny + the composition wiring. Background shell jobs (`shell_bg*`) remain 37-09. `requirements-completed: []` — REQUIREMENTS.md left for the orchestrator/verifier to reconcile the multi-plan SBX-01 after the wave (consistent with 37-04/37-05).

## Known Stubs

None. `sandboxMaterializeSources` intentionally sources only the skills export dir (`/skills`) — the Agent.md / pyscripts per-identity roots are a documented forward seam (their dedicated wiring lands with their own consumers), not a stub; a routed shell_exec/snippet-exec is fully functional against a reachable Docker daemon.

## Threat Flags

None — no new trust-boundary surface beyond the plan's `<threat_model>`. The tools interpose exactly the model→tool / tool→box-vs-host / box-artifact→host-delivery boundaries the plan enumerates, each with its mitigation test (FailClosed / RoutedInBox / SnippetPath / WorkspaceFence / SkillsFence).

## Issues Encountered

- **Live docker_integration run deferred to CI/WSL.** dockerd is unreachable in this Windows worktree (npipe is not stdlib-dialable), so `TestRoute_StrictExecInBox` + `TestSnippetExec_RoutedEndToEnd` skip locally (the sanctioned local skip; `t.Fatal` under `$CI`). The suite compiles under `-tags docker_integration`; the live strict-exec-in-box + snippet-E2E green runs at phase validation on the native-Linux stack.
- **Session-quota interruption mid-Task 2.** Execution was resumed from the uncommitted serve_dispatch.go edit; no code was lost (Task 1 was already committed at `c6b1a97a`).

## Next Phase Readiness

- **37-09** (shell_bg lifecycle) has the `sandboxUnavailableResult` deny shape + the `SandboxRouter.Exec`/`Route` seam to route background box jobs; `shell_exec`'s background branch was left untouched (its host path) for 37-09 to route.
- **37-08** (docs/ADR/compose/bench-soak/validation) is unaffected — no file overlap.
- Blockers: none. The live docker_integration run is a phase-validation (WSL/CI) step, not a code blocker.

## Self-Check: PASSED

- Created files exist: `sandbox_route.go`, `shell_exec_cwd.go`, `shell_exec_sandbox.go`, `send_file_sandbox.go`, `shell_exec_sandbox_test.go`, `shell_exec_sandbox_docker_test.go`, `router_tools.go` — all FOUND on disk.
- Task commits exist: `c6b1a97a`, `b560072c` — both FOUND in `git log`.
- Plan `<verification>` re-run: `go build ./...` green; `go vet ./...` clean (exit 0); `go build ./cmd/aura/` green; `go test ./internal/agent/tools/` green; `go test ./internal/skilladapters/` green; `go test ./cmd/aura/` green; `go vet -tags docker_integration ./internal/agent/tools/` green + the two docker_integration tests compile and skip locally. Grep gate: `Router:`/`SandboxRouter:` present on the 5 tools, `WebSearch`/`WebFetch` unrouted. No touched file > 600 LOC (shell_exec.go 571).

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-07*
