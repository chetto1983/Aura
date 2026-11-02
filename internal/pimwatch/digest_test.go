package pimwatch

import (
	"context"
	"errors"
	"fmt"
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

type fakeLocale struct {
	lang Lang
	loc  *time.Location
}

func (f fakeLocale) Language(context.Context, string) Lang { return f.lang }

func (f fakeLocale) Location(context.Context, string) *time.Location { return f.loc }

// windowStore is a day of verdicts with no database behind it.
type windowStore struct {
	verdicts []Verdict
	moved    bool
}

func (s *windowStore) Settings(context.Context, string) (Settings, error) { return Settings{}, nil }
func (s *windowStore) Book(context.Context, string) (Book, error)         { return Book{}, nil }
func (s *windowStore) Labels(context.Context, string) ([]Label, error)    { return nil, nil }
func (s *windowStore) DigestVerdicts(context.Context, string, time.Time, time.Time) ([]Verdict, error) {
	return s.verdicts, nil
}

func (s *windowStore) SetLastDigest(context.Context, string, time.Time) error {
	s.moved = true
	return nil
}

// everyItem reads every ref as a mail and counts what it was asked for.
type everyItem struct {
	read int
}

func (p *everyItem) Changes(context.Context, string, string, string, int) (ChangesPage, error) {
	return ChangesPage{}, nil
}

func (p *everyItem) Items(_ context.Context, _, _ string, refs []ItemRef) (ItemsResult, error) {
	p.read += len(refs)
	items := make([]Item, 0, len(refs))
	for _, ref := range refs {
		items = append(items, Item{ID: ref.ID, From: ref.ID + "@acme.it", Subject: "Subject of " + ref.ID})
	}
	return ItemsResult{Items: items}, nil
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
