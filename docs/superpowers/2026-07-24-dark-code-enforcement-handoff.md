# Handoff — CLAUDE.md dark-code enforcement (deferred to a clean tree)

**Date:** 2026-07-24
**Trigger:** CLAUDE.md updated — **"DARK CODE IS FORBIDDEN"** added to the NO GOD CLASS rule + a new CODE BASE RULES (Zen) section. Operator: "PROCEED TO ENFORCE CLAUDE.md."
**Status:** ⏸ **Deferred.** The full dark-code sweep CANNOT run reliably right now — a parallel spike (PRD #93.2, legacy adaptive-stack decommission) has ~126 uncommitted files / 40 deletions in the working tree, which makes `deadcode` produce false positives. Run the pass below **once the spike commits and `go build ./...` + `golangci-lint` are clean on master.**

## Why it's deferred (with proof)

`deadcode ./...` was run on the churned tree and is **demonstrably unreliable there**:
- It flagged `internal/agent/workflow` (`ParallelAgent`/`SequentialAgent`) as unreachable, **but at the clean committed HEAD that package has 5 real references** (`cmd/aura/agent.go`, `internal/onboarding/interview.go`, spike-039 sources). The spike's uncommitted decommission temporarily severed its callers → **false dark-code positive**. Removing it now would delete live, wired code.
- Conclusion: enforcing dark-code removal on a churned tree deletes working code. **Confirm every finding against a clean HEAD before acting.**

## What WAS enforced safely (2026-07-24)

- **NO GOD CLASS (≤600 LOC):** ✅ clean — 0 non-test / non-generated `.go` files over 600 LOC.
- **Phase A code (Tasks 1-2) not dark:** ✅ `classifyResolve` is called by `SubmitAnswer`; `ResolveDirective` is returned + read by every caller; `isScheduledTaskApproval` is called by `classifyResolve` and `scheduledApprovalAnswer`. All wired.

## The `deadcode` findings to triage post-spike

Run `deadcode -test ./...` (the `-test` flag includes test binaries as roots — it eliminates the `agenttest` false positives below). Then, for each finding, confirm **0 non-test callers at clean HEAD** before removing.

### A. IGNORE — spike-owned (the decommission's own new/unwired package)
Do NOT touch; the spike owns these:
- `internal/adaptive/*` — `graph.go` (GraphStore.Project), `hook.go` (NewDecisionHook), `policy.go` (validatePolicyUpdate), `policy_service.go` (NewPolicyService, Transition, Promote, transition, classifyPolicyTransitionError), `projector.go` (NewProjector, ProjectOne), `promotion.go` (EvaluatePromotion, validatePromotionEnvelope, summarizeArm, validateArmEvidence, wilson).

### B. LIKELY FALSE POSITIVE — test-support (deadcode default ignores test roots)
`internal/agent/agenttest/*` — `fakeclient.go` (TextThenErr, WithUsage), `mocks.go` (EmitNThenEscalate.*, RecordingAgent.*, CountingAgent.*). These are test doubles; CLAUDE.md's coverage gate already excludes `internal/agent/agenttest` as test-support. Re-run with `deadcode -test ./...`; only remove any that are used by NO test.

### C. VERIFY-THEN-ACT — candidates in stable packages
Confirm each against clean HEAD (`git grep -l "<sym>" HEAD -- '*.go' | grep -v _test.go`). Spot-checks already done on 2026-07-24 (churned-tree deadcode vs clean-HEAD grep):

| Finding | clean-HEAD non-test refs | verdict |
|---|---|---|
| `internal/agent/workflow/*` (NewParallel, NewSequential, ParallelAgent.*, SequentialAgent.*, runSub) | **5** | **NOT dark — false positive, KEEP** |
| `internal/agent/budget.go:99 NewBudgetFromEnv` | 3 | likely reachable — verify |
| `cmd/aura/serve_lifecycle.go:85 runServeComponents` | 2 | likely reachable — verify |
| `internal/agent/tracing.go:103 NewTracerProvider` (+ newTracerProvider, ExportSpans) | 2 | likely reachable — verify |
| `internal/agent/tools/shell_exec.go:448 renderShellOutput` (+ appendStatus) | 1 | verify (single ref may be another dead func) |
| `internal/agent/mcptools/bridge.go:190 Bridge`, `:431 Mount` | **0** | **strongest genuine-dark candidate** — but sits next to the spike's MCP work; verify + confirm not spike-adjacent before removing |
| `cmd/aura/idempotency.go:172 cliOperationFromContext` | not yet checked | verify |
| `cmd/aura/serve_drain.go:32 drainResult.String` | not yet checked | verify (String() may be for fmt/logging — check %v usage) |
| `internal/agent/metrics.go:160 recordSpanExportFailure`, `:181 recordLLMDuration` | not yet checked | verify (observability — may be dark if tracing is dark) |
| `internal/agent/tracing.go` (whole) | see above | if the tracer provider is never wired, the whole OTel path may be dark — investigate as a unit |
| `internal/agent/display/normalize.go:22 Normalize`, `systemevent.go:46 normalizeSwarmStatus` | not yet checked | verify |
| `internal/agent/panicobs/panicobs.go:50 Count` | not yet checked | verify (test-only counter?) |

**Note the `String()`/`Name()`/`Description()` methods:** deadcode flags interface-satisfying methods as unreachable when the concrete type is never constructed on a live path. Treat them as symptoms of a dead TYPE, not individually — remove the type, not one method.

## Resume procedure (post-spike, clean tree)

1. Confirm `go build ./...` + `go vet ./...` + `golangci-lint run` are clean on master (spike landed).
2. `deadcode -test ./...` → authoritative list (no spike churn, no test-helper FPs).
3. For each finding in group C: `git grep` the clean tree for non-test callers; remove only 0-caller code, following DEEP REFACTOR ON TOUCH (remove dead code + unused params + update comments in the same commit).
4. Also run `dupl` (duplication) and re-check `deadcode`/`golangci-lint` after removals.
5. Remove the whole dead TYPE when its constructor is unreachable (don't leave orphan methods).
6. This pass pairs naturally with resuming Phase A Tasks 3–10 on the same clean tree.

## Cross-refs
- Rule: [CLAUDE.md](../../CLAUDE.md) → Behavioral rules → NO GOD CLASS ("DARK CODE IS FORBIDDEN") + CODE BASE RULES.
- Dark-code pattern precedent: [docs/audit/consolidated-fix-plan-2026-07-20.md](../audit/consolidated-fix-plan-2026-07-20.md) → "Pattern A: built-but-not-wired (dark code)".
- Phase A ledger (gitignored scratch): `.superpowers/sdd/progress.md`.
- Raw deadcode output captured 2026-07-24 in the churned tree (60 findings) — reproduce with `deadcode -test ./...` on the clean tree.
