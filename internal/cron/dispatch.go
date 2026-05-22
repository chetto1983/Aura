package cron

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/identity"
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
	RunID         string
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

// LessonPromoter runs one lesson-promotion pass.
// Satisfied by an adapter in cmd/aura that calls learning.PromoteLessons.
type LessonPromoter interface {
	Promote(ctx context.Context) (promoted, skipped int, err error)
}

// ProposalTTLSweeper sweeps stale pending proposals older than a configured age.
// Satisfied by an adapter in cmd/aura that calls learning.SweepStaleProposals.
type ProposalTTLSweeper interface {
	Sweep(ctx context.Context) (purged int, err error)
}

// MemoryDecayRunner removes stale low-priority operational lessons.
type MemoryDecayRunner interface {
	Decay(ctx context.Context) (scanned, deleted, kept int, err error)
}

// HandlerConfig wires a Handler's dispatch dependencies.
type HandlerConfig struct {
	Notifier        Notifier
	AgentRunner     JobRunner
	Wiki            wiki.Repository
	Issues          IssueRepository
	Sources         AgentJobSourceReader
	SchedDB         AgentJobRepository
	Identity        identity.Delegator
	Logger          *slog.Logger
	Location        *time.Location
	Promoter        LessonPromoter
	ProposalSweeper ProposalTTLSweeper
	MemoryDecay     MemoryDecayRunner
	BackupVerifier  BackupVerifier
	WALCheckpointer WALCheckpointer
}

// Handler dispatches cron tasks using injected deps.
// It is the cron-package implementation of cron.Dispatcher and exposes
// RunNow for manual agent-job execution (wired via cmd/aura as ScheduledTaskRunner).
type Handler struct {
	notifier        Notifier
	runner          JobRunner
	wiki            wiki.Repository
	issues          IssueRepository
	sources         AgentJobSourceReader
	schedDB         AgentJobRepository
	identity        identity.Delegator
	logger          *slog.Logger
	loc             *time.Location
	promoter        LessonPromoter
	proposalSweeper ProposalTTLSweeper
	memoryDecay     MemoryDecayRunner
	backupVerifier  BackupVerifier
	walCheckpointer WALCheckpointer
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
		notifier:        cfg.Notifier,
		runner:          cfg.AgentRunner,
		wiki:            cfg.Wiki,
		issues:          cfg.Issues,
		sources:         cfg.Sources,
		schedDB:         cfg.SchedDB,
		identity:        cfg.Identity,
		logger:          logger,
		loc:             loc,
		promoter:        cfg.Promoter,
		proposalSweeper: cfg.ProposalSweeper,
		memoryDecay:     cfg.MemoryDecay,
		backupVerifier:  cfg.BackupVerifier,
		walCheckpointer: cfg.WALCheckpointer,
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
	case KindLessonPromotion:
		return h.dispatchLessonPromotion(ctx)
	case KindProposalTTLSweep:
		return h.dispatchProposalTTLSweep(ctx)
	case KindMemoryDecay:
		return h.dispatchMemoryDecay(ctx)
	case KindBackupVerify:
		return h.dispatchBackupVerify(ctx)
	case KindWALCheckpoint:
		return h.dispatchWALCheckpoint(ctx)
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
