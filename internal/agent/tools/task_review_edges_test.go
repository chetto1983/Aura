package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The review refuses before writing anything whenever a step it depends on is missing or fails.
func TestAMessageReviewWritesNothingWhenAStepFails(t *testing.T) {
	t.Parallel()
	at := time.Now().Add(time.Hour)
	cases := map[string]func(*TaskTool, *messageTaskStore, *fakeMessageDrafts){
		"no drafts seam": func(tool *TaskTool, _ *messageTaskStore, _ *fakeMessageDrafts) { tool.MessageDrafts = nil },
		"no store":       func(tool *TaskTool, _ *messageTaskStore, _ *fakeMessageDrafts) { tool.Store = nil },
		"the route cannot deliver": func(tool *TaskTool, _ *messageTaskStore, _ *fakeMessageDrafts) {
			tool.Destinations = &fakeDestinations{err: errors.New("no WhatsApp link")}
		},
		"the task is not written": func(_ *TaskTool, store *messageTaskStore, _ *fakeMessageDrafts) {
			store.createErr = errors.New("db down")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			tool, store, drafts := messageTool()
			setup(tool, store, drafts)
			if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, at, whatsappPayload, `,"notify":"whatsapp"`)); ok || pause != nil {
				t.Fatalf("parked: %+v", pause)
			}
			if len(drafts.inputs) != 0 {
				t.Fatalf("drafts written: %d", len(drafts.inputs))
			}
		})
	}
}

func TestAFailedDraftWhoseTaskCannotBeCancelledStillParksNothing(t *testing.T) {
	t.Parallel()
	tool, store, drafts := messageTool()
	drafts.err = errors.New("WhatsApp is not connected")
	store.cancelErr = errors.New("db down")
	if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, time.Now().Add(time.Hour), whatsappPayload, `,"notify":"none"`)); ok || pause != nil {
		t.Fatalf("parked: %+v", pause)
	}
}

func TestOnlyAScheduleIsReviewed(t *testing.T) {
	t.Parallel()
	tool, store, _ := messageTool()
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw := json.RawMessage(`{"action":"list","kind":"message","schedule_kind":"at","at":"` + at + `","payload":` + whatsappPayload + `,"notify":"none"}`)
	if pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", raw); ok || pause != nil || store.creates != 0 {
		t.Fatalf("a list call was parked: %+v", pause)
	}
	if pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", json.RawMessage(`not json`)); ok || pause != nil {
		t.Fatal("unparsable arguments were parked")
	}
}

func TestTheReviewPausesRankAboveAnOrdinaryQuestion(t *testing.T) {
	t.Parallel()
	tool, _, _ := messageTool()
	pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, time.Now().Add(time.Hour), whatsappPayload, `,"notify":"none"`))
	if !ok || pause.Priority != 90 || pause.Question != "Review this scheduled message before it is sent" {
		t.Fatalf("pause = %+v", pause)
	}
	approval, _ := approvalTool()
	pause, ok = approval.HostReview(context.Background(), "conv-1", "call-1", json.RawMessage(scheduleArgs("agent_job", inTwoHours(), `{"goal":"g"}`)))
	if !ok || pause.Priority != 90 {
		t.Fatalf("approval pause = %+v", pause)
	}
	if ScheduledMessageGrace != 15*time.Minute {
		t.Fatalf("grace = %v", ScheduledMessageGrace)
	}
}

// The horizon is 24h less the grace, to the minute.
func TestTheMessageHorizonIs23h45m(t *testing.T) {
	t.Parallel()
	tool, _, _ := messageTool()
	now := time.Now()
	for offset, ok := range map[time.Duration]bool{
		23*time.Hour + 44*time.Minute + 30*time.Second: true,
		23*time.Hour + 45*time.Minute + 30*time.Second: false,
	} {
		_, _, err := tool.messageSchedule(messageArgs(t, now.Add(offset), whatsappPayload, ""), now)
		if (err == nil) != ok {
			t.Errorf("at now+%v: err = %v, want accepted %v", offset, err, ok)
		}
	}
}

func TestAMessageMustNameItsChannelRecipientAndText(t *testing.T) {
	t.Parallel()
	tool, _, _ := messageTool()
	at := time.Now().Add(time.Hour)
	for name, payload := range map[string]string{
		"a blank WhatsApp recipient": `{"channel":"whatsapp","recipient":"  ","text":"Ciao"}`,
		"an email with no one":       `{"channel":"email","to":[],"subject":"s","text":"Ciao"}`,
		"an email given a recipient": `{"channel":"email","recipient":"10000000000","text":"Ciao"}`,
		"WhatsApp given a list":      `{"channel":"whatsapp","to":["someone@example.com"],"text":"Ciao"}`,
		"another channel":            `{"channel":"sms","recipient":"10000000000","text":"Ciao"}`,
		"a blank text":               `{"channel":"whatsapp","recipient":"10000000000","text":"  "}`,
		"not an object":              `"Ciao"`,
	} {
		if _, _, err := tool.messageSchedule(messageArgs(t, at, payload, ""), time.Now()); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	bad := messageArgs(t, at, whatsappPayload, "")
	bad.At = "tomorrow"
	if _, _, err := tool.messageSchedule(bad, time.Now()); err == nil || !strings.Contains(err.Error(), "a time in the future") {
		t.Fatalf("an unparsable time: %v", err)
	}
	if _, err := (messagePayload{Channel: "sms"}).recipeArgs(); err == nil {
		t.Fatal("an unknown channel mapped to recipe arguments")
	}
	if err := validateSchedule(taskArgs{}); err == nil || !strings.Contains(err.Error(), "kind is required") {
		t.Fatalf("validateSchedule with no kind = %v", err)
	}
}

// Execute hands back the delivery directive for a message before anything else.
func TestAMessageOutsideAReviewReturnsTheChoiceFirst(t *testing.T) {
	t.Parallel()
	tool, store, _ := messageTool()
	raw := json.RawMessage(`{"action":"schedule","kind":"message","schedule_kind":"at","at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `","payload":` + whatsappPayload + `}`)
	res, err := tool.Execute(context.Background(), raw)
	if err != nil || !strings.Contains(res.Preview, "delivery_choice_required") || store.creates != 0 {
		t.Fatalf("execute = %q, %v", res.Preview, err)
	}
}

func TestAnApprovalReviewWritesNothingWhenAStepFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args  string
		setup func(*TaskTool, *messageTaskStore)
	}{
		"no store":                {scheduleArgs("agent_job", inTwoHours(), `{"goal":"g"}`), func(tool *TaskTool, _ *messageTaskStore) { tool.Store = nil }},
		"the task is not written": {scheduleArgs("agent_job", inTwoHours(), `{"goal":"g"}`), func(_ *TaskTool, s *messageTaskStore) { s.createErr = errors.New("db down") }},
		"the route cannot deliver": {strings.Replace(scheduleArgs("agent_job", inTwoHours(), `{"goal":"g"}`), `"stdout"`, `"whatsapp"`, 1), func(tool *TaskTool, _ *messageTaskStore) {
			tool.Destinations = &fakeDestinations{err: errors.New("no link")}
		}},
		"the schedule is invalid": {scheduleArgs("agent_job", `"schedule_kind":"cron","cron":"not a cron"`, `{"goal":"g"}`), func(*TaskTool, *messageTaskStore) {}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tool, store := approvalTool()
			c.setup(tool, store)
			if pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", json.RawMessage(c.args)); ok || pause != nil {
				t.Fatalf("parked: %+v", pause)
			}
		})
	}
}

// A schedule with no tz runs in the operator's zone, and the review stores it so.
func TestTheReviewStoresTheOperatorsZone(t *testing.T) {
	t.Parallel()
	tool, store := approvalTool()
	tool.DeploymentZone = "Asia/Tokyo"
	raw := json.RawMessage(scheduleArgs("agent_job", `"schedule_kind":"cron","cron":"30 7 * * *"`, `{"goal":"g"}`))
	if _, ok := tool.HostReview(context.Background(), "conv-1", "call-1", raw); !ok || store.inputs[0].TZ != "Asia/Tokyo" {
		t.Fatalf("parked %v, inputs %+v", ok, store.inputs)
	}
}

func TestTheSummaryCutsOnlyPastItsLimit(t *testing.T) {
	t.Parallel()
	exact := strings.Repeat("a", taskSummaryMaxRunes)
	if got := taskSummary("agent_job", json.RawMessage(`{"goal":"`+exact+`"}`)); got != exact {
		t.Fatalf("a %d-rune goal was cut: %q", taskSummaryMaxRunes, got)
	}
	over := taskSummary("agent_job", json.RawMessage(`{"goal":"`+exact+`b"}`))
	if utf8.RuneCountInString(over) != taskSummaryMaxRunes+1 || !strings.HasSuffix(over, "…") || taskSummaryMaxRunes != 160 {
		t.Fatalf("over the limit: %q (%d runes)", over, utf8.RuneCountInString(over))
	}
}
