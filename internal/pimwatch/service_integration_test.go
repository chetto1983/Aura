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
	for _, tasks := range []map[string]WatchTask{f.watches, f.digests} {
		if task, ok := tasks[taskID]; ok {
			task.Paused = false
			tasks[taskID] = task
		}
	}
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
	if spec.EveryMinutes != 5 || spec.Zone != "UTC" || spec.Delivery.Route != "stdout" || !strings.Contains(spec.Title, "home") {
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

func TestEnablingResumesAPausedDigestAndMovesItToTheCurrentHour(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	key := r.identity + "|digest"
	task := r.tasks.digests[key]
	task.Paused = true
	r.tasks.digests[key] = task
	if _, err := r.store.SaveSettings(ctx, r.identity, 7, 5); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if r.tasks.digests[key].Paused || r.tasks.digest[key].Hour != 7 {
		t.Fatalf("digest task %+v, spec %+v", r.tasks.digests[key], r.tasks.digest[key])
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
