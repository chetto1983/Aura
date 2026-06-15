---
phase: 17
slug: packaging-distribution
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-14
validated: 2026-06-15
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

> Every task maps to one of the 16 SPEC acceptance criteria (see `17-SPEC.md`
> §Acceptance Criteria and `17-RESEARCH.md` §Validation Architecture). Go-surface
> tasks get an `<automated>` `go test` verify; packaging tasks get a
> machine-checkable shell/Docker assertion (static proxy) plus a live-Docker
> acceptance leg. All 19 task-level automated verifications were executed and
> are green as of the 2026-06-15 audit; the live-acceptance legs that need real
> appliance hardware are tracked in **Manual-Only Verifications** below.

| Task ID | Plan | Wave | Requirement (SPEC AC) | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-----------------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 17-01-01 | 01 | 1 | OPS-01 / D-04 | — | PRD-first: amendment lands before any code; gVisor tier added as transparent boundary | docs | `grep -q "gVisor" prd.md` | ✅ | ✅ green |
| 17-01-02 | 01 | 1 | OPS-01 / D-01 | — | ec7fe2f6 revert recorded; stale SPEC Background + in-scope line corrected | docs | `grep -q "ec7fe2f6" .planning/phases/17-packaging-distribution/17-SPEC.md` | ✅ | ✅ green |
| 17-02-01 | 02 | 2 | OPS-01 / AC5,6,7,2 | T-17-secret | 4 runtimes resolve in-image; `docker history` clean (no secret layer) | integration (build) | `go test ./cmd/aura -run TestProductionContainerArtifactsMatchFatImageContract` (static proxy) **+ live** `docker buildx build … && docker run --rm <img> sh -c 'command -v python3 node uvx mcp-neo4j-cypher'` | ✅ | ✅◇ proxy green; live amd64 proven (17-02 summary); arm64 Manual-Only |
| 17-02-02 | 02 | 2 | OPS-01 / AC1 (D-01) | T-17-jail | distroless root Dockerfile removed | static | `! grep -rq "gcr.io/distroless" Dockerfile` (root Dockerfile deleted) | ✅ | ✅ green |
| 17-02-03 | 02 | 2 | OPS-01 / AC1 (D-01/02) | T-17-jail | de-harden: no cap_drop/read_only/user 65532 on aura service | static | `! grep -q 'cap_drop' compose.yaml` | ✅ | ✅ green |
| 17-03-01 | 03 | 2 | OPS-01 / AC3 (D-08) | T-17-socket | in-box docker-runtime returns clear "deploy as sibling" error | unit | `go test ./internal/mcp/manager/ -run TestRuntimeGuard` | ✅ | ✅ green (2 tests) |
| 17-03-02 | 03 | 2 | OPS-01 / AC4 (D-07) | — | whatsapp resolves to sibling; no wsl.exe; fail-soft when down | unit | `go test ./internal/mcp/manager/ -run 'Whatsapp'` ⟵ *audit-fix: was `-run TestWhatsapp` (matched 0 tests)* | ✅ | ✅ green (3 tests) |
| 17-04-01 | 04 | 2 | OPS-01 / AC9 (D-10) | T-17-keyless | serve boots w/o key; LoadDB()/db migrate unchanged | unit | `go test ./internal/llm/ ./internal/config/ -run 'TestLoadAllowEmptyKey\|TestLoadServe\|TestLoadDB_NoLLMKeyRequired'` ⟵ *audit-fix: was ellipsis placeholder `…Keyless…`* | ✅ | ✅ green (3 tests) |
| 17-04-02 | 04 | 2 | OPS-01 / AC9 (D-10) | T-17-keyless | agent call w/o key fail-CLOSED → `llm_not_configured` | integration | `go test ./cmd/aura/ -run 'TestServeKeyless\|TestLLMNotConfigured'` | ✅ | ✅ green |
| 17-05-01 | 05 | 2 | OPS-01 / AC10 (D-09) | — | per-check + non-zero exit; no `docker compose ps`; direct probes | unit/integration | `go test ./cmd/aura/ -run TestDoctor` (live leg `TestDoctorLiveStack` is `db_integration neo4j_integration`-tagged) | ✅ | ✅ green (9 unit tests; live leg Manual-Only) |
| 17-05-02 | 05 | 2 | OPS-01 / AC10 (D-09) | — | `aura doctor` wired into the cobra command tree | build | `grep -q 'case "doctor"' cmd/aura/main.go && go build ./...` | ✅ | ✅ green |
| 17-06-01 | 06 | 3 | OPS-01 / AC8,1,2 | — | aura-migrate gate + aura-home persists + restart unless-stopped | integration (live) | `grep -q 'service_completed_successfully' compose.yaml` + `go test ./cmd/aura -run TestProductionContainerArtifacts…` (+ live `compose up`) | ✅ | ✅◇ proxy green; live `compose up` Manual-Only |
| 17-06-02 | 06 | 3 | OPS-01 / AC4,3 | T-17-loopback | whatsapp sibling loopback-only; streamable-HTTP mount | integration (live) | `test -f docker/whatsapp/Dockerfile` + artifact contract test (+ live tools list) | ✅ | ✅◇ proxy green; live tools-list Manual-Only |
| 17-06-03 | 06 | 3 | OPS-01 / AC11 | T-17-loopback | Caddy TLS+token fronts user surface; data/sidecars never LAN | integration (live) | `test -f caddy/Caddyfile` + artifact contract test (+ live 401-without-token probe) | ✅ | ✅◇ proxy green; live 401/LAN probe Manual-Only |
| 17-06-04 | 06 | 3 | OPS-01 / D-03 | — | optional gVisor tier transparent (full parity in-box) | integration (manual) | `test -f compose.gvisor.yaml && grep -q 'runsc' compose.gvisor.yaml` | ✅ | ✅◇ proxy green; live runsc smoke Manual-Only |
| 17-07-01 | 07 | 3 | OPS-01 / AC12,13 | — | idempotent secret-gen; HW preflight warn/abort; zero host python/node/pip | shell/integration | `bash -n scripts/install.sh && grep -q 'openssl rand' scripts/install.sh` (+ clean-host manual) | ✅ | ✅◇ proxy green; clean-host run Manual-Only |
| 17-07-02 | 07 | 3 | OPS-01 / AC14 | — | ghcr image pinned per tag (never latest); host binary retained | build | `grep -q 'ghcr.io' .goreleaser.yaml` + `go test ./cmd/aura -run TestDistributionSurface…` | ✅ | ✅◇ proxy green; real publish Manual-Only |
| 17-07-03 | 07 | 3 | OPS-01 / AC15 | — | systemd autostart on power-on | static (manual reboot) | `test -f deploy/aura.service` + artifact contract test | ✅ | ✅◇ proxy green; reboot Manual-Only |
| 17-08-01 | 08 | 3 | OPS-01 / AC16 | T-17-socket | backup model decided (no `docker exec` in socket-less box) | manual (checkpoint) | `<human-check>` decision (default option-b network `pg_dump`) | n/a | ✅ resolved (option-b-network-dump locked, 17-08 summary) |
| 17-08-02 | 08 | 3 | OPS-01 / AC16 | — | network `pg_dump` writes to host-visible `AURA_BACKUP_DIR` | unit/integration | `go test ./internal/cron/handlers/ -run TestBackup` (live leg `TestBackupNetworkPostgresLive` is `db_integration backup_live`-tagged) | ✅ | ✅ green (14 unit tests; live leg Manual-Only) |
| 17-08-03 | 08 | 3 | OPS-01 / AC16,13 | — | documented restore drill + `docker compose pull` update path | docs | `grep -q 'docker compose pull' README.md` + `go test ./cmd/aura -run TestBackupLifecycle…` | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ✅◇ automated proxy green + live-acceptance leg Manual-Only · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

> Test scaffolding that had to exist before the implementing waves. All created
> and green (verified 2026-06-15).
- [x] Go test stubs for the `aura doctor` aggregate (`cmd/aura/doctor_test.go`) — per-check + exit-code assertions (9 unit tests; live leg `doctor_integration_test.go`).
- [x] Go test stubs for the D-22 keyless-boot relaxation (`internal/config/config_serve_test.go`, `internal/llm/config_test.go`, `cmd/aura/keyless_test.go`) — serve-path boots with empty `OPENROUTER_API_KEY`; agent call returns `llm_not_configured`; `LoadDB()`/`db migrate` unchanged.
- [x] Go test stubs for the in-container docker-runtime guard (`internal/mcp/manager/runtime_guard_test.go`) — `AURA_IN_CONTAINER=1` → clear "deploy as a compose sibling" error.
- [x] Static artifact-contract harness (`cmd/aura/container_artifacts_test.go`) — pins the image/compose/Caddy/gVisor/WhatsApp/release/backup contracts without a live daemon, the no-skip-as-green proxy for the live-Docker acceptance tier.

---

## Manual-Only Verifications

> Live-acceptance legs that genuinely cannot run in the dev/CI environment
> (amd64-only, Docker Desktop, no physical reboot). Each task above has a green
> automated static/unit proxy; these remain for the operator's real-hardware
> `/gsd-verify-work` sign-off.

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Multi-arch image builds on `linux/arm64` (DGX appliance) | Req 5/14 | CI/dev host is amd64; arm64 needs `buildx` emulation or a native arm64 runner | `docker buildx build --platform linux/arm64 …` then `docker run --rm <img> aura version` |
| Live image runtime + `docker history` secret scan + Neo4j round-trip | Req 5/6/2 | Needs a built image + live stack | `docker run --rm <img> sh -c "command -v python3 && node && uvx && mcp-neo4j-cypher"`; `docker history --no-trunc <img>` shows no secret; in-container Neo4j round-trip returns version |
| `docker compose up` ordered boot + restart + `aura-home` persistence | Req 8 | Needs the live stack up | `docker compose up` healthy only after `aura-migrate` exit 0; `down && up` preserves `llm.json`/`mcp/servers.json`; kill `aura` → auto-restart |
| Caddy TLS + token gate + data-loopback LAN probes | Req 11 | Needs a second LAN host | from another LAN host the wizard is HTTPS-reachable only with the token (no token → 401); Postgres/Neo4j/embed/agent-memory/sidecars refused/unreachable |
| gVisor `runsc` tier smoke (optional appliance tier, D-03) | Req 11b | Native-Linux/arm64 only; Docker Desktop cannot host `runsc` (spikes 010/059) | On a native-Linux host with `runsc` in `/etc/docker/daemon.json`: `docker compose -f compose.yaml -f compose.gvisor.yaml up` then verify python survives in-box |
| Appliance reboot autostart (systemd) | Req 15 | Requires a real appliance/host reboot | Reboot the host; confirm `docker compose up -d` ran via the systemd unit and the stack is healthy with no human action |
| `curl\|sh` installer on a clean host | Req 12/13 | Needs a pristine Docker host (no Python/Node/pip) | Run on a clean VM; assert healthy stack + printed summary + `which python3 pip node` shows none; re-run asserts secret byte-identity |
| Public ghcr multi-arch publish | Req 14 | Needs a release tag + CI with buildx/QEMU | Tag a release; assert `ghcr.io/<org>/aura:<tag>` pulls on amd64+arm64 AND host-binary archives publish; `compose.yaml` references the pinned tag |
| Live network `pg_dump` backup + destructive Neo4j restore | Req 16 | Needs live DBs; restore is destructive | `go test -tags "db_integration backup_live" -run TestBackupNetworkPostgresLive ./internal/cron/handlers/`; restore drill against a live graph with an existing `neo4j-*.cypher` artifact |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (all Wave-0 stubs created + green)
- [x] No watch-mode flags
- [x] Feedback latency bounded (Go ~seconds–minutes; container acceptance warm-cache)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-15 — per-task Nyquist sampling map fully green (19/19 automated verifies executed live this audit). Manual-Only table retains the live-acceptance legs that require real appliance hardware (arm64, gVisor, reboot, clean-host install, LAN probes, live publish) for the final `/gsd-verify-work` sign-off.

---

## Validation Audit 2026-06-15

Audited at HEAD on branch `tabula-rasa` (phase-17 commits `59caa5d3`…`46143802` all present). Every task-level automated verification was executed live this session — static greps, `go build ./...`, and the Go unit tiers — and confirmed green via real `--- PASS` output (no `[no tests to run]` false-greens). Two recorded commands were defective and were corrected:

| Metric | Count |
|--------|-------|
| Tasks audited | 19 |
| Automated verifies executed + green | 19 |
| MISSING gaps found | 0 |
| Defective commands fixed | 2 |
| Escalated to Manual-Only | 0 (live-acceptance legs were already documented) |
| New test files generated | 0 (full TDD coverage already shipped during execution) |

**Defective-command fixes (false-green risk eliminated):**
- **17-03-02** — `-run TestWhatsapp` matched **zero** tests (real funcs are `TestCatalogIncludesWhatsappStreamableHTTPRecipe` / `TestCatalogWhatsappURLHonorsPortEnv` / `TestCatalogWhatsappURLRejectsNonPortEnv`). Corrected to `-run 'Whatsapp'` (3 tests run green).
- **17-04-01** — command was the unfinalized placeholder `-run …Keyless…` (ellipsis literal, never a valid pattern). Corrected to `-run 'TestLoadAllowEmptyKey|TestLoadServe|TestLoadDB_NoLLMKeyRequired'` (3 tests run green).

**Scope note:** this audit verifies the *automated Nyquist sampling map* (every task has a machine-checkable verify that runs and passes). It deliberately did **not** re-run the ~45–60 min live-Docker acceptance tier (multi-arch build, `compose up`, LAN probes, clean-host installer, reboot) — those remain Manual-Only by nature and carry the 17-02/17-06 recorded live evidence (amd64) plus the static artifact-contract proxy. The phase is Nyquist-compliant at the task level; the live SPEC-acceptance sign-off belongs to `/gsd-verify-work` on real hardware.
