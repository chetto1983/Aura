package mcp

import (
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/secret"
)

// process_env.go builds the environment a stdio MCP subprocess is launched with.
// It is relocated off client.go (deleted in plan 45.1-03) because the SDK's
// CommandTransport takes an *exec.Cmd Aura still has to populate: the SDK owns the
// wire, never the child's environment.
//
// The child does NOT inherit Aura's environment wholesale. Only the keys below cross
// over, and a key that IsSecretEnvKey recognises is dropped even when it is on that
// list — an operator's explicit Env entry is the one supported way to hand a server
// a credential.

func processEnvForMCP(configured []string) []string {
	env, seen := inheritedEnv(mcpInheritedEnvKey, func(k, _ string) bool {
		return !caBundleEnvKey(k) && secret.IsSecretEnvKey(k)
	})
	for _, kv := range configured {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		upper := strings.ToUpper(k)
		if _, dup := seen[upper]; dup {
			env = replaceEnv(env, k, kv)
		} else {
			env = append(env, kv)
		}
		seen[upper] = struct{}{}
	}
	return env
}

// inheritedEnv collects the keys allow reports, dropping every pair deny recognises, and
// reports what it kept so a caller can merge its own entries over it. deny is a parameter
// rather than a constant because the two callers need different strengths: the mount keeps
// the key-only predicate it has always used, and the installer needs the value-aware one so a
// credential inside an otherwise innocuous key does not cross.
func inheritedEnv(allow func(string) bool, deny func(key, value string) bool) ([]string, map[string]struct{}) {
	env := make([]string, 0, 16)
	seen := map[string]struct{}{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !allow(k) || deny(k, v) {
			continue
		}
		upper := strings.ToUpper(k)
		if _, dup := seen[upper]; dup {
			// A host commonly carries both spellings of a proxy variable. Keeping whichever
			// os.Environ happened to list first made the value a coin flip, so the canonical
			// spelling — the one the allow-lists are written in — wins (audit A9).
			if k == upper {
				env = replaceEnv(env, k, kv)
			}
			continue
		}
		seen[upper] = struct{}{}
		env = append(env, kv)
	}
	return env, seen
}

// InstallerEnv is the environment a PACKAGE INSTALLER runs with — uv and npm, in the #211
// prepare step. It exists for the same reason processEnvForMCP does, and it matters more:
// `uv pip install` and `npm install` execute the package's OWN code (setup.py, npm lifecycle
// scripts), so an installer inheriting Aura's environment hands every secret in it to
// whatever the operator just chose to install. Measured 2026-09-05 (audit A1): it did — the
// prepare runner set no Env at all, so POSTGRES_PASSWORD, AURA_AUTHULA_SECRET and the rest
// crossed over while the MOUNT of the very same server was narrowed to fourteen keys.
//
// It is the mount's list plus the proxy keys, because a resolver has to reach an index and a
// mounted server does not. A credential-bearing value is still withheld: IsSecretEnvVar reads
// the VALUE, so `https://user:pw@proxy` does not cross even though HTTPS_PROXY looks
// innocuous — an install behind such a proxy fails loudly rather than leaking quietly.
func InstallerEnv() []string {
	env, _ := inheritedEnv(installerInheritedEnvKey, func(k, v string) bool {
		return !caBundleEnvKey(k) && secret.IsSecretEnvVar(k, v)
	})
	return env
}

func installerInheritedEnvKey(key string) bool {
	switch strings.ToUpper(key) {
	case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY":
		return true
	default:
		return caBundleEnvKey(key) || mcpInheritedEnvKey(key)
	}
}

// caBundleEnvKey names the variables that point an installer at a CA bundle. They are exempt
// from the secret predicate, which matches the substring "cert" and would otherwise drop every
// one of them. That is not over-caution being relaxed: these hold a PATH to a public trust
// store, never a private key, and without them a host behind a TLS-inspecting proxy or a
// corporate CA cannot install anything. Measured 2026-09-05: with SSL_CERT_FILE withheld,
// `uv pip install` failed with "invalid peer certificate: UnknownIssuer" before reaching PyPI.
//
// The MOUNT applies the same exemption (audit A10). Its allow-list has NAMED SSL_CERT_FILE and
// SSL_CERT_DIR since it was written, and that same substring silently dropped them, so a stdio
// server behind a corporate CA could never verify TLS and two entries in that list did
// nothing. This restores the intent the list already declared; it does not widen it.
func caBundleEnvKey(key string) bool {
	switch strings.ToUpper(key) {
	case "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE":
		return true
	default:
		return false
	}
}

func mcpInheritedEnvKey(key string) bool {
	switch strings.ToUpper(key) {
	case "PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "COMSPEC", "HOME", "USERPROFILE", "TMP", "TEMP", "TMPDIR", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR":
		return true
	default:
		return false
	}
}

func replaceEnv(env []string, key, kv string) []string {
	want := strings.ToUpper(key)
	for i, existing := range env {
		k, _, ok := strings.Cut(existing, "=")
		if ok && strings.ToUpper(k) == want {
			env[i] = kv
			return env
		}
	}
	return append(env, kv)
}
