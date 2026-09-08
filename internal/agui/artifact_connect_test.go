package agui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
)

func TestArtifactConnectPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		origins []string
		connect string
	}{
		{"offline default", nil, "connect-src 'none'"},
		{"approved API", []string{"https://api.example.test"}, "connect-src https://api.example.test"},
		{"cockpit host excluded across ports", []string{"https://aura.test:8444", "https://api.example.test"}, "connect-src https://api.example.test"},
		{"policy injection rejected", []string{"https://evil.test; script-src *", "http://api.example.test", "*"}, "connect-src 'none'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeAssetService{
				openResp:  io.NopCloser(strings.NewReader(`<html><body><script>fetch("https://api.example.test")</script></body></html>`)),
				openAsset: assets.Asset{ID: "asset-1", IdentityID: assetAPIIdentityID, FileName: "page.html", MIMEType: "text/html"},
			}
			s := newRenderServer(t, fake)
			s.cfg.ArtifactConnectOrigins = tc.origins
			req := withPrincipal(httptest.NewRequest(http.MethodGet, "https://aura.test/api/assets/asset-1/render", nil), assetAPIIdentityID)
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("render status = %d", rec.Code)
			}
			policy := rec.Header().Get("Content-Security-Policy")
			if !strings.Contains(policy, tc.connect+";") {
				t.Fatalf("unexpected connect policy: %s", policy)
			}
			if !strings.Contains(rec.Body.String(), tc.connect) {
				t.Fatalf("header/meta policy disagree: %s", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "frame-ancestors") {
				t.Fatal("header-only directive leaked into HTML meta policy")
			}
			for _, blocked := range []string{"aura.test", "evil.test", "script-src *", "'unsafe-eval'"} {
				if strings.Contains(policy, blocked) {
					t.Fatalf("policy admitted %q: %s", blocked, policy)
				}
			}
			if !strings.Contains(policy, "script-src 'unsafe-inline';") || !strings.Contains(policy, "frame-ancestors 'self'") {
				t.Fatalf("render isolation changed: %s", policy)
			}
		})
	}
}
