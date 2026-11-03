package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/pimwatch"
)

type fakeWatchService struct {
	identity string
	account  string
	day      string
	status   pimwatch.Status
	lines    []pimwatch.DecisionLine
	got      pimwatch.Correction
	rule     *pimwatch.WrittenRule
	ruleNew  bool
	rules    []pimwatch.Rule
	added    pimwatch.RuleInput
	labels   []pimwatch.Label
	removed  string
	hour     int
	cap      int
	err      error
}

func (f *fakeWatchService) Status(_ context.Context, identityID string) (pimwatch.Status, error) {
	f.identity = identityID
	return f.status, f.err
}

func (f *fakeWatchService) Enable(_ context.Context, identityID, accountID string) error {
	f.identity, f.account = identityID, accountID
	return f.err
}

func (f *fakeWatchService) Disable(_ context.Context, _, accountID string) error {
	f.account = accountID
	return f.err
}

func (f *fakeWatchService) RemoveAccount(_ context.Context, _, accountID string) error {
	f.removed = accountID
	return f.err
}

func (f *fakeWatchService) Decisions(_ context.Context, _, accountID, day string) (pimwatch.DayDecisions, error) {
	f.account, f.day = accountID, day
	return pimwatch.DayDecisions{Day: "2026-11-02", Lines: f.lines}, f.err
}

func (f *fakeWatchService) Correct(_ context.Context, _ string, c pimwatch.Correction) (pimwatch.Verdict, *pimwatch.WrittenRule, error) {
	f.got = c
	return pimwatch.Verdict{Tier: c.Tier}, f.rule, f.err
}

func (f *fakeWatchService) Rules(context.Context, string) ([]pimwatch.Rule, error) {
	return f.rules, f.err
}

func (f *fakeWatchService) AddRule(_ context.Context, _ string, in pimwatch.RuleInput) (pimwatch.WrittenRule, error) {
	f.added = in
	return pimwatch.WrittenRule{ID: "r1", Key: in.Key, Kind: pimwatch.KeySender, Tier: in.Tier, Created: f.ruleNew}, f.err
}

func (f *fakeWatchService) RemoveRule(_ context.Context, _, ruleID string) error {
	f.removed = ruleID
	return f.err
}

func (f *fakeWatchService) Labels(context.Context, string) ([]pimwatch.Label, error) {
	return f.labels, f.err
}

func (f *fakeWatchService) AddLabel(_ context.Context, _, name string, tier pimwatch.Tier) (pimwatch.Label, error) {
	return pimwatch.Label{ID: "l1", Name: name, Tier: tier}, f.err
}

func (f *fakeWatchService) UpdateLabel(_ context.Context, _, labelID, name string, tier pimwatch.Tier) (pimwatch.Label, error) {
	return pimwatch.Label{ID: labelID, Name: name, Tier: tier}, f.err
}

func (f *fakeWatchService) RemoveLabel(_ context.Context, _, labelID string) error {
	f.removed = labelID
	return f.err
}

func (f *fakeWatchService) SaveSettings(_ context.Context, _ string, hour, dailyCap int) (pimwatch.Settings, error) {
	f.hour, f.cap = hour, dailyCap
	return pimwatch.Settings{DigestHour: hour, DailyAlertCap: dailyCap, IntervalMinutes: 5}, f.err
}

func mailWatchCall(t *testing.T, service mailWatchService, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if service != nil {
		s.SetMailWatch(service)
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "key-"+method+target)
	req = req.WithContext(identityctx.WithIdentityID(req.Context(), "11111111-1111-1111-1111-111111111111"))
	rec := httptest.NewRecorder()
	s.mailWatchRouteMux().ServeHTTP(rec, req)
	return rec
}

// mailWatchRouteMux is the routes on their own: the idempotency wrapper has its own sweep.
func (s *Server) mailWatchRouteMux() *http.ServeMux {
	mux := http.NewServeMux()
	s.registerMailWatchRoutes(mux)
	return mux
}

func decodeWatch[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMailWatchRoutesAnswer503WithoutTheService(t *testing.T) {
	if rec := mailWatchCall(t, nil, http.MethodGet, "/api/mail-watch", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMailWatchStatusIsTheCallersAndSaysWhyItCannotRun(t *testing.T) {
	since := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	service := &fakeWatchService{status: pimwatch.Status{
		Settings: pimwatch.Settings{DigestHour: 18, DailyAlertCap: 5, IntervalMinutes: 5},
		Accounts: []pimwatch.AccountStatus{{AccountID: "work", AlertsToday: 2, Resources: []pimwatch.ResourceStatus{
			{Resource: pimwatch.ResourceMail, Status: pimwatch.CursorFailing, LastError: "provider_auth", FailingSince: since},
		}}},
	}}
	out := decodeWatch[mailWatchStatusJSON](t, mailWatchCall(t, service, http.MethodGet, "/api/mail-watch", ""))
	if service.identity != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("identity = %q", service.identity)
	}
	if out.Configured || out.Reason != "pim_not_configured" || out.Settings.DigestHour != 18 {
		t.Fatalf("status = %+v", out)
	}
	resource := out.Accounts[0].Resources[0]
	if resource.LastError != "provider_auth" || resource.FailingSince == nil || !resource.FailingSince.Equal(since) || resource.AdvancedAt != nil {
		t.Fatalf("resource = %+v", resource)
	}
}

func TestMailWatchSwitchNamesItsAccount(t *testing.T) {
	service := &fakeWatchService{}
	if rec := mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/accounts/work/enable", ""); rec.Code != http.StatusOK || service.account != "work" {
		t.Fatalf("enable: %d %q", rec.Code, service.account)
	}
	if rec := mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/accounts/home/disable", ""); rec.Code != http.StatusOK || service.account != "home" {
		t.Fatalf("disable: %d %q", rec.Code, service.account)
	}
}

func TestMailWatchSettingsNeedBothValues(t *testing.T) {
	service := &fakeWatchService{}
	if rec := mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/settings", `{"digest_hour":7}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	out := decodeWatch[mailWatchSettingsJSON](t, mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/settings", `{"digest_hour":0,"daily_alert_cap":9}`))
	if service.hour != 0 || service.cap != 9 || out.DailyAlertCap != 9 || out.LastDigestAt != nil {
		t.Fatalf("hour %d cap %d out %+v", service.hour, service.cap, out)
	}
}

func TestMailWatchDecisionsCarryTheDayAndTheLiveText(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	service := &fakeWatchService{lines: []pimwatch.DecisionLine{
		{AccountID: "work", Resource: pimwatch.ResourceMail, ItemID: "m1", From: "a@b.it", Subject: "Linea ferma", Tier: pimwatch.TierNow, LabelID: "l1", Label: "Cliente", By: pimwatch.ByRule, DecidedAt: at},
		{AccountID: "work", Resource: pimwatch.ResourceMail, ItemID: "m2", Gone: true, Tier: pimwatch.TierToday, By: pimwatch.ByDefault, DecidedAt: at},
	}}
	out := decodeWatch[mailWatchDayJSON](t, mailWatchCall(t, service, http.MethodGet, "/api/mail-watch/accounts/work/decisions?day=2026-11-01", ""))
	rows := out.Decisions
	if service.day != "2026-11-01" || service.account != "work" || out.Day != "2026-11-02" || len(rows) != 2 {
		t.Fatalf("day %q account %q out %+v", service.day, service.account, out)
	}
	if rows[0].From != "a@b.it" || rows[0].Label != "Cliente" || rows[0].DecidedBy != "rule" || rows[0].Start != nil {
		t.Fatalf("first = %+v", rows[0])
	}
	if !rows[1].Gone || rows[1].From != "" {
		t.Fatalf("second = %+v", rows[1])
	}
}

func TestMailWatchDecisionsCarryTheNameAndAnAllDayDate(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	service := &fakeWatchService{lines: []pimwatch.DecisionLine{
		{AccountID: "work", Resource: pimwatch.ResourceMail, ItemID: "m1", From: "a@b.it", FromName: "Mario Rossi"},
		{AccountID: "work", Resource: pimwatch.ResourceCalendar, ItemID: "e1", Start: day, AllDay: true, Change: pimwatch.WordCancelled},
	}}
	rec := mailWatchCall(t, service, http.MethodGet, "/api/mail-watch/accounts/work/decisions?day=2026-11-01", "")
	body := rec.Body.String()
	for _, want := range []string{`"from":"a@b.it"`, `"from_name":"Mario Rossi"`, `"all_day":true`, `"change":"cancelled"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
}

func TestMailWatchDecisionsCarryTheCentroidWhereItDisagrees(t *testing.T) {
	service := &fakeWatchService{lines: []pimwatch.DecisionLine{
		{AccountID: "work", Resource: pimwatch.ResourceMail, ItemID: "m1", Tier: pimwatch.TierToday, Centroid: pimwatch.TierNow},
		{AccountID: "work", Resource: pimwatch.ResourceMail, ItemID: "m2", Tier: pimwatch.TierToday},
	}}
	body := mailWatchCall(t, service, http.MethodGet, "/api/mail-watch/accounts/work/decisions", "").Body.String()
	if strings.Count(body, `"centroid_tier"`) != 1 || !strings.Contains(body, `"centroid_tier":"now"`) {
		t.Fatalf("body = %s, want one centroid_tier, now", body)
	}
}

func TestMailWatchCorrectDefaultsToMailAndReportsARule(t *testing.T) {
	service := &fakeWatchService{rule: &pimwatch.WrittenRule{
		ID: "r1", Key: "a@b.it", Kind: pimwatch.KeySender, Tier: pimwatch.TierLater, Created: true,
	}}
	out := decodeWatch[mailWatchCorrectedJSON](t, mailWatchCall(t, service, http.MethodPost,
		"/api/mail-watch/accounts/work/decisions/correct", `{"item_id":"m1","tier":"later","label_id":""}`))
	if service.got.AccountID != "work" || service.got.Resource != pimwatch.ResourceMail || service.got.LabelID == nil || *service.got.LabelID != "" {
		t.Fatalf("correction = %+v", service.got)
	}
	if out.Tier != "later" || out.Rule == nil || out.Rule.Key != "a@b.it" || !out.RuleCreated {
		t.Fatalf("out = %+v", out)
	}
}

func TestMailWatchCorrectSaysARuleMergedIntoOneOnFile(t *testing.T) {
	service := &fakeWatchService{rule: &pimwatch.WrittenRule{ID: "r1", Key: "a@b.it", Kind: pimwatch.KeySender}}
	rec := mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/accounts/work/decisions/correct", `{"item_id":"m1","tier":"later"}`)
	if !strings.Contains(rec.Body.String(), `"rule"`) || strings.Contains(rec.Body.String(), "rule_created") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestMailWatchCorrectLeavesAnAbsentLabel(t *testing.T) {
	service := &fakeWatchService{}
	rec := mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/accounts/work/decisions/correct",
		`{"resource":"calendar","item_id":"ev1","tier":"today"}`)
	if rec.Code != http.StatusOK || service.got.LabelID != nil || service.got.Resource != pimwatch.ResourceCalendar {
		t.Fatalf("%d %+v", rec.Code, service.got)
	}
}

func TestMailWatchRulesAndLabelsRoundTrip(t *testing.T) {
	service := &fakeWatchService{
		rules:  []pimwatch.Rule{{ID: "r1", Key: "acme.it", Kind: pimwatch.KeyDomain, Tier: pimwatch.TierLater, Source: "person"}},
		labels: []pimwatch.Label{{ID: "l1", Name: "Cliente"}},
	}
	rules := decodeWatch[[]mailWatchRuleJSON](t, mailWatchCall(t, service, http.MethodGet, "/api/mail-watch/rules", ""))
	if len(rules) != 1 || rules[0].KeyKind != "domain" {
		t.Fatalf("rules = %+v", rules)
	}
	added := decodeWatch[mailWatchRuleJSON](t, mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/rules", `{"key":"a@b.it","tier":"now"}`))
	if added.ID != "r1" || service.added.Tier != pimwatch.TierNow {
		t.Fatalf("added = %+v", added)
	}
	if rec := mailWatchCall(t, service, http.MethodDelete, "/api/mail-watch/rules/r1", ""); rec.Code != http.StatusOK || service.removed != "r1" {
		t.Fatalf("remove rule: %d %q", rec.Code, service.removed)
	}
	labels := decodeWatch[[]mailWatchLabelJSON](t, mailWatchCall(t, service, http.MethodGet, "/api/mail-watch/labels", ""))
	if len(labels) != 1 || labels[0].Name != "Cliente" {
		t.Fatalf("labels = %+v", labels)
	}
	label := decodeWatch[mailWatchLabelJSON](t, mailWatchCall(t, service, http.MethodPost, "/api/mail-watch/labels", `{"name":"Lavoro"}`))
	if label.Name != "Lavoro" {
		t.Fatalf("label = %+v", label)
	}
	label = decodeWatch[mailWatchLabelJSON](t, mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/labels/l1", `{"name":"Clienti","tier":"today"}`))
	if label.ID != "l1" || label.Tier != "today" {
		t.Fatalf("label = %+v", label)
	}
	if rec := mailWatchCall(t, service, http.MethodDelete, "/api/mail-watch/labels/l1", ""); rec.Code != http.StatusOK || service.removed != "l1" {
		t.Fatalf("remove label: %d %q", rec.Code, service.removed)
	}
}

func TestMailWatchRefusesABodyItCannotRead(t *testing.T) {
	for _, call := range [][2]string{
		{http.MethodPut, "/api/mail-watch/settings"},
		{http.MethodPost, "/api/mail-watch/accounts/work/decisions/correct"},
		{http.MethodPost, "/api/mail-watch/rules"},
		{http.MethodPost, "/api/mail-watch/labels"},
		{http.MethodPut, "/api/mail-watch/labels/l1"},
	} {
		if rec := mailWatchCall(t, &fakeWatchService{}, call[0], call[1], `{"unknown":`); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: %d", call[0], call[1], rec.Code)
		}
	}
}

func TestMailWatchErrorsKeepToFixedWords(t *testing.T) {
	cases := []struct {
		err    error
		status int
		body   string
	}{
		{pimwatch.ErrUnavailable, http.StatusServiceUnavailable, "mail_watch_unavailable"},
		{&pimwatch.InvalidError{Code: "invalid_tier", Message: "the tier is now, today or later"}, http.StatusBadRequest, `"error":"invalid_tier"`},
		{pimwatch.ErrNotFound, http.StatusNotFound, "not_found"},
		{&pimwatch.PIMError{Class: pimwatch.FailureHard, Code: "provider_auth"}, http.StatusBadGateway, "provider_auth"},
		{errors.New("dial postgres://aura:secret@db:5432"), http.StatusInternalServerError, "mail_watch_failed"},
	}
	for _, c := range cases {
		rec := mailWatchCall(t, &fakeWatchService{err: c.err}, http.MethodPost, "/api/mail-watch/accounts/work/enable", "")
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) {
			t.Fatalf("%v: %d %s", c.err, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("a secret reached the wire: %s", rec.Body.String())
		}
	}
}

func TestAnUnavailableAnswerCarriesTheCode(t *testing.T) {
	refused := mailWatchCall(t, &fakeWatchService{err: pimwatch.ErrUnavailable}, http.MethodPost, "/api/mail-watch/accounts/work/enable", "")
	if refused.Code != http.StatusServiceUnavailable || !strings.Contains(refused.Body.String(), `"reason":"pim_not_configured"`) {
		t.Fatalf("enable = %d %s", refused.Code, refused.Body.String())
	}
}

func TestEveryMailWatchRouteReportsAServiceFailure(t *testing.T) {
	broken := &fakeWatchService{err: errors.New("broken")}
	for _, call := range [][3]string{
		{http.MethodGet, "/api/mail-watch", ""},
		{http.MethodPut, "/api/mail-watch/settings", `{"digest_hour":1,"daily_alert_cap":1}`},
		{http.MethodPost, "/api/mail-watch/accounts/work/enable", ""},
		{http.MethodPost, "/api/mail-watch/accounts/work/disable", ""},
		{http.MethodGet, "/api/mail-watch/accounts/work/decisions", ""},
		{http.MethodPost, "/api/mail-watch/accounts/work/decisions/correct", `{"item_id":"m"}`},
		{http.MethodGet, "/api/mail-watch/rules", ""},
		{http.MethodPost, "/api/mail-watch/rules", `{"key":"a@b.it"}`},
		{http.MethodDelete, "/api/mail-watch/rules/r1", ""},
		{http.MethodGet, "/api/mail-watch/labels", ""},
		{http.MethodPost, "/api/mail-watch/labels", `{"name":"x"}`},
		{http.MethodPut, "/api/mail-watch/labels/l1", `{"name":"x"}`},
		{http.MethodDelete, "/api/mail-watch/labels/l1", ""},
	} {
		if rec := mailWatchCall(t, broken, call[0], call[1], call[2]); rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s: %d", call[0], call[1], rec.Code)
		}
	}
}

// Removing an account in the cockpit leaves the watch nothing to fire on, and only a removal that
// happened: a sidecar that refused keeps the account, and with it the watch.
func TestRemovingAnAccountForgetsItsWatchOnlyWhenItLeft(t *testing.T) {
	cases := []struct {
		status int
		forget bool
	}{
		{http.StatusOK, true}, {http.StatusNoContent, true}, {http.StatusNotFound, true},
		{http.StatusForbidden, false}, {http.StatusBadGateway, false},
	}
	for _, c := range cases {
		sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
		}))
		service := &fakeWatchService{}
		s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
		s.SetCalendarMCP(sidecar.URL, "", staticPIMToken{})
		s.SetMailWatch(service)
		req := httptest.NewRequest(http.MethodDelete, "/api/connect/pim/accounts/work", nil)
		req = withPrincipal(req, "11111111-1111-1111-1111-111111111111")
		req.SetPathValue("id", "work")
		rec := httptest.NewRecorder()
		s.handlePIMDeleteAccount(rec, req)
		sidecar.Close()

		if rec.Code != c.status {
			t.Fatalf("status %d answered %d", c.status, rec.Code)
		}
		if forgotten := service.removed == "work"; forgotten != c.forget {
			t.Fatalf("status %d: forgotten = %v", c.status, forgotten)
		}
	}
}

func TestAWatchThatCannotBeForgottenDoesNotFailTheRemoval(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer sidecar.Close()
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecar.URL, "", staticPIMToken{})
	s.SetMailWatch(&fakeWatchService{err: errors.New("broken")})
	req := httptest.NewRequest(http.MethodDelete, "/api/connect/pim/accounts/work", nil)
	req = withPrincipal(req, "11111111-1111-1111-1111-111111111111")
	req.SetPathValue("id", "work")
	rec := httptest.NewRecorder()
	s.handlePIMDeleteAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

type staticPIMToken struct{}

func (staticPIMToken) AccessToken(context.Context, string) (string, error) { return "token", nil }

func TestMailWatchAddRuleSaysWhetherTheKeyHadNone(t *testing.T) {
	for _, created := range []bool{true, false} {
		service := &fakeWatchService{ruleNew: created}
		out := decodeWatch[mailWatchSavedRuleJSON](t, mailWatchCall(t, service, http.MethodPost,
			"/api/mail-watch/rules", `{"key":"a@b.it","tier":"later"}`))
		if out.Key != "a@b.it" || out.RuleCreated != created {
			t.Fatalf("created %v: out = %+v", created, out)
		}
	}
}
