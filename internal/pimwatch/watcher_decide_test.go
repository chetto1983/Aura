package pimwatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/decide"
)

// fakeDecider answers from its script in turn, the last answer standing for the rest. It honours its
// context, so a sleep the budget cuts short returns the deadline as the real client does.
type fakeDecider struct {
	route    decide.Route
	routeErr error
	routes   int
	script   []fakeAnswer
	calls    int
	states   []string
	// allowed is the time each call's context had left when it arrived.
	allowed []time.Duration
}

type fakeAnswer struct {
	choice   string
	p        float64
	sleep    time.Duration
	err      error
	nameless bool
}

func (f *fakeDecider) Route(context.Context) (decide.Route, error) {
	f.routes++
	return f.route, f.routeErr
}

func (f *fakeDecider) Choose(ctx context.Context, _ decide.Route, state string, _ decide.Question) (decide.Answer, error) {
	f.calls++
	f.states = append(f.states, state)
	if deadline, ok := ctx.Deadline(); ok {
		f.allowed = append(f.allowed, time.Until(deadline))
	}
	answer := f.script[min(f.calls, len(f.script))-1]
	if answer.sleep > 0 {
		select {
		case <-time.After(answer.sleep):
		case <-ctx.Done():
			return decide.Answer{}, ctx.Err()
		}
	}
	if answer.err != nil {
		return decide.Answer{}, answer.err
	}
	model := "fake/d1"
	if answer.nameless {
		model = ""
	}
	return decide.Answer{Choice: answer.choice, Probabilities: map[string]float64{answer.choice: answer.p}, Model: model}, nil
}

var (
	localDecide  = decide.Route{BaseURL: "http://decide.example:8085"}
	errModelDown = errors.New("the model is down")
)

// decideStore answers what the model step reads and writes. The rest of WatchStore is never
// reached by it, and a call there panics.
type decideStore struct {
	WatchStore
	consent    bool
	consentErr error
	recorded   []string
	recordErr  error
}

func (s *decideStore) CloudDecide(context.Context, string, string) (bool, error) {
	return s.consent, s.consentErr
}

func (s *decideStore) RecordDecideFailure(_ context.Context, _, _, class string) error {
	s.recorded = append(s.recorded, class)
	return s.recordErr
}

var (
	unitUrgent = Label{ID: "l-urgent", Name: "Breakdown", Tier: TierNow, Description: "A machine has stopped."}
	unitFiled  = Label{ID: "l-filed", Name: "Filed", Tier: TierLater}
)

// openUnit opens a tick's model step over the given labels, as Watcher.open does.
func openUnit(t *testing.T, w *Watcher, labels ...Label) (*tick, error) {
	t.Helper()
	tk := &tick{IdentityID: "id-1", AccountID: "work", book: NewBook(nil, labels), lang: LangEN}
	return tk, w.openDecide(context.Background(), tk)
}

func TestTheModelStepOpensOnlyWithARouteConsentAndADescribedLabel(t *testing.T) {
	t.Parallel()
	hosted := decide.Route{BaseURL: "https://openrouter.example/api/v1", Model: "vendor/decider"}
	cases := []struct {
		name    string
		decider *fakeDecider
		store   *decideStore
		labels  []Label
		on      bool
		note    string
	}{
		{"no decider", nil, &decideStore{}, []Label{unitUrgent}, false, ""},
		{"a route that cannot be read", &fakeDecider{route: localDecide, routeErr: errModelDown}, &decideStore{}, []Label{unitUrgent}, false, "route unreadable"},
		{"an off route", &fakeDecider{}, &decideStore{}, []Label{unitUrgent}, false, ""},
		{"a hosted route without consent", &fakeDecider{route: hosted}, &decideStore{}, []Label{unitUrgent}, false, "no consent"},
		{"a hosted route with consent", &fakeDecider{route: hosted}, &decideStore{consent: true}, []Label{unitUrgent}, true, ""},
		{"no described label", &fakeDecider{route: localDecide}, &decideStore{}, []Label{unitFiled}, false, "no described label"},
		{"a local route", &fakeDecider{route: localDecide}, &decideStore{}, []Label{unitUrgent, unitFiled}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &Watcher{Store: tc.store}
			if tc.decider != nil {
				w.Decider = tc.decider
			}
			tk, err := openUnit(t, w, tc.labels...)
			if err != nil {
				t.Fatal(err)
			}
			if tk.decide.on != tc.on || tk.decide.note != tc.note {
				t.Fatalf("step = on %v, note %q", tk.decide.on, tk.decide.note)
			}
			if tc.note != "" && tk.decide.summary() != "decide: "+tc.note {
				t.Fatalf("summary = %q", tk.decide.summary())
			}
			if tc.on && (tk.decide.budget != DecideBudget || len(tk.decide.question.Options) != 2 || tk.decide.labelIDs[unitUrgent.Name] != unitUrgent.ID) {
				t.Fatalf("step = %+v", tk.decide)
			}
		})
	}
}

func TestAConsentThatCannotBeReadFailsTheTick(t *testing.T) {
	t.Parallel()
	hosted := decide.Route{BaseURL: "https://openrouter.example/api/v1", Model: "vendor/decider"}
	w := &Watcher{Store: &decideStore{consentErr: errModelDown}, Decider: &fakeDecider{route: hosted}}
	if _, err := openUnit(t, w, unitUrgent); !errors.Is(err, errModelDown) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecideTimeSetsTheBudget(t *testing.T) {
	t.Parallel()
	w := &Watcher{Store: &decideStore{}, Decider: &fakeDecider{route: localDecide}, DecideTime: 5 * time.Second}
	if tk, _ := openUnit(t, w, unitUrgent); tk.decide.budget != 5*time.Second || tk.decide.remaining() != 5*time.Second {
		t.Fatalf("budget %s, remaining %s", tk.decide.budget, tk.decide.remaining())
	}
}

// pageOf drafts mails as the ladder does, with their lines.
func pageOf(t *tick, mails ...Change) ([]VerdictDraft, map[string]Line) {
	drafts := make([]VerdictDraft, len(mails))
	lines := map[string]Line{}
	for i, mail := range mails {
		d := DecideMail(t.book, mail.From, mail.Signals)
		drafts[i] = VerdictDraft{ItemID: mail.ID, Ladder: d, Decision: d}
		lines[mail.ID] = Line{ItemID: mail.ID, Tier: d.Tier}
	}
	return drafts, lines
}

func stranger(id, snippet string) Change {
	return Change{Type: ChangeAdded, ID: id, From: id + "@shop.example", Subject: "Subject of " + id, Snippet: snippet, Signals: Signals{WrittenTo: no()}}
}

func TestTheModelStepAsksOnlyWhatNoRuleLabelled(t *testing.T) {
	t.Parallel()
	decider := &fakeDecider{route: localDecide, script: []fakeAnswer{
		{choice: unitUrgent.Name, p: 0.9, nameless: true}, {choice: unitUrgent.Name, p: 0.4}, {choice: "None", p: 0.95},
	}}
	store := &decideStore{}
	w := &Watcher{Store: store, Decider: decider}
	tk, _ := openUnit(t, w, unitUrgent)
	mails := []Change{stranger("ruled", "text"), stranger("blank", "  "), stranger("applied", "text"), stranger("unsure", "text"), stranger("none", "text")}
	drafts, lines := pageOf(tk, mails...)
	drafts[0].Decision.LabelID = unitFiled.ID
	w.decide(context.Background(), tk, mails, drafts, lines)

	if decider.calls != 3 || drafts[0].Model != nil || drafts[0].ModelSkipped {
		t.Fatalf("calls %d; a rule's label was asked or skipped: %+v", decider.calls, drafts[0])
	}
	if !drafts[1].ModelSkipped || drafts[1].Model != nil {
		t.Fatalf("an empty snippet = %+v", drafts[1])
	}
	applied := drafts[2]
	if applied.Decision.Tier != TierNow || applied.Decision.By != ByModel || applied.Ladder.Tier != TierToday || lines["applied"].Tier != TierNow {
		t.Fatalf("applied = %+v, line %+v", applied, lines["applied"])
	}
	if want := (ModelAnswer{LabelID: unitUrgent.ID, Probability: 0.9, Name: "local", Options: 2, Applied: true}); *applied.Model != want {
		t.Fatalf("model = %+v, want %+v", *applied.Model, want)
	}
	unsure := drafts[3]
	if unsure.Decision != unsure.Ladder || unsure.Model.Applied || unsure.Model.LabelID != unitUrgent.ID || lines["unsure"].Tier != TierToday {
		t.Fatalf("below the threshold = %+v", unsure)
	}
	if none := drafts[4]; none.Model.LabelID != "" || none.Model.Applied || none.Model.Name != "fake/d1" || none.Decision.LabelID != "" {
		t.Fatalf("none = %+v", none.Model)
	}
	if got := tk.decide.summary(); got != "decide: 1 labelled, 2 none, 0 left, 1 no text" {
		t.Fatalf("summary = %q", got)
	}
	if len(store.recorded) != 1 || store.recorded[0] != "" {
		t.Fatalf("recorded = %q, want one clear for the tick's first answer", store.recorded)
	}
	if allowed := decider.allowed[0]; allowed > DecideBudget || allowed < DecideBudget-time.Second {
		t.Fatalf("the first call had %s, want the whole budget", allowed)
	}
}

// The record itself failing costs the cockpit its line, and the page goes on.
func TestAFailureStopsTheStepAndIsRecorded(t *testing.T) {
	t.Parallel()
	decider := &fakeDecider{route: localDecide, script: []fakeAnswer{{err: errModelDown}}}
	store := &decideStore{recordErr: errModelDown}
	w := &Watcher{Store: store, Decider: decider}
	tk, _ := openUnit(t, w, unitUrgent)
	mails := []Change{stranger("m1", "text"), stranger("m2", "text")}
	drafts, lines := pageOf(tk, mails...)
	w.decide(context.Background(), tk, mails, drafts, lines)
	if decider.calls != 1 || !tk.decide.failed || tk.decide.remaining() != 0 || !drafts[0].ModelSkipped || !drafts[1].ModelSkipped {
		t.Fatalf("calls %d, step %+v", decider.calls, tk.decide)
	}
	if len(store.recorded) != 1 || store.recorded[0] != "unreachable" {
		t.Fatalf("recorded = %q", store.recorded)
	}
	if got := tk.decide.summary(); got != "decide: 0 labelled, 0 none, 2 left, 0 no text (failed)" {
		t.Fatalf("summary = %q", got)
	}
}

func TestADeadlineSpendsTheBudgetAndIsNoFailure(t *testing.T) {
	t.Parallel()
	decider := &fakeDecider{route: localDecide, script: []fakeAnswer{{err: context.DeadlineExceeded}}}
	store := &decideStore{}
	w := &Watcher{Store: store, Decider: decider}
	tk, _ := openUnit(t, w, unitUrgent)
	mails := []Change{stranger("m1", "text"), stranger("m2", "text")}
	drafts, lines := pageOf(tk, mails...)
	w.decide(context.Background(), tk, mails, drafts, lines)
	if decider.calls != 1 || tk.decide.failed || tk.decide.remaining() != 0 || len(store.recorded) != 0 {
		t.Fatalf("calls %d, step %+v, recorded %q", decider.calls, tk.decide, store.recorded)
	}
	if got := tk.decide.summary(); got != "decide: 0 labelled, 0 none, 2 left, 0 no text" {
		t.Fatalf("summary = %q", got)
	}
}

func TestEachCallHasWhatTheBudgetHasLeft(t *testing.T) {
	t.Parallel()
	decider := &fakeDecider{route: localDecide, script: []fakeAnswer{{choice: "None", p: 0.9, sleep: 30 * time.Millisecond}}}
	w := &Watcher{Store: &decideStore{}, Decider: decider, DecideTime: time.Second}
	tk, _ := openUnit(t, w, unitUrgent)
	mails := []Change{stranger("m1", "text"), stranger("m2", "text")}
	drafts, lines := pageOf(tk, mails...)
	w.decide(context.Background(), tk, mails, drafts, lines)
	if len(decider.allowed) != 2 || decider.allowed[1] > time.Second-30*time.Millisecond || tk.decide.spent < 60*time.Millisecond {
		t.Fatalf("allowed %v, spent %s", decider.allowed, tk.decide.spent)
	}
}

func TestAnOffStepSaysNothingAndSkipsNothing(t *testing.T) {
	t.Parallel()
	tk := &tick{book: NewBook(nil, []Label{unitUrgent}), lang: LangEN}
	mails := []Change{stranger("m1", "")}
	drafts, lines := pageOf(tk, mails...)
	(&Watcher{}).decide(context.Background(), tk, mails, drafts, lines)
	if drafts[0].ModelSkipped || tk.decide.summary() != "" {
		t.Fatalf("draft %+v, summary %q", drafts[0], tk.decide.summary())
	}
}

func TestAMailPageReservesWhatTheModelHasLeft(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	w := &Watcher{Now: func() time.Time { return at }}
	ctx, cancel := context.WithDeadline(context.Background(), at.Add(85*time.Second))
	defer cancel()
	tk := &tick{decide: decideStep{on: true, budget: DecideBudget}}
	if w.hasTime(ctx, tk, ResourceMail) || !w.hasTime(ctx, tk, ResourceCalendar) {
		t.Fatal("the calendar page held the model's budget, or the mail page did not")
	}
	tk.decide.spent = 20 * time.Second
	if !w.hasTime(ctx, tk, ResourceMail) {
		t.Fatal("a mail page held the budget already spent")
	}
	tk.decide.failed = true
	tk.decide.spent = 0
	if !w.hasTime(ctx, tk, ResourceMail) {
		t.Fatal("a failed step still held its budget")
	}
}
