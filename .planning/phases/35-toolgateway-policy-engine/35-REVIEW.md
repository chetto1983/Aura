---
phase: 35-toolgateway-policy-engine
reviewed: 2026-07-04T00:00:00Z
depth: standard
files_reviewed: 17
files_reviewed_list:
  - internal/gateway/approvals.go
  - internal/gateway/approvals_test.go
  - internal/gateway/approve.go
  - internal/gateway/approve_test.go
  - internal/gateway/decide.go
  - internal/gateway/decide_test.go
  - internal/gateway/gateway.go
  - internal/gateway/reserve.go
  - internal/gateway/reserve_test.go
  - internal/gateway/gateway_integration_test.go
  - internal/agent/llm_agent_retry.go
  - internal/agent/llm_agent_retry_gateway_test.go
  - internal/runner/runner_resume.go
  - cmd/aura/serve_adapters.go
  - cmd/aura/chat_boot.go
  - cmd/aura/gateway_resume_hook_test.go
  - cmd/aura/gateway_approval_roundtrip_test.go
findings:
  critical: 1
  warning: 3
  info: 2
  total: 6
status: issues_found
---

# Phase 35: Code Review Report

**Reviewed:** 2026-07-04T00:00:00Z
**Depth:** standard
**Files Reviewed:** 17
**Status:** issues_found

## Summary

This gap-closure (35-06) adds the interactive `approve` verdict for mutating `GateRecommended`
tool calls under `single_user_hardened`: a shell_exec-style approval-required tool RESULT, a
cross-turn `GatewayApprovals` ledger, the host-side `newGatewayResumeHook` writer, and the
`routeApprove` Consume-on-resume path.

Most of the security boundary is sound and well-tested:

- **Responder signal is host-side.** `WithResponder` is set only by the interactive Runner
  (`internal/runner/runner.go:551`); the model cannot mint it from args/ctx. Verified across
  `TestApproveIsHostSideOnly`, `TestApproveHeadlessDenies`, `TestApproveProductionDenies`.
- **Consume is one-shot and race-safe.** `GatewayApprovals.Consume` deletes under `sync.Mutex`
  (`approvals.go:66-79`); no double-consume window. `New()` always builds a fresh ledger; every
  method is nil-receiver-safe.
- **Fingerprint binding is correct.** `sha256(canonicaljson.CanonicalArgs(args))` absorbs cosmetic
  JSON diffs but keeps `1` vs `1.0` and value changes distinct (`canonicaljson.go`), and the key
  excludes the mutable `tool_call_id`. A tampered re-emit misses the Consume → fail-closed.
- **Single-reservation invariant holds.** The approved re-emit takes exactly one 35-04 reservation
  with `operator_id` folded into that one start's Meta; no competing Insert (`reserve.go:57-80`,
  proven by the live-PG `TestApprovedCallReservedAndIdempotent` / `TestGatewayApprovalResumeReentersAndReservesOnce`).

**However, the review found one BLOCKER on the authorization path.** The code repeatedly claims
`newGatewayResumeHook` is "the byte-for-byte analog of `newShellResumeHook`" — but it is **not**:
it drops the shell path's server-side challenge binding and its operator-question verification. As
a result the *content* of the approval (the tool + args being authorized, and the prompt the
operator sees) is fully model-controlled and unverified. The model cannot self-approve without an
operator click, but it can make that click authorize an action entirely different from what the
operator was shown — a confused-deputy / informed-consent bypass. Two related WARNINGs (deny-check
ordering, model-relayed conversation id) compound it.

## Narrative Findings (AI reviewer)

### Critical

#### CR-01: Gateway approval has no challenge/question binding — the model can get an operator to approve a benign-looking prompt while authorizing a different destructive action

**File:** `cmd/aura/serve_adapters.go:376-410` (missing binding), `internal/gateway/approvals.go:28-31` (no pending-challenge map), `internal/gateway/approve.go:109-116` (issues approval-required without recording a challenge)

**Issue:**
`newGatewayResumeHook` records an approval straight from the **model-relayed** `resume_context`,
with no check that the gateway ever issued that approval and no check that the operator-visible
question matches the gateway-generated one:

```go
// serve_adapters.go:396-407
if rc.Tool == "" || rc.ArgsSHA256 == "" {
    return fmt.Errorf("gateway resume context: missing tool or args_sha256")
}
convID := rc.ConversationID
if convID == "" { convID = pending.ConversationID }
g.RecordResolvedApproval(convID, rc.Tool, rc.ArgsSHA256,
    gateway.ResolvedApproval{Approved: true, OperatorID: "local"})   // records model-supplied (tool, fp); pending.Question never inspected
```

The shell path it claims to mirror does **both** missing checks. `newShellResumeHook`
(`serve_adapters.go:363`) calls `approvals.ApproveChallenge(pending.ConversationID, rc.CommandSHA256, pending.Question)`,
and `ShellApprovals.ApproveChallenge` (`internal/agent/tools/shell_approval.go:105-125`) rejects
the accept unless (a) a **server-recorded pending challenge** exists for that digest (created by
`CreateChallenge` only for a command the model actually surfaced) and (b) `question == challenge.Question`
(the operator-visible prompt equals the server-generated prompt). `GatewayApprovals` has **no
`pending` map at all** (`approvals.go:28-31`) and the hook never looks at `pending.Question`, so
neither guard exists.

Because the operator interface renders only the model-supplied `pending.Question`
(`internal/agui/approvals_api.go:86`; the `approvalItem` projection at `:56-64` does not even expose
the `resume_context`), the operator has no trusted view of what they are approving.

**Concrete failure scenario (verified against the code path end-to-end):**
1. Model in `conv-X` emits `swarm_spawn {"goals":["destroy prod"]}`. `routeApprove`
   (`approve.go:109-116`) returns the approval-required result whose preview carries the real
   `question` ("Approve swarm_spawn (risk=risky)? … args: goals") and `resume_context`
   `{type:gateway_approval, tool:swarm_spawn, conversation_id:conv-X, args_sha256:FP}`.
2. The model reads `FP` from that result and calls
   `ask_user {kind:"approval", question:"Save your meeting notes?", resume_context:{…same tool + FP…}}`
   — real `resume_context`, but a **benign, false question**.
3. `detectPause` (`llm_agent_pause.go:100-105`) stores `pending.Question="Save your meeting notes?"`
   and `pending.ResumeContext=<model's>`. The approval center shows "Save your meeting notes?"
   (`approvals_api.go:86`). The operator accepts.
4. `SubmitAnswers → applyResumeHook → newGatewayResumeHook` records
   `Approve(conv-X, swarm_spawn, FP)` with **no** question check.
5. Resume `Turn(conv-X, nil)`: the model re-emits `swarm_spawn {"goals":["destroy prod"]}`;
   `routeApprove` Consumes the approval (`approve.go:99-102`) → `Allow` → reserve → **Execute**.

The operator consented to "Save your meeting notes?" and unknowingly authorized a destructive
swarm. The interactive-approval gate — whose sole purpose is informed operator consent under
`single_user_hardened` — is defeated. (The model can even fabricate a `resume_context` for a tool
it never surfaced, since there is no pending-challenge existence check; it just needs the correct
`args_sha256`, which the approval-required result hands it for free.)

The phase's own `35-VERIFICATION.md` asserts "No model self-approval" is proven by
`TestApproveIsHostSideOnly`, but that test only checks that model *args* cannot flip the verdict; no
test drives the `ask_user` relay with a mismatched question (the round-trip client at
`gateway_approval_roundtrip_test.go:161-178` copies question + resume_context verbatim), so this
path is entirely unexercised (see WR-03).

**Fix:** Port the shell challenge mechanism the code claims to already mirror. Add a pending-challenge
map to `GatewayApprovals`; have `routeApprove` record the gateway-generated question keyed on the
**authenticated** `(key.ConversationID, spec.Name, fp)` when it issues the approval-required result;
and make the hook verify existence + question-match before recording:

```go
// approvals.go — new pending map + record + verify-on-approve
type gatewayChallenge struct{ question string }

func (a *GatewayApprovals) Challenge(convID, tool, fp, question string) {
    if a == nil || convID == "" || tool == "" || fp == "" { return }
    a.mu.Lock(); defer a.mu.Unlock()
    if a.pending == nil { a.pending = map[string]gatewayChallenge{} }
    a.pending[gatewayApprovalKey(convID, tool, fp)] = gatewayChallenge{question: question}
}

// ApproveChallenge records the ResolvedApproval ONLY if a server-issued challenge exists
// AND the operator-visible question matches it (mirrors ShellApprovals.ApproveChallenge).
func (a *GatewayApprovals) ApproveChallenge(convID, tool, fp, question string, r ResolvedApproval) error {
    if a == nil || convID == "" || tool == "" || fp == "" {
        return fmt.Errorf("gateway approval challenge %q not found", fp)
    }
    a.mu.Lock(); defer a.mu.Unlock()
    key := gatewayApprovalKey(convID, tool, fp)
    ch, ok := a.pending[key]
    if !ok { return fmt.Errorf("gateway approval challenge %q not found", fp) }
    if question != ch.question { return fmt.Errorf("gateway approval challenge %q question mismatch", fp) }
    if a.approved == nil { a.approved = map[string]ResolvedApproval{} }
    a.approved[key] = r
    delete(a.pending, key)
    return nil
}
```

`routeApprove` records the challenge right before returning the approval-required result:

```go
// approve.go, in the single_user_hardened branch
result := gatewayApprovalRequiredResult(spec, tier, rawArgs, key)
g.approvals.Challenge(key.ConversationID, spec.Name, gatewayArgsFingerprint(rawArgs),
    gatewayApprovalQuestion(spec, tier, rawArgs))
return Verdict{Decision: Approve, Tier: tier, ApprovalRequest: &result}, nil
```

and `newGatewayResumeHook` calls `ApproveChallenge(pending.ConversationID, rc.Tool, rc.ArgsSHA256, pending.Question, …)`
instead of `RecordResolvedApproval` (also resolving WR-02 by using the authenticated
`pending.ConversationID`). Extend `Evict` to sweep the `pending` map too (shell parity,
`shell_approval.go:190-200`), and add a negative test that a swapped/benign question is rejected.

### Warnings

#### WR-01: `Consume` runs before the `server_production` / no-responder fail-closed check, so a recorded (or CR-01-fabricated) approval overrides the hard-deny

**File:** `internal/gateway/approve.go:99-108`

**Issue:** `routeApprove` Consumes the cross-turn ledger *before* the profile/responder gate:

```go
fp := gatewayArgsFingerprint(rawArgs)
if r, ok := g.approvals.Consume(key.ConversationID, spec.Name, fp); ok && r.Approved {
    return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID}, nil   // (1) consume first
}
if g.profile == config.ProfileServerProduction || !responderPresent(ctx) {       // (2) deny gate second
    g.recordDegradedDeny(ctx, spec, key, tier)
    return Verdict{Decision: Deny, ...}, nil
}
```

The only thing preventing an approval from existing under `server_production` is that the gateway
never issues an approval-required result there — but `newGatewayResumeHook` is **not** profile-gated
(`serve_adapters.go:376-409` records on any accept regardless of profile), and via CR-01 the model
can induce a `gateway_approval` pause without the gateway issuing one. If any such approval reaches
the ledger, the `server_production`/headless hard-deny (D-03a/b) is bypassed because Consume returns
`Allow` first. The fail-closed guarantee should not depend on a single upstream property.

**Fix:** Gate the Consume behind the same profile/responder check (a production/headless run must
never consume a cross-turn approval), or refuse to *record* approvals under
`ProfileServerProduction` in `RecordResolvedApproval`/`ApproveChallenge`. Prefer both (defense in
depth). Add a test: an approval recorded, then a `server_production` Decide must still Deny with
`Execute==0`.

#### WR-02: The ledger key trusts the model-relayed `conversation_id` over the authenticated `pending.ConversationID`

**File:** `cmd/aura/serve_adapters.go:402-407`

**Issue:**

```go
convID := rc.ConversationID          // from the model's relayed resume_context
if convID == "" { convID = pending.ConversationID }
g.RecordResolvedApproval(convID, rc.Tool, rc.ArgsSHA256, …)
```

`rc.ConversationID` comes from the model's `ask_user` `resume_context`; `pending.ConversationID` is
the server-stored conversation of the pause (set at pause creation, not model-forgeable). Trusting
the model value first lets the model record an approval under a **different** conversation than the
one the operator resolved in. Because the cross-thread approval center aggregates pending pauses
from all conversations (`approvals_api.go:66-93`), an operator accept surfaced from `conv-A` could
authorize an action re-emitted in `conv-B` — a cross-conversation approval transfer that compounds
CR-01. There is no benefit to preferring the model-supplied id: it is only ever expected to equal
`pending.ConversationID`.

**Fix:** Use `pending.ConversationID` unconditionally (drop the `rc.ConversationID` primary), or
verify `rc.ConversationID == pending.ConversationID` and return an error on mismatch. The comment
at `:399-401` already asserts they are equal — enforce it instead of trusting the model.

#### WR-03: No test exercises the adversarial `ask_user` relay (mismatched/benign question) — the security property is unverified

**File:** `cmd/aura/gateway_approval_roundtrip_test.go:161-201`, `internal/gateway/approve_test.go:176-190`

**Issue:** Every approval test drives a cooperative "model": the round-trip client copies the
gateway question + `resume_context` **verbatim** (`scriptRoundTripTurn` / `extractApprovalRelay`),
and `TestApproveIsHostSideOnly` only tamper-tests *tool args*, not the relay. The property that
actually matters for this security-critical gate — "the operator cannot be shown a question that
differs from the action being authorized" — is never asserted, which is why CR-01 shipped green and
`35-VERIFICATION.md` over-claims "No model self-approval." Per the `golang-testing` discipline,
an authorization boundary needs an explicit negative/adversarial case.

**Fix:** Add a test where the relayed `ask_user` carries the real `resume_context` but a **different**
question, and assert the resume hook records nothing and the re-drive stays `Approve` (withheld).
After the CR-01 fix, this test should pass; today it would demonstrate the bypass.

### Info

#### IN-01: `GatewayApprovals.Peek` is dead production code (used only by tests)

**File:** `internal/gateway/approvals.go:81-91`

**Issue:** `Peek` is a public method with no production caller (only `approvals_test.go` references
it; grep confirms no non-test use). `ShellApprovals` — the pattern this file mirrors — has no `Peek`.
Under the project's "dead-code removal on touch" rule (CLAUDE.md), a newly-added unused export
should not ship.

**Fix:** Remove `Peek`, or if it is only a test affordance, move the presence check into the test
package. If the CR-01 fix reuses it internally, keep it and add the production caller.

#### IN-02: `gatewayArgsFingerprint(rawArgs)` is computed twice per approval-required result

**File:** `internal/gateway/approve.go:127` and `:187` (via `gatewayApprovalContext`)

**Issue:** `gatewayApprovalRequiredResult` computes `fp := gatewayArgsFingerprint(rawArgs)` at
`:127`, then calls `gatewayApprovalContext` which recomputes the identical `sha256` at `:187`. The
values cannot diverge (pure function, same input), so this is a minor DRY/redundant-hash nit, not a
correctness issue.

**Fix:** Pass the already-computed `fp` into `gatewayApprovalContext` (and, once CR-01 is
implemented, reuse the same `fp` for the `Challenge` record) so the fingerprint has a single
computation site.

---

_Reviewed: 2026-07-04T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
