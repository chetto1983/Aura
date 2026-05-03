package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
)

const dailyBriefingMaxChars = 8000

// DailyBriefingTool composes the read-only stores that matter for the
// everyday "what needs my attention?" question.
type DailyBriefingTool struct {
	tasks     *scheduler.Store
	sources   *source.Store
	summaries *summarizer.SummariesStore
	issues    *scheduler.IssuesStore
	archive   *conversation.ArchiveStore
	loc       *time.Location
	now       func() time.Time
}

func NewDailyBriefingTool(
	tasks *scheduler.Store,
	sources *source.Store,
	summaries *summarizer.SummariesStore,
	issues *scheduler.IssuesStore,
	archive *conversation.ArchiveStore,
	loc *time.Location,
) *DailyBriefingTool {
	if tasks == nil && sources == nil && summaries == nil && issues == nil && archive == nil {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	return &DailyBriefingTool{
		tasks:     tasks,
		sources:   sources,
		summaries: summaries,
		issues:    issues,
		archive:   archive,
		loc:       loc,
		now:       time.Now,
	}
}

func (t *DailyBriefingTool) Name() string { return "daily_briefing" }

func (t *DailyBriefingTool) Description() string {
	return "Build a read-only daily briefing from Aura's tasks, pending wiki proposals, source inbox, wiki issues, and recent conversation archive. Use when the user asks what to do today, what changed, or for a morning briefing."
}

func (t *DailyBriefingTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum rows per section. Defaults to 5.",
			},
		},
	}
}

func (t *DailyBriefingTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	limit, _, err := positiveIntArg(args, "limit")
	if err != nil {
		return "", fmt.Errorf("%s", strings.Replace(err.Error(), "schedule_task: ", "daily_briefing: ", 1))
	}
	if limit == 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	now := t.now().In(t.loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, t.loc)
	end := start.Add(24 * time.Hour)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Daily briefing\n\n")
	fmt.Fprintf(&sb, "Local date: %s\n\n", now.Format("2006-01-02 15:04 MST"))

	if err := t.writeTasksSection(ctx, &sb, now, end, limit); err != nil {
		return "", err
	}
	if err := t.writeProposalsSection(ctx, &sb, limit); err != nil {
		return "", err
	}
	if err := t.writeIssuesSection(ctx, &sb, limit); err != nil {
		return "", err
	}
	if err := t.writeSourcesSection(&sb, limit); err != nil {
		return "", err
	}
	if err := t.writeConversationsSection(ctx, &sb, start, limit); err != nil {
		return "", err
	}

	return truncateForToolContext(sb.String(), dailyBriefingMaxChars), nil
}

func (t *DailyBriefingTool) writeTasksSection(ctx context.Context, sb *strings.Builder, now, end time.Time, limit int) error {
	fmt.Fprintf(sb, "## Tasks needing attention\n")
	if t.tasks == nil {
		fmt.Fprintf(sb, "- Scheduler unavailable.\n\n")
		return nil
	}
	tasks, err := t.tasks.List(ctx, scheduler.StatusActive)
	if err != nil {
		return fmt.Errorf("daily_briefing: list tasks: %w", err)
	}
	written := 0
	for _, task := range tasks {
		due := task.NextRunAt.In(t.loc)
		if due.After(end) {
			continue
		}
		label := "today"
		if due.Before(now) {
			label = "overdue"
		}
		payload := ""
		if task.Kind == scheduler.KindReminder && strings.TrimSpace(task.Payload) != "" {
			payload = " - " + oneLine(task.Payload, 90)
		}
		fmt.Fprintf(sb, "- [%s] `%s` %s at %s%s\n", label, task.Name, task.Kind, due.Format("15:04"), payload)
		written++
		if written >= limit {
			break
		}
	}
	if written == 0 {
		fmt.Fprintf(sb, "- No active tasks due before the end of today.\n")
	}
	fmt.Fprintf(sb, "\n")
	return nil
}

func (t *DailyBriefingTool) writeProposalsSection(ctx context.Context, sb *strings.Builder, limit int) error {
	fmt.Fprintf(sb, "## Pending wiki proposals\n")
	if t.summaries == nil {
		fmt.Fprintf(sb, "- Review queue unavailable.\n\n")
		return nil
	}
	proposals, err := t.summaries.List(ctx, "pending", limit)
	if err != nil {
		return fmt.Errorf("daily_briefing: list proposals: %w", err)
	}
	if len(proposals) == 0 {
		fmt.Fprintf(sb, "- No pending wiki proposals.\n\n")
		return nil
	}
	for _, proposal := range proposals {
		target := proposal.TargetSlug
		if target == "" {
			target = "new page"
		}
		fmt.Fprintf(sb, "- #%d %s -> %s: %s\n", proposal.ID, proposal.Action, target, oneLine(proposal.Fact, 120))
	}
	fmt.Fprintf(sb, "\n")
	return nil
}

func (t *DailyBriefingTool) writeIssuesSection(ctx context.Context, sb *strings.Builder, limit int) error {
	fmt.Fprintf(sb, "## Open wiki issues\n")
	if t.issues == nil {
		fmt.Fprintf(sb, "- Wiki issue queue unavailable.\n\n")
		return nil
	}
	issues, err := t.issues.List(ctx, "open")
	if err != nil {
		return fmt.Errorf("daily_briefing: list wiki issues: %w", err)
	}
	if len(issues) == 0 {
		fmt.Fprintf(sb, "- No open wiki issues.\n\n")
		return nil
	}
	for i, issue := range issues {
		if i >= limit {
			break
		}
		fmt.Fprintf(sb, "- #%d [%s] %s on [[%s]]: %s\n", issue.ID, issue.Severity, issue.Kind, issue.Slug, oneLine(issue.Message, 110))
	}
	fmt.Fprintf(sb, "\n")
	return nil
}

func (t *DailyBriefingTool) writeSourcesSection(sb *strings.Builder, limit int) error {
	fmt.Fprintf(sb, "## Recent sources\n")
	if t.sources == nil {
		fmt.Fprintf(sb, "- Source inbox unavailable.\n\n")
		return nil
	}
	sources, err := t.sources.List(source.ListFilter{})
	if err != nil {
		return fmt.Errorf("daily_briefing: list sources: %w", err)
	}
	if len(sources) == 0 {
		fmt.Fprintf(sb, "- No sources stored yet.\n\n")
		return nil
	}
	for i, src := range sources {
		if i >= limit {
			break
		}
		note := ""
		if src.Error != "" {
			note = " - error: " + oneLine(src.Error, 80)
		}
		fmt.Fprintf(sb, "- `%s` %s %s (%s)%s\n", src.ID, src.Kind, src.Status, src.Filename, note)
	}
	fmt.Fprintf(sb, "\n")
	return nil
}

func (t *DailyBriefingTool) writeConversationsSection(ctx context.Context, sb *strings.Builder, start time.Time, limit int) error {
	fmt.Fprintf(sb, "## Recent conversation signals\n")
	if t.archive == nil {
		fmt.Fprintf(sb, "- Conversation archive unavailable.\n\n")
		return nil
	}
	turns, err := t.archive.ListAll(ctx, limit)
	if err != nil {
		return fmt.Errorf("daily_briefing: list conversations: %w", err)
	}
	written := 0
	startUTC := start.UTC()
	for _, turn := range turns {
		if turn.CreatedAt.Before(startUTC) {
			continue
		}
		fmt.Fprintf(sb, "- %s: %s\n", turn.Role, oneLine(turn.Content, 120))
		written++
		if written >= limit {
			break
		}
	}
	if written == 0 {
		fmt.Fprintf(sb, "- No archived conversation turns from today.\n")
	}
	fmt.Fprintf(sb, "\n")
	return nil
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
