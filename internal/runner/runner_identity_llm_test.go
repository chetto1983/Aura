package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
)

// fakeIdentityScopedClient is a minimal llm.Client whose identity a test can assert on
// directly (by pointer), instead of by inspecting Config fields alone.
type fakeIdentityScopedClient struct {
	label string
}

func (c *fakeIdentityScopedClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk)
	close(ch)
	return ch, nil
}

var _ llm.Client = (*fakeIdentityScopedClient)(nil)

// fakeKeyLoader scripts one Record (or ErrNoKey) per identity, keyed by the identity
// id the resolver scopes ctx to before calling Load — mirroring how
// identitykey.Store reads identityctx.IdentityID(ctx).
type fakeKeyLoader struct {
	mu      sync.Mutex
	records map[string]identitykey.Record
}

func newFakeKeyLoader(records map[string]identitykey.Record) *fakeKeyLoader {
	return &fakeKeyLoader{records: records}
}

func (f *fakeKeyLoader) Load(ctx context.Context) (identitykey.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := identityctx.IdentityID(ctx)
	rec, ok := f.records[id]
	if !ok {
		return identitykey.Record{}, identitykey.ErrNoKey
	}
	return rec, nil
}

func fakeClientFactory() llmClientFactory {
	return func(cfg llm.Config) llm.Client {
		return &fakeIdentityScopedClient{label: cfg.APIKey}
	}
}

func TestResolveBuildsIdentityScopedSnapshot(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{
		"identity-a": {Key: "key-for-a"},
		"identity-b": {Key: "key-for-b"},
	})
	rs := NewIdentityLLMResolver(loader, nil, llm.Config{Provider: "openrouter"}, fakeClientFactory())

	snapB, err := rs.SnapshotFor(context.Background(), "identity-b")
	if err != nil {
		t.Fatalf("SnapshotFor(b): %v", err)
	}
	if snapB.Config.APIKey != "key-for-b" {
		t.Fatalf("snapshot for b APIKey = %q, want key-for-b", snapB.Config.APIKey)
	}

	snapBAgain, err := rs.SnapshotFor(context.Background(), "identity-b")
	if err != nil {
		t.Fatalf("SnapshotFor(b) again: %v", err)
	}
	if snapBAgain.Client != snapB.Client {
		t.Fatal("resolving twice for the same identity did not return the cached client instance")
	}

	snapA, err := rs.SnapshotFor(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("SnapshotFor(a): %v", err)
	}
	if snapA.Client == snapB.Client {
		t.Fatal("identity-a and identity-b resolved to the SAME client instance")
	}
}

func TestResolveRefusesWhenNoKey(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(nil) // no identity has a stored key
	processRuntime := llm.NewRuntime(&fakeIdentityScopedClient{label: "process-wide"}, llm.Config{Provider: "openrouter"})
	rs := NewIdentityLLMResolver(loader, processRuntime, llm.Config{Provider: "openrouter"}, fakeClientFactory())

	snap, err := rs.SnapshotFor(context.Background(), "identity-nokey")
	if err == nil {
		t.Fatal("SnapshotFor with no stored key: want error, got nil")
	}
	if !errors.Is(err, ErrNoIdentityLLMKey) {
		t.Fatalf("SnapshotFor with no stored key: err = %v, want ErrNoIdentityLLMKey", err)
	}
	if snap.Client == processRuntime.Snapshot().Client {
		t.Fatal("a refused snapshot must not be the process runtime's client")
	}
	if snap.Client != nil {
		t.Fatalf("a refused snapshot must carry a nil client, got %v", snap.Client)
	}
}

// TestResolveLocalBackendExemption proves the D-13 carve-out is reached by a
// DIFFERENT branch than the refusal above: a local base URL with no stored key
// returns the process snapshot, not an error.
func TestResolveLocalBackendExemption(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(nil)
	processClient := &fakeIdentityScopedClient{label: "process-wide-local"}
	processRuntime := llm.NewRuntime(processClient, llm.Config{Provider: "openrouter", BaseURL: "http://localhost:8080"})
	rs := NewIdentityLLMResolver(loader, processRuntime, llm.Config{Provider: "openrouter", BaseURL: "http://localhost:8080"}, fakeClientFactory())

	snap, err := rs.SnapshotFor(context.Background(), "identity-local")
	if err != nil {
		t.Fatalf("SnapshotFor (local exemption): %v", err)
	}
	if snap.Client != processClient {
		t.Fatalf("local-backend exemption did not return the process runtime's client: got %v", snap.Client)
	}
}

func TestResolveConcurrentIdentitiesDoNotCross(t *testing.T) {
	loader := newFakeKeyLoader(map[string]identitykey.Record{
		"identity-a": {Key: "key-for-a"},
		"identity-b": {Key: "key-for-b"},
	})
	rs := NewIdentityLLMResolver(loader, nil, llm.Config{Provider: "openrouter"}, fakeClientFactory())

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for range n {
		wg.Add(2)
		go func() {
			defer wg.Done()
			snap, err := rs.SnapshotFor(context.Background(), "identity-a")
			if err != nil {
				errs <- err
				return
			}
			if snap.Config.APIKey != "key-for-a" {
				errs <- fmt.Errorf("identity-a resolved to APIKey %q", snap.Config.APIKey)
			}
		}()
		go func() {
			defer wg.Done()
			snap, err := rs.SnapshotFor(context.Background(), "identity-b")
			if err != nil {
				errs <- err
				return
			}
			if snap.Config.APIKey != "key-for-b" {
				errs <- fmt.Errorf("identity-b resolved to APIKey %q", snap.Config.APIKey)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
