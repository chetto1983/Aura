package tools

import (
	"context"
	"strings"
	"testing"
)

func TestScheduleRefusalNamesWhatArrived(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args string
		want []string
	}{
		{"the call the VM received", `{"action":"schedule","schedule_kind":"at","task_id":"x","telegram_recipients":[]}`,
			[]string{"kind", "action, schedule_kind, task_id, telegram_recipients", "payload: (none)", "No task was created"}},
		{"payload dropped", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","tz":"Europe/Rome"}`,
			[]string{"payload.text", "action, at, kind, schedule_kind, tz", "payload: (none)", "same call"}},
		{"payload without text", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":{"goal":"x"}}`,
			[]string{"payload.text", "action, at, kind, payload, schedule_kind", "payload: goal"}},
		{"at missing", `{"action":"schedule","kind":"reminder","schedule_kind":"at","payload":{"text":"x"}}`,
			[]string{"`at`", "RFC-3339", "action, kind, payload, schedule_kind", "payload: text", "No task was created"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeTaskStore{}
			_, err := (&TaskTool{Store: store}).actionSchedule(context.Background(), []byte(c.args))
			if err == nil {
				t.Fatal("accepted")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			if strings.Contains(err.Error(), "ask the operator for it") {
				t.Errorf("error still sends the model to the operator: %q", err)
			}
		})
	}
}

func TestMalformedAtKeepsItsParseError(t *testing.T) {
	t.Parallel()
	args := `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"tomorrow","payload":{"text":"x"}}`
	_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(args))
	if err == nil || !strings.Contains(err.Error(), "at must be RFC-3339") {
		t.Fatalf("err = %v", err)
	}
}
