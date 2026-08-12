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
	provider := newMemoryContextProvider(client, 5, time.Second)

	got, err := provider.Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if client.name != "memory_digest" {
		t.Fatalf("tool = %q, want memory_digest", client.name)
	}
	if client.args["user_identifier"] != "identity-a" || client.args["limit"] != 50 || client.args["facts_per_entity"] != 3 {
		t.Fatalf("args = %+v", client.args)
	}
	if got != "covered=true entities=2 facts=1\nDavide located_in Caraglio" {
		t.Fatalf("context = %q", got)
	}
}

func TestMountedMemoryContextRejectsBrokenResponses(t *testing.T) {
	for name, client := range map[string]*memoryReadinessClient{
		"transport": {err: errors.New("offline")},
		"malformed": {text: "not json"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newMemoryContextProvider(client, 5, time.Second).Context(context.Background(), "identity-a")
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMountedMemoryContextOmitsAnEmptyDigest(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"  ","entities":0,"facts":0,"covered":true}`}
	got, err := newMemoryContextProvider(client, 5, time.Second).Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("context = %q, want empty", got)
	}
}

func TestMountedMemoryContextSearchPreloadsRelevantFacts(t *testing.T) {
	client := &memoryReadinessClient{text: `{"facts":[{"statement":"Davide prefers Go"},{"statement":"lives in Caraglio"}],"retrieval":{"abstained":false}}`}
	provider := newMemoryContextProvider(client, 5, time.Second)

	got, err := provider.Search(context.Background(), "identity-a", "what does the user prefer")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if client.name != "memory_search" {
		t.Fatalf("tool = %q, want memory_search", client.name)
	}
	if client.args["user_identifier"] != "identity-a" || client.args["query"] != "what does the user prefer" || client.args["limit"] != 5 {
		t.Fatalf("args = %+v", client.args)
	}
	if !strings.Contains(got, "Davide prefers Go") || !strings.Contains(got, "lives in Caraglio") {
		t.Fatalf("preload = %q", got)
	}
}

func TestMountedMemoryContextSearchAbstainsToEmpty(t *testing.T) {
	client := &memoryReadinessClient{text: `{"facts":[{"statement":"x"}],"retrieval":{"abstained":true}}`}
	got, err := newMemoryContextProvider(client, 5, time.Second).Search(context.Background(), "identity-a", "q")
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
			if _, err := newMemoryContextProvider(client, 5, time.Second).Search(context.Background(), "identity-a", "q"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
