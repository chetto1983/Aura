package cron

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	toolsets "github.com/aura/aura/internal/agent/tools/sets"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/wiki"
)

// Notifier is the user-facing delivery surface the dispatcher depends on.
// *telegram.Bot implements this for Telegram-channel delivery.
type Notifier interface {
	// SendReminder delivers a plain-text reminder to a Telegram user.
	SendReminder(userID, body string) error
	// SendCompletion delivers a Markdown agent-job completion report.
	SendCompletion(userID, body string) error
	// NotifyOwners broadcasts a plain-text maintenance message to all owners.
	NotifyOwners(ctx context.Context, msg string)
}

// JobRunner executes a bounded scheduled agent job.
// Satisfied by agentJobRunnerAdapter (cmd/aura) which calls agent.RunTask
// (import-cycle boundary: agent package cannot depend on cron).
type JobRunner interface {
	RunJob(ctx context.Context, req JobRequest) (JobResult, error)
}

// JobRequest is the cron-native agent task request passed to JobRunner.
type JobRequest struct {
	SystemPrompt  string
	Prompt        string
	ToolAllowlist []string
	UserID        string
	Temperature   *float64
}

// JobResult is the cron-native outcome returned by JobRunner.
type JobResult struct {
	Content          string
	LLMCalls         int
	ToolCalls        int
	TokensPrompt     int
	TokensCompletion int
	TokensTotal      int
	Elapsed          time.Duration
}

// RunNowResult is returned by Handler.RunNow for manual agent-job execution.
type RunNowResult struct {
	OK               bool
	Name             string
	Kind             string
	Status           string
	Summary          string
	LastError        string
	LLMCalls         int
	ToolCalls        int
	TokensPrompt     int
	TokensCompletion int
	TokensTotal      int
	ElapsedMS        int64
	Notified         bool
	Skipped          bool
	WakeSignature    string
	ToolAllowlist    []string
}

// HandlerConfig wires a Handler's dispatch dependencies.
type HandlerConfig struct {
	Notifier    Notifier
	AgentRunner JobRunner
	Wiki        wiki.Repository
	Issues      IssueRepository
	Sources     AgentJobSourceReader
	SchedDB     AgentJobRepository
	Logger      *slog.Logger
	Location    *time.Location
}

// Handler dispatches cron tasks using injected deps.
// It is the cron-package implementation of cron.Dispatcher and exposes
// RunNow for manual agent-job execution (wired via cmd/aura as ScheduledTaskRunner).
type Handler struct {
	notifier Notifier
	runner   JobRunner
	wiki     wiki.Repository
	issues   IssueRepository
	sources  AgentJobSourceReader
	schedDB  AgentJobRepository
	logger   *slog.Logger
	loc      *time.Location
}

// NewHandler constructs a Handler from the supplied config.
func NewHandler(cfg HandlerConfig) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	loc := cfg.Location
	if loc == nil {
		loc = time.Local
	}
	return &Handler{
		notifier: cfg.Notifier,
		runner:   cfg.AgentRunner,
		wiki:     cfg.Wiki,
		issues:   cfg.Issues,
		sources:  cfg.Sources,
		schedDB:  cfg.SchedDB,
		logger:   logger,
		loc:      loc,
	}
}

// Dispatch routes a fired task to the right side-effect.
// Satisfies cron.Dispatcher — use h.Dispatch as cron.Config.Dispatcher.
func (h *Handler) Dispatch(ctx context.Context, task *Task) error {
	switch task.Kind {
	case KindReminder:
		return h.dispatchReminder(task)
	case KindWikiMaintenance:
		return h.dispatchWikiMaintenance(ctx)
	default:
		return fmt.Errorf("dispatchTask: unknown kind %q", task.Kind)
	}
}

// RunNow manually fires a named agent_job task and returns a detailed result.
func (h *Handler) RunNow(ctx context.Context, name string) (RunNowResult, error) {
	if h.schedDB == nil {
		return RunNowResult{}, errors.New("scheduler store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return RunNowResult{}, errors.New("task name required")
	}
	task, err := h.schedDB.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunNowResult{}, fmt.Errorf("task %q not found", name)
		}
		return RunNowResult{}, err
	}
	if task.Kind != KindAgentJob {
		return RunNowResult{}, fmt.Errorf("task %q is kind %q; run_task_now MVP supports agent_job only", task.Name, task.Kind)
	}
	if task.Status == StatusCancelled {
		return RunNowResult{}, fmt.Errorf("task %q is cancelled", task.Name)
	}

	started := time.Now().UTC()
	run, runErr := h.runAgentJob(ctx, task)
	status := "completed"
	lastErr := ""
	if runErr != nil {
		status = "failed"
		lastErr = runErr.Error()
	}
	if runErr == nil && run.Payload.Notify != nil && *run.Payload.Notify && task.RecipientID != "" {
		notified, notifyErr := h.notifyAgentJob(task, run.Result.Content)
		run.Notified = notified
		if notifyErr != nil {
			status = "failed"
			lastErr = notifyErr.Error()
		}
	}
	h.persistAgentJobResult(ctx, task, run)
	if err := h.schedDB.RecordManualRun(ctx, task.ID, started, lastErr); err != nil && lastErr == "" {
		status = "failed"
		lastErr = err.Error()
	}

	return RunNowResult{
		OK:               lastErr == "",
		Name:             task.Name,
		Kind:             string(task.Kind),
		Status:           status,
		Summary:          truncate(run.Result.Content, 1600),
		LastError:        lastErr,
		LLMCalls:         run.Result.LLMCalls,
		ToolCalls:        run.Result.ToolCalls,
		TokensPrompt:     run.Result.TokensPrompt,
		TokensCompletion: run.Result.TokensCompletion,
		TokensTotal:      run.Result.TokensTotal,
		ElapsedMS:        run.Result.Elapsed.Milliseconds(),
		Notified:         run.Notified,
		Skipped:          run.Skipped,
		WakeSignature:    run.WakeSignature,
		ToolAllowlist:    run.ToolAllowlist,
	}, nil
}

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

// ---- agent job internals ----------------------------------------------------

type agentJobRun struct {
	Payload       AgentJobPayload
	ToolAllowlist []string
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
	now := time.Now()
	zero := 0.0
	result, err := h.runner.RunJob(ctx, JobRequest{
		SystemPrompt:  AgentJobSystemPrompt(payload, now, h.loc),
		Prompt:        h.agentJobPrompt(ctx, task, payload, now, h.loc),
		ToolAllowlist: allowlist,
		UserID:        task.RecipientID,
		Temperature:   &zero,
	})
	run := agentJobRun{Payload: payload, ToolAllowlist: allowlist, Result: result, WakeSignature: wakeSignature}
	if err != nil {
		return run, fmt.Errorf("agent_job %q: %w", task.Name, err)
	}
	return run, nil
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

// ---- prompt builders --------------------------------------------------------

func agentJobNotificationMessage(task *Task, payload AgentJobPayload, content string) string {
	name := ""
	if task != nil {
		name = task.Name
	}
	if payload.Language == "it" {
		return fmt.Sprintf("Job agente %q completato.\n\n%s", name, truncate(content, 3200))
	}
	return fmt.Sprintf("Agent job %q completed.\n\n%s", name, truncate(content, 3200))
}

func AgentJobSystemPrompt(payload AgentJobPayload, now time.Time, loc *time.Location) string {
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

// AgentJobUserPrompt builds a minimal user-turn prompt for Hub-routed agent jobs.
// It omits schedule context and prior outputs (not available from InboundMessage alone).
func AgentJobUserPrompt(payload AgentJobPayload) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Goal: %s", payload.Goal)
	if len(payload.EnabledToolsets) > 0 {
		fmt.Fprintf(&sb, "\n\nEnabled toolsets: %s", strings.Join(payload.EnabledToolsets, ", "))
	}
	if len(payload.Skills) > 0 {
		fmt.Fprintf(&sb, "\n\nAttached skills: %s\nFind and read the matching SKILL.md files before relying on their procedures. Do not install, delete, or edit skills directly.", strings.Join(payload.Skills, ", "))
	}
	if len(payload.ContextFrom) > 0 {
		fmt.Fprintf(&sb, "\n\nContext anchors: %s\nUse these anchors as the first retrieval targets.", strings.Join(payload.ContextFrom, ", "))
	}
	if len(payload.WakeIfChanged) > 0 {
		fmt.Fprintf(&sb, "\n\nWake-if-changed signals: %s\nBefore doing the full routine, check whether these signals changed materially. If not, return a concise no-change report and stop.", strings.Join(payload.WakeIfChanged, ", "))
	}
	return sb.String()
}

func agentJobLanguageInstruction(language string) string {
	switch NormalizeAgentJobLanguage(language) {
	case "it":
		return "Output language: Italian. Write the final report and all user-facing prose in Italian, regardless of tool or web result language."
	case "en":
		return "Output language: English. Write the final report and all user-facing prose in English, regardless of tool or web result language."
	default:
		return ""
	}
}

func (h *Handler) agentJobPrompt(ctx context.Context, task *Task, payload AgentJobPayload, now time.Time, loc *time.Location) string {
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
		if prior := h.agentJobPriorOutputs(ctx, payload.ContextFrom); prior != "" {
			fmt.Fprintf(&sb, "\n\nPrior job outputs:\n%s", prior)
		}
	}
	if len(payload.WakeIfChanged) > 0 {
		fmt.Fprintf(&sb, "\n\nWake-if-changed signals: %s\nBefore doing the full routine, check whether these signals changed materially. If not, return a concise no-change report and stop.", strings.Join(payload.WakeIfChanged, ", "))
	}
	return sb.String()
}

func agentJobScheduleContext(task *Task, now time.Time, loc *time.Location) string {
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
		case ScheduleDaily:
			fmt.Fprintf(&sb, " daily=%s", task.ScheduleDaily)
			if task.ScheduleWeekdays != "" {
				fmt.Fprintf(&sb, " weekdays=%s", task.ScheduleWeekdays)
			}
		case ScheduleEvery:
			fmt.Fprintf(&sb, " every_minutes=%d", task.ScheduleEveryMinutes)
		}
		sb.WriteString("\n")
	}
	if delay := now.Sub(task.NextRunAt); delay > time.Minute {
		fmt.Fprintf(&sb, "Run delay: %s. Treat current-date research as of Running at, not Scheduled for, unless the goal explicitly asks for historical state.\n", delay.Round(time.Minute))
	}
	return strings.TrimSpace(sb.String())
}

func (h *Handler) agentJobPriorOutputs(ctx context.Context, anchors []string) string {
	if h.schedDB == nil {
		return ""
	}
	var lines []string
	for _, anchor := range anchors {
		name, ok := AgentJobTaskAnchor(anchor)
		if !ok {
			continue
		}
		task, err := h.schedDB.GetByName(ctx, name)
		if err != nil || strings.TrimSpace(task.LastOutput) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", task.Name, truncate(task.LastOutput, 800)))
	}
	return strings.Join(lines, "\n")
}

// ---- helpers ----------------------------------------------------------------

func SafeAgentJobTools(requested []string) []string {
	out := toolsets.FilterAllowed(requested, AgentJobAllowedTools)
	if len(out) == 0 {
		return append([]string(nil), DefaultAgentJobTools...)
	}
	return out
}

func truncate(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
