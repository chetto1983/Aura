---
phase: 37-per-user-full-capability-sandbox
plan: 01
subsystem: infra
tags: [sandbox, docker, moby, egress, config, gVisor, requirements-amendment, dockerfile]

# Dependency graph
requires:
  - phase: 36-multi-user-identity-isolation
    provides: identityctx owner-scoping + RuntimeProfile.Strict() gate + PROF env-catalog knob registry
provides:
  - "SBX-04 egress-default amendment (full-internet-minus-internal, D-06) recorded BEFORE any egress code (Gate-1)"
  - "docker/aura-sandbox/Dockerfile — the shared fat full-capability box image (D-12/D-13), digest-pinned, build+probe verified"
  - "github.com/moby/moby/{client,api} promoted to direct requires + the exact v0.4.1 options-struct API surface captured for 37-04"
  - "internal/config SandboxConfig — the AURA_SANDBOX_* operator surface (idle-TTL, cgroup caps, egress allowlist, image) + KnobSpec catalog rows, for 37-05/37-06"
affects: [37-02, 37-04, 37-05, 37-06, 37-08]

# Tech tracking
tech-stack:
  added: ["github.com/moby/moby/client v0.4.1 (direct)", "github.com/moby/moby/api v1.54.2 (direct)", "docker/aura-sandbox fat box image (debian bookworm + gh + jq + uv)"]
  patterns: ["leaf sub-struct config composition (SandboxConfig, mirrors db.Config/knowledge.Config)", "KindInt reparse-gated numeric operator knobs (PROF env-catalog)", "digest-pinned in-repo Dockerfile (D-13)"]

key-files:
  created:
    - docker/aura-sandbox/Dockerfile
    - internal/config/config_sandbox.go
    - internal/config/config_sandbox_test.go
  modified:
    - .planning/REQUIREMENTS.md
    - prd.md
    - go.mod
    - internal/config/config.go
    - internal/config/config_knobs.go

key-decisions:
  - "Egress default amended to full-internet-minus-internal (D-06) recorded in REQUIREMENTS.md + prd.md BEFORE any egress code — the PRD-first Gate-1 discipline."
  - "moby/moby/{client,api} promoted to direct via a manual go.mod edit (D-01 forward-declaration): go get + go mod tidy leave them // indirect because 37-01 imports neither; organic promotion lands in 37-02 (api) / 37-04 (client)."
  - "SandboxConfig numeric caps + TTL are KindInt so the reparse pass flags a malformed value Fatal under a strict tier (T-37-01-CFG); a KindString registration would get no reparse check."
  - "CORRECTION captured for 37-04: in moby/moby/api v1.54.2, NetworkMode lives in the `container` package (not `network` as RESEARCH stated)."

patterns-established:
  - "Digest-pinned fat box image derived from the docker/aura runtime base (D-12 posture: root, writable, no USER)."
  - "AURA_SANDBOX_* operator knobs as a leaf SandboxConfig sub-struct + matching KnobSpec catalog rows (registry-is-the-engine)."

requirements-completed: [SBX-01, SBX-03, SBX-04]

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "SBX-04 egress-default amendment (D-06) + gVisor⊥nat note recorded in REQUIREMENTS.md + prd.md before any egress code (Gate-1)."
    requirement: "SBX-04"
    verification:
      - kind: other
        ref: "grep 'full-internet-minus-internal|full public internet' + 'runc-only|#934|gVisor' .planning/REQUIREMENTS.md .planning/ROADMAP.md"
        status: pass
    human_judgment: true
    rationale: "grep proves the amended text is present, but a human should confirm the requirement-policy wording faithfully captures the D-04/D-05/D-06 intent (a planning-artifact adequacy call, not a machine assertion)."
  - id: D2
    description: "Fat full-capability sandbox image builds and proves full-capability: root, writable rootfs, python3/node/uvx/git/gh/jq all resolve; digest-pinned base + uv."
    requirement: "SBX-01"
    verification:
      - kind: integration
        ref: "docker build -f docker/aura-sandbox/Dockerfile && docker run probe (command -v python3/node/uvx/git/gh/jq; id -u=0; echo x>/probe)"
        status: pass
    human_judgment: false
  - id: D3
    description: "moby/moby/{client,api} promoted to direct requires; the exact v0.4.1 options-struct API surface captured for the 37-04 DockerBackend."
    requirement: "SBX-01"
    verification:
      - kind: integration
        ref: "go mod verify && ! grep 'moby/moby/(client|api).*// indirect' go.mod && go build ./..."
        status: pass
    human_judgment: false
  - id: D4
    description: "AURA_SANDBOX_* operator config surface (idle-TTL/CPU/memory/pids/egress-allowlist/image) defined, cataloged (KindInt reparse gate), and parse-tested."
    requirement: "SBX-03"
    verification:
      - kind: unit
        ref: "internal/config/config_sandbox_test.go#TestLoad_SandboxConfig (defaults, overrides, IDLE_TTL_SEC=abc → Fatal under server_production / Warn under dev)"
        status: pass
    human_judgment: false

# Metrics
duration: ~40 min
completed: 2026-07-06
status: complete
---

# Phase 37 Plan 01: Foundation & Gate-1 Summary

**SBX-04 egress-default amendment (full-internet-minus-internal, D-06) committed before any egress code, plus the digest-pinned fat box image, moby SDK promoted to direct with its v0.4.1 API surface captured, and the AURA_SANDBOX_* operator config surface defined + reparse-gated.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-07-06T21:55Z (approx)
- **Completed:** 2026-07-06T22:36Z
- **Tasks:** 4
- **Files modified:** 8 (3 created, 5 modified)

## Accomplishments

- **Gate-1 PRD-first discipline honored:** the SBX-04 egress default is amended from the literal `--network none` to **full public internet minus the tenancy boundary** (DROP RFC1918 + `169.254.169.254` + the shared-services bridge, D-04/D-05/D-06) in REQUIREMENTS.md + prd.md, with the gVisor⊥nat mutual-exclusion (issue #934) recorded — committed as the very first commit, before any egress code exists.
- **The shared fat box image is real and verified:** `docker/aura-sandbox/Dockerfile` builds a digest-pinned debian box with python3/node24/uv/git/gh 2.96.0/jq baked in; the probe confirms all runtimes resolve, `id -u=0` (root), and the rootfs is writable — the D-12/D-13 posture, no distroless/non-root/read-only regression.
- **The moby Go SDK is a direct dep** and its exact v0.4.1 options-struct API surface (which differs from every `docker/docker/client` example — RESEARCH Pitfall 1) is captured below for the 37-04 DockerBackend.
- **The AURA_SANDBOX_* operator surface** is a leaf `SandboxConfig` sub-struct + six cataloged KnobSpec rows; the numeric caps + TTL are KindInt so a malformed value is FLAGGED Fatal under a strict tier, never silently defaulted.

## Task Commits

1. **Task 1: SBX-04 amendment (D-06 Gate-1)** — `f31926ff` (docs)
2. **Task 2: Fat full-capability sandbox image (D-12/D-13)** — `a802113c` (feat)
3. **Task 3: Promote moby modules to direct (D-01)** — `e8af7e23` (chore)
4. **Task 4: AURA_SANDBOX_* operator config surface** — `14499b66` (feat)

## Files Created/Modified

- `docker/aura-sandbox/Dockerfile` (new) — the shared fat box image (digest-pinned base + uv; gh/jq/python3/node/uvx/git; no USER, no aura binary).
- `internal/config/config_sandbox.go` (new) — `SandboxConfig` + `loadSandboxConfig()` + `int64Default` helper.
- `internal/config/config_sandbox_test.go` (new) — defaults/overrides + reparse-Fatal parse test.
- `.planning/REQUIREMENTS.md` — SBX-04 amended (D-06 + gVisor⊥nat note).
- `prd.md` — Phase-37 SBX-04 egress amendment note in the Slice-2 sandbox section.
- `go.mod` — moby/moby/{client,api} promoted indirect→direct.
- `internal/config/config.go` — `Sandbox SandboxConfig` field + `loadBase()` wiring (590 LOC, ≤600).
- `internal/config/config_knobs.go` — six `AURA_SANDBOX_*` KnobSpec rows (4 KindInt, 2 KindString).

## Moby v0.4.1 API surface (captured for 37-04 — RESEARCH Pitfall 1 / Assumption A2)

The v0.4.1 client uses an **options-struct API** (NOT the classic `docker/docker/client` positional signature). Verbatim `go doc` against the vendored module:

```
# github.com/moby/moby/client.ContainerCreateOptions
type ContainerCreateOptions struct {
	Config           *container.Config
	HostConfig       *container.HostConfig
	NetworkingConfig *network.NetworkingConfig
	Platform         *ocispec.Platform
	Name             string
	Image            string // shortcut for Config.Image — only one of Image or Config.Image
}

# github.com/moby/moby/client.ExecCreateOptions
type ExecCreateOptions struct {
	User, DetachKeys, WorkingDir string
	Privileged, TTY, AttachStdin, AttachStderr, AttachStdout bool
	ConsoleSize ConsoleSize
	Env, Cmd []string
}

# github.com/moby/moby/client.ExecAttachOptions
type ExecAttachOptions struct {
	TTY bool
	ConsoleSize ConsoleSize `json:",omitzero"`
}

# github.com/moby/moby/client.CopyToContainerOptions
type CopyToContainerOptions struct {
	DestinationPath           string
	Content                   io.Reader   // tar stream
	AllowOverwriteDirWithFile bool
	CopyUIDGID                bool
}

# github.com/moby/moby/api/types/container.HostConfig (key fields for SBX-02 pinning)
type HostConfig struct {
	Binds          []string        // pin nil (no host bind — D-10)
	NetworkMode    NetworkMode     // pin non-host; egress via sidecar netns "container:<box>"
	AutoRemove     bool            // pin false (Pitfall 5: --rm destroys a suspendable box)
	Privileged     bool            // pin false (SBX-02)
	ReadonlyRootfs bool            // leave false (D-12 fat box)
	CapAdd, CapDrop []string       // keep default caps (D-12)
	Runtime        string          // "" (runc) or "runsc" (D-12, server_production only)
	Mounts         []mount.Mount   // named volume + tmpfs scratch + uv warm-cache
	Init           *bool
	Resources                      // embedded (below)
	...  // + Sysctls, SecurityOpt, Tmpfs, PidMode, IpcMode, UTSMode, etc.
}

# github.com/moby/moby/api/types/container.Resources (cgroup caps)
type Resources struct {
	NanoCPUs  int64  // CPU quota in 1e-9 CPUs  (= CPULimit * 1e9)
	Memory    int64  // memory limit in bytes   (= MemoryLimit)
	PidsLimit *int64 // pids cap (POINTER — take &int64)  (= &PidsLimit)
	CPUShares, CPUPeriod, CPUQuota int64
	...
}

# github.com/moby/moby/api/types/container.NetworkMode  (CORRECTION — see note)
type NetworkMode string  // methods: IsHost() IsNone() IsContainer() IsBridge() IsDefault() ...

# github.com/moby/moby/api/types/network.NetworkingConfig  (referenced by ContainerCreateOptions)
type NetworkingConfig struct {
	EndpointsConfig map[string]*network.EndpointSettings
}
```

**API-surface corrections for 37-04:**
1. **`NetworkMode` is in the `container` package** in v1.54.2 (`container.NetworkMode`), NOT `network.NetworkMode` as RESEARCH stated. `ContainerCreateOptions.NetworkingConfig` IS `*network.NetworkingConfig` (the `network` package is still used for that).
2. `ContainerCreateOptions` embeds `*container.Config` + `*container.HostConfig` + `*network.NetworkingConfig` (Assumption A2 CONFIRMED).
3. `Resources.PidsLimit` is a `*int64` — the translator must take the address of the config's `PidsLimit int64`.
4. `HostConfig` DOES expose `Privileged`/`Binds`/`NetworkMode`/`ReadonlyRootfs` — SBX-02's unrepresentability (37-02) must pin these in the private translator since the moby type cannot omit them.

## Resolved image digests (D-13)

- Base pins (from the live registry, 2026-07-06):
  - `debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df`
  - `ghcr.io/astral-sh/uv:0.11.21@sha256:ff07b86af50d4d9391d9daf4ff89ce427bc544f9aae87057e69a1cc0aa369946`
- Local build (`aura-sandbox:build`): image config `sha256:a6dafb91017f6ecaec137f9f3a5b377a69f5efb531b370e63b1e727fe86cf2a5`, manifest list `sha256:97d978c1c98beb1d7f094e374e60549fbdb7d41ad2329bcec586325df1696233`. (A local build has no registry digest until pushed; production overrides `AURA_SANDBOX_IMAGE` with the pushed digest ref.)
- Probe (green): `python3=/usr/bin/python3`, `node=/usr/bin/node`, `uvx=/usr/local/bin/uvx`, `git=/usr/bin/git`, `gh=/usr/bin/gh` (2.96.0), `jq=/usr/bin/jq`, `uid=0`; `echo x > /probe` round-trips.

## Resolved SandboxConfig field list (for 37-05/37-06 specFor / buildEgressSidecar)

| Field | Type | Env | Default | Downstream mapping |
|-------|------|-----|---------|--------------------|
| `IdleTTLSec` | `int` | `AURA_SANDBOX_IDLE_TTL_SEC` | 1800 (30m) | reaper: `time.Duration(IdleTTLSec)*time.Second` (D-08) |
| `CPULimit` | `int` | `AURA_SANDBOX_CPU_LIMIT` | 2 | `Resources.NanoCPUs = int64(CPULimit)*1e9` (D-14) |
| `MemoryLimit` | `int64` | `AURA_SANDBOX_MEMORY_LIMIT` | 2 GiB | `Resources.Memory` (D-14) |
| `PidsLimit` | `int64` | `AURA_SANDBOX_PIDS_LIMIT` | 512 | `Resources.PidsLimit = &PidsLimit` (D-14) |
| `EgressAllowlist` | `[]string` | `AURA_SANDBOX_EGRESS_ALLOWLIST` | empty = floor-only | `EgressPolicy.FQDNAllowlist` (D-06) |
| `Image` | `string` | `AURA_SANDBOX_IMAGE` | `aura-sandbox:latest` | `SandboxSpec.Image` (D-13) |

## Decisions Made

- **Gate-1 ordering:** the SBX-04 amendment is commit #1 (`f31926ff`), strictly before any egress code — this plan writes zero egress/`internal/sandbox` code, so the discipline holds trivially and is recorded for the wave-3 egress plan (37-06).
- **moby direct-promotion via manual go.mod edit** (see Deviations) — a deliberate D-01 forward-declaration.
- **CPULimit modeled as a CPU count** (not raw NanoCPUs) for an operator-friendly knob; 37-05 does the ×1e9 conversion.
- **`aura-sandbox:latest` as the image default** — a local build tag; production overrides with a digest-pinned registry ref (a not-yet-pushed local image has no stable pull-by-digest ref).

## Deviations from Plan

### 1. [Rule 3 - Blocking / Go-module semantics] moby direct-promotion required a manual go.mod edit

- **Found during:** Task 3.
- **Issue:** The plan's method (`go get …@v0.4.1 && go get …@v1.54.2 && go mod tidy`) does NOT produce direct requires — empirically confirmed: `go mod tidy` re-annotates both modules `// indirect` because **no in-repo package imports them yet** (37-02's `translate.go` imports `moby/moby/api`; 37-04's `docker_backend.go` imports `moby/moby/client`). The plan's `! grep '// indirect'` acceptance can't be met by `go get`+`tidy` alone.
- **Fix:** Promoted both to the direct require block via a manual `go.mod` edit (the plan's explicit, repeated acceptance: must_haves.truth #3 + verify + artifact `contains`). An import-anchor alternative was rejected — it would place a file in `internal/sandbox/usersandbox/`, which **37-02 owns in the same wave** (a cross-plan overlap the orchestrator's "no overlap" invariant forbids), and `config` (this plan's only Go package) is a leaf that must not import moby.
- **Safety:** CI is tidy-safe (only `.goreleaser.yaml` runs `go mod tidy`, at release time — no CI/Makefile `tidy -diff` gate). `go mod verify` + `go build ./...` are green. The state converges with the organic import-driven promotion when 37-02 (api) / 37-04 (client) land; a stray `go mod tidy` before then would revert `client` (harmless — 37-04 re-promotes it via its import).
- **Files modified:** `go.mod`.
- **Committed in:** `e8af7e23`.

### 2. [Note — no edit needed] ROADMAP.md SC#4 already carried the D-06 posture

- **Found during:** Task 1.
- **Detail:** Task 1's action anticipated stale `--network none` text in ROADMAP.md SC#4 (line ~342) and the milestone-index SC#4 (line ~95). Both **already read the D-06 posture** ("full public internet minus the tenancy boundary … not `--network none`") — reconciled when the phase was added to the roadmap. The verify grep passes against the existing text; no edit was made (SCOPE CONTROL — no gratuitous change). ROADMAP.md is therefore absent from this plan's diff.

### 3. [Minor — API-surface correction captured] NetworkMode package location

- **Found during:** Task 3 (`go doc`).
- **Detail:** RESEARCH referenced `network.NetworkMode`; in moby/moby/api v1.54.2 the type is `container.NetworkMode`. Captured above so 37-04 imports it from the right package (the `network` package is still correct for `NetworkingConfig`).

---

**Total deviations:** 1 auto-fixed (Rule 3 — Go-module semantics), 2 informational notes.
**Impact on plan:** No scope creep. The manual promotion satisfies the plan's explicit acceptance while staying inside this plan's declared file set and respecting the wave's package-ownership boundary. All acceptance criteria and the plan-level `<verification>` are green.

## Issues Encountered

None — every task's acceptance criteria verified green (grep checks, `docker build`+probe, `go mod verify`/`go build`, `go test -run Sandbox`).

## User Setup Required

None — no external service configuration. (Operators MAY set `AURA_SANDBOX_*` knobs and, in production, override `AURA_SANDBOX_IMAGE` with a digest-pinned registry ref; all have safe defaults.)

## Next Phase Readiness

- **37-02** (SBX-02 spec/translator/Backend) can build against the captured moby API surface without guessing; note the `container.NetworkMode` location and `Resources.PidsLimit *int64` pointer.
- **37-04** (DockerBackend) has the exact `ContainerCreateOptions`/`ExecCreateOptions`/`ExecAttachOptions`/`CopyToContainerOptions` field wiring.
- **37-05/37-06** have the resolved `SandboxConfig` field list to read into `specFor` / `buildEgressSidecar`.
- **37-06** (egress) has the recorded SBX-04 default + the gVisor⊥nat mutual-exclusion constraint.
- Blockers: none. Note the moby `// indirect` reconciliation is expected to converge organically in 37-02/37-04 (see Deviation 1).

## Self-Check: PASSED

- Created files exist: `docker/aura-sandbox/Dockerfile`, `internal/config/config_sandbox.go`, `internal/config/config_sandbox_test.go` — all FOUND.
- Commits exist: `f31926ff`, `a802113c`, `e8af7e23`, `14499b66` — all FOUND.
- Plan `<verification>` re-run green: `docker build`+probe (root/writable/full-runtime), `go build ./...` + `go mod verify` (moby direct), REQUIREMENTS.md + prd.md carry the dated D-06 + gVisor⊥nat note, `go test ./internal/config/ -run Sandbox` green, `internal/sandbox/` writes zero Go source (dir does not exist).

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-06*
