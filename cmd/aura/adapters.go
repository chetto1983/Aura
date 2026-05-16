package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/learning"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/swarm"
)

// skillsDeleterAdapter bridges auraskills.FSDeleter (which surfaces a
// package-internal not-found sentinel) to api.SkillDeleter (which
// expects api.ErrSkillNotFound for 404 routing). Keeping the cycle out
// of the two packages costs us this 8-line shim.
type skillsDeleterAdapter struct {
	inner *auraskills.FSDeleter
}

func (a skillsDeleterAdapter) Delete(name string) error {
	if a.inner == nil {
		return api.ErrSkillNotFound
	}
	if err := a.inner.Delete(name); err != nil {
		if auraskills.IsSkillNotFound(err) {
			return api.ErrSkillNotFound
		}
		return err
	}
	return nil
}

type skillProposalApplierAdapter struct {
	inner *auraskills.FSProposalApplier
}

func (a skillProposalApplierAdapter) ApplySkillProposal(ctx context.Context, proposal api.SkillProposalApplyRequest) error {
	if a.inner == nil {
		return api.ErrSkillNotFound
	}
	return a.inner.ApplySkillProposal(ctx, auraskills.LocalProposal{
		Action:  proposal.Action,
		Name:    proposal.Name,
		Content: proposal.Content,
	})
}

func newSkillsDeleterAdapter(d *auraskills.FSDeleter) api.SkillDeleter {
	return skillsDeleterAdapter{inner: d}
}

func newSkillProposalApplierAdapter(p *auraskills.FSProposalApplier) api.SkillProposalApplier {
	return skillProposalApplierAdapter{inner: p}
}

// subagentBridgeAdapter satisfies tools.subagentDispatcher using *swarm.HubBridge.
// It converts SubagentNodeSpec (local type, no import-cycle) → swarm.NodeSpec,
// hardcoding RiskTier="read_only" as required by the Phase-R read-only fanout slice.
type subagentBridgeAdapter struct {
	bridge *swarm.HubBridge
}

func (a *subagentBridgeAdapter) Dispatch(ctx context.Context, spec tools.SubagentNodeSpec, principalID string) (string, error) {
	riskTier := spec.RiskTier
	if riskTier == "" {
		riskTier = "read_only"
	}
	nodeSpec := swarm.NodeSpec{
		Goal:          spec.Goal,
		Instruction:   spec.Instruction,
		ToolAllowlist: spec.ToolAllowlist,
		BudgetSecs:    spec.BudgetSecs,
		ParentRunID:   spec.ParentRunID,
		RiskTier:      riskTier,
		MaxIterations: 5,
	}
	if err := nodeSpec.Validate(); err != nil {
		return "", err
	}
	return a.bridge.Dispatch(ctx, nodeSpec, identity.Principal{ID: principalID})
}

// subagentHubAdapter satisfies tools.subagentWaiter using *chat.Hub.
// It converts chat.Result (concrete type) → tools.SubagentResult (local type).
type subagentHubAdapter struct {
	hub *chat.Hub
}

func (a *subagentHubAdapter) WaitForRun(ctx context.Context, runID string) (tools.SubagentResult, error) {
	result, err := a.hub.WaitForRun(ctx, runID)
	if err != nil {
		return tools.SubagentResult{}, err
	}
	return tools.SubagentResult{
		ReplyText:      result.ReplyText,
		ToolCallsCount: result.ToolCallsCount,
		TokensTotal:    result.TokensTotal,
		ElapsedMs:      result.ElapsedMs,
	}, nil
}

// lessonPromoterAdapter bridges learning.PromoteLessons to cron.LessonPromoter.
type lessonPromoterAdapter struct {
	attemptsRepo  *attempts.SQLiteRepo
	proposalStore *learning.SQLProposalStore
}

func (a *lessonPromoterAdapter) Promote(ctx context.Context) (promoted, skipped int, err error) {
	return learning.PromoteLessons(ctx, a.attemptsRepo, a.proposalStore, 3, 7)
}

// proposalTTLSweeperAdapter bridges learning.SweepStaleProposals to cron.ProposalTTLSweeper.
type proposalTTLSweeperAdapter struct {
	db *sql.DB
}

func (a *proposalTTLSweeperAdapter) Sweep(ctx context.Context) (int, error) {
	return learning.SweepStaleProposals(ctx, a.db, 30*24*time.Hour)
}
