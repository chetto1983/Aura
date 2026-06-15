---
phase: 11-skills
plan: 02
subsystem: skills
tags: [skills, slice-7a, loader, frontmatter, goccy-yaml, nfkc, bm25, manifest, action-router, tool-schema]

requires:
  - phase: 11-01
    provides: "Amended prd.md §Slice 7 truth-source (D-01 ONE skill tool, D-06 manifest-in-Description, D-09 BM25 overflow, D-28 no-blocklist-on-load, D-30 frontmatter tolerance, D-31 skill-creator builtin, D-34 env catalog)"
  - phase: 10-05
    provides: "ActionRouter + TaskTool copy-from template; consumer-declared taskStore seam; newTaskTool composition-root adapter pattern"
  - phase: 08.1-03
    provides: "tools/bm25.go in-process Okapi BM25 ranker (reused for the skill list overflow query)"
  - phase: 03
    provides: "byte-stable SystemPrompt mechanism-not-enumeration discipline; tools.NewResult preview+sidecar; Spec/Deferred/Registry.Validate"
provides:
  - "Greenfield internal/skills package: multi-root TTL-cached Loader, goccy/go-yaml frontmatter parse, manifest renderer, embedded skill-creator builtin"
  - "ONE non-deferred skill tool (tools.SkillTool) with read actions list|info|use and reserved write/install actions; OpenAI-wire-safe schema"
  - "Manifest-in-Description (turn-stable, alphabetical, BM25-overflow tail) + ONE frozen messages[0] mechanism sentence"
  - "consumer-declared skillLoader seam (tools package has NO internal/skills import) + skillLoaderAdapter at the cmd/aura composition root"
  - "D-34 AURA_SKILL_* config knobs (SkillsDir/BodyCap/ManifestCap/ExportDir)"
affects: [11-03, 11-04, 11-05, "skills-write-boundary", "skill-install", "skill-snippets"]

tech-stack:
  added:
    - "github.com/goccy/go-yaml v1.19.2 (direct) — SKILL.md frontmatter parse (D-30)"
    - "golang.org/x/text v0.37.0 (promoted indirect→direct) — NFKC identity fold (D-27)"
  patterns:
    - "Read-half-first interface-ordering: loader seam + tool schema defined here, write/install/snippet handlers reserved for downstream"
    - "Parse-only validation on the load path (D-28): structure + name-regex + body-cap, NO blocklist (that is a write-boundary concern)"
    - "Manifest-in-tool-Description computed from a loader snapshot at build time (turn-stable, busts the prefix cache ONCE on add/remove)"

key-files:
  created:
    - internal/skills/frontmatter.go
    - internal/skills/loader.go
    - internal/skills/manifest.go
    - internal/skills/builtin.go
    - internal/skills/embed/skill-creator/SKILL.md
    - internal/skills/{main,frontmatter,loader,manifest,builtin}_test.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_read.go
    - internal/agent/tools/skill_test.go
  modified:
    - internal/agent/prompt.go
    - internal/config/config.go
    - cmd/aura/main.go
    - cmd/aura/serve_adapters.go
    - go.mod
    - go.sum

key-decisions:
  - "NFKC fold lives on the READ path (parseFrontmatter normalizes Name+Description) — gives golang.org/x/text a meaningful direct-dep use now, and pre-canonicalizes to the same form the write-boundary blocklist (11-04) will check"
  - "Loader is lazy-on-read with a mutex-guarded snapshot + last-scan timestamp; NO background goroutine, so goleak stays clean"
  - "The skill `list` overflow ranker reuses tools/bm25.go by projecting SkillMeta into synthetic Specs (Name+Description) — no new ranker, index-aligned back to the skill"
  - "Reserved actions (create|update|delete|install|catalog|restore|archive) are router keys returning a 'not yet available' error so the schema enum is downstream-stable from day one (D-01)"

patterns-established:
  - "skillLoader consumer-seam in package tools (List/Body/ManifestDescription) keeps internal/agent/tools free of an internal/skills import; the live *skills.Loader is adapted at cmd/aura"
  - "gosec floor for new internal/* production packages: 0o750 dirs / 0o600 files + justified #nosec G304 on operator-trusted-path reads"

requirements-completed: [CAP-07]

duration: ~40min
completed: 2026-06-05
---

# Phase 11 Plan 02: Skills Read Path (Slice 7a) Summary

**Greenfield `internal/skills` loader (goccy/go-yaml frontmatter + NFKC fold + multi-root 1s-TTL cache + Lstat-no-follow strip + parse-only validation) plus the ONE non-deferred `skill` tool — read actions list/info/use, OpenAI-wire-safe schema, turn-stable alphabetical manifest-in-Description with BM25 overflow, embedded skill-creator builtin, and one frozen messages[0] sentence.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-06-05
- **Tasks:** 2 (both TDD)
- **Files modified:** 19 (10 created, 9 modified incl. go.mod/go.sum)

## Accomplishments

- **Loader (`internal/skills`)** — scans multi-root FS in precedence order (later root wins), parses SKILL.md frontmatter with goccy/go-yaml (handles the spike-006 double-quoted-escaped-quotes + CRLF corpus), NFKC-folds the identity fields, caches behind a 1s TTL (lazy re-scan, goleak-clean), validates STRUCTURE only (name regex + name==dir + body cap) with NO blocklist (D-28), Lstat-strips symlinks (T-11-02-T1), and skip-logs invalid skills via slog.Warn (T-11-02-R1).
- **Manifest** — `RenderManifest` produces a byte-stable, alphabetically-sorted name+description block with the `"N more — search with skill action=list {query}"` overflow tail past the cap (D-09); `BM25Corpus` feeds the overflow ranker.
- **Embedded builtin** — `skill-creator` is `//go:embed`'d and fingerprint-idempotently materialized into AURA_SKILLS_DIR on first boot (D-31); it appears in the loader's very first scan.
- **`skill` tool** — ONE non-deferred tool (D-05) with an ActionRouter dispatching list/info/use (+ 7 reserved write/install actions), an OpenAI-wire-safe schema (root `required=["action"]`, no root oneOf/anyOf/enum, D-10), and a Description that IS the turn-stable manifest (D-06). `use` wraps the body in the load-bearing authority frame; `info` returns the plain body (D-08).
- **Wiring** — consumer-declared `skillLoader` seam keeps `internal/agent/tools` free of an `internal/skills` import (verified `go list -deps` = 0); `skillLoaderAdapter` + `newSkillTool(cfg)` register it in `buildBaseRegistry`; ONE frozen mechanism sentence added to `SystemPrompt`; D-34 `AURA_SKILL_*` knobs added to config.

## Task Commits

1. **Task 1: deps + greenfield loader/frontmatter/manifest + builtin** — `bad2e1e4` (feat, TDD test+impl folded)
2. **Task 2: skill tool + manifest-in-Description + registry wiring** — `3cc93205` (feat, TDD test+impl folded)

_TDD note: per-task RED/GREEN were developed together and committed once per task (the package is greenfield — tests and implementation land atomically); both commits carry the failing-then-passing test suite alongside the implementation._

## Files Created/Modified

- `internal/skills/frontmatter.go` — goccy/go-yaml parse + CRLF normalize + NFKC fold + tolerated-optional fields (D-30)
- `internal/skills/loader.go` — multi-root TTL-cached scan, Lstat strip, parse-only validation (D-28)
- `internal/skills/manifest.go` — byte-stable alphabetical RenderManifest + BM25Corpus (D-06/D-09)
- `internal/skills/builtin.go` + `embed/skill-creator/SKILL.md` — embedded builtin + idempotent materialization (D-31)
- `internal/agent/tools/skill.go` — SkillTool, ActionRouter, skillLoader seam, OpenAI-wire-safe schema, manifest Description
- `internal/agent/tools/skill_read.go` — actionList (manifest/BM25), actionInfo (plain body), actionUse (authority frame)
- `internal/agent/prompt.go` — ONE frozen skills mechanism sentence in SystemPrompt
- `internal/config/config.go` — D-34 AURA_SKILL_* knobs + ~/.aura/skills defaults
- `cmd/aura/main.go` — `reg.Register(newSkillTool(cfg))`
- `cmd/aura/serve_adapters.go` — skillLoaderAdapter + newSkillTool composition-root wiring

## Decisions Made

- **NFKC on the read path:** `parseFrontmatter` NFKC-folds Name+Description. This makes `golang.org/x/text` a meaningful direct dep (the plan required it direct) AND pre-canonicalizes skill identity to the exact form the 11-04 write-boundary blocklist will match against — a correctness alignment, not just a dep-promotion stub.
- **Lazy loader, no goroutine:** the TTL cache is a mutex-guarded snapshot refreshed on read past the TTL, so the loader spawns nothing and goleak stays clean.
- **BM25 reuse via synthetic Specs:** the list-overflow ranker projects SkillMeta into `Spec{Name,Description}` and reuses `tools/bm25.go` rather than adding a second ranker.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] gosec floor for the new internal/skills package**
- **Found during:** Task 2 (golangci-lint Gate-2 pass)
- **Issue:** `internal/agent/tools` is lint-excluded (pre-rewrite skeleton path), but `internal/skills` is NEW production code and IS linted — gosec flagged G301 (0o755 dirs), G306 (0o644 file), and G304 (variable file reads) in builtin.go/loader.go.
- **Fix:** Tightened materialization perms to 0o750 dirs / 0o600 files; added justified `#nosec G304` on the two operator-trusted-path reads (SKILL.md under a trusted root, symlink-stripped; builtin target under the operator skills dir + a compile-time-fixed embedded path). Also fixed a staticcheck QF1001 (De Morgan) in manifest_test.go.
- **Files modified:** internal/skills/builtin.go, internal/skills/loader.go, internal/skills/manifest_test.go
- **Verification:** `golangci-lint run ./internal/skills/... ./internal/config/... ./cmd/aura/...` → 0 issues; tests still green.
- **Committed in:** `3cc93205` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 missing-critical security floor)
**Impact on plan:** The gosec floor is a correctness/security requirement for new production code under the lint gate. No scope creep — the read-path behavior is exactly as planned.

## Issues Encountered

- **x/text stayed `// indirect` after `go get`:** promoting a dep to direct requires an actual non-test import. Resolved by adding the NFKC fold (`norm.NFKC.String`) on the frontmatter read path, which both satisfies the direct-dep acceptance and is a genuine correctness improvement (D-27 identity canonicalization).

## Verification Evidence

- `go test ./internal/skills/ ./internal/agent/tools/ ./internal/agent/ ./internal/config/` → all PASS; `go test -race ...` (WSL) green, goleak clean.
- `go list -deps ./internal/agent/tools | grep -c internal/skills` → **0** (cycle-free boundary held).
- `golangci-lint run` on the new/touched lintable packages (skills, config, cmd/aura) → **0 issues**.
- `go build ./...` → exit 0; `aura tools` lists `[active] skill — List, inspect, and apply skills...` (non-deferred).
- go.mod: `github.com/goccy/go-yaml` and `golang.org/x/text` both **direct** (no `// indirect`).
- All touched files ≤600 LOC (largest: loader.go 229).
- Schema discipline asserted: root `required==["action"]`, no root oneOf/anyOf/enum, action enum includes list/info/use + reserved; `reg.Validate()` holds; Description byte-identical across two calls (turn-stable).
- `git diff --diff-filter=D HEAD~2 HEAD` → no file deletions.

## TDD Gate Compliance

The plan is `type: execute` with two `tdd="true"` tasks over a GREENFIELD package. RED/GREEN were developed together and committed once per task (a separate failing `test(...)` commit on an empty package would not compile in isolation). Both `feat(...)` commits carry the full test suite (loader/frontmatter/manifest/builtin + skill tool) that exercises every `<behavior>` clause. No `refactor(...)` commit was needed.

## Known Stubs

The seven reserved skill actions (create|update|delete|install|catalog|restore|archive) return a `"not yet available"` error by design — they are router keys present so the schema enum is downstream-stable (D-01). This is an INTENTIONAL stub: their handlers land in plans 11-03/11-04/11-05. The read path (list/info/use) is fully wired and functional. Not a goal-blocking stub for 7a (the read half).

## Threat Flags

None — no new security surface beyond the plan's `<threat_model>`. The load path is parse-only (D-28), reads are operator-trusted-path with justified #nosec, symlinks are Lstat-stripped, and the manifest is byte-stable (CAP-04 invariant preserved by the Description-byte-identity test).

## Next Phase Readiness

- The loader seam (`skills.Loader`) + tool schema (`SkillTool` action enum) are the contracts plans 11-03/04/05 build on (write boundary, native install, snippets).
- Write-boundary blocklist + NFKC validator (the `validator.go` the RESEARCH names) lands in 11-04, matching against the same NFKC-canonical form the loader now produces.
- The ro `/skills` mount (D-17) + AURA_SANDBOX_AGENT_TOKEN wiring (D-38) are downstream (compose + sandbox-agent), not in this read-path plan.

## Self-Check: PASSED

- FOUND: all 9 created files (internal/skills/{frontmatter,loader,manifest,builtin}.go, embed/skill-creator/SKILL.md, internal/agent/tools/{skill,skill_read,skill_test}.go, 11-02-SUMMARY.md)
- FOUND: commit `bad2e1e4` (Task 1)
- FOUND: commit `3cc93205` (Task 2)

---
*Phase: 11-skills*
*Completed: 2026-06-05*
