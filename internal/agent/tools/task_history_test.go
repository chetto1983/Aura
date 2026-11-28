package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func runHistory(t *testing.T, store *fakeTaskStore, args string) (string, error) {
	t.Helper()
	res, err := (&TaskTool{Store: store}).Execute(context.Background(), json.RawMessage(args))
	return res.Preview, err
}

// TestHistoryRendersNewestFirst: the run_now job of 2026-11-28 left its report on the ledger,
// and the turn that fired it had no way to read it there.
func TestHistoryRendersNewestFirst(t *testing.T) {
	t.Parallel()
	store := &fakeTaskStore{runs: []TaskRun{
		{StartedAt: time.Date(2026, 11, 28, 11, 11, 26, 0, time.UTC), Status: "completed", Summary: "Nessuna anomalia rilevata"},
		{StartedAt: time.Date(2026, 11, 28, 11, 7, 56, 0, time.UTC), Status: "failed", LastError: "model timeout"},
	}}
	got, err := runHistory(t, store, `{"action":"history","task_id":"01a221d9"}`)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	newer, older := strings.Index(got, "2026-11-28T11:11:26Z"), strings.Index(got, "2026-11-28T11:07:56Z")
	if newer < 0 || older < 0 || newer > older {
		t.Fatalf("history does not list both runs newest first:\n%s", got)
	}
	for _, want := range []string{"completed", "Nessuna anomalia rilevata", "failed", "model timeout"} {
		if !strings.Contains(got, want) {
			t.Errorf("history lacks %q:\n%s", want, got)
		}
	}
	if store.runsID != "01a221d9" {
		t.Errorf("history read task %q, want 01a221d9", store.runsID)
	}
}

func TestHistoryBoundsSummary(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("è", 3000)
	store := &fakeTaskStore{runs: []TaskRun{{StartedAt: time.Now(), Status: "completed", Summary: long}}}
	got, err := runHistory(t, store, `{"action":"history","task_id":"t1"}`)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if strings.Contains(got, strings.Repeat("è", 1500)) || !strings.Contains(got, strings.Repeat("è", 1499)+"…") {
		t.Fatalf("summary not bounded to 1500 runes, ellipsis included (len %d runes)", len([]rune(got)))
	}
}

func TestHistoryLimitDefaultsAndCaps(t *testing.T) {
	t.Parallel()
	for args, want := range map[string]int{
		`{"action":"history","task_id":"t1"}`:            5,
		`{"action":"history","task_id":"t1","limit":50}`: 20,
		`{"action":"history","task_id":"t1","limit":3}`:  3,
	} {
		store := &fakeTaskStore{}
		if _, err := runHistory(t, store, args); err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		if store.runsLimit != want {
			t.Errorf("%s: store limit %d, want %d", args, store.runsLimit, want)
		}
	}
}

func TestHistoryRequiresTaskID(t *testing.T) {
	t.Parallel()
	_, err := runHistory(t, &fakeTaskStore{}, `{"action":"history"}`)
	if err == nil || err.Error() != "task history: task_id is required" {
		t.Fatalf("err = %v, want task history: task_id is required", err)
	}
}

func TestHistoryNoRuns(t *testing.T) {
	t.Parallel()
	got, err := runHistory(t, &fakeTaskStore{}, `{"action":"history","task_id":"t1"}`)
	if err != nil || got != "no runs for task t1" {
		t.Fatalf("history = %q, %v; want no runs for task t1", got, err)
	}
}

// TestRunNowSaysWhereTheReportGoes: on 2026-11-28 the model slept 20+25+28 s and polled
// task list four times waiting for a report that had already been recorded.
func TestRunNowSaysWhereTheReportGoes(t *testing.T) {
	t.Parallel()
	got, err := runHistory(t, &fakeTaskStore{}, `{"action":"run_now","task_id":"r1"}`)
	want := "running task r1 now. Its outcome is recorded in this conversation, and an agent_job's report also reaches this turn after your next tool call; task action=history reads it. Do not sleep or poll to wait for it."
	if err != nil || got != want {
		t.Fatalf("run_now = %q, %v\nwant %q", got, err, want)
	}
}
