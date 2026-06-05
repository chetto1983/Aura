---
phase: 11-skills
plan: 07
subsystem: skills
tags: [snippets, sandbox-agent, bearer-token, cron, ttl, docker-compose, by-path-exec, usage-sidecar]

# Dependency graph
requires:
  - phase: 11-06
    provides: skills installer + Writer pending/activate/materialize + audit store + skill ActionRouter
  - phase: 08-sandbox
    provides: sandbox-agent :2468 exec seam (tools.SandboxExec + internal/sandboxagent.Client)
  - phase: 10-scheduler
    provides: cron TaskKind handler registration + Store + Scheduler + daily-seed machinery
provides:
  - sandbox-agent bearer-token auth (--token + Authorization: Bearer on exec + Health; D-38 spike 008)
  - read-only /skills bind mount from AURA_SKILL_EXPORT_DIR (D-17 spike 005)
  - baked xlsx North-Star Python deps (openpyxl/defusedxml/lxml/validators) in the sandbox image
  - executable snippets v1 (SaveSnippet language-enum + risk-gated pending; UseSnippet by-path /skills path)
  - per-skill usage sidecar JSON (status/last_used_at/use_count, atomic-write) + StampUsage
  - skill_ttl_sweep cron TaskKind + daily seed (archives+de-materializes snippets past AURA_SKILL_SNIPPET_TTL_DAYS)
  - aura skills snippet {save|exec} CLI (deterministic by-path exec + usage stamp)
affects: [11-08, packaging, sandbox-hardening, phase-8-gvisor]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Snippet = a type:snippet skill (SKILL.md docs + <name>.<ext> code file) reusing the gated writer path"
    - "By-path exec only: interpreter + /skills/<name>/<name>.<ext> via the shipped sandbox_exec — never the exec bit (spike 005)"
    - "Usage sidecar JSON is the ONE live-state source (D-19); snippet_runs DB forensics intentionally skipped"
    - "Bearer carried on EVERY sandbox-agent request incl. health via a centralized authHeaders helper"
    - "TTL sweep is a system-seeded cron TaskKind (D-16), NOT a goroutine; consumer-declared SnippetSweeper seam"

key-files:
  created:
    - internal/skills/snippet.go
    - internal/skills/snippet_usage.go
    - internal/skills/snippet_test.go
    - internal/skills/snippet_integration_test.go
    - internal/skills/snippet_sweep_integration_test.go
    - cmd/aura/skills_snippet.go
    - internal/cron/handlers/skill_ttl.go
    - internal/cron/handlers/skill_ttl_test.go
    - internal/cron/skill_ttl_integration_test.go
  modified:
    - compose.yaml
    - docker/sandbox-agent/Dockerfile
    - internal/sandboxagent/client.go
    - internal/sandboxagent/client_test.go
    - internal/config/config.go
    - internal/skills/loader.go
    - internal/skills/writer.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_read.go
    - cmd/aura/skills.go
    - cmd/aura/serve.go
    - cmd/aura/serve_adapters.go
    - internal/cron/store.go
    - internal/cron/handlers/handler.go

key-decisions:
  - "Snippet code lives in a sibling <name>.<ext> file; SKILL.md body is a docs frame (so the model never re-enters the code into context, D-04)"
  - "skillFileBytes emits language: into the materialized SKILL.md so UseSnippet can resolve the by-path interpreter/extension"
  - "AURA_SANDBOX_AGENT_TOKEN gen at first boot + best-effort .env persist (mirrors AURA_SETUP_TOKEN intent) so compose + client share one value"
  - "TTL staleness = usage sidecar last_used_at, falling back to SKILL.md mtime for a never-used snippet"
  - "snippet_runs migration 0011 deliberately SKIPPED (D-19/A4 optional) — the sidecar is the live-state source; scope kept lean"

patterns-established:
  - "snippetSweeperAdapter / SnippetSweeper: composition-root adapter bridges Writer.SweepExpiredSnippets onto the cron handler seam (10-05 lineage)"
  - "Live sandbox_integration tests close http.DefaultTransport idle conns in t.Cleanup to satisfy the package goleak gate"

requirements-completed: [CAP-08]

# Metrics
duration: ~60min
completed: 2026-06-05
---

# Phase 11 Plan 07: Executable Snippets v1 + Sandbox Hardening Floor Summary

**7e executable snippets (save → activate → by-path exec via sandbox_exec → TTL archive) on a hardened sandbox-agent floor: bearer-token auth + a read-only /skills bind mount + baked xlsx deps, with the TTL sweep shipped as a daily-seeded cron TaskKind.**

## Performance

- **Duration:** ~60 min
- **Started:** 2026-06-05T16:13Z (approx)
- **Completed:** 2026-06-05T17:13Z
- **Tasks:** 3
- **Files modified:** 23 (9 created, 14 modified)

## Accomplishments
- **D-38 portable hardening floor closed:** the sandbox-agent now runs `--token`; `internal/sandboxagent.Client` sends `Authorization: Bearer` on every request (exec + a new `Health` probe), a 401 surfaces a clear auth-failed error, and the compose healthcheck carries the bearer. **Verified live** on the real container: no-auth → 401, good-auth → 200.
- **D-17 ro /skills mount:** compose adds the first stack bind mount (`${AURA_SKILL_EXPORT_DIR}:/skills:ro`); confirmed in the resolved compose config (`target: /skills`, `read_only: true`) and on the live container.
- **Snippets v1 (D-04/D-20):** `SaveSnippet` validates the language enum (python|shell|js) + the write-boundary blocklist on the CODE, writes SKILL.md + `<name>.<ext>` atomically into pending, gates RISKY, surfaces needs_network, and never self-activates. `action=use` / `UseSnippet` return instructions + the stable `/skills/<name>/<name>.<ext>` path + interpreter; the model execs BY PATH via the shipped `sandbox_exec`.
- **Usage sidecar (D-19):** per-skill `.usage.json` (status/last_used_at/use_count) atomic temp+rename; `StampUsage` bump; `aura skills snippet exec` runs by-path AND stamps.
- **TTL sweep (D-16):** `skill_ttl_sweep` cron TaskKind + daily 03:00 seed (idempotent); `Writer.SweepExpiredSnippets` archives+de-materializes snippets past `AURA_SKILL_SNIPPET_TTL_DAYS` (default 90) with the D-29 `auto` audit row.
- **SC#4 proven LIVE:** the `sandbox_integration db_integration` `TestSnippetExec` RAN against the real stack — save → activate → materialize into the live /skills mount → `python3 /skills/<name>/<name>.py` → marker captured → usage stamped. The db_integration `TestSkillTTLSweep` + `TestSkillTTLSeed` also RAN green (A2 kind-CHECK landmine confirmed closed).

## Task Commits

Each task was committed atomically:

1. **Task 1: compose token + ro /skills mount + sandboxagent Bearer + baked deps** - `0272a973` (feat)
2. **Task 2: snippet save/use + usage sidecar + by-path exec + CLI** - `f315163b` (feat)
3. **Task 3: skill_ttl_sweep cron TaskKind + daily seed** - `7ccaa43e` (feat)

## Files Created/Modified
- `compose.yaml` - `--token` (was `--no-token`), `/skills:ro` bind mount, bearer-carrying healthcheck
- `docker/sandbox-agent/Dockerfile` - pinned `pip install openpyxl/defusedxml/lxml/validators` (offline xlsx North-Star, spike 007)
- `internal/sandboxagent/client.go` - `Config.Token` → Bearer on exec + new `Health`; 401 → clear auth error
- `internal/config/config.go` - `AURA_SANDBOX_AGENT_TOKEN` first-boot gen+.env persist; `AURA_SKILL_SNIPPET_TTL_DAYS`
- `internal/skills/snippet.go` - SaveSnippet/UseSnippet/SnippetInvocation + language enum + by-path path helpers
- `internal/skills/snippet_usage.go` - usage sidecar (atomic write/StampUsage/SetUsageStatus) + SweepExpiredSnippets
- `internal/skills/loader.go` / `writer.go` - Skill.Language; skillFileBytes emits `language:` for snippets
- `internal/agent/tools/skill.go` / `skill_read.go` - skillLoader.Snippet seam; action=use returns the by-path frame for snippets
- `cmd/aura/skills_snippet.go` - `aura skills snippet {save|exec}` (exec runs by-path + stamps usage)
- `internal/cron/handlers/skill_ttl.go` - SkillTTLSweepHandler + SnippetSweeper seam
- `cmd/aura/serve.go` / `serve_adapters.go` - handler registration + idempotent daily seed + snippetSweeperAdapter

## Decisions Made
- Snippet code lives in a sibling `<name>.<ext>` file (not the SKILL.md body) so by-path exec works and the code never re-enters context (D-04).
- `skillFileBytes` emits `language:` into the materialized SKILL.md (load-bearing for UseSnippet's interpreter/extension resolution).
- TTL staleness uses the usage sidecar `last_used_at`, falling back to SKILL.md mtime for a never-used snippet.
- Migration `0011_snippet_runs` deliberately SKIPPED (D-19/A4 optional/discretion) — the sidecar JSON is the live-state source; lean scope.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added `language:` emission to the writer's SKILL.md renderer**
- **Found during:** Task 2 (snippet save/use)
- **Issue:** `skillFileBytes` (the writer's SKILL.md renderer) did not emit the `language:` frontmatter, so a materialized snippet's active SKILL.md lacked the language field UseSnippet needs to resolve the by-path interpreter/extension — `UseSnippet` would have failed on every saved snippet.
- **Fix:** `skillFileBytes` now emits `language: <lang>` when `type == snippet`; the loader carries `Skill.Language`.
- **Files modified:** internal/skills/writer.go, internal/skills/loader.go
- **Verification:** Unit `TestUseSnippetReturnsPath` + the live `TestSnippetExec` both resolve the path from the round-tripped SKILL.md.
- **Committed in:** f315163b (Task 2 commit)

**2. [Rule 2 - Missing Critical] Added `AURA_SKILL_SNIPPET_TTL_DAYS` to config**
- **Found during:** Task 3 (TTL sweep)
- **Issue:** The TTL knob (D-34, default 90) existed only in planning docs — config.go had no field, so the daily sweep had no threshold to read.
- **Fix:** Added `SkillSnippetTTLDays` (`AURA_SKILL_SNIPPET_TTL_DAYS`, default 90) to config; serve.go feeds it as the handler TTL.
- **Files modified:** internal/config/config.go, cmd/aura/serve.go
- **Verification:** `go test ./internal/config/` green; the live `TestSkillTTLSweep` exercises a 90d TTL.
- **Committed in:** 7ccaa43e (Task 3 commit)

**3. [Rule 1 - Bug] Added daily-seed code to serve.go (the plan's "serve.go:109-110 backup daily-seed" did not exist)**
- **Found during:** Task 3 (daily seed)
- **Issue:** The plan's read_first referenced an existing "backup daily-seed at serve.go:109-110"; in fact serve.go only registered handlers — there was NO daily-seed code to mirror. Without adding one, the TTL sweep TaskKind would never be seeded and would never run.
- **Fix:** Wrote `seedSkillTTLSweep` (idempotent: scans ListActiveTasks, inserts a daily 03:00 cron task only if absent) and call it in bootServe.
- **Files modified:** cmd/aura/serve.go
- **Verification:** `TestSkillTTLSeed` (live db_integration) confirms the row INSERTs against the 0010-widened kind CHECK.
- **Committed in:** 7ccaa43e (Task 3 commit)

**4. [Rule 3 - Blocking] Closed http.DefaultTransport idle conns in the live snippet test (goleak)**
- **Found during:** Task 2 (live TestSnippetExec)
- **Issue:** The first live run PASSED the assertions but FAILED the package goleak gate — the sandboxagent client's keep-alive readLoop/writeLoop goroutines outlived the request.
- **Fix:** `t.Cleanup` closes `http.DefaultTransport` idle connections (mirrors the tools live-sandbox test pattern).
- **Files modified:** internal/skills/snippet_integration_test.go
- **Verification:** Re-ran live — `--- PASS: TestSnippetExec`, no goleak.
- **Committed in:** f315163b (Task 2 commit)

**5. [Rule 3 - Blocking] Renamed test helper to avoid a `contains` redeclaration under db_integration**
- **Found during:** Task 3 (db_integration build)
- **Issue:** `catalog_test.go` already declares `contains(s, sub string)`; my sweep test's `contains([]string,string)` collided under the db_integration tag.
- **Fix:** Renamed the sweep helper to `containsName`.
- **Files modified:** internal/skills/snippet_sweep_integration_test.go
- **Verification:** `go test -tags db_integration` compiles + the test ran green.
- **Committed in:** 7ccaa43e (Task 3 commit)

**6. [Rule 3 - Refactor on touch] Split usage sidecar + sweep into snippet_usage.go**
- **Found during:** Task 3
- **Issue:** snippet.go reached 491 LOC and mixed two concerns (save/use vs usage/sweep).
- **Fix:** Moved the usage sidecar + TTL sweep into `internal/skills/snippet_usage.go` (294/205 LOC split).
- **Files modified:** internal/skills/snippet.go, internal/skills/snippet_usage.go
- **Verification:** `go test -race ./internal/skills/` green; both files ≤600 LOC.
- **Committed in:** 7ccaa43e (Task 3 commit)

---

**Total deviations:** 6 auto-fixed (2 missing-critical, 1 bug, 3 blocking/refactor)
**Impact on plan:** All auto-fixes were necessary for correctness (the snippet runtime literally could not resolve a snippet without the `language:` emission + the TTL knob + the seed). No scope creep. Deviation #3 corrected a factual error in the plan's read_first (no pre-existing backup daily-seed existed).

## Issues Encountered
- The currently-running shared sandbox-agent container predated this change (`--no-token`, no /skills mount). To run SC#4 live I recreated ONLY the `aura-sandbox-agent` service (`docker compose up -d --force-recreate --no-deps`) with a generated token + the export-dir mount; the rest of the shared stack (postgres/neo4j) was left untouched. The container is healthy with the bearer-carrying healthcheck.
- The `image: aura-sandbox-agent:py3` was NOT rebuilt, so the live container does not yet carry the newly-baked openpyxl/defusedxml/lxml/validators deps — `TestSnippetExec` only needs `python3` (present). The baked-deps acceptance (`docker exec ... python3 -c "import openpyxl,..."`) requires a `docker compose build aura-sandbox-agent` and is a deferred verification (the Dockerfile change is static-validated; see Next Phase Readiness).

## Threat Flags
None — no new trust-boundary surface beyond the plan's threat model (the bearer + ro mount + by-path exec are the planned mitigations T-11-07-E1/T1/T2/D1/R1).

## Known Stubs
None. The `notYetWired` restore/archive router keys are pre-existing reserved D-01 keys, out of this plan's scope.

## Next Phase Readiness
- **11-08 (xlsx North-Star E2E) is unblocked:** the portable hardening floor (token + ro /skills mount) + the snippet runtime + the daily TTL sweep are all in place.
- **Deferred verification for 11-08:** rebuild the sandbox image (`docker compose build aura-sandbox-agent`) so the baked openpyxl/defusedxml/lxml/validators deps are present — required for the E2E to be offline-deterministic on native-Linux (spike 007). The currently-running container has python3 but not the baked deps.
- **Phase-8 dependency (NOT this plan):** gVisor `runsc` overlay + seccomp re-tightening remain a Phase-8 sandbox-wide regression (D-38 scope note); Phase 11 depends only on the portable floor delivered here.
- **Self-Check: PASSED** (all 9 created files present; all 3 task commits present in git log).

---
*Phase: 11-skills*
*Completed: 2026-06-05*
