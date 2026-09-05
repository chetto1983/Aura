package mcp

import (
	"slices"
	"strings"
	"testing"
)

func envKeys(env []string) []string {
	keys := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		keys = append(keys, strings.ToUpper(k))
	}
	return keys
}

func envValue(t *testing.T, env []string, key string) (string, bool) {
	t.Helper()
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

// Audit A1, 2026-09-05: `uv pip install` and `npm install` run the package's own setup code,
// and the prepare step used to launch them with no Env at all — so every secret Aura holds
// crossed into whatever the operator had just chosen to install, while the MOUNT of that same
// server was already narrowed. This is the regression test for the narrowing.
func TestInstallerEnvWithholdsAurasSecrets(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("POSTGRES_PASSWORD", "hunter2")
	t.Setenv("AURA_AUTHULA_SECRET", "deadbeef")
	t.Setenv("OPENROUTER_API_KEY", "sk-live")
	t.Setenv("AURA_EMBED_DIMENSIONS", "768") // innocuous, but not an installer's business

	keys := envKeys(InstallerEnv())
	for _, unwanted := range []string{"POSTGRES_PASSWORD", "AURA_AUTHULA_SECRET", "OPENROUTER_API_KEY", "AURA_EMBED_DIMENSIONS"} {
		if slices.Contains(keys, unwanted) {
			t.Fatalf("InstallerEnv carries %s; keys = %v", unwanted, keys)
		}
	}
	if !slices.Contains(keys, "PATH") {
		t.Fatalf("InstallerEnv dropped PATH, so no resolver could be found; keys = %v", keys)
	}
}

// The installer's list is the mount's plus the proxy keys: a resolver has to reach an index,
// a mounted server does not. A credential inside the proxy URL is still withheld — the value
// is read, not only the key — so an install behind such a proxy fails loudly.
// Both cases are set on purpose: a Unix deployment commonly carries https_proxy AND
// HTTPS_PROXY, the collector dedupes case-insensitively, and it keeps the FIRST it meets — so
// a test that set only one would assert whichever os.Environ happened to list first. Setting
// both is what a real host looks like and makes the assertion independent of that order.
func setProxy(t *testing.T, value string) {
	t.Helper()
	t.Setenv("HTTPS_PROXY", value)
	t.Setenv("https_proxy", value)
}

func TestInstallerEnvAddsProxiesButNotTheirCredentials(t *testing.T) {
	setProxy(t, "http://proxy.internal:3128")
	if v, ok := envValue(t, InstallerEnv(), "HTTPS_PROXY"); !ok || v != "http://proxy.internal:3128" {
		t.Fatalf("InstallerEnv should carry a plain proxy; got %q ok=%v", v, ok)
	}
	if slices.Contains(envKeys(processEnvForMCP(nil)), "HTTPS_PROXY") {
		t.Fatal("the mount inherited a proxy key it has never carried")
	}

	setProxy(t, "http://user:pw@proxy.internal:3128")
	if _, ok := envValue(t, InstallerEnv(), "HTTPS_PROXY"); ok {
		t.Fatal("InstallerEnv carried a proxy URL with embedded credentials")
	}
}

// The mount's own policy, asserted directly rather than only through a live session: the
// operator's explicit Env entry is the one supported way to hand a server a credential.
func TestProcessEnvForMCPNarrowsAndLetsConfiguredEntriesWin(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("AURA_AUTHULA_SECRET", "deadbeef")

	env := processEnvForMCP([]string{"PATH=/opt/custom", "SERVER_TOKEN=given-on-purpose"})
	if v, _ := envValue(t, env, "PATH"); v != "/opt/custom" {
		t.Fatalf("configured PATH did not win; got %q", v)
	}
	if v, ok := envValue(t, env, "SERVER_TOKEN"); !ok || v != "given-on-purpose" {
		t.Fatalf("an explicitly configured credential must cross; got %q ok=%v", v, ok)
	}
	if slices.Contains(envKeys(env), "AURA_AUTHULA_SECRET") {
		t.Fatal("the mount inherited an Aura secret")
	}
}

// Measured live 2026-09-05: with SSL_CERT_FILE withheld, `uv pip install` never reached PyPI —
// "invalid peer certificate: UnknownIssuer". A CA-bundle variable holds a PATH to a public
// trust store, so it is exempt from the secret predicate that matches the substring "cert".
//
// The second half pins audit A10 rather than fixing it: the mount's allow-list names
// SSL_CERT_FILE and SSL_CERT_DIR, and that same substring has always dropped them, so they
// have never actually crossed to a mounted server. This test fails the day that changes, which
// is when someone has decided it deliberately.
func TestCABundlePathsReachTheInstallerButNotTheMount(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/etc/ssl/corp-ca.pem")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/ssl/corp-ca.pem")

	for _, key := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS"} {
		if v, ok := envValue(t, InstallerEnv(), key); !ok || v != "/etc/ssl/corp-ca.pem" {
			t.Fatalf("InstallerEnv dropped %s, so no install can reach an index behind a custom CA", key)
		}
	}
	if slices.Contains(envKeys(processEnvForMCP(nil)), "SSL_CERT_FILE") {
		t.Fatal("the mount now carries SSL_CERT_FILE — audit A10 was decided; update this test and the amendment")
	}
}
