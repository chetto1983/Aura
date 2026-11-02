package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/pimwatch"
)

type fakeMailWatch struct {
	status     pimwatch.Status
	identity   string
	enabled    string
	disabled   string
	correction pimwatch.Correction
	rule       *pimwatch.WrittenRule
	added      pimwatch.RuleInput
	removed    string
	rules      []pimwatch.Rule
	labels     []pimwatch.Label
	decisions  string
	asked      []string
	err        error
}

func (f *fakeMailWatch) DecisionsText(_ context.Context, identityID, accountID, day string) (string, error) {
	f.asked = []string{identityID, accountID, day}
	return f.decisions, f.err
}

func (f *fakeMailWatch) Status(_ context.Context, identityID string) (pimwatch.Status, error) {
	f.identity = identityID
	return f.status, f.err
}

func (f *fakeMailWatch) Enable(_ context.Context, identityID, accountID string) error {
	f.identity, f.enabled = identityID, accountID
	return f.err
}

func (f *fakeMailWatch) Disable(_ context.Context, _, accountID string) error {
	f.disabled = accountID
	return f.err
}

func (f *fakeMailWatch) Correct(_ context.Context, _ string, c pimwatch.Correction) (pimwatch.Verdict, *pimwatch.WrittenRule, error) {
	f.correction = c
	return pimwatch.Verdict{Tier: c.Tier}, f.rule, f.err
}

func (f *fakeMailWatch) AddRule(_ context.Context, _ string, in pimwatch.RuleInput) (pimwatch.WrittenRule, error) {
	f.added = in
	return pimwatch.WrittenRule{ID: "r1", Key: in.Key, Tier: in.Tier, LabelID: in.LabelID}, f.err
}

func (f *fakeMailWatch) RemoveRule(_ context.Context, _, ruleID string) error {
	f.removed = ruleID
	return f.err
}

func (f *fakeMailWatch) Rules(context.Context, string) ([]pimwatch.Rule, error) {
	return f.rules, f.err
}

func (f *fakeMailWatch) Labels(context.Context, string) ([]pimwatch.Label, error) {
	return f.labels, f.err
}

func (f *fakeMailWatch) LabelID(_ context.Context, _, name string) (string, error) {
	for _, label := range f.labels {
		if label.Name == name {
			return label.ID, nil
		}
	}
	return "", pimwatch.ErrInvalid
}

func mailWatchRun(t *testing.T, tool *MailWatchTool, args string) (string, error) {
	t.Helper()
	ctx := identityctx.WithIdentityID(boardCtx("session"), "id-1")
	result, err := tool.Execute(ctx, json.RawMessage(args))
	return result.Preview, err
}

func TestMailWatchIsDeferredAndMultiplexed(t *testing.T) {
	spec := (&MailWatchTool{}).Spec()
	if spec.Name != "mail_watch" || !spec.Deferred || !spec.Mutating || !spec.Multiplexed {
		t.Fatalf("spec = %+v", spec)
	}
	if !json.Valid(spec.Parameters) {
		t.Fatalf("parameters are not valid JSON")
	}
}

func TestMailWatchWithoutAServiceSaysSo(t *testing.T) {
	_, err := mailWatchRun(t, &MailWatchTool{}, `{"action":"status"}`)
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err = %v", err)
	}
}

func TestMailWatchEnableRunsAsTheCallersIdentity(t *testing.T) {
	service := &fakeMailWatch{}
	text, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"enable","account_id":"work"}`)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if service.identity != "id-1" || service.enabled != "work" || !strings.Contains(text, "work") {
		t.Fatalf("identity %q, enabled %q, text %q", service.identity, service.enabled, text)
	}
}

func TestMailWatchListDecisionsReadsOneAccountsDay(t *testing.T) {
	service := &fakeMailWatch{decisions: "1. later by default | account=home resource=mail id=m1 | a@b.it: hi"}
	text, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"list_decisions","account_id":"home","day":"2026-11-01"}`)
	if err != nil || text != service.decisions {
		t.Fatalf("text = %q, err = %v", text, err)
	}
	if strings.Join(service.asked, " ") != "id-1 home 2026-11-01" {
		t.Fatalf("asked = %v", service.asked)
	}
}

func TestMailWatchListDecisionsDefaultsToToday(t *testing.T) {
	service := &fakeMailWatch{}
	if _, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"list_decisions","account_id":"home"}`); err != nil {
		t.Fatalf("list: %v", err)
	}
	if service.asked[2] != "" {
		t.Fatalf("day = %q", service.asked[2])
	}
}

func TestMailWatchListDecisionsNeedsAnAccount(t *testing.T) {
	service := &fakeMailWatch{}
	if _, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"list_decisions"}`); err == nil {
		t.Fatalf("a list with no account was accepted")
	}
	if service.asked != nil {
		t.Fatalf("asked = %v", service.asked)
	}
}

func TestMailWatchListDecisionsPassesAFailureOn(t *testing.T) {
	service := &fakeMailWatch{err: pimwatch.ErrUnavailable}
	if _, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"list_decisions","account_id":"home"}`); !errors.Is(err, pimwatch.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestMailWatchDisableNeedsAnAccount(t *testing.T) {
	service := &fakeMailWatch{}
	tool := &MailWatchTool{Service: service}
	if _, err := mailWatchRun(t, tool, `{"action":"disable"}`); err == nil {
		t.Fatalf("disable with no account reported success")
	}
	if _, err := mailWatchRun(t, tool, `{"action":"disable","account_id":"work"}`); err != nil || service.disabled != "work" {
		t.Fatalf("disabled %q, %v", service.disabled, err)
	}
}

func TestMailWatchStatusReadsAsAPersonWould(t *testing.T) {
	since := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	service := &fakeMailWatch{status: pimwatch.Status{
		Configured: true, Settings: pimwatch.Settings{DigestHour: 18, DailyAlertCap: 5},
		Accounts: []pimwatch.AccountStatus{
			{AccountID: "work", AlertsToday: 2, Resources: []pimwatch.ResourceStatus{
				{Resource: pimwatch.ResourceMail, Status: pimwatch.CursorFailing, LastError: "provider_auth", FailingSince: since},
				{Resource: pimwatch.ResourceCalendar, Status: pimwatch.CursorOK},
			}},
			{AccountID: "home", Paused: true, PausedReason: "failures"},
		},
	}}
	text, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"status"}`)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"Digest at 18:00, at most 5 alerts", "- work: watching, 2 alerts today",
		"mail failing (provider_auth since 2026-11-01T09:00:00Z)", "calendar ok", "- home: paused (failures)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestMailWatchStatusSaysWhyItCannotRun(t *testing.T) {
	text, err := mailWatchRun(t, &MailWatchTool{Service: &fakeMailWatch{}}, `{"action":"status"}`)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(text, "cannot watch mail") || !strings.Contains(text, "No account is watched.") {
		t.Fatalf("text = %q", text)
	}
}

func TestMailWatchCorrectMapsALabelsNameAndDefaultsToMail(t *testing.T) {
	service := &fakeMailWatch{labels: []pimwatch.Label{{ID: "l1", Name: "Cliente"}}, rule: &pimwatch.WrittenRule{Key: "a@b.it"}}
	text, err := mailWatchRun(t, &MailWatchTool{Service: service},
		`{"action":"correct","account_id":"work","item_id":"m1","tier":"later","label":"Cliente"}`)
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	got := service.correction
	if got.Resource != pimwatch.ResourceMail || got.Tier != pimwatch.TierLater || got.LabelID == nil || *got.LabelID != "l1" {
		t.Fatalf("correction = %+v", got)
	}
	if !strings.Contains(text, "later") || !strings.Contains(text, "a rule now") {
		t.Fatalf("text = %q", text)
	}
}

func TestMailWatchCorrectTellsLeavingTheLabelFromClearingIt(t *testing.T) {
	service := &fakeMailWatch{}
	tool := &MailWatchTool{Service: service}
	if _, err := mailWatchRun(t, tool, `{"action":"correct","account_id":"w","item_id":"m","tier":"now"}`); err != nil {
		t.Fatalf("correct: %v", err)
	}
	if service.correction.LabelID != nil {
		t.Fatalf("an absent label became %q", *service.correction.LabelID)
	}
	if _, err := mailWatchRun(t, tool, `{"action":"correct","account_id":"w","item_id":"m","label":""}`); err != nil {
		t.Fatalf("correct: %v", err)
	}
	if service.correction.LabelID == nil || *service.correction.LabelID != "" {
		t.Fatalf("an empty label must clear: %v", service.correction.LabelID)
	}
}

func TestMailWatchCorrectRefusesWhatItCannotPlace(t *testing.T) {
	tool := &MailWatchTool{Service: &fakeMailWatch{}}
	if _, err := mailWatchRun(t, tool, `{"action":"correct","account_id":"work"}`); err == nil {
		t.Fatalf("correct with no item reported success")
	}
	if _, err := mailWatchRun(t, tool, `{"action":"correct","account_id":"w","item_id":"m","label":"nobody"}`); !errors.Is(err, pimwatch.ErrInvalid) {
		t.Fatalf("unknown label: %v", err)
	}
}

func TestMailWatchRulesGoThroughTheService(t *testing.T) {
	service := &fakeMailWatch{
		labels: []pimwatch.Label{{ID: "l1", Name: "Cliente"}},
		rules:  []pimwatch.Rule{{ID: "r1", Key: "a@b.it", Tier: pimwatch.TierLater, LabelID: "l1", Source: "person"}},
	}
	tool := &MailWatchTool{Service: service}

	text, err := mailWatchRun(t, tool, `{"action":"add_rule","key":"a@b.it","tier":"later","label":"Cliente"}`)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if service.added.LabelID != "l1" || !strings.Contains(text, "a@b.it goes to later with label Cliente") {
		t.Fatalf("added %+v, text %q", service.added, text)
	}
	if _, err := mailWatchRun(t, tool, `{"action":"add_rule","key":"a@b.it","label":"nobody"}`); !errors.Is(err, pimwatch.ErrInvalid) {
		t.Fatalf("unknown label: %v", err)
	}
	text, err = mailWatchRun(t, tool, `{"action":"list_rules"}`)
	if err != nil || !strings.Contains(text, "- r1: a@b.it goes to later with label Cliente. (person)") {
		t.Fatalf("list: %q, %v", text, err)
	}
	if _, err := mailWatchRun(t, tool, `{"action":"remove_rule","rule_id":"r1"}`); err != nil || service.removed != "r1" {
		t.Fatalf("removed %q, %v", service.removed, err)
	}
}

func TestMailWatchListsSayWhenTheyAreEmpty(t *testing.T) {
	tool := &MailWatchTool{Service: &fakeMailWatch{}}
	if text, err := mailWatchRun(t, tool, `{"action":"list_rules"}`); err != nil || text != "No rules." {
		t.Fatalf("rules: %q, %v", text, err)
	}
	if text, err := mailWatchRun(t, tool, `{"action":"list_labels"}`); err != nil || text != "No labels." {
		t.Fatalf("labels: %q, %v", text, err)
	}
}

func TestMailWatchListsLabelsWithTheirTier(t *testing.T) {
	service := &fakeMailWatch{labels: []pimwatch.Label{{Name: "Cliente"}, {Name: "Pubblicità", Tier: pimwatch.TierLater}}}
	text, err := mailWatchRun(t, &MailWatchTool{Service: service}, `{"action":"list_labels"}`)
	if err != nil || text != "- Cliente\n- Pubblicità (later)" {
		t.Fatalf("labels: %q, %v", text, err)
	}
}

func TestMailWatchReportsAServiceFailure(t *testing.T) {
	broken := errors.New("broken")
	tool := &MailWatchTool{Service: &fakeMailWatch{err: broken}}
	for _, args := range []string{
		`{"action":"status"}`, `{"action":"enable","account_id":"w"}`, `{"action":"disable","account_id":"w"}`,
		`{"action":"correct","account_id":"w","item_id":"m","tier":"now"}`, `{"action":"add_rule","key":"a@b.it","tier":"now"}`,
		`{"action":"remove_rule","rule_id":"r"}`, `{"action":"list_rules"}`, `{"action":"list_labels"}`,
	} {
		if _, err := mailWatchRun(t, tool, args); !errors.Is(err, broken) {
			t.Fatalf("%s: %v", args, err)
		}
	}
}

func TestMailWatchRefusesArgumentsThatAreNotAnObjectOfStrings(t *testing.T) {
	tool := &MailWatchTool{Service: &fakeMailWatch{}}
	for _, action := range []string{"enable", "disable", "correct", "add_rule", "remove_rule"} {
		if _, err := mailWatchRun(t, tool, `{"action":"`+action+`","account_id":1,"key":1,"rule_id":1}`); err == nil {
			t.Fatalf("%s: a number was accepted", action)
		}
	}
}
