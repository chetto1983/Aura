---
phase: 05-sandbox-2a-stateless
plan: 02
subsystem: sandbox
tags: [sandbox, sidecar, seccomp, gvisor, runsc, docker, compose, hardening, supply-chain]

# Dependency graph
requires:
  - phase: 05-sandbox-2a-stateless
    plan: 01
    provides: "PRD amendments — gVisor-primary D12 (#36), D-09 auto-start, D-20 build-time bake (#37)"
provides:
  - "sandbox/sidecar.py — stdlib http.server: POST /exec/python + /exec/shell + GET /healthz, D-16 JSON contract, 1 MiB per-stream truncation, timeout/oom/pids limit_hit"
  - "sandbox/Dockerfile — python:3.12-slim digest-pinned, non-root uid 65532, BUILD-time --require-hashes bake of curated requirements + import smoke, NO runtime pip (D-20)"
  - "sandbox/requirements.txt — curated 11-package set + 12 transitive deps, version+sha256-hash-pinned for amd64+arm64 cp312 (D-20b)"
  - "sandbox/seccomp.json — multi-arch positive allowlist (moby v27.5.1 baseline minus dangerous+network set), 394 allowed, by-name, SCMP_ACT_ERRNO default"
  - "compose.yaml aura-sandbox — full CAP-01 SC#5 hardening floor + loopback healthcheck"
  - "compose.gvisor.yaml — x86-only runsc overlay (D-04)"
  - "Makefile sandbox-up — arch-gated operator default (gVisor overlay default-on x86, runc+seccomp arm64)"
affects: [05-03-go-runner, 05-04-gate3-evidence]

# Tech tracking
tech-stack:
  added:
    - "python:3.12-slim@sha256:090ba77e... (sidecar base image, digest-pinned manifest list)"
    - "moby v27.5.1 seccomp default.json (allowlist baseline, hardened by subtraction)"
    - "curated user-code package set: numpy/pandas/scipy/sympy/matplotlib/pillow/beautifulsoup4/lxml/pyyaml/python-dateutil/openpyxl (build-time bake, D-20)"
  patterns:
    - "harden-from-moby-default-by-subtraction (never hand-author the allowlist; flatten CAP-gated groups inert under cap_drop:ALL, subtract dangerous+network)"
    - "build-time --require-hashes package bake into read-only image layers; runtime stays net-none + read_only with zero pip"
    - "arch-gated Makefile target via make-parse-time uname -m so make -n prints the overlay-included command on x86"
    - "compose overlay file (-f base -f overlay) for x86-only runtime selection, default-on through the operator make target"

key-files:
  created:
    - sandbox/sidecar.py
    - sandbox/Dockerfile
    - sandbox/requirements.txt
    - sandbox/seccomp.json
    - sandbox/seccomp.README.md
    - compose.gvisor.yaml
    - .planning/phases/05-sandbox-2a-stateless/05-02-SUMMARY.md
  modified:
    - compose.yaml
    - Makefile

key-decisions:
  - "Seccomp baseline pinned to moby tag v27.5.1 (master/main raw path 404s; a tag is more auditable than a moving branch); flattened moby's CAP-gated conditional ALLOW groups into one unconditional by-name group (inert under cap_drop:ALL) then subtracted 20 dangerous+network names. kexec_load/userfaultfd were never in moby's ALLOW set, so they stay denied by omission (defaultAction SCMP_ACT_ERRNO)."
  - "python:3.12-slim pinned by manifest-LIST digest (resolved via Docker Hub registry API, not docker CLI) so the same FROM line resolves the correct per-arch image on amd64 + arm64."
  - "requirements.txt pins the full runtime dependency closure (23 entries: 11 curated + 12 transitive) because --require-hashes refuses to install any unpinned package; multi-arch sha256 hashes cover amd64+arm64 cp312 manylinux + pure-python none-any wheels (no compiler/build-essential needed)."
  - "pids limit_hit is a best-effort stderr-text heuristic (RESEARCH OQ2); D-16 only requires the field be reported, never guessed Go-side. Tracked-refinement comment left for a precise cgroup-v2 pids.events read in 2b."
  - "make sandbox-up is arch-gated at make-PARSE time (uname -m via $(shell)), so make -n deterministically prints the overlay-included command on x86 (the verify gate greps for it) — not a runtime shell branch."

requirements-completed: []

# Metrics
duration: ~18min
completed: 2026-06-01
---

# Phase 5 Plan 02: Sidecar Artifacts + Hardening Surface Summary

**The isolation boundary the Go runner talks to: a stdlib-only Python sidecar (`/exec/python` + `/exec/shell` + `/healthz`, D-16 JSON contract), its non-root digest-pinned `python:3.12-slim` Dockerfile that bakes a curated hash-pinned package set at BUILD time with NO runtime pip (D-20), a multi-arch positive seccomp allowlist hardened from the moby v27.5.1 baseline by subtraction, the full CAP-01 SC#5 hardening floor on the `aura-sandbox` compose service, an x86-only `runsc` overlay, and a `make sandbox-up` operator default that makes gVisor default-on x86 real (D-04/SC#5).**

## Performance

- **Duration:** ~18 min
- **Completed:** 2026-06-01
- **Tasks:** 3 (all landed this run)
- **Files created:** 7 / modified: 2

## Accomplishments

- **Task 1 — sidecar image (`acb23dd`):** `sandbox/sidecar.py` is a stdlib-only `ThreadingHTTPServer` routing `POST /exec/python` -> `python3 -c`, `POST /exec/shell` -> `bash -c`, and `GET /healthz`. It honours the request `timeout_sec` (safe default 30 when missing/<=0), maps `TimeoutExpired`->exit 124 + `limit_hit:"timeout"`, rc 137 -> `"oom"`, and a best-effort stderr heuristic -> `"pids"`, truncates each stream to exactly 1 MiB, and returns the exact D-16 keys `{stdout,stderr,exit_code,elapsed_ms,truncated,limit_hit}`. Unknown path -> 404, malformed JSON -> 400. `sandbox/Dockerfile` is `python:3.12-slim` pinned by manifest-list digest, installs `bash`/`coreutils`, runs the BUILD-time `pip install --require-hashes` bake + an `import ...; print('baked-ok')` smoke that fails the build if any baked lib is missing, creates uid 65532, and does NO pip after the `USER` switch. `sandbox/requirements.txt` carries the curated 11-package set + the 12 transitive deps, all version+sha256-pinned for amd64+arm64.
- **Task 2 — seccomp allowlist (`cea5eba`):** `sandbox/seccomp.json` is a positive allowlist (`defaultAction: SCMP_ACT_ERRNO`, `defaultErrnoRet: 1`) built by flattening the moby v27.5.1 default profile's ALLOW names and subtracting the dangerous set (`ptrace`/`unshare`/`process_vm_readv`/`bpf`/`kexec_load`/`userfaultfd`/`mount`) + the network socket syscalls — 394 allowed, both `SCMP_ARCH_X86_64` + `SCMP_ARCH_AARCH64` by-name, no syscall by number. `sandbox/seccomp.README.md` records the moby tag, the subtracted sets, and the regeneration recipe.
- **Task 3 — compose + overlay + operator default (`baef9e1`):** appended the `aura-sandbox` service to `compose.yaml` with the complete CAP-01 SC#5 floor (`cap_drop:ALL`, `no-new-privileges:true`, `read_only:true`, tmpfs `/tmp`, `network_mode:none`, `pids_limit:64`, `mem_limit:512m`, `cpus:1.0`, `ulimits nofile:64`, `user 65532:65532`, `seccomp=./sandbox/seccomp.json`, loopback `127.0.0.1:18901:18901`) + a stdlib-urllib `/healthz` healthcheck. Created `compose.gvisor.yaml` as the x86-only `runtime: runsc` overlay; the base compose sets no runtime. Added the arch-gated `make sandbox-up` (registered in `.PHONY` + help) that appends `-f compose.gvisor.yaml` on x86_64 and omits it on arm64.

## Task Commits

1. **Task 1: sidecar image** — `acb23dd` (feat)
2. **Task 2: multi-arch positive seccomp allowlist** — `cea5eba` (feat)
3. **Task 3: compose aura-sandbox + gVisor overlay + make sandbox-up** — `baef9e1` (feat)

## Files Created/Modified

- `sandbox/sidecar.py` (200 LOC) — stdlib HTTP sidecar
- `sandbox/Dockerfile` (50 LOC) — digest-pinned non-root build-time bake
- `sandbox/requirements.txt` (48 LOC) — curated + transitive hash-pinned set
- `sandbox/seccomp.json` (425 LOC) — multi-arch positive allowlist
- `sandbox/seccomp.README.md` (65 LOC) — audit trail
- `compose.gvisor.yaml` (20 LOC) — x86 runsc overlay
- `compose.yaml` — appended `aura-sandbox` service (existing services untouched)
- `Makefile` — `sandbox-up` target + `.PHONY` + help line (existing targets untouched)

## Decisions Made

See the `key-decisions` frontmatter. Highlights: moby seccomp baseline pinned to tag `v27.5.1` (the `master` raw path 404s and a tag is more auditable); `python:3.12-slim` pinned by the multi-arch manifest-list digest resolved via the Docker Hub registry API (no docker CLI); `requirements.txt` pins the full 23-entry runtime closure because `--require-hashes` rejects any unpinned package; `make sandbox-up` is arch-gated at make-parse time so `make -n` deterministically prints the overlay-included command on x86.

## Deviations from Plan

**None functional — plan executed as written.** Two environment-driven adaptations, both within Claude's Discretion per CONTEXT.md:

1. **Seccomp baseline source (Task 2):** the plan's `curl .../moby/moby/master/profiles/seccomp/default.json` 404s (the path is not served from the `master`/`main` raw ref in current moby). Fetched the identical profile from the tagged release `v27.5.1` instead, which is both reachable and more auditable than a moving branch. The result is functionally the moby default-minus-dangerous allowlist the plan specifies; the README + `_comment` record the exact tag. The GitHub commits API was rate-limited (403) so the README cites the tag rather than a raw commit SHA.
2. **python:3.12-slim digest resolution (Task 1):** the plan suggests `docker buildx imagetools inspect`; Docker is unavailable in this environment, so the multi-arch manifest-list digest (`sha256:090ba77e...`) was resolved directly via the Docker Hub registry v2 API (anonymous token + `Docker-Content-Digest` header). Same digest a docker CLI would return.

## Verification

### Statically verified in this environment (Docker daemon NOT available)

- `python3 -m py_compile sandbox/sidecar.py` + `ast.parse` — PASS; both `/exec/python` + `/exec/shell` routes, `limit_hit`, and `1 << 20` truncation present.
- `json.load(sandbox/seccomp.json)` — PASS; `defaultAction == SCMP_ACT_ERRNO`, both arches present, banned set `{ptrace,unshare,process_vm_readv,bpf,kexec_load,userfaultfd,mount,socket,connect}` absent from ALLOW, core syscalls (`read`/`write`/`futex`) present, no syscall by number (394 allowed).
- `yaml.safe_load` on `compose.yaml` + `compose.gvisor.yaml` — PASS.
- Task 1/2/3 plan `<verify>` gates — all PASS (Task 3 `make -n sandbox-up` includes `-f compose.gvisor.yaml` on this x86_64 host; base `compose.yaml` contains no `runtime: runsc`).
- File-size cap (CLAUDE.md <=600 LOC): all artifacts well under (max 425 = seccomp.json data).

### DEFERRED to CI DinD (05-04) + human checkpoint — Docker-gated, CANNOT run here

The Docker daemon is not available in this execution environment (no `/var/run/docker.sock`), so the following are authored-to-spec but NOT live-verified here:

- `docker build -f sandbox/Dockerfile sandbox` — the BUILD-time `--require-hashes` pip bake + the `baked-ok` import smoke (the build will FAIL if a hash mismatches or a baked lib is unimportable; this is the live supply-chain + seccomp-fit gate, proven by 05-03 `TestRunner_BakedPackagesImport` + the CI DinD job).
- `docker compose -f compose.yaml -f compose.gvisor.yaml up -d aura-sandbox` under `runsc` — live container start, healthcheck green, and the gVisor runtime actually intercepting.
- The seccomp negative tests (`ptrace`/`socket`/`unshare`/`mount` -> EPERM) + the positive `import numpy` smoke under the live hardened runtime — 05-03 integration tier.
- The 18-scenario escape bench config-regression assertions + escape-rate — 05-04.

These deferrals are by design for this wave (artifacts are the deliverable) and are the explicit scope of plan 05-04's CI DinD workflow + the phase Gate-3 checkpoint.

## Known Stubs

None. The sidecar's `pids` `limit_hit` detection is a documented best-effort heuristic (RESEARCH OQ2 / D-16), not a stub — the field is always reported, with a tracked-refinement comment for a precise cgroup-v2 read in 2b.

## Issues Encountered

- moby `master`/`main` raw seccomp path 404 + GitHub API 403 rate-limit — resolved by fetching the profile from tag `v27.5.1` and citing the tag (see Deviations).

## User Setup Required

Per the plan `user_setup`: install + register gVisor `runsc` as a Docker runtime on the x86 production host + the CI DinD inner daemon (`runsc install` + daemon restart; `daemon.json` {`userns-remap:default`, `no-new-privileges:true`, `runtimes.runsc`}). This is host/daemon config (D-05/D-15), not Go config — the runner never drives the runtime. Validated live by the 05-04 bench, not by this wave.

## Next Phase Readiness

- The wire endpoints (`/exec/python`, `/exec/shell`, `/healthz`) + the D-16 JSON contract are fixed — the Go `DockerRunner` (05-03) can be written against them.
- The hardening floor + seccomp profile + the gVisor overlay + the operator default are all in place; 05-04 supplies the CI DinD live build/run + the escape-rate bench.
- Tracked obligation carried forward (from 05-01): QEMU-arm64 CI seccomp emulation can diverge from a real arm64 kernel -> real-DGX confirmation is a pre-production arm64 obligation.

## Self-Check: PASSED

- `sandbox/sidecar.py`, `sandbox/Dockerfile`, `sandbox/requirements.txt`, `sandbox/seccomp.json`, `sandbox/seccomp.README.md`, `compose.gvisor.yaml` — all present on disk.
- Commits `acb23dd`, `cea5eba`, `baef9e1` — all present in git log.
- `compose.yaml` contains `aura-sandbox`; `Makefile` contains `sandbox-up`.

---
*Phase: 05-sandbox-2a-stateless*
*Completed: 2026-06-01*
