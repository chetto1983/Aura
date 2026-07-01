---
phase: 34
slug: agent-loop-correctness-durable-ledger
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-01
---

# Phase 34 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `34-RESEARCH.md` §Validation Architecture (Requirement → Test Map). Task IDs are
> finalized at plan time; this map keys by requirement + target test file until then.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` — table-driven, `-race`, `go.uber.org/goleak` (package-wide `TestMain` in `runner`/`conversations`/`askuser`) |
| **Config file** | none — existing harness (`fakeDBTX`, `agenttest.NewFakeClient`, live-PG `db_integration` stack, `sqlc` all present) |
| **Quick run command** | `go test -race ./internal/<touched-pkg>/` |
| **Full suite command** | `go test -race -tags db_integration ./internal/runner/ ./internal/conversations/ ./internal/agent/... ./internal/askuser/ ./cmd/aura/` (live PG; composed `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`) |
| **Estimated runtime** | unit sub-second/pkg; full `db_integration` matrix ~tens of seconds against the live stack |

---

## Sampling Rate

- **After every task commit:** Run `go test -race ./internal/<touched-pkg>/` (unit tier, sub-second)
- **After every plan wave:** full unit matrix + `go test -tags db_integration -race ./internal/runner/ ./internal/conversations/` against the live stack
- **Before `/gsd-verify-work`:** `make quality-full` green — owned-surface coverage ≥85%, race-clean across the tag matrix
- **Phase gate:** mutation spot-check ≥70% on critical files: `askuser/store.go` (batch tx), `conversations/store.go` `loadTurns` (fence), `llm_agent_dispatch.go` (reject), `runner_resume.go` (ResumeCommitter)
- **Max feedback latency:** unit < 5s; integration bounded by live-PG round-trips
- **No-skip-as-green:** the `db_integration` tier `t.Fatal`s under `$CI` when its env is unset — a skipped tier fails the gate, never passes it

---

## Per-Task Verification Map

> Task IDs (`34-NN-MM`) assigned once PLAN.md files exist; the Nyquist audit backfills them.
> Tier: **U** = unit (`-race`), **I** = integration (`-tags db_integration`, live PG).

| Requirement | Behavior to prove | Threat Ref | Test Type | Target test file (new/extend) | Status |
|-------------|-------------------|------------|-----------|-------------------------------|--------|
| LOOP-01 | terminal `text_response` + mutating sibling → sibling never runs; step trips replan/finalize | T-34-A (F-003) | U table | new `internal/agent/llm_agent_terminal_reject_test.go` | ⬜ pending |
| LOOP-01 | terminal + read-only sibling → also hard-rejected (D-01, not option B) | T-34-A | U | ″ | ⬜ pending |
| LOOP-01 | two `text_response`, no other tool → 2nd not silently dropped (`terminalCount>1`) | T-34-A | U | ″ | ⬜ pending |
| LOOP-02 | duplicate batch → exactly one answer/pause, no orphan `RoleTool`, dup → `ErrPauseNotFound` | T-34-B (F-004) | U + I | new `runner_resume_batch_atomic_test.go` (+ `_integration`) | ⬜ pending |
| LOOP-02 | two concurrent identical batches → one wins, all-or-nothing, deadlock-free (sorted-token claim) | T-34-B | I live PG | ″ | ⬜ pending |
| LOOP-03 | append failure after claim → whole tx rolls back → pause stays pending → retry works | T-34-C (F-029) | I fault-inject | new `runner_resume_single_atomic_integration_test.go` | ⬜ pending |
| LOOP-03 | existing single-resume dup test stays green | regression | U | keep `runner_resume_atomic_test.go` | ⬜ pending |
| LOOP-04 | pause never visible without wire-valid assistant tool_call turn | T-34-D (F-030) | I live PG | new `runner_pause_exposure_integration_test.go` | ⬜ pending |
| LOOP-04 | happy path: after flush, pause visible AND assistant tool_call turn durable; N pauses → one turn | — | I + U | extend `runner_multipause_test.go` | ⬜ pending |
| LOOP-05 | outside-root / traversal / symlink sidecar reads rejected; valid rehydrates (both `loadTurns` + `loadBranchTurns`) | T-34-E (F-005) | U tmpfs table | new `store_sidecar_fence_test.go` + relocate fixtures in `store_fakedbtx_test.go` | ⬜ pending |
| LOOP-06 | outside-workspace `send_file` → deterministic error, no `ask_user`/`resume_context` | T-34-F (F-009) | U | extend `send_file_test.go` | ⬜ pending |
| LOOP-07 | overwrite preserves mode; new file → 0o644; mid-write crash never truncates (`fs_write` + `fs_edit`) | T-34-G (F-010) | U table | extend `internal/agent/tools/fs_test.go` | ⬜ pending |
| LOOP-08 | mutating tool that panics after side effect → `Mutating==true` propagates, `a.sideEffected` armed | T-34-H (F-031) | U (impl present; test closes box) | new `llm_agent_mutating_panic_test.go` | ⬜ pending |
| LOOP-09 | unreferenced aged `.content` removed; referenced `.content` kept; `.result` kept; young `.content` kept; symlink not followed | T-34-I (F-040) | I | extend `orphan_scan_test.go` | ⬜ pending |
| LOOP-10 | `>cap` turn spills (content=NULL) AND absent from `SearchConversationTurns`; inline turn IS found | — (F-048) | I live pg_trgm + doc comments | new `store_search_spill_integration_test.go` | ⬜ pending |
| LOOP-11 | repeated `Stop` on hung worker → `NumGoroutine` Δ constant; clean worker → no leak | — (F-045) | U (NumGoroutine delta, NOT goleak) | new `runner_stop_leak_test.go` | ⬜ pending |
| QUAL-04a | `ListRecent` int32 overflow guarded (unset/0/50/MaxInt32/overflow) | — | U table | extend `askuser/store_unit_test.go` | ⬜ pending |
| QUAL-04b | boot error paths close the pool; no overlay-path leak | — | U | new `cmd/aura/chat_boot_test.go` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agent/llm_agent_terminal_reject_test.go` — LOOP-01 (fake mutating tool w/ call-flag; check `agenttest` for an existing one)
- [ ] `internal/agent/llm_agent_mutating_panic_test.go` — LOOP-08
- [ ] `internal/runner/runner_resume_batch_atomic_test.go` (+ `_integration`) — LOOP-02/03 (reuse `fakes_test.go`; add a `ResumeCommitter` fake + fault-injecting conversation-append wrapper)
- [ ] `internal/runner/runner_resume_single_atomic_integration_test.go` — LOOP-03
- [ ] `internal/runner/runner_pause_exposure_integration_test.go` — LOOP-04
- [ ] `internal/runner/runner_stop_leak_test.go` — LOOP-11
- [ ] `internal/conversations/store_sidecar_fence_test.go` + relocate fixtures in `store_fakedbtx_test.go` — LOOP-05
- [ ] `internal/conversations/store_search_spill_integration_test.go` — LOOP-10
- [ ] extend `internal/conversations/orphan_scan_test.go` — LOOP-09
- [ ] extend `send_file_test.go`, `internal/agent/tools/fs_test.go`, `askuser/store_unit_test.go`; new `cmd/aura/chat_boot_test.go`
- **Framework install:** none — `goleak`, `sqlc`, live-PG integration harness all already present

*Free-riding existing infra: package-wide `goleak.VerifyTestMain`; `fakeDBTX` for non-tx projection branches; `agenttest.NewFakeClient` + `newTestRunner` in-memory fakes; the live `db_integration` stack.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| AG-UI approvals SSE does not surface a pause token before `flushPause` (assumption A1) | LOOP-04 | Emission-path timing check across the SSE boundary — verify during planning/impl, not a pure code assertion | Trace `persistPause`→`flushPause`→SSE token emission; confirm the token is minted after the pause Event is observed (code shape says it can't leak early) |

*All other phase behaviors have automated verification (unit + `db_integration`).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 5s (unit) / bounded by live PG (integration)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
