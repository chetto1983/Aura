//go:build db_integration

package pimwatch

import (
	"context"
	"strings"
	"testing"
	"time"
)

func (r *watchRig) digester(lang Lang) *Digester {
	return &Digester{Store: r.store, PIM: r.pim, Locale: fakeLocale{lang, time.UTC}, Now: r.clock.now}
}

func TestTheDigestListsNowFirstThenLabelsAndCountsAsAWholeDay(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("t1", TierToday, ""), r.draft("t2", TierToday, label.ID), r.draft("n1", TierNow, ""), r.draft("skip", TierLater, ""))
	r.clock.advance(time.Minute)

	out, err := r.digester(LangIT).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	want := strings.Join([]string{
		"Riepilogo di posta e calendario", "Urgenti", "1. [work] n1@acme.it: Subject of n1",
		"Cliente", "2. [work] t2@acme.it: Subject of t2", "Da smistare", "3. [work] t1@acme.it: Subject of t1",
	}, "\n")
	if out.Notice != want {
		t.Fatalf("digest:\n%s\nwant:\n%s", out.Notice, want)
	}
	mustContain(t, out.Summary, "3 in the window, 3 shown")
}

func TestAnUnreadableAccountIsNamedAndTheWindowStays(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.clock.advance(time.Minute)
	r.commit("work", r.draft("w1", TierToday, ""))
	r.commit("home", r.draft("h1", TierToday, ""))
	r.clock.advance(time.Minute)
	r.pim.itemsErr["home"] = transient()

	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "w1@acme.it", "I could not read: home.")
	mustContain(t, out.Summary, "unreadable: home")

	delete(r.pim.itemsErr, "home")
	r.clock.advance(time.Minute)
	out, err = r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	mustContain(t, out.Notice, "h1@acme.it", "w1@acme.it")
}

func TestTheWindowMovesAfterAGoodDigestAndHoldsWhatCameAfter(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("before", TierToday, ""))
	r.clock.advance(time.Minute)
	if _, err := r.digester(LangEN).Run(ctx, r.identity); err != nil {
		t.Fatalf("digest: %v", err)
	}

	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("after", TierToday, ""))
	r.clock.advance(time.Minute)
	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "after@acme.it")
	if strings.Contains(out.Notice, "before@acme.it") {
		t.Fatalf("the window did not move:\n%s", out.Notice)
	}
}

func TestAnEmptyWindowSendsNothingAndStillMoves(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if out.Notice != "" || out.Head != "" {
		t.Fatalf("an empty digest spoke: %+v", out)
	}
	settings, err := r.store.Settings(ctx, r.identity)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !settings.LastDigestAt.Equal(r.clock.at.Truncate(time.Microsecond)) {
		t.Fatalf("window at %v, want %v", settings.LastDigestAt, r.clock.at)
	}
}

func TestADigestIsCappedAtTwentyLinesAndReadsNoMore(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	drafts := make([]VerdictDraft, 0, 30)
	for _, change := range changesOf(r.todayMail, "d", 30) {
		drafts = append(drafts, VerdictDraft{ItemID: change.ID, Decision: Decision{Tier: TierToday, By: ByDefault}})
	}
	r.clock.advance(time.Minute)
	r.commit(rigAccount, drafts...)
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "20. [work]", "And 10 more.")
	if strings.Contains(out.Notice, "21. [work]") {
		t.Fatalf("past the cap:\n%s", out.Notice)
	}
	read := 0
	for _, refs := range r.pim.reads {
		read += len(refs)
	}
	if read != 20 {
		t.Fatalf("read %d items for a digest of 20", read)
	}
}

func TestADigestShowsAGoneMailAsGone(t *testing.T) {
	r := newWatchRig(t)
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("kept", TierToday, ""), r.draft("gone", TierToday, ""))
	delete(r.pim.items, "gone")
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(context.Background(), r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "kept@acme.it", "(no longer there)")
}

func TestARemovedLabelsMailsReadAsUnsorted(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Ex", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("m1", TierToday, label.ID))
	if err := r.store.RemoveLabel(ctx, r.identity, label.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "To sort\n1. [work] m1@acme.it")
}
