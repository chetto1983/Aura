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

// capUSD stays a real helper rather than an inlined new(expr) (Go 1.26): every caller
// below passes an integer literal (capUSD(5), capUSD(0)), which new(x) types as *int,
// not *float64 — a compile error against the fields it targets.
//
//nolint:modernize // inlining only compiles for an explicit float literal (none used here).
func capUSD(v float64) *float64 { return &v }

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
		"identity-a": {Key: "key-for-a", LimitUSD: capUSD(5)},
		"identity-b": {Key: "key-for-b", LimitUSD: capUSD(5)},
	})
	rs := NewIdentityLLMResolver(loader, nil, llm.Config{Provider: "openrouter"}, fakeClientFactory(), nil)

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
	rs := NewIdentityLLMResolver(loader, processRuntime, llm.Config{Provider: "openrouter"}, fakeClientFactory(), nil)

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
	rs := NewIdentityLLMResolver(loader, processRuntime, llm.Config{Provider: "openrouter", BaseURL: "http://localhost:8080"}, fakeClientFactory(), nil)

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
		"identity-a": {Key: "key-for-a", LimitUSD: capUSD(5)},
		"identity-b": {Key: "key-for-b", LimitUSD: capUSD(5)},
	})
	rs := NewIdentityLLMResolver(loader, nil, llm.Config{Provider: "openrouter"}, fakeClientFactory(), nil)

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

// fakeExhaustedClient is a stand-in for cmd/aura's creditExhaustedClient — a
// distinct type a test can assert on BY IDENTITY (pointer equality), never by
// merely checking "non-nil".
type fakeExhaustedClient struct{}

func (fakeExhaustedClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, errors.New("fake: credit exhausted")
}

var _ llm.Client = fakeExhaustedClient{}

// TestResolverReturnsCreditExhaustedOnZeroCap: a stored key with a zero cap
// returns a snapshot whose Client IS the injected exhausted sentinel — not a
// real client built from the key, and not the process runtime's client.
func TestResolverReturnsCreditExhaustedOnZeroCap(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{
		"identity-broke": {Key: "key-for-broke", LimitUSD: capUSD(0)},
	})
	processClient := &fakeIdentityScopedClient{label: "process-wide"}
	processRuntime := llm.NewRuntime(processClient, llm.Config{Provider: "openrouter"})
	exhausted := fakeExhaustedClient{}
	rs := NewIdentityLLMResolver(loader, processRuntime, llm.Config{Provider: "openrouter"}, fakeClientFactory(), exhausted)

	snap, err := rs.SnapshotFor(context.Background(), "identity-broke")
	if err != nil {
		t.Fatalf("SnapshotFor (zero cap): %v", err)
	}
	if snap.Client != exhausted {
		t.Fatalf("snapshot.Client = %v, want the injected exhausted sentinel", snap.Client)
	}
	if snap.Client == processClient {
		t.Fatal("snapshot.Client must not be the process runtime's client")
	}
}

// TestResolverReturnsRefusalOnNoKey mirrors TestResolveRefusesWhenNoKey's
// identity-based assertions (nil client, specifically ErrNoIdentityLLMKey,
// specifically not the process client) on a billing backend — the refusal
// this resolver returns when an identity has no stored key at all.
func TestResolverReturnsRefusalOnNoKey(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(nil)
	processClient := &fakeIdentityScopedClient{label: "process-wide"}
	processRuntime := llm.NewRuntime(processClient, llm.Config{Provider: "openrouter"})
	rs := NewIdentityLLMResolver(loader, processRuntime, llm.Config{Provider: "openrouter"}, fakeClientFactory(), fakeExhaustedClient{})

	snap, err := rs.SnapshotFor(context.Background(), "identity-nokey")
	if !errors.Is(err, ErrNoIdentityLLMKey) {
		t.Fatalf("err = %v, want ErrNoIdentityLLMKey", err)
	}
	if snap.Client != nil {
		t.Fatalf("snapshot.Client = %v, want nil", snap.Client)
	}
	if snap.Client == processClient {
		t.Fatal("snapshot.Client must not be the process runtime's client")
	}
}

// TestResolverExemptLocalReturnsProcessSnapshot: on a local backend the
// resolver returns the process snapshot with a nil error — the exemption,
// reached by a differently-named branch than the refusal above.
func TestResolverExemptLocalReturnsProcessSnapshot(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(nil)
	processClient := &fakeIdentityScopedClient{label: "process-wide-local"}
	localCfg := llm.Config{Provider: "openrouter", BaseURL: "http://localhost:8080"}
	processRuntime := llm.NewRuntime(processClient, localCfg)
	rs := NewIdentityLLMResolver(loader, processRuntime, localCfg, fakeClientFactory(), fakeExhaustedClient{})

	snap, err := rs.SnapshotFor(context.Background(), "identity-local")
	if err != nil {
		t.Fatalf("SnapshotFor (exempt local): %v", err)
	}
	if snap.Client != processClient {
		t.Fatalf("snapshot.Client = %v, want the process runtime's client", snap.Client)
	}
}

// TestResolverCacheInvalidatedOnCapChange: a cap raised from zero after the
// exhausted snapshot was cached is NOT served stale — Invalidate must be
// called (mirroring how a cap-change route will call it) before the next
// resolve picks up the new cap.
func TestResolverCacheInvalidatedOnCapChange(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{
		"identity-topup": {Key: "key-for-topup", LimitUSD: capUSD(0)},
	})
	exhausted := fakeExhaustedClient{}
	rs := NewIdentityLLMResolver(loader, nil, llm.Config{Provider: "openrouter"}, fakeClientFactory(), exhausted)

	before, err := rs.SnapshotFor(context.Background(), "identity-topup")
	if err != nil {
		t.Fatalf("SnapshotFor before top-up: %v", err)
	}
	if before.Client != exhausted {
		t.Fatalf("before top-up: snapshot.Client = %v, want the exhausted sentinel", before.Client)
	}

	loader.mu.Lock()
	loader.records["identity-topup"] = identitykey.Record{Key: "key-for-topup", LimitUSD: capUSD(5)}
	loader.mu.Unlock()

	stale, err := rs.SnapshotFor(context.Background(), "identity-topup")
	if err != nil {
		t.Fatalf("SnapshotFor without invalidation: %v", err)
	}
	if stale.Client != exhausted {
		t.Fatal("resolving without Invalidate served a fresh client instead of proving the cache is stale")
	}

	rs.Invalidate("identity-topup")

	after, err := rs.SnapshotFor(context.Background(), "identity-topup")
	if err != nil {
		t.Fatalf("SnapshotFor after invalidation: %v", err)
	}
	if after.Client == exhausted {
		t.Fatal("after Invalidate + top-up, snapshot.Client is still the exhausted sentinel")
	}
	if after.Config.APIKey != "key-for-topup" {
		t.Fatalf("after top-up: snapshot.Config.APIKey = %q, want key-for-topup", after.Config.APIKey)
	}
}

func TestResolverFollowsTheRuntimeModel(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{"identity-a": {Key: "key-for-a", LimitUSD: capUSD(5)}})
	cloud := llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "model-one", APIKey: "services-key"}
	runtime := llm.NewRuntime(&fakeIdentityScopedClient{label: "services"}, cloud)
	rs := NewIdentityLLMResolver(loader, runtime, llm.Config{Provider: "openrouter", Model: "boot-model"}, fakeClientFactory(), nil)

	first, err := rs.SnapshotFor(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("SnapshotFor: %v", err)
	}
	if first.Config.Model != "model-one" || first.Config.APIKey != "key-for-a" {
		t.Fatalf("snapshot = model %q key %q, want the live model-one with the identity's key", first.Config.Model, first.Config.APIKey)
	}

	cloud.Model = "model-two"
	runtime.Replace(&fakeIdentityScopedClient{label: "services"}, cloud)
	second, err := rs.SnapshotFor(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("SnapshotFor after Replace: %v", err)
	}
	if second.Config.Model != "model-two" || second.Config.APIKey != "key-for-a" {
		t.Fatalf("after a model change: model %q key %q, want model-two with the identity's key", second.Config.Model, second.Config.APIKey)
	}
	if second.Client == first.Client {
		t.Fatal("a model change served the client cached for the old model")
	}
}

func TestResolverExemptsOnceTheRouteTurnsLocal(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(nil)
	cloud := llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "m"}
	runtime := llm.NewRuntime(&fakeIdentityScopedClient{label: "cloud"}, cloud)
	rs := NewIdentityLLMResolver(loader, runtime, cloud, fakeClientFactory(), nil)

	if _, err := rs.SnapshotFor(context.Background(), "identity-nokey"); !errors.Is(err, ErrNoIdentityLLMKey) {
		t.Fatalf("cloud route with no key: err = %v, want ErrNoIdentityLLMKey", err)
	}
	local := &fakeIdentityScopedClient{label: "local"}
	runtime.Replace(local, llm.Config{Provider: "ollama", BaseURL: "http://host.docker.internal:11434/v1", Model: "gemma"})
	snap, err := rs.SnapshotFor(context.Background(), "identity-nokey")
	if err != nil {
		t.Fatalf("local route: %v", err)
	}
	if snap.Client != local {
		t.Fatal("after the switch to a local route the resolver did not serve the process runtime's client")
	}
}

func TestResolverRefusalCarriesNoServicesKey(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{"identity-broke": {Key: "key-broke", LimitUSD: capUSD(0)}})
	runtime := llm.NewRuntime(nil, llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "m", APIKey: "services-key"})
	rs := NewIdentityLLMResolver(loader, runtime, llm.Config{}, fakeClientFactory(), fakeExhaustedClient{})

	snap, err := rs.SnapshotFor(context.Background(), "identity-broke")
	if err != nil {
		t.Fatalf("SnapshotFor: %v", err)
	}
	if snap.Config.APIKey != "" {
		t.Fatal("a refusal snapshot carries the services key (CRED-07)")
	}
}
