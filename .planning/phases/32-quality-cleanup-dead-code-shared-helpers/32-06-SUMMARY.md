---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 06
subsystem: agent-shared-helpers
tags: [qual-03, qa-a-01, qa-a-02, dedup, canonicaljson, retry-classifier, asymmetric, refactor, tdd]

# Dependency graph
requires:
  - phase: 32-03
    provides: "agent-package KEEP/swap baseline so the same-package extractions touch already-clean call sites"
provides:
  - "internal/canonicaljson.CanonicalArgs — the single tool-arg canonicalizer (agent.canonicalArgs + workflow.canonArgs deleted, both call sites repointed; zero new package edge)"
  - "internal/agent.isTransientNetworkErr — the shared typed-network sentinel subset both retry classifiers reuse"
  - "isTransientToolErr widened (DeadlineExceeded || isTransientNetworkErr) — intentional behavior change; retryableStreamOpenError kept STRICT (context.*->false guard FIRST, byte-identical golden)"
affects: [32-07, 32-08, agent, agent/workflow, canonicaljson]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Same-package extraction into an existing leaf (QA-A-01): CanonicalArgs lands in internal/canonicaljson because both agent + agent/workflow already imported it — no new edge, no cycle."
    - "Asymmetric-classifier extraction (QA-A-02, Pitfall 2): extract ONLY the typed-network subset; the tool path WIDENS (DeadlineExceeded || subset), the stream path stays STRICT (context.*->false guard FIRST, since a deadline is itself a net.Error{Timeout})."
    - "Golden-before parity (D-09/D-10): a 21-row golden table for retryableStreamOpenError captured GREEN against the OLD code, asserted byte-identical after the refactor (strict); the tool table characterizes OLD then asserts the documented widened set."

key-files:
  created: []
  modified:
    - internal/canonicaljson/canonicaljson.go
    - internal/canonicaljson/canonicaljson_test.go
    - internal/agent/llm_agent_args.go
    - internal/agent/llm_agent_dispatch.go
    - internal/agent/agent_fuzz_test.go
    - internal/agent/llm_agent_cover_test.go
    - internal/agent/workflow/loop.go
    - internal/agent/workflow/workflow_contract_test.go
    - internal/agent/llm_agent_retry.go
    - internal/agent/llm_agent_stream_retry.go
    - internal/agent/llm_agent_retry_test.go
  deleted: []

key-decisions:
  - "CanonicalArgs home = internal/canonicaljson (not a new leaf): the three copies were byte-identical and both call sites already imported canonicaljson, so the move adds zero package edges and cannot form a cycle."
  - "The two transient-error classifiers stay ASYMMETRIC. The shared subset is the typed-network sentinels only (net.Error timeout + io.EOF/io.ErrUnexpectedEOF/ECONNRESET/ECONNREFUSED/ETIMEDOUT). The tool path widens to include them; the stream path keeps its leading context.Canceled/DeadlineExceeded -> false guard FIRST. A symmetric merge would flip the stream path's deliberate deadline->false to true (Pitfall 2), because context.DeadlineExceeded is itself a net.Error{Timeout}."
  - "The stream golden table + the new isTransientNetworkErr/widened-tool tables live in the internal llm_agent_retry_test.go (package agent). The plan named llm_agent_stream_retry_test.go, but that file is package agent_test and cannot reach the unexported retryableStreamOpenError."

requirements-completed: []  # QUAL-03 partial — left to the orchestrator/verifier. 32-07/08 carry the remaining QUAL-03 items (agentrender + frontend dedup).

# Metrics
duration: ~1h (sequential, no worktree; concurrent-Codex isolation held)
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 06: Same-Package Agent Helper Extractions (CanonicalArgs + isTransientNetworkErr) Summary

**Two same-package QUAL-03 extractions, each test-first per D-09/D-10: (1) the byte-identical `agent.canonicalArgs` and `workflow.canonArgs` collapse into their existing home `internal/canonicaljson.CanonicalArgs` (zero new package edge); (2) the ASYMMETRIC transient-error classifiers share a new `isTransientNetworkErr` typed-network subset — the tool path is INTENTIONALLY WIDENED (now retries ECONNRESET/EOF) while the stream path stays byte-identical, proven by a 21-row golden table captured GREEN before the refactor. All call sites repointed, both copies deleted, `go test -race` green across canonicaljson + agent + agent/workflow.**

## Accomplishments

- **Task 1 — `canonicaljson.CanonicalArgs` (QA-A-01):** the single tool-arg canonicalizer now lives in the `canonicaljson` leaf. `agent.canonicalArgs` (`llm_agent_args.go`) and `workflow.canonArgs` (`workflow/loop.go`) are deleted; the production call sites (`llm_agent_dispatch.go`, `workflow/loop.go` x2) and the agent `FuzzCanonicalArgs` test now call `canonicaljson.CanonicalArgs`. Dropped the now-unused `encoding/json` import from `loop.go` and `canonicaljson` import from `llm_agent_args.go`. A characterization table (sorted objects, arrays, nested, number forms `1e3`/`1.0`/2^53, string/bool/null, malformed/empty/whitespace -> raw fallback) plus key-order-invariance + idempotence tests was captured GREEN before the copies were removed. Behaviour byte-identical.
- **Task 2 — `isTransientNetworkErr` shared subset (QA-A-02):** extracted the typed-network sentinel subset both retry classifiers share. `isTransientToolErr` now delegates (`errors.Is(context.DeadlineExceeded) || isTransientNetworkErr`) — an **intentional widening**; `retryableStreamOpenError` keeps its `context.*->false` guard FIRST, then HTTPError 429/5xx, `url.Error`, `ErrStreamIdleTimeout`, the shared subset, and the `retryableNetworkText` fallback last. Removed the now-unused `io`/`net`/`syscall` imports from `llm_agent_stream_retry.go`.

## Behavior Change (documented per acceptance criteria — T-32-06-TOOL)

`isTransientToolErr` is **intentionally widened**. The old rule retried only a `net.Error` timeout or `context.DeadlineExceeded`. It now ALSO retries the typed connection sentinels `syscall.ECONNRESET`/`ECONNREFUSED`/`ETIMEDOUT` and `io.EOF`/`io.ErrUnexpectedEOF` (via `errors.Is`, so wrapped sentinels with marker-free messages count). Net effect: a non-mutating tool whose `Execute` fails with a transient connection-drop is now retried (bounded linear backoff, max 3 attempts) instead of surfacing as a permanent observation. **Mutating tools are still never retried** — at-most-once side-effect semantics are unchanged. The stream path did NOT inherit this widening (see Pitfall 2).

## Task Commits

Each extraction committed atomically (D-11), direct `git commit -o -F <msgfile> -- <paths>` (explicit `--only` pathspecs to stay isolated from the concurrent Codex session):

1. **Task 1 — CanonicalArgs unification** — `1b436593` `refactor(32-06): unify tool-arg canonicalization into canonicaljson.CanonicalArgs` (8 files).
2. **Task 2 — isTransientNetworkErr extraction + tool widening + strict stream** — `9828f3c5` `refactor(32-06): extract shared isTransientNetworkErr; widen tool retry, keep stream strict` (3 files).

## Decisions Made

- **CanonicalArgs goes into the existing `canonicaljson` leaf, not a new package.** The three implementations were byte-identical and both consumers already imported `canonicaljson` (`llm_agent_args.go:8`, `loop.go:24`), so the move is edge-free and cycle-free by construction.
- **The classifiers stay asymmetric (Pitfall 2).** Only the typed-network sentinel subset is shared. `context.DeadlineExceeded` is itself a `net.Error{Timeout}`, so a naive single predicate that both paths merely `return` would flip the stream path's deliberate deadline->false to true. The stream path keeps its leading context guard; the tool path keeps (and widens) its `DeadlineExceeded -> true`.
- **Golden table placed in the internal test file.** `retryableStreamOpenError` is unexported, so its golden table must be `package agent`; it lives in `llm_agent_retry_test.go` alongside the `isTransientToolErr`/`isTransientNetworkErr` tables (all retry-classifier characterization in one place), not the external `llm_agent_stream_retry_test.go`.

## Deviations from Plan

**1. [Test placement] Stream golden table in llm_agent_retry_test.go, not llm_agent_stream_retry_test.go**
- **Found during:** Task 2 setup.
- **Issue:** The plan's `files_modified` listed `internal/agent/llm_agent_stream_retry_test.go` for the stream golden table, but that file is `package agent_test` (external) and cannot reference the unexported `retryableStreamOpenError`.
- **Fix:** The 21-row golden table + `TestIsTransientNetworkErr` + `TestIsTransientToolErr_WidenedNetworkSubset` were added to the internal `llm_agent_retry_test.go` (`package agent`), reusing the existing `timeoutErr` and `opaqueWrapErr` helpers. `llm_agent_stream_retry_test.go` was left unchanged.
- **Impact:** None on coverage or correctness — `retryableStreamOpenError` is 100% covered and the golden table proves byte-identical output.

**2. [Scope — necessary caller/test files] More files than files_modified listed (Task 1)**
- **Issue:** Deleting the unexported `canonicalArgs`/`canonArgs` (required by the acceptance `rg` check) forces repointing every caller. Beyond the plan's four Task-1 files this touched `llm_agent_dispatch.go` (production call site), `agent_fuzz_test.go` (the `FuzzCanonicalArgs` test calls it directly — would not compile otherwise), and comment-only accuracy fixes in `llm_agent_cover_test.go` + `workflow_contract_test.go` (they named the now-deleted helper).
- **Impact:** None on scope/behaviour — mechanical call-site migration committed atomically with the extraction.

**3. [Behavior change — per plan, not a regression] isTransientToolErr widened**
- The widening is mandated by the plan/research (QA-A-02 "INTENTIONAL WIDENING") and is documented above + tested by `TestIsTransientToolErr_WidenedNetworkSubset` (the `wasOld` column records the pre-change result). Listed here for the audit trail.

---
**Total deviations:** 2 mechanical (test placement, necessary-files expansion) + 1 documented intentional behavior change (per-plan widening).

## Coverage

| ID | Description | Verification | Status |
|----|-------------|--------------|--------|
| T1 | CanonicalArgs unified into canonicaljson; both agent copies deleted; call sites repointed; parity table green pre- and post-deletion. | `go test -race ./internal/canonicaljson/ ./internal/agent/ ./internal/agent/workflow/` green; `rg 'func canonicalArgs\|func canonArgs' internal/agent/` -> NONE; canonicaljson pkg **89.6%**, `CanonicalArgs` **85.7%** (the post-Unmarshal Marshal-error guard is unreachable from a string input — `json.Unmarshal` already yields only canonicalizable values; inherited verbatim from the old copies). | pass |
| T2 | Shared `isTransientNetworkErr` extracted; tool path widened (tested + documented); stream path byte-identical (golden table). | `go test -race ./internal/agent/` green; `func isTransientNetworkErr` present; `isTransientNetworkErr` / `isTransientToolErr` / `retryableStreamOpenError` all **100.0%**; 21-row golden table identical before/after. | pass |

All three retry-classifier functions are at 100% statement coverage; the canonicaljson leaf is 89.6% (>= the 85% owned-surface floor).

## Issues Encountered

- **Concurrent Codex session on master:** committed `564b21c0 fix: satisfy document workspace lint` between this plan's two task commits. Every commit here used explicit `--only` pathspecs; `git show --stat` confirmed each commit lists ONLY this plan's files (zero `internal/agui/**` or `.planning/graphs/**` swept in). The parallel session's uncommitted `.planning/graphs/*` changes were left untouched.
- **`gofmt`/`go env GOROOT` in WSL:** the `go` shim auto-resolves the go1.26.4 toolchain and returns an empty `GOROOT`; `gofmt` was invoked from the toolchain bin (`.../toolchain@v0.0.1-go1.26.4.../bin/gofmt`). All 10 edited Go files are gofmt-clean; the pre-commit hook (gofmt + vet + file-size) passed on both commits.

## User Setup Required

None — behaviour-preserving internal refactor on the dedup/canonicalization path, plus one documented, intentional retry-widening on the non-mutating tool path. No env, schema, or external-service changes.

## Next Phase Readiness

- **QUAL-03 same-package agent extractions are done.** Remaining QUAL-03 items: `internal/agentrender` (32-07) and the frontend `getJSON`/`focusTrap`/skeleton dedup (32-08).
- No new package was created, so no `scripts/coverage_gate.sh` registration is needed; the touched packages remain auto-included.

## Self-Check: PASSED

- FOUND: internal/canonicaljson/canonicaljson.go contains `func CanonicalArgs(` (line 50)
- FOUND: internal/canonicaljson/canonicaljson_test.go `TestCanonicalArgs` table (green pre-deletion)
- FOUND: internal/agent/llm_agent_retry.go contains `func isTransientNetworkErr(` (line 65); `isTransientToolErr` delegates + keeps DeadlineExceeded->true
- FOUND: `rg 'func canonicalArgs|func canonArgs' internal/agent/` -> NO MATCHES (both copies deleted)
- FOUND: call sites repointed — llm_agent_dispatch.go + workflow/loop.go (x2) -> canonicaljson.CanonicalArgs
- FOUND: 21-row retryableStreamOpenError golden table byte-identical before/after (context.*->false guard FIRST)
- FOUND commit: 1b436593 (Task 1 — CanonicalArgs unification, isolated: 8 files, 0 agui/graphs)
- FOUND commit: 9828f3c5 (Task 2 — isTransientNetworkErr extraction, isolated: 3 files, 0 agui/graphs)
- CONFIRMED: STATE.md and ROADMAP.md NOT modified (orchestrator owns those writes)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
