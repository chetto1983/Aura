---
spike: 007
name: uv-on-demand-deps
type: standard
validates: "Given pip+uv baked as TOOLING (no curated dep set), when a skill needs a Python dep at run time, then uv creates a venv and installs it fast enough to be viable on-demand — removing the build-time bake obligation"
verdict: VALIDATED
related: [006, 005]
tags: [skills, sandbox, uv, pip, deps, phase-11]
---

# Spike 007: uv-on-demand-deps

## What This Validates

User's proposal after spike 006: **install pip+uv in the sandbox and resolve skill deps on demand** instead of (or alongside) baking a curated set at image-build time. This spike OVERTURNS spike 006's central premise.

## The spike-006 premise was wrong

Spike 006 concluded "the runtime bridge is egressless → runtime pip is impossible → 7e MUST bake deps." **That assumption was never probed — it was inherited from the Phase-8 sandbox design.** Ground truth (probed 2026-06-05):

- `compose.yaml` network `aura-sandbox-local` sets `com.docker.network.bridge.enable_ip_masquerade: "false"`.
- BUT on Docker Desktop the vpnkit/gateway NATs the container regardless. Live from inside the container: `curl -sI https://example.com` → `HTTP/2 200`; `github.com` → 200; **`pip3 download validators` from pypi.org → 686ms success.**
- So the production `aura-sandbox-agent:py3` image **already has full internet egress + pip 23.0.1**. Runtime installs work TODAY with zero image change.

(On native-Linux prod with a real non-masquerading bridge this would NOT hold — egress there needs the Phase-8 host forward-proxy, which did NOT survive the sandbox-agent pivot — `grep proxy compose.yaml` = nothing. See "Open obligation" below.)

## How to Run

```bash
docker build -t aura-sandbox-agent:spike007 .planning/spikes/007-uv-on-demand-deps
docker compose -f compose.yaml -f .planning/spikes/007-uv-on-demand-deps/compose.spike007.yaml up -d aura-sandbox-agent
# then drive /v1/processes/run with uv venv + uv pip install (see Results)
docker compose -f compose.yaml up -d aura-sandbox-agent   # restore
```

## Results

**VALIDATED — on-demand deps are fast enough to be the primary strategy.** uv 0.7.13 (COPYed from `ghcr.io/astral-sh/uv` — static binary, no `curl|sh` installer):

| Operation | Time |
|---|---|
| `uv venv` (per-skill isolated env) | 45–53 ms |
| `uv pip install openpyxl` (cold, 2 pkgs) | 292 ms |
| openpyxl `.xlsx` produce+readback via venv python | 226 ms |
| `uv pip install pandas` (cold, 4 pkgs incl. numpy 16 MiB) | 3.12 s |

A real skill dep set installs in **sub-second** (openpyxl) to **~3s** (pandas+numpy from scratch). uv's global cache makes the 2nd skill needing the same dep ~instant.

## Strategy verdict for Phase 11 (planner decision, not locked here)

Three viable models — the spike makes all three real; the planner/discuss picks:

1. **Build-time bake only** (spike 006): hash-pinned curated set in the Dockerfile. Pro: hermetic, offline-resilient, no per-skill latency, deterministic. Con: every new skill dep = image rebuild + `make sandbox-up`; image grows.
2. **On-demand uv only** (this spike): bake uv as tooling, skills declare `deps:` (the frontmatter field — now LOAD-BEARING, not docs-only), executor runs `uv pip install` into a per-skill venv before first run. Pro: any skill self-provisions, tiny base image, matches how xlsx/find-skills ship. Con: needs egress (prod proxy obligation), first-run latency, supply-chain surface at RUN time (mitigate: pin versions + hash, allowlist index).
3. **Hybrid (recommended lean):** bake the common heavy set (the Phase-5 list: numpy/pandas/openpyxl/...) for offline-fast defaults; uv-install the long tail on demand into venvs. Best of both; `deps:` frontmatter drives the on-demand leg.

**This reframes spike-006's "PLAN-CHANGING" obligation:** 7e does NOT *have* to bake — baking becomes a perf/offline choice, and `deps:` frontmatter graduates from documentation (CONTEXT D-20) to a real executor input under models 2/3.

## Open obligation (prod parity)

Docker Desktop's accidental NAT masks the real posture. On native-Linux prod the non-masquerading bridge means on-demand uv needs egress to a PyPI index — either re-enable masquerade for the sandbox net (loosens isolation) or restore the Phase-8 host forward-proxy with a `pypi.org` allowlist (the disciplined option; it was lost in the sandbox-agent pivot). The planner MUST decide the egress posture explicitly; do not ship relying on Docker Desktop's behavior. Security note: on-demand install = arbitrary-package execution surface → pin+hash, allowlist the index, and it rides the existing per-snippet RISKY gate when `needs_network`.
