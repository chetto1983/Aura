// worker_pause_sweep.go is the per-worker pause TTL sweep (51-06b Task 3, SWARM-06,
// D-08 extended to the queue row): a background delegation worker's pause that the
// operator never answered expires with a READABLE trace in the origin conversation and
// takes its parked aura.ingestion_jobs row with it -- the pause and the parked row were
// created together (Task 1's one-transaction PauseAndPark) and must die together, or the
// row stays parked forever waiting for a human who never came.
//
// A sibling of approval_expiry.go, not a modification of it: that file is
// approval-specific by name and doc comment (RESEARCH.md File Targets). Same shape --
// list due, loop, commit each outcome atomically with its trace, skip a vanished row,
// return (expired, err) -- with one deliberate difference in WHERE the trace lands. An
// approval's expiry answer is a RoleTool turn keyed by the pause's tool_call_id, because
// that assistant tool_call turn is in the SAME conversation. A worker's ask_user call
// lives in the worker's OWN persisted history (DelegationResumeState.History), not in
// the origin conversation, so a RoleTool turn there would be an orphan with no matching
// assistant tool_calls entry -- wire-invalid. The trace is therefore a plain assistant
// turn, exactly the shape plan 51-10's DelegationDelivery used to surface the question
// in the first place.
package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// ExpiredWorkerPause is one background worker's pause past AURA_ASKUSER_PAUSE_TTL_SEC
// together with the queue row it parked. JobID is the aura.ingestion_jobs row (also the
// pause's OwningWorkerID, D-13's level identity); IdentityID scopes the expiry
// transaction. Pause carries the token, the D-12 fence (PendingActionID), the origin
// ConversationID and the Question the trace names.
type ExpiredWorkerPause struct {
	JobID      string
	IdentityID string
	Pause      askuser.Pending
}

// WorkerPauseLister lists identityID's parked jobs whose pause is still unanswered
// and older than cutoff. *documents.PostgresIngestionJobStore backs it in production
// (adapted at cmd/aura); the read crosses from aura.ingestion_jobs into
// aura.paused_states on the pause TOKEN this park cycle minted, never on
// owning_worker_id alone (a job can pause more than once across its lifetime).
type WorkerPauseLister interface {
	ListExpiredWorkerPauses(ctx context.Context, identityID string, cutoff time.Time, limit int) ([]ExpiredWorkerPause, error)
}

// WorkerPauseExpiry is ONE expiry outcome the expirer commits atomically: the fenced
// pause claim (Claim.Token + Claim.ExpectActionID, resolved with Claim.Answer), the
// readable trace (Claim.Turn, an assistant turn in the origin conversation), and the
// parked queue row's terminal resolution (JobID under IdentityID, with ErrorMessage
// as the row's error_message).
type WorkerPauseExpiry struct {
	Claim        ResumeClaim
	JobID        string
	IdentityID   string
	ErrorMessage string
}

// WorkerPauseExpirer commits one WorkerPauseExpiry in ONE transaction: if any leg
// fails, none of the three is visible. It returns askuser.ErrPauseNotFound (wrapped)
// when the pause has vanished or was answered first -- the sweep skips that row and
// continues, exactly as ExpirePendingApprovals does for a lost claim.
// *PoolWorkerPauseExpirer (worker_pause_expirer.go) is the production implementation.
type WorkerPauseExpirer interface {
	ExpireWorkerPause(ctx context.Context, expiry WorkerPauseExpiry) error
}

// WorkerPauseSweepDeps is the pair of seams the sweep runs over. They are passed per
// call rather than hung on the Runner: the Runner owns HITL policy (what an expiry
// says and how it is fenced), not the ingestion queue, and threading a queue store
// through runner.Deps for one sweep would widen every Runner constructor and fake.
type WorkerPauseSweepDeps struct {
	Lister  WorkerPauseLister
	Expirer WorkerPauseExpirer
}

// expiredWorkerPauseContent is the resumed_answer content a swept worker pause
// carries -- distinct from expiredApprovalContent so a reader of aura.paused_states
// can tell which sweep closed a row without joining anything.
const expiredWorkerPauseContent = "expired: the operator never answered"

// ExpireWorkerPauses expires identityID's background-worker pauses older than
// now-ttl and resolves their parked queue rows, at most limit per pass. A ttl <= 0
// disables expiry entirely (the shipped AURA_ASKUSER_PAUSE_TTL_SEC precedent). It
// returns the number of pauses actually expired this pass; on an expirer error it
// returns the count so far and the error, so a partial sweep is never reported as
// a full one. Idempotent by construction: an expired pause has resumed_at set and
// its row is no longer awaiting_input, so a second pass lists nothing.
//
// The receiver carries no state the sweep reads; the method lives on *Runner because
// the expiry's wording and fencing are HITL policy the Runner owns (its sibling
// ExpirePendingApprovals is the precedent), and cmd/aura wires both sweeps off the
// same *Runner.
func (*Runner) ExpireWorkerPauses(ctx context.Context, deps WorkerPauseSweepDeps, identityID string, now time.Time, ttl time.Duration, limit int) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	if deps.Lister == nil || deps.Expirer == nil {
		return 0, fmt.Errorf("expire worker pauses: sweep is not configured")
	}
	if identityID == "" {
		return 0, fmt.Errorf("expire worker pauses: identity is required")
	}
	due, err := deps.Lister.ListExpiredWorkerPauses(ctx, identityID, now.Add(-ttl), limit)
	if err != nil {
		return 0, fmt.Errorf("expire worker pauses: %w", err)
	}
	expired := 0
	for _, pause := range due {
		if err := deps.Expirer.ExpireWorkerPause(ctx, workerPauseExpiry(pause)); err != nil {
			if errors.Is(err, askuser.ErrPauseNotFound) {
				continue
			}
			return expired, fmt.Errorf("expire worker pause %s: %w", pause.Pause.Token, err)
		}
		expired++
	}
	return expired, nil
}

// workerPauseExpiry turns one due pause into the three-legged outcome the expirer
// commits. The trace names WHICH worker asked and WHAT went unanswered, so the agent
// can truthfully report what did NOT happen (the worker was stopped, its work was not
// completed); the queue row's error_message carries the same fact for an operator
// reading aura.ingestion_jobs directly.
func workerPauseExpiry(p ExpiredWorkerPause) WorkerPauseExpiry {
	trace := fmt.Sprintf("Background worker %s asked: %q -- %s before AURA_ASKUSER_PAUSE_TTL_SEC elapsed. The worker was stopped and its task was NOT completed.",
		p.JobID, p.Pause.Question, expiredWorkerPauseContent)
	return WorkerPauseExpiry{
		Claim: ResumeClaim{
			Token:  p.Pause.Token,
			Answer: askuser.ResumeAnswer{Action: askuser.ActionExpired, Content: expiredWorkerPauseContent},
			Turn: conversations.AppendTurnParams{
				ConversationID: p.Pause.ConversationID,
				Role:           llm.RoleAssistant,
				Content:        trace,
			},
			ExpectActionID: p.Pause.PendingActionID,
		},
		JobID:        p.JobID,
		IdentityID:   p.IdentityID,
		ErrorMessage: fmt.Sprintf("%s: %s", expiredWorkerPauseContent, p.Pause.Question),
	}
}
