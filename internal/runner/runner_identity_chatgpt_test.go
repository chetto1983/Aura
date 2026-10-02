package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
)

type chatGPTScopeRecorder struct{ owner string }

func (c *chatGPTScopeRecorder) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	c.owner = identityctx.IdentityID(ctx)
	return nil, errors.New("fixture terminal error")
}

type forbiddenOpenRouterLoader struct{ calls int }

type switchingRouteLoader struct {
	runtime *llm.Runtime
}

func (l switchingRouteLoader) Load(context.Context) (identitykey.Record, error) {
	l.runtime.Replace(&fakeIdentityScopedClient{label: "deployment-key"}, llm.Config{
		Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: "deployment-key",
	})
	return identitykey.Record{}, identitykey.ErrNoKey
}

func TestIdentitySnapshotKeepsClassifiedClientDuringRouteSwitch(t *testing.T) {
	localClient := &fakeIdentityScopedClient{label: "local"}
	localConfig := llm.Config{Provider: "ollama", BaseURL: "http://localhost:11434/v1"}
	runtime := llm.NewRuntime(localClient, localConfig)
	resolver := NewIdentityLLMResolver(switchingRouteLoader{runtime}, runtime, localConfig, nil, nil)
	snapshot, err := resolver.SnapshotFor(context.Background(), "owner")
	if err != nil || snapshot.Client != localClient || snapshot.Config.Provider != "ollama" {
		t.Fatal("route switch substituted a hosted deployment credential after classification")
	}
}

func (l *forbiddenOpenRouterLoader) Load(context.Context) (identitykey.Record, error) {
	l.calls++
	return identitykey.Record{}, errors.New("OpenRouter is unavailable")
}

func TestChatGPTSnapshotUsesTheSelectedIdentityWithoutOpenRouterCredit(t *testing.T) {
	client := &chatGPTScopeRecorder{}
	loader := &forbiddenOpenRouterLoader{}
	cfg := llm.Config{Provider: llm.ChatGPTProvider, APIKey: "unrelated-key", Model: "account-model"}
	runtime := llm.NewRuntime(client, cfg)
	resolver := NewIdentityLLMResolver(loader, runtime, cfg, nil, nil)
	for _, owner := range []string{"identity-a", "identity-b"} {
		snapshot, err := resolver.SnapshotFor(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Config.APIKey != "" || snapshot.Config.Model != cfg.Model {
			t.Fatal("wrong account route or leaked OpenRouter key")
		}
		_, _ = snapshot.Client.Stream(identityctx.WithIdentityID(context.Background(), "foreign-context"), llm.Request{})
		if client.owner != owner {
			t.Fatalf("stream scoped to %q instead of selected owner %q", client.owner, owner)
		}
	}
	if loader.calls != 0 {
		t.Fatal("ChatGPT route depended on OpenRouter credit")
	}
	if _, err := (&identityScopedLLMClient{}).Stream(context.Background(), llm.Request{}); err == nil {
		t.Fatal("unwired ChatGPT client accepted a turn")
	}
}
