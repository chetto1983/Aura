package agui

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/pimwatch"
)

func (f *fakeWatchService) SetCloudDecide(_ context.Context, _, accountID string, on bool) error {
	f.account, f.consent = accountID, on
	return f.err
}

func TestMailWatchCloudDecideSetsTheAccountsConsent(t *testing.T) {
	service := &fakeWatchService{}
	rec := mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/accounts/work/cloud-decide", `{"on":true}`)
	if rec.Code != http.StatusOK || service.account != "work" || !service.consent {
		t.Fatalf("on: %d %q %v", rec.Code, service.account, service.consent)
	}
	rec = mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/accounts/work/cloud-decide", `{"on":false}`)
	if rec.Code != http.StatusOK || service.consent {
		t.Fatalf("off: %d %v", rec.Code, service.consent)
	}
	for _, body := range []string{``, `{}`, `{"on":"yes"}`, `{"on":true,"extra":1}`} {
		if rec := mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/accounts/work/cloud-decide", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status %d", body, rec.Code)
		}
	}
	service.err = pimwatch.ErrNotWatched
	if rec := mailWatchCall(t, service, http.MethodPut, "/api/mail-watch/accounts/gone/cloud-decide", `{"on":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unwatched: status %d", rec.Code)
	}
}

func TestMailWatchStatusCarriesTheDecideRouteAndEachAccountsConsent(t *testing.T) {
	failedAt := time.Date(2026, 11, 4, 8, 30, 0, 0, time.UTC)
	service := &fakeWatchService{status: pimwatch.Status{
		DecideRoute: "openrouter",
		Accounts: []pimwatch.AccountStatus{
			{AccountID: "work", CloudDecide: true},
			{AccountID: "home", DecideError: "rejected", DecideFailedAt: failedAt},
		},
	}}
	out := decodeWatch[mailWatchStatusJSON](t, mailWatchCall(t, service, http.MethodGet, "/api/mail-watch", ""))
	work, home := out.Accounts[0], out.Accounts[1]
	if out.DecideRoute != "openrouter" || !work.CloudDecide || work.DecideError != "" || work.DecideFailedAt != nil {
		t.Fatalf("route %q work %+v", out.DecideRoute, work)
	}
	if home.CloudDecide || home.DecideError != "rejected" || home.DecideFailedAt == nil || !home.DecideFailedAt.Equal(failedAt) {
		t.Fatalf("home = %+v", home)
	}
}
