package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/learning"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/telegram"
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

// ---- agent.RunTask adapters -------------------------------------------------
// These four blocks bridge agent.RunTask to swarm/cron/scheduler interfaces.
// Each *DepsGetter closure reads cfg.AuraBot* fields fresh on every call so
// dashboard live-tune of MaxIterations/Timeout propagates to the next RunTask
// without mutating shared state.

// swarmRunnerAdapter bridges agent.RunTask to swarm.AgentRunner.
type swarmRunnerAdapter struct {
	getDeps func() agent.RunTaskDeps
}

func (a *swarmRunnerAdapter) Run(ctx context.Context, task agent.Task) (agent.Result, error) {
	deps := a.getDeps()
	deps.RunID = identity.RunIDFromContext(ctx)
	return agent.RunTask(ctx, deps, task)
}

// newSwarmDepsGetter returns a lazy RunTaskDeps builder for swarmRunnerAdapter.
func newSwarmDepsGetter(cfg *config.Config, deps *telegram.Deps) func() agent.RunTaskDeps {
	return func() agent.RunTaskDeps {
		return newRunTaskDeps(cfg, deps)
	}
}

// agentJobRunnerAdapter bridges agent.RunTask to cron.JobRunner.
type agentJobRunnerAdapter struct {
	getDeps func() agent.RunTaskDeps
}

func (a *agentJobRunnerAdapter) RunJob(ctx context.Context, req cron.JobRequest) (cron.JobResult, error) {
	if a.getDeps == nil {
		return cron.JobResult{}, fmt.Errorf("agent runner unavailable")
	}
	deps := a.getDeps()
	deps.RunID = req.RunID
	if deps.RunID == "" {
		deps.RunID = identity.RunIDFromContext(ctx)
	}
	result, err := agent.RunTask(ctx, deps, agent.Task{
		SystemPrompt:  req.SystemPrompt,
		Prompt:        req.Prompt,
		ToolAllowlist: req.ToolAllowlist,
		UserID:        req.UserID,
		Temperature:   req.Temperature,
	})
	if err != nil {
		return cron.JobResult{}, err
	}
	return cron.JobResult{
		Content:          result.Content,
		LLMCalls:         result.LLMCalls,
		ToolCalls:        result.ToolCalls,
		TokensPrompt:     result.Tokens.PromptTokens,
		TokensCompletion: result.Tokens.CompletionTokens,
		TokensTotal:      result.Tokens.TotalTokens,
		Elapsed:          result.Elapsed,
	}, nil
}

// newAgentJobDepsGetter returns a lazy RunTaskDeps builder for agentJobRunnerAdapter.
// Returning nil when LLM is unconfigured disables scheduled agent job execution.
func newAgentJobDepsGetter(cfg *config.Config, deps *telegram.Deps) func() agent.RunTaskDeps {
	if deps.LLM == nil {
		return nil
	}
	return func() agent.RunTaskDeps {
		return newRunTaskDeps(cfg, deps)
	}
}

// newWebChatDepsGetter returns a lazy RunTaskDeps builder for the Hub-backed
// web chat service. Returns nil when LLM is unconfigured so /api/chat returns
// a clean disabled state instead of half-wiring the endpoint.
func newWebChatDepsGetter(cfg *config.Config, deps *telegram.Deps) func() agent.RunTaskDeps {
	if deps.LLM == nil {
		return nil
	}
	return func() agent.RunTaskDeps {
		return newRunTaskDeps(cfg, deps)
	}
}

// newRunTaskDeps assembles agent.RunTaskDeps with cfg-driven budgets resolved
// at call time. Shared by all three *DepsGetter closures above; the body used
// to be triplicated, which made it easy to drift one and miss the others.
func newRunTaskDeps(cfg *config.Config, deps *telegram.Deps) agent.RunTaskDeps {
	timeoutSec := cfg.AuraBotTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultAuraBotTimeoutSec
	}
	maxIterations := cfg.AuraBotMaxIterations
	if maxIterations <= 0 {
		maxIterations = 100
	}
	return agent.RunTaskDeps{
		LLM:               deps.LLM,
		Tools:             deps.Tools,
		AgentDefs:         deps.AgentDefs,
		Model:             cfg.LLMModel,
		ReasoningEffort:   cfg.ReasoningEffort,
		Logger:            deps.Logger,
		AttemptsRepo:      attempts.NewSQLiteRepo(deps.Pool),
		TokenJuiceEnabled: cfg.TokenJuiceEnabled,
		BudgetCaps:        cfg.ToolBudgetCaps(),
		MaxIterations:     maxIterations,
		Timeout:           time.Duration(timeoutSec) * time.Second,
		ToolTimeout:       time.Duration(timeoutSec) * time.Second,
	}
}

// scheduledTaskRunnerAdapter bridges cron.Handler.RunNow to tools.ScheduledTaskRunner.
type scheduledTaskRunnerAdapter struct {
	h *cron.Handler
}

func (a *scheduledTaskRunnerAdapter) RunTaskNow(ctx context.Context, name string) (tools.RunTaskNowResult, error) {
	res, err := a.h.RunNow(ctx, name)
	if err != nil {
		return tools.RunTaskNowResult{}, err
	}
	return tools.RunTaskNowResult{
		OK:               res.OK,
		Name:             res.Name,
		Kind:             res.Kind,
		Status:           res.Status,
		Summary:          res.Summary,
		LastError:        res.LastError,
		LLMCalls:         res.LLMCalls,
		ToolCalls:        res.ToolCalls,
		TokensPrompt:     res.TokensPrompt,
		TokensCompletion: res.TokensCompletion,
		TokensTotal:      res.TokensTotal,
		ElapsedMS:        res.ElapsedMS,
		Notified:         res.Notified,
		Skipped:          res.Skipped,
		WakeSignature:    res.WakeSignature,
		ToolAllowlist:    res.ToolAllowlist,
	}, nil
}
