---
status: complete
phase: 02-agent-cornerstone
source: [02-00-SUMMARY.md, 02-01-SUMMARY.md, 02-02-SUMMARY.md, 02-03-SUMMARY.md, 02-04-SUMMARY.md, 02-05-SUMMARY.md, 02-06-SUMMARY.md, 02-07-SUMMARY.md]
started: 2026-05-30T05:34:47Z
updated: 2026-05-30T05:39:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Cold Start Smoke — clean build + dry-run boots
expected: `go build ./...` compiles clean; `go run ./cmd/aura agent dry-run` boots and streams Event JSON lines ending in a terminal Event. No panic, no nil deref, defaults load fine.
result: pass
evidence: "go build ./... exit 0; dry-run exit 0, 26 JSON lines, terminal Event actions.escalate=true limit_hit=max_steps steps_consumed=25; no stderr. (Note: span_id all-zero + timestamp 0001-01-01 are the documented INFO-level deferred items, not SC assertions.)"

### 2. SC#2 — Loop terminates at budget (smoke script)
expected: `bash scripts/loop_budget_smoke.sh` exits 0 and reports "ok (SC#2): 26 lines, terminal Event limit_hit=max_steps" plus the B4 coverage gate >= 85%.
result: pass
evidence: "smoke exit 0; 'ok (SC#2): 26 lines, terminal Event limit_hit=max_steps'; 'ok (B4): Phase-2 coverage 90.7% >= 85%'. (Coverage 90.7% vs 91.5% in SUMMARY — minor drift, still well above floor.)"

### 3. SC#4 — UUIDv7 request_id (auto)
expected: `--request-id auto` emits 26 lines, every line the SAME request_id, valid UUIDv7.
result: pass
evidence: "26 lines, 1 distinct request_id (019e7762-778d-71bf-8c7f-69e7002ba9db), all 26 lines match the UUIDv7 regex."

### 4. SC#4 — request_id literal is reproduced verbatim
expected: `--request-id 0192f000-0000-7000-8000-000000000001` emits 26 lines, every line that exact UUID.
result: pass
evidence: "26 lines, 1 distinct request_id == 0192f000-0000-7000-8000-000000000001 (verbatim, not regenerated)."

### 5. SC#4 — CLI flag beats env var (precedence)
expected: `AURA_LOOP_MAX_STEPS=5 ... --max-steps 10` yields 11 lines (10 step + 1 terminal).
result: pass
evidence: "11 lines; terminal steps_consumed=10 — CLI flag won over env=5 (D-06 CLI>env>default)."

### 6. Budget fail-fast on malformed env (D-06)
expected: `AURA_LOOP_MAX_STEPS=abc ...` fails fast with a verbatim malformed error; no silent default.
result: pass
evidence: "exit 1; zero stdout lines; stderr: 'AURA_LOOP_MAX_STEPS=\"abc\": not a valid value'. Fail-closed, no fallback."

### 7. SC#1 — zero goroutine leak + race-clean (full agent suite)
expected: `go test -race -count=1 ./internal/agent/...` exits 0, no DATA RACE, no goleak failure.
result: pass
evidence: "exit 0; ok internal/agent, internal/agent/agenttest, internal/agent/workflow; internal/agent/tools = no test files (later-slice skeleton). No race, no leak."

### 8. SC#3 — depth-3 nested ParallelAgents share ONE budget
expected: `TestParallelAgent_DepthChainBudgetShared_NotFresh` passes under -race; 9-leaf tree at 25 consumes ≤ 25.
result: pass
evidence: "--- PASS: TestParallelAgent_DepthChainBudgetShared_NotFresh (0.00s); race-clean."

## Summary

total: 8
passed: 8
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none — all 8 user-observable tests passed; SC#1–SC#4 all confirmed live on the real binary + race suite]
