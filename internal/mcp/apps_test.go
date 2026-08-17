package mcp

import (
	"reflect"
	"testing"
)

func TestIsAppURI(t *testing.T) {
	for _, tc := range []struct {
		uri  string
		want bool
	}{
		{"ui://server/app.html", true},
		{"ui://a", true},
		{"ui://", false},
		{"https://example.test/app.html", false},
		{"file:///etc/passwd", false},
		{"", false},
		{" ui://server/app.html", false},
	} {
		if got := IsAppURI(tc.uri); got != tc.want {
			t.Errorf("IsAppURI(%q) = %v, want %v", tc.uri, got, tc.want)
		}
	}
}

func TestViewRefFromMeta(t *testing.T) {
	t.Run("reads the resource uri and visibility", func(t *testing.T) {
		ref, ok := ViewRefFromMeta(map[string]any{
			"ui": map[string]any{
				"resourceUri": "ui://x/app.html",
				"visibility":  []any{"model", "app"},
			},
		})
		if !ok {
			t.Fatal("a well-formed ui ref must parse")
		}
		if ref.ResourceURI != "ui://x/app.html" {
			t.Errorf("ResourceURI = %q", ref.ResourceURI)
		}
		if !reflect.DeepEqual(ref.Visibility, []string{"model", "app"}) {
			t.Errorf("Visibility = %#v", ref.Visibility)
		}
	})

	t.Run("absent visibility stays nil rather than defaulting", func(t *testing.T) {
		ref, ok := ViewRefFromMeta(map[string]any{"ui": map[string]any{"resourceUri": "ui://x/a.html"}})
		if !ok {
			t.Fatal("must parse")
		}
		if ref.Visibility != nil {
			t.Errorf("Visibility = %#v, want nil so a caller can apply its own default", ref.Visibility)
		}
	})

	for name, meta := range map[string]map[string]any{
		"no meta at all":        nil,
		"no ui member":          {"other": map[string]any{"resourceUri": "ui://x/a.html"}},
		"ui is not an object":   {"ui": "ui://x/a.html"},
		"no resourceUri":        {"ui": map[string]any{"visibility": []any{"model"}}},
		"blank resourceUri":     {"ui": map[string]any{"resourceUri": "   "}},
		"resourceUri is a bool": {"ui": map[string]any{"resourceUri": true}},
	} {
		t.Run(name+" does not parse", func(t *testing.T) {
			if _, ok := ViewRefFromMeta(meta); ok {
				t.Error("must not parse")
			}
		})
	}

	// A tool aiming the host at a non-ui:// scheme is a misdeclaration, not a
	// view. Resolving it would let a server point the sandbox somewhere it was
	// never designed to go.
	for _, uri := range []string{"https://evil.test/app.html", "file:///etc/passwd", "javascript:alert(1)", "data:text/html,<b>"} {
		t.Run("rejects "+uri, func(t *testing.T) {
			if _, ok := ViewRefFromMeta(map[string]any{"ui": map[string]any{"resourceUri": uri}}); ok {
				t.Errorf("%q must not parse as a view reference", uri)
			}
		})
	}
}

func TestViewPolicyFromMeta(t *testing.T) {
	t.Run("reads csp, permissions and hints", func(t *testing.T) {
		policy := ViewPolicyFromMeta(map[string]any{
			"ui": map[string]any{
				"domain":        "reports",
				"prefersBorder": true,
				"permissions":   map[string]any{"camera": map[string]any{}},
				"csp": map[string]any{
					"connectDomains":  []any{"https://api.test"},
					"resourceDomains": []any{"https://cdn.test"},
					"frameDomains":    []any{},
					"baseUriDomains":  []any{},
				},
			},
		})
		if policy.Domain != "reports" || !policy.PrefersBorder {
			t.Errorf("hints = %q/%v", policy.Domain, policy.PrefersBorder)
		}
		if _, ok := policy.Permissions["camera"]; !ok {
			t.Errorf("permissions = %#v", policy.Permissions)
		}
		if !reflect.DeepEqual(policy.ConnectDomains, []string{"https://api.test"}) {
			t.Errorf("ConnectDomains = %#v", policy.ConnectDomains)
		}
		if !policy.DeclaredCSP {
			t.Error("a csp object was present, so DeclaredCSP must be true")
		}
		if policy.Sealed() {
			t.Error("a policy naming domains is not sealed")
		}
	})

	t.Run("an all-empty csp is sealed", func(t *testing.T) {
		policy := ViewPolicyFromMeta(map[string]any{
			"ui": map[string]any{"csp": map[string]any{
				"connectDomains":  []any{},
				"resourceDomains": []any{},
				"frameDomains":    []any{},
				"baseUriDomains":  []any{},
			}},
		})
		if !policy.Sealed() {
			t.Error("a resource declaring no domain in any direction must read as sealed")
		}
	})

	// "Declared as none" and "said nothing" are different statements, and the
	// host answers them differently: it can seal the first and must apply its own
	// default to the second.
	t.Run("no csp object is not sealed", func(t *testing.T) {
		policy := ViewPolicyFromMeta(map[string]any{"ui": map[string]any{"domain": "x"}})
		if policy.DeclaredCSP {
			t.Error("DeclaredCSP must be false when no csp object was carried")
		}
		if policy.Sealed() {
			t.Error("an undeclared policy must not pass for a sealed one")
		}
		if policy.ConnectDomains != nil {
			t.Errorf("ConnectDomains = %#v, want nil", policy.ConnectDomains)
		}
	})

	t.Run("non-string domain members are dropped, not fatal", func(t *testing.T) {
		policy := ViewPolicyFromMeta(map[string]any{
			"ui": map[string]any{"csp": map[string]any{"connectDomains": []any{"https://ok.test", 42, nil}}},
		})
		if !reflect.DeepEqual(policy.ConnectDomains, []string{"https://ok.test"}) {
			t.Errorf("ConnectDomains = %#v", policy.ConnectDomains)
		}
	})

	t.Run("malformed meta yields the zero policy", func(t *testing.T) {
		for name, meta := range map[string]map[string]any{
			"nil":            nil,
			"no ui":          {"other": 1},
			"ui not object":  {"ui": []any{"nope"}},
			"csp not object": {"ui": map[string]any{"csp": "none"}},
		} {
			if got := ViewPolicyFromMeta(meta); got.DeclaredCSP {
				t.Errorf("%s: DeclaredCSP must be false, got %#v", name, got)
			}
		}
	})
}

func TestTrustMayRenderViews(t *testing.T) {
	// Rendering is a larger grant than calling: it puts the server's own document
	// in the operator's browser.
	for _, trust := range []string{TrustTrustedRecipe, TrustTrustedLocal} {
		if !TrustMayRenderViews(trust) {
			t.Errorf("%q is Aura's own infrastructure and must render", trust)
		}
	}
	for _, trust := range []string{TrustSandboxedLocal, TrustRemoteHTTP, TrustBlocked, "", "made-up"} {
		if TrustMayRenderViews(trust) {
			t.Errorf("%q must not render a view", trust)
		}
	}
	if !TrustMayRenderViews("  " + TrustTrustedRecipe + "  ") {
		t.Error("a padded class must still resolve")
	}
}

func TestAppsClientSettings(t *testing.T) {
	settings := AppsClientSettings()
	mimes, ok := settings["mimeTypes"].([]any)
	if !ok || len(mimes) != 1 || mimes[0] != AppMIMEType {
		t.Fatalf("settings = %#v, want the app MIME type a server keys on", settings)
	}
}
