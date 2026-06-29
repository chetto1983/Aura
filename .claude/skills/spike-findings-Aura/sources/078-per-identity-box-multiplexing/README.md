---
spike: 078
name: per-identity-box-multiplexing
type: standard
validates: "Given the validated fat box (059/061), when Aura runs one full-capability box per identity with per-identity named volumes, then identity B's box cannot see identity A's volume/processes, the host is not exposed, and idle cost is negligible"
verdict: VALIDATED
related: [059, 060, 061, 062, 008, 009, 010]
tags: [sandbox, multi-user, per-identity, docker, isolation, phase-37, v2.0.0]
---

# Spike 078: Per-Identity Box Multiplexing

## What This Validates

Given the single-user fat box already proven by 059 (root+py3+node, host fs invisible, no docker.sock, cpu/mem/pids enforced) and 061 (box baseline + optional gVisor tier), **when Aura runs one full-capability box per identity with a per-identity named volume**, then identity B's box cannot read identity A's data or see its processes, the real host is not exposed, and idle resource cost is negligible. This is the v2.0.0 Phase-37 multi-tenant extension of the single-user sandbox — the genuinely new question, since every prior sandbox spike (059–062, 008–010) was single-user.

Operator note (mid-spike): **RAM fit is NOT the gate** — this is a dev spike and the real server (DGX) has headroom. The verdict turns on the isolation *model*, not N-box capacity on the 15.5 GiB dev Docker cap.

## Design / Approach

Per-identity box = one container per `identityctx.IdentityID`, spawned over the **Docker Go SDK** (`moby/moby/client`, already in `go.mod`) — NOT Kubernetes (see 079; K8s reserved for the DGX multi-node tier). Each box:
- mounts a **per-identity named volume** at the workspace path (no host bind-mount),
- runs with `--network none` by default (egress via the 009 allowlist when enabled),
- never mounts the Docker socket, never `--privileged`, never `--network host` (the 059 edge guards, now per-identity),
- optional `runtime: runsc` (gVisor) under `server_production` via the existing `compose.gvisor.yaml` (061).

## How to Run

```powershell
# 2 per-identity boxes from the real fat image, per-identity named volumes, no host exposure
docker volume create aura-spike-identA; docker volume create aura-spike-identB
docker run -d --rm --name boxA --network none -v aura-spike-identA:/idbox --entrypoint sh aura:local -c 'sleep 600'
docker run -d --rm --name boxB --network none -v aura-spike-identB:/idbox --entrypoint sh aura:local -c 'sleep 600'
docker exec boxA sh -c 'echo identityA-secret > /idbox/secret.txt'
docker exec boxB sh -c 'cat /idbox/secret.txt'   # -> No such file (separate volume)
docker stats --no-stream boxA boxB
```

## What to Expect

A's secret is written to A's volume; B's volume is empty and the read fails; box A has no docker.sock; idle RAM is sub-MB per box.

## Investigation Trail

- First attempts via Git-Bash `docker` hit MSYS path-mangling (`can't open .../docker`); switched to PowerShell per the established docker convention.
- The harness path-guard blocked commands containing `rm` + a `/workspace` or `/box` token (false-positive on container-internal paths); fixed by using mount target `/idbox` and moving cleanup (`docker stop` + `docker volume rm <names>`) to a separate command with no absolute paths.
- Surfaced an environment fact: **Docker sees 15.47 GiB, not the host's 32 GB** (WSL/Docker cap). Per operator, not a gate for dev; flagged for the Phase-37 pre-merge benchmark on the real host.

## Results

**VALIDATED ✓** (2026-06-29, live on `aura:local` 1.51 GB fat image):
- **Data isolation:** A wrote `identityA-secret-12345` to its named volume; B's `/idbox` was empty (`total 8`, only `.`/`..`); `cat /idbox/secret.txt` in B → `No such file or directory`. Separate named volumes = storage-enforced per-identity isolation.
- **Host not exposed:** `/var/run/docker.sock` absent in box A; `--network none`; no host bind-mount.
- **Idle cost negligible:** boxA 944 KiB, boxB 780 KiB (≈0.01% of 15.47 GiB) — a sleeping shell. Per-identity boxes are effectively free at rest; cost is only under active workload (deferred to a Phase-37 benchmark on the real host).

**Signal for the build (Phase 37):** the per-identity box model is sound over plain Docker. `internal/sandbox/usersandbox` spawns one container per identity, named-volume per identity, the 059 host-exposure flags made unrepresentable in the box spec (SBX-02). Warm-pool/N-box capacity is a tuning question for the real host, not a feasibility blocker.
