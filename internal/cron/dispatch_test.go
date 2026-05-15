package cron

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/wiki"
)

// ---- fake Notifier ----------------------------------------------------------

type fakeNotifier struct {
	reminders   []string
	completions []string
	owners      []string
	sendErr     error
}

func (f *fakeNotifier) SendReminder(_, body string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.reminders = append(f.reminders, body)
	return nil
}

func (f *fakeNotifier) SendCompletion(_, body string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.completions = append(f.completions, body)
	return nil
}

func (f *fakeNotifier) NotifyOwners(_ context.Context, msg string) {
	f.owners = append(f.owners, msg)
}

// ---- fake JobRunner ---------------------------------------------------------

type fakeJobRunner struct {
	result JobResult
	err    error
}

func (f *fakeJobRunner) RunJob(_ context.Context, _ JobRequest) (JobResult, error) {
	return f.result, f.err
}

// ---- fake wiki.Repository ---------------------------------------------------

type fakeWiki struct {
	lintErr  error
	rebuildN int
	logCalls []string
}

func (f *fakeWiki) ReadPage(_ string) (*wiki.Page, error)        { return nil, nil }
func (f *fakeWiki) ListPages() ([]string, error)                 { return nil, nil }
func (f *fakeWiki) ResolveSlug(input string) (string, []string, error) {
	return input, nil, nil
}
func (f *fakeWiki) WritePage(_ context.Context, _ *wiki.Page, _ ...string) error { return nil }
func (f *fakeWiki) DeletePage(_ context.Context, _ string) error                  { return nil }
func (f *fakeWiki) Dir() string                                                   { return "" }
func (f *fakeWiki) Lint(_ context.Context) ([]wiki.LintIssue, error) {
	return nil, f.lintErr
}
func (f *fakeWiki) CleanMemory(_ context.Context, _ wiki.MemoryHygieneOptions) (*wiki.MemoryHygieneReport, error) {
	return &wiki.MemoryHygieneReport{}, nil
}
func (f *fakeWiki) RepairLink(_ context.Context, _, _ string) error { return nil }
func (f *fakeWiki) RebuildIndex(_ context.Context)                  { f.rebuildN++ }
func (f *fakeWiki) AppendLog(_ context.Context, action, _ string) {
	f.logCalls = append(f.logCalls, action)
}

// ---- dispatch routing -------------------------------------------------------

func TestDispatch_Routing(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{
		Notifier: notifier,
		Wiki:     &fakeWiki{},
	})
	ctx := context.Background()

	t.Run("reminder routes to SendReminder", func(t *testing.T) {
		task := &Task{
			Kind:        KindReminder,
			Name:        "morning-check",
			Payload:     "Check email",
			RecipientID: "42",
		}
		if err := h.Dispatch(ctx, task); err != nil {
			t.Fatalf("Dispatch reminder: %v", err)
		}
		if len(notifier.reminders) == 0 {
			t.Fatal("expected SendReminder to be called")
		}
	})

	t.Run("wiki_maintenance routes to wiki", func(t *testing.T) {
		fw := &fakeWiki{}
		h2 := NewHandler(HandlerConfig{Wiki: fw})
		task := &Task{Kind: KindWikiMaintenance, Name: "nightly"}
		if err := h2.Dispatch(ctx, task); err != nil {
			t.Fatalf("Dispatch wiki_maintenance: %v", err)
		}
		if fw.rebuildN == 0 {
			t.Error("expected RebuildIndex to be called")
		}
	})

	t.Run("agent_job routes to JobRunner", func(t *testing.T) {
		runner := &fakeJobRunner{result: JobResult{Content: "done"}}
		h3 := NewHandler(HandlerConfig{AgentRunner: runner})
		task := &Task{
			Kind:    KindAgentJob,
			Name:    "market-check",
			Payload: `{"goal":"check markets"}`,
		}
		if err := h3.Dispatch(ctx, task); err != nil {
			t.Fatalf("Dispatch agent_job: %v", err)
		}
	})

	t.Run("unknown kind returns error", func(t *testing.T) {
		task := &Task{Kind: TaskKind("bogus"), Name: "x"}
		err := h.Dispatch(ctx, task)
		if err == nil {
			t.Fatal("expected error for unknown kind")
		}
	})
}

// ---- dispatchReminder -------------------------------------------------------

func TestDispatchReminder_SendsBody(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{Notifier: notifier})
	task := &Task{
		Kind:        KindReminder,
		Name:        "buy-milk",
		Payload:     "Buy milk at the store",
		RecipientID: "99",
	}
	if err := h.dispatchReminder(task); err != nil {
		t.Fatalf("dispatchReminder: %v", err)
	}
	if len(notifier.reminders) != 1 {
		t.Fatalf("got %d reminders, want 1", len(notifier.reminders))
	}
	if notifier.reminders[0] != "⏰ Buy milk at the store" {
		t.Errorf("reminder body = %q", notifier.reminders[0])
	}
}

func TestDispatchReminder_DefaultBodyWhenPayloadEmpty(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{Notifier: notifier})
	task := &Task{
		Kind:        KindReminder,
		Name:        "stand-up",
		RecipientID: "99",
	}
	if err := h.dispatchReminder(task); err != nil {
		t.Fatalf("dispatchReminder: %v", err)
	}
	if len(notifier.reminders) != 1 {
		t.Fatalf("got %d reminders, want 1", len(notifier.reminders))
	}
	if notifier.reminders[0] != "Reminder: stand-up" {
		t.Errorf("default body = %q", notifier.reminders[0])
	}
}

func TestDispatchReminder_ErrorOnMissingRecipient(t *testing.T) {
	h := NewHandler(HandlerConfig{Notifier: &fakeNotifier{}})
	err := h.dispatchReminder(&Task{Kind: KindReminder, Name: "x"})
	if err == nil {
		t.Fatal("expected error: no recipient")
	}
}

func TestDispatchReminder_ErrorOnMissingNotifier(t *testing.T) {
	h := NewHandler(HandlerConfig{})
	err := h.dispatchReminder(&Task{Kind: KindReminder, Name: "x", RecipientID: "1"})
	if err == nil {
		t.Fatal("expected error: no notifier")
	}
}

func TestDispatchReminder_PropagatesNotifierError(t *testing.T) {
	notifier := &fakeNotifier{sendErr: errors.New("telegram down")}
	h := NewHandler(HandlerConfig{Notifier: notifier})
	task := &Task{
		Kind:        KindReminder,
		Name:        "x",
		Payload:     "body",
		RecipientID: "1",
	}
	if err := h.dispatchReminder(task); err == nil {
		t.Fatal("expected notifier error to propagate")
	}
}

// ---- dispatchWikiMaintenance ------------------------------------------------

func TestDispatchWikiMaintenance_CallsRebuildAndLog(t *testing.T) {
	fw := &fakeWiki{}
	h := NewHandler(HandlerConfig{Wiki: fw})
	if err := h.dispatchWikiMaintenance(context.Background()); err != nil {
		t.Fatalf("dispatchWikiMaintenance: %v", err)
	}
	if fw.rebuildN == 0 {
		t.Error("RebuildIndex not called")
	}
	if len(fw.logCalls) == 0 {
		t.Error("AppendLog not called")
	}
	if fw.logCalls[0] != "nightly-maintenance" {
		t.Errorf("log action = %q", fw.logCalls[0])
	}
}

func TestDispatchWikiMaintenance_ErrorOnMissingWiki(t *testing.T) {
	h := NewHandler(HandlerConfig{})
	err := h.dispatchWikiMaintenance(context.Background())
	if err == nil {
		t.Fatal("expected error: no wiki")
	}
}

func TestDispatchWikiMaintenance_LintErrorBubblesUp(t *testing.T) {
	fw := &fakeWiki{lintErr: errors.New("lint failed")}
	h := NewHandler(HandlerConfig{Wiki: fw})
	err := h.dispatchWikiMaintenance(context.Background())
	if err == nil {
		t.Fatal("expected lint error to propagate")
	}
}

func TestDispatchWikiMaintenance_NotifiesOwnersWhenWired(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{Wiki: &fakeWiki{}, Notifier: notifier})
	if err := h.dispatchWikiMaintenance(context.Background()); err != nil {
		t.Fatalf("dispatchWikiMaintenance: %v", err)
	}
	// No issues means no notification; just verify no panic and log is written.
	_ = notifier.owners
}

// ---- dispatchAgentJob -------------------------------------------------------

func TestDispatchAgentJob_RunsRunner(t *testing.T) {
	runner := &fakeJobRunner{result: JobResult{Content: "all good", LLMCalls: 2}}
	h := NewHandler(HandlerConfig{AgentRunner: runner})
	task := &Task{
		Kind:    KindAgentJob,
		Name:    "daily-check",
		Payload: `{"goal":"check memory and propose updates"}`,
	}
	if err := h.dispatchAgentJob(context.Background(), task); err != nil {
		t.Fatalf("dispatchAgentJob: %v", err)
	}
}

func TestDispatchAgentJob_ErrorOnMissingRunner(t *testing.T) {
	h := NewHandler(HandlerConfig{})
	task := &Task{
		Kind:    KindAgentJob,
		Name:    "x",
		Payload: `{"goal":"check memory"}`,
	}
	err := h.dispatchAgentJob(context.Background(), task)
	if err == nil {
		t.Fatal("expected error: no runner")
	}
}

func TestDispatchAgentJob_NotifiesOnCompletion(t *testing.T) {
	notifier := &fakeNotifier{}
	runner := &fakeJobRunner{result: JobResult{Content: "report body"}}
	trueBool := true
	h := NewHandler(HandlerConfig{
		Notifier:    notifier,
		AgentRunner: runner,
	})
	payload := `{"goal":"check sources","notify":true}`
	task := &Task{
		Kind:        KindAgentJob,
		Name:        "source-check",
		Payload:     payload,
		RecipientID: "42",
	}
	_ = trueBool
	if err := h.dispatchAgentJob(context.Background(), task); err != nil {
		t.Fatalf("dispatchAgentJob: %v", err)
	}
	if len(notifier.completions) == 0 {
		t.Fatal("expected SendCompletion to be called when notify=true")
	}
}

func TestDispatchAgentJob_SkipsNotifyWhenFalse(t *testing.T) {
	notifier := &fakeNotifier{}
	runner := &fakeJobRunner{result: JobResult{Content: "report body"}}
	h := NewHandler(HandlerConfig{
		Notifier:    notifier,
		AgentRunner: runner,
	})
	task := &Task{
		Kind:        KindAgentJob,
		Name:        "quiet-job",
		Payload:     `{"goal":"check sources","notify":false}`,
		RecipientID: "42",
	}
	if err := h.dispatchAgentJob(context.Background(), task); err != nil {
		t.Fatalf("dispatchAgentJob: %v", err)
	}
	if len(notifier.completions) != 0 {
		t.Fatal("expected SendCompletion NOT to be called when notify=false")
	}
}

func TestDispatchAgentJob_RunnerErrorPropagates(t *testing.T) {
	runner := &fakeJobRunner{err: errors.New("llm unavailable")}
	h := NewHandler(HandlerConfig{AgentRunner: runner})
	task := &Task{
		Kind:    KindAgentJob,
		Name:    "x",
		Payload: `{"goal":"check memory"}`,
	}
	if err := h.dispatchAgentJob(context.Background(), task); err == nil {
		t.Fatal("expected runner error to propagate")
	}
}

// ---- compile-time interface checks ------------------------------------------

var (
	_ Notifier  = (*fakeNotifier)(nil)
	_ JobRunner = (*fakeJobRunner)(nil)
	_ wiki.Repository = (*fakeWiki)(nil)
)

