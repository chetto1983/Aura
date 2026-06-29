package settings

import (
	"context"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeLister struct{ rows []sqlc.AuraSettings }

func (f fakeLister) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, nil }

func TestAllowed(t *testing.T) {
	if m, ok := Allowed("OPENROUTER_API_KEY"); !ok || !m.Secret {
		t.Errorf("OPENROUTER_API_KEY should be allowed + secret, got ok=%v meta=%+v", ok, m)
	}
	for _, key := range []string{
		"AURA_LLM_BASE_URL",
		"AURA_LLM_MAX_TOKENS",
		"AURA_MODEL_CONTEXT_WINDOW",
		"AURA_MODEL_MAX_OUTPUT_TOKENS",
	} {
		if m, ok := Allowed(key); !ok || m.Secret || m.Kind != KindInt && key != "AURA_LLM_BASE_URL" {
			t.Errorf("%s should be an allowlisted non-secret model/token setting, got ok=%v meta=%+v", key, ok, m)
		}
	}
	if _, ok := Allowed("AURA_RERANK_MODEL"); !ok {
		t.Error("AURA_RERANK_MODEL should be allowed")
	}
	if _, ok := Allowed("POSTGRES_PASSWORD"); ok {
		t.Error("POSTGRES_PASSWORD must NOT be settings-overridable")
	}
}

func TestOverlayEnvAppliesAllowlistOnly(t *testing.T) {
	// A unique allowlisted key value + a clearly non-allowlisted key. Cleanup
	// restores the environment so other tests are unaffected.
	const allowed = "AURA_RERANK_MODEL"
	const denied = "AURA_NOT_A_REAL_OVERRIDE_KEY"
	prev, had := os.LookupEnv(allowed)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(allowed, prev)
		} else {
			_ = os.Unsetenv(allowed)
		}
		_ = os.Unsetenv(denied)
	})

	l := fakeLister{rows: []sqlc.AuraSettings{
		{Key: allowed, Value: "cohere/rerank-4-fast"},
		{Key: denied, Value: "should-be-ignored"},
	}}
	if err := OverlayEnv(t.Context(), l); err != nil {
		t.Fatalf("OverlayEnv: %v", err)
	}
	if got := os.Getenv(allowed); got != "cohere/rerank-4-fast" {
		t.Errorf("%s = %q, want the overlaid value (DB wins)", allowed, got)
	}
	if got := os.Getenv(denied); got != "" {
		t.Errorf("%s = %q, want unset — non-allowlisted rows must be ignored", denied, got)
	}
}
