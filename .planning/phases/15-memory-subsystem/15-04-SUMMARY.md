---
phase: 15-memory-subsystem
plan: 04
subsystem: infra
tags: [docker, compose, agent-memory-mcp, neo4j-agent-memory, supply-chain, reproducible-build, sidecar]

# Dependency graph
requires:
  - phase: 15-01
    provides: "PRD amendment #62 + AURA_AGENT_MEMORY_MCP_IMAGE env catalog (default :local) — the re-scoped contract this build serves"
provides:
  - "docker/agent-memory/Dockerfile — reproducible pinned-fork pip-install image for the agent-memory MCP sidecar (python:3.11-slim + pip install -e .[mcp,google,openai] from vendored src/)"
  - "docker/agent-memory/ vendored fork at c1c2d65 (pyproject.toml v0.5.0, README.md, README-pypi.md, src/ — 135 files)"
  - "compose.yaml aura-agent-memory-mcp build:/image: ...:local/pull_policy: never stanza replacing the hand-built :spike-fixed image"
affects: [15-05, phase-17-packaging, aura-quality-snapshot]

# Tech tracking
tech-stack:
  added: ["vendored neo4j-agent-memory v0.5.0 fork (branch aura/provenance-safe-dedup @ c1c2d65)"]
  patterns: ["pinned-vendored-fork reproducible compose build (markitdown shape + cloudrun Dockerfile)"]

key-files:
  created:
    - docker/agent-memory/Dockerfile
    - docker/agent-memory/.dockerignore
    - docker/agent-memory/pyproject.toml
    - docker/agent-memory/README.md
    - docker/agent-memory/README-pypi.md
    - docker/agent-memory/src/ (vendored fork, 135 files)
  modified:
    - compose.yaml

key-decisions:
  - "Vendor the fork src/ via git archive at c1c2d65 (not a submodule, not PyPI) — reproducible offline build, Pitfall-5 safe"
  - "Image carries no CMD: the compose command: drives the full mcp serve invocation; only the neo4j-agent-memory entry point must be installed"
  - "Default image tag changes :spike-fixed → :local (markitdown convention)"

patterns-established:
  - "Pinned-vendored-fork reproducible build: COPY pyproject.toml + README*.md + src/ then pip install -e .[extras], pinned to a commit recorded in a top-of-Dockerfile comment with the Pitfall-5 reason"

requirements-completed: [UX-08]

# Metrics
duration: 7min
completed: 2026-06-12
---

# Phase 15 Plan 04: Reproducible agent-memory MCP sidecar build Summary

**Reproducible `docker compose build aura-agent-memory-mcp` from a vendored `neo4j-agent-memory` fork pinned at `c1c2d65`, replacing the hand-built `:spike-fixed` image with a `pip install -e ".[mcp,google,openai]"` Dockerfile (markitdown shape) — live build verified (image built, `neo4j-agent-memory` CLI present).**

## Performance

- **Duration:** 7 min
- **Started:** 2026-06-12T06:53:46Z
- **Completed:** 2026-06-12T07:00:16Z
- **Tasks:** 2
- **Files modified:** 141 (140 created under docker/agent-memory/ + 1 compose.yaml edit)

## Accomplishments
- Vendored the `aura/provenance-safe-dedup` fork at the pinned commit `c1c2d65` (v0.5.0) into `docker/agent-memory/` via `git archive` — `pyproject.toml`, `README.md`, `README-pypi.md`, and the full `src/` tree (135 files, tree count matches the fork exactly). No network git fetch at build time (RESEARCH A2).
- Wrote `docker/agent-memory/Dockerfile` (`python:3.11-slim` + `gcc` + `pip install --no-cache-dir -e ".[mcp,google,openai]"` from vendored `src/`), modeled on `docker/markitdown/Dockerfile` and the fork's `deploy/cloudrun/Dockerfile`. No CMD — the compose `command:` already drives `neo4j-agent-memory mcp serve …`.
- Top-of-Dockerfile comment records the pinned commit `c1c2d65`, the branch, and **why PyPI must NOT be used** (Pitfall 5 — the `_deduplication_scope` provenance-safe-dedup fix is fork-internal; a PyPI install drops it and cross-run over-merge returns).
- Added `.dockerignore` trimming `tests/`/`.git`/`__pycache__`/`*.pyc` (T-15-04-02 — minimal supply-chain surface).
- Swapped `compose.yaml`'s single `image: …:spike-fixed` line on `aura-agent-memory-mcp` for the markitdown-shaped `build.context: ./docker/agent-memory` + `image: …:local` + `pull_policy: never` triple, with all other service config (environment/command/healthcheck/ports/depends_on) byte-for-byte unchanged.
- **Live-verified the build**: `docker compose build aura-agent-memory-mcp` (distinct throwaway tag, `:spike-fixed` preserved) installed `neo4j-agent-memory-0.5.0` + the `[mcp,google,openai]` extras; the `neo4j-agent-memory` CLI is present at `/usr/local/bin/neo4j-agent-memory` and runs. Throwaway image removed afterward.

## Task Commits

Each task was committed atomically:

1. **Task 1: Vendor the fork at c1c2d65 + write docker/agent-memory/Dockerfile** - `52efa92c` (feat)
2. **Task 2: Swap compose.yaml `image:` for a reproducible `build:` stanza** - `95acc0f8` (feat)

## Files Created/Modified
- `docker/agent-memory/Dockerfile` - reproducible pinned-fork pip-install image (no CMD; compose drives serve)
- `docker/agent-memory/.dockerignore` - trims tests/.git/__pycache__ from the build context
- `docker/agent-memory/pyproject.toml` - vendored fork metadata (v0.5.0, requires-python >=3.10)
- `docker/agent-memory/README.md`, `docker/agent-memory/README-pypi.md` - referenced by the install (`COPY … README*.md`)
- `docker/agent-memory/src/neo4j_agent_memory/**` - vendored fork source (135 files; includes the `_deduplication_scope` fix in `memory/long_term.py`)
- `compose.yaml` - `aura-agent-memory-mcp` build:/image: …:local/pull_policy: never (image line only; all else unchanged)

## Decisions Made
- **Vendoring over submodule** (RESEARCH A2): `git archive c1c2d65` of the four paths is simpler for Phase-17 packaging and needs no network fetch at build. The vendored tree's file count (135) matches the fork's `git ls-tree -r c1c2d65 src/`.
- **No CMD in the Dockerfile**: the compose `command:` already invokes the full `neo4j-agent-memory mcp serve --profile extended --session-strategy persistent --user-id aura-local --embedding-dimensions 384 --no-auto-preferences`; the image only needs the entry point installed (confirmed present in the built image).
- **Default tag `:spike-fixed` → `:local`** to match the markitdown `:local` convention and 15-01's `AURA_AGENT_MEMORY_MCP_IMAGE` default.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' automated verify checks passed on the first attempt; no Rule 1–4 deviations, no auth gates, no checkpoints. (Beyond the plan's required `docker compose config` parse, the executor additionally ran a real `docker compose build` to a throwaway tag to confirm the Dockerfile builds end-to-end from git — a confirmation, not a change.)

## Issues Encountered
- `docker` in Git Bash resolves to a spaced Windows path (`C:/Program Files/Docker/Docker/resources/bin/docker`); the bare invocation under the w64devkit `sh` failed with `can't open … docker`. Resolved by invoking `docker.exe` via its explicit quoted path and using `docker compose config --quiet` (no redirect-to-file).
- `docker compose config` requires the `:?`-guarded `NEO4J_PASSWORD`/`POSTGRES_PASSWORD` to be set (pre-existing guards, unrelated to this change). Sourcing the main-repo `.env` (single-quoted values stripped per the env-loading gotcha) rendered the full file → `rc=0`. The memory service's `pull_policy: never` + `:local` default rendered correctly.

## Threat Surface Notes
- T-15-04-SC (supply chain): mitigated — image installs from the **vendored `src/` pinned at `c1c2d65`**, not a registry name resolution. The build's transitive pip resolution (`neo4j-6.2.0`, `fastmcp-2.14.7`, `openai-2.41.1`, `google-cloud-aiplatform-1.157.0`, …) happens inside the image; the top-level source is pinned to a known commit.
- T-15-04-01 (losing the dedup fix): mitigated — the vendored `memory/long_term.py` contains `_deduplication_scope` (the `c1c2d65` fix); 15-05's integration tier re-asserts `action=none` for a new entity against the rebuilt image.
- T-15-04-02 (secrets baked in): accepted — no secret is COPYed; `.dockerignore` excludes `.git`/tests; `NAM_LLM_API_KEY`/`NEO4J_PASSWORD` arrive at runtime via compose env.

No new threat surface beyond the plan's `<threat_model>` was introduced.

## Known Stubs
None — this plan ships Docker/compose config only; no Go code, no data sources, no placeholders.

## Next Phase Readiness
- The memory sidecar image is reproducible from git: `docker compose build aura-agent-memory-mcp` builds `:local` (live-verified). Plan 15-05's `memory_integration` tier consumes this build (live `tools/list == 16`, dedup `action=none`).
- Load-bearing for Phase 17 packaging (the sidecar is now rebuildable from the repo, no hand-built artifact).
- No blockers. The existing `:spike-fixed` image remains on the host until 15-05 builds `:local`; nothing depends on it being removed.

## Self-Check: PASSED

- `docker/agent-memory/Dockerfile`, `.dockerignore`, `pyproject.toml`, `README.md`, `README-pypi.md`, `src/`, `compose.yaml`, and `15-04-SUMMARY.md` all exist.
- Commits `52efa92c` (Task 1) and `95acc0f8` (Task 2) present in `git log`.
- Live build confirmation: `docker compose build aura-agent-memory-mcp` produced a working image (`neo4j-agent-memory-0.5.0` installed, CLI present); `:spike-fixed` preserved; throwaway tag removed.

---
*Phase: 15-memory-subsystem*
*Completed: 2026-06-12*
