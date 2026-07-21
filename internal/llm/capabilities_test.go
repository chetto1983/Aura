package llm

import "testing"

// ProviderErrorReserveTokens is zero on the OpenRouter path (authoritative usage +
// the Wave-1.11 middle-out net) and on an unrecognized target, so the L2 hard-cap
// formula is unchanged there.
func TestProviderErrorReserveTokens_OpenRouterAndUnknownAreZero(t *testing.T) {
	openrouter := Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"}
	if got := ProviderErrorReserveTokens(openrouter); got != 0 {
		t.Fatalf("openrouter reserve = %d, want 0", got)
	}
	unknown := Config{Provider: "somethingelse", BaseURL: "https://example.test/v1"}
	if got := ProviderErrorReserveTokens(unknown); got != 0 {
		t.Fatalf("unknown-target reserve = %d, want 0", got)
	}
}

// The local llama.cpp path reserves the default headroom, env-overridable.
func TestProviderErrorReserveTokens_LlamaCppDefaultAndOverride(t *testing.T) {
	cfg := Config{Provider: "llamacpp", BaseURL: "http://127.0.0.1:8080/v1"}
	if got := ProviderErrorReserveTokens(cfg); got != defaultLlamaCppErrorReserve {
		t.Fatalf("llamacpp default reserve = %d, want %d", got, defaultLlamaCppErrorReserve)
	}

	t.Setenv(envLlamaCppErrorReserve, "8192")
	if got := ProviderErrorReserveTokens(cfg); got != 8192 {
		t.Fatalf("llamacpp reserve with override = %d, want 8192", got)
	}

	// A malformed or negative override falls back to the default (a typo in a safety
	// margin must not block a turn).
	t.Setenv(envLlamaCppErrorReserve, "-5")
	if got := ProviderErrorReserveTokens(cfg); got != defaultLlamaCppErrorReserve {
		t.Fatalf("llamacpp reserve with negative override = %d, want default %d", got, defaultLlamaCppErrorReserve)
	}
	t.Setenv(envLlamaCppErrorReserve, "notanint")
	if got := ProviderErrorReserveTokens(cfg); got != defaultLlamaCppErrorReserve {
		t.Fatalf("llamacpp reserve with malformed override = %d, want default %d", got, defaultLlamaCppErrorReserve)
	}
}

// The llama.cpp capability row declares no trustworthy provider tokenizer and the
// resolved reserve as its DeclaredErrorTokens.
func TestLlamaCppEstimatorRow(t *testing.T) {
	row := llamaCppEstimator()
	if row.ID != "llamacpp" {
		t.Fatalf("row ID = %q, want llamacpp", row.ID)
	}
	if row.ProviderTokenizer {
		t.Fatal("llama.cpp must NOT claim a trustworthy provider tokenizer")
	}
	if row.DeclaredErrorTokens != defaultLlamaCppErrorReserve {
		t.Fatalf("DeclaredErrorTokens = %d, want %d", row.DeclaredErrorTokens, defaultLlamaCppErrorReserve)
	}
}
