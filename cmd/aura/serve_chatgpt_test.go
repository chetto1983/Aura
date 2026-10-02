package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type missingChatGPTToken struct{}

func TestChatGPTRouteDoesNotRequireOpenRouterCredits(t *testing.T) {
	runtime := llm.NewRuntime(nil, llm.Config{Provider: llm.ChatGPTProvider, BaseURL: llm.ChatGPTBaseURL})
	bills := liveRouteBills(&chatEnv{llmRuntime: runtime})
	if bills() {
		t.Fatal("ChatGPT plan triggers OpenRouter key minting")
	}
	runtime.Replace(nil, llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
	if !bills() {
		t.Fatal("switching back lost the OpenRouter credit policy")
	}
}

func (missingChatGPTToken) AccessToken(context.Context) (string, error) {
	return "", errors.New("ChatGPT authorization required")
}

func TestChatGPTFactoryAndPersistenceFailClosed(t *testing.T) {
	if newChatGPTPlan(nil) != nil || newChatGPTPlan(&config.Config{}) != nil || chatGPTTokenSourceOrNil(nil) != nil {
		t.Fatal("disabled connection boxed as live")
	}
	cfg := &config.Config{SkillsDir: filepath.Join(t.TempDir(), "skills"), AuthulaSecret: "00000000000000000000000000000000000000000000000000000000000000a1"}
	service := newChatGPTPlan(cfg)
	if service == nil || chatGPTTokenSourceOrNil(service) == nil {
		t.Fatal("connection service not created")
	}
	client := newLLMClient(llm.Config{Provider: llm.ChatGPTProvider, APIKey: "unrelated-key"}, missingChatGPTToken{})
	if _, err := client.Stream(context.Background(), llm.Request{}); err == nil {
		t.Fatal("missing ChatGPT token borrowed OpenRouter key")
	}
}

func TestChatGPTModelSaveUsesAccountCatalogAndClearsOtherKeys(t *testing.T) {
	r := &primaryLLMRouteReloader{fallback: validFallbackLLMConfig()}
	overrides := map[string]string{"AURA_LLM_PROVIDER": llm.ChatGPTProvider, "AURA_LLM_BASE_URL": llm.ChatGPTBaseURL, "AURA_LLM_MODEL": "account-model"}
	cfg, err := r.resolve(overrides, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ResolveChatGPTProfile(); err != nil {
		t.Fatal(err)
	}
	efforts := []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}
	if err := applyChatGPTCatalogModel(&cfg, []llm.ModelCatalogEntry{{ID: "account-model", ContextWindow: 128000, SupportedReasoningEfforts: efforts, ReasoningMandatory: true}}); err != nil {
		t.Fatal(err)
	}
	if cfg.ContextWindow != 128000 || cfg.CostStatus != llm.CostStatusSubscriptionIncluded || cfg.APIKey != "" {
		t.Fatal("wrong profile metadata")
	}
	if !slices.Equal(cfg.SupportedReasoningEfforts, efforts) || !cfg.ReasoningMandatory {
		t.Fatal("account reasoning capabilities were discarded")
	}
	efforts[0] = llm.ReasoningEffortNone
	if cfg.SupportedReasoningEfforts[0] != llm.ReasoningEffortLow {
		t.Fatal("catalog alias mutated route snapshot")
	}
	if applyChatGPTCatalogModel(&cfg, nil) == nil {
		t.Fatal("accepted model outside account catalog")
	}
	if validateChatGPTModel(context.Background(), &cfg, nil) == nil || validateChatGPTModel(context.Background(), &cfg, missingChatGPTToken{}) == nil {
		t.Fatal("accepted unconnected account")
	}
	overrides["AURA_LLM_BASE_URL"] = "https://attacker.example/v1"
	if _, err := r.resolve(overrides, nil); err == nil {
		t.Fatal("accepted credential forwarding endpoint")
	}
	if validateChatGPTModel(context.Background(), &llm.Config{Provider: "ollama"}, nil) != nil {
		t.Fatal("Ollama required ChatGPT connection")
	}
}

func TestChatGPTSettingsRemainProtected(t *testing.T) {
	server := &agui.Server{}
	handler, err := newServeHandler(server.Mux(), agui.AuthDeps{SecretConfigured: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/settings/chatgpt/status", http.StatusUnauthorized},
		{http.MethodPost, "/api/settings/chatgpt/login", http.StatusUnauthorized},
		{http.MethodDelete, "/api/settings/chatgpt/login", http.StatusUnauthorized},
		{http.MethodDelete, "/api/settings/chatgpt", http.StatusUnauthorized},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(test.method, test.path, nil))
		if rr.Code != test.status {
			t.Fatalf("%s: status %d, want %d", test.path, rr.Code, test.status)
		}
	}
}

type chatGPTMemberIdentities struct{ wiringIdentities }

func (chatGPTMemberIdentities) HasCapability(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestChatGPTOwnerMayConnectWithoutSettingsAdministration(t *testing.T) {
	const owner = "00000000-0000-0000-0000-000000000007"
	auth := authulaTestDeps(owner, chatGPTMemberIdentities{wiringIdentities{id: owner}})
	handler, err := newServeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if identityctx.IdentityID(r.Context()) != owner {
			t.Fatal("connection lost its authenticated owner")
		}
		w.WriteHeader(http.StatusOK)
	}), auth, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range agui.ChatGPTPlanRoutes() {
		parts := strings.SplitN(route, " ", 2)
		request := httptest.NewRequest(parts[0], parts[1], nil)
		addAuthulaSession(request)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatal("member cannot connect their own ChatGPT account")
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", nil)
	addAuthulaSession(request)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatal("own-account connection granted deployment administration")
	}
}
