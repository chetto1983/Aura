---
phase: 17-packaging-distribution
plan: 02
subsystem: packaging
tags: [docker, compose, image, gvisor-ready, mcp]
requires: [17-01]
provides:
  - Fat docker/aura/Dockerfile runtime image
  - Root distroless audit-jail Dockerfile removed
  - De-hardened compose aura service with stability limits
affects: [docker-aura-image, compose-aura-service, mcp-runtime, backup-runtime]
tech-stack:
  added:
    - debian:bookworm-slim runtime image
    - ghcr.io/astral-sh/uv:0.11.21
    - postgresql-client-17 from PGDG
    - mcp-neo4j-cypher==0.6.0
  patterns:
    - Cache-stable multi-stage Dockerfile
    - Full-power writable Aura box with no host Docker socket
key-files:
  created:
    - docker/aura/Dockerfile
    - .planning/phases/17-packaging-distribution/17-02-SUMMARY.md
  modified:
    - compose.yaml
  deleted:
    - Dockerfile
key-decisions:
  - The Aura image runtime is Debian slim, not distroless, so the agent has full Linux parity inside the box.
  - The uv copy stage is pinned to ghcr.io/astral-sh/uv:0.11.21 instead of latest for reproducible image history.
  - The compose service keeps cpus/mem/pids stability limits but drops non-root/read-only/cap-drop/no-new-privileges jail directives.
requirements-completed: [OPS-01]
metrics:
  duration: ~10min
  tasks: 3
  files-modified: 3
  completed: 2026-06-14
---

# Phase 17 Plan 02: Fat Aura Image Summary

The baseline Aura box now builds from `docker/aura/Dockerfile`, runs as a writable full-power container, and no longer has a root distroless image path.

## Performance

- Started: 2026-06-14T12:27:00Z
- Completed: 2026-06-14T12:36:57Z
- Duration: ~10 min
- Tasks completed: 3
- Files changed: 3

## Accomplishments

- Created the fat multi-stage Aura image: Go build stage, Debian runtime, Python, Node/npm/npx, uv/uvx, pinned `mcp-neo4j-cypher==0.6.0`, PGDG `postgresql-client-17`, recipe cache warm-up, and `AURA_IN_CONTAINER=1`.
- Removed the root distroless Dockerfile so the audit-jail image is no longer buildable from the repo root.
- Repointed `compose.yaml` to `docker/aura/Dockerfile`, removed `user`, `read_only`, `cap_drop`, and `security_opt`, added `pids_limit`, and kept the no-Docker-socket invariant.

## Task Commits

| Task | Commit | Summary |
| --- | --- | --- |
| 1 | 66058ddc | Added the fat Aura container image. |
| 2 | 382a397e | Removed the root distroless Aura image. |
| 3 | 64926036 | De-hardened and repointed the compose `aura` service. |

## Verification Evidence

- `docker build -f docker/aura/Dockerfile -t aura:p17test .` passed on linux/amd64 Docker Desktop.
- `docker run --rm --entrypoint sh aura:p17test -c "command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher"` resolved all four runtimes.
- `docker run --rm --entrypoint sh aura:p17test -c "command -v pg_dump && pg_dump --version"` reported `pg_dump (PostgreSQL) 17.10`.
- `docker run --rm --entrypoint sh aura:p17test -c 'test "$AURA_IN_CONTAINER" = 1'` passed.
- `docker history --no-trunc aura:p17test` matched no `OPENROUTER_API_KEY`, `POSTGRES_PASSWORD`, or `NEO4J_PASSWORD`.
- `docker compose -f compose.yaml config` passed with placeholder required secrets.
- Structural checks confirmed no `cap_drop`, `read_only: true`, `user: "65532`, or `/var/run/docker.sock` in `compose.yaml`.

## Deviations

- Multi-arch arm64 build remains Manual-Only for the release/buildx leg; the local proof here is linux/amd64.

## Issues Encountered

None.

## User Setup Required

None for this plan. Operators still need Docker for the stack; gVisor setup belongs to the later optional tier.

## Next Phase Readiness

Plan `17-03` can consume the baked `AURA_IN_CONTAINER=1` marker for the in-container docker-runtime guard.

## Self-Check: PASSED
