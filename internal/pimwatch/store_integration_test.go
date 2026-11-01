//go:build db_integration

package pimwatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAnotherIdentitySeesNothingOfAWatch(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	other := seedIdentity(t, migratedPool(t))
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.urgentMail("m1")}, NextCursor: "c1"})
	r.tick()
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	rule, err := r.store.PutRule(ctx, r.identity, Rule{Key: "a@b.it", Kind: KeySender, Tier: TierNow})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	if _, err := r.store.Verdict(ctx, other, rigAccount, ResourceMail, "m1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("verdict across identities: %v", err)
	}
	if _, err := r.store.Cursor(ctx, other, rigAccount, ResourceMail); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cursor across identities: %v", err)
	}
	if rules, err := r.store.Rules(ctx, other); err != nil || len(rules) != 0 {
		t.Fatalf("rules across identities: %v %v", rules, err)
	}
	if labels, err := r.store.Labels(ctx, other); err != nil || len(labels) != 0 {
		t.Fatalf("labels across identities: %v %v", labels, err)
	}
	if err := r.store.RemoveRule(ctx, other, rule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing another identity's rule: %v", err)
	}
	if err := r.store.RemoveLabel(ctx, other, label.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing another identity's label: %v", err)
	}
	if _, err := r.store.ApplyCorrection(ctx, other, "", Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", Tier: TierLater}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("correcting another identity's verdict: %v", err)
	}
	if verdict, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceMail, "m1"); err != nil || verdict.Tier != TierNow {
		t.Fatalf("the owner's verdict changed: %+v %v", verdict, err)
	}
}

func TestAConnectionWithNoIdentitySeesNoRowOfAnyTable(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.urgentMail("m1")}, NextCursor: "c1"})
	r.tick()
	if _, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0); err != nil {
		t.Fatalf("label: %v", err)
	}
	if _, err := r.store.PutRule(ctx, r.identity, Rule{Key: "a@b.it", Kind: KeySender, Tier: TierNow}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := r.store.ApplyCorrection(ctx, r.identity, "a@b.it", Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", Tier: TierToday}); err != nil {
		t.Fatalf("correction: %v", err)
	}

	for _, table := range []string{"settings", "labels", "cursors", "verdicts", "rules", "corrections"} {
		var n int
		if err := r.store.pool.QueryRow(ctx, "SELECT count(*) FROM aura.pim_watch_"+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("a connection with no identity sees %d rows of %s", n, table)
		}
	}
}

func TestDeletingAnIdentityTakesItsWatchWithIt(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.urgentMail("m1")}, NextCursor: "c1"})
	r.tick()

	if _, err := r.store.pool.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, r.identity); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	if _, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceMail, "m1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a verdict outlived its identity: %v", err)
	}
}

func TestAReadOfUnknownIDsFindsNothingAndABadIdentityIsRefused(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	if _, err := r.store.Settings(ctx, "not-a-uuid"); err == nil {
		t.Fatalf("a malformed identity was accepted")
	}
	if err := r.store.RemoveRule(ctx, r.identity, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove rule: %v", err)
	}
	if err := r.store.RemoveLabel(ctx, r.identity, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove label: %v", err)
	}
	if _, err := r.store.UpdateLabel(ctx, r.identity, "not-a-uuid", "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update label: %v", err)
	}
	if _, err := r.store.UpdateLabel(ctx, r.identity, uuid.NewString(), "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing label: %v", err)
	}
}

func TestSettingsDefaultAndValidate(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	settings, err := r.store.Settings(ctx, r.identity)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if settings.DigestHour != 18 || settings.DailyAlertCap != 5 || settings.IntervalMinutes != 5 || settings.VerdictRetentionDays != 30 || !settings.LastDigestAt.IsZero() {
		t.Fatalf("defaults = %+v", settings)
	}
	for _, bad := range [][2]int{{-1, 5}, {24, 5}, {18, -1}, {18, 101}} {
		if _, err := r.store.SaveSettings(ctx, r.identity, bad[0], bad[1]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("settings %v: %v", bad, err)
		}
	}
	saved, err := r.store.SaveSettings(ctx, r.identity, 7, 0)
	if err != nil || saved.DigestHour != 7 || saved.DailyAlertCap != 0 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}

func TestTheDefaultLabelsGoInOnceInAnIdentitysLife(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	defaults := DefaultLabels(LangIT)
	if err := r.store.SeedLabels(ctx, r.identity, defaults); err != nil {
		t.Fatalf("seed: %v", err)
	}
	labels, err := r.store.Labels(ctx, r.identity)
	if err != nil || len(labels) != len(defaults) {
		t.Fatalf("labels = %v, %v", labels, err)
	}
	if labels[4].Name != "Pubblicità" || labels[4].Tier != TierLater {
		t.Fatalf("labels[4] = %+v", labels[4])
	}
	for _, label := range labels {
		if err := r.store.RemoveLabel(ctx, r.identity, label.ID); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	if err := r.store.SeedLabels(ctx, r.identity, defaults); err != nil {
		t.Fatalf("seed again: %v", err)
	}
	if labels, err = r.store.Labels(ctx, r.identity); err != nil || len(labels) != 0 {
		t.Fatalf("removed labels came back: %v, %v", labels, err)
	}
}

func TestALabelsNameIsUniqueAndARuleThatOnlyGaveItGoesWithIt(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	if _, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a duplicate name: %v", err)
	}
	updated, err := r.store.UpdateLabel(ctx, r.identity, label.ID, "Clienti", TierToday)
	if err != nil || updated.Name != "Clienti" || updated.Tier != TierToday {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	if _, err := r.store.PutRule(ctx, r.identity, Rule{Key: "only@label.it", Kind: KeySender, LabelID: label.ID}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := r.store.PutRule(ctx, r.identity, Rule{Key: "tier@label.it", Kind: KeySender, Tier: TierNow, LabelID: label.ID}); err != nil {
		t.Fatalf("rule: %v", err)
	}

	if err := r.store.RemoveLabel(ctx, r.identity, label.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	rules, err := r.store.Rules(ctx, r.identity)
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	if len(rules) != 1 || rules[0].Key != "tier@label.it" || rules[0].LabelID != "" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestARuleKeepsItsLastWordAndALearnedOneKeepsTheOtherField(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	if _, err := r.store.PutRule(ctx, r.identity, Rule{Key: "a@b.it", Kind: KeySender, Tier: TierNow}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	replaced, err := r.store.PutRule(ctx, r.identity, Rule{Key: "a@b.it", Kind: KeySender, LabelID: label.ID})
	if err != nil || replaced.Tier != "" || replaced.LabelID != label.ID || replaced.Source != sourcePerson {
		t.Fatalf("replaced = %+v, %v", replaced, err)
	}
	learned, err := r.store.LearnRule(ctx, r.identity, "a@b.it", TierLater, "")
	if err != nil || learned.Tier != TierLater || learned.LabelID != label.ID || learned.Source != sourceCorrection {
		t.Fatalf("learned = %+v, %v", learned, err)
	}
	if learned.Created {
		t.Fatalf("a rule merged into one on file reads as created: %+v", learned)
	}
	fresh, err := r.store.LearnRule(ctx, r.identity, "new@b.it", TierLater, "")
	if err != nil || !fresh.Created {
		t.Fatalf("fresh = %+v, %v", fresh, err)
	}
}

func TestExpiryFollowsTheIdentitysRetentionInBatches(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("old1", TierToday, ""), r.draft("old2", TierToday, ""), r.draft("old3", TierToday, ""))
	later := r.clock.at.Add(31 * 24 * time.Hour)

	removed, err := r.store.ExpireVerdicts(ctx, r.identity, later, 2)
	if err != nil || removed != 2 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	removed, err = r.store.ExpireVerdicts(ctx, r.identity, r.clock.at.Add(time.Hour), 100)
	if err != nil || removed != 0 {
		t.Fatalf("a fresh verdict expired: %d, %v", removed, err)
	}
}

func TestDeleteAccountForgetsThatAccountAlone(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.commit("work", r.draft("w1", TierToday, ""))
	r.commit("home", r.draft("h1", TierToday, ""))
	if _, err := r.store.ApplyCorrection(ctx, r.identity, "a@b.it", Correction{AccountID: "work", Resource: ResourceMail, ItemID: "w1", Tier: TierLater}); err != nil {
		t.Fatalf("correction: %v", err)
	}

	if err := r.store.DeleteAccount(ctx, r.identity, "work"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.store.Verdict(ctx, r.identity, "work", ResourceMail, "w1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("work's verdict stayed: %v", err)
	}
	if _, err := r.store.Cursor(ctx, r.identity, "work", ResourceMail); !errors.Is(err, ErrNotFound) {
		t.Fatalf("work's cursor stayed: %v", err)
	}
	past, err := r.store.CorrectionsBefore(ctx, r.identity, "a@b.it", time.Now().Add(time.Hour))
	if err != nil || len(past) != 0 {
		t.Fatalf("work's corrections stayed: %v, %v", past, err)
	}
	if _, err := r.store.Verdict(ctx, r.identity, "home", ResourceMail, "h1"); err != nil {
		t.Fatalf("home's verdict went: %v", err)
	}
}

func TestCommitPageOnAReadOnlyContextReportsTheFailure(t *testing.T) {
	r := newWatchRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.store.CommitPage(ctx, r.identity, PageCommit{AccountID: rigAccount, Resource: ResourceMail, Cursor: "c"}); err == nil {
		t.Fatalf("a cancelled commit reported success")
	}
	if _, err := r.store.Settings(ctx, r.identity); err == nil {
		t.Fatalf("a cancelled read reported success")
	}
	if err := r.store.MarkUnsupported(ctx, r.identity, rigAccount, ResourceMail); err == nil {
		t.Fatalf("a cancelled write reported success")
	}
}
