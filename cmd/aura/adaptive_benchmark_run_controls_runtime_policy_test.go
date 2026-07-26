package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestAdaptiveBenchmarkMemoryMismatchStreamAvoidsExternalModel(
	t *testing.T,
) {
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: make(chan llm.Chunk),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	clear, err := adaptiveBenchmarkInstallMemoryMismatchStream(client)
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Stream(t.Context(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	chunks := adaptiveBenchmarkTestChunks(stream)
	if len(chunks) != 2 ||
		chunks[0].Text == "" ||
		chunks[1].FinishReason != "stop" ||
		delegate.calls != 0 {
		t.Fatalf(
			"mismatch chunks/delegate = %#v/%d",
			chunks,
			delegate.calls,
		)
	}

	clear()
	if _, err := client.Stream(t.Context(), llm.Request{}); err != nil {
		t.Fatal(err)
	}
	if delegate.calls != 1 {
		t.Fatalf("delegate calls after clear = %d", delegate.calls)
	}
}
