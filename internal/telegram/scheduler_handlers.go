package telegram

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	wakeSignature, hasWakeSignature := b.agentJobWakeSignature(ctx, payload)
	if hasWakeSignature && task.WakeSignature != "" && task.WakeSignature == wakeSignature {
		return agentJobRun{
			Payload:       payload,
			ToolAllowlist: allowlist,
			Result:        agent.Result{Content: "Agent job skipped: wake_if_changed signals unchanged."},
			Skipped:       true,
			WakeSignature: wakeSignature,
		}, nil
	}
	result, err := b.agentRunner.Run(ctx, agent.Task{
		SystemPrompt:  agentJobSystemPrompt(payload),
		Prompt:        b.agentJobPrompt(ctx, payload),
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

func agentJobSystemPrompt(payload scheduler.AgentJobPayload) string {
	prompt := "You are Aura running a scheduled agent job. Complete the saved routine with concise, evidence-oriented work. Write policy: " + payload.WritePolicy + ". Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state directly. If durable memory growth is useful, use propose_wiki_change so the user can review it. If reusable procedural knowledge is useful, use propose_skill_change so the user can review it. Return a short report with what you checked, any proposal created, and unresolved issues."
	if len(payload.Skills) > 0 {
		prompt += " This job is skill-backed: inspect attached skills with read_skill when available before applying their procedures."
	}
	if len(payload.WakeIfChanged) > 0 {
		prompt += " Respect wake_if_changed as a no-op guard: check those signals first and finish quickly with no proposal if there is no material change."
	}
	return prompt
}

func (b *Bot) agentJobPrompt(ctx context.Context, payload scheduler.AgentJobPayload) string {
	var sb strings.Builder
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

func (b *Bot) agentJobPriorOutputs(ctx context.Context, anchors []string) string {
	if b.schedDB == nil {
		return ""
	}
	var lines []string
	for _, anchor := range anchors {
		name, ok := agentJobTaskAnchor(anchor)
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

func (b *Bot) agentJobWakeSignature(ctx context.Context, payload scheduler.AgentJobPayload) (string, bool) {
	if len(payload.WakeIfChanged) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(payload.WakeIfChanged))
	for _, signal := range payload.WakeIfChanged {
		if part, ok := b.agentJobWakePart(ctx, signal); ok {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), true
}

func (b *Bot) agentJobWakePart(ctx context.Context, signal string) (string, bool) {
	signal = strings.TrimSpace(signal)
	if signal == "" {
		return "", false
	}
	if slug, ok := wikiSignalSlug(signal); ok {
		if b.wiki == nil {
			return "", false
		}
		page, err := b.wiki.ReadPage(slug)
		if err != nil {
			return "wiki:" + slug + ":missing", true
		}
		return fmt.Sprintf("wiki:%s:%s:%s:%s", slug, page.UpdatedAt, page.Title, strings.Join(page.Related, ",")), true
	}
	if id, ok := strings.CutPrefix(signal, "source:"); ok {
		if b.sources == nil {
			return "", false
		}
		id = strings.TrimSpace(id)
		src, err := b.sources.Get(id)
		if err != nil {
			return "source:" + id + ":missing", true
		}
		return fmt.Sprintf("source:%s:%s:%s:%d:%s:%s", src.ID, src.SHA256, src.Status, src.PageCount, strings.Join(src.WikiPages, ","), src.Error), true
	}
	if name, ok := agentJobTaskAnchor(signal); ok {
		if b.schedDB == nil {
			return "", false
		}
		task, err := b.schedDB.GetByName(ctx, name)
		if err != nil {
			return "task:" + name + ":missing", true
		}
		return fmt.Sprintf("task:%s:%s:%s:%s", task.Name, task.LastRunAt.UTC().Format(time.RFC3339), task.WakeSignature, task.LastError), true
	}
	return "", false
}

func wikiSignalSlug(signal string) (string, bool) {
	if slug, ok := strings.CutPrefix(signal, "wiki:"); ok {
		slug = strings.TrimSpace(slug)
		return slug, slug != ""
	}
	if strings.HasPrefix(signal, "[[") && strings.HasSuffix(signal, "]]") {
		slug := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(signal, "[["), "]]"))
		return slug, slug != ""
	}
	return "", false
}

func agentJobTaskAnchor(anchor string) (string, bool) {
	anchor = strings.TrimSpace(anchor)
	for _, prefix := range []string{"task:", "agent_job:"} {
		if name, ok := strings.CutPrefix(anchor, prefix); ok {
			name = strings.TrimSpace(name)
			return name, name != ""
		}
	}
	if anchor == "" || strings.Contains(anchor, ":") || strings.HasPrefix(anchor, "[[") {
		return "", false
	}
	return anchor, true
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

	var proposals []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Params      string `json:"params"`
		Code        string `json:"code"`
		Usage       string `json:"usage"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &proposals); err != nil {
		logger.Warn("auto_improve: failed to parse LLM proposals", "error", err)
		return nil
	}

	for _, p := range proposals {
		if _, err := b.toolReg.GetToolCode(p.Name); err == nil {
			logger.Info("auto_improve: tool already exists, skipping", "name", p.Name)
			continue
		}

		if err := b.toolReg.SaveTool(ctx, p.Name, p.Description, p.Params, p.Code, p.Usage); err != nil {
			logger.Warn("auto_improve: failed to save tool", "name", p.Name, "error", err)
			continue
		}

		logger.Info("auto_improve: saved new tool", "name", p.Name)

		for _, ownerID := range b.collectOwnerIDs() {
			b.SendToUser(ownerID, fmt.Sprintf("I wrote a new tool: **%s** — %s", p.Name, p.Description))
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
