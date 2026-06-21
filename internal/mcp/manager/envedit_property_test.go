package manager

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/secret"
	"pgregory.net/rapid"
)

// envedit_property_test.go is the redacted-placeholder-preserved PROPERTY backstop
// (SPEC Prohibition #2 / T-29-05). The "no overwrite on a redacted placeholder" guarantee
// cannot be inferred from a single example — it must hold for EVERY secret key. This rapid
// property generates many (key, storedSecret) pairs and proves: after SetServerEnv submits
// the redacted ${KEY} placeholder, the stored value is EXACTLY the original secret, never
// the placeholder text "${KEY}".

// secretMarkers mirrors internal/secret.secretEnvMarkers (the substrings that make a key
// secret). A generated key always embeds one so secret.IsSecretEnvKey is true by
// construction — the preserve branch only fires for secret keys.
var secretMarkers = []string{
	"KEY", "TOKEN", "SECRET", "PASS", "AUTH", "BEARER", "CREDENTIAL", "PWD", "JWT", "DSN",
}

// genSecretKey produces a syntactically valid, always-secret env key (UPPER + a marker).
func genSecretKey(t *rapid.T) string {
	marker := rapid.SampledFrom(secretMarkers).Draw(t, "marker")
	prefix := rapid.StringMatching(`[A-Z][A-Z0-9_]{0,12}`).Draw(t, "prefix")
	key := prefix + "_" + marker
	if !secret.IsSecretEnvKey(key) {
		t.Fatalf("generated key %q is not secret — generator invariant broken", key)
	}
	return key
}

// genSecretValue produces a non-empty real secret value that is NOT a placeholder for key.
func genSecretValue(t *rapid.T, key string) string {
	v := rapid.StringMatching(`[a-zA-Z0-9._:/+-]{1,40}`).Draw(t, "value")
	if strings.TrimSpace(v) == "" || v == "${"+key+"}" {
		v = "real-" + v + "-secret"
	}
	return v
}

// TestSetServerEnvPreservesAllSecrets is the property: for ALL secret keys K and stored
// secrets S, submitting K=${K} preserves S exactly (never "${K}", never empty). It also
// asserts a re-submitted REAL value rotates (the four-state's other branch) so the property
// is not satisfied by a degenerate always-preserve.
func TestSetServerEnvPreservesAllSecrets(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		key := genSecretKey(t)
		stored := genSecretValue(t, key)

		doc := &mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
			"srv": {Command: "srv-bin", Env: []string{key + "=" + stored}},
		}}

		// Submit the redacted placeholder — the stored secret MUST be preserved exactly.
		placeholder := "${" + key + "}"
		if err := SetServerEnv(doc, "srv", []string{key + "=" + placeholder}); err != nil {
			t.Fatalf("SetServerEnv(placeholder): %v", err)
		}
		got, ok := envValue(doc.MCPServers["srv"].Env, key)
		if !ok {
			t.Fatalf("key %q vanished after a placeholder edit", key)
		}
		if got == placeholder {
			t.Fatalf("key %q stored the placeholder text %q — the secret was overwritten (Prohibition #2 VIOLATED)", key, placeholder)
		}
		if got != stored {
			t.Fatalf("key %q = %q after placeholder edit, want the preserved secret %q", key, got, stored)
		}

		// Submit a DIFFERENT real value — it MUST rotate (the four-state's overwrite branch),
		// proving the preserve is conditional on the placeholder, not a blanket keep.
		rotated := "rotated-" + stored + "-x"
		if err := SetServerEnv(doc, "srv", []string{key + "=" + rotated}); err != nil {
			t.Fatalf("SetServerEnv(rotate): %v", err)
		}
		if got, _ := envValue(doc.MCPServers["srv"].Env, key); got != rotated {
			t.Fatalf("key %q = %q after a real-value edit, want the rotated value %q (rotation must take effect)", key, got, rotated)
		}
	})
}
