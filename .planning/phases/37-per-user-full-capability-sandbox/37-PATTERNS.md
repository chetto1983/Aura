# Phase 37: Per-User Full-Capability Sandbox - Pattern Map

**Mapped:** 2026-07-06
**Files analyzed:** 25 (13 new source/test + 1 new image + 2 new migration + 6 modified tools/composition + 3 deps/config/docs)
**Analogs found:** 21 / 25 (11 exact/role-match in-repo templates, 6 self-analog modifications, 4 partial/none)

> Every anchor below was **verified by direct read** (CLAUDE.md NEVER SUPPOSE). The greenfield
> package `internal/sandbox/usersandbox/` confirmed **empty** (Glob `internal/sandbox/**/*.go` → no
> files). The 4 "no analog" rows are flagged for the planner to source from RESEARCH.md instead.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/sandbox/usersandbox/backend.go` | service (E2B-verb seam) | request-response | `internal/cron/handlers/identity_purge.go` (consumer-declared seam iface) | role-match |
| `internal/sandbox/usersandbox/docker_backend.go` | service (Docker SDK adapter) | request-response + file-I/O | — (moby/moby/client v0.4.1 options-struct; no in-repo SDK caller) | none |
| `internal/sandbox/usersandbox/spec.go` | model (unrepresentability type) | transform | `internal/config/config_runtimeprofile.go` (typed-enum discipline) | partial |
| `internal/sandbox/usersandbox/translate.go` | utility (private translator) | transform | `internal/agent/tools/shell_exec.go` `mergeEnv` (pin/filter transform) | partial |
| `internal/sandbox/usersandbox/router.go` | service (router seam) | request-response | `internal/gateway/decide.go` `Decide` (Strict() no-op) + `shell_bg_owner.go` `ownerFromContext` | role-match |
| `internal/sandbox/usersandbox/egress.go` | config/service (sidecar spec) | transform | `compose.gvisor.yaml` (runtime override) | none |
| `internal/sandbox/usersandbox/reap.go` | service (`SuspendIdle` impl) | batch | `internal/agui` `Deprovisioner.PurgeExpired` (the reaper seam impl) | role-match |
| `internal/sandbox/usersandbox/materialize.go` | service (docker cp in/out) | file-I/O | `internal/agent/tools/fs_write.go` `atomicWriteFile` (write discipline) | partial |
| `internal/sandbox/usersandbox/*_test.go` | test | unit + `docker_integration` | `internal/gateway/decide_test.go` (`TestDecideDevNoOp`) | role-match |
| `internal/cron/handlers/sandbox_reap.go` | handler (cron TaskKind) | batch/scheduled | `internal/cron/handlers/identity_purge.go` | **exact** |
| `internal/cron/handlers/sandbox_reap_test.go` | test | unit | `internal/cron/handlers/identity_purge_test.go` | **exact** |
| `internal/db/migrations/0034_*_sandbox_reap_kind.{up,down}.sql` | migration | — | `internal/db/migrations/0033_scheduler_identity_purge_kind.{up,down}.sql` | **exact** |
| `docker/aura-sandbox/Dockerfile` | config (fat box image) | — | `docker/aura/Dockerfile` (fat debian-slim, root, py3/node/uv) | **exact** |
| `internal/agent/tools/shell_exec.go` (mod) | tool/controller | request-response | self + `gateway/decide.go` gate + `shell_approval.go` deny shape | self-analog |
| `internal/agent/tools/shell_bg.go` + `shell_bg_owner.go` (mod) | tool/controller | streaming | self (Open Q4 — most invasive; `*exec.Cmd` → box exec handle) | self-analog |
| `internal/agent/tools/fs_read.go` (mod) | tool/controller | file-I/O | self + router gate | self-analog |
| `internal/agent/tools/fs_write.go` (mod) | tool/controller | file-I/O | self + router gate | self-analog |
| `internal/agent/tools/skill_read.go` + `skill.go` + `internal/skilladapters/skilladapters.go` (mod — snippet `action=use`) | tool/controller | request-response | self — CORRECTION: `Snippet` does NOT yet return `SandboxPath` (returns only hostPath); 37-07 extends it, adapter sources `SandboxPath` from `skills.SnippetSandboxPath`, `renderSnippetUse` picks the path by `SandboxRouter.Strict()` for the rendered `shell_exec` (NO `backend.Exec`) | self-analog |
| `cmd/aura/main.go` (mod) | config (composition root) | — | `cmd/aura/main.go:163` tool registration | self-analog |
| `cmd/aura/serve_provisioning.go` + `serve_dispatch.go` (mod) | config (seed + register) | — | `seedIdentityPurgeSweep` + `serve_dispatch.go:57` handler map | **exact** |
| `go.mod` (mod) | config (deps) | — | `go.mod:143-144` moby indirect entries | self-analog |
| `compose.yaml` (mod) | config | — | `compose.gvisor.yaml` + `compose.yaml` | partial |
| `docs/adr/…SBX-05.md` (new) | doc | — | — (planner sources decisions from CONTEXT D-01/D-15) | none |
| `.planning/REQUIREMENTS.md` (mod — SBX-04 amendment) | doc | — | — (D-06 PRD/REQUIREMENTS-amendment commit before impl) | none |

---

## Pattern Assignments

### `internal/cron/handlers/sandbox_reap.go` (handler, batch/scheduled) — EXACT TEMPLATE

**Analog:** `internal/cron/handlers/identity_purge.go` (copy this file verbatim; swap purge→suspend).

**Consumer-declared seam interface — no reverse import** (`identity_purge.go:26-28`):
```go
type IdentityPurger interface {
	PurgeExpired(ctx context.Context, now time.Time) (purged int, err error)
}
```
**Handler + Meta + Run** (`identity_purge.go:14,42-63`):
```go
const KindIdentityPurge TaskKind = "identity_purge"
const identityPurgeMaxDuration = 5 * time.Minute

func (IdentityPurgeHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindIdentityPurge, MaxDuration: identityPurgeMaxDuration, ReschedulesOnRecovery: false}
}

func (h IdentityPurgeHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.Purger == nil {
		return "identity purge: disabled (no purger)", nil
	}
	now := time.Now().UTC()
	if h.now != nil { now = h.now() }
	purged, err := h.Purger.PurgeExpired(ctx, now)
	if err != nil { return "", fmt.Errorf("identity purge: %w", err) }
	return fmt.Sprintf("identity purge ok: purged %d expired identit(y/ies)", purged), nil
}
```
**Handler contract types** (`internal/cron/handlers/handler.go:27,42-45,73-76`): `type TaskKind string`; `HandlerMeta{Kind, MaxDuration, ReschedulesOnRecovery}`; `Handler interface { Meta() HandlerMeta; Run(ctx, job Job) (string, error) }`. The kind constant is declared **in the handler's own file** (like `KindIdentityPurge`), NOT in the shared list — `handler.go:29` comments say kinds are one-per-file.

**Delta the new code introduces:**
- `const KindSandboxReap TaskKind = "sandbox_reap"`.
- Seam: `type SandboxReaper interface { SuspendIdle(ctx context.Context, now time.Time) (int, error) }` — satisfied by the live `usersandbox` router (so `handlers` does NOT import `usersandbox`, exactly as it does not import `internal/agui`).
- `Meta()` → `{Kind: KindSandboxReap, MaxDuration: 5*time.Minute, ReschedulesOnRecovery: false}` (idempotent sweep — matches RESEARCH Code Example lines 419-431 verbatim).
- Nil-reaper → `"sandbox reap: disabled"` no-op success. **Auto-resume is NOT scheduled** — it is inline in `Route` (D-08).

---

### `internal/cron/handlers/sandbox_reap_test.go` (test, unit) — EXACT TEMPLATE

**Analog:** `internal/cron/handlers/identity_purge_test.go` (copy verbatim; rename `fakePurger`→`fakeReaper`, `PurgeExpired`→`SuspendIdle`).

Covers the four cases proven for the purge handler (`identity_purge_test.go:28-80`): `TestMeta` (kind + 5-min budget + `!ReschedulesOnRecovery`), `TestRunPurges` (drives the seam with a non-zero time, reports the count), `TestDisabled` (nil seam → disabled success), `TestRunError` (seam error is terminal). The `fakePurger` shape (`identity_purge_test.go:13-24`) records `gotNow`/`called` and returns a fixed count or error — mirror it as `fakeReaper`.

---

### `internal/db/migrations/0034_scheduler_sandbox_reap_kind.{up,down}.sql` (migration) — EXACT TEMPLATE

**Analog:** `0033_scheduler_identity_purge_kind.up.sql` / `.down.sql` (latest migration; next number = **0034**).

**Up — drop + re-add the unnamed inline CHECK with the new member** (`0033…up.sql:14-16`):
```sql
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge'));
```
**Down — delete admitted rows FIRST, then narrow** (`0033…down.sql:9-15`): the down must `DELETE FROM aura.agent_job_runs WHERE task_id IN (… kind = 'sandbox_reap')` then `DELETE FROM aura.scheduler_tasks WHERE kind = 'sandbox_reap'` before re-adding the narrower CHECK — a live seeded sweep row otherwise violates the restored constraint and aborts the down mid-chain (dirty DB). `agent_job_runs` FKs `scheduler_tasks(id) ON DELETE CASCADE`.

**Delta:** append `'sandbox_reap'` to the up CHECK member list; the down deletes `sandbox_reap` rows + restores the 0033 list (with `'identity_purge'`). No GRANT change (0009 already granted `aura_app` DML; `aura_migrate` owns DDL).

---

### `internal/sandbox/usersandbox/router.go` (service, request-response) — TWO-ANALOG COMPOSITE

**Analog A — the Strict() no-op short-circuit:** `internal/gateway/decide.go:30-33`:
```go
func (g *Gateway) Decide(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey) (Verdict, error) {
	if g == nil || !g.profile.Strict() {
		return Verdict{Decision: Allow, Reason: "no-op (dev/local_trusted)"}, nil
	}
	...
```
The router's `Route(ctx)` mirrors this exactly: `if r == nil || !r.profile.Strict() { return nil, false, nil }` → tool runs its existing host `os/exec` path unchanged (SC-4 dev no-op). `Strict()` is `config_runtimeprofile.go:56-58` — true only for `single_user_hardened` + `server_production`.

**Analog B — identity→owner resolution with `local` fallback:** `internal/agent/tools/shell_bg_owner.go:25,54-59`:
```go
const localOwnerID = "00000000-0000-0000-0000-000000000001" // seeded `local` identity (migration 0004)

func ownerFromContext(ctx context.Context) string {
	if id := identityctx.IdentityID(ctx); id != "" {
		return id
	}
	return localOwnerID
}
```
`identityctx.IdentityID(ctx)` returns `""` when unscoped (`identityctx/identityctx.go:18-21`). The router keys its get-or-create on this exact resolution — the `local`/no-principal CLI identity under a strict profile gets a `local`-id box (D-09), never host.

**Owner-scoped `*ForIdentity` shape (Phase 36 additive convention):** confirmed live as `ListActivityForIdentity` / `GetForIdentity` (e.g. `internal/agui/audit_store_integration_test.go:93`, `internal/assets/store_test.go:95`) — an `identityID`-parameterized additive method. `SandboxRouter.Resolve(spec)` follows the same shape (key on identity, never cross-identity).

**Delta:** `Route` returns `(BoxHandle, routed bool, err error)`. The **fail-CLOSED** branch (`routed=true, err!=nil`) is the containment invariant — see Shared Pattern "Fail-CLOSED deny". Idle tracking = a `lastUsedAt` per box bumped on each `Exec` (D-08); `SuspendIdle(ctx, now)` (the `SandboxReaper` seam) sweeps boxes past the TTL.

---

### `internal/sandbox/usersandbox/spec.go` + `translate.go` (model + utility, transform) — SBX-02 crux

**Analog (partial) — typed-enum-not-free-string discipline:** `config_runtimeprofile.go:20-32` (`RuntimeProfile` string enum with named consts + a total `ParseProfile`). The `SandboxSpec.Runtime RuntimeClass` enum `{Runc, Runsc}` follows this — never a free string that could carry `"host"`.

**Analog (partial) — the pin/filter transform pattern:** `translate.go`'s "dangerous moby fields pinned to safe constants" has no exact structural twin, but the closest posture is `shell_exec.go:433-454` `mergeEnv`, which *filters out* dangerous inputs (secret env) unconditionally as it builds the child's env. `translate.go` is the same shape at the type layer: build the moby `HostConfig` with `Privileged:false`, `Binds:nil`, `NetworkMode("")`, `AutoRemove:false` as **literals present nowhere else** (RESEARCH Pattern 2, lines 240-259; Code Example lines 396-411).

**Delta / NO in-repo analog for the moby types:** `container.HostConfig`/`Resources`/`mount.Mount` come from `moby/moby/api` v1.54.2 (`go.mod:143`, currently indirect). There is **no existing caller of the moby SDK in the codebase** — this is genuinely greenfield binding work. Planner MUST follow RESEARCH Pitfall 1 (lines 333-337): run `go doc github.com/moby/moby/client.ContainerCreateOptions` against the vendored v0.4.1 **before** writing `translate.go`/`docker_backend.go` (options-struct API, not the classic `docker/docker/client` signature every example uses).

**Test (SBX-02):** structural test (reflect over `SandboxSpec`, assert no privileged/host-net/bind/socket field) + behavioral table/rapid test (`toHostConfig` on adversarial specs → assert safe pins). RESEARCH Test-Map (lines 514-515): `TestSpec_NoHostExposureFields`, `TestTranslate_PinsSafe`.

---

### `internal/sandbox/usersandbox/docker_backend.go` (service, request-response + file-I/O) — NO IN-REPO ANALOG

**No analog** — first moby-SDK caller in the repo. Source shape is RESEARCH Code Example (lines 377-390): `ExecCreate`/`ExecAttach` + `stdcopy.StdCopy` demux + `ExecInspect` for exit code, honoring `ctx` cancellation. Replaces `exec.CommandContext` (`shell_exec.go:165`).

**Reuse anchor — secret scrub on the box exec env:** the box `Exec` env MUST be scrubbed exactly as the host path does (`shell_exec.go:433-454` `mergeEnv` → `secret.IsSecretEnvVar(k, v)` at `internal/secret/envkey.go:83`). See Shared Pattern "Secret env scrub". RESEARCH Runtime-State (line 326) + Security Domain (line 565) both require this.

**Windows→Linux caveat (RESEARCH Pitfall 6, lines 363-367):** in the box-exec branch use `/bin/sh -c` + plain `pwd` (POSIX) — NOT `windowsShell()`/`pwd -W` (`shell_exec.go:326-327,389-402`). Drive `docker cp` via the Go SDK tar stream (`CopyToContainer`), never a shelled `docker` command (MSYS path mangling). Keep `AutoRemove:false` (Pitfall 5 — `--rm` destroys a suspendable box).

---

### `docker/aura-sandbox/Dockerfile` (config, image) — EXACT TEMPLATE (reuse the fat base)

**Analog:** `docker/aura/Dockerfile` (verified **fat**, D-12 posture already correct — the `ec7fe2f6` distroless revert is done). Runtime stage `FROM debian:bookworm-slim` (line 36), installs `curl git python3 python3-pip python3-venv` (43-49), Node 24 (59-61), `uv`/`uvx` from `ghcr.io/astral-sh/uv:0.11.21` (72), **root user (no `USER` line)**, `postgresql-client-18`, `mcp-neo4j-cypher==0.6.0`. This is precisely the D-13 "fat base bakes python3/node/go/uv/git/gh/jq + Phase-5 heavy set" image.

**Delta:** derive the shared box image from this base (or reuse `docker/aura` directly per RESEARCH structure note line 182); **digest-pin** it (D-13); add the shared `uv` warm-cache volume mount target (`/root/.cache/uv`, Code Example line 404). Anti-pattern guard (RESEARCH line 298): the planner must NOT distroless / non-root / read-only-rootfs it — that breaks `shell_exec` parity (D-12).

---

### `internal/agent/tools/{shell_exec,fs_read,fs_write}.go` (tool/controller, mod) — SELF-ANALOG + gate

Each tool gains a `Router *usersandbox.SandboxRouter` field (nil = host-direct, preserving today's behavior). The routed branch replaces the host primitive:
- `shell_exec.go:165-175` `exec.CommandContext(...)` → `router.Route(ctx)`; if routed, `backend.Exec(h, ExecRequest{...})`. The cwd-marker wrap (`wrapForCwdTracking`, `shell_exec.go:326`) still applies inside the box (shell-level).
- `fs_read.go:51` `os.ReadFile(path)` → `backend.Exec(cat)` or `CopyFromContainer`.
- `fs_write.go:64` `atomicWriteFile(...)` → `CopyToContainer` (tar) or `Exec` a write (the box IS the boundary — atomic-write host helper not needed inside).

**Wiring site:** `cmd/aura/main.go:163` today constructs `&tools.ShellExec{WorkspaceRoot: workspace, Background: handles.BackgroundShells, Approvals: handles.ShellApprovals}` and `main.go:181` `&tools.FSWrite{SkillsDir: cfg.SkillsDir}` — the `Router:` field is added here. `web_fetch`/`web_search` are deliberately NOT routed (D-11).

**Delta:** the D-09 fail-CLOSED result on a routed-but-errored call (Shared Pattern below). RESEARCH Test-Map (line 523): `TestShellExec_FailClosedNoHostFallback`.

---

### `internal/agent/tools/shell_bg.go` + `shell_bg_owner.go` (tool/controller, streaming, mod) — SELF-ANALOG (Open Q4, most invasive)

`shell_bg.go:33-46` `BackgroundShells` holds a `map[string]*bgShell`, and `bgShell` (66-84) holds a host `*exec.Cmd` lifecycle via `start()` (181-235: `exec.CommandContext` + `setProcessGroup` + `cmd.Cancel`). Routing background jobs into a box exec-stream means the registry holds a **box-exec handle** instead of a `*exec.Cmd` — the kill/poll mapping moves from process-group signal to `ExecInspect`/box signal.

**Delta / RISK (RESEARCH Open Q4 line 475, Assumption A5 line 454):** this is the riskiest of the five tools — scope it as its own plan slice. The owner/authority layer (`shell_bg_owner.go:73-86` `authorizeCaller`, `localOwnerID`) is orthogonal and stays; only the process handle behind `bgShell` changes.

---

### `cmd/aura/serve_provisioning.go` + `serve_dispatch.go` (composition, mod) — EXACT TEMPLATE

**Seed (idempotent) — analog `seedIdentityPurgeSweep`** (`serve_provisioning.go:374-399`):
```go
func seedIdentityPurgeSweep(ctx context.Context, store *cron.Store) error {
	tasks, err := store.ListActiveTasks(ctx)
	if err != nil { return fmt.Errorf("list active tasks: %w", err) }
	for _, t := range tasks {
		if t.Kind == cron.KindIdentityPurge { return nil } // already seeded — idempotent
	}
	spec, err := cron.ParseSchedule(string(cron.KindEvery), "", identityPurgeSweepMinutes, time.Time{}, "Europe/Rome")
	...
	if _, err := store.CreateTask(ctx, cron.CreateTaskParams{Kind: cron.KindIdentityPurge, Spec: spec, NextRunAt: next}); err != nil {
		return fmt.Errorf("create identity_purge task: %w", err)
	}
}
```
Called at `serve.go:314` with a non-fatal warn. **Handler registration** — analog `serve_dispatch.go:57`:
```go
cron.KindIdentityPurge: handlers.IdentityPurgeHandler{Purger: buildDeprovisioner(chat)},
```
**Delta:** add `seedSandboxReapSweep` (mirror; `every ~Nm` cadence; `const sandboxReapSweepMinutes`), register `cron.KindSandboxReap: handlers.SandboxReapHandler{Reaper: <the usersandbox router>}`, and call the seed from `serve.go`. A nil reaper (no-pool build) yields a no-op — always safe, exactly as the purge registration notes (`serve_dispatch.go:54-56`).

---

## Shared Patterns

### Strict() no-op gate (dev/local_trusted short-circuit)
**Source:** `internal/gateway/decide.go:30-33` — `if g == nil || !g.profile.Strict()` → Allow no-op.
**Apply to:** `router.go` `Route`, and the routing branch of all five tools. Under `dev`/`local_trusted` the box is entirely bypassed (SC-4). Proof template: `internal/gateway/decide_test.go:93` `TestDecideDevNoOp` (iterates `ProfileDev`+`ProfileLocalTrusted`, asserts Allow + no side effect).
```go
if r == nil || !r.profile.Strict() {
	return nil, false, nil // host-direct no-op — identical shape to gateway.Decide
}
```

### Fail-CLOSED deny ToolResult (D-09 / GATE-01)
**Source:** `internal/agent/tools/shell_approval.go:145-162` `shellApprovalRequiredResult` — and its byte-for-byte gateway twin `internal/gateway/approve.go:133-142` `gatewayApprovalRequiredResult`.
**Apply to:** all five routed tools, on a routed-but-box-failed call (Docker down / pull fail / OOM). Return the deny-shaped `ToolResult`, **NEVER** a host `os/exec` fallback.
```go
// shell_approval.go:145 — the exact shape D-09 mirrors
func shellApprovalRequiredResult(challenge ShellApprovalChallenge) ToolResult {
	payload := map[string]string{
		"error":   "shell_approval_required",
		"command": challenge.Command,
		"message": "...",
	}
	raw, err := json.Marshal(payload)
	if err != nil { raw = []byte(`{"error":"shell_approval_required"}`) }
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}
```
`ToolResult` fields (`internal/agent/tools/spec.go:78-85`): `Preview string`, `Bytes int`, `Meta *ToolResultMeta`. The sandbox deny mirrors this with `error:"sandbox_unavailable"` + guidance.

### Consumer-declared seam interface (no reverse import)
**Source:** `internal/cron/handlers/identity_purge.go:26-28` `IdentityPurger` (live `*agui.Deprovisioner` satisfies it; `handlers` never imports `agui`).
**Apply to:** `sandbox_reap.go` (`SandboxReaper` satisfied by the `usersandbox` router) and the `backend.go` `Backend` seam (the DGX `agent-sandbox` E2B impl drops in behind it). "Accept interfaces" — avoids the reverse cycle.

### Secret env scrub on the box exec path
**Source:** `internal/agent/tools/shell_exec.go:433-454` `mergeEnv` → `internal/secret/envkey.go:83` `IsSecretEnvVar(name, value)` (marker-based + credential-URL detection).
**Apply to:** `docker_backend.go` `Exec` — scrub the box's exec env identically so no host secret leaks into a per-identity box (RESEARCH Security Domain line 565; Runtime-State line 326).
```go
// shell_exec.go:438 — the filter to reuse verbatim on the box exec env
if !ok || secret.IsSecretEnvVar(k, strings.TrimPrefix(kv, k+"=")) { continue }
```

### `sandbox_exec` display/preview pre-wiring (already landed — do NOT rebuild)
**Source:** `internal/agent/display/normalize.go:56` + `preview.go:69` + `code.go:3` already `case "shell_exec", "sandbox_exec":` → `CodeInput`. Test coverage exists: `normalize_test.go:22`, `preview_test.go:120`.
**Apply to:** the routed exec's tool-card surface — emit results under the `sandbox_exec` name (or the routed `shell_exec` name, both handled) and the preview renders with zero new display code.

---

## No Analog Found

Planner sources these from RESEARCH.md / CONTEXT.md, not an in-repo template:

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/sandbox/usersandbox/docker_backend.go` | service | request-response + file-I/O | First moby/moby/client v0.4.1 caller in the repo — options-struct API differs from every online `docker/docker/client` example (RESEARCH Pitfall 1). `go doc` the vendored types before writing. |
| `internal/sandbox/usersandbox/egress.go` | config/service | transform | No egress-sidecar precedent. Adopt OpenSandbox (D-03/D-07) for the FQDN allowlist (runc-only) + a bespoke filter-table nft floor (gVisor-compatible, always-on). RESEARCH Pattern 6 + Pitfall 2 (gVisor⊥nat). |
| `docs/adr/…SBX-05.md` | doc | — | New ADR. Decisions come from CONTEXT D-01/D-15 (container-per-identity over Docker; K8s+agent-sandbox+gVisor-default reserved for DGX; E2B template/claim shape). |
| `.planning/REQUIREMENTS.md` (SBX-04 amendment) | doc | — | D-06 requires a PRD/REQUIREMENTS-amendment commit BEFORE implementation (`--network none` → full-internet-minus-internal). No code template. |

---

## Metadata

**Analog search scope:** `internal/cron/handlers/`, `internal/agent/tools/`, `internal/config/`, `internal/identityctx/`, `internal/gateway/`, `internal/secret/`, `internal/agent/display/`, `internal/skills/`, `internal/db/migrations/`, `cmd/aura/`, `docker/`, `compose*.yaml`, `go.mod`.
**Files scanned (read in full or targeted):** 24 — `identity_purge.go`(+test), `config_runtimeprofile.go`, `identityctx.go`, `shell_exec.go`, `shell_exec_env.go`, `shell_approval.go`, `fs_read.go`, `fs_write.go`, `shell_bg.go`, `shell_bg_owner.go`, `0033…up/down.sql`, `decide.go`(grep), `approve.go`(grep), `envkey.go`(grep), `normalize.go`/`preview.go`/`code.go`(grep), `snippet.go`(grep), `skill_read.go`(grep), `serve_provisioning.go`, `serve_dispatch.go`(grep), `serve.go`(grep), `spec.go`, `handler.go`(grep), `shell_bg.go`, `compose.gvisor.yaml`, `docker/aura/Dockerfile`, `go.mod`(grep).
**Greenfield confirmed:** `internal/sandbox/**/*.go` → 0 files.
**Latest migration confirmed:** 0033 → new is **0034**.
**moby deps confirmed:** `go.mod:143-144` both `// indirect` (D-01 promote to direct).
**Pattern extraction date:** 2026-07-06
