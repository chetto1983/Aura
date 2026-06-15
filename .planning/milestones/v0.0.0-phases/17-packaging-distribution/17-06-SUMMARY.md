---
phase: 17-packaging-distribution
plan: 06
subsystem: compose-topology
tags: [packaging, compose, caddy, whatsapp, gvisor, ops]
requires: [17-02, 17-03]
provides:
  - Ordered compose boot through aura-migrate and service_completed_successfully
  - Durable aura-home config volume at AURA_CONFIG_DIR
  - Caddy TLS internal CA front door with shared-token gate
  - Optional WhatsApp MCP sibling on loopback port 8092
  - Native-Linux gVisor runtime override
affects: [compose, docker, cmd-aura]
tech-stack:
  added:
    - caddy:2
    - whatsapp-mcp vendored source archive
  patterns:
    - Compose one-shot migration gate
    - Loopback sidecar publishing with single Caddy LAN front
    - Source archive vendoring for third-party build contexts
key-files:
  created:
    - caddy/Caddyfile
    - compose.gvisor.yaml
    - docker/whatsapp/Dockerfile
    - docker/whatsapp/entrypoint.sh
    - docker/whatsapp/PROVENANCE.md
    - docker/whatsapp/whatsapp-mcp-src.tar.gz
    - .planning/phases/17-packaging-distribution/17-06-SUMMARY.md
  modified:
    - compose.yaml
    - .env.example
    - cmd/aura/container_artifacts_test.go
key-decisions:
  - WhatsApp publishes the FastMCP Streamable HTTP front on container port 8080 and runs the Go bridge REST API on loopback 8081, because two processes cannot bind the same port.
  - Caddy uses a local `@authed` header-or-query token matcher with `respond 401`, not `forward_auth`.
  - The host backup bind source is `${AURA_BACKUP_DIR:-./backups}` and the in-container path is consistently `/backups`.
  - The WhatsApp fork is vendored as `whatsapp-mcp-src.tar.gz` so Docker builds are reproducible while repo Go file-size gates do not scan third-party source.
requirements-completed: [OPS-01]
metrics:
  duration: ~25min
  tasks: 4
  files-modified: 12
  completed: 2026-06-14
---

# Phase 17 Plan 06: Compose Topology Summary

The compose appliance topology is now ordered, durable, and fronted through one LAN entrypoint. `aura-migrate` runs `aura db migrate && aura neo4j migrate` before `aura`, `aura-home` owns `/var/lib/aura`, Postgres and Neo4j share the host-visible `/backups` bind, and Caddy is the only non-loopback publish.

## Performance

- Started: 2026-06-14T15:13:00+02:00
- Completed: 2026-06-14T15:32:00+02:00
- Duration: ~25 min active work
- Tasks completed: 4
- Files changed: 12

## Accomplishments

- Added `aura-migrate` and gated `aura` with `service_completed_successfully`; extended `aura` dependencies to Neo4j and the embed sidecar healthchecks.
- Replaced the old separate run/skills/export volumes with `aura-home:/var/lib/aura` and set `AURA_CONFIG_DIR`.
- Added Caddy with `tls internal`, `@authed` header/query token matching, `respond 401`, persisted `caddy-data`, and only `0.0.0.0:${AURA_HTTPS_PORT:-443}:443` published to LAN.
- Added optional WhatsApp MCP sibling on `127.0.0.1:${AURA_WHATSAPP_MCP_PORT:-8092}:8080` with a named session volume and no `aura` boot dependency.
- Added `compose.gvisor.yaml` as the minimal `runtime: runsc` override for native-Linux/arm64 appliances.
- Extended the container artifact test to pin the new compose/Caddy/gVisor/WhatsApp contracts.

## Task Commits

| Task | Commit | Summary |
| --- | --- | --- |
| 1-4 | 72113947 | Wired the compose appliance topology, Caddy front, WhatsApp sibling, gVisor override, env template, and artifact tests. |

## Verification Evidence

- `go test ./cmd/aura -run TestProductionContainerArtifactsMatchFatImageContract -v` passed.
- `docker compose config --quiet` passed with throwaway required env values.
- `docker compose -f compose.yaml -f compose.gvisor.yaml config --quiet` passed with throwaway required env values.
- `docker run --rm -e AURA_ACCESS_TOKEN=test-token -v <Caddyfile>:/etc/caddy/Caddyfile:ro caddy:2 caddy validate --config /etc/caddy/Caddyfile` passed.
- `docker build -f docker/whatsapp/Dockerfile --target source docker/whatsapp` passed.
- `docker build -f docker/whatsapp/Dockerfile docker/whatsapp` passed, including CGO bridge build and `uv sync --frozen --no-dev`.
- `go test ./cmd/aura -run "TestProductionContainerArtifactsMatchFatImageContract|TestDoctor" -v` passed.
- `go test ./cmd/aura -v` passed.
- `go test ./internal/mcp/manager -run TestWhatsApp -v` passed with no matching tests.
- `go vet ./cmd/aura` passed.
- `go build ./...` passed.
- `bash scripts/check-file-size.sh` passed.
- Acceptance greps confirmed `aura-migrate`, `service_completed_successfully`, `aura-home`, WhatsApp port 8092, Caddy 443, backup mounts, `tls internal`, `@authed`, `respond 401`, and no `forward_auth`/gVisor `cap_drop`/`read_only`.
- Pre-commit `gofmt`, `vet`, and file-size hooks passed.

## Deviations

- The plan phrased both the Go bridge and FastMCP front as `:8080`; the implementation keeps the public MCP front on `:8080` and moves the bridge REST API to loopback `:8081`, wired through `WHATSAPP_API_BASE_URL`.
- The WhatsApp fork is vendored as a tarball instead of unpacked source directories. This keeps the build reproducible while avoiding repository file-size gates on third-party Go source.
- Caddy proxies only known AG-UI paths and `/setup/*`; authenticated unknown paths return 404 instead of defaulting to either upstream.

## Issues Encountered

- PowerShell `Compress-Archive` produced zip entries with Windows-style separators, so the vendored source was switched to `tar.gz` and verified with Docker.

## User Setup Required

- `AURA_ACCESS_TOKEN` must be generated by the installer or set in `.env` before compose boot.
- LAN clients using `tls internal` must trust Caddy's internal CA root from the persisted `caddy-data` volume if they want a clean browser trust state.
- The gVisor override requires host provisioning (`runsc install`, Docker daemon runtime registration, Docker reload) and is not for Docker Desktop dev.

## Next Phase Readiness

Plan `17-07` can generate `.env`, install compose/systemd assets, and document the Caddy trust and gVisor host-provisioning path against the real topology.

## Self-Check: PASSED
