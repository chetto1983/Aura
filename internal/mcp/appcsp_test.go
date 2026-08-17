package mcp

import (
	"strings"
	"testing"
)

func TestContentSecurityPolicy_SealedByDefault(t *testing.T) {
	csp := ViewPolicy{}.ContentSecurityPolicy()
	for _, want := range []string{
		"default-src 'none'",
		"connect-src 'none'",
		"frame-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("a policy declaring nothing must contain %q, got %q", want, csp)
		}
	}
	// A view IS a single self-contained document, so its own inline script and
	// style are the two grants it cannot work without.
	if !strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Errorf("a view's own inline script must be allowed, got %q", csp)
	}
}

func TestContentSecurityPolicy_FillsTheDeclaredDirectives(t *testing.T) {
	// The shape the Microsoft ext-apps sample declares (employee-training): one
	// frame domain for an embedded video and nothing else.
	csp := ViewPolicy{
		DeclaredCSP:  true,
		FrameDomains: []string{"https://learn-video.azurefd.net"},
	}.ContentSecurityPolicy()
	if !strings.Contains(csp, "frame-src https://learn-video.azurefd.net") {
		t.Errorf("a declared frame domain must reach frame-src, got %q", csp)
	}
	if !strings.Contains(csp, "connect-src 'none'") {
		t.Errorf("an undeclared directive must stay sealed, got %q", csp)
	}
}

func TestContentSecurityPolicy_ResourceDomainsReachEveryAssetDirective(t *testing.T) {
	csp := ViewPolicy{DeclaredCSP: true, ResourceDomains: []string{"https://cdn.test"}}.ContentSecurityPolicy()
	for _, directive := range []string{"style-src", "img-src", "font-src", "media-src"} {
		if !strings.Contains(csp, directive) || !strings.Contains(csp, "https://cdn.test") {
			t.Errorf("%s must carry the declared resource domain, got %q", directive, csp)
		}
	}
	if strings.Contains(csp, "connect-src https://cdn.test") {
		t.Errorf("a resource domain must NOT become a connect target, got %q", csp)
	}
}

// A declared domain is a string a mounted server chose, and it lands inside a
// CSP. Anything that could close one directive and open another is the reason
// this validator exists.
func TestContentSecurityPolicy_RejectsDirectiveEscapes(t *testing.T) {
	hostile := []string{
		"https://ok.test; script-src *",
		"https://ok.test' 'unsafe-eval",
		"*",
		"'unsafe-inline'",
		"http://plain.test",
		"data:",
		"https://ok.test/path",
		"https://ok.test, https://other.test",
		"https://ok.test\nscript-src *",
		"javascript:alert(1)",
		"",
		"   ",
	}
	for _, domain := range hostile {
		t.Run(domain, func(t *testing.T) {
			csp := ViewPolicy{DeclaredCSP: true, ConnectDomains: []string{domain}}.ContentSecurityPolicy()
			if !strings.Contains(csp, "connect-src 'none'") {
				t.Fatalf("%q must be dropped, leaving connect-src sealed; got %q", domain, csp)
			}
			if strings.Count(csp, "script-src") != 1 {
				t.Fatalf("%q introduced or duplicated a directive: %q", domain, csp)
			}
		})
	}
}

func TestContentSecurityPolicy_AcceptsTheOriginShapesTheSpecAllows(t *testing.T) {
	for _, domain := range []string{
		"https://api.test",
		"https://*.example.com",
		"https://api.example.co.uk",
		"https://localhost:8443",
		"https://a-b.test:443",
	} {
		t.Run(domain, func(t *testing.T) {
			csp := ViewPolicy{DeclaredCSP: true, ConnectDomains: []string{domain}}.ContentSecurityPolicy()
			if !strings.Contains(csp, "connect-src "+domain) {
				t.Fatalf("%q must be kept, got %q", domain, csp)
			}
		})
	}
}

func TestArmedHTML_PolicyLandsFirstInHead(t *testing.T) {
	html := `<!doctype html><html><head><title>x</title></head><body>hi</body></html>`
	armed := ArmedHTML(html, "default-src 'none'")
	meta := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'">`
	if !strings.Contains(armed, meta) {
		t.Fatalf("armed document must carry the policy: %q", armed)
	}
	// A meta CSP governs only what FOLLOWS it, so anything already in <head> must
	// come after — otherwise the policy is decoration.
	if strings.Index(armed, meta) > strings.Index(armed, "<title>") {
		t.Fatalf("the policy must precede the head's own content: %q", armed)
	}
}

func TestArmedHTML_AttributeDelimiterCannotBeEscaped(t *testing.T) {
	armed := ArmedHTML("<html><head></head></html>", `default-src 'none'; report-to "x"`)
	if strings.Contains(armed, `content="default-src 'none'; report-to "x""`) {
		t.Fatalf("a quote in the policy must not close the attribute: %q", armed)
	}
	if !strings.Contains(armed, "&quot;") {
		t.Fatalf("a quote must be entity-escaped: %q", armed)
	}
}

func TestArmedHTML_DocumentWithoutHead(t *testing.T) {
	armed := ArmedHTML("<div>bare</div>", "default-src 'none'")
	if !strings.HasPrefix(armed, `<meta http-equiv="Content-Security-Policy"`) {
		t.Fatalf("a headless document must be prefixed with the policy: %q", armed)
	}
}

func TestArmedHTML_HeadWithAttributes(t *testing.T) {
	armed := ArmedHTML(`<html><HEAD lang="en"><title>t</title></HEAD></html>`, "default-src 'none'")
	if strings.Index(armed, "Content-Security-Policy") > strings.Index(armed, "<title>") {
		t.Fatalf("an uppercase/attributed head must still be found: %q", armed)
	}
}

// A cookie ignores the port, so a declared domain pointing at the deployment's
// own host is a way back through the sandbox wall carrying the operator's
// session. It comes out of the policy that is applied — while the declaration
// itself is still reported, so a human can see what the server asked for.
func TestWithoutHost_DropsSourcesPointingBackAtTheDeployment(t *testing.T) {
	policy := ViewPolicy{
		DeclaredCSP:     true,
		ConnectDomains:  []string{"https://aura.lan", "https://aura.lan:8444", "https://api.test"},
		ResourceDomains: []string{"https://cdn.test"},
		FrameDomains:    []string{"https://*.aura.lan"},
	}
	granted := policy.WithoutHost("aura.lan")

	// Both spellings of our own host go, port and all — that IS the point, since
	// the cookie does not distinguish them.
	if got := granted.ConnectDomains; len(got) != 1 || got[0] != "https://api.test" {
		t.Errorf("ConnectDomains = %#v", got)
	}
	if len(granted.FrameDomains) != 0 {
		t.Errorf("a wildcard over our own domain must go too, got %#v", granted.FrameDomains)
	}
	if len(granted.ResourceDomains) != 1 {
		t.Errorf("an unrelated domain must survive, got %#v", granted.ResourceDomains)
	}
	// The ORIGINAL is untouched: the route reports what was declared and applies
	// what was granted, and the two must not be the same object.
	if len(policy.ConnectDomains) != 3 {
		t.Errorf("the declaration must not be mutated, got %#v", policy.ConnectDomains)
	}
}

func TestWithoutHost_KeepsWhatOnlyLOOKSLikeOurHost(t *testing.T) {
	policy := ViewPolicy{DeclaredCSP: true, ConnectDomains: []string{
		"https://aura.lan.evil.test", // a suffix trick: a different registrable domain
		"https://notaura.lan",        // a different host that merely ends the same
	}}
	if got := policy.WithoutHost("aura.lan").ConnectDomains; len(got) != 2 {
		t.Fatalf("only our own host may be dropped, got %#v", got)
	}
	// A wildcard covers the subdomain it names, so a subdomain host drops it.
	sub := ViewPolicy{DeclaredCSP: true, ConnectDomains: []string{"https://*.aura.lan"}}
	if got := sub.WithoutHost("app.aura.lan").ConnectDomains; len(got) != 0 {
		t.Fatalf("a wildcard covering the host must be dropped, got %#v", got)
	}
	// An empty hostname (a request with no Host) changes nothing rather than
	// silently dropping everything.
	if len(policy.WithoutHost("  ").ConnectDomains) != 2 {
		t.Error("an empty hostname must be a no-op")
	}
}
