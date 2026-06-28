---
spike: 060
name: phase17-fat-image-base
type: standard
validates: "Given a multi-stage build (debian-slim + python3 + uv/uvx + node/npx + pinned mcp-neo4j-cypher==0.6.0 + git + curl), when built, then all four binaries resolve (SPEC Req 5 acceptance) + image size + heavy-layer cache cost are measured"
verdict: VALIDATED
related: [059, 061]
tags: [phase-17, packaging, dockerfile, fat-image, mcp-neo4j-cypher, cache-stable]
---

# Spike 060: phase17-fat-image-base

## What This Validates

SPEC Req 5 ("Fat multi-arch image"): a single Aura container bundles every runtime so the
host needs only Docker. Acceptance: `docker run --rm <image> sh -c "command -v python3 &&
command -v node && command -v uvx && command -v mcp-neo4j-cypher"` resolves all four. This
spike builds the runtime base (no Go build) to de-risk the SPEC's most expensive constraint —
the heavy layer must build clean *and* stay cache-stable (the SPEC warns cold rebuild ~45-60min;
layer order must keep it cached on code changes).

## Research

- Base = `debian:bookworm-slim` (same family as the markitdown sidecar `python:3.12-slim`).
- `uv`/`uvx` = static binaries COPYed from `ghcr.io/astral-sh/uv` (sandbox-runtime.md finding —
  not pip-installed).
- `mcp-neo4j-cypher==0.6.0` pinned (SPEC constraint + `internal/knowledge/client.go` hint).
- Debian python is PEP-668 externally-managed → `pip install --break-system-packages` (the
  container IS the boundary; no venv needed at image level).

## How to Run

```powershell
docker build -f Dockerfile.runtime -t aura-spike-runtime:060 .
((Get-Content probe_resolve.sh -Raw) -replace "`r","") | docker run -i --rm aura-spike-runtime:060 sh -s
docker image ls aura-spike-runtime:060
docker build -f Dockerfile.runtime -t aura-spike-runtime:060 .   # second build = cache hit
```

## Investigation Trail

1. Wrote a runtime-only Dockerfile (debian-slim + python3/pip + nodejs/npm + git + curl + uv/uvx
   + pinned mcp-neo4j-cypher==0.6.0). No multi-stage Go build — this validates the base, not aura.
2. Cold build: **73s** (apt heavy layer + uv COPY + pip mcp-neo4j-cypher). `mcp-neo4j-cypher==0.6.0`
   installs clean with `--break-system-packages`.
3. Acceptance probe: all four resolve. Re-built to measure cache behavior.

## Results

**VALIDATED — the fat runtime base resolves SPEC Req 5 and the heavy layer is cache-stable.**

| Binary | Path | Version |
|---|---|---|
| python3 | `/usr/bin/python3` | 3.11.2 |
| node | `/usr/bin/node` | v18.20.4 |
| uvx | `/usr/local/bin/uvx` | uv 0.11.21 |
| **mcp-neo4j-cypher** | `/usr/local/bin/mcp-neo4j-cypher` | **0.6.0** (exact pin) |

- **Image size:** 875 MB (fat runtime, single-arch, no Go binary yet). The "parity tax" over the
  6.12 MB distroless — expected and acceptable; this is what `shell_exec` + self-extension + MCP cost.
- **Cache-stability:** cold 73s → warm **1.4s, 3 CACHED layers**. The apt/uv/pip runtime layers sit
  BEFORE the (omitted) final `COPY aura` — so a code change re-runs only the tiny final COPY, never
  the heavy layer. The SPEC's cache-stability constraint is satisfied by layer ordering.

**Signal for the build:** the real `docker/aura/Dockerfile` = this runtime base + a `golang:1.26.4`
build stage's `COPY --from=build /out/aura` as the LAST layer + pre-baked recipes (uvx calculator,
npx mail) as their own cache-warm layers. The SPEC's ~45-60min cold figure is the *multi-arch*
(buildx amd64+arm64, ~2×) + recipe-bake cost, not the base (73s single-arch here). Node is apt v18
(fine for `npx`); pin a newer node via NodeSource only if a recipe needs it. **No blockers for Req 5.**
