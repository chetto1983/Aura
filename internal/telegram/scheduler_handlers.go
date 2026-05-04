package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
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
	case scheduler.KindAutoImprove:
		return b.dispatchAutoImprove(ctx)
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
	Payload       scheduler.AgentJobPayload
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

func (b *Bot) runAgentJob(ctx context.Context, task *scheduler.Task) (agentJobRun, error) {
	if b.agentRunner == nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: agent runner unavailable", task.Name)
	}
	payload, err := scheduler.NormalizeAgentJobPayload(task.Payload)
	if err != nil {
		return agentJobRun{}, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	allowlist := safeAgentJobTools(payload.ToolAllowlist)
	wakeSignature, hasWakeSignature := scheduler.AgentJobWakeSignature(ctx, payload, scheduler.AgentJobWakeDeps{
		Wiki:    b.wiki,
		Sources: b.sources,
		Tasks:   b.schedDB,
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
	result, err := b.agentRunner.Run(ctx, agent.Task{
		SystemPrompt:  agentJobSystemPrompt(payload, now, time.Local),
		Prompt:        b.agentJobPrompt(ctx, task, payload, now, time.Local),
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

func (b *Bot) logAgentJobRun(task *scheduler.Task, run agentJobRun) {
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

func (b *Bot) persistAgentJobResult(ctx context.Context, task *scheduler.Task, run agentJobRun) {
	if b.schedDB == nil || task == nil || task.ID == 0 {
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
	if err := b.schedDB.RecordAgentJobResult(ctx, task.ID, truncateTelegramText(run.Result.Content, 4000), string(data), run.WakeSignature); err != nil {
		b.logger.Warn("agent job result persistence failed", "name", task.Name, "error", err)
	}
}

func (b *Bot) notifyAgentJob(task *scheduler.Task, content string) (bool, error) {
	msg := agentJobNotificationMessage(task, content)
	if err := b.sendGeneratedToUser(task.RecipientID, msg); err != nil {
		return false, fmt.Errorf("agent_job %q notify: %w", task.Name, err)
	}
	return true, nil
}

func agentJobNotificationMessage(task *scheduler.Task, content string) string {
	name := ""
	if task != nil {
		name = task.Name
	}
	return fmt.Sprintf("Agent job %q completed.\n\n%s", name, truncateTelegramText(content, 3200))
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
	b.persistAgentJobResult(ctx, task, run)
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
		Skipped:          run.Skipped,
		WakeSignature:    run.WakeSignature,
		ToolAllowlist:    run.ToolAllowlist,
	}, nil
}

func agentJobSystemPrompt(payload scheduler.AgentJobPayload, now time.Time, loc *time.Location) string {
	prompt := "You are Aura running a scheduled agent job. Complete the saved routine with concise, evidence-oriented work. Write policy: " + payload.WritePolicy + ". Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state directly. If durable memory growth is useful, use propose_wiki_change so the user can review it. If reusable procedural knowledge is useful, use propose_skill_change so the user can review it. Return a short report with what you checked, any proposal created, and unresolved issues."
	if len(payload.Skills) > 0 {
		prompt += " This job is skill-backed: inspect attached skills with read_skill when available before applying their procedures."
	}
	if len(payload.WakeIfChanged) > 0 {
		prompt += " Respect wake_if_changed as a no-op guard: check those signals first and finish quickly with no proposal if there is no material change."
	}
	prompt += conversation.RenderRuntimeContext(now, loc)
	return prompt
}

func (b *Bot) agentJobPrompt(ctx context.Context, task *scheduler.Task, payload scheduler.AgentJobPayload, now time.Time, loc *time.Location) string {
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
		fmt.Fprintf(&sb, "\n\nAttached skills: %s\nUse read_skill on these names before relying on their procedures. Do not install, delete, or edit skills directly.", strings.Join(payload.Skills, ", "))
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

func agentJobScheduleContext(task *scheduler.Task, now time.Time, loc *time.Location) string {
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
		case scheduler.ScheduleDaily:
			fmt.Fprintf(&sb, " daily=%s", task.ScheduleDaily)
			if task.ScheduleWeekdays != "" {
				fmt.Fprintf(&sb, " weekdays=%s", task.ScheduleWeekdays)
			}
		case scheduler.ScheduleEvery:
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
	if b.schedDB == nil {
		return ""
	}
	var lines []string
	for _, anchor := range anchors {
		name, ok := scheduler.AgentJobTaskAnchor(anchor)
		if !ok {
			continue
		}
		task, err := b.schedDB.GetByName(ctx, name)
		if err != nil || strings.TrimSpace(task.LastOutput) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", task.Name, truncateTelegramText(task.LastOutput, 800)))
	}
	return strings.Join(lines, "\n")
}

func safeAgentJobTools(requested []string) []string {
	out := toolsets.FilterAllowed(requested, scheduler.AgentJobAllowedTools)
	if len(out) == 0 {
		return append([]string(nil), scheduler.DefaultAgentJobTools...)
	}
	return out
}

func (b *Bot) dispatchAutoImprove(ctx context.Context) error {
	if b.llm == nil {
		return fmt.Errorf("auto_improve: no LLM client available")
	}
	if b.toolReg == nil {
		return fmt.Errorf("auto_improve: no tool registry available")
	}
	if b.archiveDB == nil {
		return fmt.Errorf("auto_improve: no conversation archive available")
	}

	logger := b.logger.With("component", "auto_improve")

	recentTurns, err := b.archiveDB.ListAll(ctx, 200)
	if err != nil {
		return fmt.Errorf("auto_improve: scan archive: %w", err)
	}

	var convSummary strings.Builder
	convSummary.WriteString("Recent conversations:\n\n")
	for _, turn := range recentTurns {
		if turn.Role == "user" {
			convSummary.WriteString(fmt.Sprintf("User: %s\n", truncate(turn.Content, 200)))
		} else if turn.Role == "assistant" && (strings.Contains(turn.Content, "I can't") || strings.Contains(turn.Content, "I don't know")) {
			convSummary.WriteString(fmt.Sprintf("Assistant (low-confidence): %s\n", truncate(turn.Content, 200)))
		}
	}

	tools, _ := b.toolReg.ListTools()
	var toolsSummary strings.Builder
	for _, t := range tools {
		toolsSummary.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
	}

	prompt := fmt.Sprintf(`You are Aura's self-improvement system. Review recent conversations and existing tools.

%s

Existing tools:
%s

Identify up to 3 gaps where a new Python tool would make future conversations better.
For each gap, write the tool code and propose it for the registry.
Focus on patterns where users asked for things Aura couldn't do or where a reusable script would save time.

Respond ONLY with a JSON array of tool proposals:
[{"name": "tool_name", "description": "...", "params": "...", "code": "...", "usage": "..."}]
If no gaps found, respond with [].`, convSummary.String(), toolsSummary.String())

	req := llm.Request{
		Messages: []llm.Message{{Role: "user", Content: prompt}},
		Model:    b.cfg.LLMModel,
	}

	resp, err := b.llm.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("auto_improve: LLM call: %w", err)
	}

	var proposals []autoImproveProposal
	if err := json.Unmarshal([]byte(resp.Content), &proposals); err != nil {
		logger.Warn("auto_improve: failed to parse LLM proposals", "error", err)
		return nil
	}

	mode := strings.ToLower(strings.TrimSpace(b.cfg.SandboxAutoImproveMode))
	if mode == "" {
		mode = "dry_run"
	}

	switch mode {
	case "dry_run":
		return b.proposeAutoImproveTools(ctx, proposals, logger)
	case "auto_apply":
		return b.applyAutoImproveTools(ctx, proposals, logger)
	default:
		logger.Warn("auto_improve: unknown mode, defaulting to dry_run", "mode", mode)
		return b.proposeAutoImproveTools(ctx, proposals, logger)
	}
}

func (b *Bot) proposeAutoImproveTools(ctx context.Context, proposals []autoImproveProposal, logger *slog.Logger) error {
	if len(proposals) == 0 {
		for _, ownerID := range b.collectOwnerIDs() {
			b.SendToUser(ownerID, "Auto-improve scan complete: no gaps found.")
		}
		return nil
	}

	for _, p := range proposals {
		if _, err := b.toolReg.GetToolCode(p.Name); err == nil {
			logger.Info("auto_improve: tool already exists, skipping proposal", "name", p.Name)
			continue
		}
		msg := fmt.Sprintf("Auto-improve proposes a new tool:\n\n**%s** — %s\n\nParams: %s\nUsage: %s\n\n```python\n%s\n```\n\nApprove with /approve_tool %s or ignore.",
			p.Name, p.Description, p.Params, p.Usage, truncate(p.Code, 2000), p.Name)
		for _, ownerID := range b.collectOwnerIDs() {
			if err := b.SendToUser(ownerID, msg); err != nil {
				logger.Warn("auto_improve: failed to send proposal", "name", p.Name, "owner_id", ownerID, "error", err)
			}
		}
	}
	return nil
}

type autoImproveProposal struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Params      string `json:"params"`
	Code        string `json:"code"`
	Usage       string `json:"usage"`
}

func (b *Bot) applyAutoImproveTools(ctx context.Context, proposals []autoImproveProposal, logger *slog.Logger) error {
	for _, p := range proposals {
		if _, err := b.toolReg.GetToolCode(p.Name); err == nil {
			logger.Info("auto_improve: tool already exists, skipping", "name", p.Name)
			continue
		}

		if b.sandboxMgr != nil {
			if err := b.sandboxMgr.ValidateCode(p.Code); err != nil {
				logger.Warn("auto_improve: tool failed validation, skipping", "name", p.Name, "error", err)
				continue
			}
			result, err := b.sandboxMgr.Execute(ctx, p.Code, false)
			if err != nil || !result.OK {
				errMsg := "sandbox error"
				if err != nil {
					errMsg = err.Error()
				} else {
					errMsg = result.Stderr
				}
				logger.Warn("auto_improve: tool failed sandbox execution, skipping", "name", p.Name, "error", errMsg)
				continue
			}
		}

		if err := b.toolReg.SaveTool(ctx, p.Name, p.Description, p.Params, p.Code, p.Usage); err != nil {
			logger.Warn("auto_improve: failed to save tool", "name", p.Name, "error", err)
			continue
		}

		logger.Info("auto_improve: saved new tool", "name", p.Name)

		for _, ownerID := range b.collectOwnerIDs() {
			if err := b.SendToUser(ownerID, fmt.Sprintf("Auto-improve saved a new tool: **%s** — %s", p.Name, p.Description)); err != nil {
				logger.Warn("auto_improve: failed to notify owner", "owner_id", ownerID, "error", err)
			}
		}
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func truncateTelegramText(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
