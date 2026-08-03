package tools

import (
	"strings"
	"testing"
)

// The child environment of a shell_exec call. This file used to test mergeEnv, which copied the
// Aura process environment into the child after dropping secret-shaped entries — a filter that had
// to be right, because everything it missed reached the command. boxEnv replaces it with a
// stronger property: NO host variable is copied at all. What still needs proving is that the
// property holds and that the caller's own overrides survive. The backend re-scrubs secret-shaped
// caller overrides on its way in (usersandbox.scrubEnv, tested there).

func TestBoxEnv_NeverCarriesTheHostEnvironment(t *testing.T) {
	t.Setenv("AURA_DB_URL", "postgres://user:pass@localhost:5432/aura")
	t.Setenv("PUBLIC_URL", "https://example.com/app")
	t.Setenv("AURA_PUBLIC_FLAG", "visible")

	env := boxEnv(nil)
	for _, key := range []string{"AURA_DB_URL", "PUBLIC_URL", "AURA_PUBLIC_FLAG"} {
		if envHasKey(env, key) {
			t.Fatalf("boxEnv copied host var %q into the box: %v", key, env)
		}
	}
	// Only the UTF-8 defaults, so a model-written python script with symbols in its progress
	// output does not die on an ASCII default encoding.
	if got, ok := envValue(env, "PYTHONUTF8"); !ok || got != "1" {
		t.Fatalf("boxEnv PYTHONUTF8 = %q, %v; want 1", got, ok)
	}
	if len(env) != 2 {
		t.Fatalf("boxEnv(nil) = %v, want only the UTF-8 defaults", env)
	}
}

func TestBoxEnv_CarriesCallerOverrides(t *testing.T) {
	env := boxEnv(map[string]string{"BUILD_TAG": "v1", "PUBLIC_URL": "https://example.com/app"})
	if got, ok := envValue(env, "BUILD_TAG"); !ok || got != "v1" {
		t.Fatalf("boxEnv BUILD_TAG = %q, %v; want v1", got, ok)
	}
	if got, ok := envValue(env, "PUBLIC_URL"); !ok || got != "https://example.com/app" {
		t.Fatalf("boxEnv PUBLIC_URL = %q, %v; want preserved", got, ok)
	}
}

func TestRedactModelPreview_RedactsDSNCredentials(t *testing.T) {
	got := redactModelPreview("db=postgres://user:pass@localhost:5432/aura")
	if strings.Contains(got, "pass@") {
		t.Fatalf("redactModelPreview leaked DSN password: %q", got)
	}
	if !strings.Contains(got, "postgres://user:***@localhost:5432/aura") {
		t.Fatalf("redactModelPreview = %q, want credential masked with host/path preserved", got)
	}
}

func envHasKey(env []string, key string) bool {
	_, ok := envValue(env, key)
	return ok
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix), true
		}
	}
	return "", false
}
