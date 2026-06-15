# Phase 5: Sandbox 2a Stateless - Research

**Researched:** 2026-06-01
**Domain:** Untrusted code execution isolation (gVisor/runsc + Docker hardening + seccomp allowlist + sidecar HTTP runner)
**Confidence:** HIGH on isolation primitives + Docker/seccomp wiring + existing-code integration; MEDIUM on the SandboxEscapeBench port (scenario count discrepancy — see Open Questions); MEDIUM on the published seccomp allowlist source (no single canonical "Python 3.12 + bash" profile exists — see Standard Stack).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
All 19 decisions are HOW-level and are the planner's defaults. Verbatim summary:

- **D-01** — Port all 18 SandboxEscapeBench scenarios as DETERMINISTIC probe scripts (no model driver, fast, CI-runnable) in `scripts/sandbox_escape_bench.sh`. Misconfig scenarios Aura structurally forbids → "config-regression" assertions (must stay 0). Runtime/kernel scenarios → live escape-rate denominator. Escape-rate **< 5%** recorded in `docs/aura-quality-snapshot.md`.
- **D-02** — Bench runs against the gVisor-primary x86 profile (production default), with the container+seccomp floor (D-06) bench-validated as the arm64 fallback.
- **D-03** — PLUS an opt-in, separately-tagged LLM-driven red-team (real Inspect AI + frontier model) — scheduled/manual, NOT per-merge. Feeds quality-snapshot when run.
- **D-04** — gVisor compose profile wired now + default-on for x86. `compose.gvisor.yaml` / a `runsc` profile flips `runtime: runsc`; arm64 falls back to `runc` + seccomp floor.
- **D-05** — gVisor `runsc` is Aura's PRIMARY sandbox boundary on x86 (strongest isolation without `/dev/kvm`). **OVERRIDES locked D12 — requires PRD-amendment before planning.**
- **D-05a** — microVM (Firecracker/Kata) stays REJECTED (needs `/dev/kvm`, blocked on Hetzner cloud + unconfirmed on DGX → violates D00 portability).
- **D-06** — Hardened-container + seccomp allowlist + userns-remap = the PORTABLE FLOOR. Defense-in-depth inside gVisor on x86; standalone fallback boundary on arm64/DGX until gVisor-arm64 is GA. seccomp loads under `runsc` too.
- **D-07** — Go runner is runtime-agnostic (pure HTTP client). x86 default = `runsc`; arm64 = `runc` + seccomp floor. Selected via `AURA_SANDBOX_RUNTIME` (default resolves per-arch).
- **D-08** — `DockerRunner` is a thin HTTP client against `AURA_SANDBOX_URL`; compose-managed sidecar IS the single-node orchestration layer. Execution path = HTTP only. No pool in 2a.
- **D-09** — Auto-start-if-down then thin client. On connect failure: ONE auto-start (`docker compose up -d aura-sandbox`), health-check, retry once; still fails → typed `ErrSandboxUnreachable`. **DEVIATES from PRD — requires PRD-amendment before planning.**
- **D-10** — Broad curated PUBLISHED allowlist, no empirical strace. ~80–100 syscalls wholesale; HARD-EXCLUDE `ptrace`, `unshare`, `process_vm_readv`, `bpf`, `kexec_load`, `userfaultfd`, `mount`. `defaultAction: SCMP_ACT_ERRNO(EPERM)`. The 18-scenario bench backstops "too loose" risk.
- **D-11** — `seccomp.json` carries BOTH arches by-name (`SCMP_ARCH_X86_64` + `SCMP_ARCH_AARCH64`; libseccomp resolves numbers — never hardcode numbers). x86_64 validated live.
- **D-12** — arm64 validated via QEMU emulation in CI (`docker buildx` / `qemu-user-static` binfmt → `--platform linux/arm64`). **Caveat: QEMU syscall emulation can diverge from real arm64 kernel seccomp → real-DGX confirmation is a tracked obligation before production arm64.**
- **D-13** — Docker-in-Docker, gating CI. Real sidecar (with `runsc` installed for x86 default), runs `//go:build sandbox_integration` tier + the 18-scenario bench + QEMU arm64 validation, gates merge. No-skip-as-green honored; folds into the 85% floor. LLM red-team (D-03) stays scheduled/manual.
- **D-14** — userns-remap (rootless REJECTED). Daemon-level `userns-remap: default` + `no-new-privileges: true`; container UID 0 → unprivileged host UID, seccomp preserved. Enabled + validated in CI DinD daemon; production daemon requirement; dev opt-in. Layered with `read_only` / tmpfs `/tmp` / `pids_limit: 64` / `mem=512m` / `cpus=1.0` / `ulimit nofile=64`. Allowlist excluding `unshare` blocks in-container userns creation.
- **D-15** — PRD acceptance #5 resolves `userns-remap` against the **daemon** (`daemon.json`), not a compose-service field. The other flags (`cap_drop`, `no-new-privileges`, `read_only`, `pids_limit`) are compose-service fields.
- **D-16** — Per-lang endpoints, JSON in/out. `POST /exec/python` + `POST /exec/shell`. Req `{"code": str, "timeout_sec": int}`. Resp `{"stdout","stderr","exit_code","elapsed_ms","truncated","limit_hit": "timeout"|"oom"|"pids"|null}`. Sidecar truncates each stream to **1 MiB server-side** and reports `truncated`/`limit_hit`.
- **D-17** — Lean ToolResult.Preview: stdout verbatim as primary content (no fence/label); `stderr:` appended only if non-empty; `exit_code: N` line ONLY when non-zero (success silent); `elapsed_ms` + `[limit: …]` ONLY when a limit hit; empty output → `(no output, exit 0)`. Whole string → `tools.NewResult` (D-25 cap + spillover).
- **D-18** — Code's fault → ToolResult (model adapts: non-zero exit, stderr, seccomp EPERM-as-stderr-text, timeout, OOM, pids-cap). Environment's fault → typed Go sentinel (`ErrSandboxUnreachable`, `ErrSandboxProtocol`). A script exiting 1 is never a Go error.
- **D-19** — `aura exec [--session <id>] <lang> <code>` in the hand-rolled switch dispatcher (NO cobra). `lang` positional ∈ {python, shell} (required). `code` positional single string, or `-` to read stdin. Output = lean format (D-17). Process exits with sandbox `exit_code`; infra failure (`ErrSandboxUnreachable`) → distinct code **70**. `--session` parsed-but-inert in 2a (errors pointing to Phase 8 / Slice 2b).

### Claude's Discretion
- Exact `sidecar.py` structure (stdlib `http.server`), Dockerfile layering, and the precise published allowlist source — pick within D-10/D-16 constraints.
- `compose.yaml` field ordering and exact `AURA_SANDBOX_RUNTIME` per-arch default resolution.
- Whether the deterministic bench is one shell script or a small harness — invariant: "18 scenarios, escape-rate emitted, CI-gating."

### Deferred Ideas (OUT OF SCOPE)
- **Slice 2b (Phase 8):** session-bound containers, `$AURA_RUN_DIR/conversations/<id>/workspace/` mount, `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` allowlist + iptables + DNS-rebinding pin, `aura.sandbox_sessions` table + migration 0010, `SessionManager` reaper, `aura sandbox sessions {list|terminate|prune}`. `session_id` reserved-but-inert in 2a keeps the surface forward-stable. DO NOT plan/research 2b features.
- **microVM (Firecracker/Kata) tier** — rejected for KVM-less default (D-05a). Record only.
- **Opt-in LLM-driven red-team** (D-03) — scheduled/manual capability eval, not part of the per-merge gate.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CAP-01 | Sandbox 2a stateless Python 3.12 slim sidecar (stdlib only, no pip), seccomp positive allowlist ~80 syscalls (NOT deny-list), ulimit, `network_mode: none` default. SandboxEscapeBench escape rate < 5% documented in `docs/aura-quality-snapshot.md`. | Standard Stack (sidecar/Dockerfile/seccomp), Architecture Patterns (HTTP runner + sidecar wire), Don't Hand-Roll (seccomp/gVisor), Validation Architecture (negative-test security signals + bench), Common Pitfalls (deny-list trap, privileged-disables-seccomp, QEMU divergence). |
</phase_requirements>

## Summary

This phase replaces `sandbox.Stub` with a production-grade isolated execution runner. The CONTEXT.md already locks every architectural decision; research confirms each against authoritative sources and surfaces the concrete implementation patterns, exact commands, and pitfalls the planner needs.

The isolation model is a two-tier defense: **gVisor `runsc` as the primary syscall-intercepting boundary on x86** (Modal's production model — verified preliminary/non-GA on arm64, 4KB-page only, so x86-only-primary is correct per D-05/D-06), layered over a **hardened-container floor** (positive seccomp allowlist + `userns-remap` + `cap_drop: ALL` + `no-new-privileges` + `read_only` + tmpfs `/tmp` + pids/mem/cpu/nofile limits) that also serves as the standalone arm64 fallback. The Go side stays runtime-agnostic: `DockerRunner` is a pure HTTP client (D-08), so switching `runc`↔`runsc` is a compose/`daemon.json` concern with zero Go change.

The sidecar is a stdlib-only Python `http.server` (no pip, no external deps) exposing `POST /exec/python` and `POST /exec/shell`, running `subprocess.run` with a timeout and 1 MiB per-stream server-side truncation. The runner maps the JSON response 1:1 onto the existing `sandbox.Result` and formats the lean preview (D-17) through `tools.NewResult` (D-25 spillover). The `execute` tool registers exactly like the existing deferred tools (`ask_user`/`read_tool_output` are the in-repo templates).

**Primary recommendation:** Adopt the **Docker default seccomp profile (moby `profiles/seccomp/default.json`) as the allowlist baseline** — it is already a positive allowlist (`defaultAction: SCMP_ACT_ERRNO`, ~44 syscalls blocked from ~300+), is multi-arch by-name, and is the most battle-tested published profile — then HARDEN it by removing the dangerous-but-default-allowed syscalls (D-10's hard-exclude set: `ptrace`, `unshare`, `process_vm_readv`, `bpf`, `kexec_load`, `userfaultfd`, `mount`, plus the network socket syscalls for net-none). Do NOT hand-author an 80-syscall list from scratch — start from the published baseline and subtract. Validate the result with the 18-scenario deterministic bench (D-01). **Two PRD-amendment commits (D-05 + D-09) are hard planning prerequisites.**

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Untrusted code execution | Sidecar container (Python `http.server` + `subprocess`) | — | Owns the actual process spawn; the Go side never spawns user code. |
| Isolation boundary | Container runtime (`runsc` x86 / `runc`+seccomp arm64) | Host Docker daemon (`userns-remap`) | gVisor intercepts syscalls in user space; daemon-level userns-remap + per-container seccomp/caps form the floor. |
| Runner / wire protocol | Go `internal/sandbox` (HTTP client) | — | Pure HTTP, runtime-agnostic (D-07/D-08). Maps JSON → `sandbox.Result`. |
| Tool exposure to model | Go `internal/agent/tools/execute.go` (deferred tool) | `tool_search` hook | Registered like existing deferred tools; loaded on demand. |
| Result shaping / spillover | Go `tools.NewResult` (existing, unchanged) | RunDir sidecar files | Reuses the D-25 cap→preview→spillover already shipped. |
| CLI surface | Go `cmd/aura/main.go` switch dispatcher | — | `aura exec` is a new `case`, hand-rolled (no cobra per repo convention). |
| Lifecycle / orchestration | Compose-managed sidecar service + one-shot auto-start (D-09) | — | No pool in 2a (stateless = fresh subprocess per call). |
| Escape-rate measurement | `scripts/sandbox_escape_bench.sh` (deterministic) | CI DinD job | Gates merge; emits escape-rate to quality-snapshot. |

## Standard Stack

### Core
| Component | Version | Purpose | Why Standard |
|-----------|---------|---------|--------------|
| gVisor `runsc` | latest release (apt repo `release` track) | Primary x86 isolation: user-space kernel intercepting guest syscalls | Modal's production model for untrusted multi-tenant code `[VERIFIED: gvisor.dev docs + AISI/Modal grounding]`. Strongest isolation without `/dev/kvm`. |
| Docker `runc` (default) | bundled with Docker Engine | arm64 fallback runtime + the OCI runtime gVisor delegates to | Native OCI runtime; gVisor-arm64 is non-GA so `runc`+seccomp is the arm64 floor `[VERIFIED: gvisor.dev arm64 compatibility — preliminary, 4KB-page only]`. |
| `python:3.12-slim` | 3.12-slim (pin a digest at plan time) | Sidecar base image (stdlib-only `http.server`, no pip) | PRD file target; smallest official Python with bash addable via apt. `[CITED: prd.md §Slice 2 file targets]` |
| Docker default seccomp profile | moby `profiles/seccomp/default.json` (HEAD) | **Allowlist baseline to harden from** (subtract dangerous syscalls) | Already a positive allowlist (`SCMP_ACT_ERRNO` default), multi-arch by-name, most battle-tested. `[VERIFIED: docs.docker.com/engine/security/seccomp + github.com/moby/moby]` |
| Go stdlib `net/http` | go1.26.3 (`go.mod`) | `DockerRunner` HTTP client | Mirrors the existing `internal/llm/openai_compat` client pattern (ctx-rides-timeout, `DisableKeepAlives` for goleak). `[VERIFIED: codebase grep internal/llm/openai_compat/client.go]` |

### Supporting
| Component | Version | Purpose | When to Use |
|-----------|---------|---------|-------------|
| `qemu-user-static` + binfmt | latest | arm64 emulation in CI to validate the multi-arch seccomp profile | D-12: `docker buildx` / `--platform linux/arm64` build + run of the sidecar in the CI DinD job. |
| Docker Compose v2/v5 | repo uses Compose (`compose.yaml`, `name: aura`) | Sidecar service definition + `runsc` profile (`compose.gvisor.yaml`) | D-04: wire `runtime: runsc` for x86, plain `runc` for arm64. |
| Inspect AI | latest (opt-in only) | D-03 LLM-driven red-team (`UKGovernmentBEIS/sandbox_escape_bench` harness) | Scheduled/manual only — NOT a per-merge dependency. Do NOT add to the gating CI. |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| gVisor (primary) | Firecracker/Kata microVM | Stronger (real hypervisor) but needs `/dev/kvm` → REJECTED per D-05a (KVM-less infra). |
| gVisor (primary) | Plain `runc` + seccomp only | The arm64 fallback; weaker (shared host kernel) but the only production-grade option identical on x86+arm64. Bench-validated as floor (D-06). |
| Hardened from moby default | Hand-authored 80-syscall list | Reinventing a battle-tested profile; high risk of missing a syscall Python 3.12/bash needs → flaky runtime. Subtract-from-baseline is safer (see Don't Hand-Roll). |
| userns-remap | Rootless Docker | Rootless forces `seccomp=unconfined` + `apparmor=unconfined` (guts the allowlist) and can't coexist with userns-remap (moby #48521) → REJECTED per D-14. `[VERIFIED: thelinuxcode 2026 + docs.docker.com/engine/security/rootless]` |

**Installation (CI / production host — runsc registration):**
```bash
# gVisor apt repo (x86_64 + arm64 builds both published)
curl -fsSL https://gvisor.dev/archive.key | sudo gpg --dearmor -o /usr/share/keyrings/gvisor-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" \
  | sudo tee /etc/apt/sources.list.d/gvisor.list
sudo apt-get update && sudo apt-get install -y runsc
# Register runsc as a Docker runtime (writes the runtimes.runsc entry into daemon.json)
sudo runsc install
sudo systemctl restart docker     # restart daemon to pick up the runtime
```
Resulting `/etc/docker/daemon.json` (x86 production + CI DinD inner daemon — D-14 + D-04):
```json
{
  "userns-remap": "default",
  "no-new-privileges": true,
  "runtimes": { "runsc": { "path": "/usr/bin/runsc" } }
}
```
`[VERIFIED: gvisor.dev/docs/user_guide/quick_start/docker + docs.docker.com/engine/security/userns-remap]`

**Version verification (run at plan time — versions move):**
```bash
# gVisor: no semver; track the 'release' channel date stamp
runsc --version
# python:3.12-slim — pin a digest
docker buildx imagetools inspect python:3.12-slim
# moby default seccomp profile — fetch HEAD at plan time, do not snapshot an old copy
curl -fsSL https://raw.githubusercontent.com/moby/moby/master/profiles/seccomp/default.json | head
```

## Package Legitimacy Audit

> This phase installs **no Go modules** (stdlib `net/http` only) and the Python sidecar is **stdlib-only (no pip)**. The only external artifacts are OS/container-level: the `runsc` Debian package (Google-published apt repo), the `python:3.12-slim` official Docker image, `qemu-user-static`, and `bash`/`coreutils` via apt. No npm/PyPI/crates registry packages → the slopcheck registry-hallucination vector does not apply.

| Artifact | Source | Provenance | Disposition |
|----------|--------|------------|-------------|
| `runsc` | `storage.googleapis.com/gvisor/releases` apt repo (GPG-signed) | Google gVisor official | Approved `[VERIFIED: gvisor.dev install docs]` |
| `python:3.12-slim` | Docker Hub official library | Docker Official Image | Approved — pin digest at plan time `[CITED: prd.md]` |
| `qemu-user-static` | distro apt | Debian/Ubuntu official | Approved (CI-only) |
| `bash`, `coreutils` | distro apt in sidecar build | Debian official | Approved `[CITED: prd.md Dockerfile target]` |
| Inspect AI + `sandbox_escape_bench` | `UKGovernmentBEIS/sandbox_escape_bench` (GitHub) | UK AISI official repo | Opt-in only (D-03) — NOT in the gating path `[VERIFIED: GitHub repo exists]` |

**Packages removed due to slopcheck [SLOP] verdict:** none (no registry packages in scope).
**Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
  aura chat / agent loop                  aura exec <lang> <code>   (D-19 CLI)
          │                                        │
          ▼                                        ▼
  tools.Registry ──(deferred)──► execute.go ───────┴──────────────┐
   (tool_search loads spec)        │ validate lang∈{py,shell}     │
                                   │ map timeout_sec (cap 600s)   │
                                   ▼                              │
                          sandbox.Runner (interface)              │
                                   │                              │
                                   ▼                              │
                       DockerRunner (HTTP client, docker.go)      │
                          │  POST AURA_SANDBOX_URL/exec/<lang>     │
                          │  {code, timeout_sec}                  │
            connect fail? │                                       │
                          ▼                                       │
              auto-start ONCE (D-09):                             │
              `docker compose up -d aura-sandbox`                 │
              health-check, retry once                            │
                  │ still down → ErrSandboxUnreachable ───────────┘ (exit 70)
                  │ malformed resp → ErrSandboxProtocol
                  ▼
   ┌─────────────────────────────────────────────────────────────┐
   │ aura-sandbox container  (runtime: runsc on x86 / runc arm64) │
   │  ┌─────────────────────────────────────────────────────┐    │
   │  │ sidecar.py (stdlib http.server, non-root uid 65532)  │    │
   │  │   /exec/python ─► subprocess.run([python3,-c,code])  │    │
   │  │   /exec/shell  ─► subprocess.run([bash,-c,code])     │    │
   │  │   timeout / OOM / pids → limit_hit                   │    │
   │  │   stdout/stderr truncate 1 MiB each → truncated      │    │
   │  └─────────────────────────────────────────────────────┘    │
   │  hardening floor (D-06/D-14):                                │
   │   cap_drop:ALL · no-new-privileges · read_only · tmpfs /tmp  │
   │   network_mode:none · pids_limit:64 · mem:512m · cpus:1.0    │
   │   ulimit nofile:64 · seccomp=sandbox/seccomp.json (allowlist)│
   │  daemon floor: userns-remap default (D-15)                   │
   └─────────────────────────────────────────────────────────────┘
                  │ JSON {stdout,stderr,exit_code,elapsed_ms,truncated,limit_hit}
                  ▼
        sandbox.Result + limit_hit/truncated
                  │
                  ▼
      lean preview formatter (D-17) ──► tools.NewResult (D-25 cap+spillover)
                  │                            │
                  ▼                            ▼
        ToolResult.Preview              RunDir sidecar file (if >cap)
```

### Recommended Project Structure
```
internal/sandbox/
├── sandbox.go        # EXISTING — Runner interface + Result (Stub removed/replaced)
├── docker.go         # NEW ~220 — DockerRunner HTTP client + one-shot auto-start (D-09)
├── config.go         # NEW ~50  — AURA_SANDBOX_* env (URL, TIMEOUT_SEC, RUNTIME)
├── errors.go         # NEW ~20  — ErrSandboxUnreachable, ErrSandboxProtocol sentinels (D-18)
└── docker_test.go    # NEW ~120 — //go:build sandbox_integration tier
internal/agent/tools/
└── execute.go        # NEW ~140 — deferred `execute` tool, lean preview (D-17) via NewResult
sandbox/
├── Dockerfile        # NEW ~30  — python:3.12-slim + apt bash/coreutils + non-root uid 65532
├── sidecar.py        # NEW ~150 — stdlib http.server, /exec/python + /exec/shell
└── seccomp.json      # NEW ~80  — multi-arch positive allowlist (moby default minus dangerous)
compose.yaml          # DIFF — append aura-sandbox service (hardening flags)
compose.gvisor.yaml   # NEW — overlay/profile that sets runtime: runsc (x86 default-on, D-04)
scripts/
└── sandbox_escape_bench.sh   # NEW — deterministic 18-scenario port, emits escape-rate
cmd/aura/main.go      # DIFF ~+60 — aura exec case + reg.Register(&tools.Execute{...})
docs/aura-quality-snapshot.md  # DIFF — escape-rate + QEMU-arm64-caveat note
```

### Pattern 1: Runtime-agnostic HTTP runner (D-07/D-08)
**What:** `DockerRunner` is a pure `net/http` client; the isolation runtime is a compose/`daemon.json` concern invisible to Go.
**When to use:** Always — this is the core decision that keeps `runc`↔`runsc` swap zero-cost.
**Example (grounded in the existing llm client):**
```go
// Source: codebase internal/llm/openai_compat/client.go — proven pattern.
// NO http.Client.Timeout (it counts body-read time and would abort mid-stream);
// the total timeout rides the request ctx. DisableKeepAlives keeps goleak
// order-independent (default Transport persistConn goroutines linger otherwise).
httpClient := &http.Client{
    Transport: &http.Transport{
        DialContext:       (&net.Dialer{Timeout: connectTimeout}).DialContext,
        DisableKeepAlives: true,
    },
}
// timeout_sec → context.WithTimeout(ctx, …) per call (cap 600s, AURA_SANDBOX_TIMEOUT_SEC).
```

### Pattern 2: Stdlib sidecar — subprocess with timeout + limit detection (D-16)
**What:** `http.server` BaseHTTPRequestHandler dispatching `/exec/python` and `/exec/shell` to `subprocess.run([interp, "-c", code], timeout=…, capture_output=True)`. Map `subprocess.TimeoutExpired` → `limit_hit:"timeout"`; negative exit code from SIGKILL (137 = OOM-kill, the container's `mem` cgroup) → `limit_hit:"oom"`; `BlockingIOError`/fork failure under `pids_limit` → `limit_hit:"pids"`. Truncate each stream to 1 MiB server-side, set `truncated`.
**When to use:** The sidecar's only job.
**Example shape:**
```python
# Source: Python stdlib docs (http.server, subprocess) — no external deps.
import subprocess, time, json
def run(interp, code, timeout):
    t0 = time.monotonic()
    try:
        p = subprocess.run([interp, "-c", code], capture_output=True,
                           timeout=timeout, text=True)
        out, err, rc, hit = p.stdout, p.stderr, p.returncode, None
    except subprocess.TimeoutExpired as e:
        out = e.stdout or ""; err = e.stderr or ""; rc = 124; hit = "timeout"
    if rc == 137: hit = "oom"          # SIGKILL from the mem cgroup
    out, t1 = out[:1<<20], len(out) > 1<<20
    err, t2 = err[:1<<20], len(err) > 1<<20
    return {"stdout": out, "stderr": err, "exit_code": rc,
            "elapsed_ms": int((time.monotonic()-t0)*1000),
            "truncated": t1 or t2, "limit_hit": hit}
```
Note: shell uses `["bash","-c",code]`; python uses `["python3","-c",code]`. `limit_hit:"pids"` is detected when the spawned process itself reports a fork failure — capture from stderr text or a non-zero rc with the cgroup `pids.events` signal; the exact mechanism is Claude's discretion (D-16 only requires the field be reported, never guessed Go-side).

### Pattern 3: Deferred tool registration (existing repo convention)
**What:** `execute` sets `Deferred: true`; the model sees only Name+Summary until `tool_search` loads the full spec. Register in `buildRegistry()`.
**Example:**
```go
// Source: codebase cmd/aura/main.go buildRegistry + internal/agent/tools/ask_user.go (deferred template)
reg.Register(&tools.Execute{Runner: dockerRunner})   // Deferred:true in Spec()
```

### Pattern 4: Lean preview → NewResult (D-17 + D-25, no new spillover code)
**What:** Build the lean string (stdout verbatim; `stderr:` only if non-empty; `exit_code: N` only when non-zero; `[limit: …]`/`elapsed_ms` only when a limit hit; `(no output, exit 0)` when empty) then pass the WHOLE string to `tools.NewResult(ctx, s)`. The D-25 cap + sidecar spillover apply unchanged — `execute` writes zero spillover logic.

### Anti-Patterns to Avoid
- **Deny-list seccomp (the old 7-syscall profile):** weaker than the Docker default ~44-blocked profile. PITFALLS Pitfall #1 / amendment #32 explicitly forbid it. Use a positive allowlist.
- **Hardcoding syscall numbers in `seccomp.json`:** x86 numbers ≠ arm64 numbers. Use by-name + `architectures: ["SCMP_ARCH_X86_64","SCMP_ARCH_AARCH64"]` (amendment #30; libseccomp resolves per-arch).
- **`http.Client.Timeout` on the runner:** counts body-read; ride the ctx instead (existing-code lesson).
- **Driving the container runtime from Go:** D-08 forbids it — HTTP only; lifecycle is compose + the one-shot auto-start helper.
- **Treating a non-zero script exit or a seccomp EPERM as a Go error:** D-18 — code's fault → ToolResult; only env failures are typed errors.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| seccomp allowlist | Author 80 syscalls from memory | Start from moby `default.json`, SUBTRACT the dangerous set | Hand-authoring risks omitting a syscall Python 3.12/bash needs (futex, rseq, clone3, epoll, …) → flaky runtime; the published profile is battle-tested and multi-arch by-name. |
| Syscall interception / user-space kernel | Custom ptrace/seccomp-bpf supervisor | gVisor `runsc` | gVisor is a mature, Modal-production user-space kernel; rebuilding it is a multi-year project. |
| Spillover for large output | Re-truncate in execute.go | `tools.NewResult` (D-25, shipped) | Already handles cap→preview→sidecar uniformly. |
| HTTP client lifecycle / goleak | New transport tuning | Copy `internal/llm/openai_compat` client shape | Proven ctx-timeout + DisableKeepAlives pattern that passes goleak. |
| CLI arg parsing | Adopt cobra | Hand-rolled switch case (repo convention) | `cmd/aura/main.go` is a switch dispatcher; D-19 + CLAUDE.md mandate matching it. |
| Container-root = host-root mitigation | Custom UID juggling | Daemon `userns-remap: default` | Maps container UID 0 → unprivileged host UID while keeping seccomp + full Docker functionality. |

**Key insight:** This domain is a minefield of subtle, battle-tested-elsewhere solutions (gVisor, the moby seccomp profile, userns-remap). Every hand-rolled alternative is both weaker and more code. The phase's value is *correctly composing* published primitives + *proving the composition* with the deterministic bench — not inventing isolation.

## Runtime State Inventory

> Not a rename/refactor phase, but it replaces a live stub and adds host-level state. Reporting the relevant categories explicitly:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — 2a is stateless, tmpfs `/tmp`, no DB rows (the `aura.sandbox_sessions` table is Slice 2b/Phase 8, OUT of scope — verified against prd.md file targets). | None |
| Live service config | New compose service `aura-sandbox`; new `daemon.json` keys (`userns-remap`, `runtimes.runsc`) on production + CI DinD daemons. These live in compose/daemon config, partly outside git (host `daemon.json`). | Document daemon requirement (D-14/D-15); CI job writes the DinD inner `daemon.json`. |
| OS-registered state | `runsc` registered as a Docker runtime via `runsc install` (writes `daemon.json`); requires daemon restart. | Install step in CI + production runbook. |
| Secrets/env vars | New `AURA_SANDBOX_URL` (default `http://127.0.0.1:18901`), `AURA_SANDBOX_TIMEOUT_SEC` (cap 600s), `AURA_SANDBOX_RUNTIME` (per-arch default). No secrets. | Add to `config.go` via `envDefault`/`envIntDefault`. |
| Build artifacts | `sandbox/` image built locally/CI; the `Stub` symbol in `sandbox.go` is removed — any test wiring `sandbox.Stub` into a registry must switch to the real runner or a test double. | Grep `sandbox.Stub` usages before deleting (the agent-loop smoke test referenced it). |

## Common Pitfalls

### Pitfall 1: Deny-list seccomp weaker than the Docker default
**What goes wrong:** A small deny-list (e.g. the old 7-syscall profile) blocks fewer syscalls than Docker's default ~44-blocked profile — a regression masquerading as hardening.
**Why it happens:** Intuition says "block the bad ones"; reality is the attack surface is the *allowed* set.
**How to avoid:** Positive allowlist (`defaultAction: SCMP_ACT_ERRNO`), start from moby `default.json`, subtract the dangerous set (D-10).
**Warning signs:** A `seccomp.json` whose `defaultAction` is `SCMP_ACT_ALLOW`. The deterministic bench (D-01) catches over-permissive profiles.

### Pitfall 2: `--privileged` (DinD) disables seccomp entirely
**What goes wrong:** The CI uses Docker-in-Docker; the *outer* DinD container runs `--privileged`, and `--privileged` disables seccomp in all Docker versions even if a profile is specified `[VERIFIED: docs.docker.com/engine/security/seccomp]`.
**Why it happens:** Conflating the privileged DinD wrapper with the sidecar it launches.
**How to avoid:** The seccomp profile must be applied to the **inner sidecar container** (launched by the inner DinD daemon), NOT relaxed because the outer DinD is privileged. Verify in CI that the running sidecar's `Seccomp` is the allowlist (inspect the container, or assert an EPERM negative test passes) — a sub-second "PASS" is a skip tell (no-skip-as-green).
**Warning signs:** Negative tests (ptrace/socket → EPERM) silently pass without the profile loaded.

### Pitfall 3: userns-remap vs Docker-in-Docker tension (D-14)
**What goes wrong:** userns-remap on a daemon that itself runs inside another container can conflict; some docs state "DinD requires real root and is incompatible with userns-remap" `[VERIFIED: oneuptime/docs.docker.com]`.
**Why it happens:** Two distinct daemons in CI — the *outer* host daemon (runs the privileged DinD container) and the *inner* DinD daemon (runs the sidecar). D-14 puts `userns-remap: default` on the **inner** daemon (Aura owns it). The constraint is about enabling userns-remap on a daemon while *that daemon* runs inside a userns-remapped parent.
**How to avoid:** Set `userns-remap: default` only on the inner DinD daemon's `daemon.json`; the outer DinD container runs `--privileged --userns=host` (host userns). Validate the sidecar actually starts under the inner remapped daemon. Flag to planner: this is the single most likely CI-setup failure — budget a checkpoint to prove `userns-remap` is live in CI (D-14 says "validated in the CI DinD daemon").
**Warning signs:** Sidecar fails to start with permission errors on the remapped UID range, or `cap_drop`/seccomp silently not applied.

### Pitfall 4: QEMU arm64 seccomp emulation diverges from a real kernel (D-12)
**What goes wrong:** CI validates the multi-arch profile under QEMU; QEMU's syscall emulation can differ from a real arm64 kernel's seccomp behavior → a profile that passes in QEMU might still misbehave on a real DGX.
**Why it happens:** QEMU user emulation translates syscalls; the seccomp-BPF filter sees emulated, not native, syscall dispatch.
**How to avoid:** Treat QEMU validation as necessary-not-sufficient. Record in `docs/aura-quality-snapshot.md` that **real-DGX arm64 confirmation is a tracked obligation before any production arm64 deployment** (D-12 caveat). Do NOT claim arm64 production-readiness from a green QEMU run.
**Warning signs:** A green QEMU job presented as production arm64 sign-off.

### Pitfall 5: gVisor arm64 is non-GA — never default it on arm64
**What goes wrong:** Enabling `runtime: runsc` on arm64 silently degrades (gVisor arm64 is preliminary, 240/294 syscall coverage, 4KB-page-only) `[VERIFIED: gvisor.dev arm64 compatibility]`.
**How to avoid:** `AURA_SANDBOX_RUNTIME` default resolves to `runsc` on x86, `runc` on arm64 (D-07). The `compose.gvisor.yaml` overlay is x86-only.
**Warning signs:** A single default `runtime: runsc` in the base compose without per-arch resolution.

### Pitfall 6: SandboxEscapeBench scenario count discrepancy (port-time trap)
**What goes wrong:** The paper's conceptual taxonomy is **18 scenarios (orchestration/runtime/kernel)** but the implemented GitHub repo lists ~20 named scenarios across Docker(11)/Kubernetes(5)/Kernel(4). A naive 1:1 port either over- or under-counts the denominator → a wrong escape-rate.
**How to avoid:** D-01 locks "all 18". Port the **paper's 18 conceptual scenarios**; map the Aura-structurally-forbidden misconfigs (docker socket, privileged, writable host mount, excess caps — ~half) to "config-regression" assertions (must stay 0) and the runtime/kernel CVE scenarios to the live escape-rate denominator. Kubernetes-only scenarios are not applicable to a single-container sidecar — document the exclusion explicitly in the bench script so the count is auditable (see Open Questions Q1).
**Warning signs:** An escape-rate computed over a denominator that includes inapplicable K8s scenarios.

## Code Examples

### `AURA_SANDBOX_*` config wiring (existing convention)
```go
// Source: codebase internal/config/config.go envDefault/envIntDefault
SandboxURL:        envDefault("AURA_SANDBOX_URL", "http://127.0.0.1:18901"),
SandboxTimeoutSec: envIntDefault("AURA_SANDBOX_TIMEOUT_SEC", 30), // cap 600 enforced in runner
SandboxRuntime:    envDefault("AURA_SANDBOX_RUNTIME", defaultRuntimeForArch()), // runsc x86 / runc arm64
```

### Sentinel errors (D-18, existing convention)
```go
// Source: codebase internal/askuser/store.go (errors.New sentinel pattern)
var (
    ErrSandboxUnreachable = errors.New("sandbox sidecar unreachable (auto-start failed)")
    ErrSandboxProtocol    = errors.New("sandbox sidecar returned a malformed response")
)
```

### Multi-arch seccomp skeleton (D-11)
```json
{
  "defaultAction": "SCMP_ACT_ERRNO",
  "defaultErrnoRet": 1,
  "architectures": ["SCMP_ARCH_X86_64", "SCMP_ARCH_AARCH64"],
  "syscalls": [
    { "names": ["read","write","futex","rseq","clone3","epoll_create1","..."],
      "action": "SCMP_ACT_ALLOW" }
  ]
}
```
Build by fetching moby `default.json` at plan time and removing `ptrace`, `unshare`, `process_vm_readv`, `bpf`, `kexec_load`, `userfaultfd`, `mount`, and the network socket syscalls (`socket`, `connect`, `bind`, `sendto`, … — net-none). `[CITED: github.com/moby/moby/blob/master/profiles/seccomp/default.json]`

### compose `aura-sandbox` service (hardening floor — D-06/D-14/D-15)
```yaml
# x86 production adds `runtime: runsc` via compose.gvisor.yaml overlay (D-04).
# userns-remap is a daemon.json setting (D-15), NOT a service field.
services:
  aura-sandbox:
    build: ./sandbox
    container_name: aura-sandbox
    user: "65532:65532"
    read_only: true
    tmpfs: [ "/tmp" ]
    network_mode: none
    cap_drop: [ "ALL" ]
    security_opt:
      - "no-new-privileges:true"
      - "seccomp=./sandbox/seccomp.json"
    pids_limit: 64
    mem_limit: 512m
    cpus: 1.0
    ulimits: { nofile: 64 }
    ports: [ "127.0.0.1:18901:18901" ]   # loopback-only, matches repo T-1.07 convention
```
`[CITED: prd.md §Slice 2 acceptance 2a + compose.yaml repo convention]`

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Deny-list seccomp (7 syscalls) | Positive allowlist hardened from moby default | amendment #32, 2026-06-01 | Mandatory; deny-list forbidden. |
| D12: container+seccomp primary, gVisor escalation-only | gVisor-primary on x86, hardened-container floor (D-05) | CONTEXT 2026-06-01 | **PRD-amendment prerequisite.** |
| PRD acceptance "sidecar down → clear error" | "sidecar down AND auto-start fails → clear error" (D-09) | CONTEXT 2026-06-01 | **PRD-amendment prerequisite.** |
| Rootless Docker hardening | userns-remap (rootless gut seccomp + ⊥ userns) | Docker 2026 best-practice | D-14; rootless rejected. |

**Deprecated/outdated:**
- The PRD's verbatim D12 ("gVisor = escalation seam only") and acceptance #4 ("sidecar down → clear error") are superseded by D-05 and D-09 respectively — must be amended in `prd.md` + `.planning/DECISIONS.md` (D12) + `.planning/ROADMAP.md` (Phase 5 SC #5) BEFORE code (PRD-first principle, CLAUDE.md).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The moby `default.json` minus the dangerous set yields a working Python-3.12+bash allowlist of ~80–100 syscalls without empirical strace. | Standard Stack / Don't Hand-Roll | If a needed syscall (e.g. a newer `clone3`/`rseq` path) is missing, exec flakes. Mitigation: the integration smoke (`print(2+2)`) + deterministic bench will catch a too-tight profile early; iterate by adding back only safe syscalls, never the hard-exclude set. |
| A2 | gVisor's apt `release` channel publishes arm64 binaries (so the floor host can at least install runsc even if non-GA). | Standard Stack | Low — arm64 uses `runc` anyway (D-07); runsc-arm64 install is not on the critical path. |
| A3 | `limit_hit:"oom"` is reliably detectable via SIGKILL exit code 137 from the mem cgroup. | Architecture Pattern 2 | Medium — cgroup v1 vs v2 OOM signalling differs; the sidecar may need to read `/sys/fs/cgroup/memory.events`. Planner should treat exact OOM detection as a small spike. |
| A4 | The 18-scenario port can exclude Kubernetes-layer scenarios as inapplicable to a single-container sidecar without violating D-01's "all 18". | Common Pitfalls #6 / Open Questions | Medium — needs an explicit auditable mapping in the bench script; see Q1. The escape-rate denominator must be the applicable subset, with excluded scenarios documented as N/A (not silently dropped). |

## Open Questions

1. **SandboxEscapeBench: 18 (paper) vs ~20 (repo) scenario reconciliation.**
   - What we know: The arXiv paper + AISI blog state **18 scenarios across orchestration/runtime/kernel** (orchestration ×4 confirmed). The `UKGovernmentBEIS/sandbox_escape_bench` repo README enumerates ~20 named implementations grouped Docker/Kubernetes/Kernel, several Kubernetes-specific (k8s_rbac, k8s_runc CVE-2024-21626, crio escape) and not applicable to Aura's single Docker sidecar.
   - What's unclear: Exactly which 18 the paper counts, and whether the repo's K8s scenarios are inside or outside that 18.
   - Recommendation: Port the **applicable** scenarios (Docker-misconfig → config-regression-must-stay-0; Docker/kernel CVE → live denominator), and in `scripts/sandbox_escape_bench.sh` print a per-scenario table with an explicit `N/A (kubernetes — no orchestrator in Aura's deployment)` line for each excluded one, so the count and escape-rate denominator are auditable. Confirm the exact 18 against the paper PDF during planning (arXiv blocks WebFetch; `gh`/manual fetch the PDF).

2. **OOM/pids `limit_hit` detection mechanism in the stdlib sidecar.**
   - What we know: timeout → `subprocess.TimeoutExpired`; OOM ≈ exit 137; pids-cap → fork failure.
   - What's unclear: Robust OOM detection under cgroup v2 vs v1 from inside a `read_only` container with `cap_drop: ALL`.
   - Recommendation: Small spike during planning; D-16 only requires the field be reported by the sidecar (never guessed Go-side), so a best-effort heuristic (137 → oom, fork-fail text → pids) is acceptable for 2a with a tracked refinement note.

3. **Auto-start (D-09) shelling out to `docker compose` from inside a containerized Aura.**
   - What we know: D-09 has the runner run `docker compose up -d aura-sandbox` on connect failure.
   - What's unclear: When Aura itself runs in a container (production), it has no docker CLI / socket by default — auto-start may only work in dev/CI where a docker CLI is on PATH.
   - Recommendation: Make auto-start best-effort and gated on `docker` being on PATH; if absent, go straight to `ErrSandboxUnreachable` with a message telling the operator to `docker compose up -d aura-sandbox`. Do NOT mount the docker socket into Aura (that would re-introduce the #1 escape vector the bench tests for). Flag for planner.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|-------------|-----------|---------|----------|
| Docker Engine + Compose | sidecar build/run, integration tier | Assumed (repo already uses compose for PG/Neo4j) | — | none — phase is Docker-centric by design |
| gVisor `runsc` | x86 primary isolation profile + CI gating | NOT installed in this research env | — | `runc` + seccomp floor (D-06) is the functional fallback; runsc required for the production x86 profile + D-13 gating CI |
| `qemu-user-static` / binfmt | D-12 arm64 validation | NOT verified here | — | none — needed for the arm64 CI leg |
| `docker buildx` | multi-arch sidecar build | Assumed with modern Docker | — | — |

**Missing dependencies with no fallback:** `runsc` (for the x86 production profile + gating bench) and the QEMU/binfmt arm64 leg must be installed in the CI runner — the planner must include explicit install steps (commands in Standard Stack). The research sandbox itself cannot run them.

**Missing dependencies with fallback:** On a host without `runsc`, the hardened-container floor (`runc` + seccomp + userns-remap) is functionally complete and is the arm64 production boundary — dev can use it without gVisor.

## Validation Architecture

> nyquist_validation is enabled (config.json `workflow.nyquist_validation: true`). This phase's correctness signals are SECURITY BOUNDARIES: each negative test asserts a syscall/escape that MUST fail with EPERM/ENOENT. A passing positive test alone is insufficient — the negatives are the load-bearing signal.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib) + table-driven + `goleak` (repo mandate) + the `golang-testing` skill patterns |
| Build-tag tier | `//go:build sandbox_integration` (real sidecar required; `t.Fatal` under `$CI` when env unset — no-skip-as-green) |
| Config file | none — Go modules; sidecar env via `AURA_SANDBOX_URL` etc. |
| Quick run command | `go test ./internal/sandbox/ ./internal/agent/tools/` (unit, no sidecar) |
| Integration command | `go test -tags sandbox_integration -race ./internal/sandbox/...` (sidecar up) |
| Bench command | `scripts/sandbox_escape_bench.sh` (deterministic 18-scenario port → escape-rate) |
| Full suite | `make quality-full` (folds sandbox coverage into the 85% floor — CLAUDE.md) |

### Phase Requirements → Test Map
| Req ID | Behavior (ROADMAP SC) | Test Type | Automated Command | File Exists? |
|--------|------------------------|-----------|-------------------|-------------|
| CAP-01 / SC#1 | `aura exec python "print(2+2)"` → `4`, sidecar idle within timeout | integration | `go test -tags sandbox_integration ./internal/sandbox/ -run TestRunner_PythonHappy` | ❌ Wave 0 |
| CAP-01 / SC#2 | `ctypes…ptrace(...)` → EPERM; `open('/proc/self/root/etc/shadow')` → ENOENT/EPERM | integration (negative) | `... -run TestRunner_PtraceBlocked` / `TestRunner_ProcRootDenied` | ❌ Wave 0 |
| CAP-01 / SC#3 | `socket().connect(('1.1.1.1',80))` → EPERM; `unshare(CLONE_NEWNET)` → EPERM | integration (negative) | `... -run TestRunner_SocketBlocked` / `TestRunner_UnshareBlocked` | ❌ Wave 0 |
| CAP-01 / SC#4 | `scripts/sandbox_escape_bench.sh` → escape-rate < 5% in quality-snapshot | bench (deterministic) | `scripts/sandbox_escape_bench.sh && grep 'escape-rate' docs/aura-quality-snapshot.md` | ❌ Wave 0 |
| CAP-01 / SC#5 | compose `aura-sandbox` has `cap_drop:ALL`, `no-new-privileges`, `read_only`, `pids_limit:64`, userns-remap (daemon) all set | config-assertion | `scripts/sandbox_escape_bench.sh` config-regression checks (must stay 0) | ❌ Wave 0 |
| D-16/D-17 | limit_hit paths: timeout / oom / pids reported; lean preview shape | integration + unit | `TestRunner_TimeoutLimitHit`, `TestExecute_LeanPreview` (unit, fake runner) | ❌ Wave 0 |
| D-18 | `ErrSandboxUnreachable` after auto-start fails; `ErrSandboxProtocol` on malformed resp; `aura exec` exit 70 | unit + cli | `TestDockerRunner_UnreachableSentinel`, `TestRunShell_MalformedProtocol`, `TestCLIExec_Exit70` | ❌ Wave 0 |
| D-11/D-12 | seccomp profile valid + loaded on both arches; arm64 under QEMU | integration | QEMU leg: `docker buildx ... --platform linux/arm64` run of the negative tests | ❌ Wave 0 |

### Negative-Test Inventory (the load-bearing signals)
Each MUST fail with the expected errno (a PASS that does NOT trigger the boundary is a false-green):
- `ptrace` → EPERM (allowlist excludes ptrace)
- `socket`/`connect` → EPERM (net-none + socket syscalls excluded)
- `unshare(CLONE_NEWNET)` → EPERM (allowlist excludes unshare; blocks in-container userns)
- `open('/proc/self/root/etc/shadow')` → ENOENT/EPERM (read-only rootfs + no host mount)
- `mount(...)` → EPERM (excluded)
- positive control: `print(2+2)` → `4`, `echo hello` → `hello` (proves the allowlist is not too tight)
- limit controls: infinite loop → `limit_hit:"timeout"`; large alloc → `limit_hit:"oom"`; fork bomb → `limit_hit:"pids"`

### Sampling Rate
- **Per task commit:** `go test ./internal/sandbox/ ./internal/agent/tools/` (unit, fast).
- **Per wave merge:** `go test -tags sandbox_integration -race ./internal/sandbox/...` + `scripts/sandbox_escape_bench.sh` (sidecar up).
- **Phase gate (Gate 3 DoD):** full `make quality-full` green (≥85% combined coverage) + escape-rate < 5% recorded + the deterministic bench config-regressions at 0 + QEMU arm64 leg green (with the divergence caveat noted) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/sandbox/docker_test.go` — `//go:build sandbox_integration` happy + negative + limit + sentinel tests
- [ ] `internal/sandbox/errors.go` + unit tests for the two sentinels
- [ ] `internal/agent/tools/execute_test.go` — lean-preview shaping (unit, fake `sandbox.Runner`), deferred-spec assertions
- [ ] `cmd/aura/` exec CLI test (exit-code 70 path; reuse the re-exec subprocess pattern in `cmd/aura/agent_test.go`)
- [ ] `scripts/sandbox_escape_bench.sh` — deterministic 18-scenario port (config-regression + live denominator)
- [ ] CI DinD job: install `runsc`, write inner `daemon.json` (userns-remap + runsc), QEMU binfmt, export sandbox env (no-skip-as-green)
- [ ] Mutation spot-check (≥70% killed) target file: `internal/sandbox/docker.go` (CLAUDE.md Gate-3)

## Security Domain

> `security_enforcement` not set to false → included. This IS the security phase.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V1 Architecture (trust boundary) | yes | gVisor primary boundary + hardened-container floor; the sidecar is the trust boundary, code inside is untrusted. |
| V5 Input Validation | yes | `lang ∈ {python, shell}` strict enum (D-19); `timeout_sec` clamped to ≤600. Code itself is NOT validated (it is meant to be arbitrary) — isolation, not validation, is the control. |
| V10 Malicious Code / Sandboxing | yes | The whole phase: seccomp allowlist, cap_drop, no-new-privileges, read_only, net-none, userns-remap, gVisor. |
| V12 File/Resources | yes | tmpfs `/tmp` only, no host mount in 2a, pids/mem/cpu/nofile cgroup limits. |
| V13 API/Wire | yes | sidecar JSON contract; `ErrSandboxProtocol` on malformed; loopback-only port (`127.0.0.1:18901`). |
| V6 Cryptography | no | No crypto in scope. |
| V2/V3 Auth/Session | no | 2a stateless, single-node, loopback sidecar (auth/session is Slice 2b+ territory). |

### Known Threat Patterns for an untrusted-code sandbox
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Container escape via syscall (ptrace, unshare, bpf, mount) | Elevation of Privilege | Positive seccomp allowlist excluding the dangerous set (D-10) + gVisor interception (x86) |
| Docker-socket / privileged escape | Elevation of Privilege | Structurally forbidden — never mount the socket, never `--privileged` the sidecar; bench config-regression asserts 0 |
| Host file read (`/proc/self/root`, host mount) | Information Disclosure | read_only rootfs, no host bind mount in 2a, ENOENT/EPERM negative test |
| Network exfiltration | Information Disclosure | `network_mode: none` + socket syscalls excluded from allowlist |
| Resource exhaustion (fork bomb, OOM, CPU spin) | Denial of Service | pids_limit:64, mem:512m, cpus:1.0, ulimit nofile:64, per-call timeout (cap 600s) → `limit_hit` |
| container-root = host-root | Elevation of Privilege | userns-remap (daemon) + non-root uid 65532 + cap_drop:ALL + no-new-privileges |
| Output flooding / log injection | DoS | 1 MiB per-stream server-side truncation + D-25 spillover cap |

## Sources

### Primary (HIGH confidence)
- Docker seccomp docs — positive-allowlist model, `SCMP_ACT_ERRNO`, `--privileged` disables seccomp: https://docs.docker.com/engine/security/seccomp/
- moby default seccomp profile (the allowlist baseline): https://github.com/moby/moby/blob/master/profiles/seccomp/default.json
- Docker userns-remap docs (daemon `userns-remap: default`): https://docs.docker.com/engine/security/userns-remap/
- Docker rootless docs (rootless ⊥ seccomp profiles): https://docs.docker.com/engine/security/rootless/
- gVisor Docker quick start + install (runsc install, daemon.json runtimes): https://gvisor.dev/docs/user_guide/quick_start/docker/ , https://gvisor.dev/docs/user_guide/install/
- gVisor arm64 compatibility (preliminary/non-GA, 4KB-page only): https://gvisor.dev/docs/user_guide/compatibility/linux/arm64/
- Codebase: `internal/llm/openai_compat/client.go` (HTTP client pattern), `internal/agent/tools/{spec,result,search,ask_user}.go`, `internal/config/config.go`, `internal/askuser/store.go`, `cmd/aura/main.go`, `compose.yaml`, `prd.md §Slice 2`.

### Secondary (MEDIUM confidence)
- SandboxEscapeBench paper + repo (18 scenarios, orchestration/runtime/kernel; Modal/gVisor grounding): https://arxiv.org/abs/2603.02277 , https://github.com/UKGovernmentBEIS/sandbox_escape_bench , https://www.aisi.gov.uk/blog/can-ai-agents-escape-their-sandboxes-a-benchmark-for-safely-measuring-container-breakout-capabilities (arXiv blocks WebFetch — taxonomy cross-verified across the paper abstract, AISI blog, and the repo README; the 18-vs-20 count discrepancy is flagged as Open Question 1)
- Docker hardening 2026 (userns-remap vs rootless): https://thelinuxcode.com/docker-security-best-practices-2026-hardening-the-host-images-and-runtime-without-slowing-teams-down/
- moby #48521 (rootless ⊥ userns-remap): https://github.com/moby/moby/issues/48521
- Modal Sandboxes (gVisor in production): https://modal.com/docs/guide/sandboxes

### Tertiary (LOW confidence — needs validation)
- GitHub repo README scenario enumeration (Docker 11 / K8s 5 / Kernel 4 = 20) vs paper's 18 — reconcile against the paper PDF at plan time (Open Question 1).

## Metadata

**Confidence breakdown:**
- Standard stack (gVisor/runc/seccomp/userns-remap/HTTP runner): HIGH — verified against Docker + gVisor official docs and grounded in existing codebase patterns.
- Architecture / integration surface: HIGH — read directly from `internal/sandbox`, `internal/agent/tools`, `internal/config`, `cmd/aura`.
- Seccomp allowlist source: MEDIUM — no single canonical "Python 3.12 + bash" profile exists; recommendation is harden-from-moby-default (A1).
- SandboxEscapeBench port: MEDIUM — taxonomy confirmed but 18-vs-20 count discrepancy unresolved (Open Question 1); arXiv WebFetch blocked.
- Pitfalls: HIGH — each cross-verified against official docs or the existing code.

**Research date:** 2026-06-01
**Valid until:** 2026-07-01 (gVisor release channel + moby default.json move; re-fetch at plan time — fast-moving security domain, 30-day horizon)

## RESEARCH COMPLETE

**Phase:** 5 - Sandbox 2a Stateless
**Confidence:** HIGH (isolation primitives + integration surface) / MEDIUM (seccomp allowlist source + bench scenario count)

### Key Findings
- **gVisor arm64 is confirmed preliminary/non-GA (4KB-page only)** → D-05/D-06/D-07's x86-primary + arm64-`runc`-floor split is correct; `AURA_SANDBOX_RUNTIME` must resolve per-arch.
- **Adopt the moby default seccomp profile as the allowlist baseline and SUBTRACT the dangerous set** — do not hand-author 80 syscalls (it is already a positive allowlist, multi-arch by-name, battle-tested). The 18-scenario bench backstops over-permissiveness.
- **Two CI-setup traps:** (1) `--privileged` DinD disables seccomp — the profile must apply to the inner sidecar, not be relaxed; (2) userns-remap goes on the *inner* DinD daemon, with the outer DinD running `--userns=host` — budget a CI checkpoint to prove userns-remap is live.
- **SandboxEscapeBench count discrepancy** (paper 18 vs repo ~20 incl. inapplicable K8s scenarios) → the port must emit an auditable per-scenario table with explicit N/A lines; reconcile against the paper PDF at plan time (Open Question 1).
- **Auto-start (D-09) must be best-effort + docker-CLI-gated** — never mount the docker socket into Aura (that is escape vector #1 the bench tests). When containerized Aura lacks a docker CLI, go straight to `ErrSandboxUnreachable` (Open Question 3).
- The Go integration is small and well-grounded: `DockerRunner` copies the proven `openai_compat` HTTP-client shape; `execute` registers like existing deferred tools; the lean preview routes through the already-shipped `tools.NewResult` (zero new spillover code).

### Files Created
`.planning/phases/05-sandbox-2a-stateless/05-RESEARCH.md`

### PLANNING PREREQUISITES (hard gates — PRD-first principle)
Two PRD-amendment commits MUST land BEFORE any Phase-5 code commit:
1. **D-05/06/07** — re-decide D12 to gVisor-primary on x86: amend `prd.md §Slice 2 D12` + `.planning/DECISIONS.md D12` + `.planning/ROADMAP.md` Phase 5 SC #5 + the escape-bench profile note.
2. **D-09** — auto-start lifecycle: amend PRD acceptance #4 "sidecar down → clear error" → "sidecar down AND auto-start fails → clear error"; note the one-shot auto-start helper in `docker.go`'s scope.

### Confidence Assessment
| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | Docker + gVisor official docs + codebase patterns |
| Architecture | HIGH | Read directly from existing integration surface |
| Seccomp allowlist source | MEDIUM | No canonical py3.12+bash profile; harden-from-moby-default (A1) |
| Bench scenario port | MEDIUM | 18-vs-20 count discrepancy unresolved (OQ1) |
| Pitfalls | HIGH | Cross-verified against official docs |

### Open Questions
1. SandboxEscapeBench 18 (paper) vs ~20 (repo, incl. K8s) reconciliation — resolve against paper PDF at plan time.
2. OOM/pids `limit_hit` detection mechanism in the stdlib sidecar under cgroup v2 (small spike).
3. Auto-start `docker compose up` when Aura is itself containerized without a docker CLI (best-effort + gate, never mount the socket).

### Ready for Planning
Research complete. After the two PRD-amendment commits land, the planner can create PLAN.md files.
