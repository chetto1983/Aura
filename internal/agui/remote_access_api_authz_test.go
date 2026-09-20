package agui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteAccessSecretsCannotBypassDedicatedAPI(t *testing.T) {
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"} {
		for _, method := range []string{"PUT", "DELETE"} {
			store := &fakeSettingsStore{}
			s := &Server{settings: store, idAdmin: remoteAdminCaps()}
			req := withPrincipal(httptest.NewRequest(method, "/api/settings/"+key, strings.NewReader(`{"value":"unchecked"}`)), "admin-1")
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, req)
			if rec.Code != 403 || len(store.upserted) > 0 || len(store.deleted) > 0 {
				t.Fatalf("%s %s bypassed dedicated API: %d", method, key, rec.Code)
			}
		}
	}
}

func TestRemoteAccessMutationsRequireIdempotencyKeys(t *testing.T) {
	s := &Server{idAdmin: remoteAdminCaps(), operations: &memoryHTTPRegistry{}}
	s.SetRemoteAccess(&fakeRemoteAccess{})
	for _, route := range RemoteAccessRoutes() {
		method, path, _ := strings.Cut(route, " ")
		if method == "GET" || strings.HasSuffix(path, "/token/verify") || strings.HasSuffix(path, "/rotate-token") {
			continue
		}
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest(method, path, strings.NewReader(`{}`)), "admin-1"))
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Idempotency-Key") {
			t.Fatalf("route=%s status=%d body=%s", route, rec.Code, rec.Body.String())
		}
	}
}
