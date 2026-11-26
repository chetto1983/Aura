# Retire gVisor: every box and the Aura container run on runc

**Closed (2026-11-26):** done, as the Status below records; its boxes had never been ticked, and are now,
so the plan no longer reads as open.

**Status (2026-10-15):** DONE. f80f96f..f94a458. Recorded in docs/superpowers/verification/2026-10-11-retire-gvisor-lab-vm.md.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Nothing in Aura selects, installs, documents or asks about gVisor any more. Boxes and the `aura` container run on Docker's default runtime, and an `install.conf` written by an already published `create-aura-appliance` still installs.

**Architecture:** gVisor sits at two levels today. Per box, `specFor` picks `Runsc` under `server_production`, through `RuntimeClass` in `SandboxSpec`. For the Aura container, `compose.yaml` reads `runtime: ${AURA_RUNTIME:-runc}`, which `install.sh --gvisor` sets after it installs `runsc`. Both go. `RuntimeClass`, `NewSandboxSpec` (no production caller), `ErrRunscRequiresServerProduction`, `ErrRunscFQDNMutualExclusion`, the router's `profile` parameter (read only to pick the runtime) and the `gateEgressAllowlistRuntime` config gate leave with it.

**Tech Stack:** Go 1.27, POSIX shell (installer and its tests), TypeScript and vitest (`packages/create-aura`).

**Spec:** the operator's decision on 2026-10-11: runc works well, and gVisor caused problems in the past. Recorded in prd.md §12 by Task 1. The VM state was measured the same day: every container on `runc`, `AURA_RUNTIME` unset, `runsc` 20260928.0 installed and registered in `/etc/docker/daemon.json` but unused (`docs/superpowers/verification/2026-10-11-browser-sidecar-lab-vm.md`).

## Global Constraints

- Files at or under 600 lines. Every file touched gets its comments brought up to date in the same commit.
- After every Go edit: `go vet`, `go build` and `go test -race` of the touched packages, run in WSL. No database tier is involved.
- Machines already installed with `--gvisor` keep `runsc`, its apt source and its `daemon.json` entry untouched (operator, 2026-10-11). The updater does not touch apt or dockerd.
- `install.sh` refuses `install.conf` keys it does not know. `create-aura-appliance` up to 0.2.10 writes `gvisor=`, so the key stays accepted and its value is ignored. A `gvisor=true` prints a warning, because a user who asked for it must learn they did not get it.
- `create-aura` changes in source only; publishing waits for the operator.
- Payload files change (`compose.yaml`, `deploy/aura.service`, `scripts/install.sh`), so `make payload-manifest` runs and its manifest is committed before the push.
- Dated history stays as written: `.planning/`, earlier plans and specs, `docs/research/`, `spikes/`, earlier verification files.
- Commit per task, with the why.

## Review Focus

1. **No path picks a runtime.** `toHostConfig` leaves `HostConfig.Runtime` empty for every spec, so the daemon's default applies. Pinned by the translate tests.
2. **An old `install.conf` still installs.** `gvisor=false` and `gvisor=true` both parse, and `true` warns. Pinned by `scripts/install_config_test.sh`.
3. **`server_production` with an egress allowlist now boots**, because its boxes run runc, where the allowlist works. Pinned by a config test.
4. **Nothing else changed:** the egress floor, the FQDN allowlist mode and every resource cap behave as before.

## Decisions taken while writing this plan

- **`--gvisor` is removed, not deprecated.** The flag is typed by a person, who gets the installer's existing "unknown argument" error and the usage. The config key stays because a program writes it.
- **The `config_validate` gate goes.** It refused an allowlist under `server_production` only because those boxes ran on runsc, which has no nat table (gVisor #934). On runc the allowlist works under every profile.
- **The egress floor stays filter-table only.** gVisor was one reason for that choice. The other, keeping the floor independent of the nat-table FQDN mode, still holds, so only the comments change.
- **`AURA_RUNTIME` left in an existing `.env` is inert.** Compose no longer reads it, so it is not cleaned up.

## File structure

| File | Task | Change |
|---|---|---|
| `prd.md` §12 and §17, `docs/adr/0037-per-identity-docker-sandbox.md`, `docs/adr/0044-deployment-profiles.md`, `README.md`, `docs/INSTALL.md`, `docs/ARCHITECTURE.md` | 1 | Record the retirement, drop the option |
| `internal/sandbox/usersandbox/spec.go`, `translate.go`, `egress.go`, `router.go`, `docker_backend.go` (+ tests) | 2 | No runtime enum, no runsc error paths, no router profile |
| `internal/config/config_validate.go` (+ test) | 2 | Gate removed |
| `cmd/aura/serve_dispatch.go`, `cmd/aura/serve_dispatch_egress_integration_test.go`, `internal/agent/tools/*_docker_test.go` | 2 | Router callers and comments |
| `scripts/install.sh`, `scripts/install_lib_test.sh`, `scripts/install_config_test.sh`, `compose.yaml`, `deploy/aura.service`, `docker/aura-egress/Dockerfile`, `cmd/aura/container_artifacts_test.go`, `cmd/aura/distribution_artifacts_test.go`, `scripts/payload_manifest.txt` | 3 | Installer, compose, unit and image comments |
| `packages/create-aura/src/{types,prompts,config-file}.ts`, `messages/{en,it}.ts` (+ tests) | 4 | No gVisor question |
| `.claude/skills/spike-findings-Aura/SKILL.md`, `references/packaging-box.md`, `references/sandbox-runtime.md` | 5 | Mark the gVisor tier retired |

### Task 1: Record the retirement
- [x] prd.md §12: a dated paragraph, the operator's reason and the VM state. §17: "runtime prerequisites" no longer applies to `server_production`.
- [x] ADR 0037 and 0044: a dated note that the gVisor option is retired. README, INSTALL and ARCHITECTURE drop it.

### Task 2: Go
- [x] Tests first: the translate tests pin `HostConfig.Runtime == ""` for every spec; the runsc cases leave `spec_test.go`, `egress_test.go` and `router_test.go`; the config test asserts that `server_production` plus an allowlist validates.
- [x] Remove `RuntimeClass`, `Runc`, `Runsc`, `dockerRuntime`, `SandboxSpec.Runtime`, `NewSandboxSpec`, `ErrRunscRequiresServerProduction`, `ErrRunscFQDNMutualExclusion`, the `runtime` parameter of `buildEgressSidecar`, the router's `profile` and `gateEgressAllowlistRuntime`. Update the callers and comments.
- [x] vet, build, `go test -race` on `usersandbox`, `config`, `cmd/aura`, `agent/tools`; compile the `docker_integration` tag.

### Task 3: Installer, compose, unit
- [x] Tests first: `install_config_test.sh` parses `gvisor=false` and `gvisor=true` without failing, and `true` warns. The `provision_gvisor` block leaves `install_lib_test.sh`. The artifact tests drop their gVisor wants.
- [x] Remove `--gvisor`, `GVISOR`, `provision_gvisor` and `set_env_value AURA_RUNTIME runsc`. Keep `gvisor)` in the config parser. Remove compose's `runtime:` line and the unit's gVisor block, and update the egress Dockerfile comments.
- [x] `make payload-manifest`; the installer shell tests and the artifact tests pass.

### Task 4: create-aura
- [x] Tests first, then remove the `gvisor` field, its question, its config line and its messages. typecheck and vitest pass.

### Task 5: Skill note and closing gates
- [x] The spike-findings skill marks the gVisor tier retired, so no later spec reuses it.
- [x] Gates: golangci-lint on the touched packages, the full pre-push from WSL, CI green on the pushed SHA.
  2026-10-11: golangci-lint 0 issues on the four packages and the full pre-push green. CI did not finish: GitHub flagged the account ("owner marked spammy", 14:21 UTC) and cancelled every run on `142813a46`; the CI tick waits for the account to be reinstated.
- [x] Lab VM, after the updater has applied the new payload: `/opt/aura/compose.yaml` has no `runtime:` line, every container runs `runc` and the stack is healthy. Recorded in `docs/superpowers/verification/`.
