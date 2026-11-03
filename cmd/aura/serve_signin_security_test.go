package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheDesktopCannotWriteToTheCockpit(t *testing.T) {
	for _, tc := range []struct {
		site, origin, media string
		want                int
	}{
		{"same-site", "https://aura.test:8444", "text/plain", 403},
		{"same-site", "https://aura.test:8444", "application/x-www-form-urlencoded", 403},
		{"", "https://aura.test:8444", "text/plain", 403},
		{"cross-site", "https://evil.example", "text/plain", 403},
		{"same-origin", "https://aura.test", "application/json", 204},
		{"", "", "application/json", 204},
	} {
		writes := 0
		h, err := newServeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes++; w.WriteHeader(204) }), authulaTestDeps("alice", wiringIdentities{"alice"}), nil, "")
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "https://aura.test/api/governance/mcp/fixture/authorization", strings.NewReader("{}"))
		r.Header.Set("Sec-Fetch-Site", tc.site)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.media)
		addAuthulaSession(r)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != tc.want || (tc.want == 403 && writes != 0) || (tc.want == 204 && writes != 1) {
			t.Fatalf("%+v: status=%d writes=%d body=%s", tc, rr.Code, writes, rr.Body)
		}
	}
}
