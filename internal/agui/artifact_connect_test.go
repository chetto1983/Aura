package agui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
)

// renderedArtifactPolicy serves one artifact and returns (response CSP header, body).
func renderedArtifactPolicy(t *testing.T) (string, string) {
	t.Helper()
	fake := &fakeAssetService{
		openResp:  io.NopCloser(strings.NewReader(`<html><body><script>fetch("https://api.example.test")</script></body></html>`)),
		openAsset: assets.Asset{ID: "asset-1", IdentityID: assetAPIIdentityID, FileName: "page.html", MIMEType: "text/html"},
	}
	s := newRenderServer(t, fake)
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "https://aura.test/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d", rec.Code)
	}
	return rec.Header().Get("Content-Security-Policy"), rec.Body.String()
}

// TestArtifactSandboxedOpaqueOrigin pins the directive the artifact threat model now
// rests on. `sandbox allow-scripts` — never allow-same-origin, which would let the
// document drop its own sandbox — gives the document an OPAQUE origin even when it is
// opened as a top-level tab rather than framed inside the cockpit.
//
// Measured against Chromium on 2026-09-09: without the directive, a top-level artifact
// reached /api/secret and got HTTP 200 carrying the operator's cookie; with it, the same
// fetch failed as cross-origin. Framed artifacts were already opaque through the iframe's
// own sandbox attribute (web/src/chat/artifacts/renderers/HtmlPreview.tsx); this closes
// the unframed path to match.
//
// It is header-only — a meta-tag CSP ignores `sandbox` — which is why it must appear in
// the response header and is asserted there.
func TestArtifactSandboxedOpaqueOrigin(t *testing.T) {
	t.Parallel()
	policy, _ := renderedArtifactPolicy(t)
	if !strings.Contains(policy, "sandbox allow-scripts") {
		t.Fatalf("policy %q is missing the sandbox directive", policy)
	}
	if strings.Contains(policy, "allow-same-origin") {
		t.Fatalf("policy %q grants allow-same-origin: the document could drop its own sandbox", policy)
	}
}

// TestArtifactConnectIsOpen pins the consequence of that opacity: connect-src is `*`
// unconditionally, with no operator allowlist to maintain.
//
// The allowlist existed to stop an artifact calling Aura's own API with the operator's
// cookie. The sandbox stops that structurally — the call is cross-origin and carries no
// cookie — so the list protected nothing an attacker could still reach while blocking
// every legitimate API. What it does NOT stop is an artifact sending its OWN contents
// outward; that is the accepted trade, not an oversight.
func TestArtifactConnectIsOpen(t *testing.T) {
	t.Parallel()
	policy, body := renderedArtifactPolicy(t)
	if !strings.Contains(policy, "connect-src *;") {
		t.Fatalf("connect-src is not open: %s", policy)
	}
	// The meta policy inside the document must agree with the header, or an artifact
	// would be governed by whichever the browser happened to apply.
	if !strings.Contains(body, "connect-src *") {
		t.Fatalf("header and meta policy disagree: %s", body)
	}
}

// TestArtifactIsolationInvariants keeps the rest of the sealed-render posture honest:
// header-only directives stay out of the meta policy, and nothing widened script
// execution while connect-src was being opened.
func TestArtifactIsolationInvariants(t *testing.T) {
	t.Parallel()
	policy, body := renderedArtifactPolicy(t)
	if strings.Contains(body, "frame-ancestors") || strings.Contains(body, "sandbox allow-scripts") {
		t.Fatalf("header-only directive leaked into the HTML meta policy: %s", body)
	}
	for _, forbidden := range []string{"'unsafe-eval'", "script-src *", "img-src *"} {
		if strings.Contains(policy, forbidden) {
			t.Fatalf("policy admitted %q: %s", forbidden, policy)
		}
	}
	for _, required := range []string{"default-src 'none'", "script-src 'unsafe-inline';", "frame-ancestors 'self'"} {
		if !strings.Contains(policy, required) {
			t.Fatalf("render isolation changed, missing %q: %s", required, policy)
		}
	}
}
