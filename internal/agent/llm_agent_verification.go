// Verify-on-stop gate: the run loop's half of the verification ledger.
//
// verification_stop.go decides WHAT to say when a turn edited code without fresh
// passing evidence. This file is what the loop contributes: it accumulates the paths
// this run's write tools touched, and it asks that policy at the voluntary
// termination the loop already has a seam for.
//
// It lives outside llm_agent.go for the no-god-class cap: that file keeps only the
// three fields and the call at the seam.
package agent

import (
	"slices"

	"github.com/chetto1983/aura/internal/llm"
)

// verificationMaxAttempts bounds the nudge per run, the way completionAttempts bounds
// the critic gate. Two, the default hermes's build_verify_on_stop_nudge carries: the
// first nudge is what makes the agent run the verification at all, and the second is
// what catches a run that failed and was then walked away from.
const verificationMaxAttempts = 2

// verifyOnStopNudgePrefix leads every nudge BuildVerifyOnStopNudge returns. The nudge
// is injected as a USER-role message, so isAgentNudge matches on this prefix: without
// it the completion critic reads Aura's own injection as the user's request and grades
// the turn against the wrong thing.
const verifyOnStopNudgePrefix = "[System: You edited code in this turn"

// gateVerification returns the follow-up for a voluntary termination that edited code
// without fresh passing verification evidence, and ok=false when there is nothing to
// say. It is deterministic and spends no model call, which is why the loop runs it
// BEFORE the completion critic.
//
// Fail-open by construction: no ledger (no pool, tests, standalone), the gate switched
// off, or the attempt budget spent all return ok=false — a ledger outage can never
// wedge a turn.
func (a *LlmAgent) gateVerification() (string, bool) {
	if a.ledger == nil || a.verificationAttempts >= verificationMaxAttempts || !verifyOnStopEnabled() {
		return "", false
	}
	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger:       a.ledger,
		SessionID:    a.sessionID,
		ChangedPaths: a.editedPaths,
		Attempts:     a.verificationAttempts,
		MaxAttempts:  verificationMaxAttempts,
	})
	if !ok {
		return "", false
	}
	a.verificationAttempts++
	return nudge, true
}

// recordEditedPath accumulates the path one dispatched call edited. It reads the
// argument name from writeToolPathArgs — the same map the ledger hook writes from — so
// the gate can never disagree with the ledger about which tools edit and where they
// say so.
//
// Called only from the SERIAL result loop in dispatch (like sideEffected and
// promoteFromMeta), so the slice needs no lock while a batch runs concurrently.
func (a *LlmAgent) recordEditedPath(call llm.ToolCall) {
	path, ok := writeToolPath(call)
	if !ok || slices.Contains(a.editedPaths, path) {
		return
	}
	a.editedPaths = append(a.editedPaths, path)
}
