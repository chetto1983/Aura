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
	env := make([]string, 0, len(configured)+8)
	seen := map[string]struct{}{}
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !mcpInheritedEnvKey(k) || secret.IsSecretEnvKey(k) {
			continue
		}
		upper := strings.ToUpper(k)
		if _, dup := seen[upper]; dup {
			continue
		}
		seen[upper] = struct{}{}
		env = append(env, kv)
	}
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
