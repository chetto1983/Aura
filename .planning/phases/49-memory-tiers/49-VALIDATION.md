---
phase: 49
slug: memory-tiers
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-31
---

# Phase 49 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `49-RESEARCH.md` §Validation Architecture. Task rows are filled by the planner.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing`; `go.uber.org/goleak v1.3.0`; `pgregory.net/rapid v1.3.0` |
| **Config file** | Build-tagged package tests; evaluator in `scripts/agent_memory_eval.py` |
| **Quick run command** | `go test ./internal/arcadedb ./internal/conversations ./internal/runner ./cmd/arcadedb-mcp` |
| **Full suite command** | `python scripts/agent_memory_eval.py --tier all` followed by WSL `make quality-full` and `bash scripts/coverage_docker.sh` |
| **Coverage gate** | Full tagged matrix ≥85% aggregate plus package-local policy; mutation spot-check ≥70% on critical files |
| **Estimated runtime** | Package baseline is the per-task fast path; live evaluator and quality/coverage are wave and phase gates |

---

## Sampling Rate

- **After every task commit:** Run the quick four-package baseline, plus `go vet ./...`, `go build ./...`, and `go test -race ./internal/<touched package>/` for each touched Go package.
- **After every plan wave:** Run touched live integration tiers, `go test -race -tags=arcadedb_integration -count=1 ./internal/arcadedb/`, the published MCP live test, and the deterministic agent-memory evaluator.
- **Before `$gsd-verify-work`:** Run `python scripts/agent_memory_eval.py --tier all`, WSL `make quality-full`, `bash scripts/coverage_docker.sh`, goleak, and mutation spot-checks on projection, recall isolation, capture sequencing/barrier, and batch final-state logic.
- **Phase gate:** Drive the real past-ladder recall, explicit-only reasoning retrieval, mid-task shell/file capture, and atomic rollback/concurrency scenarios on the live stack; score the real E2E above 9.8.
- **Max feedback latency:** Keep the daemon-free package baseline in the per-task loop; daemon-backed and full-matrix checks run at wave boundaries.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| _filled by planner_ | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

### Requirement → test map (from RESEARCH.md, pre-task)

| Req ID | Behavior | Test Type | Automated Command / Target | File Exists? |
|--------|----------|-----------|----------------------------|--------------|
| MEM-01 | Eligible user/final-assistant turns project idempotently; spill/edit/delete/rebuild converge; PostgreSQL and ArcadeDB failure semantics remain explicit | unit + live integration | New focused tests under `internal/conversations`, `internal/arcadedb`, and `internal/runner`; quick package baseline | ❌ Wave 0 |
| MEM-02 | Conversation and fact tiers are queried independently, fused stably, suppress active turns, and return bounded anchored windows/cursors | unit/property + live E2E | Extend MCP live suite and evaluator through published `memory_recall` | ❌ Wave 0 |
| MEM-03 | Authorized reasoning trace/steps/tools/`TOUCHED` edges persist; ordinary paths cannot read reasoning | unit + live integration | New ArcadeDB, runner, MCP, and history-isolation tests | ❌ Wave 0 |
| MEM-06 | Amendment extending #91 is a separate ancestor commit before reasoning-tier code | repository contract | Scripted `git log` ancestry and touched-path check | ❌ Wave 0 |
| TOOL-05 | One model-facing question spans both tiers, reports host-selected path, and abstains on weak/empty evidence | unit + live MCP | Extend `tool_memory_test.go` and `TestAgentMemoryMCPLive*` | ⚠️ fact-only coverage exists |
| AUTO-03 | Accepted captures remain ordered and are durable before task completion; provenance never names reasoning | unit/goleak/race + live shell/file E2E | New runner queue/terminal-barrier tests plus evaluator scenario | ❌ Wave 0 |
| CTX-05 | Reasoning remains absent from history, compaction, proactive context, ordinary recall, and durable-fact capture | structural/unit + live negative | Keep `TestHistoryTypesAreStructurallyReasoningFree`; add graph-resident negative cases | ⚠️ pre-graph regression exists |
| HARN-05 | Final-state validation, first-error semantics, unchanged rollback state, idempotent replay, and concurrent retry from committed state | rapid/property + race + live integration | New pure batch-planner tests and ArcadeDB live transaction tests | ❌ Wave 0 |

---

## Wave 0 Requirements

- [ ] Freeze the additive `memory_recall` mode/evidence/cursor schema, trusted active-context metadata route, and atomic batch-operation schema in tests.
- [ ] Add an authoritative paged eligible-turn fixture covering spilled content, edit/delete, and replay.
- [ ] Update `scripts/agent_memory_eval.py`: the current exact-latency `cli_identity_mcp_search` path exercises raw search, not the final one-read mixed recall contract.
- [ ] Add daemon-free pure tests for projection decisions, trace segmentation/redaction, cursor codec, capture sequencing and terminal barrier, and the final-state batch compiler.
- [ ] Add live authenticated tests for past-ladder mixed recall, explicit-only reasoning, mid-task shell/file capture, and atomic rollback/concurrency.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Amendment #91 extension precedes all reasoning-tier implementation commits | MEM-06 | Commit ancestry and semantic amendment content must be reviewed together | Inspect `git log --reverse --format='%H %cs %s'` and path-specific logs for `prd.md` plus reasoning-tier files; quote hashes proving the amendment commit is earlier and independent |
| Past-ladder live question is answered by one published `memory_recall` call and reports the retrieval path used | MEM-01, MEM-02, TOOL-05 | Requires a real conversation whose target turn has left the deterministic prompt ladder | Drive the scenario on the live stack; capture the MCP result and OTel span; verify evidence spans recent conversation and facts without duplicate active context |
| Extended reasoning is graph-resident but absent from later injected context until explicitly requested | MEM-03, CTX-05 | Requires a real reasoning turn, graph inspection, and a later context build | Persist an extended-reasoning turn, query ArcadeDB for trace/step/tool/entity edges, inspect the next prompt/context, then explicitly retrieve reasoning and compare |
| Durable fact revealed during a live shell/file task is committed before completion with direct provenance | AUTO-03 | Crosses model output, ordered capture queue, task completion barrier, graph write, and provenance | Drive the live task, wait for completion, query the recorded fact and provenance, and prove no reasoning-trace summarizer is the source |
| Multi-operation memory batch is atomic under a failing member and concurrent retry | HARN-05 | Final proof requires the deployed ArcadeDB transaction path | Execute a mixed batch with an injected invalid operation, verify no intermediate state, then race a retry against a concurrent committed update and verify final-state recomputation |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Daemon-free feedback stays in the per-task loop
- [ ] Full tagged coverage is ≥85% with package-local policy green
- [ ] Mutation spot-check is ≥70% on each critical Phase 49 file
- [ ] Live E2E scenarios are scored above 9.8 with OTel/ArcadeDB/provenance evidence
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
