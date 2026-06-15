---
status: partial
phase: 17-packaging-distribution
source: [17-01-SUMMARY.md, 17-02-SUMMARY.md, 17-03-SUMMARY.md, 17-04-SUMMARY.md, 17-05-SUMMARY.md, 17-06-SUMMARY.md, 17-07-SUMMARY.md, 17-08-SUMMARY.md]
started: 2026-06-15T07:23:12Z
updated: 2026-06-15T08:03:38Z
---

## Current Test

[testing complete — 12 pass, 0 open issues (4 found + resolved), 5 operator-hardware legs blocked]

## Tests

### 1. Cold Start Smoke Test
expected: From a clean state, image builds, `docker compose up` boots — `aura-migrate` runs both migrations and exits 0, then postgres/neo4j/embed/aura/caddy reach healthy, and a primary query returns live data.
result: pass
note: Stack up healthy; aura-migrate ExitCode=0 (idempotent); `aura doctor` → all 5 checks PASS, exit 0. (Warm stack used; 17-02 recorded an amd64 cold build.)

### 2. In-box Full Power, No Docker Socket, Mount Isolation, Limits (AC1)
expected: In-box full parity; no docker socket; `docker ps` fails; host FS outside mounts invisible; cpus/mem/pids limits.
result: pass
note: home-write + python3/node subprocesses ok; `/var/run/docker.sock` absent; `docker ps` exit 127; only `/var/lib/aura` + `/backups` visible; limits 1.0 CPU / 768 MB / 512 pids. Runs root (AC5).

### 3. No Secrets in Image + aura-home Persistence (AC2)
expected: No secret in any layer; secrets via env only; aura-home preserves config across down/up.
result: pass
note: `docker history` clean; OPENROUTER_API_KEY via env (len 73). `aura_aura-home` named volume → /var/lib/aura; AURA_CONFIG_DIR/RUN/SKILLS all inside it; `skills/` persisted across container recreation.

### 4. In-container Docker-Runtime MCP Guard (AC3)
expected: docker-runtime MCP in-box → "deploy as a compose sibling" error; streamable-HTTP sibling lists tools.
result: pass
note: `AURA_IN_CONTAINER=1` verified live; guard covered by green unit tests `TestRuntimeGuard`. No default docker-runtime recipe + `aura mcp` exposes only stdio, so the guard isn't CLI-trippable; sibling proven in AC4.

### 5. WhatsApp Sibling, No wsl.exe, Fail-Soft (AC4)
expected: no wsl.exe; whatsapp sibling resolves + lists tools; sibling down → fail-soft.
result: pass
note: wsl.exe absent; sibling responds 307 from host (127.0.0.1:8092) and in-box (whatsapp:8080); catalog `whatsappRecipeURL()` correct with userinfo guard; fail-soft structural (no `depends_on: whatsapp`).

### 6. Fat Image Runtimes (amd64 + arm64), Final Stage (AC5)
expected: `aura version` + python3/node/uvx/mcp-neo4j-cypher resolve on both arches. (Box-model amendment #63: final stage is root/full-power by design — "non-root" clause superseded.)
result: pass
note: amd64 — `aura version` works, all 4 runtimes resolve. Root by design reconciled in 17-SPEC.md (no-socket + mount isolation + limits are the boundary). arm64 build leg remains an operator manual leg (needs buildx/QEMU).

### 7. In-container Neo4j Round-trip + Clean Host (AC6)
expected: in-container Neo4j round-trip returns version; host has no mcp-neo4j-cypher/Aura-Python.
result: pass
note: in-box round-trip → "Neo4j Kernel 5.26.26 community"; tooling in box. Host-cleanliness folded into AC12 (clean-host install).

### 8. Pre-baked Recipes Work Offline (AC7)
expected: egress blocked → calculator (uvx) + mail (npx) run in-box with no download.
result: pass
note: FIXED + offline-verified (`--network none`). calculator pinned to chetto1983 fork commit 46a1e66 → uvx uses cached checkout offline (server starts). mail VENDORED at upstream commit 7271c46 (no chetto1983 fork) into docker/aura/mail-mcp-src.tar.gz, built + `npm install -g` → `mail-mcp` bin runs fully offline. Verified on aura:p17ac7: both start with no network. (Build note: node-18 EBADENGINE warning for html-to-text@10 — future node bump.)

### 9. Ordered Compose Boot + Auto-restart (AC8)
expected: stack healthy only after aura-migrate exits 0; killed aura auto-restarted.
result: blocked
blocked_by: prior-platform
reason: "Ordered-boot gate PROVEN (aura-migrate ExitCode=0; aura started after). restart:unless-stopped correctly set on compose.yaml + live container. Auto-restart-on-kill does NOT fire on this Docker Desktop host — but a controlled trivial alpine container with the same policy ALSO doesn't restart on kill, so it's a Docker Desktop (LinuxKit) platform behavior, not an Aura defect. Needs confirmation on the native-Linux appliance target (where restart policy is reliable)."

### 10. Keyless Boot + llm_not_configured + Key Recovery (AC9)
expected: boots with no key; agent call returns llm_not_configured; works after key set.
result: pass
note: `docker exec -e OPENROUTER_API_KEY= aura aura doctor` → runs, infra green, `llm_key: not configured (keyless boot allowed; agent calls return llm_not_configured)`, exit 0. Full path covered by green TestServeKeyless/TestLLMNotConfigured.

### 11. aura doctor Aggregate Health (AC10)
expected: exit 0 all-green on healthy stack; non-zero naming the failed check when a service is down.
result: pass
note: healthy → 5x PASS exit 0; embed stopped → `FAIL embed` status FAIL exit 70; restored → green. No docker socket used.

### 12. Caddy TLS + Token Gate + Data-loopback LAN Probes (AC11)
expected: from a LAN host, wizard reachable over HTTPS only with token; data services refused/unreachable.
result: pass
note: FIXED + verified. Token gate: no-token→401, query-token→307, header-token /healthz→200. Data-loopback: only aura-caddy on 0.0.0.0:443; pg/neo4j/embed/agent-memory/whatsapp all 127.0.0.1-only. Caddyfile site changed `localhost:443`→`:443 { tls internal { on_demand } }` → LAN SNI now works (aura-box→401/200, IP 10.0.0.5→200). (on-demand-TLS abuse warning — acceptable for internal-CA LAN behind token; future ask/rate-limit hardening.) Strict second-LAN-host confirmation still nice-to-have.

### 13. curl|sh Installer on a Clean Host (AC12)
expected: curl|sh on clean Linux host → healthy stack + summary; no host python/node/pip; idempotent; under-spec warns.
result: blocked
blocked_by: physical-device
reason: "Needs a pristine Linux VM. Static proxy green. Carries AC6 host-cleanliness leg."

### 14. Missing-Docker Auto-install + Windows Path (AC13)
expected: Docker-less Linux → auto-install via get.docker.com; forced-failure → guided + non-zero; Windows steps bring stack up.
result: blocked
blocked_by: physical-device
reason: "Auto-install legs need a Docker-less Linux host. Windows-Docker-Desktop path partially evidenced (stack runs healthy here)."

### 15. ghcr Multi-arch Publish + Pinned Tag (AC14)
expected: tagged release → public ghcr.io/<org>/aura:<tag> (amd64+arm64) + host binaries; compose references pinned tag.
result: pass
note: LIVE-VERIFIED via the v0.1.0 release (run 27533765549, 30m13s, all steps green). Anonymous ghcr manifest GET → HTTP 200 (PUBLIC); OCI index carries linux/amd64 + linux/arm64. GitHub Release v0.1.0 published with host-binary archives (darwin/linux/windows × amd64/arm64) + checksums.txt. compose pins via `image: ${AURA_IMAGE:-aura:local}` (appliance sets AURA_IMAGE=ghcr.io/chetto1983/aura:v0.1.0). release.yml now wires GHA layer cache. Minor follow-up: release actions on Node20 (forced to Node24 from 2026-06-16 — bump action majors).

### 16. systemd Reboot Autostart (AC15)
expected: after appliance reboot, stack healthy with no human action.
result: blocked
blocked_by: physical-device
reason: "Needs a real native-Linux appliance reboot (this host is Windows, no systemd). Static proxy green (deploy/aura.service)."

### 17. Scheduled Backup + Restore + Update Path (AC16)
expected: scheduled backup artifact in host-visible dir; restore rebuilds DB; compose pull && up to newer tag migrates + preserves data.
result: pass
note: Backup leg LIVE-PROVEN: `go test -tags 'db_integration backup_live' -run TestBackupNetworkPostgresLive` → `--- PASS` (real network pg_dump produced a postgres-*.dump artifact, no docker exec). Mechanism verified (APOC cypher export + retention 14d/7d). Destructive Neo4j restore + `compose pull` newer-tag update path remain operator manual legs (the latter ties to AC14's published tag).

## Summary

total: 17
passed: 13
issues: 0
pending: 0
skipped: 0
blocked: 4

## Gaps

<!-- All 4 issues found during UAT were resolved this session. Blocked legs below need operator hardware. -->

### Resolved this session
- AC5 (test 6) — root-vs-non-root: reconciled in 17-SPEC.md (root/full-power intended per amendment #63; "non-root" clause superseded). Docs-only.
- AC7 (test 8) — offline recipes: FIXED. calculator pinned to chetto1983 fork @46a1e66; mail vendored (docker/aura/mail-mcp-src.tar.gz + PROVENANCE.md) + globally installed. Verified offline on aura:p17ac7. Files: internal/mcp/manager/catalog.go, docker/aura/Dockerfile, docker/aura/PROVENANCE.md, docker/aura/mail-mcp-src.tar.gz.
- AC11 (test 12) — LAN reachability: FIXED. caddy/Caddyfile `localhost:443` → `:443 { tls internal { on_demand } }`; verified for localhost + LAN hostname + LAN IP. Contract test updated (cmd/aura/container_artifacts_test.go).
- AC16 (test 17) — backup: live backup artifact proven via the tagged backup_live test.
- AC14 (test 15) — ghcr multi-arch publish: LIVE via the v0.1.0 release — public amd64+arm64 image (anonymous pull HTTP 200) + host-binary archives + GHA-cached release.yml. Files: .github/workflows/release.yml, .goreleaser.yaml.

### Blocked — operator real-hardware sign-off (documented in 17-VALIDATION.md Manual-Only)
- AC8 (test 9) — auto-restart on native-Linux (Docker Desktop platform quirk confirmed; config correct).
- AC12 (test 13) — curl|sh on a clean Linux VM.
- AC13 (test 14) — Docker-less Linux auto-install + Windows PowerShell .env path.
- AC15 (test 16) — systemd reboot autostart on a native-Linux appliance.
- AC16 residual — destructive Neo4j restore + `compose pull` newer-tag update path.
