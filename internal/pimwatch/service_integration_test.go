//go:build db_integration

package pimwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTasks is the scheduler in memory.
type fakeTasks struct {
	watches map[string]WatchTask
	digests map[string]WatchTask
	specs   map[string]WatchSpec
	digest  map[string]DigestSpec
	created int
	err     error
}

func newFakeTasks() *fakeTasks {
	return &fakeTasks{
		watches: map[string]WatchTask{}, digests: map[string]WatchTask{},
		specs: map[string]WatchSpec{}, digest: map[string]DigestSpec{},
	}
}

func (f *fakeTasks) list(from map[string]WatchTask, identityID string) []WatchTask {
	var out []WatchTask
	for key, task := range from {
		if strings.HasPrefix(key, identityID+"|") {
			out = append(out, task)
		}
	}
	return out
}

func (f *fakeTasks) Watches(_ context.Context, identityID string) ([]WatchTask, error) {
	return f.list(f.watches, identityID), f.err
}

func (f *fakeTasks) Digests(_ context.Context, identityID string) ([]WatchTask, error) {
	return f.list(f.digests, identityID), f.err
}

func (f *fakeTasks) CreateWatch(_ context.Context, identityID string, spec WatchSpec) error {
	f.created++
	key := identityID + "|" + spec.AccountID
	f.watches[key] = WatchTask{ID: key, AccountID: spec.AccountID}
	f.specs[key] = spec
	return nil
}

func (f *fakeTasks) CreateDigest(_ context.Context, identityID string, spec DigestSpec) error {
	f.created++
	key := identityID + "|digest"
	f.digests[key] = WatchTask{ID: key}
	f.digest[key] = spec
	return nil
}

func (f *fakeTasks) RescheduleDigest(_ context.Context, taskID string, spec DigestSpec) error {
	f.digest[taskID] = spec
	return nil
}

func (f *fakeTasks) Resume(_ context.Context, taskID string) error {
	task := f.watches[taskID]
	task.Paused = false
	f.watches[taskID] = task
	return nil
}

func (f *fakeTasks) Cancel(_ context.Context, taskID string) error {
	delete(f.watches, taskID)
	delete(f.digests, taskID)
	return nil
}

type fakeRoutes struct {
	delivery Delivery
	err      error
}

func (f fakeRoutes) DeliveryFor(context.Context, string) (Delivery, error) { return f.delivery, f.err }

type serviceRig struct {
	*watchRig
	tasks   *fakeTasks
	service *Service
}

func newServiceRig(t *testing.T) *serviceRig {
	t.Helper()
	r := newWatchRig(t)
	tasks := newFakeTasks()
	return &serviceRig{watchRig: r, tasks: tasks, service: &Service{
		Store: r.store, PIM: r.pim, Tasks: tasks, Routes: fakeRoutes{delivery: Delivery{Route: "stdout"}},
		Locale: fakeLocale{LangIT, time.UTC}, Now: r.clock.now,
	}}
}

func TestEnablingCreatesTheWatchAndTheOneDigestAndIsSafeToRepeat(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for range 2 {
		if err := r.service.Enable(ctx, r.identity, " home "); err != nil {
			t.Fatalf("enable: %v", err)
		}
	}
	if r.tasks.created != 2 {
		t.Fatalf("%d tasks created", r.tasks.created)
	}
	spec := r.tasks.specs[r.identity+"|home"]
	if spec.EveryMinutes != 5 || spec.Delivery.Route != "stdout" || !strings.Contains(spec.Title, "home") {
		t.Fatalf("watch spec = %+v", spec)
	}
	digest := r.tasks.digest[r.identity+"|digest"]
	if digest.Hour != 18 || digest.Zone != "UTC" || digest.Title == "" {
		t.Fatalf("digest spec = %+v", digest)
	}
	if got, err := r.store.Cursor(ctx, r.identity, "home", ResourceMail); err != nil || got.Value != "start-mail" {
		t.Fatalf("cursor = %+v, %v", got, err)
	}
	labels, err := r.store.Labels(ctx, r.identity)
	if err != nil || len(labels) != 5 || labels[0].Name != "Cliente" {
		t.Fatalf("labels = %+v, %v", labels, err)
	}
}

func TestASecondAccountSharesTheDigest(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for _, account := range []string{"home", "work"} {
		if err := r.service.Enable(ctx, r.identity, account); err != nil {
			t.Fatalf("enable: %v", err)
		}
	}
	if len(r.tasks.digests) != 1 || len(r.tasks.watches) != 2 {
		t.Fatalf("%d digests, %d watches", len(r.tasks.digests), len(r.tasks.watches))
	}
}

func TestEnablingResumesAPausedWatch(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	key := r.identity + "|home"
	task := r.tasks.watches[key]
	task.Paused = true
	r.tasks.watches[key] = task

	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if r.tasks.watches[key].Paused {
		t.Fatalf("the watch stayed paused")
	}
}

func TestEnablingWithoutASidecarSaysWhy(t *testing.T) {
	r := newServiceRig(t)
	r.service.PIM = nil
	if r.service.Configured() {
		t.Fatalf("configured without a sidecar")
	}
	if err := r.service.Enable(context.Background(), r.identity, "home"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.service.Decisions(context.Background(), r.identity, "home", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("decisions: %v", err)
	}
}

func TestEnablingNeedsAnAccount(t *testing.T) {
	r := newServiceRig(t)
	if err := r.service.Enable(context.Background(), r.identity, "  "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestAFailedEnableLeavesNothingRunning(t *testing.T) {
	r := newServiceRig(t)
	r.pim.changeErr[ResourceCalendar] = hard()
	err := r.service.Enable(context.Background(), r.identity, "home")
	if class, _ := failureClass(err); class != FailureHard {
		t.Fatalf("err = %v", err)
	}
	if r.tasks.created != 0 {
		t.Fatalf("%d tasks after a failed enable", r.tasks.created)
	}
}

func TestEnablingAnAccountWithNoCalendarStillWatchesItsMail(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.pim.changeErr[ResourceCalendar] = unsupported()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	cursor, err := r.store.Cursor(ctx, r.identity, "home", ResourceCalendar)
	if err != nil || cursor.Status != CursorUnsupported {
		t.Fatalf("cursor = %+v, %v", cursor, err)
	}
}

func TestDisablingTheLastWatchTakesTheDigestAndKeepsTheVerdicts(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for _, account := range []string{"home", "work"} {
		if err := r.service.Enable(ctx, r.identity, account); err != nil {
			t.Fatalf("enable: %v", err)
		}
	}
	r.commit("home", r.draft("h1", TierToday, ""))

	if err := r.service.Disable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(r.tasks.watches) != 1 || len(r.tasks.digests) != 1 {
		t.Fatalf("%d watches, %d digests", len(r.tasks.watches), len(r.tasks.digests))
	}
	if _, err := r.store.Cursor(ctx, r.identity, "home", ResourceMail); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the cursor stayed: %v", err)
	}
	if _, err := r.store.Verdict(ctx, r.identity, "home", ResourceMail, "h1"); err != nil {
		t.Fatalf("the verdict went: %v", err)
	}

	if err := r.service.Disable(ctx, r.identity, "work"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(r.tasks.watches) != 0 || len(r.tasks.digests) != 0 {
		t.Fatalf("%d watches, %d digests", len(r.tasks.watches), len(r.tasks.digests))
	}
}

func TestDisablingWorksWithoutASidecar(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	r.service.PIM = nil
	if err := r.service.Disable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("disable: %v", err)
	}
}

func TestRemovingAnAccountForgetsItAndKeepsTheRules(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	r.commit("home", r.draft("h1", TierToday, ""))
	if _, err := r.service.AddRule(ctx, r.identity, RuleInput{Key: "a@b.it", Tier: TierNow}); err != nil {
		t.Fatalf("rule: %v", err)
	}

	if err := r.service.RemoveAccount(ctx, r.identity, "home"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(r.tasks.watches) != 0 || len(r.tasks.digests) != 0 {
		t.Fatalf("%d watches, %d digests", len(r.tasks.watches), len(r.tasks.digests))
	}
	if _, err := r.store.Verdict(ctx, r.identity, "home", ResourceMail, "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the verdict stayed: %v", err)
	}
	if rules, err := r.store.Rules(ctx, r.identity); err != nil || len(rules) != 1 {
		t.Fatalf("rules = %v, %v", rules, err)
	}
}

func TestStoppingAnIdentityLeavesNothingToFire(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for _, account := range []string{"home", "work"} {
		if err := r.service.Enable(ctx, r.identity, account); err != nil {
			t.Fatalf("enable: %v", err)
		}
	}
	if err := r.service.StopIdentity(ctx, r.identity); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(r.tasks.watches) != 0 || len(r.tasks.digests) != 0 {
		t.Fatalf("%d watches, %d digests", len(r.tasks.watches), len(r.tasks.digests))
	}
}

func TestStatusShowsEachWatchAndHowItFares(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, rigAccount); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := r.store.MarkFailing(ctx, r.identity, rigAccount, ResourceMail, "provider_transient", r.clock.at); err != nil {
		t.Fatalf("fail: %v", err)
	}
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{NextCursor: "c1"})

	status, err := r.service.Status(ctx, r.identity)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Configured || status.Settings.DailyAlertCap != 5 || len(status.Accounts) != 1 {
		t.Fatalf("status = %+v", status)
	}
	account := status.Accounts[0]
	if len(account.Resources) != 2 || account.Resources[1].Resource != ResourceMail || account.Resources[1].LastError != "provider_transient" {
		t.Fatalf("account = %+v", account)
	}
}

func TestDecisionsReadTheDayLive(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	r.commit(rigAccount, r.draft("m1", TierToday, label.ID), r.draft("gone", TierNow, ""))
	delete(r.pim.items, "gone")

	lines, err := r.service.Decisions(ctx, r.identity, rigAccount, "")
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %+v", lines)
	}
	byID := map[string]DecisionLine{}
	for _, line := range lines {
		byID[line.ItemID] = line
	}
	if got := byID["m1"]; got.From != "m1@acme.it" || got.Label != "Cliente" || got.By != ByDefault {
		t.Fatalf("m1 = %+v", got)
	}
	if got := byID["gone"]; !got.Gone || got.Tier != TierNow {
		t.Fatalf("gone = %+v", got)
	}

	yesterday := r.clock.at.AddDate(0, 0, -1).Format(time.DateOnly)
	if lines, err = r.service.Decisions(ctx, r.identity, rigAccount, yesterday); err != nil || len(lines) != 0 {
		t.Fatalf("yesterday = %+v, %v", lines, err)
	}
	if _, err := r.service.Decisions(ctx, r.identity, rigAccount, "01/11/2026"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a bad day: %v", err)
	}
	r.pim.itemsErr[rigAccount] = transient()
	if _, err := r.service.Decisions(ctx, r.identity, rigAccount, ""); err == nil {
		t.Fatalf("a failed read reported success")
	}
}

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
	if rule == nil || rule.Key != "mario@acme.it" || rule.Tier != TierLater || rule.Source != sourceCorrection {
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
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
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
	if err := r.service.StopIdentity(ctx, r.identity); !errors.Is(err, errBroken) {
		t.Fatalf("stop: %v", err)
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
