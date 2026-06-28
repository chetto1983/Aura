# packaging box

Phase-17 packaging. The box = a FAT, full-power container (writable rootfs, container
root, `shell_exec` + skills self-extension + `mcp-neo4j-cypher` parity), NOT a distroless
jail. Optional strong-isolation appliance tier = gVisor `runsc` via `compose.gvisor.yaml`,
native-Linux/arm64 only. Docker Sandboxes `sbx` is NOT the appliance runtime.

## Requirements

(Binding, from MANIFEST.md Session-15 — non-negotiable for `/gsd-plan-phase 17`.)

- **The box = fat full-power container, NOT a jail.** Default everywhere (dev + appliance,
  x86_64 + arm64): writable rootfs, **container root** (no `USER` directive), `shell_exec` +
  skills self-extension + `mcp-neo4j-cypher` parity. **NO** `cap_drop`, **NO** `read_only`,
  **NO** distroless, **NO** forced-non-root. This *is* the SPEC's "Aura's computer, not a jail."
- **REVERT audit commit `ec7fe2f6`** (2026-06-12) on the `aura` service: drop the distroless
  root `Dockerfile`, and remove `cap_drop: ALL` + `read_only: true` + `user 65532` from the
  `aura` compose service. It was added two days *after* the SPEC locked "not a jail" (2026-06-10)
  and structurally breaks the primary execution surface (spike 059). The SPEC pre-authorizes the
  revert ("accepted residual, operator's informed choice 2026-06-10").
- **The portable host edge is free on plain runc** — keep it, don't re-harden the inside to get
  it: no host docker socket mounted, host fs outside declared mounts invisible, and `cpus`/`mem`/
  `pids` resource limits enforced (all proven on plain runc, spike 059). The host socket is
  **never** mounted.
- **Fat image must bundle every runtime (SPEC Req 5):** `python3` + `node`/`npx` + `uv`/`uvx`
  + pinned **`mcp-neo4j-cypher==0.6.0`** + `git` + `curl`, so the host needs only Docker.
  Acceptance: all four of `python3`, `node`, `uvx`, `mcp-neo4j-cypher` resolve via `command -v`.
- **Heavy runtime layer must be cache-stable:** order the apt/uv/pip runtime layers BEFORE the
  final `COPY --from=build /out/aura` so a code change re-runs only the tiny final COPY, never the
  ~73s heavy layer.
- **Optional strong-isolation appliance tier = gVisor `runsc` via `compose.gvisor.yaml`**
  (`runtime: runsc` on the `aura` service), native-Linux/arm64 only, **OFF** on Docker-Desktop dev
  (can't host runsc — spikes 010/059). Frame it as a transparent isolation BOUNDARY, NOT
  capability-stripping (the workload keeps full caps + full Linux; only the host gets thicker
  walls). **Requires a PRD/SPEC amendment** (gVisor is not in the locked SPEC).

## How to Build It

**The box model (spike 061 decision):**
1. **Baseline box — default dev *and* appliance, x86_64 *and* arm64:** the fat, writable,
   full-power container. Root-of-its-box, full parity, no internal lockdown. The host edge
   (no socket, host fs invisible, `cpus`/`mem`/`pids` limits) comes free from plain runc.
2. **Optional appliance tier — native-Linux incl. arm64:** a `compose.gvisor.yaml` override
   adding `runtime: runsc` to the `aura` service. gVisor is the only strong-isolation option
   that is simultaneously a Docker-runtime drop-in (Docker-only host ✓), transparent to the
   workload (full parity inside — spike 010), arm64-capable, and **KVM-free**. Not applied on
   Docker-Desktop dev (the dev box runs the plain baseline).

**Fat runtime Dockerfile base (spike 060, proven — `Dockerfile.runtime`):**
```dockerfile
FROM debian:bookworm-slim
# --- heavy runtime layer (cache-stable: nothing here changes when aura's code changes) ---
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      python3 python3-pip nodejs npm git curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
# uv/uvx as static binaries (sandbox-runtime finding: COPY from astral image, not pip)
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/
# pinned MCP server; Debian python is PEP-668 externally-managed -> break-system-packages
RUN pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0
# --- in the REAL image the LAST layer is: COPY --from=build /out/aura /usr/local/bin/aura ---
```
- Base = `debian:bookworm-slim` (same family as the markitdown sidecar `python:3.12-slim`).
- The real `docker/aura/Dockerfile` = this runtime base + a `golang:1.26.4` build stage's
  `COPY --from=build /out/aura` as the LAST layer + pre-baked recipes (uvx calculator, npx
  mail) as their own cache-warm layers ordered BEFORE the aura COPY.
- **NO `USER` directive** → runs as root inside its own box (Claude-Code parity).
- Node is apt v18 (`v18.20.4`, fine for `npx`); pin a newer node via NodeSource only if a
  recipe needs it.

**SPEC Req 5 acceptance probe (spike 060, all four resolved):**
```sh
docker run --rm <image> sh -c 'command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher'
```
Proven resolutions: `python3` `/usr/bin/python3` 3.11.2 · `node` `/usr/bin/node` v18.20.4 ·
`uvx` `/usr/local/bin/uvx` (uv 0.11.21) · `mcp-neo4j-cypher` `/usr/local/bin/mcp-neo4j-cypher`
**0.6.0 exact pin**.

**Parity + host-edge probe (spike 059, `probe_parity_edge.sh`):** proves `shell=OK`, `uid=0`,
python3+node resolve, writable rootfs (`echo hello > /opt/selfextend` round-trips), subprocess
spawns, no `/var/run/docker.sock`, `NO-DOCKER-BINARY`, rootfs reports Debian (not the Windows
host), `/host` absent.

**Resource-limit enforcement (spike 059, `probe_limits.sh`):** run with
`--memory=256m --cpus=0.5 --pids-limit=64`; cgroup-v2 reflects it: `memory.max=268435456`,
`pids.max=64`, `cpu.max=50000 100000` (=0.5 cpu). NOTE: `nproc` inside still reports host cores
(8) — cgroup CPU is enforced by the `cpu.max` **quota**, not by hiding cores; the quota is the
real control, `nproc` is advisory.

**Build commands (spikes 059/060, PowerShell 5.1 — stdin-pipe scripts to dodge PS arg-quoting):**
```powershell
docker build -f Dockerfile.runtime -t aura-spike-runtime:060 .
((Get-Content probe_resolve.sh -Raw) -replace "`r","") | docker run -i --rm aura-spike-runtime:060 sh -s
docker build -f Dockerfile.runtime -t aura-spike-runtime:060 .   # second build = cache hit
```

**gVisor appliance tier (spike 061):** a separate `compose.gvisor.yaml` override file that only
adds `runtime: runsc` to the `aura` service. Applied via `docker compose -f compose.yaml -f
compose.gvisor.yaml up` on native-Linux/arm64 hosts only. The baseline `compose.yaml` stays the
default. gVisor builds for arm64 and needs no KVM (unlike Kata/Firecracker).

**PRD/SPEC amendment needed before implementation (spike 061):** (a) ADD the optional gVisor
appliance-isolation tier, framed as a transparent isolation boundary (not capability-stripping);
(b) NOTE the revert of `ec7fe2f6` and correct the SPEC Background (it still wrongly claims "no
aura Dockerfile / no aura compose service"); (c) RECORD Docker Sandboxes `sbx` as
evaluated-and-deferred plus the docker-in-box-without-host-socket future capability.

## What to Avoid

- **The audit's distroless jail (`ec7fe2f6`) — INVALIDATED as the Phase-17 box.** A
  `gcr.io/distroless/static-debian12:nonroot` final stage has **no shell**:
  `docker run --rm gcr.io/distroless/static-debian12:nonroot sh -c 'echo X'` →
  `exec: "sh": executable file not found`, **exit 127**. Distroless structurally cannot run
  `shell_exec` (Aura's primary execution surface). Plus `read_only: true` blocks self-extension,
  no python interpreter means `mcp-neo4j-cypher` can't spawn, and `user 65532` forces non-root.
  Each of `cap_drop: ALL` / `read_only: true` / forced-non-root / distroless individually breaks
  parity — REVERT all of them on the `aura` service.
- **Do NOT re-harden the inside to get host isolation.** The host edge (no socket, host fs
  invisible, resource limits) is already free on plain runc *without* any capability stripping.
  Stripping caps to "feel safe" only breaks parity and buys nothing the namespaces don't already
  give.
- **Docker Sandboxes `sbx` is NOT the appliance runtime (spike 062 PARTIAL — two hard blockers):**
  (1) **arm64 gap** — `sbx` arm64 is macOS-only; Windows + Linux are x86_64-only, so the
  commercial **DGX Spark appliance (arm64) is unsupported**. (2) **Shape mismatch** — `sbx` wraps
  a one-shot coding-agent CLI (`sbx run claude`); hosting Aura's persistent multi-service compose
  stack (aura+pg+neo4j+embed+memory+searxng+multimodal) inside one microVM's nested docker engine
  is off-label, perf-"crippling," and `sbx login`-coupled (third-party Docker-account SaaS auth).
  Keep `sbx` only as an optional x86_64-Linux dev/power-user wrapper + the direction-signal.
- **Don't try to run gVisor on Docker Desktop / Windows.** Host runtimes there are `runc`/`nvidia`
  only (no `runsc`), no `/dev/kvm` in-shell — re-confirmed spike 010's "Docker Desktop can't host
  gVisor." Kata/Firecracker microVMs need a runtime + KVM and per-VM RAM — heavier than gVisor for
  one box. agent-sandbox / OpenSandbox are k8s — OUT of scope (SPEC: no k8s).
- **PS 5.1 gotchas when building/probing:** PS 5.1 mangles inline `$(...)`/nested quotes — pipe
  scripts to `sh -s` via stdin and strip CR (`-replace "`r",""`). A leftover UTF-8 BOM eats a
  script's first label line only (every substantive line still runs).

## Constraints

- **Version pins:** base `debian:bookworm-slim`; Go build stage `golang:1.26.4`; uv/uvx COPYed
  from `ghcr.io/astral-sh/uv:latest` (measured `uv 0.11.21`); **`mcp-neo4j-cypher==0.6.0`**
  (exact pin, SPEC constraint + `internal/knowledge/client.go` hint). apt python3 = 3.11.2,
  apt nodejs = v18.20.4.
- **Image sizes:** fat runtime base (no Go binary) **875 MB** single-arch; minimal fat probe
  image **308 MB**; distroless **6.12 MB** (the "parity tax" — accepted; it's what `shell_exec`
  + self-extension + MCP cost).
- **Build times:** cold base build **73s** (apt + uv COPY + pip mcp-neo4j-cypher); warm rebuild
  **1.4s with 3 CACHED layers**. The SPEC's ~45-60min cold figure is the *multi-arch*
  (buildx amd64+arm64, ~2×) + recipe-bake cost, NOT the base.
- **PEP-668:** Debian python is externally-managed → `pip install --break-system-packages`
  (the container IS the boundary; no venv needed at image level).
- **SPEC hard constraints (17-SPEC.md, non-negotiable):** single-host · Docker-only host ·
  k8s/Helm OUT of scope · mini-PC 16-core/32GB (tight CPU budget) · DGX appliance is **arm64** ·
  host Docker socket **never** mounted · full `shell_exec` + skills self-extension +
  `mcp-neo4j-cypher` parity.
- **gVisor:** Docker-runtime drop-in (`runtime: runsc`), userspace kernel / syscall intercept,
  KVM-free, builds for arm64, transparent to the workload — the only strong-isolation option that
  is all of these single-host. Cannot run on Docker Desktop.
- **Docker Sandboxes `sbx` facts (spike 062):** Firecracker microVM, own kernel per sandbox
  (~125ms boot, <5 MiB), docker engine inside the VM (no host socket), host-side network proxy.
  Host OS support: macOS (Apple silicon), Windows 11 (x86_64), Ubuntu 24.04+ (x86_64). arm64 =
  macOS-only. Its **docker-in-box-without-host-socket** idea is a future capability that could
  safely lift SPEC Req 3 (docker-runtime MCP inside the box) without ever mounting the host
  socket — NOT for now.
- **Commit to revert:** `ec7fe2f6` (2026-06-12). SPEC locked "not a jail" `2026-06-10`.

## Origin

Synthesized from spikes: 059, 060, 061, 062. Source files in:
`sources/059-phase17-box-parity-edge/` (README + `Dockerfile.fat` + `probe_parity_edge.sh` +
`probe_limits.sh`), `sources/060-phase17-fat-image-base/` (README + `Dockerfile.runtime` +
`probe_resolve.sh`), `sources/061-phase17-isolation-tier/` (README — decision capstone),
`sources/062-docker-sandboxes-sbx-fit/` (README). Verdicts: 059 VALIDATED · 060 VALIDATED ·
061 VALIDATED · 062 PARTIAL (validated as dev tool + direction-signal; INVALIDATED as appliance
runtime).
