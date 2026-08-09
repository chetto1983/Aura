// The verify-on-stop wiring: the two halves of the verification evidence ledger the
// runner hands to each per-turn agent.
//
//	verificationHooks  — the WRITE half. VerificationHook records a finished shell
//	                     command as evidence and marks a write tool's workspace edited.
//	verificationLedger — the READ half. The gate at the agent's voluntary-termination
//	                     seam asks it whether the workspace the turn edited has fresh
//	                     passing evidence (agent/llm_agent_verification.go).
//
// Both are per TURN, not per process, because both are scoped to (identity, session)
// and the runner builds a fresh agent per turn. What IS per process is the
// EvidenceStore: it holds the pgxpool, so the composition root builds it once and
// injects it through Deps.
//
// Nil is the disabled state throughout, never a panic: no store (no pool — tests,
// standalone) or no identity on ctx yields a nil ledger and the unmodified
// process-wide hook manager, which the agent reads as "no gate".
package runner

import (
	"context"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
)

// verificationLedger builds the read half for the identity scoped onto ctx.
func (r *Runner) verificationLedger(ctx context.Context) agent.VerificationLedger {
	identityID := identityctx.IdentityID(ctx)
	if r.verificationStore == nil || identityID == "" {
		return nil
	}
	return agent.LedgerAdapter{
		Detector:   agent.FilesystemProjectDetector{},
		Store:      r.verificationStore,
		IdentityID: identityID,
	}
}

// verificationHooks returns the turn's hook manager: the process-wide one plus the
// evidence writer, appended FailOpen. FailOpen is the whole point — the ledger is
// an observer, and a failed write to it must never abort the turn it was watching.
// Register's FailClosed default would do exactly that.
func (r *Runner) verificationHooks(ctx context.Context, sessionID string) *agent.HookManager {
	identityID := identityctx.IdentityID(ctx)
	if r.verificationStore == nil || identityID == "" {
		return r.hookManager
	}
	return r.hookManager.With(&agent.VerificationHook{
		Store:      r.verificationStore,
		IdentityID: identityID,
		SessionID:  sessionID,
	}, agent.FailOpen)
}
