package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSortDigestPutsNowFirstThenLabelsThenTheRest(t *testing.T) {
	labels := []Label{{ID: "l1"}, {ID: "l2"}}
	verdicts := []Verdict{
		{ItemID: "none", Tier: TierToday},
		{ItemID: "second", Tier: TierToday, LabelID: "l2"},
		{ItemID: "first", Tier: TierToday, LabelID: "l1"},
		{ItemID: "urgent", Tier: TierNow, LabelID: "l2"},
		{ItemID: "stale", Tier: TierToday, LabelID: "removed"},
	}
	sortDigest(verdicts, labels)
	got := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		got = append(got, v.ItemID)
	}
	if strings.Join(got, " ") != "urgent first second none stale" {
		t.Fatalf("order = %v", got)
	}
}

func TestSortDigestPutsWhatWasToldLast(t *testing.T) {
	labels := []Label{{ID: "l1"}}
	verdicts := []Verdict{
		{ItemID: "told", Tier: TierNow, LabelID: "l1", Notified: true},
		{ItemID: "plain", Tier: TierToday},
		{ItemID: "urgent", Tier: TierNow},
	}
	sortDigest(verdicts, labels)
	if verdicts[0].ItemID != "urgent" || verdicts[1].ItemID != "plain" || verdicts[2].ItemID != "told" {
		t.Fatalf("order = %+v", verdicts)
	}
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

type fakeLocale struct {
	lang Lang
	loc  *time.Location
}

func (f fakeLocale) Language(context.Context, string) Lang { return f.lang }

func (f fakeLocale) Location(context.Context, string) *time.Location { return f.loc }

// windowStore is a day of verdicts with no database behind it.
type windowStore struct {
	verdicts  []Verdict
	discarded int
	moved     bool
}

func (s *windowStore) Settings(context.Context, string) (Settings, error) { return Settings{}, nil }
func (s *windowStore) Book(context.Context, string) (Book, error)         { return Book{}, nil }
func (s *windowStore) Labels(context.Context, string) ([]Label, error)    { return nil, nil }
func (s *windowStore) DigestVerdicts(context.Context, string, time.Time, time.Time) ([]Verdict, error) {
	return s.verdicts, nil
}

func (s *windowStore) DiscardedInWindow(context.Context, string, time.Time, time.Time) (int, error) {
	return s.discarded, nil
}

func (s *windowStore) SetLastDigest(context.Context, string, time.Time) error {
	s.moved = true
	return nil
}

// everyItem reads every ref as a mail, but for those in gone, and counts what it was asked for.
type everyItem struct {
	read int
	gone map[string]bool
}

func (p *everyItem) Changes(context.Context, string, string, string, int) (ChangesPage, error) {
	return ChangesPage{}, nil
}

func (p *everyItem) Items(_ context.Context, _, _ string, refs []ItemRef) (ItemsResult, error) {
	p.read += len(refs)
	items := make([]Item, 0, len(refs))
	for _, ref := range refs {
		if !p.gone[ref.ID] {
			items = append(items, Item{ID: ref.ID, From: ref.ID + "@acme.it", Subject: "Subject of " + ref.ID})
		}
	}
	return ItemsResult{Items: items}, nil
}

// scriptedModel answers one way and remembers what it was asked.
type scriptedModel struct {
	reply ModelReply
	err   error
	asks  []ModelAsk
}

func (m *scriptedModel) Write(_ context.Context, _ string, ask ModelAsk) (ModelReply, error) {
	m.asks = append(m.asks, ask)
	return m.reply, m.err
}

func writtenDigester(store DigestStore, pim PIMReader, model DigestModel) *Digester {
	return &Digester{Store: store, PIM: pim, Model: model, Locale: fakeLocale{LangEN, time.UTC}}
}

func TestTheModelWritesTheDigest(t *testing.T) {
	store := &windowStore{verdicts: dayOf("new", 2, false)}
	model := &scriptedModel{reply: ModelReply{
		Text:         `{"lines":[{"section":"do","items":[2],"text":"answer the quote"}]}`,
		PromptTokens: 120, CompletionTokens: 30,
	}}

	out, err := writtenDigester(store, &everyItem{}, model).Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "To do\n1. new001@acme.it: answer the quote", "1 more asks nothing of you.")
	mustContain(t, out.Summary, "2 in the window", "1 written by the model", "120+30 tokens")
	mustContain(t, model.asks[0].Items, "1 | new | today", "Subject of new000")
	mustContain(t, model.asks[0].System, "in English")
	if !store.moved {
		t.Fatal("the window did not move")
	}
}

func TestAToldItemIsContextForTheModelAndAGoneOneIsNot(t *testing.T) {
	store := &windowStore{verdicts: slices.Concat(dayOf("told", 1, true), dayOf("new", 2, false))}
	sortDigest(store.verdicts, nil)
	model := &scriptedModel{reply: ModelReply{Text: `{"lines":[]}`}}
	pim := &everyItem{gone: map[string]bool{"new000": true}}

	if _, err := writtenDigester(store, pim, model).Run(context.Background(), "identity"); err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, model.asks[0].Items, "| told |", "Subject of new001")
	if strings.Contains(model.asks[0].Items, "new000") {
		t.Fatalf("a gone item reached the model:\n%s", model.asks[0].Items)
	}
}

func TestAModelThatFailsSendsTheList(t *testing.T) {
	for name, model := range map[string]*scriptedModel{
		"a refused call":    {err: errExpire},
		"an unusable reply": {reply: ModelReply{Text: "Here is your digest."}},
		"a number it made":  {reply: ModelReply{Text: `{"lines":[{"section":"do","items":[9],"text":"x"}]}`}},
	} {
		store := &windowStore{verdicts: dayOf("new", 2, false)}
		out, err := writtenDigester(store, &everyItem{}, model).Run(context.Background(), "identity")
		if err != nil {
			t.Fatalf("%s: digest: %v", name, err)
		}
		mustContain(t, out.Notice, "1. [work] new000@acme.it: Subject of new000", "2. [work] new001@acme.it")
		mustContain(t, out.Summary, "2 in the window, 2 shown", "the model failed")
		if !store.moved {
			t.Fatalf("%s: the window did not move", name)
		}
	}
}

func urgentOf(prefix string, n int) []Verdict {
	out := dayOf(prefix, n, false)
	for i := range out {
		out[i].Tier = TierNow
	}
	return out
}

// A subject that reached the model could talk an urgent mail out of the digest.
func TestTheModelNeverReadsWhatIsOwed(t *testing.T) {
	store := &windowStore{verdicts: slices.Concat(urgentOf("owed", 1), dayOf("new", 1, false))}
	model := &scriptedModel{reply: ModelReply{Text: `{"lines":[]}`}}

	out, err := writtenDigester(store, &everyItem{}, model).Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if strings.Contains(model.asks[0].Items, "owed000") {
		t.Fatalf("the model read it:\n%s", model.asks[0].Items)
	}
	mustContain(t, out.Notice, "Worth a look\n1. owed000@acme.it: Subject of owed000", "1 more asks nothing of you.")
}

func TestADayOfWhatIsOwedAsksNoModel(t *testing.T) {
	store := &windowStore{verdicts: urgentOf("owed", 2)}
	model := &scriptedModel{}

	out, err := writtenDigester(store, &everyItem{}, model).Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if len(model.asks) != 0 {
		t.Fatalf("asked %d times", len(model.asks))
	}
	mustContain(t, out.Notice, "Urgent\n1. [work] owed000@acme.it", "2. [work] owed001@acme.it")
}

func TestNothingNewAsksNoModel(t *testing.T) {
	store := &windowStore{verdicts: dayOf("told", 3, true)}
	model := &scriptedModel{}

	out, err := writtenDigester(store, &everyItem{}, model).Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if len(model.asks) != 0 || out.Notice != "" || !store.moved {
		t.Fatalf("asks %d, notice %q, moved %v", len(model.asks), out.Notice, store.moved)
	}
}

func dayOf(prefix string, n int, told bool) []Verdict {
	out := make([]Verdict, 0, n)
	for i := range n {
		out = append(out, Verdict{
			AccountID: "work", Resource: ResourceMail, ItemID: fmt.Sprintf("%s%03d", prefix, i), Tier: TierToday, Notified: told,
		})
	}
	return out
}

// A day of alerts must not push new mail past what a digest reads, or the digest would be about
// everything but the mail it exists for.
func TestToldLinesDoNotEatTheReadBudget(t *testing.T) {
	store := &windowStore{verdicts: append(dayOf("told", 150, true), dayOf("new", 10, false)...)}
	pim := &everyItem{}
	digester := &Digester{Store: store, PIM: pim, Locale: fakeLocale{LangEN, time.UTC}}

	out, err := digester.Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if pim.read != maxDigestRead {
		t.Fatalf("read %d, want the budget", pim.read)
	}
	if !strings.Contains(out.Notice, "10. [work] new009@acme.it") || strings.Contains(out.Notice, "told") {
		t.Fatalf("digest:\n%s", out.Notice)
	}
	if strings.Contains(out.Notice, "more.") {
		t.Fatalf("told lines were counted:\n%s", out.Notice)
	}
}

func TestNewMailPastTheBudgetIsCounted(t *testing.T) {
	store := &windowStore{verdicts: dayOf("new", maxDigestRead+7, false)}
	digester := &Digester{Store: store, PIM: &everyItem{}, Locale: fakeLocale{LangEN, time.UTC}}

	out, err := digester.Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if want := fmt.Sprintf("And %d more.", maxDigestRead+7-maxDigestLines); !strings.Contains(out.Notice, want) {
		t.Fatalf("missing %q in:\n%s", want, out.Notice)
	}
}

func TestADayOfWhatWasToldSendsNothingAndMovesTheWindow(t *testing.T) {
	store := &windowStore{verdicts: dayOf("told", 3, true)}
	digester := &Digester{Store: store, PIM: &everyItem{}, Locale: fakeLocale{LangEN, time.UTC}}

	out, err := digester.Run(context.Background(), "identity")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if out.Notice != "" || !store.moved {
		t.Fatalf("notice %q, moved %v", out.Notice, store.moved)
	}
}

type staticRoster struct {
	ids []string
	err error
}

func (s staticRoster) IdentityIDs(context.Context) ([]string, error) { return s.ids, s.err }

// partExpirer fails one identity and removes one row from each of the others.
type partExpirer struct {
	failing string
	seen    []string
}

var errExpire = errors.New("expire failed")

func (p *partExpirer) ExpireVerdicts(_ context.Context, identityID string, _ time.Time, _ int) (int, error) {
	p.seen = append(p.seen, identityID)
	if identityID == p.failing {
		return 0, errExpire
	}
	return 1, nil
}

func TestSweepVisitsEveryIdentityAndCarriesOnPastOneThatFails(t *testing.T) {
	expirer := &partExpirer{failing: "b"}
	sweep := Sweep{Store: expirer, Identities: staticRoster{ids: []string{"a", "b", "c"}}}

	removed, err := sweep.SweepExpired(context.Background(), time.Now())
	if removed != 2 || !errors.Is(err, errExpire) || len(expirer.seen) != 3 {
		t.Fatalf("removed %d, err %v, seen %v", removed, err, expirer.seen)
	}
}

func TestSweepReportsARosterThatCannotBeRead(t *testing.T) {
	sweep := Sweep{Identities: staticRoster{err: errExpire}}
	if _, err := sweep.SweepExpired(context.Background(), time.Now()); !errors.Is(err, errExpire) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheDigestCountsTheDiscardedMail(t *testing.T) {
	for lang, want := range map[Lang]string{LangIT: "\nMail scartate: 3.", LangEN: "\nDiscarded mail: 3."} {
		store := &windowStore{verdicts: dayOf("new", 2, false), discarded: 3}
		digester := &Digester{Store: store, PIM: &everyItem{}, Locale: fakeLocale{lang, time.UTC}}

		out, err := digester.Run(context.Background(), "identity")
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if !strings.HasSuffix(out.Notice, want) || !strings.HasSuffix(out.Head, want) {
			t.Fatalf("%s: missing %q at the end of:\n%s\nhead:\n%s", lang, want, out.Notice, out.Head)
		}
	}
}

func TestOnlyDiscardedMailMakesNoDigest(t *testing.T) {
	store := &windowStore{discarded: 4}
	digester := &Digester{Store: store, PIM: &everyItem{}, Locale: fakeLocale{LangEN, time.UTC}}

	out, err := digester.Run(context.Background(), "identity")
	if err != nil || out.Notice != "" || out.Head != "" || !store.moved {
		t.Fatalf("out %+v, err %v, moved %v", out, err, store.moved)
	}
}
