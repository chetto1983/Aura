package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The owner's Telegram account list and unlink are mounted like their three sibling Telegram
// routes: behind the session, then behind governance.write.
func TestServeWebuiTelegramAccountRoutesAreGated(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/settings/telegram/accounts"},
		{http.MethodDelete, "/api/settings/telegram/accounts/4242"},
	}
	for name, c := range map[string]struct {
		identities aguiIdentityStore
		session    bool
		want       int
	}{
		"no session":          {wiringIdentities{id: localID}, false, http.StatusUnauthorized},
		"no governance.write": {uncapableIdentities{id: localID}, true, http.StatusForbidden},
		"capable session":     {wiringIdentities{id: localID}, true, http.StatusOK},
	} {
		var hits []string
		aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits = append(hits, r.URL.Path)
			_, _ = io.WriteString(w, `{"agui":true}`)
		})
		handler, err := newServeHandler(aguiHandler, authulaTestDeps(localID, c.identities), &fakeAuthulaProvider{}, "")
		if err != nil {
			t.Fatalf("newServeHandler: %v", err)
		}
		for _, route := range routes {
			hits = nil
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(route.method, route.path, nil)
			if c.session {
				addAuthulaSession(req)
			}
			handler.ServeHTTP(rec, req)
			reached := len(hits) == 1 && hits[0] == route.path
			if rec.Code != c.want || reached != (c.want == http.StatusOK) {
				t.Errorf("%s: %s %s = %d, reached AG-UI %t; want %d", name, route.method, route.path, rec.Code, reached, c.want)
			}
		}
	}
}
