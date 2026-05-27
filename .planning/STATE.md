---
milestone: v4.1
milestone_name: Codebase Cleanup (Phase-CLEAN)
status: planning
last_updated: 2026-05-27
progress:
  phases_total: 7
  phases_completed: 0
  requirements_total: 44
  requirements_completed: 0
---

# STATE

## Project Reference

See: `.planning/PROJECT.md` (updated 2026-05-27)

**Core value:** The wiki is the graph — write once, retrievable forever, owned by the user.
**Current focus:** v4.1 Codebase Cleanup — eliminate the lint backlog accumulated across 2 weeks of GSD-free shipping; flip the CI hard-gate on.

## Current Position

Phase: Not started (Phase 1 — Wave 0 Baselines next)
Plan: —
Status: Defining requirements complete; roadmap drafted; awaiting `/gsd:plan-phase 1`.
Last activity: 2026-05-27 — Milestone v4.1 bootstrapped (rescan-grounded after 2026-05-12 `.planning/` wipe).

## Accumulated Context

### Decisions (carried in from CLAUDE.md + memory + source plan §0)

- Errcheck handling style locked: `_ = closer()` for defer Close, `slog.Warn` when failure is observable, never silently drop business errors.
- Cross-package dupl helper placement: callee package the depguard boundary allows, or a new `internal/<domain>util/` substrate package.
- Test cleanup is **mainline** (Wave 5 retained, runs after Wave 6 hard-gate so fixture extractions are CI-protected).
- CI ratcheting: Wave 0 adds **warning** steps, Wave 6 promotes to **fail**; never break CI mid-sweep.
- Per-slice verify gate: `go vet ./... && go build ./... && go test ./internal/<touched-pkg>/... && golangci-lint run <touched files> && dupl -t 60 <touched files>`.
- 1 story = 1 commit (Wave 5 mechanical sweeps explicitly allowed exception).
- Deep-refactor-on-touch: any file edited gets fully cleaned in the same commit (dead code, dupl, file-size, stale comments).
- Never modify tests to make lint pass; fix the production code path the test exercises.

### Blockers

- None known. Source plan is fully scoped and Codex-ready.
- ⚠️ `cmd/probe_chat/cases.go` at 1511 LOC (CONCERNS C-1) — out of scope for v4.1, may need its own follow-on milestone.
- ⚠️ SQLite WAL Windows-bind-mount corruption (CONCERNS C-2) — out of scope for v4.1; documented in code via `journal_mode=DELETE` env override, no code-side guard.

### Pending Todos

- None tracked here. `.planning/todos/` does not exist yet for v4.1.

---
*Last updated: 2026-05-27 after milestone bootstrap.*
