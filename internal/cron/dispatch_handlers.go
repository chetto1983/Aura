package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/identity"
)

// ---- private dispatch methods -----------------------------------------------

func (h *Handler) dispatchReminder(task *Task) error {
	if task.RecipientID == "" {
		return fmt.Errorf("reminder %q has no recipient", task.Name)
	}
	body := task.Payload
	if body == "" {
		body = "Reminder: " + task.Name
	} else {
		body = "⏰ " + body
	}
	if h.notifier == nil {
		return fmt.Errorf("reminder %q: notifier unavailable", task.Name)
	}
	return h.notifier.SendReminder(task.RecipientID, body)
}

func (h *Handler) dispatchWikiMaintenance(ctx context.Context) error {
	if h.wiki == nil {
		return fmt.Errorf("wiki maintenance: wiki store unavailable")
	}
	h.wiki.RebuildIndex(ctx)
	job := NewMaintenanceJob(h.wiki, h.logger).
		WithIssuesStore(h.issues)
	if h.notifier != nil {
		job = job.WithOwnerNotifier(func(ctx context.Context, msg string) {
			h.notifier.NotifyOwners(ctx, msg)
		})
	}
	fixed, deferred, err := job.Run(ctx)
	if err != nil {
		return fmt.Errorf("wiki maintenance: %w", err)
	}
	h.wiki.AppendLog(ctx, "nightly-maintenance", "")
	h.logger.Info("nightly wiki maintenance complete",
		"auto_fixed", fixed, "deferred", deferred)
	return nil
}

func (h *Handler) dispatchLessonPromotion(ctx context.Context) error {
	if h.promoter == nil {
		return fmt.Errorf("lesson promotion: promoter unavailable")
	}
	promoted, skipped, err := h.promoter.Promote(ctx)
	if err != nil {
		return fmt.Errorf("lesson promotion: %w", err)
	}
	h.logger.Info("lesson promotion complete", "promoted", promoted, "skipped", skipped)
	return nil
}

func (h *Handler) dispatchProposalTTLSweep(ctx context.Context) error {
	if h.proposalSweeper == nil {
		return fmt.Errorf("proposal TTL sweep: sweeper unavailable")
	}
	purged, err := h.proposalSweeper.Sweep(ctx)
	if err != nil {
		return fmt.Errorf("proposal TTL sweep: %w", err)
	}
	h.logger.Info("proposal TTL sweep complete", "purged", purged)
	return nil
}

func (h *Handler) dispatchMemoryDecay(ctx context.Context) error {
	if h.memoryDecay == nil {
		return fmt.Errorf("memory decay: runner unavailable")
	}
	scanned, deleted, kept, err := h.memoryDecay.Decay(ctx)
	if err != nil {
		return fmt.Errorf("memory decay: %w", err)
	}
	h.logger.Info("memory decay complete", "scanned", scanned, "deleted", deleted, "kept", kept)
	return nil
}

// ---- agent job internals ----------------------------------------------------

type agentJobRun struct {
	Payload       AgentJobPayload
	ToolAllowlist []string
	ActorID       string
	Result        JobResult
	Notified      bool
	Skipped       bool
	WakeSignature string
}

type agentJobMetrics struct {
	Skipped          bool  `json:"skipped"`
	LLMCalls         int   `json:"llm_calls"`
	ToolCalls        int   `json:"tool_calls"`
	TokensPrompt     int   `json:"tokens_prompt"`
	TokensCompletion int   `json:"tokens_completion"`
	TokensTotal      int   `json:"tokens_total"`
	ElapsedMS        int64 `json:"elapsed_ms"`
}

func (h *Handler) runAgentJob(ctx context.Context, task *Task) (agentJobRun, error) {
	if h.runner == nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: agent runner unavailable", task.Name)
	}
	payload, err := NormalizeAgentJobPayload(task.Payload)
	if err != nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	allowlist := SafeAgentJobTools(payload.ToolAllowlist)
	wakeSignature, hasWakeSignature := AgentJobWakeSignature(ctx, payload, AgentJobWakeDeps{
		Wiki:    h.wiki,
		Sources: h.sources,
		Tasks:   h.schedDB,
	})
	if hasWakeSignature && task.WakeSignature != "" && task.WakeSignature == wakeSignature {
		return agentJobRun{
			Payload:       payload,
			ToolAllowlist: allowlist,
			Result:        JobResult{Content: "Agent job skipped: wake_if_changed signals unchanged."},
			Skipped:       true,
			WakeSignature: wakeSignature,
		}, nil
	}
	runCtx, actorID, err := h.delegateAgentJobActor(ctx, task, allowlist)
	if err != nil {
		return agentJobRun{Payload: payload, ToolAllowlist: allowlist}, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	now := time.Now()
	zero := 0.0
	result, err := h.runner.RunJob(runCtx, JobRequest{
		RunID:         identity.RunIDFromContext(runCtx),
		SystemPrompt:  AgentJobSystemPrompt(payload, now, h.loc),
		Prompt:        h.agentJobPrompt(ctx, task, payload, now, h.loc),
		ToolAllowlist: allowlist,
		UserID:        task.RecipientID,
		Temperature:   &zero,
	})
	run := agentJobRun{Payload: payload, ToolAllowlist: allowlist, ActorID: actorID, Result: result, WakeSignature: wakeSignature}
	if err != nil {
		return run, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	return run, nil
}

func (h *Handler) delegateAgentJobActor(ctx context.Context, task *Task, allowlist []string) (context.Context, string, error) {
	delegator, parentActorID := h.delegationAuthority(ctx, task)
	if delegator == nil {
		return ctx, "", fmt.Errorf("%w: cron agent job requires identity delegator", identity.ErrUnauthorized)
	}
	if parentActorID == "" {
		return ctx, "", fmt.Errorf("%w: cron agent job requires parent actor", identity.ErrUnauthorized)
	}
	constraints, err := json.Marshal(map[string]any{
		"task_id":        task.ID,
		"task_name":      task.Name,
		"tool_allowlist": allowlist,
		"channel":        "cron",
	})
	if err != nil {
		return ctx, "", fmt.Errorf("cron delegation constraints: %w", err)
	}
	scopeID := fmt.Sprintf("cron:%d:%s", task.ID, task.Name)
	result, err := delegator.DelegateActor(ctx, identity.DelegateActorParams{
		ID:              identity.DelegatedActorID(identity.ActorTypeCron, parentActorID, scopeID),
		ParentActorID:   parentActorID,
		ActorType:       identity.ActorTypeCron,
		RunID:           scopeID,
		Capabilities:    delegatedAgentJobCapabilities(allowlist),
		ConstraintsJSON: string(constraints),
	})
	if err != nil {
		return ctx, "", err
	}
	return identity.WithRunID(identity.WithActorID(identity.WithAuthority(ctx, delegator), result.Actor.ID), scopeID), result.Actor.ID, nil
}

func (h *Handler) delegationAuthority(ctx context.Context, task *Task) (identity.Delegator, string) {
	delegator, ok := identity.DelegatorFromContext(ctx)
	if !ok {
		delegator = h.identity
	}
	if delegator == nil {
		return nil, ""
	}
	parentActorID := identity.ActorIDFromContext(ctx)
	if parentActorID == "" && task != nil && strings.TrimSpace(task.RecipientID) != "" && strings.TrimSpace(task.RecipientID) != "cron" {
		parentActorID = identity.TelegramSessionActorID(task.RecipientID)
	}
	return delegator, parentActorID
}

func delegatedAgentJobCapabilities(allowlist []string) []identity.Capability {
	capabilities := []identity.Capability{identity.CapabilityCronRun}
	if len(allowlist) > 0 {
		capabilities = append(capabilities, identity.CapabilityToolExecute)
	}
	return capabilities
}

func (h *Handler) persistAgentJobResult(ctx context.Context, task *Task, run agentJobRun) {
	if h.schedDB == nil || task == nil || task.ID == 0 {
		return
	}
	if run.Payload.Goal == "" && run.Result.Content == "" && run.WakeSignature == "" {
		return
	}
	metrics := agentJobMetrics{
		Skipped:          run.Skipped,
		LLMCalls:         run.Result.LLMCalls,
		ToolCalls:        run.Result.ToolCalls,
		TokensPrompt:     run.Result.TokensPrompt,
		TokensCompletion: run.Result.TokensCompletion,
		TokensTotal:      run.Result.TokensTotal,
		ElapsedMS:        run.Result.Elapsed.Milliseconds(),
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		h.logger.Warn("agent job metrics marshal failed", "name", task.Name, "error", err)
		return
	}
	if err := h.schedDB.RecordAgentJobResult(ctx, task.ID, truncate(run.Result.Content, 4000), string(data), run.WakeSignature); err != nil {
		h.logger.Warn("agent job result persistence failed", "name", task.Name, "error", err)
	}
}

func (h *Handler) notifyAgentJob(task *Task, content string) (bool, error) {
	if h.notifier == nil {
		return false, nil
	}
	payload, _ := NormalizeAgentJobPayload(task.Payload)
	msg := agentJobNotificationMessage(task, payload, content)
	if err := h.notifier.SendCompletion(task.RecipientID, msg); err != nil {
		return false, fmt.Errorf("agent_job %q notify: %w", task.Name, err)
	}
	return true, nil
}
