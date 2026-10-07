package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskPauseAndResumeReachTheStore(t *testing.T) {
	next := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
	store := &fakeTaskStore{resumeAt: next}
	tool := &TaskTool{Store: store}

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"pause","task_id":"t-1"}`))
	if err != nil || store.paused != "t-1" || !strings.Contains(res.Preview, "paused task t-1") {
		t.Fatalf("pause = %+v, %v (store saw %q)", res, err, store.paused)
	}
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"action":"resume","task_id":"t-1"}`))
	if err != nil || store.resumed != "t-1" || !strings.Contains(res.Preview, "next run 2030-01-02T09:00:00Z") {
		t.Fatalf("resume = %+v, %v (store saw %q)", res, err, store.resumed)
	}
}

func TestTaskPauseAndResumeRequireATaskID(t *testing.T) {
	store := &fakeTaskStore{}
	tool := &TaskTool{Store: store}
	for _, action := range []string{"pause", "resume"} {
		_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"`+action+`"}`))
		if err == nil || !strings.Contains(err.Error(), "task_id is required") {
			t.Fatalf("%s without task_id: err = %v, want task_id is required", action, err)
		}
	}
	if store.paused != "" || store.resumed != "" {
		t.Fatalf("store reached without a task id: paused %q resumed %q", store.paused, store.resumed)
	}
}

func TestTaskPauseAndResumeSurfaceStoreErrors(t *testing.T) {
	store := &fakeTaskStore{pauseErr: errors.New("not active"), resumeErr: errors.New("not paused")}
	tool := &TaskTool{Store: store}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"pause","task_id":"t"}`)); err == nil || !strings.Contains(err.Error(), "task pause: not active") {
		t.Fatalf("pause err = %v", err)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"resume","task_id":"t"}`)); err == nil || !strings.Contains(err.Error(), "task resume: not paused") {
		t.Fatalf("resume err = %v", err)
	}
}

func TestTaskListSaysWhyATaskIsPaused(t *testing.T) {
	out := renderTaskList([]ScheduledTask{
		{ID: "a", Kind: "reminder", ScheduleKind: "cron", Status: "paused", PausedReason: "operator"},
		{ID: "b", Kind: "agent_job", ScheduleKind: "every", Status: "paused", PausedReason: "failures", ConsecutiveFailures: 3},
	})
	if !strings.Contains(out, "a  kind=reminder  cron  next=— [paused]") {
		t.Fatalf("operator pause not flagged:\n%s", out)
	}
	if !strings.Contains(out, "b  kind=agent_job  every  next=— [paused after 3 failed runs]") {
		t.Fatalf("failure pause not flagged with its count:\n%s", out)
	}
}
