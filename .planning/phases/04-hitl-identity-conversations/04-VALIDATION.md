---
phase: 04
slug: hitl-identity-conversations
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-30
---

# Phase 04 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `04-RESEARCH.md` § Validation Architecture. Every assertion verifies the
> **artifact** (DB row / filesystem / CLI output), never the LLM reply alone.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `go.uber.org/goleak` v1.3.0 + `pgregory.net/rapid` v1.3.0 (property tests) |
| **Config file** | none (Go convention); `sqlc.yaml` for query generation |
| **Quick run command** | `go test ./internal/{runner,identity,conversations,askuser,agent}/...` (unit, in-memory fakes) |
| **Full suite command** | `go test -tags db_integration -race ./internal/... -count=1` (WSL, stack up; derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`) |
| **Estimated runtime** | ~10s unit / ~60–90s full db_integration |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/<package>/ && go test -race ./internal/<package>/` (Gate-2 post-edit validation)
- **After every plan wave:** `go test -tags db_integration -race ./internal/... -count=1` (WSL, stack up)
- **Before `/gsd:verify-work`:** full tag matrix green + `golangci-lint run ./...` == 0 + coverage ≥85% (owned surface) + mutation ≥70% on critical files (`context.go`, `askuser` Store, pause-detection)
- **Max feedback latency:** ~90 seconds (full db_integration)

---

## Per-Requirement Verification Map

> Task IDs (`04-NN-MM`) are assigned by the planner; this map binds each SPEC requirement /
> acceptance criterion to its ground-truth assertion and tier. Wave 0 stubs all `❌` entries.

| Req / SPEC AC | Behavior | Test Type | Ground-truth assertion | Tier | Status |
|---------------|----------|-----------|------------------------|------|--------|
| CORE-02 Req#1 / AC1 | `ask_user` pauses, writes 1 `paused_states` row, no fake RoleTool | unit + db_integration | `SELECT count(*) FROM aura.paused_states WHERE resumed_at IS NULL` == 1; assistant msg has no `role='tool'` for the call | ❌ W0 | ⬜ pending |
| CORE-02 Req#2 / AC2 | 3 simultaneous `ask_user` → 3 rows, FIFO `priority DESC, created_at ASC[, token]` | db_integration + rapid | `ListPending` returns 3 in deterministic order; `ResumeBatch` injects 3 `RoleTool` | ❌ W0 | ⬜ pending |
| CORE-02 Req#2 / AC3 | intra-turn exclusivity: only `ask_user` dispatched, siblings dropped | unit | persisted assistant msg contains ONLY ask_user tool_calls; `len(pending)==2` for 2×ask_user+1×other | ❌ W0 | ⬜ pending |
| CORE-02 Req#3 | crash recovery: restart store, `ListPending` returns rows in order; invalid token rejected | db_integration | rows survive new `Store` instance; `Resume(badToken)` returns clear error | ❌ W0 | ⬜ pending |
| CORE-02 Req#4 | no internal timeout / no `timed_out` status | grep/smoke | `grep -r timed_out internal/` empty; no `timed_out` in schema or loop | ❌ W0 | ⬜ pending |
| CORE-03 Req#5 / AC7 | fresh boot seeds `local`/`*`; `HasCapability("local","any_tool")`==true | db_integration | 1 row `(0…001,'*')` in `aura.capability_grants`; `HasCapability` true via wildcard | ❌ W0 | ⬜ pending |
| CORE-03 Req#6 / AC8 | grant/revoke idempotent; `'*'` grant/revoke rejected; FK cascade | db_integration | repeat grant = no error/1 row; `grant local '*'` → non-zero exit; delete identity cascades grants | ❌ W0 | ⬜ pending |
| CORE-04 Req#7 / AC5 | persist 3 turns, restart, resume reconstructs history; >cap spills to sidecar | db_integration | `LoadHistory` returns 3 turns post-restart; `content=NULL` + `content_sidecar_path` set + file on disk for >65536B | ❌ W0 | ⬜ pending |
| CORE-04 Req#8 / SC-2 | `LoadHistory` byte-identical ×2; atomic per-turn tx; failure → no partial turn | db_integration + rapid | two `LoadHistory` byte-equal; injected mid-tx failure → rollback, no orphan turn | ❌ W0 | ⬜ pending |
| CORE-04 Req#9 / AC4 | auto-title after seq≥3; LLM fail leaves NULL no crash; `chat list` shows non-zero USD | unit (fake client) + db_integration | `title` set after 3 turns; fake-error → title NULL, chat continues; `total_cost_usd` > 0 aggregated | ❌ W0 | ⬜ pending |
| CORE-04 Req#10 / AC9,AC10 / SC-1 | L1 evicts tool result after N turns (sidecar fetchable); L2.5 drops oldest pair + `context_rot_events`, `len` even; L1-first | smoke + unit | tool turn content→pointer after N; `read_tool_output` still works; `context_rot_events` row on hard-drop; `len%2==0`; zero rot rows when L1 alone fits | ❌ W0 + script | ⬜ pending |
| CORE-04 Req#11 / AC11 | `Runner.Stop` auto-resolves orphan pendings | db_integration | zero `resumed_at IS NULL` rows for conv after Stop; `paused-states list` shows auto-terminated answer | ❌ W0 | ⬜ pending |
| CORE-04 Req#12 / AC12 / SC-3 | delete cascade removes turns+paused_states+run dir; boot orphan scan; resume on broken state recovers | db_integration | dir gone after delete; stray dir removed at boot; pending auto-resolved + byte-identical LoadHistory | ❌ W0 | ⬜ pending |
| CORE-05 Req#13 / AC6 | `aura chat search "phrase"` returns excerpts by similarity; same query → identical set | db_integration + CLI smoke | rows ordered by `similarity` DESC from GIN index; query layer identical to future Telegram path | ❌ W0 | ⬜ pending |
| CORE-04 Req#14 / AC13,AC14 | `0003`→`0006` apply clean; re-run no-op; denied as `aura_app`, ok as `aura_migrate` | db_integration | migrate count 4 on fresh DB, 0 on re-run; DDL as `aura_app` → permission denied | ❌ W0 | ⬜ pending |
| CORE-02 SC-4 | resume injects RoleTool, no duplicate ask_user tool_call / no silent LLM re-run | unit (fake client) | next request messages carry original question→answer pair, no second ask_user call | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/runner/*_test.go` — unit (fakes) for Turn/SubmitAnswer/Stop + SC-4 (no silent re-run)
- [ ] `internal/identity/*_test.go` — HasCapability wildcard, grant/revoke idempotency, `'*'` rejection, FK cascade (db_integration)
- [ ] `internal/conversations/*_test.go` — LoadHistory byte-identity, AppendTurn atomicity (SC-2), context L1/L2/L2.5 (SC-1), search, orphan scan
- [ ] `internal/askuser/*_test.go` — Insert/ListPending FIFO order, MarkResumed(Batch), crash recovery, cleanup
- [ ] `internal/agent/llm_agent_pause_test.go` — sentinel interception, intra-turn rewrite, `Actions.AwaitingInput` emission
- [ ] `scripts/microcompact_smoke.sh` — L1 eviction + sidecar fetch + L2.5 pair-drop with `context_rot_events` row (mirrors `scripts/loop_budget_smoke.sh`)
- [ ] Shared fakes: in-memory `PauseStore`/`ConversationStore`/`IdentityStore` for unit tests (no DB → supports 85% floor); reuse `agenttest.FakeClient` for LLM
- [ ] CI: ensure `0003`–`0006` migration job runs under `db_integration` with composed DSNs (no-skip-as-green)
- [ ] Framework install: `go get github.com/pkoukk/tiktoken-go@v0.1.8` (behind `checkpoint:human-verify`)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation score ≥70% on critical files | phase gate | go-mutesting runs only on WSL fork; not in standard CI tier | WSL: `go-mutesting ./internal/conversations/context.go ./internal/askuser/...`; PASS=killed, score = killed/total ≥0.70 |
| `aura_app` denied DDL | Req#14 / AC14 | requires two distinct Postgres roles + live DB | Connect as `aura_app`, attempt `CREATE TABLE` → must return permission denied; repeat as `aura_migrate` → succeeds |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
