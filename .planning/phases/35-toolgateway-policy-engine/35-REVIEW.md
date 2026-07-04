---
phase: 35-toolgateway-policy-engine
reviewed: 2026-07-04T18:40:00Z
depth: deep
files_reviewed: 8
files_reviewed_list:
  - internal/gateway/approvals.go
  - internal/gateway/approve.go
  - internal/gateway/gateway.go
  - cmd/aura/serve_adapters.go
  - internal/gateway/approvals_test.go
  - internal/gateway/approve_test.go
  - cmd/aura/gateway_resume_hook_test.go
  - internal/gateway/gateway_integration_test.go
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
resolved:
  - "WR-01 (GatewayApprovals.Approve did not clear a same-key pending challenge) — fixed in 63922e54, mirroring ShellApprovals.Approve, with a discriminating regression test."
status: clean
---

# Phase 35: Code Review Report

**Reviewed:** 2026-07-04T18:40:00Z
**Depth:** deep
**Files Reviewed:** 8
**Status:** clean — CR-01 confirmed closed; the one Warning found in the delta was fixed in-cycle (commit `63922e54`)

## Summary

This is a RE-REVIEW of the 35-07 gap-closure (commits `e2c9f6c1`, `f257d9f4`, `e75f5bf6`, diff base `e2c9f6c1~1`) written to close **CR-01** (the gateway interactive-approval confused-deputy / informed-consent bypass) and fold WR-01/WR-02/WR-03/IN-01/IN-02 from the prior standard-depth review. The mandate was to independently confirm CR-01 is genuinely closed and hunt for any NEW defect the fix introduced — not to re-trust the prior review's own trace.

**Verdict: CR-01 is genuinely closed.** Independent verification performed (not just re-reading the diff):

- **Line-by-line diff against the claimed mirror.** `GatewayApprovals.ApproveChallenge` (`approvals.go:130-150`) was diffed against `ShellApprovals.ApproveChallenge` (`internal/agent/tools/shell_approval.go:105-125`): both perform the identical two-guard sequence under the identical single-mutex critical section — existence check on the `pending` map, then byte-exact `question != challenge.question` check, then write-approved-and-delete-pending. No guard is dropped in the gateway version.
- **Challenge-on-issue confirmed.** `routeApprove` (`approve.go:114-130`) now computes `fp` and `question` exactly once, records `g.approvals.Challenge(key.ConversationID, spec.Name, fp, question)` **before** returning the approval-required result, and threads the SAME `fp`/`question` into both the challenge record and the model-facing preview (IN-02 confirmed fixed — single `gatewayArgsFingerprint` call site, verified by reading and by `grep`).
- **Production writer traced end-to-end.** `newGatewayResumeHook` (`serve_adapters.go:381-411`) now calls `g.ApproveChallenge(pending.ConversationID, rc.Tool, rc.ArgsSHA256, pending.Question, ...)` instead of the old unguarded `RecordResolvedApproval`. Traced `pending.ConversationID` to its origin: `internal/runner/runner_persist.go:334-345` (`persistPause`) sets `ConversationID: tr.convID` — the Runner's own authenticated turn id — never anything model-supplied. The model-relayed `rc.ConversationID` field was removed from the parsed struct entirely (not just deprioritized), closing WR-02 structurally rather than by convention.
- **Adversarial test actually exercises the boundary.** `TestGatewayResumeHookRejectsMismatchedQuestion` (`gateway_resume_hook_test.go:143-161`) drives a REAL challenge via a real `Decide` call, relays the authentic `resume_context` (fp matches) with a swapped, benign question, and asserts the hook errors, records nothing, and the re-drive stays `Approve`. This is exactly the WR-03 gap the prior review flagged as unexercised. Confirmed passing live (see Validation below), and the pre-existing cooperative round-trip test (`cmd/aura/gateway_approval_roundtrip_test.go`, unmodified by this delta, verbatim-relay path) still passes — the fix does not regress the honest-relay case.
- **WR-01 ordering verified, not just asserted.** `routeApprove` (`approve.go:99-117`) now evaluates `g.profile == config.ProfileServerProduction || !responderPresent(ctx)` **before** the cross-turn `Consume`. `TestRouteApproveProductionDeniesEvenWithLedgerApproval` injects an approval directly into the ledger (bypassing all record guards) and proves it is never consumed under `server_production` (`reserves() == 0`). Defense-in-depth is real, not cosmetic: both `RecordResolvedApproval` (`gateway.go:126-131`) and the new `Gateway.ApproveChallenge` (`gateway.go:141-149`) independently refuse to record under `ProfileServerProduction`.
- **Concurrency audit (explicit focus area).** The new `pending map[string]gatewayChallenge` is guarded by the SAME single `sync.Mutex` as `approved`; every method (`Approve`, `Consume`, `Challenge`, `ApproveChallenge`, `Evict`) takes the lock, does its work, and returns — none calls another lock-taking method internally, so there is no reentrant-lock deadlock and no lock-ordering hazard. `Challenge`-then-`ApproveChallenge` has no TOCTOU window: the existence check, the question check, and the approved/pending map mutation all happen inside one critical section per call. `Evict` now sweeps **both** maps (verified by `TestGatewayApprovalsEvictPrefixSweep`) — session end (`runner_resume.go:271`, unchanged, calls `r.gateway.EvictSession(convID)`) bounds ledger growth for abandoned/declined challenges, matching the shell ledger's identical tradeoff.
- **Map-key collision (explicit focus area).** `gatewayApprovalKey` (`convID + "\x00" + toolName + "\x00" + fp`) is unchanged by this delta and was not newly introduced as a risk: `convID` is always a host-minted UUID (never attacker-supplied), so an embedded-NUL delimiter collision is not attacker-reachable through this path. Pre-existing, out of this diff's blast radius.
- **Scope discipline confirmed.** The 6 files the fix explicitly promised to leave untouched (`internal/agui/approvals_api.go`, `internal/runner/runner_resume.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`, `internal/agent/llm_agent_pause.go`, `internal/agent/llm_agent_retry.go`) were independently `git diff`'d against the pre-35-07 tree — all six are byte-identical. No prohibited file was touched.
- **No test was weakened.** The only non-additive test change is `gateway_integration_test.go`'s two `context.Background()` → `WithResponder(context.Background())` edits on the resumed re-drive. This is a required correction, not a loosened assertion: it was made necessary by the WR-01 reorder (a headless re-drive now correctly denies before reaching `Consume`), and `runner.go:551` confirms every interactive turn the real Runner drives — including a resume `Turn(convID, nil)` — is unconditionally marked `WithResponder`, so the edit accurately models production. The asserted invariants (`Allow`, `operator_id` in the single reservation `Meta`, idempotent replay) are unchanged.
- **Live verification, not just reading.** Ran `go build ./...`, `go vet ./...`, and the same with `-tags db_integration` (all clean, zero output) and `go test -count=1 -v` across `internal/gateway`, `internal/agent` (+ subpackages), `internal/runner`, and `cmd/aura` — all green, including every adversarial/challenge/production-refusal test enumerated above (not a cached result).

**One WARNING-level finding surfaced** (an incomplete port from the shell precedent, currently inert in production — see below). No Critical issues, and no other new Warning/Info issues were found in the 35-07 delta.

## Warnings

### WR-01 [RESOLVED in `63922e54`]: `GatewayApprovals.Approve` did not clear a same-key pending challenge — an incomplete port of `ShellApprovals.Approve`

> **Resolution:** Fixed in-cycle as prescribed — `Approve` now computes the key once, sets `approved`, and `delete(a.pending, key)` (byte-for-byte parity with `ShellApprovals.Approve`, shell_approval.go:56-58), with the doc comment corrected. A discriminating regression test (`TestGatewayApprovalsApproveClearsSameKeyPendingChallenge`) asserts a post-`Approve` `ApproveChallenge` for the same key finds nothing. `go vet ./...`, build, gateway unit + `-race` (WSL) all green.

**File:** `internal/gateway/approvals.go:73-83`

**Issue:** The package header and multiple doc comments assert `GatewayApprovals` "mirrors ShellApprovals' shape exactly" and that `Approve` is "the parity analog of ShellApprovals.Approve" (`gateway.go:119-120`). That claim is not quite true for this one method. Shell's version clears a stale pending challenge for the same key when a raw approval is recorded:

```go
// internal/agent/tools/shell_approval.go:47-59
func (a *ShellApprovals) Approve(sessionID, digest string) {
	...
	key := shellApprovalKey(sessionID, digest)
	a.approved[key] = struct{}{}
	delete(a.pending, key)   // <-- clears any stale pending entry
}
```

The gateway version does not:

```go
// internal/gateway/approvals.go:73-83
func (a *GatewayApprovals) Approve(convID, toolName, argsFingerprint string, r ResolvedApproval) {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved == nil {
		a.approved = map[string]ResolvedApproval{}
	}
	a.approved[gatewayApprovalKey(convID, toolName, argsFingerprint)] = r
	// no delete(a.pending, key) here
}
```

Today this is inert: `grep -rn "RecordResolvedApproval" --include=*.go` (the only caller of `Approve`) turns up exactly one production definition site (`gateway.go`) and three `_test.go` call sites — `Approve`/`RecordResolvedApproval` has no production caller post-35-07 (`gateway.go:119-125` documents it as "a LOW-LEVEL test-seed seam ... NOT the faithful production writer any more"). Because the only thing that matters for `Consume` is the `approved` map entry, leaving a stale `pending` entry behind has no exploitable consequence under the current call graph: a later legitimate `Challenge()` for the same key just overwrites it, and a later `ApproveChallenge()` for the same key would at worst redundantly re-confirm an already-granted approval.

The risk is latent rather than active: the method is explicitly *retained* (not deleted) as a test seam, the file's own comments claim exact parity with a mirror that does clean up this map, and a future change that re-purposes `Approve`/`RecordResolvedApproval` as a production path (or adds a new caller without re-reading this gap) would inherit a pending-map entry that never gets cleared by that path — silently diverging from the shell precedent this whole ledger is supposed to track byte-for-byte.

**Fix:** Add the same cleanup shell does, for actual parity and to remove the latent trap:

```go
func (a *GatewayApprovals) Approve(convID, toolName, argsFingerprint string, r ResolvedApproval) {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved == nil {
		a.approved = map[string]ResolvedApproval{}
	}
	key := gatewayApprovalKey(convID, toolName, argsFingerprint)
	a.approved[key] = r
	delete(a.pending, key)
}
```

---

_Reviewed: 2026-07-04T18:40:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
