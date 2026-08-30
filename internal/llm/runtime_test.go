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
	retained := runtime.Snapshot()

	runtime.Replace(second, Config{Provider: "llamacpp", Model: "gemma-4-12b"})
	got := runtime.Snapshot()

	if got.Client != second {
		t.Fatalf("client = %p, want replacement %p", got.Client, second)
	}
	if got.Config.Provider != "llamacpp" || got.Config.Model != "gemma-4-12b" {
		t.Fatalf("config = provider %q model %q, want llamacpp/gemma-4-12b", got.Config.Provider, got.Config.Model)
	}
	if retained.Client != first || retained.Config.Provider != "openrouter" || retained.Config.Model != "cloud" {
		t.Fatalf("retained snapshot changed after replacement: client=%p provider=%q model=%q", retained.Client, retained.Config.Provider, retained.Config.Model)
	}
}

func TestRuntimeClonesMutableConfigMapsBeforePublishing(t *testing.T) {
	prices := map[string]Price{"model": {InputPer1M: 1}}
	headers := map[string]string{"X-Route": "one"}
	runtime := NewRuntime(nil, Config{Prices: prices, Headers: headers})
	prices["model"] = Price{InputPer1M: 99}
	headers["X-Route"] = "mutated"

	got := runtime.Snapshot().Config
	if got.Prices["model"].InputPer1M != 1 || got.Headers["X-Route"] != "one" {
		t.Fatalf("published snapshot aliases caller maps: prices=%v headers=%v", got.Prices, got.Headers)
	}
}
