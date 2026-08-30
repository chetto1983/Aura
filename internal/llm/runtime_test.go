package llm

import (
	"context"
	"testing"
)

type runtimeTestClient struct{ name string }

func (*runtimeTestClient) Stream(context.Context, Request) (<-chan Chunk, error) {
	ch := make(chan Chunk)
	close(ch)
	return ch, nil
}

func TestRuntimeReplacePublishesOneClientConfigSnapshot(t *testing.T) {
	first := &runtimeTestClient{name: "first"}
	second := &runtimeTestClient{name: "second"}
	runtime := NewRuntime(first, Config{Provider: "openrouter", Model: "cloud"})

	runtime.Replace(second, Config{Provider: "llamacpp", Model: "gemma-4-12b"})
	got := runtime.Snapshot()

	if got.Client != second {
		t.Fatalf("client = %p, want replacement %p", got.Client, second)
	}
	if got.Config.Provider != "llamacpp" || got.Config.Model != "gemma-4-12b" {
		t.Fatalf("config = provider %q model %q, want llamacpp/gemma-4-12b", got.Config.Provider, got.Config.Model)
	}
}
