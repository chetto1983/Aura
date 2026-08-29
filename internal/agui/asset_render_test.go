package agui

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
)

// asset_render_test.go pins the sealed-artifact surface: the scrub that removes the tags
// which fetch WITHOUT a script, the policy the document is served under, and the handler's
// ownership/shape/size gates.
//
// The scrub cases are written the way a bypass is actually attempted — spaces around `=`,
// mixed case, unquoted values — because those are all valid HTML and all defeat a regex.
// They exist to prove the parser is the filter, not a pattern.

func TestPrepareArtifactHTML_RemovesNoScriptFetchVectors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		absent  string
		present string
	}{
		{
			name:   "base rewrites every relative URL",
			in:     `<html><head><base href="https://evil.test/"></head><body>x</body></html>`,
			absent: "<base",
		},
		{
			name:   "base survives odd spacing and case",
			in:     `<html><head><BASE   HREF = "https://evil.test/"  ></head><body>x</body></html>`,
			absent: "<base",
		},
		{
			name:   "meta refresh navigates with no script at all",
			in:     `<html><head><meta http-equiv="refresh" content="0;url=https://evil.test/?x=1"></head><body>x</body></html>`,
			absent: "evil.test",
		},
		{
			name:   "meta refresh with spaces around the equals",
			in:     `<html><head><meta http-equiv = "REFRESH" content="0;url=https://evil.test/"></head><body>x</body></html>`,
			absent: "evil.test",
		},
		{
			name:   "link preload fetches on parse",
			in:     `<html><head><link rel="preload" as="image" href="https://evil.test/beacon.png"></head><body>x</body></html>`,
			absent: "evil.test",
		},
		{
			name:   "link dns-prefetch leaks the hostname alone",
			in:     `<html><head><link rel=dns-prefetch href="https://evil.test"></head><body>x</body></html>`,
			absent: "evil.test",
		},
		{
			name:   "modulepreload is in the family",
			in:     `<html><head><link rel="modulepreload" href="https://evil.test/m.js"></head><body>x</body></html>`,
			absent: "evil.test",
		},
		{
			name:   "a multi-token rel still matches",
			in:     `<html><head><link rel="alternate PREFETCH" href="https://evil.test/x"></head><body>x</body></html>`,
			absent: "evil.test",
		},
		{
			// The scrub is targeted, not a blanket strip: an inline stylesheet link is how a
			// legitimate page carries its own CSS and must survive.
			name:    "an ordinary stylesheet link survives",
			in:      `<html><head><link rel="stylesheet" href="style.css"></head><body>x</body></html>`,
			present: `rel="stylesheet"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := prepareArtifactHTML(tc.in)
			if err != nil {
				t.Fatalf("prepareArtifactHTML: %v", err)
			}
			if tc.absent != "" && strings.Contains(strings.ToLower(got), strings.ToLower(tc.absent)) {
				t.Fatalf("output still contains %q:\n%s", tc.absent, got)
			}
			if tc.present != "" && !strings.Contains(got, tc.present) {
				t.Fatalf("output dropped %q:\n%s", tc.present, got)
			}
		})
	}
}

func TestPrepareArtifactHTML_RetargetsExternalAnchors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		in         string
		wantTarget bool
	}{
		{name: "https", in: `<a href="https://example.test/x">e</a>`, wantTarget: true},
		{name: "http", in: `<a href="http://example.test/x">e</a>`, wantTarget: true},
		{name: "protocol relative", in: `<a href="//example.test/x">e</a>`, wantTarget: true},
		{name: "mailto", in: `<a href="mailto:a@b.test">e</a>`, wantTarget: true},
		{name: "tel", in: `<a href="tel:+390000">e</a>`, wantTarget: true},
		{name: "in-document fragment stays", in: `<a href="#top">t</a>`, wantTarget: false},
		{name: "relative path stays", in: `<a href="page2.html">t</a>`, wantTarget: false},
		{name: "empty href stays", in: `<a href="">t</a>`, wantTarget: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := prepareArtifactHTML("<html><body>" + tc.in + "</body></html>")
			if err != nil {
				t.Fatalf("prepareArtifactHTML: %v", err)
			}
			hasTarget := strings.Contains(got, `target="_blank"`)
			if hasTarget != tc.wantTarget {
				t.Fatalf("target=_blank present = %v, want %v:\n%s", hasTarget, tc.wantTarget, got)
			}
			if !tc.wantTarget {
				return
			}
			for _, token := range []string{"noopener", "noreferrer"} {
				if !strings.Contains(got, token) {
					t.Fatalf("rel is missing %q:\n%s", token, got)
				}
			}
		})
	}
}

// An anchor that already declares one of the tokens must not end up with it twice — the rel
// list is a set, and a duplicated token is the tell that it was appended blindly.
func TestPrepareArtifactHTML_AnchorRelIsASet(t *testing.T) {
	t.Parallel()
	got, err := prepareArtifactHTML(`<html><body><a href="https://e.test" rel="noopener">x</a></body></html>`)
	if err != nil {
		t.Fatalf("prepareArtifactHTML: %v", err)
	}
	if n := strings.Count(got, "noopener"); n != 1 {
		t.Fatalf("noopener appears %d times, want 1:\n%s", n, got)
	}
}

// The regression this whole surface exists to make impossible: a Vite/React artifact is a
// DEFERRED module script, and a rewrite that drops `type="module"` turns it into a blocking
// classic script in <head> that runs before #root exists — React error #299, blank page.
// The scrub must be transparent to script content and attributes.
func TestPrepareArtifactHTML_PreservesModuleScriptVerbatim(t *testing.T) {
	t.Parallel()
	const app = `<html><head><script type="module">const a = 1 < 2 && 3 > 2;
document.getElementById("root").textContent = String(a);</script></head><body><div id="root"></div></body></html>`
	got, err := prepareArtifactHTML(app)
	if err != nil {
		t.Fatalf("prepareArtifactHTML: %v", err)
	}
	if !strings.Contains(got, `type="module"`) {
		t.Fatalf("the module type was dropped — the artifact would run before #root exists:\n%s", got)
	}
	if !strings.Contains(got, `const a = 1 < 2 && 3 > 2;`) {
		t.Fatalf("script body was entity-escaped or altered:\n%s", got)
	}
	if !strings.Contains(got, `id="root"`) {
		t.Fatalf("the mount point was dropped:\n%s", got)
	}
}

func TestPrepareArtifactHTML_ArmsPolicyAndShim(t *testing.T) {
	t.Parallel()
	got, err := prepareArtifactHTML(`<html><head><title>t</title></head><body>x</body></html>`)
	if err != nil {
		t.Fatalf("prepareArtifactHTML: %v", err)
	}
	metaIdx := strings.Index(got, "Content-Security-Policy")
	if metaIdx < 0 {
		t.Fatalf("no CSP meta was injected:\n%s", got)
	}
	shimIdx := strings.Index(got, "__aura_artifact_probe__")
	if shimIdx < 0 {
		t.Fatalf("the opaque-origin shim was not injected:\n%s", got)
	}
	// The policy has to precede everything it governs, the shim has to precede the artifact's
	// own scripts; both hold only if the meta lands ahead of the shim.
	if metaIdx > shimIdx {
		t.Fatalf("CSP meta (%d) must precede the shim (%d)", metaIdx, shimIdx)
	}
	if bodyIdx := strings.Index(got, ">x<"); bodyIdx >= 0 && shimIdx > bodyIdx {
		t.Fatalf("the shim (%d) must precede the artifact body (%d)", shimIdx, bodyIdx)
	}
}

// A document with no <head> of its own still has to come back armed: ArmedHTML prepends the
// meta when there is no head boundary to insert at.
func TestPrepareArtifactHTML_FragmentStillArmed(t *testing.T) {
	t.Parallel()
	got, err := prepareArtifactHTML(`<p>just a fragment</p>`)
	if err != nil {
		t.Fatalf("prepareArtifactHTML: %v", err)
	}
	if !strings.Contains(got, "Content-Security-Policy") {
		t.Fatalf("a fragment came back unarmed:\n%s", got)
	}
	if !strings.Contains(got, "just a fragment") {
		t.Fatalf("the content was lost:\n%s", got)
	}
}

func TestArtifactRenderCSP_SealedFloor(t *testing.T) {
	t.Parallel()
	csp := artifactRenderCSP()
	for _, want := range []string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"connect-src 'none'",     // the scripted exfiltration half
		"base-uri 'none'",        // a re-introduced <base> would still be inert
		"form-action 'none'",     // a form cannot post the document out
		"frame-ancestors 'self'", // only the cockpit may frame it
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("policy is missing %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "'unsafe-eval'") {
		t.Fatalf("policy grants unsafe-eval, which no self-contained build needs: %s", csp)
	}
}

func TestRenderableArtifact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mime, name string
		want       bool
	}{
		{"text/html", "page.html", true},
		{"text/html; charset=utf-8", "page.html", true},
		{"TEXT/HTML", "page.html", true},
		{"application/octet-stream", "page.HTML", true}, // name carries it when the mime is generic
		{"application/octet-stream", "page.htm", true},
		{"application/octet-stream", "notes.txt", false},
		{"application/pdf", "report.pdf", false},
		{"image/svg+xml", "logo.svg", false}, // script-bearing, deliberately never rendered
		{"", "", false},
		{"text/html-ish", "x", false}, // a near-miss mime must not slip through
	}
	for _, tc := range cases {
		if got := renderableArtifact(tc.mime, tc.name); got != tc.want {
			t.Fatalf("renderableArtifact(%q, %q) = %v, want %v", tc.mime, tc.name, got, tc.want)
		}
	}
}

func newRenderServer(t *testing.T, fake *fakeAssetService) *Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(fake)
	return s
}

func TestAssetRender_OwnerGetsSealedDocument(t *testing.T) {
	t.Parallel()
	const raw = `<html><head><base href="https://evil.test/"><script type="module">1</script></head><body><div id="root"></div></body></html>`
	fake := &fakeAssetService{
		openResp: io.NopCloser(strings.NewReader(raw)),
		openAsset: assets.Asset{
			ID:         "asset-1",
			IdentityID: assetAPIIdentityID,
			FileName:   "memoria.html",
			MIMEType:   "text/html; charset=utf-8",
			SizeBytes:  int64(len(raw)),
		},
	}
	s := newRenderServer(t, fake)

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", ns)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("response carries no sealed policy: %q", csp)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	body := rec.Body.String()
	if strings.Contains(strings.ToLower(body), "<base") {
		t.Fatalf("the scrub did not run on the served body:\n%s", body)
	}
	if !strings.Contains(body, `type="module"`) {
		t.Fatalf("the module script did not survive the round trip:\n%s", body)
	}
	if fake.openID != "asset-1" || fake.openIdentityID != assetAPIIdentityID {
		t.Fatalf("OpenForIdentity(id=%q, identity=%q) — ownership was not bound", fake.openID, fake.openIdentityID)
	}
}

// Existence hiding (D-12): a non-owner and a missing id are indistinguishable, and the
// ownership gate is reached before any byte is read.
func TestAssetRender_NonOwnerIs404(t *testing.T) {
	t.Parallel()
	// The single generic error the WHERE-scoped lookup returns for both non-owned and absent.
	fake := &fakeAssetService{openErr: errors.New("asset not found")}
	s := newRenderServer(t, fake)

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAssetRender_NoSessionIs401(t *testing.T) {
	t.Parallel()
	fake := &fakeAssetService{}
	s := newRenderServer(t, fake)

	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if fake.openID != "" {
		t.Fatalf("the store was reached without a session: id=%q", fake.openID)
	}
}

// An owned asset that is not a page is refused rather than served as one — the route decides
// the renderable set from the STORED shape, never from the request.
func TestAssetRender_NonHTMLIsRefused(t *testing.T) {
	t.Parallel()
	const svg = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	fake := &fakeAssetService{
		openResp: io.NopCloser(strings.NewReader(svg)),
		openAsset: assets.Asset{
			ID:         "asset-1",
			IdentityID: assetAPIIdentityID,
			FileName:   "logo.svg",
			MIMEType:   "image/svg+xml",
			SizeBytes:  int64(len(svg)),
		},
	}
	s := newRenderServer(t, fake)

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alert(1)") {
		t.Fatalf("the refused body was echoed back: %s", rec.Body.String())
	}
}

// Over the cap the route refuses rather than truncating: a half-read document is malformed
// markup, and silently rendering it would be worse than saying no.
func TestAssetRender_OversizeIsRefused(t *testing.T) {
	t.Parallel()
	big := bytes.Repeat([]byte("a"), maxRenderableArtifactBytes+1)
	fake := &fakeAssetService{
		openResp: io.NopCloser(bytes.NewReader(big)),
		openAsset: assets.Asset{
			ID:         "asset-1",
			IdentityID: assetAPIIdentityID,
			FileName:   "huge.html",
			MIMEType:   "text/html",
			SizeBytes:  int64(len(big)),
		},
	}
	s := newRenderServer(t, fake)

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

// A document exactly AT the cap is served — the boundary is inclusive, and an off-by-one here
// would silently refuse a legal artifact.
func TestAssetRender_AtCapIsServed(t *testing.T) {
	t.Parallel()
	filler := strings.Repeat("a", maxRenderableArtifactBytes-len("<html><body></body></html>"))
	exact := "<html><body>" + filler + "</body></html>"
	if len(exact) != maxRenderableArtifactBytes {
		t.Fatalf("fixture is %d bytes, want exactly %d", len(exact), maxRenderableArtifactBytes)
	}
	fake := &fakeAssetService{
		openResp: io.NopCloser(strings.NewReader(exact)),
		openAsset: assets.Asset{
			ID:         "asset-1",
			IdentityID: assetAPIIdentityID,
			FileName:   "edge.html",
			MIMEType:   "text/html",
			SizeBytes:  int64(len(exact)),
		},
	}
	s := newRenderServer(t, fake)

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 at exactly the cap", rec.Code)
	}
}

func TestAssetRender_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/render", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
