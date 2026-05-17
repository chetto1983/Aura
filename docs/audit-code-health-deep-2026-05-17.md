# Code-Health Deep Audit: Aura — 2026-05-17

Read-only audit by subagent. Looks for issues that survive after
golangci-lint reduces the lint count to 2 (both third-party). The
codebase already shipped 17+ cleanup commits today; this audit dug
deeper for things the linter can't see.

## Status of findings

### 1. Tests that test nothing useful
**NO FINDINGS.** Tests have proper assertions. Every `if err != nil { t.Fatal(...) }`
is followed by content checks. No `t.Skip()` for dead reasons. No `// TODO: assert`
markers left behind.

### 2. Commented-out code blocks
**NO FINDINGS.** Comments that look like code (matching `// .*[{};]`) are all
legitimate doc/explanation, not dead code parked in comments.

### 3. Architectural shadow paths
**ONE FINDING (shipped 2026-05-17):** Duplicate DB test helpers —
`cron.NewTestDB(t)` and `testutil.OpenTestDB(t, migrateFunc)` solved the
same problem with different APIs. The cron version hardcoded the migrate
step; testutil takes an optional migrate function. Consolidated to
`testutil.OpenTestDB(t, migrations.Run)` across 23 call sites in 5 test
files; `internal/cron/testdb.go` deleted.

### 4. Unreachable-but-compiling code paths
**NO FINDINGS.** All enum variants (`Outcome`, `RunStatus`, `Channel`,
`DeliveryMode`) are actually emitted somewhere. Switch defaults catch
real cases. No phantom-switch dormant values.

### 5. Tests that lie
**NO FINDINGS.** Spot-checked `internal/agent/loop_test.go` (42 tests),
`internal/chat/hub_test.go` (18+), and the Telegram test suite. Every
assertion exercises the current code path. Release conditional skips are
intentional (Docker-first branch doesn't track `.goreleaser.yml`).

### 6. Production helpers used only in tests
**NO FINDINGS.** Exported test helpers (`testutil.OpenTestDB`,
`testutil.TempDBPath`) are intentionally public for cross-package reuse.
No "hidden test harnesses" masquerading as production APIs.

### 7. Flakiness markers
**LOW RISK.** Inventory of `time.Sleep` and busy-poll patterns:
- 52 `time.Sleep` calls, mostly in `internal/concurrency/` threshold tests
  and `internal/chat/hub_test.go` async-event polling (legitimate).
- 38 high-iteration `for i := 0; i < N; i++` loops, mostly test-data
  generation (not race-condition workarounds).
- Worst offenders: `internal/chat/hub_test.go:221` 200×5ms poll for
  `run_started` event delivery, `internal/agent/tools/index/hash_test.go:32`
  200-iteration collision test. Both intentional.

### 8. Pseudo-config fields
**NO FINDINGS.** Spot-checked `cfg.ToolSearchTopK`, `cfg.ReasoningEffort`,
`cfg.TerminalToolPolicy`, `cfg.AllowlistConfigured`, `cfg.ConvArchiveEnabled`.
All read and used; none "always default in production".

## Summary

The codebase shows excellent hygiene post-cleanup. The single actionable
finding (duplicate DB test helpers) shipped immediately. Everything else
is clean.

Audit produced 2026-05-17 after rounds 1–2 (god files, duplication, dead
code, legacy, module bloat, doc drift) had already cleared the surface
issues. Future audits of this type should expect zero findings for at
least the next few milestones unless a major refactor regresses these
dimensions.
