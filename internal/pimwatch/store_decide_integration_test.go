//go:build db_integration

package pimwatch

import (
	"context"
	"strings"
	"testing"
	"time"
)

const decideModel = "liquid/d1.1-20261027"

func (r *watchRig) mailRow(id string) Verdict {
	r.t.Helper()
	verdict, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceMail, id)
	if err != nil {
		r.t.Fatalf("read verdict %s: %v", id, err)
	}
	return verdict
}

func TestAMailRowKeepsTheModelsAnswerAfterACorrection(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, LabelInput{Name: "Pubblicità", Tier: TierLater, Discard: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, VerdictDraft{
		ItemID:   "m1",
		Ladder:   Decision{Tier: TierToday, By: ByDefault},
		Decision: Decision{Tier: TierLater, LabelID: label.ID, By: ByModel, Discarded: true},
		Model:    &ModelAnswer{LabelID: label.ID, Probability: 0.82, Name: decideModel, Options: 6, Applied: true},
	})
	row := r.mailRow("m1")
	if row.Tier != TierLater || row.By != ByModel || row.LadderTier != TierToday || row.LadderBy != ByDefault ||
		row.LabelID != label.ID || !row.Discarded {
		t.Fatalf("decision columns = %+v", row)
	}
	if !row.ModelAsked || row.ModelLabelID != label.ID || row.ModelProbability != 0.82 || row.ModelName != decideModel ||
		row.ModelTier != TierLater || !row.ModelDiscarded || row.ModelOptions != 6 || row.ModelSkipped {
		t.Fatalf("model columns = %+v", row)
	}

	corrected, err := r.store.ApplyCorrection(ctx, r.identity, "m1@one.example", Correction{
		AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", Tier: TierToday,
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.By != ByPerson || corrected.Tier != TierToday {
		t.Fatalf("corrected row = %+v", corrected)
	}
	if !corrected.ModelAsked || corrected.ModelLabelID != label.ID || corrected.ModelProbability != 0.82 ||
		corrected.ModelName != decideModel || corrected.ModelTier != TierLater || !corrected.ModelDiscarded ||
		corrected.ModelOptions != 6 {
		t.Fatalf("a correction changed the model's answer: %+v", corrected)
	}
}

func TestADraftWithoutALadderKeepsItsLadderColumns(t *testing.T) {
	r := newWatchRig(t)
	r.commit(rigAccount, VerdictDraft{ItemID: "m1", Decision: Decision{Tier: TierToday, By: ByPeople}})
	row := r.mailRow("m1")
	if row.LadderTier != TierToday || row.LadderBy != ByPeople {
		t.Fatalf("ladder columns = %q, %q", row.LadderTier, row.LadderBy)
	}
	if row.ModelAsked || row.ModelSkipped || row.Discarded {
		t.Fatalf("a mail the model never saw reads %+v", row)
	}
}

func TestAModelThatChoseNoneIsStoredAsAsked(t *testing.T) {
	r := newWatchRig(t)
	r.commit(rigAccount, VerdictDraft{
		ItemID:   "m1",
		Ladder:   Decision{Tier: TierToday, By: ByDefault},
		Decision: Decision{Tier: TierToday, By: ByDefault},
		Model:    &ModelAnswer{Probability: 0.7, Options: 6},
	})
	row := r.mailRow("m1")
	if !row.ModelAsked || row.ModelLabelID != "" || row.ModelTier != "" || row.ModelProbability != 0.7 || row.ModelOptions != 6 {
		t.Fatalf("row = %+v", row)
	}
	if row.By != ByDefault || row.Tier != TierToday {
		t.Fatalf("a model that chose none changed the decision: %+v", row)
	}
}

func TestASkippedMailIsMarked(t *testing.T) {
	r := newWatchRig(t)
	r.commit(rigAccount, VerdictDraft{
		ItemID: "m1", Decision: Decision{Tier: TierToday, By: ByDefault}, ModelSkipped: true,
	})
	row := r.mailRow("m1")
	if !row.ModelSkipped || row.ModelAsked {
		t.Fatalf("row = %+v", row)
	}
}

func TestALabelDeletedBeforeTheCommitIsNulled(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, LabelInput{Name: "Cliente"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	draft := VerdictDraft{
		ItemID:   "m1",
		Ladder:   Decision{Tier: TierToday, By: ByDefault},
		Decision: Decision{Tier: TierToday, LabelID: label.ID, By: ByDefault},
		Model:    &ModelAnswer{LabelID: label.ID, Probability: 0.9, Options: 6},
	}
	if err := r.store.RemoveLabel(ctx, r.identity, label.ID); err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, draft)
	row := r.mailRow("m1")
	if row.LabelID != "" || row.ModelLabelID != "" || !row.ModelAsked || row.ModelProbability != 0.9 {
		t.Fatalf("row = %+v", row)
	}
}

func TestLabelsKeepTheirDescriptionAndDiscard(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, LabelInput{
		Name: "Spam", Tier: TierLater, Description: "Offers sent to a list.", Discard: true,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if label.Description != "Offers sent to a list." || !label.Discard {
		t.Fatalf("added label = %+v", label)
	}
	updated, err := r.store.PatchLabel(ctx, r.identity, label.ID, overwrite(LabelInput{Name: "Spam", Description: "Promotions.", Discard: false}))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != "Promotions." || updated.Discard || updated.Tier != "" {
		t.Fatalf("updated label = %+v", updated)
	}
	if _, err := r.store.AddLabel(ctx, r.identity, LabelInput{Name: "Long", Description: strings.Repeat("a", 301)}, 1); err == nil {
		t.Fatal("a description of 301 characters was stored")
	}
	labels, err := r.store.Labels(ctx, r.identity)
	if err != nil || len(labels) != 1 || labels[0] != updated {
		t.Fatalf("Labels = %+v, %v", labels, err)
	}
	book, err := r.store.Book(ctx, r.identity)
	if err != nil || book.labels[label.ID] != updated {
		t.Fatalf("Book label = %+v, %v", book.labels[label.ID], err)
	}
}

func TestDescriptionsAreSeededOnceAndOnlyIntoEmptyDefaults(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	for i, in := range []LabelInput{{Name: "Cliente"}, {Name: "Lavoro", Description: "mia"}, {Name: "Fornitori"}, {Name: "Work"}} {
		if _, err := r.store.AddLabel(ctx, r.identity, in, i); err != nil {
			t.Fatal(err)
		}
	}
	defaults := append(DefaultLabels(LangIT), DefaultLabels(LangEN)...)
	seeded, err := r.store.SeedLabelDescriptions(ctx, r.identity, defaults)
	if err != nil || !seeded {
		t.Fatalf("first seed = %v, %v", seeded, err)
	}
	described := func() map[string]string {
		labels, err := r.store.Labels(ctx, r.identity)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, l := range labels {
			out[l.Name] = l.Description
		}
		return out
	}
	got := described()
	if got["Cliente"] != DefaultLabels(LangIT)[0].Description || got["Work"] != DefaultLabels(LangEN)[1].Description ||
		got["Lavoro"] != "mia" || got["Fornitori"] != "" {
		t.Fatalf("descriptions = %+v", got)
	}

	labels, _ := r.store.Labels(ctx, r.identity)
	if _, err := r.store.PatchLabel(ctx, r.identity, labels[0].ID, overwrite(LabelInput{Name: "Cliente"})); err != nil {
		t.Fatal(err)
	}
	seeded, err = r.store.SeedLabelDescriptions(ctx, r.identity, defaults)
	if err != nil || seeded {
		t.Fatalf("second seed = %v, %v", seeded, err)
	}
	if described()["Cliente"] != "" {
		t.Fatal("a second seed refilled a description the person cleared")
	}
}

// overwrite is a patch that sets every field, the way a full rewrite of a label does.
func overwrite(in LabelInput) LabelPatch {
	return LabelPatch{Name: &in.Name, Tier: &in.Tier, Description: &in.Description, Discard: &in.Discard}
}

func TestAFailureKeepsItsFirstTimeUntilAnAnswerClearsIt(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	record := func(class string) AccountSettings {
		t.Helper()
		if err := r.store.RecordDecideFailure(ctx, r.identity, rigAccount, class); err != nil {
			t.Fatal(err)
		}
		return r.accountSettings()
	}
	first := record("unreachable")
	time.Sleep(10 * time.Millisecond)
	second := record("loading")
	if second.DecideError != "loading" || first.DecideFailedAt.IsZero() || !second.DecideFailedAt.Equal(first.DecideFailedAt) {
		t.Fatalf("first %+v, second %+v: want the second class since the first time", first, second)
	}
	if cleared := record(""); cleared.DecideError != "" || !cleared.DecideFailedAt.IsZero() {
		t.Fatalf("cleared = %+v", cleared)
	}
	time.Sleep(10 * time.Millisecond)
	if again := record("upstream"); !again.DecideFailedAt.After(first.DecideFailedAt) {
		t.Fatalf("a failure after the clear reads the old time: %+v", again)
	}
}
