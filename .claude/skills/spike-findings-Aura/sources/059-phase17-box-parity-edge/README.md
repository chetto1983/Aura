---
spike: 059
name: phase17-box-parity-edge
type: standard
validates: "Given a fat full-power image on plain runc, when Aura runs shell+python3+node+subprocess on a writable rootfs as root AND probes the host edge, then parity holds (which the audit's distroless/read_only/non-root image breaks) AND the host fs outside declared mounts is invisible + no docker socket + cpus/mem/pids limits hold"
verdict: VALIDATED
related: [010, 060, 061]
tags: [phase-17, packaging, container, boundary, shell-exec, parity, audit-vs-spec]
---

# Spike 059: phase17-box-parity-edge

## What This Validates

The Phase-17 hinge. The audit commit `ec7fe2f6` (2026-06-12, *two days after* the SPEC
locked "the container is Aura's computer, not a jail" on 2026-06-10) hardened the `aura`
container into a jail: a **distroless** root `Dockerfile` + `cap_drop: ALL` + `read_only: true`
+ forced non-root `65532` on the compose service. The SPEC explicitly **declines** all of
that. This spike asks, with on-host evidence: does the fat full-power box deliver the
parity (`shell_exec`, self-extension, subprocess, root) that the audit's image breaks — and
does the SPEC's "free host edge" actually hold on a plain container?

## How to Run

```powershell
# PROBE A — distroless has no shell:
docker run --rm gcr.io/distroless/static-debian12:nonroot sh -c 'echo PARITY-OK'

# Build the fat box + run parity/edge probes (stdin-piped to dodge PS 5.1 arg-quoting):
docker build -f Dockerfile.fat -t aura-spike-fat:059 .
((Get-Content probe_parity_edge.sh -Raw) -replace "`r","") | docker run -i --rm aura-spike-fat:059 sh -s
((Get-Content probe_limits.sh -Raw) -replace "`r","") | docker run -i --rm --memory=256m --cpus=0.5 --pids-limit=64 aura-spike-fat:059 sh -s
```

## What to Expect

Distroless → `exec: "sh": executable file not found` (exit 127). Fat box → root shell,
python3+node resolve, writable rootfs, subprocess spawns, no docker socket, host fs invisible,
limits enforced.

## Investigation Trail

1. Confirmed firsthand the audit/SPEC conflict (git: `ec7fe2f6` added the lockdown 2026-06-12;
   SPEC committed 2026-06-10). Host probe: runtimes = `runc`/`nvidia` only (no `runsc`), Docker
   Desktop on Windows, no `/dev/kvm` in-shell — re-confirms spike 010's "Docker Desktop can't
   host gVisor".
2. PROBE A: `gcr.io/distroless/static-debian12:nonroot` (the current root Dockerfile's final
   stage) → `exec: "sh": not found`, exit 127. **Distroless structurally cannot run `shell_exec`**
   — which is Aura's primary execution surface (Claude-Code parity, `c9e1124e`).
3. Built a minimal fat image (debian:bookworm-slim + python3 + nodejs, no USER directive).
4. Parity + edge probes via stdin (PS 5.1 mangles inline `$(...)`/nested quotes; a leftover
   UTF-8 BOM ate each script's first label line only — every substantive line ran).

## Results

**VALIDATED — the fat full-power box delivers full parity the distroless jail cannot; the SPEC "free edge" holds on plain runc.**

| Probe | Distroless (audit `ec7fe2f6`) | Fat full-power box (SPEC model) |
|---|---|---|
| `shell_exec` (run `sh`) | **`exec "sh": not found`, exit 127** | `shell=OK` |
| run as root | non-root `65532` (forced) | `uid=0` |
| python3 / node resolve | absent (no interpreter) | `Python 3.11.2` / `node v18.20.4` |
| writable rootfs (self-extension) | `read_only: true` (blocked) | wrote+read `/opt/selfextend=hello` |
| spawn subprocess | n/a | subprocess pid spawned |
| no host docker socket | — | no `/var/run/docker.sock`; `NO-DOCKER-BINARY` |
| host fs outside mounts invisible | — | rootfs=`Debian GNU/Linux 12` (not the Windows host); `/host` absent |
| cpus/mem/pids limits in effect | — | `mem.max=268435456` (=256m), `pids.max=64`, `cpu.max=50000 100000` (=0.5 cpu) |

Image size: fat **308 MB** vs distroless **6.12 MB** (the parity tax — acceptable; the real
fat runtime size is measured in spike 060).

**Conclusion for the hinge:** the audit's `ec7fe2f6` distroless+cap_drop+read_only+non-root box
is **INVALIDATED as the Phase-17 box** — it cannot run `shell_exec`, cannot self-extend (read-only),
cannot spawn `mcp-neo4j-cypher` (no python), runs non-root. The SPEC's "full power inside, host
only outside" model is **VALIDATED**: a plain fat container already gives the free host edge (no
socket, host fs invisible, resource limits) *without* any capability stripping. **`ec7fe2f6`'s
lockdown of the `aura` service + the distroless root `Dockerfile` must be reverted in Phase 17.**

Note (cosmetic): `nproc` inside the limited container still reports 8 (host cores) — cgroup-v2
CPU is enforced by the `cpu.max` quota (`50000 100000` = 0.5 cpu), not by hiding cores. The
quota is the real control; `nproc` is advisory.
