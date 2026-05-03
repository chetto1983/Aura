package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
)

func TestDailyBriefingTool_ComposesAttentionSignals(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	sched := newTestSchedStore(t)
	srcStore, err := source.NewStore(filepath.Join(t.TempDir(), "wiki"), nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	summaries := summarizer.NewSummariesStore(sched.DB())
	issues := scheduler.NewIssuesStore(sched.DB())
	archive, err := conversation.NewArchiveStore(sched.DB())
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}

	_, err = sched.Upsert(ctx, &scheduler.Task{
		Name:         "call-client",
		Kind:         scheduler.KindReminder,
		Payload:      "call client about renewal",
		ScheduleKind: scheduler.ScheduleAt,
		ScheduleAt:   now.Add(2 * time.Hour),
		NextRunAt:    now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Upsert task: %v", err)
	}
	if _, err := summaries.Propose(ctx, summarizer.ProposalInput{
		Fact:       "Add a [[daily-briefing]] operating note.",
		Action:     string(summarizer.ActionNew),
		Similarity: 0.8,
	}); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if err := issues.Enqueue(ctx, scheduler.Issue{
		Kind:     "broken_link",
		Severity: "high",
		Slug:     "project-aura",
		Message:  "Missing [[daily-briefing]] page.",
	}); err != nil {
		t.Fatalf("Enqueue issue: %v", err)
	}
	src, _, err := srcStore.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "morning-note.txt",
		MimeType: "text/plain",
		Bytes:    []byte("Remember the renewal conversation."),
	})
	if err != nil {
		t.Fatalf("Put source: %v", err)
	}
	if err := archive.Append(ctx, conversation.Turn{
		ChatID:    1,
		UserID:    1,
		TurnIndex: 1,
		Role:      "user",
		Content:   "Tomorrow we need to follow up on Aura daily usefulness.",
	}); err != nil {
		t.Fatalf("Append turn: %v", err)
	}

	tool := NewDailyBriefingTool(sched, srcStore, summaries, issues, archive, time.UTC)
	tool.now = func() time.Time { return now }
	out, err := tool.Execute(ctx, map[string]any{"limit": float64(5)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, want := range []string{
		"Daily briefing",
		"call-client",
		"call client about renewal",
		"Pending wiki proposals",
		"daily-briefing",
		"Open wiki issues",
		"project-aura",
		"Recent sources",
		src.ID,
		"morning-note.txt",
		"Recent conversation signals",
		"daily usefulness",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("briefing missing %q:\n%s", want, out)
		}
	}
}

func TestDailyBriefingTool_HandlesEmptyStores(t *testing.T) {
	tool := NewDailyBriefingTool(nil, nil, nil, nil, nil, time.UTC)
	if tool != nil {
		t.Fatal("expected nil tool when every store is nil")
	}

	sched := newTestSchedStore(t)
	tool = NewDailyBriefingTool(sched, nil, nil, nil, nil, time.UTC)
	tool.now = func() time.Time { return time.Now().UTC() }
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No active tasks due before the end of today") {
		t.Fatalf("empty briefing did not explain task state:\n%s", out)
	}
	if !strings.Contains(out, "Source inbox unavailable") {
		t.Fatalf("empty briefing did not explain unavailable source inbox:\n%s", out)
	}
}
