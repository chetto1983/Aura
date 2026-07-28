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
// governance, plan 11-05). Each action validates at the write boundary (NFKC+blocklist
// HARD reject for the model — D-27, no escape) and the mutation TAKES EFFECT: the
// writer lands it active + materialized + audited before the call returns (amendment
// #97). A validation or blocklist failure comes back as a plain tool error so the model
// self-corrects; nothing here ever pauses the turn.
//
// The optional Alerter still fires (D-26), now purely as an operator notification: a
// human learns a self-extension happened. It does not gate anything.
//
// The tools package stays free of an internal/skills import: skillWriter is a
// consumer-declared seam (golang-structs-interfaces) the live *skills.Writer
// satisfies through a thin adapter wired at the composition root (cmd/aura).

// skillWriter is the consumer-declared write seam the skill tool's create/update/
// delete handlers dispatch against. The live internal/skills.Writer satisfies it
// through an adapter wired at registration, keeping internal/agent/tools free of an
// internal/skills import (the boundary 11-02 established for the read path).
type skillWriter interface {
	// WriteMutation applies a create/update/delete and returns the resulting status.
	// name is the skill name; for delete, description/body are empty. An error means
	// nothing was written (a blocklist hit, a validation failure, or an IO/DB failure).
	WriteMutation(ctx context.Context, action string, name, description, body string, always bool) (status string, err error)
	// SaveSnippet writes a snippet and materializes it, so it is runnable by path when
	// this returns. It still validates and runs the injection blocklist on the CODE; a
	// reject comes back as an error the model self-corrects from, never a pause.
	SaveSnippet(ctx context.Context, name, language, code, description string, needsNetwork, needsWorkspace bool) (status string, err error)
	// Restore unarchives a snippet (archived->active + re-materialize + audit), the
	// inverse of Archive. It returns the resulting status (active) or an error.
	Restore(ctx context.Context, name string) (status string, err error)
	// ArchiveSnippet de-materializes + moves active->archived + audits (SAFE tier). It
	// returns a status string or an error.
	ArchiveSnippet(ctx context.Context, name string) (status string, err error)
}

// skillAlerter is the optional alert seam (D-26): the tool fires an IMMEDIATE alert so
// the operator LEARNS a self-extension happened — in a headless context (a swarm
// worker, a cron job) that is the only channel they have. It is a notification, not a
// gate: the mutation is already applied when it fires. Nil in unit tests.
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

// actionCreate handles action=create: a model-authored new skill. It validates and
// writes; the skill is usable on this same turn. A blocklist/validation failure is
// returned as a tool error the model self-corrects from (D-27).
func (t *SkillTool) actionCreate(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return t.writeAction(ctx, raw, scoring.SkillCreate)
}

// actionUpdate handles action=update: a model-authored revision of an existing skill.
// The revision replaces the old body on disk and in the export dir; the next turn's
// manifest carries it.
func (t *SkillTool) actionUpdate(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return t.writeAction(ctx, raw, scoring.SkillUpdate)
}

// actionDelete handles action=delete: a Destructive-tiered removal. It requires only a
// name; the writer de-materializes the skill and removes it.
//
// The tier still matters OUTSIDE this file: under a strict profile the gateway PEP
// withholds a model-issued delete before the tool runs at all (gateway/decide.go). So
// "takes effect immediately" is true of this code path and not of the deployed
// appliance — a difference recorded in the amendment, not papered over here.
func (t *SkillTool) actionDelete(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return t.writeAction(ctx, raw, scoring.SkillDelete)
}

// writeAction is the shared create/update/delete flow: decode the args, require the
// writer seam, call it (validation + blocklist rejects come back as plain errors the
// model self-corrects from), fire the operator alert, and return a normal result. It
// never pauses the turn.
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
	if _, err := t.Writer.WriteMutation(ctx, string(action), name, a.Description, a.Body, a.Always); err != nil {
		return ToolResult{}, fmt.Errorf("skill %s %q: %w", action, name, err)
	}
	// The alert (D-26) is an OPERATOR notification, not a gate: it tells a human that a
	// self-extension happened, and the turn continues either way.
	if t.Alerter != nil {
		t.Alerter.AlertPendingSkill(ctx, name, string(action), scoring.ComputeSkillTier(action, a.Body))
	}
	if action == scoring.SkillDelete {
		return NewResult(ctx, fmt.Sprintf("Skill %q deleted.", name))
	}
	return NewResult(ctx, fmt.Sprintf("Skill %s %q is active now — you can use it on this turn.", action, name))
}

// actionSaveSnippet handles action=save_snippet (D-02 — the in-loop snippet-save path).
// It decodes the args, requires name+language+code and the writer seam, and calls
// SaveSnippet, which validates, runs the injection blocklist on the CODE, writes and
// materializes — so the snippet is runnable by path when this returns. It NEVER returns
// the *ErrAwaitingUserInput sentinel (only ask_user may pause —
// TestAskUserOnlyPauseConstraint); a validation/blocklist reject comes back as a plain
// tool error so the model self-corrects.
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
		"Snippet %q saved and ready (status=%s). Call action=use to get its path and interpreter, "+
			"then run it — no further step is needed.", name, status))
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

// ApprovalPriority is the shared security-approval FIFO priority (single source of
// truth, reused by the gateway PEP's routeApprove so the gateway does NOT re-derive
// it): a Destructive gate outranks a Risky one, both ahead of routine clarifications.
func ApprovalPriority(tier scoring.RiskTier) int {
	if tier == scoring.Destructive {
		return 80
	}
	return 60
}
