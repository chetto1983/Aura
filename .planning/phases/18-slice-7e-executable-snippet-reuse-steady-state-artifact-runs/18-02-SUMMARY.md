---
phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs
plan: 02
subsystem: skills
tags: [skills, snippet, host-primary, shell_exec, d-01, tool-seam, by-path]

# Dependency graph
requires:
  - phase: 18 (Wave 1 / 18-01)
    provides: PRD amendment #55 (host-primary snippet posture, D-01) + tool_invocations ledger
  - phase: 11 (Slice 7e-core)
    provides: snippet store (SaveSnippet/UseSnippet/SnippetSandboxPath), the skill action-enum tool, host shell_exec surface
provides:
  - skills.SnippetHostPath + skills.SnippetHostInvocation — host export-dir by-path resolvers (mirror of SnippetSandboxPath/SnippetInvocation)
  - skills.SnippetUse.HostPath — host-primary by-path target on the existing struct (SandboxPath preserved for escalation)
  - renderSnippetUse host shell_exec by-path frame (action=use hands a HOST frame, sandbox_exec named as escalation only)
  - skillLoader.Snippet seam widened to return the HOST path (param sandboxPath -> hostPath, signature shape unchanged)
  - skillLoaderAdapter.exportDir wiring from cfg.SkillExportDir
affects: [18-03, 18-04, slice-7e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "host-primary by-path frame: action=use emits a host shell_exec command line (interpreter + host path) as the primary instruction, sandbox_exec named as the deliberate escalation"
    - "use-time path derivation (RESEARCH option b): the host path is resolved fresh at UseSnippet/seam time from the Writer/cfg export dir, so already-materialized snippets need no re-render on a posture change"

key-files:
  created:
    - internal/agent/tools/skill_read_test.go
  modified:
    - internal/skills/snippet.go
    - internal/skills/snippet_test.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_read.go
    - internal/agent/tools/skill_test.go
    - cmd/aura/serve_adapters.go

key-decisions:
  - "Added SnippetHostInvocation (host mirror of SnippetInvocation) so the cmd/aura adapter resolves host path + interpreter in ONE validated call, instead of inventing exported wrappers around the unexported validSnippetLanguage/snippetMeta — keeps validation in one place, no API surface bloat"
  - "renderSnippetDocs softened to a GENERIC by-path frame (no baked execution-tier path) per RESEARCH option b — the concrete host vs sandbox invocation is computed at use-time by the tool layer, so the materialized SKILL.md body is posture-agnostic"
  - "SandboxPath kept populated on SnippetUse and SnippetInvocation/SnippetSandboxPath kept intact — they remain the named sandbox_exec escalation path (still used by spike 012a + its dedicated test), NOT dead code"

patterns-established:
  - "Load-bearing-literal contract flip: when the model-facing frame literal changes because the POSTURE changed (D-01), update the asserting test IN THE SAME COMMIT and justify it as a contract change in the commit body (NEVER a test fudged to pass)"

requirements-completed: [CAP-08.1]

# Metrics
duration: ~25min
completed: 2026-06-06
---

# Phase 18 Plan 02: Host-primary snippet `action=use` frame Summary

**Flipped the snippet `action=use` execution frame from a `sandbox_exec` in-container by-path call to a HOST `shell_exec` by-path call (#55/D-01): an approved snippet now resolves to `<interpreter> <export-dir-path>` run through the host terminal the production loop + D-35 eval gate actually use, with `sandbox_exec` demoted to the named untrusted-code escalation. The host path is derived fresh at use-time so already-materialized snippets need no re-render.**

## Performance

- **Duration:** ~25 min wall
- **Completed:** 2026-06-06
- **Tasks:** 2/2 (both TDD: RED -> GREEN)
- **Files modified:** 7 (1 created)

## Accomplishments

- **SnippetHostPath** (`snippet.go`): host export-dir by-path resolver, an EXACT mirror of `SnippetSandboxPath` (same `snippetMetaByLang` ext map, same `ErrInvalidStructure` for an unknown language) but rooted at `AURA_SKILL_EXPORT_DIR` via `filepath.Join` (OS-correct separators). `SnippetUse` gained a `HostPath` field; `UseSnippet` now sets it fresh at use-time from `w.exportDir` while keeping `SandboxPath` populated for the escalation.
- **SnippetHostInvocation** (`snippet.go`): host mirror of `SnippetInvocation` — validates the (aliased) language once and returns `(hostPath, interpreter)`, the single call the cmd/aura adapter uses.
- **renderSnippetUse** (`skill_read.go`) rewritten to a HOST `shell_exec` by-path frame: the PRIMARY instruction is `Run this stored snippet by path with shell_exec: command="<interpreter> <hostPath>"`, with `sandbox_exec` named only as the secondary escalation (mirroring `shell_exec.go`'s own Description wording).
- **skillLoader.Snippet seam** (`skill.go`) widened: param `sandboxPath` -> `hostPath`, doc comment flipped to host-primary (dropped the "/skills mount" wording); signature shape unchanged (4 returns).
- **skillLoaderAdapter** (`serve_adapters.go`): new `exportDir` field set from `cfg.SkillExportDir` in `newSkillTool`; `Snippet` now resolves the HOST path via `skills.SnippetHostInvocation` against the SAME export dir the Writer materializes into, so the host path points at the real materialized file.
- **renderSnippetDocs** softened to a generic by-path frame (no baked execution-tier path) so the materialized SKILL.md body is posture-agnostic.

## Task Commits

1. **Task 1: SnippetHostPath + HostPath on UseSnippet (TDD)** - `b970916a` (feat)
2. **Task 2: host shell_exec use frame + widened seam + adapter wiring (TDD)** - `5bb4f4c8` (feat)

## Files Created/Modified
- `internal/skills/snippet.go` — `SnippetHostPath`, `SnippetHostInvocation`, `SnippetUse.HostPath`, use-time host-path resolution in `UseSnippet`, generic `renderSnippetDocs`
- `internal/skills/snippet_test.go` — new `TestSnippetHostPath`; `TestUseSnippetReturnsPath` flipped to assert the HOST path (sandbox path still asserted for escalation)
- `internal/agent/tools/skill.go` — `Snippet` seam comment + param rename to host-primary
- `internal/agent/tools/skill_read.go` — `renderSnippetUse` host frame + `actionUse` passes the host path
- `internal/agent/tools/skill_read_test.go` (new) — `TestRenderSnippetUseHostFrame` + `TestActionUseSnippetEmitsHostFrame` asserting shell_exec is the primary verb and sandbox_exec the escalation
- `internal/agent/tools/skill_test.go` — `fakeSnippet`/`fakeSkillLoader.Snippet` updated to the `hostPath` shape
- `cmd/aura/serve_adapters.go` — `skillLoaderAdapter.exportDir` field + wiring + host-path `Snippet` resolution

## Decisions Made
- Added `SnippetHostInvocation` (host mirror of `SnippetInvocation`) so the adapter resolves host path + interpreter in ONE validated call rather than exporting wrappers around the unexported `validSnippetLanguage`/`snippetMeta.interpreter`.
- `renderSnippetDocs` kept generic (no baked path) per RESEARCH option b — posture is computed at use-time, not in the SKILL.md body.
- `SandboxPath` / `SnippetInvocation` / `SnippetSandboxPath` preserved as the named sandbox_exec escalation path (still referenced by spike 012a + its test) — not dead code.

## Deviations from Plan

### 1. `skill_read_test.go` did not pre-exist — created it
- **Found during:** Task 2 read_first
- **Issue:** The plan's `read_first` and `files` referenced `internal/agent/tools/skill_read_test.go` as the home of the load-bearing snippet-frame assertion, but no such file existed; the prior snippet-`use` coverage lived only in `skill_test.go`'s `TestSkillReadActions` (which exercised instruction-skill use, not the snippet frame).
- **Fix:** Created `skill_read_test.go` with the two host-frame assertions (`TestRenderSnippetUseHostFrame`, `TestActionUseSnippetEmitsHostFrame`); updated the `fakeSnippet`/`fakeSkillLoader.Snippet` shape in `skill_test.go` to the widened `hostPath` seam. No production contract change — the test home is the only difference.

### 2. Load-bearing-literal flips (expected, per plan)
- **Found during:** Tasks 1 + 2 (TDD RED->GREEN)
- **Issue:** `TestUseSnippetReturnsPath` asserted the sandbox literal `/skills/calc/calc.py` as the primary; `renderSnippetUse` emitted a `sandbox_exec`-primary frame.
- **Fix:** Both flipped to the HOST contract in the same commit as the production change, justified in each commit body as the D-01/#55 posture change (NOT a test fudged to pass). The sandbox literal is retained as a secondary assertion (escalation path preserved).

**Impact on plan:** deviation 1 is a test-location detail; deviation 2 is the planned, expected load-bearing-literal contract flip. No scope creep.

## Verification Evidence
- `go test ./internal/skills/ ./internal/agent/tools/` — green (skills 0.47s, tools 4.27s)
- `go test -race ./internal/skills/ ./internal/agent/tools/` — green (skills 1.92s, tools 5.52s)
- `go vet ./internal/agent/tools/ ./internal/skills/ ./cmd/aura/` — clean
- `go build ./...` + `go build ./cmd/aura/` — exit 0 (builds alongside the parallel-session `cmd/aura/toolpipe.go` + `main.go`, untouched)
- `go list -deps ./internal/agent/tools | grep -c internal/skills` — **0** (tools↔skills boundary held)
- `bash scripts/cache_invariant_audit.sh` — green: 22 identical messages[0] / messages[1] / skill-manifest-in-Description hashes (the use frame is a per-turn RESULT literal, not the cached prefix — invariant holds)
- All touched files ≤600 LOC (snippet.go 335, skill_read.go 155, skill.go 184, serve_adapters.go 381)

## Threat Surface
No new security-relevant surface beyond the plan's `<threat_model>`. T-18-05-T (crafted name → traversal) mitigation holds: `UseSnippet` calls `SanitizeName(name, name)` BEFORE any `filepath.Join`; `SnippetHostInvocation` validates the language enum before joining. T-18-04-E (host frame runs an approved snippet) accepted per the single-operator host posture (#50/D-15c).

## Known Stubs
None — the host path is wired to live config (`cfg.SkillExportDir`) and resolves a materialized file; no placeholder/empty-value flows.

## Next Phase Readiness
- 18-03 (restore/archive ActionRouter fill-in) unblocked: the host-primary `use` frame is in place; the reserved `restore`/`archive` keys still return "not yet available".
- 18-04 (steady-state E2E gate) consumes the host frame: a snippet `use` now resolves against the host terminal the gate measures from the ledger (end-event count ≤6 + wall <40s, per 18-01's gate-metric correction).

## Self-Check: PASSED
- Created files verified present: `internal/agent/tools/skill_read_test.go`, `18-02-SUMMARY.md`; modified files present.
- Task commits verified in git log: `b970916a` (Task 1), `5bb4f4c8` (Task 2).
