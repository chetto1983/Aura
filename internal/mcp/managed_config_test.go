package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestManagedConfigPathUsesOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "servers.json")
	t.Setenv("AURA_MCP_CONFIG", want)

	got, err := ManagedConfigPath()
	if err != nil {
		t.Fatalf("ManagedConfigPath: %v", err)
	}
	if got != want {
		t.Fatalf("ManagedConfigPath = %q, want %q", got, want)
	}
}

func TestManagedConfigRoundTripFiltersDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"calculator": {
			Command: "uvx",
			Args:    []string{"calculator-mcp-server", "--stdio"},
			Env:     []string{"PYTHONUNBUFFERED=1"},
		},
		"calendar": {
			Command: "dotnet",
			Args:    []string{"Calendar.Mcp.dll"},
			Enabled: boolPtr(false),
		},
	}}

	if err := SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("SaveManagedConfig: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat saved config: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("saved mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
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

func TestManagedConfigDockerRuntimeDoesNotRequireLocalCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"third-party": {
			Source: "manual",
			Trust:  ManagedTrust{Class: TrustSandboxedLocal},
			Runtime: ManagedRuntime{
				Kind:    RuntimeKindDocker,
				Image:   "example/mcp:1",
				Command: []string{"server", "--stdio"},
			},
		},
	}}

	if err := SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("SaveManagedConfig: %v", err)
	}
	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
	if got.MCPServers["third-party"].Command != "" {
		t.Fatalf("local command = %q, want empty for docker runtime", got.MCPServers["third-party"].Command)
	}
}

func TestManagedConfigLegacyLoadsWithDefaultVersionAndProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "legacy": {
      "command": "node",
      "args": ["server.js"],
      "env": ["API_TOKEN=secret"]
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
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
	if got := doc.NormalizedTrust("manual"); got != TrustBlocked {
		t.Fatalf("manual trust = %q, want %q", got, TrustBlocked)
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

func TestManagedConfigPathDefaultUsesHome(t *testing.T) {
	t.Setenv("AURA_MCP_CONFIG", "")
	home := t.TempDir()
	// HOME (POSIX) / USERPROFILE (Windows) back os.UserHomeDir.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := ManagedConfigPath()
	if err != nil {
		t.Fatalf("ManagedConfigPath: %v", err)
	}
	want := filepath.Join(home, ".aura", "mcp", "servers.json")
	if got != want {
		t.Fatalf("ManagedConfigPath = %q, want %q", got, want)
	}
}

func TestManagedConfigPathBlankOverrideFallsBack(t *testing.T) {
	t.Setenv("AURA_MCP_CONFIG", "   ")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := ManagedConfigPath()
	if err != nil {
		t.Fatalf("ManagedConfigPath: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(got), ".aura/mcp/servers.json") {
		t.Fatalf("blank override should fall back to home path, got %q", got)
	}
}

func TestLoadManagedConfigMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig(missing): %v", err)
	}
	if got.MCPServers == nil || len(got.MCPServers) != 0 {
		t.Fatalf("missing config should yield empty registry, got %#v", got.MCPServers)
	}
}

func TestLoadManagedConfigEmptyFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("   \n\t"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig(empty): %v", err)
	}
	if got.MCPServers == nil || len(got.MCPServers) != 0 {
		t.Fatalf("empty file should yield empty registry, got %#v", got.MCPServers)
	}
}

func TestLoadManagedConfigMalformedErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadManagedConfig(path); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("want parse error, got %v", err)
	}
}

func TestSaveManagedConfigRejectsInvalidServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"broken": {Type: ServerTypeStdio}, // missing command for local runtime
	}}
	if err := SaveManagedConfig(path, doc); err == nil || !strings.Contains(err.Error(), "command cannot be empty") {
		t.Fatalf("want validation error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid config should not be written, stat err = %v", err)
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
			"b": {Command: "b", Enabled: boolPtr(false)},
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
		"remote": {Type: ServerTypeStreamableHTTP, URL: "https://example.test/mcp"},
	}}
	if got := doc.NormalizedTrust("remote"); got != TrustBlocked {
		t.Fatalf("remote http trust with no explicit class = %q, want %q (F-013: must not auto-promote to %q)",
			got, TrustBlocked, TrustRemoteHTTP)
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
			"docker-no-image",
			map[string]ManagedServer{"s": {Runtime: ManagedRuntime{Kind: RuntimeKindDocker}}},
			"docker image cannot be empty",
		},
		{
			"docker-gateway-no-profile",
			map[string]ManagedServer{"s": {Runtime: ManagedRuntime{Kind: RuntimeKindDockerGateway}}},
			"docker gateway profile cannot be empty",
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
		"docker":  {Runtime: ManagedRuntime{Kind: RuntimeKindDocker, Image: "img:1"}},
		"gateway": {Runtime: ManagedRuntime{Kind: RuntimeKindDockerGateway, Profile: "p"}},
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
		{ManagedRuntime{Kind: RuntimeKindDocker}, RuntimeKindDocker},
		{ManagedRuntime{Kind: RuntimeKindDockerGateway}, RuntimeKindDockerGateway},
		{ManagedRuntime{Kind: "  custom  "}, "custom"},
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

func TestLoadManagedConfigReadErrorOnDirectory(t *testing.T) {
	// Pointing at a directory makes os.ReadFile fail with a non-NotExist error.
	dir := t.TempDir()
	if _, err := LoadManagedConfig(dir); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("want read error for directory path, got %v", err)
	}
}

func TestLoadManagedConfigNormalizesNilMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	// JSON with no mcpServers/profiles keys exercises the nil-map normalization.
	if err := os.WriteFile(path, []byte(`{"activeProfile":"work"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
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

func boolPtr(v bool) *bool { return &v }
