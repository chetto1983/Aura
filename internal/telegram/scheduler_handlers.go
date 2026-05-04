package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/toolsets"
	tele "gopkg.in/telebot.v4"
)

// dispatchTask is the scheduler.Dispatcher implementation. It routes a
// fired task to the right side-effect: reminders go to Telegram via the
// stored RecipientID, wiki_maintenance runs the autonomous pass.
// Errors are returned so the scheduler records last_error; the row is
// always persisted regardless of outcome so the LLM can introspect.
func (b *Bot) dispatchTask(ctx context.Context, task *scheduler.Task) error {
	switch task.Kind {
	case scheduler.KindReminder:
		return b.dispatchReminder(task)
	case scheduler.KindWikiMaintenance:
		return b.dispatchWikiMaintenance(ctx)
	case scheduler.KindAgentJob:
		return b.dispatchAgentJob(ctx, task)
	default:
		return fmt.Errorf("dispatchTask: unknown kind %q", task.Kind)
	}
}

func (b *Bot) dispatchReminder(task *scheduler.Task) error {
	if task.RecipientID == "" {
		return fmt.Errorf("reminder %q has no recipient", task.Name)
	}
	chatID, err := strconv.ParseInt(task.RecipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse recipient %q: %w", task.RecipientID, err)
	}
	body := task.Payload
	if body == "" {
		body = "Reminder: " + task.Name
	} else {
		body = "⏰ " + body
	}
	if _, err := b.bot.Send(tele.ChatID(chatID), body); err != nil {
		return fmt.Errorf("send reminder: %w", err)
	}
	return nil
}

// dispatchWikiMaintenance runs the autonomous nightly wiki pass via
// MaintenanceJob: rebuilds index, lints, auto-fixes single-candidate
// broken links (Levenshtein ≤ 2), and defers the rest to 12h.
func (b *Bot) dispatchWikiMaintenance(ctx context.Context) error {
	if b.wiki == nil {
		return fmt.Errorf("wiki maintenance: wiki store unavailable")
	}
	b.wiki.RebuildIndex(ctx)
	job := scheduler.NewMaintenanceJob(b.wiki, b.logger).
		WithIssuesStore(b.issues).
		WithOwnerNotifier(func(ctx context.Context, msg string) {
			for _, ownerID := range b.collectOwnerIDs() {
				if err := b.SendToUser(ownerID, msg); err != nil {
					b.logger.Warn("maintenance notify failed", "owner", ownerID, "error", err)
				}
			}
		})
	fixed, deferred, err := job.Run(ctx)
	if err != nil {
		return fmt.Errorf("wiki maintenance: %w", err)
	}
	b.wiki.AppendLog(ctx, "nightly-maintenance", "")
	b.logger.Info("nightly wiki maintenance complete",
		"auto_fixed", fixed, "deferred", deferred)
	return nil
}

func (b *Bot) dispatchAgentJob(ctx context.Context, task *scheduler.Task) error {
	run, err := b.runAgentJob(ctx, task)
	b.logAgentJobRun(task, run)
	if err != nil {
		return err
	}
	if run.Payload.Notify != nil && *run.Payload.Notify && task.RecipientID != "" {
		notified, err := b.notifyAgentJob(task, run.Result.Content)
		run.Notified = notified
		if err != nil {
			return err
		}
	}
	return nil
}

type agentJobRun struct {
	Payload       scheduler.AgentJobPayload
	ToolAllowlist []string
	Result        agent.Result
	Notified      bool
}

func (b *Bot) runAgentJob(ctx context.Context, task *scheduler.Task) (agentJobRun, error) {
	if b.agentRunner == nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: agent runner unavailable", task.Name)
	}
	payload, err := scheduler.NormalizeAgentJobPayload(task.Payload)
	if err != nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	allowlist := safeAgentJobTools(payload.ToolAllowlist)
	result, err := b.agentRunner.Run(ctx, agent.Task{
		SystemPrompt:  agentJobSystemPrompt(payload.WritePolicy),
		Prompt:        payload.Goal,
		ToolAllowlist: allowlist,
		UserID:        task.RecipientID,
		Temperature:   llm.Float64Ptr(0),
	})
	run := agentJobRun{Payload: payload, ToolAllowlist: allowlist, Result: result}
	if err != nil {
		return run, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	return run, nil
}

func (b *Bot) logAgentJobRun(task *scheduler.Task, run agentJobRun) {
	b.logger.Info("agent job complete",
		"name", task.Name,
		"recipient_id", task.RecipientID,
		"llm_calls", run.Result.LLMCalls,
		"tool_calls", run.Result.ToolCalls,
		"tokens_prompt", run.Result.Tokens.PromptTokens,
		"tokens_completion", run.Result.Tokens.CompletionTokens,
		"tokens_total", run.Result.Tokens.TotalTokens,
		"elapsed_ms", run.Result.Elapsed.Milliseconds(),
	)
}

func (b *Bot) notifyAgentJob(task *scheduler.Task, content string) (bool, error) {
	msg := fmt.Sprintf("Agent job %q completed.\n\n%s", task.Name, truncateTelegramText(content, 3200))
	if err := b.SendToUser(task.RecipientID, msg); err != nil {
		return false, fmt.Errorf("agent_job %q notify: %w", task.Name, err)
	}
	return true, nil
}

func (b *Bot) RunTaskNow(ctx context.Context, name string) (tools.RunTaskNowResult, error) {
	if b.schedDB == nil {
		return tools.RunTaskNowResult{}, errors.New("scheduler store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return tools.RunTaskNowResult{}, errors.New("task name required")
	}
	task, err := b.schedDB.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tools.RunTaskNowResult{}, fmt.Errorf("task %q not found", name)
		}
		return tools.RunTaskNowResult{}, err
	}
	if task.Kind != scheduler.KindAgentJob {
		return tools.RunTaskNowResult{}, fmt.Errorf("task %q is kind %q; run_task_now MVP supports agent_job only", task.Name, task.Kind)
	}
	if task.Status == scheduler.StatusCancelled {
		return tools.RunTaskNowResult{}, fmt.Errorf("task %q is cancelled", task.Name)
	}

	started := time.Now().UTC()
	run, runErr := b.runAgentJob(ctx, task)
	status := "completed"
	lastErr := ""
	if runErr != nil {
		status = "failed"
		lastErr = runErr.Error()
	}
	if runErr == nil && run.Payload.Notify != nil && *run.Payload.Notify && task.RecipientID != "" {
		notified, notifyErr := b.notifyAgentJob(task, run.Result.Content)
		run.Notified = notified
		if notifyErr != nil {
			status = "failed"
			lastErr = notifyErr.Error()
		}
	}
	if err := b.schedDB.RecordManualRun(ctx, task.ID, started, lastErr); err != nil && lastErr == "" {
		status = "failed"
		lastErr = err.Error()
	}

	return tools.RunTaskNowResult{
		OK:               lastErr == "",
		Name:             task.Name,
		Kind:             string(task.Kind),
		Status:           status,
		Summary:          truncateTelegramText(run.Result.Content, 1600),
		LastError:        lastErr,
		LLMCalls:         run.Result.LLMCalls,
		ToolCalls:        run.Result.ToolCalls,
		TokensPrompt:     run.Result.Tokens.PromptTokens,
		TokensCompletion: run.Result.Tokens.CompletionTokens,
		TokensTotal:      run.Result.Tokens.TotalTokens,
		ElapsedMS:        run.Result.Elapsed.Milliseconds(),
		Notified:         run.Notified,
		ToolAllowlist:    run.ToolAllowlist,
	}, nil
}

func agentJobSystemPrompt(writePolicy string) string {
	return "You are Aura running a scheduled agent job. Complete the saved routine with concise, evidence-oriented work. Write policy: " + writePolicy + ". Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state directly. If durable memory growth is useful, use propose_wiki_change so the user can review it. Return a short report with what you checked, any proposal created, and unresolved issues."
}

func safeAgentJobTools(requested []string) []string {
	out := toolsets.FilterAllowed(requested, scheduler.DefaultAgentJobTools)
	if len(out) == 0 {
		return append([]string(nil), scheduler.DefaultAgentJobTools...)
	}
	return out
}

func truncateTelegramText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
