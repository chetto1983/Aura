---
phase: 09-swarm-minimal
fixed_at: 2026-06-04T11:05:29Z
review_path: .planning/phases/09-swarm-minimal/09-REVIEW.md
iteration: 1
findings_in_scope: 8
fixed: 8
skipped: 0
status: all_fixed
---

# Phase 9: Code Review Fix Report

**Fixed at:** 2026-06-04T11:05:29Z
**Source review:** .planning/phases/09-swarm-minimal/09-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 8 (CR-01, CR-02, WR-01..WR-06; Info findings IN-01..IN-04 out of scope)
- Fixed: 8
- Skipped: 0

## Fixed Issues

### CR-01: proxied_from_child_id UUID contract is unsatisfiable from the swarm report

**Files modified:** `internal/db/migrations/0008_proxied_child_id_text.up.sql`, `internal/db/migrations/0008_proxied_child_id_text.down.sql`, `internal/db/sqlc/models.go`, `internal/db/sqlc/paused_states.sql.go`, `internal/askuser/store.go`, `internal/askuser/store_test.go`, `internal/agent/tools/ask_user.go`
**Commit:** 2e7a702d
**Applied fix:** Picked the text-typed contract end-to-end (the review's recommended direction, consistent with D-15/D-16 flat child ids). New migration 0008 ALTERs `aura.paused_states.proxied_from_child_id` from `uuid` to `text` (down reverses with a `USING ::uuid` cast that fails on a flat id — the intended forward-only guard). Regenerated sqlc via `sqlc generate` (the repo's normal path): `ProxiedFromChildID` flips to `pgtype.Text` in both `models.go` and the insert params. `store.Insert` now stores the flat id verbatim (no `parseUUID`). The `ask_user` tool description drops the false "uuid" claim and says the flat id (e.g. `"w2"`). The masking integration test `TestInsertProxied` feeds the real `"w2"` shape and scans `pgtype.Text`. Verified live against the migrated DB (db_integration `TestInsertProxied` passes).

### CR-02: detectPause runs ask_user with context.Background(), ignoring cancellation/deadline

**Files modified:** `internal/agent/llm_agent_pause.go`, `internal/agent/llm_agent.go`, `internal/agent/llm_agent_pause_internal_test.go`
**Commit:** 8f2c4ee6
**Applied fix:** Threaded the live invocation context (`ic.Ctx`) through `Run -> pauseCalls -> detectPause` and passed it to `tool.Execute`, replacing `context.Background()`. The pre-execution now honors the agent's cancellation, the per-child swarm `WithTimeout` (D-11), and the budget wallclock `WithDeadline` (D-13), matching `runTool`'s ctx discipline. Internal white-box tests adapted to the new signatures with `context.Background()` (their assertions — name-gating and registry-miss — are unchanged).

### WR-01: runChild timeout-normalization can clobber a completed worker report

**Files modified:** `internal/swarm/swarm.go`, `internal/swarm/swarm_test.go`
**Commit:** 93bf88c0
**Applied fix:** Guarded the deadline-trip normalization to skip a worker that already produced a terminal success (`StatusOK` + populated `Summary`), treating that as authoritative over the post-hoc deadline observation. A genuine deadline failure (stream error, or no terminal output) still normalizes to the uniform `{failed, "timeout"}` — confirmed by the unchanged `TestSwarmChildTimeout`. Added a white-box test `TestRunChildTimeoutDoesNotClobberCompletedSuccess` that feeds `runChild` an already-expired ctx with an ok worker and asserts the success survives.

### WR-02: countSwarmWorkers over-counts on a summary containing "goal_index"

**Files modified:** `internal/eval/scoring_cot_eval.go`
**Commit:** 8a2de173
**Applied fix:** Replaced the `strings.Count(tr, "goal_index")` substring scan with a JSON parse of each tool result into the ChildReport array, counting array length (the deterministic ground truth). Removes the inflation vector where a worker summary or another MCP tool result quoting the key would let the D-22 ">=2 workers" hard floor pass on a single real worker. Per the project's no-substring-scan-over-NL rule.

### WR-03: swarm-demo writes transcripts into a shared os.TempDir() under a fixed conv id

**Files modified:** `cmd/aura/swarm_demo.go`
**Commit:** b002f88d
**Applied fix:** Switched `RunDir` from `os.TempDir()` (with the predictable `swarm-demo` conv path) to a per-invocation `os.MkdirTemp("", "aura-swarm-demo-")` cleaned up with `RemoveAll` on return — isolating concurrent demos and removing the world-shared-path collision and the persisted-transcript pollution.

### WR-04: Bridge allowlist silently drops requested-but-unadvertised tools

**Files modified:** `internal/agent/mcptools/bridge.go`, `internal/agent/mcptools/mount_test.go`
**Commit:** de503b98
**Applied fix:** Tracked which allowlist entries matched an advertised tool during the bridge loop and emit a `slog.Warn` for each unmatched one (WARN-not-fatal, preserving fail-soft boot). Added a sub-test asserting the warn fires (and `Mount` does not fail) for a plausible server-side rename (`searchEmails`) that matches nothing.

### WR-05: TestPersistPause_ForwardsProxiedIDs uses a synthetic UUID, hiding the CR-01 failure

**Files modified:** `internal/runner/runner_persist_test.go`
**Commit:** 7ee5c983
**Applied fix:** Changed the fixture from the synthetic uuid `"11111111-..."` to the real flat worker id `"w2"` the swarm produces and the model is told to relay, so the test models reality after CR-01 made the column text-typed. The test would now catch a regression that re-imposes uuid parsing.

### WR-06: mcp add len(args)<3 pre-check contradicts the post-parse checks

**Files modified:** `cmd/aura/mcp.go`
**Commit:** 0cc95d3c
**Applied fix:** Dropped the brittle `len(args) < 3` pre-check (which implied a contradictory "name + -- + command" minimum) in favor of a minimal `len(args) == 0` guard so `args[0]` stays safe, relying on the precise post-parse empty-name and `len(commandParts) == 0` checks that already enforce the real invariant.

## Verification

- Per-fix: `go vet` + `go build` + `go test` on each touched package (all green); pre-commit hooks (gofmt, vet, file-size <=600 LOC) passed on every commit.
- cot_eval-tagged file (WR-02): `go vet -tags cot_eval` + `go build -tags cot_eval` + tagged test compile all green.
- Final full pass: `go build ./...` clean; `go test ./...` clean (no failures); `go test -race` green on `internal/swarm`, `internal/agent`, `internal/askuser`, `internal/runner`.
- Live DB tier: `db_integration` `TestInsertProxied` (CR-01) passes against the migrated Postgres (migration 0008 applied, TEXT column round-trips the flat `"w2"` id).

---

_Fixed: 2026-06-04T11:05:29Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
