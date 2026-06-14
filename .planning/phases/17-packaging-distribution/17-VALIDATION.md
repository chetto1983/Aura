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

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {N}-01-01 | 01 | 1 | OPS-01 / Req-{XX} | T-17-XX / — | {expected secure behavior or "N/A"} | unit / integration | `{command}` | ✅ / ❌ W0 | ⬜ pending |

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
