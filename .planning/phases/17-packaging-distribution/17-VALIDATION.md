---
phase: 17
slug: packaging-distribution
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-14
---

# Phase 17 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `17-RESEARCH.md` §Validation Architecture. Phase 17 is mostly
> Dockerfile / compose / shell-script / packaging work with a SMALL Go surface
> (`aura doctor`, the D-22 config relaxation, the in-container docker-runtime
> guard). Two validation tiers: **Go unit/integration** (CLAUDE.md ≥85% owned
> floor, race, no-skip-as-green) and **live-Docker integration** (the packaging
> acceptance criteria — image build, compose up, installer, LAN probes).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go 1.26) for the code surface; `docker`/`docker compose` + shell assertions for the packaging surface |
| **Config file** | none — `Makefile` targets (`make quality`, `make quality-full`, `make coverage`) + `scripts/coverage_gate.sh` |
| **Quick run command** | `go test ./internal/config/... ./cmd/aura/... ./internal/mcp/manager/...` (the touched Go packages) |
| **Full suite command** | `make quality-full` (vet+build+lint+race+vuln+coverage ≥85%) **plus** the live-Docker acceptance tier (image build on amd64+arm64, `docker compose up`, `aura doctor`, installer dry-run, LAN-reachability probes) |
| **Estimated runtime** | Go suite ~minutes; live-Docker cold image build ~45–60 min (cache-stable warm ~1.4s per spike 060) |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched package; for a Dockerfile/compose/script task, run its targeted assertion (e.g. `docker build` of the changed stage, `docker compose config` lint).
- **After every plan wave:** Run `make quality` (no-container) for Go waves; bring the stack up and run the live-Docker acceptance assertions for the packaging waves.
- **Before `/gsd-verify-work`:** `make quality-full` green AND the 16 SPEC acceptance criteria each demonstrated against a live stack.
- **Max feedback latency:** Go ~seconds–minutes; container acceptance bounded by image build (warm-cache, not cold).

---

## Per-Task Verification Map

> Populated by the planner / `gsd-nyquist-auditor` once tasks exist. Each task
> maps to one of the 16 SPEC acceptance criteria (see `17-SPEC.md` §Acceptance
> Criteria and `17-RESEARCH.md` §Validation Architecture). Go-surface tasks get
> an `<automated>` `go test` verify; packaging tasks get a machine-checkable
> shell/Docker assertion (live-Docker integration tier).

| Task ID | Plan | Wave | Requirement (SPEC AC) | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-----------------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 17-01-01 | 01 | 1 | OPS-01 / D-04 | — | PRD-first: amendment lands before any code; gVisor tier added as transparent boundary | docs | `grep -q "gVisor" prd.md` | ✅ | ⬜ pending |
| 17-01-02 | 01 | 1 | OPS-01 / D-01 | — | ec7fe2f6 revert recorded; stale SPEC Background + in-scope line corrected | docs | `grep -q "ec7fe2f6" .planning/phases/17-packaging-distribution/17-SPEC.md` | ✅ | ⬜ pending |
| 17-02-01 | 02 | 2 | OPS-01 / AC5,6,7,2 | T-17-secret | 4 runtimes resolve in-image; `docker history` clean (no secret layer) | integration (build) | `docker buildx build … && docker run --rm <img> sh -c 'command -v python3 node uvx mcp-neo4j-cypher'` | ❌ W0 | ⬜ pending |
| 17-02-02 | 02 | 2 | OPS-01 / AC1 (D-01) | T-17-jail | distroless root Dockerfile removed | static | `! grep -rq "gcr.io/distroless" Dockerfile` | ✅ | ⬜ pending |
| 17-02-03 | 02 | 2 | OPS-01 / AC1 (D-01/02) | T-17-jail | de-harden: no cap_drop/read_only/user 65532 on aura service | static | `! grep -q 'cap_drop' compose.yaml` | ✅ | ⬜ pending |
| 17-03-01 | 03 | 2 | OPS-01 / AC3 (D-08) | T-17-socket | in-box docker-runtime returns clear "deploy as sibling" error | unit | `go test ./internal/mcp/manager/ -run TestRuntimeGuard` | ❌ W0 | ⬜ pending |
| 17-03-02 | 03 | 2 | OPS-01 / AC4 (D-07) | — | whatsapp resolves to sibling; no wsl.exe; fail-soft when down | unit | `go test ./internal/mcp/manager/ -run TestWhatsapp` | ❌ W0 | ⬜ pending |
| 17-04-01 | 04 | 2 | OPS-01 / AC9 (D-10) | T-17-keyless | serve boots w/o key; LoadDB()/db migrate unchanged | unit | `go test ./internal/llm/ ./internal/config/ -run …Keyless…` | ❌ W0 | ⬜ pending |
| 17-04-02 | 04 | 2 | OPS-01 / AC9 (D-10) | T-17-keyless | agent call w/o key fail-CLOSED → `llm_not_configured` | integration | `go test ./cmd/aura/ -run 'TestServeKeyless\|TestLLMNotConfigured'` | ❌ W0 | ⬜ pending |
| 17-05-01 | 05 | 2 | OPS-01 / AC10 (D-09) | — | per-check + non-zero exit; no `docker compose ps`; direct probes | unit/integration | `go test ./cmd/aura/ -run TestDoctor` | ❌ W0 | ⬜ pending |
| 17-05-02 | 05 | 2 | OPS-01 / AC10 (D-09) | — | `aura doctor` wired into the cobra command tree | build | `grep -q 'case "doctor"' cmd/aura/main.go && go build ./...` | ✅ | ⬜ pending |
| 17-06-01 | 06 | 3 | OPS-01 / AC8,1,2 | — | aura-migrate gate + aura-home persists + restart unless-stopped | integration (live) | `grep -q 'service_completed_successfully' compose.yaml` (+ live `compose up`) | ✅ | ⬜ pending |
| 17-06-02 | 06 | 3 | OPS-01 / AC4,3 | T-17-loopback | whatsapp sibling loopback-only; streamable-HTTP mount | integration (live) | `test -f docker/whatsapp/Dockerfile` (+ live tools list) | ✅ | ⬜ pending |
| 17-06-03 | 06 | 3 | OPS-01 / AC11 | T-17-loopback | Caddy TLS+token fronts user surface; data/sidecars never LAN | integration (live) | `test -f caddy/Caddyfile` (+ live 401-without-token probe) | ✅ | ⬜ pending |
| 17-06-04 | 06 | 3 | OPS-01 / D-03 | — | optional gVisor tier transparent (full parity in-box) | integration (manual) | `test -f compose.gvisor.yaml && grep -q 'runsc' compose.gvisor.yaml` | ✅ | ⬜ pending |
| 17-07-01 | 07 | 3 | OPS-01 / AC12,13 | — | idempotent secret-gen; HW preflight warn/abort; zero host python/node/pip | shell/integration | `bash -n scripts/install.sh && grep -q 'openssl rand' scripts/install.sh` (+ clean-host manual) | ✅ | ⬜ pending |
| 17-07-02 | 07 | 3 | OPS-01 / AC14 | — | ghcr image pinned per tag (never latest); host binary retained | build | `grep -q 'ghcr.io' .goreleaser.yaml` | ✅ | ⬜ pending |
| 17-07-03 | 07 | 3 | OPS-01 / AC15 | — | systemd autostart on power-on | static (manual reboot) | `test -f deploy/aura.service` | ✅ | ⬜ pending |
| 17-08-01 | 08 | 3 | OPS-01 / AC16 | T-17-socket | backup model decided (no `docker exec` in socket-less box) | manual (checkpoint) | `<human-check>` decision (default option-b network `pg_dump`) | n/a | ⬜ pending |
| 17-08-02 | 08 | 3 | OPS-01 / AC16 | — | network `pg_dump` writes to host-visible `AURA_BACKUP_DIR` | unit/integration | `go test ./internal/cron/handlers/ -run TestBackup` | ❌ W0 | ⬜ pending |
| 17-08-03 | 08 | 3 | OPS-01 / AC16,13 | — | documented restore drill + `docker compose pull` update path | docs | `grep -q 'docker compose pull' README.md` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

> Test scaffolding that must exist before the implementing waves. Filled by the
> planner. Candidates from `17-RESEARCH.md`:
- [ ] Go test stubs for the `aura doctor` aggregate (`cmd/aura`) — per-check + exit-code assertions (reuse the `web doctor`/`mcp doctor` pattern).
- [ ] Go test stubs for the D-22 keyless-boot relaxation (`internal/config`) — serve-path boots with empty `OPENROUTER_API_KEY`; agent call returns `llm_not_configured`; `LoadDB()`/`db migrate` unchanged.
- [ ] Go test stubs for the in-container docker-runtime guard (`internal/mcp/manager/runtime.go`) — `AURA_IN_CONTAINER=1` → clear "deploy as a compose sibling" error.
- [ ] Live-Docker acceptance harness — a script that builds the image, brings the stack up, and runs the 16 acceptance-criteria probes (no-skip-as-green: `t.Fatal`/non-zero under `$CI` when the env/stack is absent).

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Multi-arch image builds on `linux/arm64` (DGX appliance) | Req 5/14 | CI/dev host is amd64; arm64 needs `buildx` emulation or a native arm64 runner | `docker buildx build --platform linux/arm64 …` then `docker run --rm <img> aura version` |
| gVisor `runsc` tier smoke (optional appliance tier, D-03) | D-03 (post-amendment) | Native-Linux/arm64 only; Docker Desktop cannot host `runsc` (spikes 010/059) | On a native-Linux host with `runsc` registered in `/etc/docker/daemon.json`: `docker compose -f compose.yaml -f compose.gvisor.yaml up` then verify python survives in-box |
| Appliance reboot autostart (systemd) | Req 15 | Requires a real appliance/host reboot | Reboot the host; confirm `docker compose up -d` ran via the systemd unit and the stack is healthy with no human action |
| `curl\|sh` installer on a clean host | Req 12/13 | Needs a pristine Docker host (no Python/Node/pip) | Run on a clean VM; assert healthy stack + printed summary + `which python3 pip node` shows none; re-run asserts secret byte-identity |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency bounded (Go ~minutes; container acceptance warm-cache)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
