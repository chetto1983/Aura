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
// Audit A10: the MOUNT's allow-list has named SSL_CERT_FILE and SSL_CERT_DIR since it was
// written, and that substring silently dropped them — two dead entries, and a stdio server
// behind a corporate CA that could not verify TLS. Both paths carry them now.
func TestCABundlePathsReachTheInstallerAndTheMount(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/etc/ssl/corp-ca.pem")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/ssl/corp-ca.pem")

	for _, key := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS"} {
		if v, ok := envValue(t, InstallerEnv(), key); !ok || v != "/etc/ssl/corp-ca.pem" {
			t.Fatalf("InstallerEnv dropped %s, so no install can reach an index behind a custom CA", key)
		}
	}
	if v, ok := envValue(t, processEnvForMCP(nil), "SSL_CERT_FILE"); !ok || v != "/etc/ssl/corp-ca.pem" {
		t.Fatalf("the mount dropped SSL_CERT_FILE, which its own allow-list names; got %q ok=%v", v, ok)
	}
	// NODE_EXTRA_CA_CERTS is the installer's, not the mount's: the mount's list never named it.
	if slices.Contains(envKeys(processEnvForMCP(nil)), "NODE_EXTRA_CA_CERTS") {
		t.Fatal("the mount widened beyond the keys its allow-list declares")
	}
}

// Audit A9: a host carrying both spellings of a proxy variable used to get whichever
// os.Environ listed first. The canonical spelling — the one the allow-lists are written in —
// wins, whichever order they arrive in.
func TestDuplicateSpellingsResolveToTheCanonicalKey(t *testing.T) {
	t.Setenv("https_proxy", "http://lower:3128")
	t.Setenv("HTTPS_PROXY", "http://upper:3128")

	env := InstallerEnv()
	if v, ok := envValue(t, env, "HTTPS_PROXY"); !ok || v != "http://upper:3128" {
		t.Fatalf("proxy = %q ok=%v, want the upper-case spelling to win", v, ok)
	}
	if n := len(slices.DeleteFunc(envKeys(env), func(k string) bool { return k != "HTTPS_PROXY" })); n != 1 {
		t.Fatalf("HTTPS_PROXY appears %d times; a child must not see two values for one key", n)
	}
}
