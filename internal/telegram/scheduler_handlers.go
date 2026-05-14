package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agent/tools/sets"
	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// dispatchTask is the cron.Dispatcher implementation. It routes a
// fired task to the right side-effect: reminders go to Telegram via the
// stored RecipientID, wiki_maintenance runs the autonomous pass.
// Errors are returned so the scheduler records last_error; the row is
// always persisted regardless of outcome so the LLM can introspect.
func (b *Bot) dispatchTask(ctx context.Context, task *cron.Task) error {
	switch task.Kind {
	case cron.KindReminder:
		return b.dispatchReminder(task)
	case cron.KindWikiMaintenance:
		return b.dispatchWikiMaintenance(ctx)
	case cron.KindAgentJob:
		return b.dispatchAgentJob(ctx, task)
	default:
		return fmt.Errorf("dispatchTask: unknown kind %q", task.Kind)
	}
}

func (b *Bot) dispatchReminder(task *cron.Task) error {
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

	gate := b.userGate()
	if gate == nil {
		// No UserGate: deliver directly (backward compat, tests).
		if _, err := b.bot.Send(tele.ChatID(chatID), body); err != nil {
			return fmt.Errorf("send reminder: %w", err)
		}
		return nil
	}

	// Non-blocking enqueue via TryAcquire (CONC-02, D-06).
	// On success the reminder is processed FIFO alongside user messages (D-07).
	// On failure return nil -- scheduler retries on next tick (D-05).
	bodyCopy := body
	chatIDCopy := chatID
	acquired := gate.TryAcquire(task.RecipientID, concurrency.Entry{
		Process: func(_ context.Context) {
			if _, err := b.bot.Send(tele.ChatID(chatIDCopy), bodyCopy); err != nil {
				b.logger.Warn("reminder delivery failed",
					"user_id", task.RecipientID, "error", err)
			}
		},
	})
	if !acquired {
		b.logger.Info("reminder dropped (gate full), will retry on next tick",
			"name", task.Name, "user_id", task.RecipientID)
		return nil // drop; scheduler retries (D-05)
	}
	return nil
}

// dispatchWikiMaintenance runs the autonomous nightly wiki pass via
// MaintenanceJob: rebuilds index, lints, auto-fixes single-candidate
// broken links (Levenshtein ≤ 2), and defers the rest to 12h.
func (b *Bot) dispatchWikiMaintenance(ctx context.Context) error {
	if b.rt == nil || b.rt.wiki == nil {
		return fmt.Errorf("wiki maintenance: wiki store unavailable")
	}
	b.rt.wiki.RebuildIndex(ctx)
	job := cron.NewMaintenanceJob(b.rt.wiki, b.logger).
		WithIssuesStore(b.rt.issues).
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
	b.rt.wiki.AppendLog(ctx, "nightly-maintenance", "")
	b.logger.Info("nightly wiki maintenance complete",
		"auto_fixed", fixed, "deferred", deferred)
	return nil
}

func (b *Bot) dispatchAgentJob(ctx context.Context, task *cron.Task) error {
	run, err := b.runAgentJob(ctx, task)
	b.logAgentJobRun(task, run)
	b.persistAgentJobResult(ctx, task, run)
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
	Payload       cron.AgentJobPayload
	ToolAllowlist []string
	Result        agent.Result
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

func (b *Bot) runAgentJob(ctx context.Context, task *cron.Task) (agentJobRun, error) {
	if b.rt == nil || b.rt.agentRunner == nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: agent runner unavailable", task.Name)
	}
	payload, err := cron.NormalizeAgentJobPayload(task.Payload)
	if err != nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	allowlist := safeAgentJobTools(payload.ToolAllowlist)
	wakeSignature, hasWakeSignature := cron.AgentJobWakeSignature(ctx, payload, cron.AgentJobWakeDeps{
		Wiki:    b.rt.wiki,
		Sources: b.rt.sources,
		Tasks:   b.rt.schedDB,
	})
	if hasWakeSignature && task.WakeSignature != "" && task.WakeSignature == wakeSignature {
		return agentJobRun{
			Payload:       payload,
			ToolAllowlist: allowlist,
			Result:        agent.Result{Content: "Agent job skipped: wake_if_changed signals unchanged."},
			Skipped:       true,
			WakeSignature: wakeSignature,
		}, nil
	}
	now := time.Now()
	result, err := b.rt.agentRunner.Run(ctx, agent.Task{
		SystemPrompt:  agentJobSystemPrompt(payload, now, b.loc),
		Prompt:        b.agentJobPrompt(ctx, task, payload, now, b.loc),
		ToolAllowlist: allowlist,
		UserID:        task.RecipientID,
		Temperature:   llm.Float64Ptr(0),
	})
	run := agentJobRun{Payload: payload, ToolAllowlist: allowlist, Result: result, WakeSignature: wakeSignature}
	if err != nil {
		return run, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	return run, nil
}

func (b *Bot) logAgentJobRun(task *cron.Task, run agentJobRun) {
	b.logger.Info("agent job complete",
		"name", task.Name,
		"recipient_id", task.RecipientID,
		"skipped", run.Skipped,
		"llm_calls", run.Result.LLMCalls,
		"tool_calls", run.Result.ToolCalls,
		"tokens_prompt", run.Result.Tokens.PromptTokens,
		"tokens_completion", run.Result.Tokens.CompletionTokens,
		"tokens_total", run.Result.Tokens.TotalTokens,
		"elapsed_ms", run.Result.Elapsed.Milliseconds(),
	)
}

func (b *Bot) persistAgentJobResult(ctx context.Context, task *cron.Task, run agentJobRun) {
	if b.rt == nil || b.rt.schedDB == nil || task == nil || task.ID == 0 {
		return
	}
	if run.Payload.Goal == "" && run.Result.Content == "" && run.WakeSignature == "" {
		return
	}
	metrics := agentJobMetrics{
		Skipped:          run.Skipped,
		LLMCalls:         run.Result.LLMCalls,
		ToolCalls:        run.Result.ToolCalls,
		TokensPrompt:     run.Result.Tokens.PromptTokens,
		TokensCompletion: run.Result.Tokens.CompletionTokens,
		TokensTotal:      run.Result.Tokens.TotalTokens,
		ElapsedMS:        run.Result.Elapsed.Milliseconds(),
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		b.logger.Warn("agent job metrics marshal failed", "name", task.Name, "error", err)
		return
	}
	if err := b.rt.schedDB.RecordAgentJobResult(ctx, task.ID, truncateTelegramText(run.Result.Content, 4000), string(data), run.WakeSignature); err != nil {
		b.logger.Warn("agent job result persistence failed", "name", task.Name, "error", err)
	}
}

func (b *Bot) notifyAgentJob(task *cron.Task, content string) (bool, error) {
	payload, _ := cron.NormalizeAgentJobPayload(task.Payload)
	msg := agentJobNotificationMessage(task, payload, content)

	gate := b.userGate()
	if gate == nil {
		// No UserGate: deliver directly (backward compat, tests).
		if err := b.sendGeneratedToUser(task.RecipientID, msg); err != nil {
			return false, fmt.Errorf("agent_job %q notify: %w", task.Name, err)
		}
		return true, nil
	}

	// Non-blocking enqueue via TryAcquire (CONC-02, D-06).
	// On full inbox return false,nil -- agent job notification is optional (D-05).
	recipientID := task.RecipientID
	msgCopy := msg
	acquired := gate.TryAcquire(recipientID, concurrency.Entry{
		Process: func(_ context.Context) {
			if err := b.sendGeneratedToUser(recipientID, msgCopy); err != nil {
				b.logger.Warn("agent job notify delivery failed",
					"user_id", recipientID, "error", err)
			}
		},
	})
	if !acquired {
		return false, nil // dropped; no error -- notification is best-effort
	}
	return true, nil
}

func agentJobNotificationMessage(task *cron.Task, payload cron.AgentJobPayload, content string) string {
	name := ""
	if task != nil {
		name = task.Name
	}
	if payload.Language == "it" {
		return fmt.Sprintf("Job agente %q completato.\n\n%s", name, truncateTelegramText(content, 3200))
	}
	return fmt.Sprintf("Agent job %q completed.\n\n%s", name, truncateTelegramText(content, 3200))
}

func (b *Bot) RunTaskNow(ctx context.Context, name string) (tools.RunTaskNowResult, error) {
	if b.rt == nil || b.rt.schedDB == nil {
		return tools.RunTaskNowResult{}, errors.New("scheduler store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return tools.RunTaskNowResult{}, errors.New("task name required")
	}
	task, err := b.rt.schedDB.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tools.RunTaskNowResult{}, fmt.Errorf("task %q not found", name)
		}
		return tools.RunTaskNowResult{}, err
	}
	if task.Kind != cron.KindAgentJob {
		return tools.RunTaskNowResult{}, fmt.Errorf("task %q is kind %q; run_task_now MVP supports agent_job only", task.Name, task.Kind)
	}
	if task.Status == cron.StatusCancelled {
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
	b.persistAgentJobResult(ctx, task, run)
	if err := b.rt.schedDB.RecordManualRun(ctx, task.ID, started, lastErr); err != nil && lastErr == "" {
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
		Skipped:          run.Skipped,
		WakeSignature:    run.WakeSignature,
		ToolAllowlist:    run.ToolAllowlist,
	}, nil
}

func agentJobSystemPrompt(payload cron.AgentJobPayload, now time.Time, loc *time.Location) string {
	prompt := "You are Aura running a scheduled agent job. Complete the saved routine with concise, evidence-oriented work. Write policy: " + payload.WritePolicy + ". Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state directly. If durable memory or reusable procedural knowledge is useful, report the exact file path and patch you recommend instead of applying it. Return a short report with what you checked, any recommended edit, and unresolved issues."
	if lang := agentJobLanguageInstruction(payload.Language); lang != "" {
		prompt += " " + lang
	} else {
		prompt += " Output language: infer the language from the saved goal and context anchors; if unclear, mirror the language most recently used by the user in that saved context."
	}
	if len(payload.Skills) > 0 {
		prompt += " This job is skill-backed: inspect attached SKILL.md files with workspace file tools before applying their procedures."
	}
	if len(payload.WakeIfChanged) > 0 {
		prompt += " Respect wake_if_changed as a no-op guard: check those signals first and finish quickly with no proposal if there is no material change."
	}
	prompt += conversation.RenderRuntimeContext(now, loc)
	return prompt
}

func agentJobLanguageInstruction(language string) string {
	switch cron.NormalizeAgentJobLanguage(language) {
	case "it":
		return "Output language: Italian. Write the final report and all user-facing prose in Italian, regardless of tool or web result language."
	case "en":
		return "Output language: English. Write the final report and all user-facing prose in English, regardless of tool or web result language."
	default:
		return ""
	}
}

func (b *Bot) agentJobPrompt(ctx context.Context, task *cron.Task, payload cron.AgentJobPayload, now time.Time, loc *time.Location) string {
	var sb strings.Builder
	if schedule := agentJobScheduleContext(task, now, loc); schedule != "" {
		sb.WriteString(schedule)
		sb.WriteString("\n\n")
	}
	fmt.Fprintf(&sb, "Goal: %s", payload.Goal)
	if len(payload.EnabledToolsets) > 0 {
		fmt.Fprintf(&sb, "\n\nEnabled toolsets: %s", strings.Join(payload.EnabledToolsets, ", "))
	}
	if len(payload.Skills) > 0 {
		fmt.Fprintf(&sb, "\n\nAttached skills: %s\nFind and read the matching SKILL.md files before relying on their procedures. Do not install, delete, or edit skills directly.", strings.Join(payload.Skills, ", "))
	}
	if len(payload.ContextFrom) > 0 {
		fmt.Fprintf(&sb, "\n\nContext anchors: %s\nUse these anchors as the first retrieval targets, preferably via search_memory or narrow read tools before broad web/tool use.", strings.Join(payload.ContextFrom, ", "))
		if prior := b.agentJobPriorOutputs(ctx, payload.ContextFrom); prior != "" {
			fmt.Fprintf(&sb, "\n\nPrior job outputs:\n%s", prior)
		}
	}
	if len(payload.WakeIfChanged) > 0 {
		fmt.Fprintf(&sb, "\n\nWake-if-changed signals: %s\nBefore doing the full routine, check whether these signals changed materially. If not, return a concise no-change report and stop.", strings.Join(payload.WakeIfChanged, ", "))
	}
	return sb.String()
}

func agentJobScheduleContext(task *cron.Task, now time.Time, loc *time.Location) string {
	if task == nil || task.NextRunAt.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	scheduledLocal := task.NextRunAt.In(loc)
	runningLocal := now.In(loc)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Scheduled task: %s\n", task.Name)
	fmt.Fprintf(&sb, "Scheduled for: %s local (%s UTC)\n",
		scheduledLocal.Format("2006-01-02 15:04:05"),
		task.NextRunAt.UTC().Format(time.RFC3339),
	)
	fmt.Fprintf(&sb, "Running at: %s local (%s UTC)\n",
		runningLocal.Format("2006-01-02 15:04:05"),
		now.UTC().Format(time.RFC3339),
	)
	if task.ScheduleKind != "" {
		fmt.Fprintf(&sb, "Schedule kind: %s", task.ScheduleKind)
		switch task.ScheduleKind {
		case cron.ScheduleDaily:
			fmt.Fprintf(&sb, " daily=%s", task.ScheduleDaily)
			if task.ScheduleWeekdays != "" {
				fmt.Fprintf(&sb, " weekdays=%s", task.ScheduleWeekdays)
			}
		case cron.ScheduleEvery:
			fmt.Fprintf(&sb, " every_minutes=%d", task.ScheduleEveryMinutes)
		}
		sb.WriteString("\n")
	}
	if delay := now.Sub(task.NextRunAt); delay > time.Minute {
		fmt.Fprintf(&sb, "Run delay: %s. Treat current-date research as of Running at, not Scheduled for, unless the goal explicitly asks for historical state.\n", delay.Round(time.Minute))
	}
	return strings.TrimSpace(sb.String())
}

func (b *Bot) agentJobPriorOutputs(ctx context.Context, anchors []string) string {
	if b.rt == nil || b.rt.schedDB == nil {
		return ""
	}
	var lines []string
	for _, anchor := range anchors {
		name, ok := cron.AgentJobTaskAnchor(anchor)
		if !ok {
			continue
		}
		task, err := b.rt.schedDB.GetByName(ctx, name)
		if err != nil || strings.TrimSpace(task.LastOutput) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", task.Name, truncateTelegramText(task.LastOutput, 800)))
	}
	return strings.Join(lines, "\n")
}

func safeAgentJobTools(requested []string) []string {
	out := toolsets.FilterAllowed(requested, cron.AgentJobAllowedTools)
	if len(out) == 0 {
		return append([]string(nil), cron.DefaultAgentJobTools...)
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
