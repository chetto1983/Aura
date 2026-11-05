//go:build db_integration

package pimwatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestACorrectionDiscardsAndRescues(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	spam, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Spam", Discard: true})
	if err != nil {
		t.Fatal(err)
	}
	client, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Cliente"})
	if err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, r.draft("m1", TierNow, ""), r.draft("m2", TierToday, spam.ID))
	r.pim.items["m1"] = Item{ID: "m1", From: "m1@one.example"}

	label := correctionOf("m1", "")
	label.LabelID = &spam.ID
	verdict, _, err := r.service.Correct(ctx, r.identity, label)
	if err != nil || verdict.Tier != TierLater || !verdict.Discarded || verdict.By != ByPerson {
		t.Fatalf("a discard label: %+v, %v", verdict, err)
	}

	verdict, _, err = r.service.Correct(ctx, r.identity, correctionOf("m1", TierToday))
	if err != nil || verdict.Tier != TierToday || verdict.Discarded {
		t.Fatalf("a tier after it: %+v, %v", verdict, err)
	}

	r.pim.items["m2"] = Item{ID: "m2", From: "m2@one.example"}
	if _, _, err = r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m2", LabelID: &spam.ID}); err != nil {
		t.Fatal(err)
	}
	rescue := Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m2", LabelID: &client.ID}
	verdict, _, err = r.service.Correct(ctx, r.identity, rescue)
	if err != nil || verdict.Tier != TierLater || verdict.Discarded || verdict.LabelID != client.ID {
		t.Fatalf("a keep label: %+v, %v", verdict, err)
	}
}

func TestACorrectionFindsAnUppercaseLabelId(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	spam, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Spam", Discard: true})
	if err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, r.draft("m1", TierToday, ""))
	upper := strings.ToUpper(spam.ID)
	verdict, _, err := r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", LabelID: &upper})
	if err != nil || !verdict.Discarded || verdict.Tier != TierLater {
		t.Fatalf("an uppercase id: %+v, %v", verdict, err)
	}
}

func TestDiscardedMailDoesNotTakeTheDaysRows(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	drafts := []VerdictDraft{r.draft("kept", TierToday, "")}
	for i := range decisionsLimit {
		draft := r.draft(fmt.Sprintf("junk%03d", i), TierLater, "")
		draft.Decision.Discarded = true
		drafts = append(drafts, draft)
	}
	r.commit(rigAccount, drafts...)

	day, err := r.service.Decisions(ctx, r.identity, rigAccount, "", false)
	if err != nil || len(day.Lines) != 1 || day.Discarded != decisionsLimit || day.Lines[0].ItemID != "kept" {
		t.Fatalf("kept: %d lines, %d discarded, %v", len(day.Lines), day.Discarded, err)
	}
	day, err = r.service.Decisions(ctx, r.identity, rigAccount, "", true)
	if err != nil || len(day.Lines) != decisionsLimit || !day.Lines[0].Discarded {
		t.Fatalf("discarded: %d lines, %v", len(day.Lines), err)
	}
}

func TestTheDigestWindowListsTheKeptMailAndCountsTheDiscarded(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	junk := r.draft("junk", TierLater, "")
	junk.Decision.Discarded = true
	rest := r.draft("rest", TierToday, "")
	hidden := r.draft("hidden", TierToday, "")
	hidden.Decision.Discarded = true
	r.commit(rigAccount, junk, rest, hidden)
	since, until := r.clock.at.Add(-time.Hour), r.clock.at.Add(time.Hour)

	listed, err := r.store.DigestVerdicts(ctx, r.identity, since, until)
	if err != nil || len(listed) != 1 || listed[0].ItemID != "rest" {
		t.Fatalf("listed %+v, %v", listed, err)
	}
	if n, err := r.store.DiscardedInWindow(ctx, r.identity, since, until); err != nil || n != 2 {
		t.Fatalf("window: %d, %v", n, err)
	}
	if n, err := r.store.DiscardedOfDay(ctx, r.identity, rigAccount, since, until); err != nil || n != 2 {
		t.Fatalf("day: %d, %v", n, err)
	}
}
