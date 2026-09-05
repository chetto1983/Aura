package mcp

import (
	"reflect"
	"strings"
	"testing"
)

func TestManagedConfigRoundTripFiltersDisabled(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"calculator": {
			Command: "uvx",
			Args:    []string{"calculator-mcp-server", "--stdio"},
			Env:     []string{"PYTHONUNBUFFERED=1"},
		},
		"calendar": {
			Command: "dotnet",
			Args:    []string{"Calendar.Mcp.dll"},
			Enabled: new(false),
		},
	}}

	got := doc
	if err := PrepareForWrite(&got); err != nil {
		t.Fatalf("PrepareForWrite: %v", err)
	}
	if !reflect.DeepEqual(got.MCPServers["calculator"].Args, doc.MCPServers["calculator"].Args) {
		t.Fatalf("calculator args = %#v, want %#v", got.MCPServers["calculator"].Args, doc.MCPServers["calculator"].Args)
	}
	enabled, err := got.EnabledServers()
	if err != nil {
		t.Fatalf("EnabledServers: %v", err)
	}
	if _, ok := enabled["calendar"]; ok {
		t.Fatal("disabled calendar server should not be returned")
	}
	if enabled["calculator"].Command != "uvx" {
		t.Fatalf("enabled calculator command = %q, want uvx", enabled["calculator"].Command)
	}
}

// The docker kinds used to be the reason a stdio entry could carry no local Command.
// Amendment #209 retired them, so the write path now refuses such an entry as an unknown
// runtime kind rather than storing a row nothing can launch.
func TestManagedConfigRefusesRetiredDockerRuntime(t *testing.T) {
	for _, kind := range []string{"docker", "docker_gateway"} {
		doc := ManagedConfig{MCPServers: map[string]ManagedServer{
			"third-party": {
				Source:  "manual",
				Trust:   ManagedTrust{Class: TrustSandboxedLocal},
				Runtime: ManagedRuntime{Kind: kind, Command: []string{"server", "--stdio"}},
			},
		}}
		err := PrepareForWrite(&doc)
		if err == nil || !strings.Contains(err.Error(), "unknown runtime kind") {
			t.Fatalf("PrepareForWrite(kind=%q) = %v, want an unknown-runtime-kind refusal", kind, err)
		}
	}
}

// A document that names no version and no profile still has to resolve to the current
// version and the default profile — a row stored before either field existed must not come
// back unmountable.
func TestManagedConfigLegacyNormalizesVersionAndProfile(t *testing.T) {
	got := ManagedConfig{MCPServers: map[string]ManagedServer{
		"legacy": {Command: "node", Args: []string{"server.js"}, Env: []string{"API_TOKEN=secret"}},
	}}
	Normalize(&got)
	if got.Version != 2 {
		t.Fatalf("Version = %d, want 2", got.Version)
	}
	if got.ActiveProfileName() != DefaultMCPProfile {
		t.Fatalf("ActiveProfileName = %q, want %q", got.ActiveProfileName(), DefaultMCPProfile)
	}
	if got.MCPServers["legacy"].Command != "node" {
		t.Fatalf("legacy command = %q, want node", got.MCPServers["legacy"].Command)
	}
}

func TestManagedConfigTrustDefaults(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"recipe":  {Command: "uvx", Source: "recipe:mail"},
		"manual":  {Command: "npx", Source: "manual"},
		"trusted": {Command: "npx", Trust: ManagedTrust{Class: TrustTrustedLocal}},
	}}

	if got := doc.NormalizedTrust("recipe"); got != TrustTrustedRecipe {
		t.Fatalf("recipe trust = %q, want %q", got, TrustTrustedRecipe)
	}
	// A hand-added stdio server resolves from its transport. It used to resolve to
	// TrustBlocked, which only meant the operator who wrote the entry had to approve it
	// again somewhere else.
	if got := doc.NormalizedTrust("manual"); got != TrustTrustedLocal {
		t.Fatalf("manual trust = %q, want %q", got, TrustTrustedLocal)
	}
	if got := doc.NormalizedTrust("trusted"); got != TrustTrustedLocal {
		t.Fatalf("trusted trust = %q, want %q", got, TrustTrustedLocal)
	}
}

func TestManagedConfigProfileMembership(t *testing.T) {
	doc := ManagedConfig{
		Profiles: map[string]ManagedProfile{
			"work": {Servers: []string{"calendar", "mail"}},
		},
		MCPServers: map[string]ManagedServer{
			"calendar": {Command: "calendar-mcp", Source: "recipe:calendar"},
			"mail":     {Command: "mail-mcp", Source: "recipe:mail"},
			"other":    {Command: "other-mcp", Source: "recipe:other"},
		},
	}

	got := doc.ProfileServerNames("work")
	want := []string{"calendar", "mail"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileServerNames(work) = %#v, want %#v", got, want)
	}
}

func TestPrepareForWriteRejectsInvalidServer(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"broken": {Type: ServerTypeStdio}, // missing command for local runtime
	}}
	if err := PrepareForWrite(&doc); err == nil || !strings.Contains(err.Error(), "command cannot be empty") {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestActiveProfileNameExplicitAndTrimmed(t *testing.T) {
	if got := (ManagedConfig{ActiveProfile: "  work  "}).ActiveProfileName(); got != "work" {
		t.Fatalf("ActiveProfileName = %q, want work", got)
	}
	if got := (ManagedConfig{}).ActiveProfileName(); got != DefaultMCPProfile {
		t.Fatalf("ActiveProfileName(empty) = %q, want %q", got, DefaultMCPProfile)
	}
}

func TestProfileServerNamesDedupAndFilters(t *testing.T) {
	doc := ManagedConfig{
		Profiles: map[string]ManagedProfile{
			"work": {Servers: []string{"calendar", "  ", "calendar", "ghost", "mail"}},
		},
		MCPServers: map[string]ManagedServer{
			"calendar": {Command: "cal"},
			"mail":     {Command: "mail"},
		},
	}
	// "ghost" is not a known server and "calendar" is duplicated; both pruned.
	got := doc.ProfileServerNames("work")
	want := []string{"calendar", "mail"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileServerNames = %#v, want %#v", got, want)
	}
}

func TestProfileServerNamesFallsBackToEnabledServers(t *testing.T) {
	doc := ManagedConfig{
		ActiveProfile: "undefined-profile",
		MCPServers: map[string]ManagedServer{
			"a": {Command: "a"},
			"b": {Command: "b", Enabled: new(false)},
			"c": {Command: "c"},
		},
	}
	// No matching profile -> all enabled servers (disabled "b" excluded), sorted.
	got := doc.ProfileServerNames("")
	want := []string{"a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileServerNames(fallback) = %#v, want %#v", got, want)
	}
}

func TestNormalizedTrustUnknownServerIsBlocked(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{}}
	if got := doc.NormalizedTrust("missing"); got != TrustBlocked {
		t.Fatalf("unknown server trust = %q, want %q", got, TrustBlocked)
	}
}

// TestNormalizedTrustRemoteHTTPInferred previously asserted the F-013 bug: an
// explicitly-typed streamable_http server with no trust class set was
// silently auto-promoted to the runnable TrustRemoteHTTP class, defeating the
// trust-approval workflow for any bare-URL remote entry. D-03/Classify closes
// this -- the fixed behavior is TrustBlocked: explicit trust is required for
// every runnable remote transport. Deliberately rewritten per CLAUDE.md
// ("never modify tests to make them pass unless the test itself is broken");
// this test's OLD assertion described exactly the bug being fixed.
func TestNormalizedTrustRemoteHTTPInferred(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"remote":  {Type: ServerTypeStreamableHTTP, URL: "https://example.test/mcp"},
		"blocked": {Type: ServerTypeStreamableHTTP, URL: "https://example.test/mcp", Trust: ManagedTrust{Class: TrustBlocked}},
	}}
	if got := doc.NormalizedTrust("remote"); got != TrustRemoteHTTP {
		t.Fatalf("remote http trust with no explicit class = %q, want %q", got, TrustRemoteHTTP)
	}
	// The escape hatch has to keep working, or "no ceremony" would have quietly become
	// "no way to block anything".
	if got := doc.NormalizedTrust("blocked"); got != TrustBlocked {
		t.Fatalf("explicitly blocked remote = %q, want %q", got, TrustBlocked)
	}
}

func TestValidateManagedServers(t *testing.T) {
	cases := []struct {
		name    string
		in      map[string]ManagedServer
		wantErr string
	}{
		{"empty-name", map[string]ManagedServer{"  ": {Command: "x"}}, "name cannot be empty"},
		{
			"stdio-local-no-command",
			map[string]ManagedServer{"s": {Type: ServerTypeStdio}},
			"command cannot be empty",
		},
		{
			"retired-docker-kind",
			map[string]ManagedServer{"s": {Runtime: ManagedRuntime{Kind: "docker"}}},
			"unknown runtime kind",
		},
		{
			"retired-docker-gateway-kind",
			map[string]ManagedServer{"s": {Runtime: ManagedRuntime{Kind: "docker_gateway"}}},
			"unknown runtime kind",
		},
		{
			"unknown-runtime-kind",
			map[string]ManagedServer{"s": {Command: "x", Runtime: ManagedRuntime{Kind: "vm"}}},
			"unknown runtime kind",
		},
		{
			"http-no-url",
			map[string]ManagedServer{"s": {Type: ServerTypeStreamableHTTP}},
			"url cannot be empty",
		},
		{
			"unknown-type",
			map[string]ManagedServer{"s": {Type: "grpc", Command: "x"}},
			"unknown type",
		},
		{
			"unknown-trust-class",
			map[string]ManagedServer{"s": {Command: "x", Trust: ManagedTrust{Class: "bogus"}}},
			"unknown trust class",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManagedServers(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateManagedServers err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateManagedServersAcceptsValid(t *testing.T) {
	in := map[string]ManagedServer{
		"local":   {Command: "uvx"},
		"http":    {Type: ServerTypeStreamableHTTP, URL: "https://x.test"},
		"trusted": {Command: "x", Trust: ManagedTrust{Class: TrustTrustedLocal}},
	}
	if err := validateManagedServers(in); err != nil {
		t.Fatalf("validateManagedServers(valid) = %v, want nil", err)
	}
}

func TestNormalizedRuntimeKind(t *testing.T) {
	cases := []struct {
		in   ManagedRuntime
		want string
	}{
		{ManagedRuntime{}, RuntimeKindLocal},
		{ManagedRuntime{Kind: "  "}, RuntimeKindLocal},
		{ManagedRuntime{Kind: RuntimeKindLocal}, RuntimeKindLocal},
		{ManagedRuntime{Kind: "  custom  "}, "custom"},
		// A retired kind normalizes to itself and is refused downstream, rather than
		// being silently folded into local (amendment #209).
		{ManagedRuntime{Kind: "docker"}, "docker"},
		{ManagedRuntime{Kind: "docker_gateway"}, "docker_gateway"},
	}
	for _, tc := range cases {
		if got := normalizedRuntimeKind(ManagedServer{Runtime: tc.in}); got != tc.want {
			t.Fatalf("normalizedRuntimeKind(%q) = %q, want %q", tc.in.Kind, got, tc.want)
		}
	}
}

// TestNormalizedServerType exercises normalizedServerType, the thin wrapper
// around Classify (D-01). Two cases previously encoded now-fixed bugs and were
// deliberately rewritten per CLAUDE.md's "rewrite the test with explicit
// justification" rule:
//   - "url-and-command-infers-stdio" previously asserted F-027 (a mixed
//     url+command entry silently resolved to stdio); normalizedServerType's
//     error-free string signature cannot itself surface Classify's rejection,
//     so the fixed behavior is asserted directly against Classify below
//     (callers that must observe the rejection -- OpenServer,
//     validateManagedServers -- dispatch through Classify directly, not
//     through this wrapper; see transport_test.go and TestValidateManagedServers
//     RejectsMixedTransport).
//   - "explicit-custom-trimmed" previously asserted that an arbitrary
//     non-empty type string (e.g. "weird") passed through verbatim. Classify's
//     single source of truth now treats any unrecognized explicit type as a
//     hard error instead of a silent passthrough (D-01).
func TestNormalizedServerType(t *testing.T) {
	cases := []struct {
		name string
		in   ManagedServer
		want string
	}{
		{"url-only-infers-http", ManagedServer{URL: "https://x.test"}, ServerTypeStreamableHTTP},
		{"command-only-infers-stdio", ManagedServer{Command: "uvx"}, ServerTypeStdio},
		{"bare-infers-stdio", ManagedServer{}, ServerTypeStdio},
		{"explicit-stdio", ManagedServer{Type: ServerTypeStdio}, ServerTypeStdio},
		{"explicit-http", ManagedServer{Type: ServerTypeStreamableHTTP}, ServerTypeStreamableHTTP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizedServerType(tc.in); got != tc.want {
				t.Fatalf("normalizedServerType = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("url-and-command-is-rejected-not-silently-stdio", func(t *testing.T) {
		if _, _, err := Classify(ManagedServer{URL: "https://x.test", Command: "uvx"}); err == nil {
			t.Fatal("Classify(mixed url+command) = nil error, want rejection (F-027 fixed)")
		}
	})

	t.Run("explicit-custom-type-is-rejected-not-passed-through", func(t *testing.T) {
		if _, _, err := Classify(ManagedServer{Type: "  weird  ", Command: "uvx"}); err == nil {
			t.Fatal("Classify(unknown explicit type) = nil error, want rejection")
		}
	})
}

// TestValidateManagedServersRejectsMixedTransport is the F-027 regression
// guard at the config-validation gate: a mixed url+command server (no
// explicit type) must fail validateManagedServers via Classify's rejection,
// so it can never be saved to disk and never reaches OpenServer.
func TestValidateManagedServersRejectsMixedTransport(t *testing.T) {
	in := map[string]ManagedServer{
		"mixed": {URL: "https://x.test", Command: "uvx"},
	}
	if err := validateManagedServers(in); err == nil || !strings.Contains(err.Error(), "both url and command") {
		t.Fatalf("validateManagedServers(mixed) = %v, want rejection containing %q", err, "both url and command")
	}
}

func TestEnabledServersExcludesHTTPAndEmptyIsNil(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"remote": {Type: ServerTypeStreamableHTTP, URL: "https://x.test"},
	}}
	enabled, err := doc.EnabledServers()
	if err != nil {
		t.Fatalf("EnabledServers: %v", err)
	}
	if enabled != nil {
		t.Fatalf("HTTP-only registry should yield nil, got %#v", enabled)
	}
}

func TestEnabledServersValidationError(t *testing.T) {
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"bad": {Type: "grpc", Command: "x"},
	}}
	if _, err := doc.EnabledServers(); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestNormalizeFillsNilMaps(t *testing.T) {
	got := ManagedConfig{ActiveProfile: "work"}
	Normalize(&got)
	if got.MCPServers == nil {
		t.Fatal("MCPServers should be normalized to non-nil map")
	}
	if got.Profiles == nil {
		t.Fatal("Profiles should be normalized to non-nil map")
	}
	if got.Version != ManagedConfigVersion {
		t.Fatalf("Version = %d, want %d", got.Version, ManagedConfigVersion)
	}
}
