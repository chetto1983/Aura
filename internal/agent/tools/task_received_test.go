package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cron"
)

func TestScheduleRefusalNamesWhatArrived(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args string
		want []string
	}{
		{"the call the VM received", `{"action":"schedule","schedule_kind":"at","task_id":"x","telegram_recipients":[]}`,
			[]string{"kind is required", "action, schedule_kind, task_id, telegram_recipients", "payload: (none)", "No task was created"}},
		{"payload dropped", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","tz":"Europe/Rome"}`,
			[]string{"payload.text", "action, at, kind, schedule_kind, tz", "payload: (none)", "same call"}},
		{"payload without text", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":{"goal":"x"}}`,
			[]string{"payload.text", "action, at, kind, payload, schedule_kind", "payload: goal"}},
		{"payload null", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":null}`,
			[]string{"payload.text", "payload: (not an object)"}},
		{"payload array", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":["x"]}`,
			[]string{"payload.text", "payload: (not an object)"}},
		{"payload number", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":7}`,
			[]string{"payload.text", "payload: (not an object)"}},
		{"payload double-encoded", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":"{\"text\":\"x\"}"}`,
			[]string{"payload.text", "action, at, kind, payload, schedule_kind", "payload: (not an object)"}},
		{"payload empty object", `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","payload":{}}`,
			[]string{"payload.text", "payload: (empty object)"}},
		{"schedule_kind missing", `{"action":"schedule","kind":"reminder","payload":{"text":"x"}}`,
			[]string{"schedule kind", "action, kind, payload", "payload: text", "No task was created"}},
		{"cron missing", `{"action":"schedule","kind":"reminder","schedule_kind":"cron","tz":"Europe/Rome","payload":{"text":"x"}}`,
			[]string{"cron", "action, kind, payload, schedule_kind, tz", "No task was created"}},
		{"every_minutes missing", `{"action":"schedule","kind":"reminder","schedule_kind":"every","tz":"Europe/Rome","payload":{"text":"x"}}`,
			[]string{"every", "action, kind, payload, schedule_kind, tz", "No task was created"}},
		{"message with empty at", `{"action":"schedule","kind":"message","schedule_kind":"at","payload":{"text":"x"}}`,
			[]string{"`at`", "RFC-3339", "action, kind, payload, schedule_kind", "No task was created"}},
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

func TestScheduleRefusalNeverEchoesValues(t *testing.T) {
	t.Parallel()
	args := `{"action":"schedule","kind":"reminder","schedule_kind":"at","at":"2030-01-01T09:30:00Z","tz":"VALUE-TZ","payload":{"goal":"VALUE-GOAL","nested":{"a":"VALUE-NESTED"}}}`
	_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(args))
	if err == nil {
		t.Fatal("accepted")
	}
	if strings.Contains(err.Error(), "VALUE-") {
		t.Fatalf("a value reached the refusal: %q", err)
	}
	if !strings.Contains(err.Error(), "payload: goal, nested") {
		t.Fatalf("payload keys missing: %q", err)
	}
}

func TestScheduleRefusalCapsAndSanitisesKeys(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`{"action":"schedule","kind":"reminder","bad\nkey\u0007":1,"aaa_` + strings.Repeat("x", 500) + `":1`)
	for i := range 100 {
		fmt.Fprintf(&b, `,"k%03d":1`, i)
	}
	b.WriteString(`}`)
	_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(b.String()))
	if err == nil {
		t.Fatal("accepted")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\n\a\r") {
		t.Fatalf("control characters reached the refusal: %q", msg)
	}
	if !strings.Contains(msg, "and 84 more") || strings.Contains(msg, strings.Repeat("x", 100)) || len(msg) > 1500 {
		t.Fatalf("keys not capped (len %d): %q", len(msg), msg)
	}
}

func TestScheduleRefusalForAWrongValueDoesNotClaimItIsMissing(t *testing.T) {
	t.Parallel()
	base := `"action":"schedule","kind":"reminder","payload":{"text":"x"}`
	cases := []struct{ name, args, field string }{
		{"every below the minimum", `{` + base + `,"schedule_kind":"every","every_minutes":3}`, "`every_minutes`"},
		{"invalid cron", `{` + base + `,"schedule_kind":"cron","cron":"not a cron"}`, "`cron`"},
		{"past at", `{` + base + `,"schedule_kind":"at","at":"2020-01-01T00:00:00Z"}`, "`at`"},
		{"malformed at", `{` + base + `,"schedule_kind":"at","at":"tomorrow"}`, "`at`"},
		{"invalid tz", `{` + base + `,"schedule_kind":"every","every_minutes":10,"tz":"Mars/Base"}`, "`tz`"},
		{"unknown schedule_kind", `{` + base + `,"schedule_kind":"weekly"}`, "`schedule_kind`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(c.args))
			if err == nil {
				t.Fatal("accepted")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.field) || !strings.Contains(msg, "fix the value in this same call") || !strings.Contains(msg, "No task was created") {
				t.Errorf("refusal does not name %s and ask for a fixed value: %q", c.field, msg)
			}
			if strings.Contains(msg, "ask the operator") || strings.Contains(msg, "Received:") {
				t.Errorf("refusal claims the field is missing: %q", msg)
			}
		})
	}
}

// The absent twins of the wrong-value cases: a field that never arrived keeps the missing-field
// refusal and the list of what did arrive, and is not called wrong.
func TestScheduleRefusalForAnAbsentFieldSaysItIsMissing(t *testing.T) {
	t.Parallel()
	base := `"action":"schedule","kind":"reminder","payload":{"text":"x"}`
	cases := []struct{ name, args, reason, received string }{
		{"every_minutes absent", `{` + base + `,"schedule_kind":"every"}`, "every interval below", "action, kind, payload, schedule_kind;"},
		{"cron absent", `{` + base + `,"schedule_kind":"cron"}`, `invalid cron expression: ""`, "action, kind, payload, schedule_kind;"},
		{"schedule_kind absent", `{` + base + `}`, `invalid schedule kind: ""`, "action, kind, payload;"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(c.args))
			if err == nil {
				t.Fatal("accepted")
			}
			msg := err.Error()
			for _, want := range []string{c.reason, "Received: " + c.received, "Put it in this same call", "No task was created"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal lacks %q: %q", want, msg)
				}
			}
			if strings.Contains(msg, "wrong, not missing") {
				t.Errorf("refusal calls an absent field wrong: %q", msg)
			}
		})
	}
}

// U+2028 and U+2029 end a line in JavaScript and in some renderers, so a key carrying one could
// start a forged line in the refusal like a newline would.
func TestScheduleRefusalStripsLineAndParagraphSeparators(t *testing.T) {
	t.Parallel()
	args := `{"action":"schedule","kind":"reminder","a\u2028b":1,"c\u2029d":1}`
	_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(args))
	if err == nil {
		t.Fatal("accepted")
	}
	if msg := err.Error(); strings.ContainsAny(msg, "\u2028\u2029") || !strings.Contains(msg, "action, a?b, c?d, kind") {
		t.Fatalf("separators reached the refusal: %q", msg)
	}
}

// The engine's reason quotes the wrong value itself, which the model chose: a long one is cut, and
// the instruction after it survives.
func TestScheduleRefusalBoundsTheEchoedValue(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("v", 5000)
	base := `"action":"schedule","kind":"reminder","payload":{"text":"x"}`
	for _, args := range []string{
		`{` + base + `,"schedule_kind":"cron","cron":"` + long + `"}`,
		`{` + base + `,"schedule_kind":"every","every_minutes":10,"tz":"` + long + `"}`,
		`{` + base + `,"schedule_kind":"at","at":"` + long + `"}`,
		`{` + base + `,"schedule_kind":"` + long + `"}`,
	} {
		_, err := (&TaskTool{Store: &fakeTaskStore{}}).actionSchedule(context.Background(), []byte(args))
		if err == nil {
			t.Fatal("accepted")
		}
		if msg := err.Error(); len(msg) > 600 || !strings.Contains(msg, "...") || !strings.Contains(msg, "No task was created") {
			t.Errorf("echoed value not bounded (len %d): %.120q", len(msg), msg)
		}
	}
	// A message reaches the engine past refuseResolve, so the bound has to be the engine's own.
	message := taskArgs{
		Kind: "message", ScheduleKind: "at", At: time.Now().Add(time.Hour).Format(time.RFC3339), TZ: long,
		Payload: json.RawMessage(`{"channel":"whatsapp","recipient":"1","text":"x"}`),
	}
	_, _, err := (&TaskTool{}).messageSchedule(message, time.Now())
	if err == nil || len(err.Error()) > 600 || !errors.Is(err, cron.ErrInvalidTimezone) {
		t.Fatalf("message tz refusal = %v (len %d), want a bounded ErrInvalidTimezone", err, len(fmt.Sprint(err)))
	}
}
