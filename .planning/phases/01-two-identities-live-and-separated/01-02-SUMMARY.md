---
phase: 01-two-identities-live-and-separated
plan: 02
subsystem: deployment-profile
tags: [install, env-contract, sandbox-image, preflight, musr-isolation, docker]

# Dependency graph
requires:
  - phase: 01-two-identities-live-and-separated (plan 01)
    provides: "usersandbox.SandboxRouter and the eager sandbox provisioning leg — this plan adds the image-availability seam the same router needs at boot"
provides:
  - "the shipped multi-identity default: AURA_PROFILE=single_user_hardened + AURA_MUSR_ISOLATION=true + AURA_SANDBOX_IMAGE written by a fresh install"
  - "sandboxPreflight — a serve-scoped fail-closed boot gate that refuses a strict + isolation-on deployment whose sandbox image is neither local nor pullable"
  - "usersandbox.SandboxRouter.EnsureImage — the image-availability seam (ImageInspect, then ImagePull on miss)"
  - "TestInstallerFreshEnvValidatesUnderStrictProfile — the shipped key set proven bootable by a test rather than by inspection"

# Actuals (#2632)
tasks_planned: 3
tasks_completed: 3
commits: 4
duration: ~2h (interrupted; see Deviations)

# Tech tracking
files_created:
  - cmd/aura/serve_sandbox_preflight.go
  - cmd/aura/serve_sandbox_preflight_test.go
  - cmd/aura/serve_env.go
  - cmd/aura/install_env_contract_test.go
  - internal/sandbox/usersandbox/router_image.go
  - internal/sandbox/usersandbox/router_image_test.go
files_modified:
  - .env.example
  - scripts/install.sh
  - cmd/aura/serve.go
  - internal/sandbox/usersandbox/docker_backend_lifecycle.go
---

# Phase 01 Plan 02: Shipped Two-Identity Deployment Profile Summary

## Performance

| Metric | Value |
|---|---|
| Tasks | 3/3 committed |
| Commits | 4 (RED → GREEN → ship → contract test) |
| New tests | 12 |
| Build | `go build ./...` clean |
| Targeted tests | `cmd/aura` ok 22.9s · `internal/sandbox/usersandbox` ok 0.13s |
| Delegated coverage gate | **FAILED — 2977/3647 = 81.6% < 85%** (see Issues) |

## Accomplishments

The chain ISO-01 actually needs — flag, profile, sandbox image, upgrade path — now holds at
each link instead of only at the flag.

A fresh install produced by `scripts/install.sh` writes `AURA_PROFILE=single_user_hardened`,
`AURA_MUSR_ISOLATION=true` and `AURA_SANDBOX_IMAGE` into the new `.env`, so the shipped
deployment profile hosts a second identity with no operator edit (D-01/D-04). `.env.example`
documents the same three values. The installer makes the sandbox image available before
`docker compose up -d`, covering the pinned-version path that `ensure_edge_channel_env`'s
`*:edge` branch never reaches.

The silent failure that turning the flag on would otherwise create is now loud. Before this
plan, a strict deployment with an unbuilt or unpublished sandbox image booted healthy,
reported `/healthz` and `/readyz` green, and then denied every tool call with no diagnostic —
because the box image was pulled lazily at the identity's first tool call. `sandboxPreflight`
(`cmd/aura/serve_sandbox_preflight.go`) turns that into a boot-time refusal naming a command
that works, scoped strictly to the strict-profile + isolation-on combination: `aura chat` and
a non-strict `aura serve` are untouched (D-03). A non-strict deployment instead prints exactly
one INFO line at boot pointing at `docs/runbooks/musr-rollout.md` — at boot, never per request
(D-05).

Underneath it, `SandboxRouter.EnsureImage` (`internal/sandbox/usersandbox/router_image.go`, 49
LOC) is the image-availability seam: `ImageInspect`, then `ImagePull` on miss, refusing when
the backend declares no such capability and when the router is nil.

Nothing about the upgrade path changed: `compose.yaml`'s `${AURA_MUSR_ISOLATION:-false}` and
`${AURA_PROFILE:-dev}` fallbacks are untouched, and `ensure_internal_env_secrets` — the
already-have-a-`.env` path — writes none of the three names, so no existing deployment is
flipped by an upgrade (D-02).

`cmd/aura/serve.go` shed 103 lines into the new `serve_env.go` (110 LOC) on the way through,
per the refactor-on-touch rule; it now sits at 515 LOC, under the 600 ceiling.

## Task Commits

| Commit | Task | What |
|---|---|---|
| `0f4c04132` | 1 (RED) | failing tests for the sandbox-image boot preflight |
| `9988af0b7` | 1 (GREEN) | preflight + eager `EnsureImage` seam; `serve.go` split into `serve_env.go` (D-03/D-05) |
| `6155996b2` | 2 | multi-identity default in `.env.example` + `install.sh`, and the installer image step (D-01/D-04) |
| `ef642c02a` | 3 | `TestInstallerFreshEnvValidatesUnderStrictProfile` — the shipped key set proven bootable |

## Files Created/Modified

| File | LOC | Role |
|---|---|---|
| `cmd/aura/serve_sandbox_preflight.go` | 80 | the fail-closed boot gate |
| `cmd/aura/serve_sandbox_preflight_test.go` | 161 | 5 tests: refusal, pass, and both skip paths |
| `cmd/aura/serve_env.go` | 110 | env resolution split out of `serve.go` |
| `cmd/aura/install_env_contract_test.go` | 175 | the fresh-install key set validated against `ProfileSingleUserHardened` |
| `internal/sandbox/usersandbox/router_image.go` | 49 | `EnsureImage` seam |
| `internal/sandbox/usersandbox/router_image_test.go` | 208 | 6 tests incl. nil-router and no-capability denials |
| `cmd/aura/serve.go` | 515 (was 618) | preflight wired into `bootServe`; env code extracted |
| `internal/sandbox/usersandbox/docker_backend_lifecycle.go` | +22 | `DockerBackend.EnsureImage` |
| `.env.example` | +14/-1 | the three shipped keys, documented |
| `scripts/install.sh` | +57 | fresh-`.env` heredoc keys + the sandbox-image step |

## Decisions Made

- The preflight is `serve`-scoped, not process-wide: a gate that also fired for `aura chat`
  would refuse a command that has no sandbox dependency at all.
- `EnsureImage` refuses rather than silently succeeding when the backend declares no image
  capability — an image seam that quietly no-ops reproduces the exact silent degradation this
  plan exists to remove.

## Deviations from Plan

### The final verification pass did not complete

The executor that implemented this plan died mid-run to an infrastructure failure — the API
returned no response (4 min, then 10 min on the retry) — while it was performing what it
described as "one final comprehensive verification pass". All three tasks were already
committed at that point; nothing was left half-written in the tree. A continuation attempt hit
the same failure. The plan was therefore closed inline by the orchestrator, which re-measured
from the real repository state rather than trusting the dead executor's claims.

**What was actually re-measured for this SUMMARY, with output seen:**

- `go build ./...` → exit 0.
- `go test -count=1 ./cmd/aura/ ./internal/sandbox/usersandbox/` → `ok cmd/aura 22.931s`,
  `ok internal/sandbox/usersandbox 0.134s`.
- `bash scripts/docker_coverage_gate.sh` against Docker 29.7.2 with the stack up → **FAILED**,
  recorded below.

**What remains UNVERIFIED and is not claimed green here:**

- No end-to-end run of `scripts/install.sh` against a clean host was performed. Task 2's
  installer changes are covered by the contract test over the key set, not by an actual
  fresh install. `TestInstallerFreshEnvValidatesUnderStrictProfile` proves the shipped
  key/value set yields zero Fatal violations from `ValidateProfile`; it does not prove the
  heredoc that writes it runs correctly on a clean machine.
- The D-03 refusal message was not exercised against a genuinely absent-and-unpullable image
  on a live daemon; the five preflight tests drive it through the fake backend.
- The full `GO_PACKAGES` suite was not re-run at close (see Issues — three unrelated
  host-only failures make it noise on this machine, and it is the phase gate's job, not
  this plan's).

## TDD Gate Compliance

RED before GREEN, in commit order: `0f4c04132` lands `serve_sandbox_preflight_test.go`
failing, `9988af0b7` makes it pass. Task 3 is test-only and exempt. Task 2 is
config/script-only and exempt.

## Issues Encountered

### RELEASE-BLOCKING — the delegated coverage authority fails at 81.6%

This plan's own last must_have required running the delegated authority here, once both
01-01's and 01-02's production code were in the tree. It was run, and it **fails**:

```
==> docker coverage gate: owned sandbox surfaces >= 85%
    internal/sandbox/usersandbox   54.8%
    internal/agent/tools           73.4%
    cmd/aura                       74.3%
    total: 81.6%
FAIL: docker_owned_internal coverage 2977/3647 (81.6% displayed) < 85%
```

`internal/sandbox/usersandbox` is `{mode: delegated, authority: docker_coverage}` in
`scripts/coverage_package_policy.json`. Its authority is `scripts/docker_coverage_gate.sh`
(floor 85%, scope `docker_owned_internal`) — **not** `scripts/coverage_docker.sh`, which is the
`db_integration` tier and does not own this package. The two denominators never concatenate and
never average. The aggregate gate cannot see this hole.

**Phase 01 must not close while this is red.** Closing it needs daemon-free unit tests over the
pure logic of the code the phase added — spec/tar builders, path-traversal and symlink guards,
nil/disabled early-return paths, structural "not supported" capability errors — per CLAUDE.md's
rule for daemon-gated runtime code. Deferred deliberately to the phase gate rather than patched
here, so the coverage is raised once against the whole of the phase's new code (01-01's
`router_provision.go` + `EnsureBox` and this plan's `router_image.go` + `EnsureImage`) instead
of twice against halves of it.

### Three host-only test failures, pre-existing and not caused by this plan

On this Windows host `cmd/arcadedb-mcp TestUnreachableJWKSIsReportedOnce`,
`internal/idroot TestContainedDirUnresolvableRoot` and
`internal/skills TestScopedRootsDeriveBothPerIdentityPaths` fail. Measured identical at
`8b1c502ee`, the commit before this phase began, in a throwaway worktree. They are Windows
artifacts (DNS timing on a nonexistent host, file locking, path separators) in packages this
phase does not touch. The authoritative judgement is WSL / CI-Linux.

### A parallel session commits to master concurrently

Non-`01-0N` commits (e.g. `5081eb170 fix(ui): …`) landed on `master` interleaved with this
plan's. No file overlap occurred. Every commit here staged explicit paths; no `git add -A` was
used. `.planning/milestone.lock` remains untracked and uncommitted.

## User Setup Required

None for a fresh install. Existing deployments are deliberately not flipped (D-02): an operator
who wants multi-identity on an existing `.env` adds the three keys themselves, following
`docs/runbooks/musr-rollout.md` — which the new boot INFO line now points at.

## Next Phase Readiness

01-03, 01-05 (wave 2) and 01-04 (wave 3) are unblocked. The delegated coverage debt above is
carried to the phase gate and is the one thing standing between this phase and closure.

## Self-Check: PASSED WITH A NAMED FAILING GATE

Build and both touched packages verified green by direct measurement. The delegated coverage
gate is red at 81.6% and is recorded as release-blocking rather than reported as passing. The
plan's final comprehensive verification pass did not run; the specific criteria left unverified
are enumerated under Deviations rather than assumed.
