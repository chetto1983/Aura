package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
)

func TestSetIdentityLLMRoutesTurnsThroughTheResolver(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{"id-a": {Key: "key-a", LimitUSD: capUSD(5)}})
	runtime := llm.NewRuntime(&fakeIdentityScopedClient{label: "services"}, llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "m", APIKey: "services-key"})
	r := &Runner{runtime: runtime}
	r.SetIdentityLLM(NewIdentityLLMResolver(loader, runtime, llm.Config{}, fakeClientFactory(), nil))

	snap, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "id-a"))
	if err != nil || snap.Config.APIKey != "key-a" {
		t.Fatalf("turn key = %q, err %v; want the identity's own key", snap.Config.APIKey, err)
	}
}

func TestSetIdentityLLMWithNilKeepsTheProcessClient(t *testing.T) {
	t.Parallel()
	r := &Runner{runtime: llm.NewRuntime(nil, llm.Config{Model: "process"})}
	r.SetIdentityLLM(nil)
	if r.identityLLM != nil {
		t.Fatal("a nil resolver was boxed into a non-nil interface")
	}
	snap, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "anyone"))
	if err != nil || snap.Config.Model != "process" {
		t.Fatalf("snapshot model = %q, err %v; want the process runtime", snap.Config.Model, err)
	}
}
