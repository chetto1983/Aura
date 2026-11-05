//go:build db_integration

package pimwatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/decide"
)

// decideRig is a watch whose model step is on: a local route and the given described labels.
func decideRig(t *testing.T, labels ...LabelInput) (*watchRig, *fakeDecider, map[string]string) {
	t.Helper()
	r := newWatchRig(t)
	ids := map[string]string{}
	for i, in := range labels {
		label, err := r.store.AddLabel(context.Background(), r.identity, in, i)
		if err != nil {
			t.Fatalf("add label: %v", err)
		}
		ids[in.Name] = label.ID
	}
	decider := &fakeDecider{route: localDecide}
	r.watcher.Decider = decider
	return r, decider, ids
}

var (
	adsLabel    = LabelInput{Name: "Advertising", Tier: TierLater, Description: "Offers sent to a list."}
	urgentLabel = LabelInput{Name: "Breakdown", Tier: TierNow, Description: "A machine has stopped."}
	junkLabel   = LabelInput{Name: "Junk", Discard: true, Description: "Nothing anyone needs."}
)

// unsortedMail is a mail from someone the account never wrote to, which the ladder leaves `today`.
func (r *watchRig) unsortedMail(id string, s Signals) Change {
	from, subject := id+"@shop.example", "Subject of "+id
	r.pim.items[id] = Item{ID: id, From: from, Subject: subject}
	return Change{Type: ChangeAdded, ID: id, From: from, Subject: subject, Snippet: "Snippet of " + id, Signals: s}
}

func (r *watchRig) mailPage(cursor, next string, more bool, changes ...Change) {
	r.pim.page(ResourceMail, cursor, ChangesPage{Changes: changes, NextCursor: next, HasMore: more})
}

func (r *watchRig) accountSettings() AccountSettings {
	r.t.Helper()
	settings, err := r.store.AccountSettings(context.Background(), r.identity)
	if err != nil {
		r.t.Fatalf("account settings: %v", err)
	}
	return settings[rigAccount]
}

func TestTheModelLabelsAnUnsortedMailAndBringsItsTier(t *testing.T) {
	r, decider, _ := decideRig(t)
	if err := r.store.SeedLabels(context.Background(), r.identity, DefaultLabels(LangEN)); err != nil {
		t.Fatal(err)
	}
	decider.script = []fakeAnswer{{choice: "Advertising", p: 0.8}}
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	out := r.tick()

	verdict := r.mailVerdict("m1")
	if verdict.Tier != TierLater || verdict.By != ByModel || verdict.LadderTier != TierToday || verdict.LadderBy != ByDefault {
		t.Fatalf("verdict = %+v", verdict)
	}
	book, err := r.store.Book(context.Background(), r.identity)
	if err != nil {
		t.Fatal(err)
	}
	if name := book.LabelName(verdict.LabelID); name != "Advertising" || verdict.ModelLabelID != verdict.LabelID {
		t.Fatalf("label %q (%s), the model's %s", name, verdict.LabelID, verdict.ModelLabelID)
	}
	if verdict.ModelProbability != 0.8 || verdict.ModelOptions != 6 || verdict.ModelTier != TierLater || verdict.ModelName != "fake/d1" || verdict.ModelSkipped {
		t.Fatalf("model columns = %+v", verdict)
	}
	if decider.states[0] != "Subject: Subject of m1\n\nSnippet of m1" || strings.Contains(decider.states[0], "@") {
		t.Fatalf("the model read %q", decider.states[0])
	}
	mustContain(t, out.Summary, "decide: 1 labelled, 0 none, 0 left, 0 no text")
}

func TestAModelNowStartsAnAlert(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}}
	mail := r.unsortedMail("m1", Signals{WrittenTo: no()})
	r.mailPage("start-mail", "m-2", false, mail)
	run, out, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out.Notice, "m1@shop.example: Subject of m1")
	if verdict := r.mailVerdict("m1"); verdict.Tier != TierNow || verdict.By != ByModel || verdict.AlertRunID != run {
		t.Fatalf("verdict = %+v", verdict)
	}

	ctx := context.Background()
	tk, err := r.watcher.open(ctx, TickInput{IdentityID: r.identity, AccountID: rigAccount, RunID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if _, lines, _ := r.watcher.draft(ctx, tk, ResourceMail, []Change{mail}); lines["m1"].Tier != TierNow {
		t.Fatalf("the alert line reads %q", lines["m1"].Tier)
	}
}

func TestThePeopleFloorHolds(t *testing.T) {
	r, decider, ids := decideRig(t, junkLabel, urgentLabel, adsLabel)
	decider.script = []fakeAnswer{{choice: junkLabel.Name, p: 0.9}, {choice: urgentLabel.Name, p: 0.9}, {choice: adsLabel.Name, p: 0.9}}
	r.mailPage("start-mail", "m-2", false,
		r.unsortedMail("written", Signals{WrittenTo: yes()}),
		r.unsortedMail("raised", Signals{WrittenTo: yes()}),
		r.unsortedMail("unknown", Signals{}))
	r.tick()
	if v := r.mailVerdict("written"); v.Tier != TierToday || v.By != ByPeople || v.Discarded || v.LabelID != ids[junkLabel.Name] {
		t.Fatalf("a discard label on someone written to = %+v", v)
	}
	if v := r.mailVerdict("raised"); v.Tier != TierNow || v.By != ByModel {
		t.Fatalf("a now label on someone written to = %+v", v)
	}
	if v := r.mailVerdict("unknown"); v.Tier != TierToday || v.By != ByDefault || v.LabelID != ids[adsLabel.Name] {
		t.Fatalf("a later label on an unknown sender = %+v", v)
	}
}

func TestARuleLabelIsFinalAndTheModelIsNotAsked(t *testing.T) {
	r, decider, ids := decideRig(t, adsLabel, urgentLabel)
	if _, err := r.store.PutRule(context.Background(), r.identity, Rule{Key: "ruled@shop.example", Kind: KeySender, LabelID: ids[adsLabel.Name]}); err != nil {
		t.Fatal(err)
	}
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}}
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("ruled", Signals{WrittenTo: no()}))
	r.tick()
	if decider.calls != 0 {
		t.Fatalf("the model was asked %d times", decider.calls)
	}
	if v := r.mailVerdict("ruled"); v.Tier != TierLater || v.By != ByRule || v.LabelID != ids[adsLabel.Name] || v.ModelAsked || v.ModelSkipped {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestBelowTheThresholdNothingIsApplied(t *testing.T) {
	r, decider, ids := decideRig(t, urgentLabel)
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.49}}
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	out := r.tick()
	v := r.mailVerdict("m1")
	if v.Tier != TierToday || v.By != ByDefault || v.LabelID != "" || v.ModelTier != "" {
		t.Fatalf("verdict = %+v", v)
	}
	if !v.ModelAsked || v.ModelLabelID != ids[urgentLabel.Name] || v.ModelProbability != 0.49 {
		t.Fatalf("what the model said was not kept: %+v", v)
	}
	mustContain(t, out.Summary, "decide: 0 labelled, 1 none, 0 left, 0 no text")
}

func TestAMailWithNoSnippetIsNotAsked(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	mail := r.unsortedMail("m1", Signals{WrittenTo: no()})
	mail.Snippet = " "
	r.mailPage("start-mail", "m-2", false, mail)
	out := r.tick()
	if v := r.mailVerdict("m1"); decider.calls != 0 || !v.ModelSkipped || v.ModelAsked {
		t.Fatalf("calls %d, verdict %+v", decider.calls, v)
	}
	mustContain(t, out.Summary, "decide: 0 labelled, 0 none, 0 left, 1 no text")
}

func TestAFailingModelIsAskedOncePerTickAndShown(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	decider.script = []fakeAnswer{{err: errBroken}}
	r.mailPage("start-mail", "m-2", true, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	r.mailPage("m-2", "m-3", false, r.unsortedMail("m2", Signals{WrittenTo: no()}))
	out := r.tick()
	if decider.calls != 1 {
		t.Fatalf("a failing model was asked %d times", decider.calls)
	}
	for _, id := range []string{"m1", "m2"} {
		if v := r.mailVerdict(id); v.Tier != TierToday || v.By != ByDefault || !v.ModelSkipped || v.ModelAsked {
			t.Fatalf("%s = %+v, want the ladder's verdict", id, v)
		}
	}
	mustContain(t, out.Summary, "decide: 0 labelled, 0 none, 2 left, 0 no text (failed)")
	if s := r.accountSettings(); s.DecideError != "unreachable" || s.DecideFailedAt.IsZero() {
		t.Fatalf("account = %+v", s)
	}

	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}}
	decider.calls = 0
	r.mailPage("m-3", "m-4", false, r.unsortedMail("m3", Signals{WrittenTo: no()}))
	r.tick()
	if s := r.accountSettings(); decider.calls != 1 || s.DecideError != "" || !s.DecideFailedAt.IsZero() {
		t.Fatalf("the answer left the failure on the account: %+v", s)
	}
}

func TestTheBudgetLeavesTheRestToTheLadder(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	r.watcher.DecideTime = 50 * time.Millisecond
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}, {choice: urgentLabel.Name, p: 0.9, sleep: 100 * time.Millisecond}, {choice: urgentLabel.Name, p: 0.9}}
	r.mailPage("start-mail", "m-2", false,
		r.unsortedMail("m1", Signals{WrittenTo: no()}),
		r.unsortedMail("m2", Signals{WrittenTo: no()}),
		r.unsortedMail("m3", Signals{WrittenTo: no()}))
	out := r.tick()
	mustContain(t, out.Summary, "decide: 1 labelled, 0 none, 2 left, 0 no text")
	if strings.Contains(out.Summary, "(failed)") || r.accountSettings().DecideError != "" {
		t.Fatalf("a spent budget read as a failure: %s", out.Summary)
	}
	for _, id := range []string{"m2", "m3"} {
		if v := r.mailVerdict(id); !v.ModelSkipped || v.Tier != TierToday {
			t.Fatalf("%s = %+v", id, v)
		}
	}
}

func TestASlowPIMReadLeavesTheModelItsBudget(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	r.watcher.DecideTime = 50 * time.Millisecond
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}}
	r.pim.onChanges = func(string, string) { time.Sleep(80 * time.Millisecond) }
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	out := r.tick()
	if decider.calls != 1 || r.mailVerdict("m1").Tier != TierNow {
		t.Fatalf("calls %d; %s", decider.calls, out.Summary)
	}
}

// tickBefore runs one tick under a deadline on the rig's clock, which hasTime reads.
func (r *watchRig) tickBefore(left time.Duration) Outcome {
	r.t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), r.clock.at.Add(left))
	defer cancel()
	out, err := r.watcher.Tick(ctx, TickInput{IdentityID: r.identity, AccountID: rigAccount, RunID: uuid.NewString()})
	if err != nil {
		r.t.Fatalf("tick: %v", err)
	}
	return out
}

func TestTheCalendarIsReadAfterASlowModel(t *testing.T) {
	t.Run("the budget spent on the mail page", func(t *testing.T) {
		r, decider, _ := decideRig(t, urgentLabel)
		r.watcher.DecideTime = 50 * time.Millisecond
		decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9, sleep: time.Second}}
		r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
		before := r.pim.calls[ResourceCalendar]
		out := r.tickBefore(85 * time.Second)
		if r.pim.calls[ResourceCalendar] != before+1 || strings.Contains(out.Summary, "backlog") {
			t.Fatalf("the calendar was not read: %s", out.Summary)
		}
		mustContain(t, out.Summary, "decide: 0 labelled, 0 none, 1 left, 0 no text")
	})
	t.Run("the unspent budget is reserved for a mail page alone", func(t *testing.T) {
		r, decider, _ := decideRig(t, urgentLabel)
		r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
		before := r.pim.calls[ResourceCalendar]
		out := r.tickBefore(85 * time.Second)
		if r.pim.calls[ResourceCalendar] != before+1 || decider.calls != 0 {
			t.Fatalf("calendar reads %d, model calls %d: %s", r.pim.calls[ResourceCalendar]-before, decider.calls, out.Summary)
		}
		mustContain(t, out.Summary, "backlog continues on the next tick")
	})
	t.Run("a second mail page waits for the next tick", func(t *testing.T) {
		r, decider, _ := decideRig(t, urgentLabel)
		r.watcher.DecideTime = 20 * time.Second
		mail := r.unsortedMail("m1", Signals{WrittenTo: no()})
		mail.Snippet = ""
		r.mailPage("start-mail", "m-2", true, mail)
		r.mailPage("m-2", "m-3", false, r.unsortedMail("m2", Signals{WrittenTo: no()}))
		// The first page's read takes 10 s of the run, so the second page finds 85 s left and needs 90.
		r.pim.onChanges = func(resource, cursor string) {
			if resource == ResourceMail && cursor == "start-mail" {
				r.clock.advance(10 * time.Second)
			}
		}
		before := r.pim.calls[ResourceCalendar]
		out := r.tickBefore(95 * time.Second)
		if r.pim.calls[ResourceCalendar] != before+1 || decider.calls != 0 || r.cursor(ResourceMail).Value != "m-2" {
			t.Fatalf("calendar reads %d, model calls %d, mail cursor %q", r.pim.calls[ResourceCalendar]-before, decider.calls, r.cursor(ResourceMail).Value)
		}
		mustContain(t, out.Summary, "backlog continues on the next tick", "decide: 0 labelled, 0 none, 0 left, 1 no text")
	})
}

func TestAHostedRouteWithoutConsentAsksNothing(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	decider.route = decide.Route{BaseURL: "https://openrouter.example/api/v1", Model: "vendor/decider"}
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}}
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	out := r.tick()
	if v := r.mailVerdict("m1"); decider.calls != 0 || v.ModelSkipped {
		t.Fatalf("calls %d, verdict %+v", decider.calls, v)
	}
	mustContain(t, out.Summary, "decide: no consent")

	if err := r.store.SetCloudDecide(context.Background(), r.identity, rigAccount, true); err != nil {
		t.Fatal(err)
	}
	r.mailPage("m-2", "m-3", false, r.unsortedMail("m2", Signals{WrittenTo: no()}))
	r.tick()
	if v := r.mailVerdict("m2"); decider.calls != 1 || v.ModelName != "fake/d1" {
		t.Fatalf("with consent: calls %d, verdict %+v", decider.calls, v)
	}
}

func TestNoDescribedLabelAsksNothing(t *testing.T) {
	r, decider, _ := decideRig(t, LabelInput{Name: "Suppliers"})
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	out := r.tick()
	if v := r.mailVerdict("m1"); decider.calls != 0 || v.ModelSkipped {
		t.Fatalf("calls %d, verdict %+v", decider.calls, v)
	}
	mustContain(t, out.Summary, "decide: no described label")
}

func TestTheTickReadsTheRouteEachTime(t *testing.T) {
	r, decider, _ := decideRig(t, urgentLabel)
	decider.script = []fakeAnswer{{choice: urgentLabel.Name, p: 0.9}}
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	r.tick()

	decider.route = decide.Route{}
	r.mailPage("m-2", "m-3", false, r.unsortedMail("m2", Signals{WrittenTo: no()}))
	out := r.tick()
	if v := r.mailVerdict("m2"); decider.calls != 1 || v.ModelSkipped || strings.Contains(out.Summary, "decide:") {
		t.Fatalf("an off route: calls %d, verdict %+v, summary %s", decider.calls, v, out.Summary)
	}

	decider.route, decider.routeErr = localDecide, errBroken
	r.mailPage("m-3", "m-4", false, r.unsortedMail("m3", Signals{WrittenTo: no()}))
	out = r.tick()
	mustContain(t, out.Summary, "decide: route unreadable")
	if decider.calls != 1 || decider.routes != 3 {
		t.Fatalf("calls %d, route reads %d", decider.calls, decider.routes)
	}
}

func TestTheShadowStillComparesTheLadder(t *testing.T) {
	r, decider, _ := decideRig(t, adsLabel)
	r.watcher.Centroid = &fakeCentroid{answers: []CentroidAnswer{{Tier: TierToday, Margin: 0.04}}}
	decider.script = []fakeAnswer{{choice: adsLabel.Name, p: 0.9}}
	r.mailPage("start-mail", "m-2", false, r.unsortedMail("m1", Signals{WrittenTo: no()}))
	out := r.tick()
	mustContain(t, out.Summary, "shadow: 1 answered, 1 agreed, 0 empty")
	if v := r.mailVerdict("m1"); v.Tier != TierLater || v.Centroid != TierToday || v.SenderMemory != TierToday || v.LadderTier != TierToday {
		t.Fatalf("verdict = %+v", v)
	}
}
