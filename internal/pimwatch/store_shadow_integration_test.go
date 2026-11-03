//go:build db_integration

package pimwatch

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestAMailRowKeepsTheLaddersAnswerAfterACorrection(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.commit(rigAccount, VerdictDraft{
		ItemID:   "m1",
		Decision: Decision{Tier: TierToday, By: ByPeople},
		Shadow: &Shadow{
			Facts: MailFacts{WrittenTo: true, Bulk: true}, SenderMemory: TierToday, Centroid: TierNow, Margin: 0.04,
		},
	})
	verdict, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceMail, "m1")
	if err != nil {
		t.Fatal(err)
	}
	want := Verdict{
		LadderTier: TierToday, LadderBy: ByPeople, Centroid: TierNow, SenderMemory: TierToday,
		Facts: MailFacts{WrittenTo: true, Bulk: true},
	}
	if verdict.LadderTier != want.LadderTier || verdict.LadderBy != want.LadderBy || verdict.Centroid != want.Centroid ||
		verdict.SenderMemory != want.SenderMemory || verdict.Facts != want.Facts {
		t.Fatalf("written row = %+v", verdict)
	}

	corrected, err := r.store.ApplyCorrection(ctx, r.identity, "m1@one.example", Correction{
		AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", Tier: TierLater,
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Tier != TierLater || corrected.By != ByPerson || corrected.LadderTier != TierToday || corrected.LadderBy != ByPeople {
		t.Fatalf("corrected row = %+v", corrected)
	}
}

func TestAnEventRowHasNoLadderAnswer(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	_, err := r.store.CommitPage(ctx, r.identity, PageCommit{
		AccountID: rigAccount, Resource: ResourceCalendar, Cursor: "c-event", Now: r.clock.at, DayStart: r.clock.at,
		Drafts: []VerdictDraft{{ItemID: "e1", Decision: Decision{Tier: TierToday, By: ByCalendar}, Event: true, Fingerprint: "f"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	verdict, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.LadderTier != "" || verdict.Centroid != "" || verdict.SenderMemory != "" {
		t.Fatalf("event row = %+v", verdict)
	}
}

func TestSenderMemoryIsTheLastCorrectedTier(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""), r.draft("m2", TierToday, ""), r.draft("m3", TierToday, ""))
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		sender string
		item   string
		tier   Tier
		label  *string
	}{
		{"a@one.example", "m1", TierToday, nil},
		{"a@one.example", "m2", TierNow, nil},
		{"b@one.example", "m3", "", &label.ID},
	} {
		if _, err := r.store.ApplyCorrection(ctx, r.identity, c.sender, Correction{
			AccountID: rigAccount, Resource: ResourceMail, ItemID: c.item, Tier: c.tier, LabelID: c.label,
		}); err != nil {
			t.Fatal(err)
		}
	}
	memory, err := r.store.SenderMemory(ctx, r.identity, []string{"a@one.example", "b@one.example", "c@one.example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(memory) != 1 || memory["a@one.example"] != TierNow {
		t.Fatalf("memory = %v, want a@one.example: now alone", memory)
	}
}

func TestCorrectedAccountsListsTheAccountsWithCorrections(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""))
	r.commit("home", r.draft("m2", TierToday, ""))
	for _, account := range []struct{ id, item string }{{rigAccount, "m1"}, {"home", "m2"}} {
		if _, err := r.store.ApplyCorrection(ctx, r.identity, "a@one.example", Correction{
			AccountID: account.id, Resource: ResourceMail, ItemID: account.item, Tier: TierNow,
		}); err != nil {
			t.Fatal(err)
		}
	}
	accounts, err := r.store.CorrectedAccounts(ctx, r.identity)
	slices.Sort(accounts)
	if err != nil || !slices.Equal(accounts, []string{"home", rigAccount}) {
		t.Fatalf("accounts = %v, %v", accounts, err)
	}
	if err := r.store.DeleteAccount(ctx, r.identity, "home"); err != nil {
		t.Fatal(err)
	}
	accounts, err = r.store.CorrectedAccounts(ctx, r.identity)
	if err != nil || !slices.Equal(accounts, []string{rigAccount}) {
		t.Fatalf("after the delete = %v, %v", accounts, err)
	}
}

func TestLastDigestReadsWithoutWriting(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	store := NewStore(pool)
	identity := seedIdentity(t, pool)
	if at, err := store.LastDigest(ctx, identity); err != nil || !at.IsZero() {
		t.Fatalf("an identity with no watch: %v, %v", at, err)
	}
	if err := store.tx(ctx, identity, func(q *sqlc.Queries, id pgtype.UUID) error {
		_, err := q.GetPIMWatchSettings(ctx, id)
		return err
	}); err == nil {
		t.Fatal("reading the last digest wrote a settings row")
	}
	if _, err := store.Settings(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if at, err := store.LastDigest(ctx, identity); err != nil || !at.IsZero() {
		t.Fatalf("a watch with no digest sent: %v, %v", at, err)
	}
	sent := time.Date(2026, 11, 2, 18, 0, 0, 0, time.UTC)
	if err := store.SetLastDigest(ctx, identity, sent); err != nil {
		t.Fatal(err)
	}
	if at, err := store.LastDigest(ctx, identity); err != nil || !at.Equal(sent) {
		t.Fatalf("after a digest: %v, %v", at, err)
	}
}

func TestShadowRowsReadOnlyMailWithALadderAnswer(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	landed := VerdictDraft{
		ItemID: "landed", Decision: Decision{Tier: TierToday, By: ByDefault},
		Shadow: &Shadow{SenderMemory: TierToday, Centroid: TierLater, Margin: 0.02},
	}
	r.commit(rigAccount, landed)
	// A row written before 0152 has no ladder answer.
	if err := r.store.tx(ctx, r.identity, func(q *sqlc.Queries, id pgtype.UUID) error {
		_, err := q.InsertPIMWatchVerdict(ctx, sqlc.InsertPIMWatchVerdictParams{
			IdentityID: id, AccountID: rigAccount, Resource: ResourceMail, ItemID: "before-landing",
			Tier: string(TierToday), DecidedBy: string(ByDefault), DecidedAt: timestamp(r.clock.at),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.CommitPage(ctx, r.identity, PageCommit{
		AccountID: rigAccount, Resource: ResourceCalendar, Cursor: "c-event", Now: r.clock.at, DayStart: r.clock.at,
		Drafts: []VerdictDraft{{ItemID: "e1", Decision: Decision{Tier: TierToday, By: ByCalendar}, Event: true, Fingerprint: "f"}},
	}); err != nil {
		t.Fatal(err)
	}
	r.clock.advance(-48 * time.Hour)
	old := VerdictDraft{ItemID: "old", Decision: Decision{Tier: TierNow, By: ByPeople}, Shadow: &Shadow{SenderMemory: TierNow}}
	r.commit(rigAccount, old)

	rows, err := r.store.ShadowRows(ctx, r.identity, r.clock.at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want the landed mail alone", rows)
	}
	got := rows[0]
	if got.Tier != TierToday || got.LadderTier != TierToday || got.By != ByDefault || got.LadderBy != ByDefault ||
		got.Centroid != TierLater || got.SenderMemory != TierToday || got.Margin != 0.02 || got.DecidedAt.IsZero() || got.Notified {
		t.Fatalf("row = %+v", got)
	}
}
