package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/obs"
)

var resumeBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/internal/runner", obs.BoundaryConfig{
	Operation: "resume", Count: obs.RunnerResumeCallsID, Duration: obs.RunnerResumeDurationID,
})

// maybeAutoTitle fires the best-effort auto-title worker when the conversation has
// reached seq>=3 and is still untitled (D-A5-01 / Req#9). The worker outlives the
// turn ctx (WithoutCancel — the turn ctx dies when Turn returns) but is bounded by
// titleTimeout and tracked by the Runner-owned WaitGroup so Stop joins it
// (goleak-clean). Errors NEVER block chat: a failed title leaves the column NULL.
func (r *Runner) maybeAutoTitle(turnCtx context.Context, convID string, history []llm.Message) {
	conv, err := r.Conv.Get(turnCtx, convID)
	if err != nil || conv.TitleSet {
		return // already titled (or unreadable) — nothing to do
	}
	n, err := r.Conv.CountTurns(turnCtx, convID)
	if err != nil || n < autoTitleMinSeq {
		return
	}

	// WR-03: the worker owns a defensive snapshot of history. The caller's slice
	// header is shared with buildAgent/Turn; copying here removes the implicit
	// "nobody mutates history after maybeAutoTitle returns" coupling so a future
	// in-place mutation cannot race the title worker.
	hist := append([]llm.Message(nil), history...)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ctx := context.WithoutCancel(turnCtx) // load-bearing: turnCtx cancels on Turn return
		ctx, cancel := context.WithTimeout(ctx, r.titleTimeout)
		defer cancel()
		title, gerr := conversations.GenerateTitle(ctx, r.client, r.cfg.Model, hist)
		if gerr != nil || title == "" {
			return // best-effort: leave the title NULL
		}
		_ = r.Conv.SetTitleIfNull(ctx, convID, title)
	}()
}

// declinedContent is the RoleTool body injected when the user declines a pause
// (D-A3-01): the model sees a benign "declined" answer and adapts/continues.
const declinedContent = "user declined to answer"

// cancelledContent is the RoleTool body injected for every open ask_user call when
// the user cancels the turn (D-A3-01 / CR-01). persistPause already wrote the
// assistant ask_user tool_call turn; cancel must answer each dangling tool_call so
// the rehydrated history stays wire-valid (no assistant tool_call without a
// matching tool response). The caller then auto-resolves the paused_states rows and
// treats the cancel as a turn termination (no further Turn(nil) is driven).
const cancelledContent = "user cancelled the request"

// SubmitAnswer resolves ONE pending pause with the three-action model (D-A3-01) and
// injects the answer into the conversation history so the next Turn(convID, nil)
// drives a fresh round over a wire-valid history (SC-4). It returns the remaining
// unresolved-pending count so the caller knows when remaining==0 to continue.
//
//   - accept  → the answer is injected as RoleTool{ToolCallID:<original>, Content}.
//   - decline → a "user declined" RoleTool is injected so the model adapts.
//   - cancel  → the turn is aborted via the Stop auto-resolve path (no injection);
//     the caller should treat a cancel as a turn termination.
func (r *Runner) SubmitAnswer(ctx context.Context, token string, resp ResponseInput) (_ ResolveDirective, err error) {
	ctx, end := resumeBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	pending, err := r.pause.GetByToken(ctx, token)
	if err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer: %w", err)
	}
	if resp.Action == askuser.ActionCancel {
		return r.cancelConversation(ctx, pending.ConversationID)
	}

	// Claim the pause AND append its answer turn in ONE cross-store tx (D-03/LOOP-03).
	// MarkResumed's RowsAffected==0 gate returns ErrPauseNotFound for an unknown or
	// already-resumed token, so a duplicate resume claims nothing and injects nothing; an
	// AppendTurn failure after the claim rolls the whole tx back, leaving resumed_at IS
	// NULL so the user can retry. The WHERE resumed_at IS NULL conditional update IS the
	// idempotency key (D-06), so the old claimed-without-answer residual is now
	// structurally impossible — no more split MarkResumed → injectAnswer.
	claim := ResumeClaim{Token: token, Answer: toResumeAnswer(resp), Turn: r.answerTurn(pending, resp)}
	if err := r.resumeCommitter.CommitResume(ctx, claim); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer: %w", err)
	}
	if err := r.applyResumeHook(ctx, pending, resp); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer: %w", err)
	}
	remaining, err := r.remainingPending(ctx, pending.ConversationID)
	if err != nil {
		return ResolveDirective{}, err
	}
	return classifyResolve(pending, resp.Action, remaining), nil
}

// SubmitAnswers resolves MANY pending pauses in ONE cross-store tx (D-04/LOOP-02): it
// claims ALL pauses (sorted-token, deadlock-free) then appends ALL answers, replacing the
// pre-34-06 inject-first bug (which appended every answer BEFORE the batch claim). Cancel
// actions short-circuit to the Stop auto-resolve path for the whole conversation. Returns
// the remaining count.
func (r *Runner) SubmitAnswers(ctx context.Context, answers map[string]ResponseInput) (remaining int, err error) {
	ctx, end := resumeBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	if len(answers) == 0 {
		return 0, nil
	}
	// Resolve each token up-front so we know the conversation + tool_call_id for the
	// answer turn, and detect a cancel.
	pendings := make(map[string]askuser.Pending, len(answers))
	var convID string
	for token := range answers {
		p, err := r.pause.GetByToken(ctx, token)
		if err != nil {
			return 0, fmt.Errorf("submit answers: %w", err)
		}
		pendings[token] = p
		convID = p.ConversationID
		if answers[token].Action == askuser.ActionCancel {
			directive, err := r.cancelConversation(ctx, p.ConversationID)
			if err != nil {
				return 0, err
			}
			return directive.Remaining, nil
		}
	}

	// Claim-all-then-append-all in one tx: a duplicate/concurrent batch serializes on the
	// conditional update and the whole tx rolls back → exactly one answer per pause, no
	// orphan RoleTool turns; the loser gets ErrPauseNotFound (D-04/D-06).
	claims := make([]ResumeClaim, 0, len(answers))
	for token, resp := range answers {
		claims = append(claims, ResumeClaim{Token: token, Answer: toResumeAnswer(resp), Turn: r.answerTurn(pendings[token], resp)})
	}
	if err := r.resumeCommitter.CommitResumeBatch(ctx, claims); err != nil {
		return 0, fmt.Errorf("submit answers: %w", err)
	}
	for token, resp := range answers {
		if err := r.applyResumeHook(ctx, pendings[token], resp); err != nil {
			return 0, fmt.Errorf("submit answers: %w", err)
		}
	}
	return r.remainingPending(ctx, convID)
}

func (r *Runner) applyResumeHook(ctx context.Context, pending askuser.Pending, resp ResponseInput) error {
	if r.resumeHook == nil || len(pending.ResumeContext) == 0 {
		return nil
	}
	return r.resumeHook(ctx, pending, resp)
}

// answerTurn builds the RoleTool answer turn keyed by the pause's original tool_call_id
// (SC-4 wire-correctness): decline injects the "user declined" marker; accept/cancel
// inject the supplied content. A scheduled_task_approval pause overrides both with an
// explicit outcome so the resumed model learns the task's new state (see
// scheduledApprovalAnswer). Seq is left 0 — the committer reserves it under the
// conversation row-lock inside its tx (the split fallback lets AppendTurn allocate it).
func (r *Runner) answerTurn(pending askuser.Pending, resp ResponseInput) conversations.AppendTurnParams {
	content := resp.Content
	if resp.Action == askuser.ActionDecline {
		content = declinedContent
	}
	if sc := scheduledApprovalAnswer(pending, resp); sc != "" {
		content = sc
	}
	return conversations.AppendTurnParams{
		ConversationID: pending.ConversationID,
		Role:           llm.RoleTool,
		ToolCallID:     pending.ToolCallID,
		Content:        content,
	}
}

// scheduledApprovalAnswer returns the RoleTool content that tells a resumed model the
// outcome of an on-channel scheduled-task approval, or "" when pending is not one (the
// caller keeps the default content, so ordinary HITL is untouched).
//
// Without it, accepting a model-relayed scheduled_task_approval pause injects only the raw
// button value ("yes"): the operator-origin activation happens in the resume hook as a
// SILENT side-effect, so the resumed model — never told the task is now active — re-runs
// task schedule and relays a fresh approval, looping on every accept (fix-plan 1.7 E2E
// BUG-1B). The explicit answer gives the model closure: accept → active + do-not-reschedule;
// decline → cancelled + do-not-reschedule. The short id (first 8) keeps it bounded and
// leaks no payload.
func scheduledApprovalAnswer(pending askuser.Pending, resp ResponseInput) string {
	if !isScheduledTaskApproval(pending) {
		return ""
	}
	var rc struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(pending.ResumeContext, &rc)
	short := rc.TaskID
	if len(short) > 8 {
		short = short[:8]
	}
	switch resp.Action {
	case askuser.ActionAccept:
		return fmt.Sprintf("Operator APPROVED scheduled task %s. It is now ACTIVE and will fire at its "+
			"scheduled time — the scheduling is already complete. Confirm this to the operator and do "+
			"NOT call task schedule again for it.", short)
	case askuser.ActionDecline:
		return fmt.Sprintf("Operator REJECTED scheduled task %s. It has been cancelled and will not fire. "+
			"Acknowledge the rejection to the operator and do NOT re-schedule it.", short)
	default:
		return ""
	}
}

// injectAnswer appends the RoleTool answer turn directly (NOT through the committer). It
// serves only the cancel path (injectCancelledAnswers): each still-open ask_user
// tool_call is answered so the rehydrated history stays wire-valid before
// AutoResolveForConversation resolves the paused_states rows.
func (r *Runner) injectAnswer(ctx context.Context, pending askuser.Pending, resp ResponseInput) error {
	if err := r.Conv.AppendTurn(ctx, r.answerTurn(pending, resp)); err != nil {
		return fmt.Errorf("inject resume answer: %w", err)
	}
	return nil
}

// cancelConversation aborts the whole turn (D-A3-01): it injects a terminating
// RoleTool answer keyed to EACH still-open ask_user tool_call (CR-01 — persistPause
// already wrote the assistant ask_user call(s), so each must be answered to keep the
// rehydrated history wire-valid) and then auto-resolves every paused_states row. The
// caller treats a cancel as a turn termination — it does NOT drive a fresh
// Turn(convID, nil) afterward. Returns the remaining count (0 after auto-resolve).
func (r *Runner) cancelConversation(ctx context.Context, conversationID string) (ResolveDirective, error) {
	if err := r.injectCancelledAnswers(ctx, conversationID); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer (cancel): %w", err)
	}
	if err := r.pause.AutoResolveForConversation(ctx, conversationID); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer (cancel): %w", err)
	}
	remaining, err := r.remainingPending(ctx, conversationID)
	if err != nil {
		return ResolveDirective{}, err
	}
	return ResolveDirective{Outcome: OutcomeTerminated, Remaining: remaining}, nil
}

func (r *Runner) injectCancelledAnswers(ctx context.Context, conversationID string) error {
	pendings, err := r.pause.ListPending(ctx, conversationID)
	if err != nil {
		return err
	}
	for _, p := range pendings {
		if err := r.injectAnswer(ctx, p, ResponseInput{Action: askuser.ActionCancel, Content: cancelledContent}); err != nil {
			return err
		}
	}
	return nil
}

// PendingFor returns the still-unresolved pauses for a conversation in FIFO order
// (priority DESC, created_at ASC) — the REPL reads it to render each prompt inline.
func (r *Runner) PendingFor(ctx context.Context, conversationID string) ([]askuser.Pending, error) {
	pending, err := r.pause.ListPending(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("pending for %s: %w", conversationID, err)
	}
	return pending, nil
}

// remainingPending returns the count of still-unresolved pendings for a conversation.
func (r *Runner) remainingPending(ctx context.Context, conversationID string) (int, error) {
	pending, err := r.pause.ListPending(ctx, conversationID)
	if err != nil {
		return 0, fmt.Errorf("remaining pending: %w", err)
	}
	return len(pending), nil
}

// toResumeAnswer maps the caller-facing ResponseInput onto the Store's AM-02
// {action, content} payload.
func toResumeAnswer(resp ResponseInput) askuser.ResumeAnswer {
	content := resp.Content
	if resp.Action == askuser.ActionDecline {
		content = declinedContent
	}
	return askuser.ResumeAnswer{Action: resp.Action, Content: content}
}

// Stop terminates the conversation lifecycle (D-A1-06 / Req#11): it auto-resolves
// every orphan pending (zero unresolved rows after) and joins the auto-title
// WaitGroup with a bounded wait so a hung worker cannot wedge shutdown (goleak-clean,
// D-A5-01). The wg.Wait() is the sync point tests hit so goleak sees no leak.
func (r *Runner) Stop(ctx context.Context, convID string) error {
	resolveErr := r.injectCancelledAnswers(ctx, convID)
	if resolveErr == nil {
		resolveErr = r.pause.AutoResolveForConversation(ctx, convID)
	}
	r.evictSessionToolState(convID)
	if !r.waitWorkers(r.stopTimeout) {
		// The drain timed out — surface it, but the auto-resolve already ran.
		if resolveErr != nil {
			return fmt.Errorf("stop %s: auto-resolve: %w (and title workers did not drain in %s)", convID, resolveErr, r.stopTimeout)
		}
		return fmt.Errorf("stop %s: title workers did not drain within %s", convID, r.stopTimeout)
	}
	if resolveErr != nil {
		return fmt.Errorf("stop %s: auto-resolve: %w", convID, resolveErr)
	}
	return nil
}

// evictSessionToolState reclaims the conversation's per-session tool state so a
// long-running `serve` daemon does not leak it as conversations come and go (audit
// R-41 / AP-16). It ranges the tool registry and calls Evict(convID) on every tool
// implementing tools.SessionEvictor (todo_write's list, shell_exec's tracked cwd,
// the shell-approval ledger), THEN evicts the gateway approval ledger — which lives
// OUTSIDE the tool registry, so the registry-ranging loop cannot reach it (Phase-35
// R-41 parity with ShellApprovals.Evict). sessionID == conversationID per D-26. Each
// Evict is idempotent, self-locked, and nil-safe, so this is safe on a conversation
// with no tracked state.
func (r *Runner) evictSessionToolState(convID string) {
	if r.registry != nil {
		for _, t := range r.registry.All() {
			if ev, ok := t.(tools.SessionEvictor); ok {
				ev.Evict(convID)
			}
		}
	}
	r.gateway.EvictSession(convID) // nil-Gateway-safe; the ledger is outside the registry
}

// waitWorkers blocks until the auto-title WaitGroup drains or the timeout elapses,
// returning true on a clean drain. The wg-drain waiter is (re)armed under stopMu: while a
// title worker is still running, stopDone stays non-nil so repeated Stop reuses the SAME
// waiter — a hung worker leaves exactly ONE blocked waiter regardless of how many times
// Stop is called (D-14/LOOP-11/F-045: the old per-call `go wg.Wait()` leaked one waiter
// per Stop). On a clean drain the waiter resets stopDone to nil, so a Stop that runs AFTER
// a title worker was spawned post-drain re-arms a fresh waiter and actually joins it
// (WR-02: the pre-fix one-shot sync.Once closed stopDone permanently, so every later Stop
// read the already-closed channel and returned "drained" while a new worker was in flight).
//
// Scope fence: conversations/sweeper.go's Stop has the analogous per-call-waiter pattern,
// but it is called once at daemon shutdown and F-045 names runner_resume.go only — leave
// it for a Phase-35 pass, do NOT touch sweeper.go here.
func (r *Runner) waitWorkers(timeout time.Duration) bool {
	r.stopMu.Lock()
	if r.stopDone == nil {
		done := make(chan struct{})
		r.stopDone = done
		go func() {
			r.wg.Wait()
			r.stopMu.Lock()
			r.stopDone = nil
			r.stopMu.Unlock()
			close(done)
		}()
	}
	done := r.stopDone
	r.stopMu.Unlock()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
