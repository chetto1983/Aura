package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/knowledge"
)

type fakeMemoryGraphClient struct {
	writeQuery string
	readQuery  string
	params     map[string]any
	remaining  any
	writeErr   error
	readErr    error
	closed     bool
}

func (f *fakeMemoryGraphClient) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.writeQuery = query
	f.params = params
	return []map[string]any{{"entities": int64(1)}}, f.writeErr
}

func (f *fakeMemoryGraphClient) Read(_ context.Context, query string, _ map[string]any) ([]map[string]any, error) {
	f.readQuery = query
	return []map[string]any{{"remaining": f.remaining}}, f.readErr
}

func (f *fakeMemoryGraphClient) Close() error {
	f.closed = true
	return nil
}

func TestMemoryGraphPurgeDeletesOwnedPlanesAndVerifies(t *testing.T) {
	client := &fakeMemoryGraphClient{remaining: int64(0)}
	adapter := memoryGraphPurgeAdapter{
		cfg: &knowledge.Config{BoltURL: "bolt://neo4j:7687"},
		open: func(context.Context, *knowledge.Config) (memoryGraphClient, error) {
			return client, nil
		},
	}
	const identity = "22222222-2222-4222-8222-222222222222"

	if err := adapter.PurgeGraph(context.Background(), identity); err != nil {
		t.Fatalf("PurgeGraph: %v", err)
	}
	for _, required := range []string{
		"HAS_CONVERSATION", "HAS_PREFERENCE", "HAS_FACT", "HAS_TRACE",
		"PERFORMED_READ", "HAS_DOCUMENT", "HAS_ENTITY", "MemoryCorpusRevision",
	} {
		if !strings.Contains(client.writeQuery, required) {
			t.Errorf("purge query does not cover %s", required)
		}
	}
	if client.params["identity_id"] != identity {
		t.Fatalf("identity param = %v", client.params["identity_id"])
	}
	if client.readQuery == "" {
		t.Fatal("purge skipped postcondition verification")
	}
	if !client.closed {
		t.Fatal("purge leaked graph client")
	}
}

func TestMemoryGraphPurgeFailsClosedOnVerificationAndTransport(t *testing.T) {
	const identity = "22222222-2222-4222-8222-222222222222"
	client := &fakeMemoryGraphClient{remaining: int64(1)}
	adapter := memoryGraphPurgeAdapter{
		cfg: &knowledge.Config{},
		open: func(context.Context, *knowledge.Config) (memoryGraphClient, error) {
			return client, nil
		},
	}
	if err := adapter.PurgeGraph(context.Background(), identity); err == nil {
		t.Fatal("non-zero verification was accepted")
	}

	writeErr := errors.New("neo4j unavailable")
	client = &fakeMemoryGraphClient{writeErr: writeErr}
	adapter.open = func(context.Context, *knowledge.Config) (memoryGraphClient, error) {
		return client, nil
	}
	if err := adapter.PurgeGraph(context.Background(), identity); !errors.Is(err, writeErr) {
		t.Fatalf("write failure = %v, want wrapped %v", err, writeErr)
	}
}
