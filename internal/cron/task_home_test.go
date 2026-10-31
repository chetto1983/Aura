package cron

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
)

// fakeTaskConversations answers whether a conversation still exists and opens new ones.
type fakeTaskConversations struct {
	exists    map[string]bool
	existsErr error
	createID  string
	createErr error
	checked   []string
	created   []createdConversation
}

type createdConversation struct {
	identityID string
	title      string
}

func (f *fakeTaskConversations) ConversationExists(_ context.Context, conversationID, _ string) (bool, error) {
	f.checked = append(f.checked, conversationID)
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.exists[conversationID], nil
}

func (f *fakeTaskConversations) CreateTaskConversation(_ context.Context, identityID, title string) (string, error) {
	f.created = append(f.created, createdConversation{identityID: identityID, title: title})
	return f.createID, f.createErr
}

// fakeTaskOrigins records the conversation a task was moved into.
type fakeTaskOrigins struct {
	set map[string]string
	err error
}

func (f *fakeTaskOrigins) SetTaskOrigin(_ context.Context, taskID, conversationID string) error {
	if f.err != nil {
		return f.err
	}
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[taskID] = conversationID
	return nil
}

func homeDispatch(handler *fakeHandler, convs *fakeTaskConversations, origins *fakeTaskOrigins, recorder *fakeConversationRecorder) *Dispatch {
	return NewDispatch(map[TaskKind]Handler{KindAgentJob: handler, KindBackupPostgres: handler}, DispatchDeps{
		Store:                &fakeCompleter{},
		ConversationRecorder: recorder,
		TaskConversations:    convs,
		TaskOrigins:          origins,
	})
}

const dailyGoal = `{"goal": "Cerca le notizie principali del giorno in italiano, riassumile in modo conciso e inviale all'utente su Telegram"}`

// TestATaskWhoseConversationWasDeletedAnswersInOneOfItsOwn pins the defect measured on the
// lab VM on 2026-10-14: the operator bulk-deleted their conversations, the daily goal's
// origin among them, and its next run lost every tool-ledger row to the conversation foreign
// key and its outcome to "conversation not found". A task outlives the conversation it was
// scheduled from: it moves into one of its own, keeps it, and answers there.
func TestATaskWhoseConversationWasDeletedAnswersInOneOfItsOwn(t *testing.T) {
	t.Parallel()

	handler := &fakeHandler{summary: "le notizie di oggi"}
	convs := &fakeTaskConversations{exists: map[string]bool{}, createID: "conv-own"}
	origins := &fakeTaskOrigins{}
	recorder := &fakeConversationRecorder{}
	d := homeDispatch(handler, convs, origins, recorder)

	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "telegram", IdentityID: "id-1",
		OriginConversationID: "conv-deleted", Payload: []byte(dailyGoal)}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(convs.created) != 1 || convs.created[0].identityID != "id-1" {
		t.Fatalf("created %+v, want one conversation owned by the task's identity", convs.created)
	}
	if got := origins.set["task-1"]; got != "conv-own" {
		t.Fatalf("the task now answers in %q, want it to keep conv-own for its next runs", got)
	}
	if got := handler.gotJob.OriginConversationID; got != "conv-own" {
		t.Fatalf("the run keyed its tool ledger on %q, want conv-own", got)
	}
	if len(recorder.appended) != 1 || recorder.appended[0].conversationID != "conv-own" {
		t.Fatalf("the outcome went to %+v, want conv-own", recorder.appended)
	}
}

// TestATaskWhoseConversationExistsStaysThere keeps the 2026-08-26 rule: the outcome goes
// where the operator asked for it, as long as that conversation is there.
func TestATaskWhoseConversationExistsStaysThere(t *testing.T) {
	t.Parallel()

	handler := &fakeHandler{summary: "done"}
	convs := &fakeTaskConversations{exists: map[string]bool{"conv-1": true}}
	origins := &fakeTaskOrigins{}
	d := homeDispatch(handler, convs, origins, &fakeConversationRecorder{})

	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "stdout", IdentityID: "id-1",
		OriginConversationID: "conv-1", Payload: []byte(dailyGoal)}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(convs.created) != 0 || len(origins.set) != 0 {
		t.Fatalf("a task whose conversation exists was moved: created %+v, set %+v", convs.created, origins.set)
	}
	if got := handler.gotJob.OriginConversationID; got != "conv-1" {
		t.Fatalf("the run answered in %q, want conv-1", got)
	}
}

// TestAnAgentJobScheduledOutsideAConversationAnswersInOneOfItsOwn pins the defect measured on
// 2026-10-31 (docs/superpowers/verification/2026-10-31-briefing-model-in-the-loop.md): an
// agent_job from `aura task add` had no conversation, so its ledger key fell back to the
// non-UUID session and a strict profile denied every tool it called. It opens one of its own,
// keeps it for its next runs, and answers there, as a task whose conversation was deleted does.
func TestAnAgentJobScheduledOutsideAConversationAnswersInOneOfItsOwn(t *testing.T) {
	t.Parallel()

	handler := &fakeHandler{summary: "il briefing"}
	convs := &fakeTaskConversations{createID: "conv-own"}
	origins := &fakeTaskOrigins{}
	recorder := &fakeConversationRecorder{}
	d := homeDispatch(handler, convs, origins, recorder)

	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "stdout", IdentityID: "id-1", Payload: []byte(dailyGoal)}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(convs.checked) != 0 {
		t.Errorf("checked %v for a task that names no conversation", convs.checked)
	}
	if len(convs.created) != 1 || convs.created[0].identityID != "id-1" || !strings.HasPrefix(convs.created[0].title, "⏰ Cerca") {
		t.Fatalf("created %+v, want one conversation owned by id-1 and named after the goal", convs.created)
	}
	if got := origins.set["task-1"]; got != "conv-own" {
		t.Fatalf("the task keeps %q, want conv-own for its next runs", got)
	}
	if got := handler.gotJob.OriginConversationID; got != "conv-own" {
		t.Fatalf("the run keyed its tool ledger on %q, want conv-own", got)
	}
	if len(recorder.appended) != 1 || recorder.appended[0].conversationID != "conv-own" {
		t.Fatalf("the outcome went to %+v, want conv-own", recorder.appended)
	}
}

// TestASystemTaskIsNotGivenAConversation leaves the other kinds alone: none of them calls
// tools through the gateway, so a conversation would be one nobody asked for.
func TestASystemTaskIsNotGivenAConversation(t *testing.T) {
	t.Parallel()

	convs := &fakeTaskConversations{createID: "conv-own"}
	origins := &fakeTaskOrigins{}
	d := homeDispatch(&fakeHandler{}, convs, origins, &fakeConversationRecorder{})

	task := Task{ID: "task-1", Kind: KindBackupPostgres, NotifyRoute: "stdout", IdentityID: "id-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(convs.checked) != 0 || len(convs.created) != 0 || len(origins.set) != 0 {
		t.Fatalf("a system task was checked %v, given %+v and moved %+v", convs.checked, convs.created, origins.set)
	}
}

// TestAnAgentJobWhoseConversationCannotOpenRunsAsBefore: a failed open is no reason to skip
// the run; it goes ahead with no conversation, and nothing is recorded on the task.
func TestAnAgentJobWhoseConversationCannotOpenRunsAsBefore(t *testing.T) {
	t.Parallel()

	handler := &fakeHandler{}
	convs := &fakeTaskConversations{createErr: errors.New("identity gone")}
	origins := &fakeTaskOrigins{}
	d := homeDispatch(handler, convs, origins, &fakeConversationRecorder{})

	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "stdout", IdentityID: "id-1", Payload: []byte(dailyGoal)}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !handler.ran || handler.gotJob.OriginConversationID != "" || len(origins.set) != 0 {
		t.Fatalf("a failed open: ran=%v in %q, recorded %+v; want a run with no conversation and nothing recorded",
			handler.ran, handler.gotJob.OriginConversationID, origins.set)
	}
}

// TestAnUnansweredExistenceCheckMovesNothing: a database that cannot say whether the
// conversation exists is no evidence that it was deleted.
func TestAnUnansweredExistenceCheckMovesNothing(t *testing.T) {
	t.Parallel()

	handler := &fakeHandler{}
	convs := &fakeTaskConversations{existsErr: errors.New("pool exhausted")}
	origins := &fakeTaskOrigins{}
	d := homeDispatch(handler, convs, origins, &fakeConversationRecorder{})

	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "stdout", IdentityID: "id-1",
		OriginConversationID: "conv-1", Payload: []byte(dailyGoal)}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(convs.created) != 0 || len(origins.set) != 0 || handler.gotJob.OriginConversationID != "conv-1" {
		t.Fatalf("an unanswered check moved the task: created %+v, set %+v, ran in %q",
			convs.created, origins.set, handler.gotJob.OriginConversationID)
	}
}

// TestATaskConversationIsNamedAfterWhatTheTaskDoes: the sidebar is where the operator finds
// it, so the title is the task's own goal or text, short enough for one line.
func TestATaskConversationIsNamedAfterWhatTheTaskDoes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		task    Task
		want    string
		maxRune int
	}{
		{name: "goal", task: Task{Kind: KindAgentJob, Payload: []byte(dailyGoal)}, want: "⏰ Cerca le notizie principali del giorno", maxRune: taskTitleRunes},
		{name: "reminder text", task: Task{Kind: KindReminder, Payload: []byte(`{"text": "bevi acqua"}`)}, want: "⏰ bevi acqua"},
		{name: "no text", task: Task{Kind: KindReminder, Payload: []byte(`{}`)}, want: "⏰ reminder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := taskConversationTitle(tc.task)
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("title %q, want it to start with %q", got, tc.want)
			}
			if tc.maxRune > 0 && utf8.RuneCountInString(got) > tc.maxRune {
				t.Fatalf("title %q has %d runes, want at most %d", got, utf8.RuneCountInString(got), tc.maxRune)
			}
		})
	}
}

// TestATaskTitleIsCutOnlyPastOneLine pins the bound itself: a title of exactly sixty runes is
// kept whole, and one rune more is cut to sixty, the last of them the ellipsis.
func TestATaskTitleIsCutOnlyPastOneLine(t *testing.T) {
	t.Parallel()

	reminder := func(runes int) Task {
		return Task{Kind: KindReminder, Payload: []byte(`{"text": "` + strings.Repeat("a", runes-2) + `"}`)}
	}
	if got := taskConversationTitle(reminder(60)); got != "⏰ "+strings.Repeat("a", 58) {
		t.Errorf("a 60-rune title became %q, want it whole", got)
	}
	if got := taskConversationTitle(reminder(61)); got != "⏰ "+strings.Repeat("a", 57)+"…" {
		t.Errorf("a 61-rune title became %q, want 59 runes and an ellipsis", got)
	}
}

// TestATaskStaysWhereItIsWithoutBothDependencies: either seam missing means the dispatch can
// neither open nor record a conversation, so it moves nothing rather than half a move.
func TestATaskStaysWhereItIsWithoutBothDependencies(t *testing.T) {
	t.Parallel()

	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "stdout", IdentityID: "id-1", Payload: []byte(dailyGoal)}
	noConvs := NewDispatch(nil, DispatchDeps{Store: &fakeCompleter{}, TaskOrigins: &fakeTaskOrigins{}})
	if got := noConvs.homeTask(context.Background(), task); got.OriginConversationID != "" {
		t.Errorf("with no TaskConversations the task moved to %q", got.OriginConversationID)
	}
	convs := &fakeTaskConversations{createID: "conv-own"}
	noOrigins := NewDispatch(nil, DispatchDeps{Store: &fakeCompleter{}, TaskConversations: convs})
	if got := noOrigins.homeTask(context.Background(), task); got.OriginConversationID != "" || len(convs.created) != 0 {
		t.Errorf("with no TaskOrigins the task moved to %q and opened %+v", got.OriginConversationID, convs.created)
	}
}

// TestEveryHomingOutcomeIsLogged: the log is the operator's only sign that a task moved, or
// could not, so each outcome says which it was and names the task. No t.Parallel: the
// capture swaps the process-wide default logger.
func TestEveryHomingOutcomeIsLogged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		convs   *fakeTaskConversations
		origins *fakeTaskOrigins
		origin  string
		want    []string
	}{
		{name: "unanswered check", convs: &fakeTaskConversations{existsErr: errors.New("pool exhausted")}, origins: &fakeTaskOrigins{},
			origin: "conv-1", want: []string{"could not check the task's conversation", "conversation=conv-1", "pool exhausted"}},
		{name: "failed open", convs: &fakeTaskConversations{createErr: errors.New("identity gone")}, origins: &fakeTaskOrigins{},
			want: []string{"none could be opened", "identity gone"}},
		{name: "unrecorded move", convs: &fakeTaskConversations{createID: "conv-own"}, origins: &fakeTaskOrigins{err: errors.New("db down")},
			want: []string{"was not recorded", "conversation=conv-own", "db down"}},
		{name: "deleted origin", convs: &fakeTaskConversations{exists: map[string]bool{}, createID: "conv-own"}, origins: &fakeTaskOrigins{},
			origin: "conv-gone", want: []string{"conversation is gone", "previous=conv-gone", "conversation=conv-own"}},
		{name: "no origin", convs: &fakeTaskConversations{createID: "conv-own"}, origins: &fakeTaskOrigins{},
			want: []string{"scheduled outside a conversation", "conversation=conv-own"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			d := homeDispatch(&fakeHandler{}, tc.convs, tc.origins, &fakeConversationRecorder{})
			task := Task{ID: "task-7", Kind: KindAgentJob, IdentityID: "id-1", OriginConversationID: tc.origin, Payload: []byte(dailyGoal)}
			d.homeTask(context.Background(), task)
			for _, want := range append(tc.want, "task=task-7") {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("log %q lacks %q", logs.String(), want)
				}
			}
		})
	}
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}
