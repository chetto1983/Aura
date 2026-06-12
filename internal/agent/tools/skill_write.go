package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/scoring"
)

// skillNameRe mirrors internal/skills.SanitizeName's grammar (^[a-z0-9-]{1,64}$)
// at the tool boundary so the tools package validates a model-supplied name BEFORE
// forwarding it to the writer, without importing internal/skills (the read-path
// boundary). The downstream chokepoint still re-validates — this is defense in depth
// and a faster, grammar-hinted self-correction for the model.
var skillNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// statusPendingApproval mirrors skills.StatusPendingApproval across the read-path
// boundary (the tools package does not import internal/skills): writeAction reflects
// the writer's returned status to decide whether to pause (still pending) or return a
// normal result (ungated/active in-box — P5).
const statusPendingApproval = "pending_approval"

// validWriteName trims whitespace and enforces the skill-name grammar at the tool
// boundary for every name-bearing write/lifecycle action, so a structurally-invalid
// name (blank, whitespace-only, "../" traversal) never reaches the writer regardless
// of which downstream method it hits. The error carries the grammar hint the schema
// description advertises so the model self-corrects in one step.
func validWriteName(action, name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("skill %s: name is required (lowercase, must match %s)", action, skillNameRe.String())
	}
	if !skillNameRe.MatchString(trimmed) {
		return "", fmt.Errorf("skill %s: invalid name %q (must match %s)", action, trimmed, skillNameRe.String())
	}
	return trimmed, nil
}

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
	// SaveSnippet stages a snippet as pending UNGATED (D-02 — Claude-Code parity, no
	// ask_user ceremony): it routes straight to the live Writer.SaveSnippet (which still
	// validates + runs the injection blocklist on the CODE + computes the RISKY tier +
	// lands pending; it NEVER self-activates). It returns the pending status or an error
	// (a blocklist/validation reject → the model self-corrects, NEVER a pause).
	SaveSnippet(ctx context.Context, name, language, code, description string, needsNetwork, needsWorkspace bool) (status string, err error)
	// Restore unarchives a snippet (archived->active + re-materialize + audit), the
	// inverse of Archive. It returns the resulting status (active) or an error.
	Restore(ctx context.Context, name string) (status string, err error)
	// ArchiveSnippet de-materializes + moves active->archived + audits (SAFE tier, no
	// gate). It returns a status string or an error.
	ArchiveSnippet(ctx context.Context, name string) (status string, err error)
}

// skillAlerter is the optional headless-alert seam (D-26): when a gated mutation is
// proposed in a context with no interactive resume (a swarm worker, a cron job), the
// tool fires an IMMEDIATE alert so the operator learns a self-extension was attempted
// even though it can never self-activate. Nil in the interactive REPL (the ask_user
// pause is the channel) and in unit tests.
type skillAlerter interface {
	AlertPendingSkill(ctx context.Context, name, action string, tier scoring.RiskTier)
}

// skillWriteArgs is the wire shape for the create/update/delete actions plus the
// 18-03 snippet-lifecycle actions (restore/archive/save_snippet), decoded per-action
// from the same raw object the router dispatched. Language/Code are the save_snippet
// fields (NeedsNetwork/NeedsWorkspace ride the frontmatter at the adapter, defaulting
// false for the model path).
type skillWriteArgs struct {
	Action         string `json:"action"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Body           string `json:"body"`
	Always         bool   `json:"always"`
	Language       string `json:"language"`
	Code           string `json:"code"`
	NeedsNetwork   bool   `json:"needs_network"`
	NeedsWorkspace bool   `json:"needs_workspace"`
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
	name, err := validWriteName(string(action), a.Name)
	if err != nil {
		return ToolResult{}, err
	}
	if t.Writer == nil {
		return ToolResult{}, fmt.Errorf("skill %s: no writer is wired in this context", action)
	}

	// The writer validates at the boundary (model path: allowBlocklisted=false). A
	// blocklist hit / validation failure returns here as an error — surfaced as a
	// RoleTool error so the model self-corrects (D-27), never a pause.
	status, err := t.Writer.WriteMutation(ctx, string(action), name, a.Description, a.Body, a.Always)
	if err != nil {
		return ToolResult{}, fmt.Errorf("skill %s %q: %w", action, name, err)
	}
	tier := scoring.ComputeSkillTier(action, a.Body)

	// P5 (2026-06-10): in-box self-extension is ungated — the writer auto-activates a
	// model-authored mutation (container = boundary, Claude-Code parity), returning a
	// non-pending status. Return a NORMAL result, no human pause; this is now the live
	// path for create/update/delete (delete de-materializes immediately).
	if status != statusPendingApproval {
		if t.Alerter != nil {
			t.Alerter.AlertPendingSkill(ctx, name, string(action), tier)
		}
		return NewResult(ctx, fmt.Sprintf("Skill %s %q is now active (status=%s).", action, name, status))
	}

	// Defensive fallback: a still-gated context staged this as pending. Fire the
	// headless alert (D-26) and pause for approval (the model cannot answer — D-03).
	if t.Alerter != nil {
		t.Alerter.AlertPendingSkill(ctx, name, string(action), tier)
	}
	question := fmt.Sprintf(
		"Approve skill %s %q (risk=%s)? It is staged as pending and will NOT take effect until you approve. "+
			"Approve to activate, or decline to discard the pending change.",
		action, name, tier,
	)
	return ToolResult{}, &ErrAwaitingUserInput{
		Question: question,
		Kind:     KindApproval,
		Priority: skillApprovalPriority(tier),
	}
}

// actionSaveSnippet handles action=save_snippet (D-02 — the in-loop snippet-save path,
// UNGATED). It is the architectural INVERSE of writeAction: it decodes the args, requires
// name+language+code, requires the writer seam, calls SaveSnippet (which still validates +
// runs the injection blocklist on the CODE + lands pending — it NEVER self-activates), and
// returns a NORMAL result confirming the pending save. It NEVER returns the
// *ErrAwaitingUserInput sentinel (only ask_user may pause — TestAskUserOnlyPauseConstraint);
// a validation/blocklist reject comes back as a plain tool error so the model self-corrects.
// Future per-identity gating of the save lands with capability_grants (Slice 1.7), not here.
func (t *SkillTool) actionSaveSnippet(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a skillWriteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("skill save_snippet args: %w", err)
	}
	name, err := validWriteName("save_snippet", a.Name)
	if err != nil {
		return ToolResult{}, err
	}
	if a.Language == "" {
		return ToolResult{}, fmt.Errorf("skill save_snippet: language is required (one of python, shell, js)")
	}
	if a.Code == "" {
		return ToolResult{}, fmt.Errorf("skill save_snippet: code is required (the executable snippet body)")
	}
	if t.Writer == nil {
		return ToolResult{}, fmt.Errorf("skill save_snippet: no writer is wired in this context")
	}
	status, err := t.Writer.SaveSnippet(ctx, name, a.Language, a.Code, a.Description, a.NeedsNetwork, a.NeedsWorkspace)
	if err != nil {
		return ToolResult{}, fmt.Errorf("skill save_snippet %q: %w", name, err)
	}
	return NewResult(ctx, fmt.Sprintf(
		"Snippet %q saved as pending (status=%s). Activate it (operator approval) before reuse; "+
			"once active, call action=use to run it by path.", name, status))
}

// actionRestore handles action=restore: it unarchives a snippet (archived->active +
// re-materialize + audit, the inverse of Archive) via the writer seam and returns a
// NORMAL result confirming the restore. Not a pause.
func (t *SkillTool) actionRestore(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	name, err := t.requireWriteName(raw, "restore")
	if err != nil {
		return ToolResult{}, err
	}
	status, rerr := t.Writer.Restore(ctx, name)
	if rerr != nil {
		return ToolResult{}, fmt.Errorf("skill restore %q: %w", name, rerr)
	}
	return NewResult(ctx, fmt.Sprintf(
		"Snippet %q restored (status=%s) — re-materialized into the skills dir; call action=use to run it by path.", name, status))
}

// actionArchive handles action=archive: a SAFE-tier manual de-materialize (active->
// archived + audit, no gate per RESEARCH Pattern 1) via the writer seam. It returns a
// NORMAL result confirming the archive. An over-eager archive is recoverable via
// action=restore. Not a pause.
func (t *SkillTool) actionArchive(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	name, err := t.requireWriteName(raw, "archive")
	if err != nil {
		return ToolResult{}, err
	}
	status, aerr := t.Writer.ArchiveSnippet(ctx, name)
	if aerr != nil {
		return ToolResult{}, fmt.Errorf("skill archive %q: %w", name, aerr)
	}
	return NewResult(ctx, fmt.Sprintf(
		"Snippet %q archived (status=%s) — de-materialized; restore it with action=restore if needed.", name, status))
}

// requireWriteName decodes {name} and requires the writer seam — the shared guard for
// the restore/archive lifecycle handlers (name-only, normal-result actions).
func (t *SkillTool) requireWriteName(raw json.RawMessage, action string) (string, error) {
	var a skillWriteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("skill %s args: %w", action, err)
	}
	name, err := validWriteName(action, a.Name)
	if err != nil {
		return "", err
	}
	if t.Writer == nil {
		return "", fmt.Errorf("skill %s: no writer is wired in this context", action)
	}
	return name, nil
}

// skillApprovalPriority orders a skill-approval pause ahead of routine clarifications
// when several pauses are pending: a Destructive (delete) gate outranks a Risky one.
func skillApprovalPriority(tier scoring.RiskTier) int {
	if tier == scoring.Destructive {
		return 80
	}
	return 60
}
