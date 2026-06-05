package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/scoring"
)

// This file wires the skill tool's WRITE actions create|update|delete (Slice 7c
// governance, plan 11-05), replacing the "not yet wired" placeholders from 11-02.
// Each action validates at the write boundary (NFKC+blocklist HARD reject for the
// model — D-27, no escape), gates via scoring, lands the mutation in pending/ via
// the writer seam, and PAUSES the turn via the *ErrAwaitingUserInput sentinel
// (D-02). There is NO model-facing approve action (D-03): activation happens only
// via an ask_user resume (skills.ResumeHandler → Writer.Activate) or the
// `aura skills approve` CLI. In a headless context (no interactive resume) the
// mutation stays pending and an optional Alerter fires (D-26) — the writer NEVER
// self-activates, so a model can never extend itself unattended.
//
// The tools package stays free of an internal/skills import: skillWriter is a
// consumer-declared seam (golang-structs-interfaces) the live *skills.Writer
// satisfies through a thin adapter wired at the composition root (cmd/aura).

// skillWriter is the consumer-declared write seam the skill tool's create/update/
// delete handlers dispatch against. The live internal/skills.Writer satisfies it
// through an adapter wired at registration, keeping internal/agent/tools free of an
// internal/skills import (the boundary 11-02 established for the read path).
//
// WriteMutation validates (model path: allowBlocklisted=false, hard-reject), gates,
// and lands the skill in pending/ recording the D-29 pending audit tuple; it returns
// the status string ("pending_approval" for the gated v1 actions) and NEVER
// activates. A blocklist hit surfaces as a plain error (NOT a pause) so the model
// self-corrects. The tier is computed by scoring; the seam takes the action enum.
type skillWriter interface {
	// WriteMutation lands a create/update/delete mutation in pending/ + audit. name
	// is the skill name; for delete, frontmatter/body are empty. It returns the
	// resulting status (StatusPendingApproval) or an error (a blocklist hit, a
	// validation failure, or an IO/DB failure).
	WriteMutation(ctx context.Context, action string, name, description, body string, always bool) (status string, err error)
}

// skillAlerter is the optional headless-alert seam (D-26): when a gated mutation is
// proposed in a context with no interactive resume (a swarm worker, a cron job), the
// tool fires an IMMEDIATE alert so the operator learns a self-extension was attempted
// even though it can never self-activate. Nil in the interactive REPL (the ask_user
// pause is the channel) and in unit tests.
type skillAlerter interface {
	AlertPendingSkill(ctx context.Context, name, action string, tier scoring.RiskTier)
}

// skillWriteArgs is the wire shape for the create/update/delete actions, decoded
// per-action from the same raw object the router dispatched.
type skillWriteArgs struct {
	Action      string `json:"action"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Always      bool   `json:"always"`
}

// actionCreate handles action=create: a model-authored new skill. It validates +
// gates + lands pending via the writer, then PAUSES via ask_user for human approval
// (D-02/D-03). A blocklist/validation failure is returned as a tool error (the model
// self-corrects, D-27), NOT a pause.
func (t *SkillTool) actionCreate(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return t.writeAction(ctx, raw, scoring.SkillCreate)
}

// actionUpdate handles action=update: a model-authored revision of an existing
// skill. The pending revision is gated while the old version keeps serving; the
// gate (resume/CLI) shows the operator the change before activation (D-05). Same
// validate→gate→pending→pause flow as create.
func (t *SkillTool) actionUpdate(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return t.writeAction(ctx, raw, scoring.SkillUpdate)
}

// actionDelete handles action=delete: a Destructive-tiered removal. It requires only
// a name; the writer de-materializes + archives the skill behind the gate. It pauses
// for approval like create/update (delete is the only Destructive action).
func (t *SkillTool) actionDelete(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return t.writeAction(ctx, raw, scoring.SkillDelete)
}

// writeAction is the shared create/update/delete flow. It decodes the args, requires
// the writer seam to be wired, computes the tier for the message + alert, calls the
// writer (which validates + gates + lands pending; a blocklist hit comes back as a
// plain error → the model self-corrects, NOT a pause), and on a successful pending
// write returns the *ErrAwaitingUserInput sentinel so the agent pauses the turn for
// human approval. There is no model-facing approve (D-03) — this pause is the ONLY
// model-side step; activation is the resume handler's / CLI's job.
func (t *SkillTool) writeAction(ctx context.Context, raw json.RawMessage, action scoring.SkillAction) (ToolResult, error) {
	var a skillWriteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("skill %s args: %w", action, err)
	}
	if a.Name == "" {
		return ToolResult{}, fmt.Errorf("skill %s: name is required", action)
	}
	if t.Writer == nil {
		return ToolResult{}, fmt.Errorf("skill %s: no writer is wired in this context", action)
	}

	// The writer validates at the boundary (model path: allowBlocklisted=false). A
	// blocklist hit / validation failure returns here as an error — surfaced as a
	// RoleTool error so the model self-corrects (D-27), never a pause.
	status, err := t.Writer.WriteMutation(ctx, string(action), a.Name, a.Description, a.Body, a.Always)
	if err != nil {
		return ToolResult{}, fmt.Errorf("skill %s %q: %w", action, a.Name, err)
	}

	tier := scoring.ComputeSkillTier(action, a.Body)

	// Headless contexts (swarm worker, cron job) cannot drive an interactive resume:
	// fire the immediate alert (D-26) so the operator learns of the attempt. The
	// mutation is already pending and CANNOT self-activate (the writer never calls
	// Activate). The interactive REPL leaves Alerter nil — the ask_user pause is its
	// channel.
	if t.Alerter != nil {
		t.Alerter.AlertPendingSkill(ctx, a.Name, string(action), tier)
	}

	// Pause the turn for human approval. The pause question frames the gated mutation
	// + its tier; the model cannot answer it (D-03) — only a human resume or the CLI
	// activates. We return the sentinel so llm_agent_pause.go suspends the turn.
	question := fmt.Sprintf(
		"Approve skill %s %q (risk=%s)? It is staged as pending and will NOT take effect until you approve. "+
			"Approve to activate, or decline to discard the pending change.",
		action, a.Name, tier,
	)
	_ = status // StatusPendingApproval — the pause itself communicates the gate
	return ToolResult{}, &ErrAwaitingUserInput{
		Question: question,
		Kind:     KindApproval,
		Priority: skillApprovalPriority(tier),
	}
}

// skillApprovalPriority orders a skill-approval pause ahead of routine clarifications
// when several pauses are pending: a Destructive (delete) gate outranks a Risky one.
func skillApprovalPriority(tier scoring.RiskTier) int {
	if tier == scoring.Destructive {
		return 80
	}
	return 60
}
