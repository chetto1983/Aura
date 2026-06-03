# Phase 8 — Multi-Source Coverage Audit + Artifacts This Phase Produces

**Created:** 2026-06-03 (by gsd-plan-phase)
**Plans:** 9 (08-01 .. 08-09) across 5 waves.

> This doc is read by the plan-review-convergence source-grounding pass to (a) confirm every source item is COVERED by a plan and (b) EXCLUDE newly-created symbols from drift verification. It is planning state, not a PRD amendment.

---

## Multi-Source Coverage Audit

### GOAL (ROADMAP Phase 8 goal + 4 success criteria)

| Goal item | Covered by | Notes |
|-----------|-----------|-------|
| Session-bound containers per conversation_id | 08-05 (SessionManager) + 08-07 (sidecar/runner) + 08-08 (wiring) | gVisor container per conv (D-04). |
| Per-conversation workspace mount (nosuid,nodev,noexec, owner 65532, 100MiB) | 08-05 (WorkspaceManager.EnsureDir + walkSize quota) + 08-08 (docker run mount flags via SessionManager) | quota knob in 08-02. |
| Network allowlist (host-proxy, not iptables) | 08-06 (forward proxy) + 08-04 (SSRF export) + 08-08 (seccomp-session.json + posture) | D-08 pivot amended in 08-01. |
| TTL reaper (1800s, 60s sweep, hard cap 5) | 08-05 (reaper + cap) | synctest-tested; live in 08-09. |
| Host-side symlink-escape guard (os.Root) | 08-05 (workspace os.Root walks + PurgeConversationDir) + 08-08 (Conversations.Delete cascade) | D-13/D-14; O_NOFOLLOW→os.Root amended in 08-01. |
| SC#1 session persistence (x=42 + workspace file) | 08-05 + 08-07; live 08-09 TestSessions_PythonStatePersists/WorkspacePersists | |
| SC#2 symlink-escape refused on host cascade | 08-05/08-08; live 08-09 TestWorkspace_SymlinkEscapeCascade | |
| SC#3 idle TTL reap | 08-05; live 08-09 TestReaper_LiveContainerRemoved | |
| SC#4 network_allow pypi allowed / 1.1.1.1 refused | 08-06/08-08; live 08-09 TestNetwork_PyPIAllowed/NonAllowlistRefused | |

### REQ (REQUIREMENTS.md phase_req_ids)

| Req ID | Covered by | Notes |
|--------|-----------|-------|
| CAP-02 | ALL 9 plans (requirements: [CAP-02] in each frontmatter) | "via iptables" wording superseded by D-08 (amended 08-01). Closes on the 08-09 human-verify. |

### RESEARCH (08-RESEARCH features/constraints + 6 landmines)

| Research item | Covered by |
|---------------|-----------|
| Persistent stdlib exec() interpreter (not IPython) | 08-07 |
| CONNECT forward proxy + deny-wins glob | 08-06 |
| os.Root openat2 walks + manual no-follow cascade | 08-05 |
| SessionManager sync.Map + reaper (synctest, goleak) | 08-05 |
| Landmine #1 FK uuid (not text) | 08-02 |
| Landmine #2 migration 0008 (not 0010) | 08-02 |
| Landmine #3 (HIGHEST) egress-bridge connect/seccomp posture | 08-08 (posture) + 08-09 (live spike) |
| Landmine #4 workspace/.result co-tenancy cascade | 08-05 (PurgeConversationDir) + 08-08 (interface wiring) |
| Landmine #5 SSRF unexported | 08-04 (export) |
| Landmine #6 gVisor not inherited by docker run | 08-05 (docker run --runtime + flags) |

### CONTEXT (D-01 .. D-14)

| Decision | Covered by | Decision | Covered by |
|----------|-----------|----------|-----------|
| D-01 persistent interp | 08-07 | D-08 host-proxy egress | 08-06/08-08 |
| D-02 asymmetric persistence | 08-07 | D-09 reuse SSRF | 08-04/08-06 |
| D-03 reaper/RAM cap | 08-05 | D-10 hostname-only/privacy | 08-02/08-05/08-06 |
| D-04 container per conv | 08-05 | D-11 full scoring module | 08-03 |
| D-05 docker-lifecycle carve-out | 08-05/08-08 | D-12 scope guard (module not pipelines) | 08-03/08-08 |
| D-06 control-plane registry/boot recovery | 08-02/08-05 | D-13 os.Root walkers | 08-05 |
| D-07 per-session lock | 08-05 | D-14 manual no-follow cascade | 08-05/08-08 |

**Verdict: ALL source items COVERED. No unplanned items. No phase split required.**

**Exclusions (not gaps):** scheduler/skills application pipelines that consume scoring (P10/P11, D-12); LLM-driven SandboxEscapeBench red-team (Phase 5 D-03, scheduled/manual); arbitrary on-demand pip UX beyond the allowlist plumbing; per-identity sessions / Firecracker / memory-snapshot resume (deferred ideas).

---

## Artifacts This Phase Produces

> Newly-created symbols — EXCLUDE from drift verification (they do not exist before Phase 8).

### Go types / functions
- `sandbox.SessionManager` (+ `Acquire`, `Release`, `Close`, `RecoverOnBoot`, `reap`) — 08-05
- `sandbox.session` / `sandbox.Session` (per-session struct, per-session mutex) — 08-05
- `sandbox.WorkspaceManager` (+ `EnsureDir`, `walkSize`, `PurgeConversationDir`) — 08-05
- `sandbox.ConversationCleaner` impl (the workspace cleaner) — 08-05; interface `ConversationCleaner` DEFINED in `conversations` — 08-08
- `sandbox.Proxy` / `sandbox.policy` (CONNECT forward proxy + deny-wins glob) — 08-06
- `sandbox.ErrSessionCapReached`, `sandbox.ErrWorkspaceQuotaExceeded` (sentinels) — 08-02
- Session-bound runner methods on `Runner`/`DockerRunner` (e.g. `RunPythonSession`/`RunShellSession`) + session exec HTTP path — 08-07
- `scoring.RiskTier` (Safe/Normal/Risky/Destructive) + `scoring.SandboxArgs` + `scoring.ComputeSandboxTier` + `scoring.ComputeTaskTier` + `scoring.ComputeSkillTier` + `scoring.GateRecommended` + `scoring.RequiresImmediateAlert` + the UP-only modifier table — 08-03
- `web.ClassifyIP` + the resolve-then-pin export (e.g. `web.NewDialGuard`/`DialGuard.ResolveAndPin`) — 08-04
- `cmd/aura` `runSandbox` (`aura sandbox sessions {list|terminate|prune}`) — 08-08

### Config / env vars (new)
- `AURA_SANDBOX_SESSION_TTL_SEC` (1800), `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS` (5), `AURA_SANDBOX_WORKSPACE_MAX_BYTES` (104857600), `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` (empty), `AURA_RISK_ALERT_THRESHOLD` (risky), `AURA_PRIVACY_MODE` (empty, newly READ) — config fields `SandboxSessionTTLSec`, `SandboxMaxConcurrentSessions`, `SandboxWorkspaceMaxBytes`, `SandboxNetworkAllowHosts`, `RiskAlertThreshold`, `PrivacyMode` — 08-02

### CLI surface (new)
- `aura sandbox sessions list|terminate|prune` — 08-08
- `aura exec --session <conv_id>` (latch ACTIVATED, was inert) — 08-08

### Tool surface (changed, not new)
- `execute` tool `session_id` arg ACTIVATED (defaults to conversation_id); advisory `{risk_tier, gate_recommended}` in the lean preview; `execute` stays `Deferred:true` — 08-08

### Persistence (new)
- migration `0008_sandbox_sessions.{up,down}.sql` + `aura.sandbox_sessions` table — 08-02
- sqlc queries: `InsertSession`, `TouchLastUsed`, `MarkTerminated`, `ListActive` — 08-02

### Files / assets (new)
- `internal/sandbox/sessions.go` (+ possible `sessions_reaper.go`/`sessions_recovery.go` if >400 LOC), `workspace.go`, `network.go`, `cleanup.go`
- `internal/scoring/scoring.go` + `scoring_test.go`
- `internal/web/export.go` + `export_test.go`
- `sandbox/seccomp-session.json` (connect-allowing variant)
- `cmd/aura/sandbox.go`
- `.planning/phases/08-sandbox-2b-session-bound/08-SECURITY.md`, `08-DECISIONS-WAVE0.md`
- integration tests: `sessions_integration_test.go`, `workspace_integration_test.go`, `network_integration_test.go`

---

## Wave Structure

| Wave | Plans | Parallel? | Gate |
|------|-------|-----------|------|
| 1 | 08-01 | n/a | PRD amendments + Wave-0 decisions (doc-only) gate ALL code |
| 2 | 08-02, 08-03, 08-04 | yes (no file overlap: DB/config/errors vs scoring vs web) | substrate + scoring + SSRF export |
| 3 | 08-05, 08-06, 08-07 | yes (sessions/workspace/cleanup vs network vs sidecar/docker/sandbox) | control plane + proxy + interpreter |
| 4 | 08-08 | n/a | wiring (execute/CLI/cascade/compose/seccomp) |
| 5 | 08-09 | n/a (has human-verify) | live Gate-3 + 08-SECURITY + CI + human sign-off |
