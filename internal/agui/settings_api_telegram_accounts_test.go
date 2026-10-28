package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeTelegramLinks struct {
	accounts  []TelegramAccount
	listErr   error
	unlinked  bool
	unlinkErr error

	listedFor  []string
	unlinkedAs []unlinkCall
}

type unlinkCall struct {
	identityID     string
	telegramUserID int64
}

func (f *fakeTelegramLinks) LinkedAccounts(_ context.Context, identityID string) ([]TelegramAccount, error) {
	f.listedFor = append(f.listedFor, identityID)
	return f.accounts, f.listErr
}

func (f *fakeTelegramLinks) UnlinkAccount(_ context.Context, identityID string, telegramUserID int64) (bool, error) {
	f.unlinkedAs = append(f.unlinkedAs, unlinkCall{identityID: identityID, telegramUserID: telegramUserID})
	return f.unlinked, f.unlinkErr
}

func serveTelegramAccounts(s *Server, r *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.registerTelegramAccountRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestListTelegramAccounts(t *testing.T) {
	t.Parallel()
	seen := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	links := &fakeTelegramLinks{accounts: []TelegramAccount{
		{TelegramUserID: 4503599627370495, Username: "andrea", FirstName: "Andrea", AddedAt: seen.Add(-time.Hour), LastSeenAt: &seen},
		{TelegramUserID: 777, AddedAt: seen},
	}}
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/settings/telegram/accounts", nil), "op-a")
	rec := serveTelegramAccounts(&Server{telegramLinks: links}, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0]["telegramUserId"] != float64(4503599627370495) || got[0]["username"] != "andrea" ||
		got[0]["firstName"] != "Andrea" || got[0]["lastSeenAt"] != "2026-10-28T09:00:00Z" || got[1]["lastSeenAt"] != nil {
		t.Fatalf("body = %s, want both accounts in order, a 52-bit id exact, null lastSeenAt when never seen", rec.Body)
	}
	if len(links.listedFor) != 1 || links.listedFor[0] != "op-a" {
		t.Fatalf("listed for %v, want the principal op-a", links.listedFor)
	}

	empty := serveTelegramAccounts(&Server{telegramLinks: &fakeTelegramLinks{}}, req)
	if empty.Code != http.StatusOK || empty.Body.String() != "[]\n" {
		t.Fatalf("no links = %d %q, want 200 []", empty.Code, empty.Body)
	}
	failing := serveTelegramAccounts(&Server{telegramLinks: &fakeTelegramLinks{listErr: errors.New("db down")}}, req)
	if failing.Code != http.StatusBadGateway {
		t.Fatalf("failing list = %d, want 502", failing.Code)
	}
}

func TestUnlinkTelegramAccount(t *testing.T) {
	t.Parallel()
	unlink := func(links *fakeTelegramLinks, path string) int {
		req := withPrincipal(httptest.NewRequest(http.MethodDelete, path, nil), "op-a")
		return serveTelegramAccounts(&Server{telegramLinks: links}, req).Code
	}

	mine := &fakeTelegramLinks{unlinked: true}
	if code := unlink(mine, "/api/settings/telegram/accounts/4242"); code != http.StatusNoContent {
		t.Fatalf("own link = %d, want 204", code)
	}
	if want := (unlinkCall{identityID: "op-a", telegramUserID: 4242}); len(mine.unlinkedAs) != 1 || mine.unlinkedAs[0] != want {
		t.Fatalf("unlinked %+v, want %+v: the identity is the principal's", mine.unlinkedAs, want)
	}
	if code := unlink(&fakeTelegramLinks{}, "/api/settings/telegram/accounts/4242"); code != http.StatusNotFound {
		t.Fatalf("a link the caller does not have = %d, want 404", code)
	}
	for _, id := range []string{"abc", "0", "-5", "9223372036854775808"} {
		links := &fakeTelegramLinks{unlinked: true}
		if code := unlink(links, "/api/settings/telegram/accounts/"+id); code != http.StatusBadRequest || len(links.unlinkedAs) != 0 {
			t.Fatalf("id %q = %d with %d unlinks, want 400 and none", id, code, len(links.unlinkedAs))
		}
	}
	if code := unlink(&fakeTelegramLinks{unlinkErr: errors.New("db down")}, "/api/settings/telegram/accounts/4242"); code != http.StatusBadGateway {
		t.Fatalf("failing unlink = %d, want 502", code)
	}
}

func TestTelegramAccountRoutesRefuseWithoutPrincipalOrSeam(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		path := "/api/settings/telegram/accounts"
		if method == http.MethodDelete {
			path += "/4242"
		}
		links := &fakeTelegramLinks{unlinked: true}
		if code := serveTelegramAccounts(&Server{telegramLinks: links}, httptest.NewRequest(method, path, nil)).Code; code != http.StatusUnauthorized {
			t.Fatalf("%s with no principal = %d, want 401", method, code)
		}
		if len(links.listedFor)+len(links.unlinkedAs) != 0 {
			t.Fatalf("%s with no principal reached the store", method)
		}
		req := withPrincipal(httptest.NewRequest(method, path, nil), "op-a")
		if code := serveTelegramAccounts(&Server{}, req).Code; code != http.StatusServiceUnavailable {
			t.Fatalf("%s with the seam unset = %d, want 503", method, code)
		}
	}
}
