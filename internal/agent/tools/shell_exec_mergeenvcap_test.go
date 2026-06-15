package tools

import (
	"math"
	"strings"
	"testing"
)

// TestMergeEnvCap is the go/allocation-size-overflow regression for mergeEnv's
// slice capacity hint: parentLen+extraLen+defaults must stay non-negative and
// saturate instead of wrapping when either input approaches maxInt.
func TestMergeEnvCap(t *testing.T) {
	cases := []struct {
		name      string
		parentLen int
		extraLen  int
		want      int
	}{
		{"zero", 0, 0, 2},
		{"typical", 40, 3, 45},
		{"negative_parent", -1, 0, 0},
		{"negative_extra", 0, -1, 0},
		{"overflow_saturates", math.MaxInt - 1, 4, math.MaxInt},
		{"max_parent_saturates", math.MaxInt, 0, math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeEnvCap(tc.parentLen, tc.extraLen); got != tc.want {
				t.Fatalf("mergeEnvCap(%d, %d) = %d, want %d", tc.parentLen, tc.extraLen, got, tc.want)
			}
		})
	}
}

func TestMergeEnv_DropsCredentialDSNButKeepsPublicURL(t *testing.T) {
	t.Setenv("AURA_DB_URL", "postgres://user:pass@localhost:5432/aura")
	t.Setenv("PUBLIC_URL", "https://example.com/app")
	t.Setenv("AURA_PUBLIC_FLAG", "visible")

	env := mergeEnv(nil)
	if envHasKey(env, "AURA_DB_URL") {
		t.Fatalf("mergeEnv leaked AURA_DB_URL: %v", env)
	}
	if got, ok := envValue(env, "PUBLIC_URL"); !ok || got != "https://example.com/app" {
		t.Fatalf("mergeEnv PUBLIC_URL = %q, %v; want preserved public URL", got, ok)
	}
	if got, ok := envValue(env, "AURA_PUBLIC_FLAG"); !ok || got != "visible" {
		t.Fatalf("mergeEnv AURA_PUBLIC_FLAG = %q, %v; want visible", got, ok)
	}
}

func TestMergeEnv_DropsCredentialDSNFromExplicitEnv(t *testing.T) {
	env := mergeEnv(map[string]string{
		"AURA_DB_URL": "postgres://user:pass@localhost:5432/aura",
		"PUBLIC_URL":  "https://example.com/app",
	})
	if envHasKey(env, "AURA_DB_URL") {
		t.Fatalf("mergeEnv leaked explicit AURA_DB_URL: %v", env)
	}
	if got, ok := envValue(env, "PUBLIC_URL"); !ok || got != "https://example.com/app" {
		t.Fatalf("mergeEnv explicit PUBLIC_URL = %q, %v; want preserved", got, ok)
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
