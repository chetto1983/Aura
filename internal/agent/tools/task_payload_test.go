package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A scheduled run starts in a fresh session and cannot ask anyone anything, so a task whose
// payload carries nothing to deliver or to do must be refused while the operator is still there.
// Measured 2026-11-05: an agent_job with payload={} was accepted, failed three runs in a row and
// sat paused for a month.
func TestTaskScheduleRefusesAnEmptyPayload(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"reminder without payload", `{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"reminder","notify":"stdout"}`, "payload.text"},
		{"reminder with blank text", `{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"reminder","payload":{"text":"  "},"notify":"stdout"}`, "payload.text"},
		{"agent_job with empty payload", `{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"agent_job","payload":{},"notify":"stdout"}`, "payload.goal"},
		{"agent_job with non-string goal", `{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"agent_job","payload":{"goal":42},"notify":"stdout"}`, "payload.goal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeTaskStore{}
			tool := &TaskTool{Store: store}
			_, err := tool.Execute(context.Background(), json.RawMessage(tc.args))
			if err == nil {
				t.Fatal("a task with nothing to deliver or do must be refused")
			}
			for _, want := range []string{tc.want, "No task was created", "ask the operator"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal must contain %q, got %q", want, err.Error())
				}
			}
			if store.created.Kind != "" {
				t.Error("nothing must be persisted for a refused payload")
			}
		})
	}
}

func TestTaskScheduleAcceptsPayloadsThatSayWhatToDo(t *testing.T) {
	for _, args := range []string{
		`{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"reminder","payload":{"text":"drink water"},"notify":"stdout"}`,
		`{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"agent_job","payload":{"goal":"summarise the inbox"},"notify":"stdout"}`,
		`{"action":"schedule","schedule_kind":"every","every_minutes":60,"kind":"backup_postgres","notify":"none"}`,
	} {
		store := &fakeTaskStore{}
		tool := &TaskTool{Store: store}
		if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err != nil {
			t.Fatalf("%s: unexpected refusal: %v", args, err)
		}
		if store.created.Kind == "" {
			t.Fatalf("%s: the task must be persisted", args)
		}
	}
}

func TestTaskSpecTellsTheModelToAskBeforeScheduling(t *testing.T) {
	desc := (&TaskTool{}).Spec().Description
	for _, want := range []string{
		"cannot ask anyone anything",
		"ask the operator one question",
		"Never invent a time or a recipient",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("task description must contain %q", want)
		}
	}
}
