package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMountedMemoryContextUsesTheAuthenticatedIdentityDigest(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"Davide located_in Caraglio","entities":2,"facts":1,"covered":true}`}
	provider := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second)

	got, err := provider.Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if client.name != "memory_digest" {
		t.Fatalf("tool = %q, want memory_digest", client.name)
	}
	// The fixture round-trips args through a real JSON-RPC wire (no hand-rolled
	// mcptools double survives, per D-103), so JSON numbers decode as float64.
	// One entity, one fact: the counts are the whole payload now, so asking the server to
	// render fifty entities would be work whose output is thrown away.
	if client.args["limit"] != float64(1) || client.args["facts_per_entity"] != float64(1) {
		t.Fatalf("args = %+v", client.args)
	}
	if aura, present := client.meta["aura"]; present {
		t.Fatalf("memory digest sent proprietary Aura metadata: %v", aura)
	}
	if _, present := client.args["user_identifier"]; present {
		t.Error("identity leaked into wire arguments")
	}
	// The POINTER, not the memory. Carrying the digest's text is what removed the need to
	// recall: the agent paraphrased what was already in front of it and the retrieval tools
	// went uncalled, the neighbourhood hop among them — it exists only behind a tool call.
	if strings.Contains(got, "Davide located_in Caraglio") {
		t.Fatalf("the digest's content leaked back into the turn: %q", got)
	}
	if !strings.Contains(got, "1 facts across 2 entities") {
		t.Fatalf("context = %q, want the memory's shape", got)
	}
	for _, wayIn := range []string{"memory_facts_about", "memory_recall", "memory_search", "memory-aura"} {
		if !strings.Contains(got, wayIn) {
			t.Fatalf("the pointer does not name %s, so it points nowhere: %q", wayIn, got)
		}
	}
}

func TestMountedMemoryContextCanAttachAfterDeferredMount(t *testing.T) {
	provider := newMemoryContextProvider(nil, 5, time.Second)
	if _, err := provider.Context(context.Background(), "identity-a"); err == nil {
		t.Fatal("context must fail before the deferred memory mount attaches")
	}

	client := &memoryReadinessClient{text: `{"text":"late memory","entities":1,"facts":1,"covered":true}`}
	provider.setClient(client.mount(t, "identity-a"))

	got, err := provider.Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context after attach: %v", err)
	}
	if !strings.Contains(got, "1 facts across 1 entities") {
		t.Fatalf("context = %q, want the pointer once the mount attaches", got)
	}
}

func TestMountedMemoryContextRejectsBrokenResponses(t *testing.T) {
	for name, client := range map[string]*memoryReadinessClient{
		"transport": {err: errors.New("offline")},
		"malformed": {text: "not json"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Context(context.Background(), "identity-a")
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMountedMemoryContextOmitsAnEmptyDigest(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"  ","entities":0,"facts":0,"covered":true}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("context = %q, want empty", got)
	}
}

func TestMountedMemoryContextSearchPreloadsRelevantFacts(t *testing.T) {
	client := &memoryReadinessClient{text: `{"facts":[{"statement":"Davide prefers Go"},{"statement":"lives in Caraglio"}],"retrieval":{"abstained":false}}`}
	provider := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second)

	got, err := provider.Search(context.Background(), "identity-a", "what does the user prefer")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if client.name != "memory_search" {
		t.Fatalf("tool = %q, want memory_search", client.name)
	}
	if client.args["query"] != "what does the user prefer" || client.args["limit"] != float64(5) {
		t.Fatalf("args = %+v", client.args)
	}
	if aura, present := client.meta["aura"]; present {
		t.Fatalf("memory search sent proprietary Aura metadata: %v", aura)
	}
	if !strings.Contains(got, "Davide prefers Go") || !strings.Contains(got, "lives in Caraglio") {
		t.Fatalf("preload = %q", got)
	}
}

func TestMountedMemoryContextSearchAbstainsToEmpty(t *testing.T) {
	client := &memoryReadinessClient{text: `{"facts":[{"statement":"x"}],"retrieval":{"abstained":true}}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "" {
		t.Fatalf("abstention must yield empty, got %q", got)
	}
}

func TestMountedMemoryContextSearchRejectsBrokenResponses(t *testing.T) {
	for name, client := range map[string]*memoryReadinessClient{
		"transport": {err: errors.New("offline")},
		"malformed": {text: "not json"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Search(context.Background(), "identity-a", "q"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
