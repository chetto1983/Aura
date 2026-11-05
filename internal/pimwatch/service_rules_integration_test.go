//go:build db_integration

package pimwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func correctionOf(id string, tier Tier) Correction {
	return Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: id, Tier: tier}
}

func TestTheSecondCorrectionOutsideTheWindowBecomesARule(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""), r.draft("m2", TierToday, ""), r.draft("m3", TierToday, ""))
	r.pim.items["m1"] = Item{ID: "m1", From: "Mario <Mario@Acme.it>"}
	r.pim.items["m2"] = Item{ID: "m2", From: "mario@acme.it"}
	r.pim.items["m3"] = Item{ID: "m3", From: "MARIO@acme.it"}

	verdict, rule, err := r.service.Correct(ctx, r.identity, correctionOf("m1", TierLater))
	if err != nil || rule != nil {
		t.Fatalf("first: %+v, %v", rule, err)
	}
	if verdict.Tier != TierLater || verdict.By != ByPerson {
		t.Fatalf("verdict = %+v", verdict)
	}

	// The correction rows are stamped by the database, so the window is measured from a clock
	// moved past them.
	r.clock.at = time.Now().UTC().Add(5 * time.Minute)
	if _, rule, err = r.service.Correct(ctx, r.identity, correctionOf("m2", TierLater)); err != nil || rule != nil {
		t.Fatalf("inside the window: %+v, %v", rule, err)
	}

	r.clock.at = time.Now().UTC().Add(correctionWindow + time.Minute)
	if _, rule, err = r.service.Correct(ctx, r.identity, correctionOf("m3", TierLater)); err != nil {
		t.Fatalf("outside the window: %v", err)
	}
	if rule == nil || rule.Key != "mario@acme.it" || rule.Tier != TierLater || rule.Source != sourceCorrection || !rule.Created {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestCorrectionsThatDisagreeTeachNothing(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""), r.draft("m2", TierToday, ""))
	r.pim.items["m2"] = Item{ID: "m2", From: "m1@acme.it"}

	if _, _, err := r.service.Correct(ctx, r.identity, correctionOf("m1", TierLater)); err != nil {
		t.Fatalf("first: %v", err)
	}
	r.clock.at = time.Now().UTC().Add(correctionWindow + time.Minute)
	if _, rule, err := r.service.Correct(ctx, r.identity, correctionOf("m2", TierNow)); err != nil || rule != nil {
		t.Fatalf("second: %+v, %v", rule, err)
	}
}

func TestTierAndLabelAreLearnedApart(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, LabelInput{Name: "Cliente"}, 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	r.commit(rigAccount, r.draft("m1", TierToday, ""), r.draft("m2", TierToday, ""))
	r.pim.items["m2"] = Item{ID: "m2", From: "m1@acme.it"}

	first := Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", LabelID: &label.ID}
	if _, _, err := r.service.Correct(ctx, r.identity, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	r.clock.at = time.Now().UTC().Add(correctionWindow + time.Minute)
	second := Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m2", Tier: TierLater, LabelID: &label.ID}
	_, rule, err := r.service.Correct(ctx, r.identity, second)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if rule == nil || rule.LabelID != label.ID || rule.Tier != "" {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestClearingALabelTeachesNothing(t *testing.T) {
	if tier, label := learned(Correction{LabelID: new("")}, []PastCorrection{{}}); tier != "" || label != "" {
		t.Fatalf("learned %q %q", tier, label)
	}
}

func TestACorrectionOfAGoneMailChangesTheVerdictAndTeachesNothing(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""))
	delete(r.pim.items, "m1")

	verdict, rule, err := r.service.Correct(ctx, r.identity, correctionOf("m1", TierLater))
	if err != nil || rule != nil || verdict.Tier != TierLater {
		t.Fatalf("verdict %+v, rule %+v, %v", verdict, rule, err)
	}
}

// A rule beats the authentication rung, so one learned from a forged mail would let that sender
// reach `now`.
func TestACorrectionOfAnUnverifiedMailChangesTheVerdictAndTeachesNothing(t *testing.T) {
	for name, order := range map[string][2]string{
		"it does not complete a pair": {"real", "forged"},
		"it does not start one":       {"forged", "real"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newServiceRig(t)
			ctx := context.Background()
			forged := r.draft("forged", TierToday, "")
			forged.Decision.AuthFailed = true
			r.commit(rigAccount, r.draft("real", TierToday, ""), forged)
			for _, id := range order {
				r.pim.items[id] = Item{ID: id, From: "mario@acme.it"}
			}

			first, second := correctionOf(order[0], TierNow), correctionOf(order[1], TierNow)
			if _, rule, err := r.service.Correct(ctx, r.identity, first); err != nil || rule != nil {
				t.Fatalf("first: %+v, %v", rule, err)
			}
			r.clock.at = time.Now().UTC().Add(correctionWindow + time.Minute)
			verdict, rule, err := r.service.Correct(ctx, r.identity, second)
			if err != nil || rule != nil {
				t.Fatalf("second: %+v, %v", rule, err)
			}
			if verdict.Tier != TierNow || verdict.By != ByPerson {
				t.Fatalf("verdict = %+v", verdict)
			}
			if rules, err := r.store.Rules(ctx, r.identity); err != nil || len(rules) != 0 {
				t.Fatalf("rules = %+v, %v", rules, err)
			}
		})
	}
}

func TestACorrectionOfAnEventTeachesNothing(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{Changes: []Change{
		eventChange("ev1", "f1", r.clock.at.Add(time.Hour)),
	}, NextCursor: "c1"})
	r.tick()
	r.pim.items["ev1"] = Item{ID: "ev1", From: "a@b.it"}

	c := Correction{AccountID: rigAccount, Resource: ResourceCalendar, ItemID: "ev1", Tier: TierLater}
	if _, rule, err := r.service.Correct(ctx, r.identity, c); err != nil || rule != nil {
		t.Fatalf("rule %+v, %v", rule, err)
	}
}

func TestEveryRefusalNamesItsCode(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""))
	if _, err := r.service.AddLabel(ctx, r.identity, "Cliente", ""); err != nil {
		t.Fatalf("label: %v", err)
	}
	ghost := "00000000-0000-0000-0000-000000000000"
	refused := map[string]func() error{
		"account_required": func() error { return r.service.Enable(ctx, r.identity, " ") },
		"invalid_day": func() error {
			_, err := r.service.Decisions(ctx, r.identity, rigAccount, "01/11/2026")
			return err
		},
		"empty_correction": func() error {
			_, _, err := r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1"})
			return err
		},
		"invalid_tier": func() error {
			_, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "a@b.it", Tier: "soon"})
			return err
		},
		"unknown_label": func() error {
			_, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "a@b.it", LabelID: ghost})
			return err
		},
		"empty_rule": func() error {
			_, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "a@b.it"})
			return err
		},
		"invalid_address": func() error {
			_, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "x@", Tier: TierNow})
			return err
		},
		"invalid_rule_key": func() error {
			_, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "nodot", Tier: TierNow})
			return err
		},
		"invalid_label_name": func() error {
			_, err := r.service.AddLabel(ctx, r.identity, " ", "")
			return err
		},
		"label_exists": func() error {
			_, err := r.service.AddLabel(ctx, r.identity, "Cliente", "")
			return err
		},
		"invalid_digest_hour": func() error {
			_, err := r.service.SaveSettings(ctx, r.identity, 24, 5)
			return err
		},
		"invalid_alert_cap": func() error {
			_, err := r.service.SaveSettings(ctx, r.identity, 18, 101)
			return err
		},
	}
	for code, call := range refused {
		t.Run(code, func(t *testing.T) {
			err := call()
			refusal, ok := errors.AsType[*InvalidError](err)
			if !ok || refusal.Code != code || refusal.Message == "" {
				t.Fatalf("err = %v", err)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%v is not ErrInvalid", err)
			}
		})
	}
}

func TestABadCorrectionIsRefused(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.commit(rigAccount, r.draft("m1", TierToday, ""))
	missing := "00000000-0000-0000-0000-000000000000"
	for _, c := range []Correction{
		{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1"},
		correctionOf("m1", "soon"),
		{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", LabelID: &missing},
	} {
		if _, _, err := r.service.Correct(ctx, r.identity, c); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	if _, _, err := r.service.Correct(ctx, r.identity, correctionOf("nope", TierLater)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing verdict: %v", err)
	}
}

func TestARuleIsAnAddressOrADomain(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for raw, want := range map[string]Rule{
		" Mario@Acme.IT ": {Key: "mario@acme.it", Kind: KeySender},
		"@Acme.it":        {Key: "acme.it", Kind: KeyDomain},
		"newsletter.it":   {Key: "newsletter.it", Kind: KeyDomain},
	} {
		rule, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: raw, Tier: TierLater})
		if err != nil || rule.Key != want.Key || rule.Kind != want.Kind {
			t.Fatalf("%q: %+v, %v", raw, rule, err)
		}
	}
	missing := "00000000-0000-0000-0000-000000000000"
	for _, bad := range []RuleInput{
		{Key: "a b@c", Tier: TierLater}, {Key: "nodot", Tier: TierLater}, {Key: "two words.it", Tier: TierLater},
		{Key: "a@b.it"}, {Key: "a@b.it", Tier: "soon"}, {Key: "a@b.it", LabelID: missing},
	} {
		if _, err := r.service.AddRule(ctx, r.identity, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

func TestSavingSettingsMovesTheDigest(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := r.service.SaveSettings(ctx, r.identity, 7, 9); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := r.tasks.digest[r.identity+"|digest"].Hour; got != 7 {
		t.Fatalf("hour = %d", got)
	}

	key := r.identity + "|digest"
	task := r.tasks.digests[key]
	task.Paused = true
	r.tasks.digests[key] = task
	if _, err := r.service.SaveSettings(ctx, r.identity, 9, 9); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := r.tasks.digest[key].Hour; got != 7 {
		t.Fatalf("a paused digest moved to %d", got)
	}
	if _, err := r.service.SaveSettings(ctx, r.identity, 99, 9); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a bad hour: %v", err)
	}
}

func TestLabelsAreManagedThroughTheService(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	first, err := r.service.AddLabel(ctx, r.identity, " Cliente ", "")
	if err != nil || first.Name != "Cliente" || first.Position != 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := r.service.AddLabel(ctx, r.identity, "Lavoro", TierToday)
	if err != nil || second.Position != 1 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if id, err := r.service.LabelID(ctx, r.identity, "cliente"); err != nil || id != first.ID {
		t.Fatalf("by name: %q, %v", id, err)
	}
	if _, err := r.service.LabelID(ctx, r.identity, "nobody"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown name: %v", err)
	}
	if updated, err := r.service.UpdateLabel(ctx, r.identity, first.ID, "Clienti", TierNow); err != nil || updated.Tier != TierNow {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	for _, bad := range []struct {
		name string
		tier Tier
	}{{"", ""}, {strings.Repeat("x", 61), ""}, {"ok", "soon"}} {
		if _, err := r.service.AddLabel(ctx, r.identity, bad.name, bad.tier); !errors.Is(err, ErrInvalid) {
			t.Fatalf("add %+v: %v", bad, err)
		}
		if _, err := r.service.UpdateLabel(ctx, r.identity, first.ID, bad.name, bad.tier); !errors.Is(err, ErrInvalid) {
			t.Fatalf("update %+v: %v", bad, err)
		}
	}
}

func TestPortFailuresReachTheCaller(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.tasks.err = errBroken
	if err := r.service.Enable(ctx, r.identity, "home"); !errors.Is(err, errBroken) {
		t.Fatalf("enable: %v", err)
	}
	if err := r.service.Disable(ctx, r.identity, "home"); !errors.Is(err, errBroken) {
		t.Fatalf("disable: %v", err)
	}
	if _, err := r.service.Status(ctx, r.identity); !errors.Is(err, errBroken) {
		t.Fatalf("status: %v", err)
	}
	r.tasks.err = nil
	r.service.Routes = fakeRoutes{err: errBroken}
	if err := r.service.Enable(ctx, r.identity, "home"); !errors.Is(err, errBroken) {
		t.Fatalf("enable with no route: %v", err)
	}
}

// The cockpit offers Undo for a saved rule only when the server says the key had none: a rule
// that replaced one holds the person's earlier words, and the cockpit's cache of the rules may be
// empty or old.
func TestARuleSavedOverOneOnFileIsNotCreated(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	first, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "a@b.it", Tier: TierNow})
	if err != nil || !first.Created {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "a@b.it", Tier: TierLater})
	if err != nil || second.Created || second.Tier != TierLater {
		t.Fatalf("second = %+v, %v", second, err)
	}
}
