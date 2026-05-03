package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
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
	if b.agentRunner == nil {
		return fmt.Errorf("agent_job %q: agent runner unavailable", task.Name)
	}
	payload, err := scheduler.NormalizeAgentJobPayload(task.Payload)
	if err != nil {
		return fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	allowlist := safeAgentJobTools(payload.ToolAllowlist)
	result, err := b.agentRunner.Run(ctx, agent.Task{
		SystemPrompt:  agentJobSystemPrompt(payload.WritePolicy),
		Prompt:        payload.Goal,
		ToolAllowlist: allowlist,
		UserID:        task.RecipientID,
		Temperature:   llm.Float64Ptr(0),
	})
	if err != nil {
		return fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	b.logger.Info("agent job complete",
		"name", task.Name,
		"recipient_id", task.RecipientID,
		"llm_calls", result.LLMCalls,
		"tool_calls", result.ToolCalls,
		"tokens_prompt", result.Tokens.PromptTokens,
		"tokens_completion", result.Tokens.CompletionTokens,
		"tokens_total", result.Tokens.TotalTokens,
		"elapsed_ms", result.Elapsed.Milliseconds(),
	)
	if payload.Notify != nil && *payload.Notify && task.RecipientID != "" {
		msg := fmt.Sprintf("Agent job %q completed.\n\n%s", task.Name, truncateTelegramText(result.Content, 3200))
		if err := b.SendToUser(task.RecipientID, msg); err != nil {
			return fmt.Errorf("agent_job %q notify: %w", task.Name, err)
		}
	}
	return nil
}

func agentJobSystemPrompt(writePolicy string) string {
	return "You are Aura running a scheduled agent job. Complete the saved routine with concise, evidence-oriented work. Write policy: " + writePolicy + ". Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state directly. If durable memory growth is useful, use propose_wiki_change so the user can review it. Return a short report with what you checked, any proposal created, and unresolved issues."
}

func safeAgentJobTools(requested []string) []string {
	allowed := map[string]bool{}
	for _, tool := range scheduler.DefaultAgentJobTools {
		allowed[tool] = true
	}
	out := make([]string, 0, len(requested))
	for _, tool := range requested {
		tool = strings.TrimSpace(tool)
		if tool != "" && allowed[tool] {
			out = append(out, tool)
		}
	}
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
