package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// approvalTool is a task tool whose store keys rows by originating call, as migration 0155 does.
func approvalTool() (*TaskTool, *messageTaskStore) {
	store := &messageTaskStore{}
	return &TaskTool{Store: store, Destinations: &fakeDestinations{}}, store
}

func scheduleArgs(kind, schedule, payload string) string {
	return fmt.Sprintf(`{"action":"schedule","kind":%q,%s,"payload":%s,"notify":"stdout"}`, kind, schedule, payload)
}

func inTwoHours() string {
	return fmt.Sprintf(`"schedule_kind":"at","at":%q`, time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339))
}

// Decision 19: the gate before the call is the gate inside it.
func TestTheGateBeforeTheCallIsTheGateInsideIt(t *testing.T) {
	t.Parallel()
	every30 := `"schedule_kind":"every","every_minutes":30`
	cases := map[string]string{
		"a reminder":              scheduleArgs("reminder", inTwoHours(), `{"text":"Buy bread"}`),
		"a destructive agent_job": scheduleArgs("agent_job", inTwoHours(), `{"goal":"drop table users"}`),
		"an ordinary agent_job":   scheduleArgs("agent_job", inTwoHours(), `{"goal":"Riassumi la posta"}`),
		"a database backup":       scheduleArgs("backup_postgres", inTwoHours(), `{}`),
		"a reminder every 30m":    scheduleArgs("reminder", every30, `{"text":"Stretch"}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var a taskArgs
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				t.Fatal(err)
			}
			_, needs := taskNeedsApproval(a)
			reviewer, reviewed := approvalTool()
			pause, parked := reviewer.HostReview(context.Background(), "conv-1", "call-1", json.RawMessage(raw))
			executor, executed := approvalTool()
			if _, err := executor.Execute(context.Background(), json.RawMessage(raw)); err != nil {
				t.Fatalf("execute: %v", err)
			}
			gatedInside := executed.inputs[0].Status == "pending_approval"
			if parked != needs || gatedInside != needs {
				t.Fatalf("needs %v, parked %v, gated inside %v", needs, parked, gatedInside)
			}
			if parked && (pause == nil || reviewed.inputs[0].Status != "pending_approval") {
				t.Fatalf("parked without a pending task: %+v", reviewed.inputs)
			}
		})
	}
}

func TestTheApprovalQuestionNamesKindTimeAndGoal(t *testing.T) {
	t.Parallel()
	spec := resolvedSpec{ScheduleKind: "at", TZ: "Europe/Rome"}
	next := time.Date(2026, 11, 6, 7, 25, 0, 0, time.UTC)
	got := approvalQuestion("agent_job", "01a19af5-0000-7000-8000-000000000000", spec, next, json.RawMessage(`{"goal":"Riassumi la posta"}`))
	want := "Approve scheduled agent_job task 01a19af5 (once), first run 06/11 08:25 (Europe/Rome)? It runs: Riassumi la posta. It will not run until you approve."
	if got != want {
		t.Fatalf("question = %q\nwant       %q", got, want)
	}
}

func TestARecurringQuestionNamesItsCadence(t *testing.T) {
	t.Parallel()
	next := time.Date(2026, 11, 6, 7, 30, 0, 0, time.UTC)
	for spec, want := range map[resolvedSpec]string{
		{ScheduleKind: "every", EveryMinutes: 30, TZ: "UTC"}:        "(every 30m)",
		{ScheduleKind: "cron", CronExpr: "30 7 * * 1-5", TZ: "UTC"}: `(cron "30 7 * * 1-5")`,
	} {
		if got := approvalQuestion("agent_job", "id", spec, next, json.RawMessage(`{"goal":"g"}`)); !strings.Contains(got, want) {
			t.Errorf("question %q lacks %q", got, want)
		}
	}
}

func TestTheSummaryIsOneBoundedLine(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("riga\n", 60)
	got := taskSummary("agent_job", json.RawMessage(`{"goal":`+fmt.Sprintf("%q", long)+`}`))
	if utf8.RuneCountInString(got) != taskSummaryMaxRunes+1 || !strings.HasSuffix(got, "…") || strings.Contains(got, "\n") {
		t.Fatalf("summary = %q (%d runes)", got, utf8.RuneCountInString(got))
	}
	for kind, want := range map[string]string{"agent_job": "(no goal)", "reminder": "Buy bread", "backup_postgres": "backup_postgres"} {
		payload := `{}`
		if kind == "reminder" {
			payload = `{"text":"Buy  bread"}`
		}
		if got := taskSummary(kind, json.RawMessage(payload)); got != want {
			t.Errorf("%s summary = %q, want %q", kind, got, want)
		}
	}
}

func TestAnAgentJobIsParkedInTheChatThatCreatesIt(t *testing.T) {
	t.Parallel()
	tool, store := approvalTool()
	raw := scheduleArgs("agent_job", inTwoHours(), `{"goal":"Riassumi la posta"}`)
	pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", json.RawMessage(raw))
	if !ok || pause.ToolCallID != "call-1" || pause.Kind != KindApproval {
		t.Fatalf("pause = %+v, %v", pause, ok)
	}
	var resume map[string]string
	if err := json.Unmarshal(pause.ResumeContext, &resume); err != nil || resume["type"] != "scheduled_task_approval" || resume["task_id"] != "task-1" {
		t.Fatalf("resume context = %s, %v", pause.ResumeContext, err)
	}
	if !strings.HasPrefix(pause.Question, "Approve scheduled agent_job task ") || !strings.Contains(pause.Question, "It runs: ") {
		t.Fatalf("question = %q", pause.Question)
	}
	if created := store.inputs[0]; created.Status != "pending_approval" || created.OriginConversationID != "conv-1" {
		t.Fatalf("created = %+v", created)
	}
}

func TestAHarmlessTaskIsNotParked(t *testing.T) {
	t.Parallel()
	tool, store := approvalTool()
	raw := json.RawMessage(scheduleArgs("reminder", inTwoHours(), `{"text":"Buy bread"}`))
	if pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", raw); ok || pause != nil {
		t.Fatalf("parked a harmless reminder: %+v", pause)
	}
	if _, err := tool.Execute(context.Background(), raw); err != nil || store.inputs[0].Status != "active" {
		t.Fatalf("execute = %v, status %q", err, store.inputs[0].Status)
	}
}

// Decision 32: an agent_job with no goal is refused, never parked.
func TestAnAgentJobWithoutAGoalIsNeverParked(t *testing.T) {
	t.Parallel()
	tool, store := approvalTool()
	raw := json.RawMessage(scheduleArgs("agent_job", inTwoHours(), `{}`))
	if pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", raw); ok || pause != nil {
		t.Fatalf("parked a job with no goal: %+v", pause)
	}
	if _, err := tool.Execute(context.Background(), raw); err == nil || !strings.Contains(err.Error(), "payload.goal") {
		t.Fatalf("execute = %v, want the payload.goal refusal", err)
	}
	if store.creates != 0 {
		t.Fatal("a task was created")
	}
}

func TestAHeadlessScheduleWaitsWithoutADirective(t *testing.T) {
	t.Parallel()
	tool, _ := approvalTool()
	res, err := tool.Execute(context.Background(), json.RawMessage(scheduleArgs("agent_job", inTwoHours(), `{"goal":"g"}`)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Preview, "Do not call ask_user for it.") || strings.Contains(res.Preview, "resume_context") || strings.Contains(res.Preview, "Call ask_user") {
		t.Fatalf("preview = %q", res.Preview)
	}
}
