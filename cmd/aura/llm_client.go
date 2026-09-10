package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

const (
	llmNotConfiguredCode = "llm_not_configured"
	llmNotConfiguredHint = "set OPENROUTER_API_KEY in .env or the environment, then retry"

	// creditExhaustedCode/Hint are CRED-05's clean pre-flight refusal: a zero-credit
	// identity is refused before the model is called, with the SAME machine-readable
	// shape llmNotConfiguredClient already ships. The hint is the actionable half of
	// the UI-SPEC's copywriting contract sentence and carries no balance figure —
	// under D-08 the only number available at refusal time is either the in-band
	// ledger (a second copy that can drift) or the provider's own lagged counter
	// (M-07: 30-40s stale, actively misleading at exactly this moment).
	creditExhaustedCode = "credit_exhausted"
	creditExhaustedHint = "Ask an administrator to add credit under Settings → Identities" // #nosec G101 -- UI copy, not credential material.
)

type llmNotConfiguredClient struct{}

type llmNotConfiguredError struct{}

// creditExhaustedClient is llmNotConfiguredClient's sibling for CRED-05: a
// zero-credit identity's Stream returns a structured refusal before any network
// call, identical in shape to the not-configured sentinel above. The resolver that
// returns this (internal/runner's IdentityLLMResolver, injected via its
// exhaustedClient constructor param since internal/runner cannot import this
// composition-root package) is the only production caller; llm_client_test.go pins
// this type's payload directly.
type creditExhaustedClient struct{}

type creditExhaustedError struct{}

func newLLMClient(cfg llm.Config) llm.Client {
	if strings.TrimSpace(cfg.APIKey) == "" && !llm.IsKeylessLocalBaseURL(cfg.BaseURL) {
		return llmNotConfiguredClient{}
	}
	return openai_compat.New(cfg)
}

func (llmNotConfiguredClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, llmNotConfiguredError{}
}

func (llmNotConfiguredError) Error() string {
	return marshalRefusalPayload(llmNotConfiguredCode, llmNotConfiguredHint,
		`{"error":"llm_not_configured","hint":"set OPENROUTER_API_KEY in .env or the environment, then retry"}`)
}

func (creditExhaustedClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, creditExhaustedError{}
}

func (creditExhaustedError) Error() string {
	return marshalRefusalPayload(creditExhaustedCode, creditExhaustedHint,
		`{"error":"credit_exhausted","hint":"Ask an administrator to add credit under Settings -> Identities"}`)
}

// marshalRefusalPayload is the one place both refusal sentinels build their
// {"error":...,"hint":...} JSON string — extracted so llmNotConfiguredError and
// creditExhaustedError stay under dupl's threshold-100 duplicate-code check
// (.golangci.yml) instead of two near-identical marshal blocks. fallback is
// returned only on a json.Marshal error, which never happens for this shape;
// it exists so Error() can never itself fail.
func marshalRefusalPayload(code, hint, fallback string) string {
	payload := struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}{
		Error: code,
		Hint:  hint,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fallback
	}
	return string(data)
}
