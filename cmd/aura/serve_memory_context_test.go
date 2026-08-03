package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMountedMemoryContextUsesTheAuthenticatedIdentityDigest(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"Davide located_in Caraglio","entities":2,"facts":1,"covered":true}`}
	provider := newMemoryContextProvider(client)

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
			_, err := newMemoryContextProvider(client).Context(context.Background(), "identity-a")
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMountedMemoryContextOmitsAnEmptyDigest(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"  ","entities":0,"facts":0,"covered":true}`}
	got, err := newMemoryContextProvider(client).Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("context = %q, want empty", got)
	}
}
