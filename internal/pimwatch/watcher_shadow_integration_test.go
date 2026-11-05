//go:build db_integration

package pimwatch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// fakeCentroid answers Judge with its answers, or its error, and records what it was asked.
type fakeCentroid struct {
	mu        sync.Mutex
	answers   []CentroidAnswer
	err       error
	asked     [][]ShadowMail
	lessons   []Lesson
	learnErr  error
	forgotten []string
	forgetErr error
}

func (f *fakeCentroid) Judge(_ context.Context, _ string, mails []ShadowMail) ([]CentroidAnswer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, mails)
	return f.answers, f.err
}

func (f *fakeCentroid) Learn(_ context.Context, _ string, lesson Lesson) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lessons = append(f.lessons, lesson)
	return f.learnErr
}

func (f *fakeCentroid) Forget(_ context.Context, _, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgotten = append(f.forgotten, accountID)
	return f.forgetErr
}

func (r *watchRig) mailVerdict(id string) Verdict {
	r.t.Helper()
	verdict, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceMail, id)
	if err != nil {
		r.t.Fatalf("verdict %s: %v", id, err)
	}
	return verdict
}

func TestTheTickWritesTheShadowBesideTheVerdict(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m0", TierToday, ""))
	if _, err := r.store.ApplyCorrection(ctx, r.identity, "a@one.example", correctionOf("m0", TierNow)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.PutRule(ctx, r.identity, Rule{Key: "two.example", Kind: KeyDomain, Tier: TierToday}); err != nil {
		t.Fatal(err)
	}
	written := true
	// The commit above moved the mail cursor, so the page is served where it now stands.
	r.pim.page(ResourceMail, r.cursor(ResourceMail).Value, ChangesPage{NextCursor: "c1", Changes: []Change{
		{Type: ChangeAdded, ID: "m1", From: "a@one.example", Subject: "Udienza", Signals: Signals{WrittenTo: &written}},
		{Type: ChangeAdded, ID: "m2", From: "b@two.example", Subject: "Offerta", Signals: Signals{Bulk: true}},
	}})
	centroid := &fakeCentroid{answers: []CentroidAnswer{{Tier: TierNow, Margin: 0.05}, {Tier: TierLater, Margin: 0.01}}}
	r.watcher.Centroid = centroid

	out := r.tick()

	first, second := r.mailVerdict("m1"), r.mailVerdict("m2")
	if first.LadderTier != TierToday || first.LadderBy != ByPeople || first.SenderMemory != TierNow || first.Centroid != TierNow ||
		first.Facts != (MailFacts{WrittenTo: true}) {
		t.Fatalf("first row = %+v", first)
	}
	if second.LadderTier != TierToday || second.LadderBy != ByRule || second.SenderMemory != TierToday || second.Centroid != TierLater ||
		second.Facts != (MailFacts{Bulk: true}) {
		t.Fatalf("second row = %+v", second)
	}
	rows, err := r.store.ShadowRows(ctx, r.identity, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	margins := map[Tier]float64{}
	for _, row := range rows {
		margins[row.Centroid] = row.Margin
	}
	if margins[TierNow] != 0.05 || margins[TierLater] != 0.01 {
		t.Fatalf("margins = %v", margins)
	}
	if !strings.HasSuffix(out.Summary, "shadow: 2 answered, 0 agreed, 0 empty") {
		t.Fatalf("summary = %q", out.Summary)
	}
	if len(centroid.asked) != 1 || centroid.asked[0][0].Facts != (MailFacts{WrittenTo: true}) || centroid.asked[0][1].Subject != "Offerta" {
		t.Fatalf("asked = %+v", centroid.asked)
	}
}

func TestAFailingCentroidLeavesTheVerdictAsToday(t *testing.T) {
	r := newWatchRig(t)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{NextCursor: "c1", Changes: []Change{r.urgentMail("m1"), r.todayMail("m2")}})
	r.watcher.Centroid = &fakeCentroid{err: errors.New("embedder down")}

	_, out, err := r.run()
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := r.mailVerdict("m1"); got.Tier != TierNow || got.Centroid != "" {
		t.Fatalf("urgent row = %+v", got)
	}
	if want := "2 decided, 1 alerted, 0 capped; shadow: 0 answered, 0 agreed, 2 empty"; out.Summary != want {
		t.Fatalf("summary = %q, want %q", out.Summary, want)
	}
	if out.Notice == "" {
		t.Fatal("the urgent mail was not told")
	}
}

func TestAFailingCentroidIsAskedOncePerTick(t *testing.T) {
	r := newWatchRig(t)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{NextCursor: "c1", HasMore: true, Changes: []Change{r.todayMail("m1")}})
	r.pim.page(ResourceMail, "c1", ChangesPage{NextCursor: "c2", HasMore: true, Changes: []Change{r.todayMail("m2")}})
	r.pim.page(ResourceMail, "c2", ChangesPage{NextCursor: "c3", Changes: []Change{r.todayMail("m3")}})
	centroid := &fakeCentroid{err: context.DeadlineExceeded}
	r.watcher.Centroid = centroid

	out := r.tick()

	if len(centroid.asked) != 1 {
		t.Fatalf("asked %d times, want once: a hung embedder costs one timeout per tick", len(centroid.asked))
	}
	if !strings.HasSuffix(out.Summary, "shadow: 0 answered, 0 agreed, 3 empty") {
		t.Fatalf("summary = %q", out.Summary)
	}
	if got := r.mailVerdict("m3"); got.LadderTier != TierToday || got.SenderMemory != TierToday || got.Centroid != "" {
		t.Fatalf("a page after the failure = %+v", got)
	}
}

func TestNoCentroidStillWritesTheFactsAndTheMemory(t *testing.T) {
	r := newWatchRig(t)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{NextCursor: "c1", Changes: []Change{r.todayMail("m1")}})

	out := r.tick()

	got := r.mailVerdict("m1")
	if got.Facts != (MailFacts{WrittenTo: true}) || got.SenderMemory != TierToday || got.LadderTier != TierToday || got.Centroid != "" {
		t.Fatalf("row = %+v", got)
	}
	if strings.Contains(out.Summary, "shadow:") {
		t.Fatalf("summary = %q", out.Summary)
	}
}

func TestAMailWithNoSenderStillGetsAShadow(t *testing.T) {
	r := newWatchRig(t)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{NextCursor: "c1", Changes: []Change{
		{Type: ChangeAdded, ID: "m1", From: "not an address"},
	}})
	centroid := &fakeCentroid{answers: []CentroidAnswer{{Tier: TierLater, Margin: 0.02}}}
	r.watcher.Centroid = centroid

	r.tick()

	got := r.mailVerdict("m1")
	if got.SenderMemory != got.LadderTier || got.LadderTier == "" || got.Centroid != TierLater {
		t.Fatalf("row = %+v", got)
	}
	if len(centroid.asked) != 1 || centroid.asked[0][0].From != "not an address" {
		t.Fatalf("asked = %+v", centroid.asked)
	}
}

// landed commits a mail row as the tick writes it after 0152, readable live by id.
func (r *serviceRig) landed(id, from string, facts MailFacts) {
	r.t.Helper()
	r.pim.items[id] = Item{ID: id, From: from, Subject: "Subject of " + id}
	r.commit(rigAccount, VerdictDraft{
		ItemID: id, Decision: Decision{Tier: TierToday, By: ByPeople, AuthFailed: facts.AuthFailed},
		Shadow: &Shadow{Facts: facts, SenderMemory: TierToday},
	})
}

func TestACorrectionTeachesTheCentroid(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	centroid := &fakeCentroid{}
	r.service.Centroid = centroid
	r.landed("m1", "Mario <mario@one.example>", MailFacts{WrittenTo: true, Bulk: true})
	r.landed("m2", "b@one.example", MailFacts{})
	r.landed("m3", "c@one.example", MailFacts{AuthFailed: true})
	// m4 is a row from before 0152: no ladder answer, so no facts to learn from.
	r.pim.items["m4"] = Item{ID: "m4", From: "d@one.example"}
	if err := r.store.tx(ctx, r.identity, func(q *sqlc.Queries, id pgtype.UUID) error {
		_, err := q.InsertPIMWatchVerdict(ctx, sqlc.InsertPIMWatchVerdictParams{
			IdentityID: id, AccountID: rigAccount, Resource: ResourceMail, ItemID: "m4",
			Tier: string(TierToday), DecidedBy: string(ByDefault), DecidedAt: timestamp(r.clock.at),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	label, err := r.store.AddLabel(ctx, r.identity, LabelInput{Name: "Cliente"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	pre := Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m4", Tier: TierLater}

	if _, _, err := r.service.Correct(ctx, r.identity, correctionOf("m1", TierLater)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m2", LabelID: &label.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.service.Correct(ctx, r.identity, correctionOf("m3", TierNow)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.service.Correct(ctx, r.identity, pre); err != nil {
		t.Fatal(err)
	}

	if len(centroid.lessons) != 1 {
		t.Fatalf("lessons = %+v, want the tier correction of m1 alone", centroid.lessons)
	}
	want := Lesson{
		AccountID: rigAccount, ItemID: "m1", From: "Mario <mario@one.example>", Subject: "Subject of m1",
		Facts: MailFacts{WrittenTo: true, Bulk: true}, Tier: TierLater,
	}
	if centroid.lessons[0] != want {
		t.Fatalf("lesson = %+v, want %+v", centroid.lessons[0], want)
	}
}

func TestAFailedLessonKeepsTheCorrection(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.service.Centroid = &fakeCentroid{learnErr: context.DeadlineExceeded}
	r.landed("m1", "a@one.example", MailFacts{})

	verdict, _, err := r.service.Correct(ctx, r.identity, correctionOf("m1", TierLater))
	if err != nil || verdict.Tier != TierLater {
		t.Fatalf("correction = %+v, %v", verdict, err)
	}
	if accounts, err := r.store.CorrectedAccounts(ctx, r.identity); err != nil || len(accounts) != 1 {
		t.Fatalf("the correction row is missing: %v, %v", accounts, err)
	}
}

func TestRemovingAnAccountForgetsItsExamples(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	centroid := &fakeCentroid{forgetErr: errors.New("arcadedb down")}
	r.service.Centroid = centroid
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}

	err := r.service.RemoveAccount(ctx, r.identity, "home")
	if err == nil || !strings.Contains(err.Error(), "forget the removed account's examples: arcadedb down") {
		t.Fatalf("remove = %v, want the forget's failure, named as the examples'", err)
	}
	if len(centroid.forgotten) != 1 || centroid.forgotten[0] != "home" {
		t.Fatalf("forgotten = %v", centroid.forgotten)
	}
	if len(r.tasks.digests) != 0 {
		t.Fatalf("the idle digest stayed: %d", len(r.tasks.digests))
	}
}

func TestDecisionsMarkWhereTheCentroidDisagrees(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for _, c := range []struct {
		id       string
		centroid Tier
	}{{"m1", TierNow}, {"m2", TierToday}, {"m3", TierToday}} {
		r.pim.items[c.id] = Item{ID: c.id, From: c.id + "@one.example", Subject: "Subject of " + c.id}
		r.commit(rigAccount, VerdictDraft{
			ItemID: c.id, Decision: Decision{Tier: TierToday, By: ByPeople},
			Shadow: &Shadow{SenderMemory: TierToday, Centroid: c.centroid, Margin: 0.03},
		})
	}
	if _, _, err := r.service.Correct(ctx, r.identity, correctionOf("m3", TierLater)); err != nil {
		t.Fatal(err)
	}

	day, err := r.service.Decisions(ctx, r.identity, rigAccount, r.clock.at.Format(time.DateOnly), false)
	if err != nil {
		t.Fatal(err)
	}
	marks := map[string]Tier{}
	for _, line := range day.Lines {
		marks[line.ItemID] = line.Centroid
	}
	if len(marks) != 3 || marks["m1"] != TierNow || marks["m2"] != "" || marks["m3"] != "" {
		t.Fatalf("marks = %v, want m1 alone, as now", marks)
	}
}
