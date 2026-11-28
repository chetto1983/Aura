package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
)

const claudeHouseKey = "sk-ant-api03-house-key-for-agui-tests-0123456789"

// claudeCatalogServer answers the account catalogue and OpenRouter's public price list
// from fixtures, recording every call with the key it was handed.
func claudeCatalogServer(t *testing.T, priceErr error, rows []sqlc.AuraSettings) (*Server, *[]catalogCall) {
	t.Helper()
	calls := &[]catalogCall{}
	s := &Server{
		settings: &fakeSettingsStore{rows: rows},
		idAdmin: &fakeIdentityAdmin{caps: map[string][]string{
			catalogAdmin:  {identity.CapIdentityCreate, identity.CapGovernanceWrite},
			catalogMember: identity.UserSet(),
		}},
	}
	s.modelCatalog = func(_ context.Context, provider, baseURL, apiKey string) ([]llm.ModelCatalogEntry, error) {
		*calls = append(*calls, catalogCall{provider: provider, baseURL: baseURL, apiKey: apiKey})
		if provider == llm.AnthropicProvider {
			return []llm.ModelCatalogEntry{
				{ID: "claude-sonnet-5-5", DisplayName: "Claude Sonnet 5.5", ContextWindow: 1_000_000, MaxOutputTokens: 128_000},
				{ID: "claude-haiku-5-5", DisplayName: "Claude Haiku 5.5", ContextWindow: 1_000_000, MaxOutputTokens: 128_000},
				{ID: "claude-unlisted-5-5", ContextWindow: 1_000_000},
			}, nil
		}
		return []llm.ModelCatalogEntry{
			{ID: "anthropic/claude-sonnet-5.5", Price: llm.Price{InputPer1M: 2, OutputPer1M: 10}, HasPrice: true},
			{ID: "anthropic/claude-haiku-5.5", Price: llm.Price{InputPer1M: 0.1, OutputPer1M: 0.5}, HasPrice: true},
			{ID: "z-ai/glm-5.3", Price: llm.Price{InputPer1M: 0.14, OutputPer1M: 0.28}, HasPrice: true},
		}, priceErr
	}
	return s, calls
}

var claudeRows = []sqlc.AuraSettings{
	{Key: settings.AnthropicKeySetting, Value: claudeHouseKey, IsSecret: true},
	{Key: "OPENROUTER_API_KEY", Value: "sk-or-stored", IsSecret: true},
}

func claudeQuery(baseURL string) string {
	return "provider=" + llm.AnthropicProvider + "&base_url=" + url.QueryEscape(baseURL)
}

func decodeCatalog(t *testing.T, body []byte) modelCatalogDTO {
	t.Helper()
	var out modelCatalogDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestModelPickerListsTheAccountsModels(t *testing.T) {
	s, calls := claudeCatalogServer(t, nil, claudeRows)
	rr := getModels(t, s, claudeQuery(llm.AnthropicBaseURL))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	out := decodeCatalog(t, rr.Body.Bytes())
	want := []modelCatalogEntryDTO{
		{ID: "claude-sonnet-5-5", DisplayName: "Claude Sonnet 5.5", ContextWindow: 1_000_000, InputPer1M: 2, OutputPer1M: 10, HasPrice: true},
		{ID: "claude-haiku-5-5", DisplayName: "Claude Haiku 5.5", ContextWindow: 1_000_000, InputPer1M: 0.1, OutputPer1M: 0.5, HasPrice: true},
		{ID: "claude-unlisted-5-5", ContextWindow: 1_000_000},
	}
	if out.Provider != llm.AnthropicProvider || len(out.Models) != len(want) {
		t.Fatalf("catalog %+v", out)
	}
	for i := range want {
		if out.Models[i] != want[i] {
			t.Fatalf("models[%d] = %+v, want %+v", i, out.Models[i], want[i])
		}
	}
	// The house key goes to the account catalogue alone; the price list is public and
	// gets no key at all, the services key included.
	wantCalls := []catalogCall{
		{provider: llm.AnthropicProvider, baseURL: llm.AnthropicBaseURL, apiKey: claudeHouseKey},
		{provider: "openrouter", baseURL: llm.DefaultBaseURL, apiKey: ""},
	}
	if len(*calls) != len(wantCalls) || (*calls)[0] != wantCalls[0] || (*calls)[1] != wantCalls[1] {
		t.Fatalf("calls %+v, want %+v", *calls, wantCalls)
	}
}

func TestModelPickerListsClaudeWithoutPricesWhenTheListIsDown(t *testing.T) {
	s, _ := claudeCatalogServer(t, errors.New("GET /models returned 503"), claudeRows)
	rr := getModels(t, s, claudeQuery(llm.AnthropicBaseURL))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	for _, model := range decodeCatalog(t, rr.Body.Bytes()).Models {
		if model.HasPrice || model.InputPer1M != 0 {
			t.Fatalf("a price appeared without the list: %+v", model)
		}
	}
}

func TestModelPickerGuardsTheHouseKey(t *testing.T) {
	s, calls := claudeCatalogServer(t, nil, claudeRows)
	if rr := getModelsAs(t, s, catalogMember, claudeQuery(llm.AnthropicBaseURL)); rr.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", rr.Code)
	}
	for _, baseURL := range []string{"https://attacker.example", "https://api.anthropic.com/v1"} {
		if rr := getModels(t, s, claudeQuery(baseURL)); rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", baseURL, rr.Code)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused request still probed: %+v", *calls)
	}

	t.Setenv(settings.AnthropicKeySetting, "sk-ant-api03-environment-key-not-the-house-key")
	s, calls = claudeCatalogServer(t, nil, nil)
	getModels(t, s, claudeQuery(llm.AnthropicBaseURL))
	if len(*calls) == 0 || (*calls)[0].apiKey != "" {
		t.Fatalf("calls %+v: the environment became the house key", *calls)
	}
}
