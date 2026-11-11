package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// messageTaskStore is the cron store as a parked message sees it: one row per originating
// call (migration 0155), and a cancel that records what it cancelled.
type messageTaskStore struct {
	fakeTaskStore
	byCall  map[string]ScheduledTask
	creates int
	inputs  []CreateTaskInput
}

func (s *messageTaskStore) CreateScheduledTask(_ context.Context, in CreateTaskInput) (ScheduledTask, error) {
	if row, ok := s.byCall[in.OriginToolCallID]; ok {
		return row, nil
	}
	s.creates++
	s.inputs = append(s.inputs, in)
	row := ScheduledTask{ID: fmt.Sprintf("task-%d", s.creates), Kind: in.Kind, Status: in.Status}
	if s.byCall == nil {
		s.byCall = map[string]ScheduledTask{}
	}
	s.byCall[in.OriginToolCallID] = row
	return row, nil
}

// fakeMessageDrafts records the drafts asked for; the same call gives the same draft back.
type fakeMessageDrafts struct {
	err    error
	inputs []ScheduledMessageDraft
	byCall map[string]string
}

func (f *fakeMessageDrafts) CreateScheduledMessageDraft(_ context.Context, in ScheduledMessageDraft) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.inputs = append(f.inputs, in)
	if id, ok := f.byCall[in.ToolCallID]; ok {
		return id, nil
	}
	if f.byCall == nil {
		f.byCall = map[string]string{}
	}
	id := fmt.Sprintf("draft-%d", len(f.byCall)+1)
	f.byCall[in.ToolCallID] = id
	return id, nil
}

func messageArgs(t *testing.T, at time.Time, payload, extra string) taskArgs {
	t.Helper()
	raw := fmt.Sprintf(`{"action":"schedule","kind":"message","schedule_kind":"at","at":%q,"payload":%s%s}`,
		at.UTC().Format(time.RFC3339), payload, extra)
	var a taskArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("args: %v", err)
	}
	return a
}

func messageTool() (*TaskTool, *messageTaskStore, *fakeMessageDrafts) {
	store := &messageTaskStore{}
	drafts := &fakeMessageDrafts{}
	return &TaskTool{Store: store, MessageDrafts: drafts, Destinations: &fakeDestinations{}}, store, drafts
}

const whatsappPayload = `{"channel":"whatsapp","recipient":"10000000000","text":"Ciao"}`

func jsonEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not JSON: %v (%s)", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %v", err)
	}
	gj, _ := json.Marshal(g)
	wj, _ := json.Marshal(w)
	if string(gj) != string(wj) {
		t.Fatalf("args = %s, want %s", gj, wj)
	}
}

// Decision 29: the kind is offered in the same commit as its handler, and the review parks it.
func TestAMessageIsOfferedToTheModel(t *testing.T) {
	t.Parallel()
	tool, store, drafts := messageTool()
	spec := tool.Spec()
	if !strings.Contains(string(spec.Parameters), `"message"]`) || !strings.Contains(spec.Description, "kind=message sends one fixed message") {
		t.Fatal("the schema or the description does not offer message")
	}
	at := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	raw := json.RawMessage(`{"action":"schedule","kind":"message","schedule_kind":"at","at":"` + at + `","payload":` + whatsappPayload + `,"notify":"stdout"}`)
	pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", raw)
	if !ok || pause == nil || store.creates != 1 || len(drafts.inputs) != 1 {
		t.Fatalf("HostReview = %+v, %v; creates %d, drafts %d", pause, ok, store.creates, len(drafts.inputs))
	}
	// Outside a review, Execute still refuses: a message is only ever parked.
	if _, err := tool.Execute(context.Background(), raw); !errors.Is(err, errMessageNeedsReview) {
		t.Fatalf("Execute = %v, want errMessageNeedsReview", err)
	}
}

func TestAMessageIsScheduledPendingWithItsDraftAndAReviewPause(t *testing.T) {
	t.Parallel()
	tool, store, drafts := messageTool()
	at := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, at, whatsappPayload, `,"notify":"stdout"`))
	if !ok || pause == nil {
		t.Fatal("the message was not parked")
	}
	if pause.ToolCallID != "call-1" || pause.Kind != KindApproval {
		t.Fatalf("pause = %+v", pause)
	}
	var resume map[string]string
	if err := json.Unmarshal(pause.ResumeContext, &resume); err != nil {
		t.Fatalf("resume context: %v", err)
	}
	if resume["type"] != "message_draft" || resume["draft_id"] != "draft-1" || resume["task_id"] != "task-1" {
		t.Fatalf("resume context = %v", resume)
	}
	created := store.inputs[0]
	if created.Kind != "message" || created.Status != "pending_approval" || created.ScheduleKind != "at" ||
		created.OriginConversationID != "conv-1" || created.OriginToolCallID != "call-1" {
		t.Fatalf("created = %+v", created)
	}
	draft := drafts.inputs[0]
	if draft.Channel != "whatsapp" || !draft.SendAt.Equal(at) || draft.TaskID != "task-1" {
		t.Fatalf("draft = %+v", draft)
	}
	jsonEqual(t, draft.Args, `{"recipient":"10000000000","message":"Ciao"}`)
}

func TestAnEmailMessageMapsToTheCalendarSend(t *testing.T) {
	t.Parallel()
	tool, _, drafts := messageTool()
	payload := `{"channel":"email","account":"acc-1","to":["someone@example.com"],"subject":"Hi","text":"Text"}`
	if _, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, time.Now().Add(time.Hour), payload, `,"notify":"none"`)); !ok {
		t.Fatal("the email was not parked")
	}
	jsonEqual(t, drafts.inputs[0].Args,
		`{"action":"send_email","accountId":"acc-1","to":["someone@example.com"],"subject":"Hi","body":"Text","bodyFormat":"text"}`)
}

func TestAMessageRefusesRecurrenceTheFarFutureAndThePast(t *testing.T) {
	t.Parallel()
	every := messageArgs(t, time.Now().Add(time.Hour), whatsappPayload, `,"notify":"none"`)
	every.ScheduleKind, every.EveryMinutes = "every", 60
	cases := map[string]taskArgs{
		"recurring":   every,
		"24h ahead":   messageArgs(t, time.Now().Add(24*time.Hour), whatsappPayload, `,"notify":"none"`),
		"in the past": messageArgs(t, time.Now().Add(-time.Minute), whatsappPayload, `,"notify":"none"`),
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			tool, store, drafts := messageTool()
			if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", a); ok || pause != nil {
				t.Fatalf("parked: %+v", pause)
			}
			if store.creates != 0 || len(drafts.inputs) != 0 {
				t.Fatalf("created %d tasks, %d drafts", store.creates, len(drafts.inputs))
			}
		})
	}
	for name, want := range map[string]string{
		"recurring":   "kind=message is one-shot only",
		"24h ahead":   "at most 23h45m ahead",
		"in the past": "needs a time in the future",
	} {
		tool, _, _ := messageTool()
		if _, _, err := tool.messageSchedule(cases[name], time.Now()); err == nil || !strings.Contains(err.Error(), want) ||
			!strings.HasSuffix(err.Error(), "Nothing was scheduled") {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
	}
}

func TestAFailedDraftCancelsTheTask(t *testing.T) {
	t.Parallel()
	tool, store, drafts := messageTool()
	drafts.err = errors.New("WhatsApp is not connected")
	if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, time.Now().Add(time.Hour), whatsappPayload, `,"notify":"none"`)); ok || pause != nil {
		t.Fatalf("parked without a draft: %+v", pause)
	}
	if store.cancelled != "task-1" {
		t.Fatalf("cancelled %q, want task-1", store.cancelled)
	}
}

func TestAMessageWithNoChannelReturnsTheChoiceFirst(t *testing.T) {
	t.Parallel()
	tool, store, _ := messageTool()
	if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", messageArgs(t, time.Now().Add(time.Hour), whatsappPayload, "")); ok || pause != nil {
		t.Fatalf("parked with no delivery route: %+v", pause)
	}
	if store.creates != 0 {
		t.Fatal("a task was created before the choice")
	}
}

// Decision 30: a retried turn parks the same call again and finds the same rows.
func TestTheSameCallParkedTwiceCreatesOneTask(t *testing.T) {
	t.Parallel()
	tool, store, _ := messageTool()
	a := messageArgs(t, time.Now().Add(time.Hour).Truncate(time.Second), whatsappPayload, `,"notify":"none"`)
	first, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", a)
	if !ok {
		t.Fatal("first park failed")
	}
	second, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", a)
	if !ok {
		t.Fatal("second park failed")
	}
	if store.creates != 1 || string(first.ResumeContext) != string(second.ResumeContext) {
		t.Fatalf("creates = %d, resume %s vs %s", store.creates, first.ResumeContext, second.ResumeContext)
	}
}

// Decision 32: one validation prefix for both paths, so a message with no text is never parked.
func TestAMessageWithoutTextIsNeverParked(t *testing.T) {
	t.Parallel()
	tool, store, _ := messageTool()
	a := messageArgs(t, time.Now().Add(time.Hour), `{"channel":"whatsapp","recipient":"10000000000","text":" "}`, `,"notify":"none"`)
	if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", a); ok || pause != nil {
		t.Fatalf("parked with no text: %+v", pause)
	}
	if err := validateSchedule(a); err == nil || !strings.Contains(err.Error(), "payload.text") {
		t.Fatalf("validateSchedule = %v, want the payload.text refusal", err)
	}
	if store.creates != 0 {
		t.Fatal("a task was created for a message with no text")
	}
}
