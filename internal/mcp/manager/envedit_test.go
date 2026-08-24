package manager

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// envValue returns the value bound to key in env ("" + false if absent), reading the
// first "KEY=VALUE" entry whose key matches.
func envValue(env []string, key string) (string, bool) {
	for _, entry := range env {
		k, v, ok := cutEnv(entry)
		if ok && k == key {
			return v, true
		}
	}
	return "", false
}

// TestSetServerEnvPreservesUntouchedSecret is the D-05 property at the heart of MCPW-02:
// for a stored secret S under a secret key K, submitting "K=${K}" (the redacted placeholder
// the edit form holds for an untouched secret) preserves S exactly — the stored value is
// NEVER overwritten with the placeholder text "${K}". A real submitted value overwrites,
// and a non-secret value edits in place. Other servers' Env are untouched.
func TestSetServerEnvPreservesUntouchedSecret(t *testing.T) {
	t.Parallel()

	doc := &mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"alpha": {
				Command: "alpha-bin",
				Env:     []string{"TOKEN=real-secret", "PUBLIC_FLAG=on"},
			},
			"beta": {
				Command: "beta-bin",
				Env:     []string{"BETA_TOKEN=beta-secret"},
			},
		},
	}

	// Submit the redacted placeholder for TOKEN (untouched) + a real edit to the non-secret.
	if err := SetServerEnv(doc, "alpha", []string{"TOKEN=${TOKEN}", "PUBLIC_FLAG=off"}); err != nil {
		t.Fatalf("SetServerEnv: %v", err)
	}

	got, _ := envValue(doc.MCPServers["alpha"].Env, "TOKEN")
	if got != "real-secret" {
		t.Fatalf("stored secret must be preserved, got %q (want real-secret — never the ${TOKEN} placeholder)", got)
	}
	if flag, _ := envValue(doc.MCPServers["alpha"].Env, "PUBLIC_FLAG"); flag != "off" {
		t.Fatalf("non-secret edit must take effect, got PUBLIC_FLAG=%q want off", flag)
	}

	// A real submitted secret value overwrites the stored one (rotation).
	if err := SetServerEnv(doc, "alpha", []string{"TOKEN=rotated-secret"}); err != nil {
		t.Fatalf("SetServerEnv rotate: %v", err)
	}
	if got, _ := envValue(doc.MCPServers["alpha"].Env, "TOKEN"); got != "rotated-secret" {
		t.Fatalf("a real submitted secret must overwrite, got %q want rotated-secret", got)
	}

	// The other server is untouched throughout.
	if beta, _ := envValue(doc.MCPServers["beta"].Env, "BETA_TOKEN"); beta != "beta-secret" {
		t.Fatalf("other server Env must be untouched, got BETA_TOKEN=%q", beta)
	}
}

// TestSetServerEnvUnknownServer asserts a 404-style sentinel for an absent server name and
// a nil/empty doc, so the handler can map it to a clean 404 without a panic.
func TestSetServerEnvUnknownServer(t *testing.T) {
	t.Parallel()

	doc := &mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{"alpha": {Command: "a"}}}
	if err := SetServerEnv(doc, "ghost", []string{"K=v"}); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("unknown server = %v, want ErrServerNotFound", err)
	}
	if err := SetServerEnv(&mcp.ManagedConfig{}, "alpha", nil); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("nil servers map = %v, want ErrServerNotFound", err)
	}
	if err := SetServerEnv(nil, "alpha", nil); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("nil doc = %v, want ErrServerNotFound", err)
	}
}

// TestSetServerEnvClearsNonSecret asserts a non-secret env var clears in place when the
// operator submits an empty value.
//
// The second half of this test used to assert the opposite of what it asserts now: that a
// key present in the stored Env but NOT re-submitted was retained. That rule made a typo
// permanent. The editor renders every stored key, so the operator can only ever submit the
// full set — and with retention there was no submission that could mean "remove this",
// leaving a mistyped key sitting in the config with nothing in the cockpit able to delete
// it. Deliberately rewritten per CLAUDE.md: the assertion described the defect.
//
// The submitted list is now authoritative for the keys the form showed, which is the same
// contract a full-form editor has everywhere else in Aura.
func TestSetServerEnvClearsNonSecret(t *testing.T) {
	t.Parallel()

	doc := &mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"alpha": {Command: "a", Env: []string{"FLAG=on", "KEEP=1"}},
		},
	}
	if err := SetServerEnv(doc, "alpha", []string{"FLAG=", "KEEP=1"}); err != nil {
		t.Fatalf("SetServerEnv: %v", err)
	}
	if v, _ := envValue(doc.MCPServers["alpha"].Env, "FLAG"); v != "" {
		t.Fatalf("non-secret clear: FLAG=%q want empty", v)
	}
	if v, ok := envValue(doc.MCPServers["alpha"].Env, "KEEP"); !ok || v != "1" {
		t.Fatalf("a re-submitted key must survive, got KEEP=%q ok=%v", v, ok)
	}
}

// TestSetServerEnvRemovesOmittedKey is the other half of the contract above, and the reason
// it changed: omitting a key deletes it, which is what gives the cockpit a way to undo a
// mistyped variable name.
func TestSetServerEnvRemovesOmittedKey(t *testing.T) {
	t.Parallel()

	doc := &mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"alpha": {Command: "a", Env: []string{"MCP_OAUTH_CLIENT_ID=abc", "MCP_OAUHT_TYPO=oops"}},
		},
	}
	if err := SetServerEnv(doc, "alpha", []string{"MCP_OAUTH_CLIENT_ID=${MCP_OAUTH_CLIENT_ID}"}); err != nil {
		t.Fatalf("SetServerEnv: %v", err)
	}
	env := doc.MCPServers["alpha"].Env
	if _, ok := envValue(env, "MCP_OAUHT_TYPO"); ok {
		t.Fatalf("an omitted key survived, so a typo can never be undone: %v", env)
	}
	// The surviving secret must still hold its real value, not the redacted placeholder.
	if v, ok := envValue(env, "MCP_OAUTH_CLIENT_ID"); !ok || v != "abc" {
		t.Fatalf("re-submitting the placeholder lost the stored secret: %q ok=%v", v, ok)
	}
}
