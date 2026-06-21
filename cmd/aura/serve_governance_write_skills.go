package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serve_governance_write_skills.go wires the composition-root concrete adapter for the
// Phase-29 SKILLS WRITE surface (SKW-01/02/03). The agui consumer declares the narrow
// SkillsWriteProvider seam (governance_write_seam.go); skillsWriteAdapter satisfies it over
// the EXISTING Phase-11 primitives — the live Writer (the pending+audit sink), the Task-1
// Installer (the npx-skills stage→validate→WriteInstallPending transport), plus the
// Phase-25 askuser.Store + conversations.Store so the install can mint its operator-origin
// approval pause.
//
// D-13 (Option A — the install→approval bridge): a cockpit install stages through the
// Installer to pending/ (one skill_audit install row, never self-activated), then mints an
// OPERATOR-ORIGIN ask_user pause via askuser.Store.Insert carrying Kind=approval +
// ResumeContext={type:"skill_approval", skill_name}. That is the SAME pause shape a
// model-proposed skill-approval mints, so it surfaces in the SAME unified /api/approvals
// queue (source-agnostic ListPendingAll) and resolves through the SAME source-agnostic
// Runner.SubmitAnswers → ResumeHandler.Resume → Writer.Activate (accept) / DiscardPending
// (decline/cancel) bridge — no new approval or activation logic.
//
// This is the SECOND legitimate writer of aura.paused_states (T-04-19 widening), reachable
// ONLY behind RequireCapability(governance.write): no model/agent/unauthenticated caller can
// mint the pause (the agent stays name-gated to ask_user). The install NEVER auto-activates;
// resolution is operator-only.

// governanceConversationID is the deterministic UUID of the dedicated, hidden conversation
// the operator-origin install pauses are parented to. aura.paused_states.conversation_id is
// plain text (no FK), but the resume path (Runner.SubmitAnswers → injectAnswer →
// Conv.AppendTurn) writes a conversation_turns row, which DOES carry an FK to
// aura.conversations(id). Parenting the pause to a real (provisioned-on-first-install)
// conversation keeps that resume-side append wire-valid. It is created idempotently the
// first time a cockpit install mints a pause.
const governanceConversationID = "00000000-0000-0000-0000-0000000900a1"

// governanceInstallToolCallID is the synthetic tool_call_id stamped on the operator-origin
// pause. The resume path injects a RoleTool answer keyed to it; it never collides with a
// model-emitted tool_call_id (those are LLM-provider ids, never this fixed sentinel).
const governanceInstallToolCallID = "governance-skill-install"

// skillsWriteAdapter satisfies agui.SkillsWriteProvider over the live Phase-11 + Phase-25
// primitives. installer stages + validates + hands off to the pending+audit sink; writer is
// the lifecycle sink (restore/archive/create/update/delete); pause mints the operator-origin
// approval pause; conv provisions the governance conversation for the resume-side FK; the
// localIdentityID parents that conversation.
type skillsWriteAdapter struct {
	installer       *skills.Installer
	writer          *skills.Writer
	pause           *askuser.Store
	conv            *conversations.Store
	localIdentityID string
	model           string
}

// skillResumeContext is the typed ResumeContext the operator-origin pause carries. It is the
// EXACT shape newSkillResumeHook (serve_adapters.go) dispatches on, so a cockpit install
// resolves identically to a model-proposed skill-approval pause.
type skillResumeContext struct {
	Type      string `json:"type"`
	SkillName string `json:"skill_name"`
}

// Install stages the fetched skill through the Task-1 Installer (→ pending/, one skill_audit
// install row, never self-activated) and mints the operator-origin approval pause so the
// install surfaces in /api/approvals. It returns the SkillsInstallInfo (RISKY label, five
// checklist items, source/hash/preview/destination, the approval token). An empty source is
// rejected with ErrSkillInvalidInput → 400.
func (a skillsWriteAdapter) Install(ctx context.Context, actor, source string) (agui.SkillsInstallInfo, error) {
	if source == "" {
		return agui.SkillsInstallInfo{}, fmt.Errorf("%w: install source is empty", agui.ErrSkillInvalidInput)
	}
	info, err := a.installer.Install(ctx, source, skills.AuditActor{ActorID: "operator", IdentityID: actor})
	if err != nil {
		// An invalid structure / blocklist hit / empty source from the Installer is a
		// client-correctable input, surfaced as a safe 400.
		if errors.Is(err, skills.ErrInvalidStructure) || errors.Is(err, skills.ErrBlocklisted) {
			return agui.SkillsInstallInfo{}, fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
		}
		return agui.SkillsInstallInfo{}, err
	}

	token, err := a.mintApprovalPause(ctx, info.Name, info.Source, info.Preview, info.RiskTier)
	if err != nil {
		return agui.SkillsInstallInfo{}, err
	}

	return agui.SkillsInstallInfo{
		Name:          info.Name,
		Source:        info.Source,
		ContentHash:   info.ContentHash,
		Preview:       info.Preview,
		Destination:   info.Destination,
		RiskTier:      info.RiskTier,
		Status:        info.Status,
		ApprovalToken: token,
		Checklist:     toSkillsChecklist(info.Checklist),
	}, nil
}

// mintApprovalPause provisions the governance conversation (idempotent) and inserts the
// operator-origin ask_user pause (D-13) carrying the skill_approval ResumeContext. It returns
// the pause token so the UI can deep-link to the /api/approvals entry. This is the ONLY
// askuser.Store.Insert call outside the Runner — and it is reachable only behind
// RequireCapability(governance.write) at the route (T-04-19 widening, capability-scoped).
func (a skillsWriteAdapter) mintApprovalPause(ctx context.Context, name, source, preview, riskTier string) (string, error) {
	if err := a.ensureGovernanceConversation(ctx); err != nil {
		return "", err
	}
	rc, err := json.Marshal(skillResumeContext{Type: "skill_approval", SkillName: name})
	if err != nil {
		return "", fmt.Errorf("skill install: marshal resume context: %w", err)
	}
	token := uuid.NewString()
	question := fmt.Sprintf(
		"Approve RISKY skill install %q from %q? (%s supply-chain input — container-isolated, approval-gated, Writer-validated)",
		name, source, riskTier,
	)
	if err := a.pause.Insert(ctx, askuser.InsertParams{
		Token:          token,
		ConversationID: governanceConversationID,
		Kind:           tools.KindApproval,
		Question:       question,
		Priority:       0,
		ToolCallID:     governanceInstallToolCallID,
		ResumeContext:  rc,
	}); err != nil {
		return "", fmt.Errorf("skill install: mint approval pause: %w", err)
	}
	return token, nil
}

// ensureGovernanceConversation provisions the dedicated governance conversation the first
// time a cockpit install mints a pause. It is idempotent: an existing row (or a concurrent
// create that lost the unique-violation race) is a benign no-op. The resume-side
// Conv.AppendTurn FK to aura.conversations(id) is satisfied by this row.
func (a skillsWriteAdapter) ensureGovernanceConversation(ctx context.Context) error {
	if _, err := a.conv.Get(ctx, governanceConversationID); err == nil {
		return nil
	} else if !errors.Is(err, conversations.ErrConversationNotFound) {
		return fmt.Errorf("skill install: check governance conversation: %w", err)
	}
	if a.localIdentityID == "" {
		return fmt.Errorf("skill install: local identity unresolved (cannot parent the approval pause)")
	}
	if _, err := a.conv.Create(ctx, conversations.CreateParams{
		ID:         governanceConversationID,
		IdentityID: a.localIdentityID,
		Model:      a.model,
	}); err != nil {
		if isUniqueViolation(err) {
			return nil // a concurrent install won the race — the row exists, benign
		}
		return fmt.Errorf("skill install: provision governance conversation: %w", err)
	}
	return nil
}

// Search runs the catalog query behind the external-discovery flag (the Installer enforces
// AURA_SKILLS_EXTERNAL_DISCOVERY; a disabled result carries the toggle state explicitly).
func (a skillsWriteAdapter) Search(ctx context.Context, q string) (agui.SkillsCatalogResult, error) {
	res, err := a.installer.Search(ctx, q)
	if err != nil {
		return agui.SkillsCatalogResult{}, err
	}
	hits := make([]agui.SkillsCatalogHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, agui.SkillsCatalogHit{Source: h.Source, Skill: h.Skill, Installs: h.Installs})
	}
	return agui.SkillsCatalogResult{Enabled: res.Enabled, Query: res.Query, Hits: hits}, nil
}

// Restore guards the restore-collision landmine (SKW-03): it returns ErrSkillActiveExists
// (→ 409) BEFORE Writer.Restore (which does an os.RemoveAll on active/{name}) when an active
// skill of the same name exists; otherwise it restores + re-materializes + audits.
func (a skillsWriteAdapter) Restore(ctx context.Context, actor, name string) error {
	if a.writer.ActiveExists(name) {
		return fmt.Errorf("%w: %q", agui.ErrSkillActiveExists, name)
	}
	return a.writer.Restore(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "operator", IdentityID: actor})
}

// Archive de-materializes + moves active/{name} → archived + audits (SKW-03).
func (a skillsWriteAdapter) Archive(ctx context.Context, actor, name string) error {
	return a.writer.Archive(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "operator", IdentityID: actor})
}

// Mutate wraps Writer.WriteMutationByName for create/update/delete (SKW-01). The actor is
// labelled "operator" (not "model") so the in-box model-bypass never applies — every
// operator-authored mutation stays gated/pending exactly like the CLI path.
func (a skillsWriteAdapter) Mutate(ctx context.Context, actor, action, name, description, body string, always bool) (string, error) {
	return a.writer.WriteMutationByName(ctx, action, name, description, body, always,
		skills.AuditActor{ActorID: "operator", IdentityID: actor})
}

// toSkillsChecklist projects the skills-package CheckItem list onto the agui wire type.
func toSkillsChecklist(in []skills.CheckItem) []agui.SkillsCheckItem {
	out := make([]agui.SkillsCheckItem, 0, len(in))
	for _, c := range in {
		out = append(out, agui.SkillsCheckItem{Label: c.Label, Passed: c.Passed})
	}
	return out
}

// buildSkillsWriteProvider constructs the concrete skills write provider best-effort: a nil
// pool (no DB) or a missing skills dir leaves it nil → the routes answer 503. Never aborts
// boot (the SetGovernanceProviders precedent). It reuses the SAME newSkillWriter wiring the
// CLI + model path use, so the cockpit operates on one set of dirs.
func buildSkillsWriteProvider(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, pause *askuser.Store, conv *conversations.Store, idStore *identity.Store) agui.SkillsWriteProvider {
	if cfg == nil || pool == nil || cfg.SkillsDir == "" || pause == nil || conv == nil {
		return nil
	}
	writer := newSkillWriter(cfg, pool)
	installer := skills.NewInstaller(skills.InstallerConfig{
		Writer:       writer,
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
	return skillsWriteAdapter{
		installer:       installer,
		writer:          writer,
		pause:           pause,
		conv:            conv,
		localIdentityID: resolveLocalIdentityID(ctx, idStore),
		model:           cfg.LLM.Model,
	}
}
