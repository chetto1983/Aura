// runner_identity_llm.go is the seam-A resolver (D-11/CRED-01/CRED-07): it builds a
// per-identity llm.RuntimeSnapshot from that identity's own stored OpenRouter key,
// instead of the process-wide deployment credential runner_llm_runtime.go's
// llmSnapshot falls back to. Deliberately its own file rather than added to
// runner_llm_runtime.go (32 lines, the unchanged seam): the resolver sits ABOVE that
// seam, publishing its result through the same withLLMRuntimeSnapshot context key, not
// woven into llmSnapshot's body.
package runner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

// ErrNoIdentityLLMKey reports that the OpenRouter path has no stored key for the
// requested identity and the request is not exempt (D-13's local-backend carve-out).
// The caller must refuse the turn — the process-wide deployment client is never
// substituted (CRED-07).
var ErrNoIdentityLLMKey = errors.New("runner: identity has no stored OpenRouter key")

// keyLoader is the resolver's consumer-side port onto the encrypted key store
// (D-A2-02 "accept interfaces, return structs"): one method, load the record for
// whatever identity the caller has scoped ctx to via identityctx.WithIdentityID.
// *identitykey.Store satisfies it structurally.
type keyLoader interface {
	Load(ctx context.Context) (identitykey.Record, error)
}

// llmClientFactory builds an llm.Client from a fully-resolved config. Narrowed to
// exactly what SnapshotFor needs so a test can substitute a fake without opening a
// real transport; production wires openai_compat.New.
type llmClientFactory func(cfg llm.Config) llm.Client

// IdentityLLMResolver builds a per-identity llm.RuntimeSnapshot from that identity's
// stored OpenRouter key (seam A). It caches one client per identity — openai_compat's
// client.go:43 already sets DisableKeepAlives, so N cached clients cost N transports,
// not N connection pools.
type IdentityLLMResolver struct {
	loader    keyLoader
	runtime   *llm.Runtime // process-wide fallback, used ONLY for the D-13 local exemption
	base      llm.Config
	newClient llmClientFactory

	mu    sync.Mutex
	cache map[string]llm.RuntimeSnapshot
}

// NewIdentityLLMResolver builds a resolver over loader (the encrypted key store),
// runtime (the process-wide snapshot source, consulted ONLY for the D-13 local-backend
// exemption — never as a fallback on the OpenRouter path), and base (the deployment's
// LLM config template: provider, base URL, model and every non-credential field an
// identity-scoped client still needs). A nil newClient defaults to openai_compat.New.
func NewIdentityLLMResolver(loader keyLoader, runtime *llm.Runtime, base llm.Config, newClient llmClientFactory) *IdentityLLMResolver {
	if newClient == nil {
		newClient = func(cfg llm.Config) llm.Client { return openai_compat.New(cfg) }
	}
	return &IdentityLLMResolver{
		loader:    loader,
		runtime:   runtime,
		base:      base,
		newClient: newClient,
		cache:     make(map[string]llm.RuntimeSnapshot),
	}
}

// SnapshotFor resolves identityID's own RuntimeSnapshot.
//
// On the OpenRouter path an identity with no stored key is refused: SnapshotFor
// returns ErrNoIdentityLLMKey and a zero-value snapshot, never r.runtime.Snapshot()'s
// process-wide client (CRED-07) — getting this backwards is invisible in a happy-path
// test, which is why the test suite asserts on the returned client's IDENTITY, not
// merely on the absence of an error.
//
// When the backend is a local one (allowsKeylessLocalLLMBaseURL's hosts, D-13) the
// process snapshot IS the correct answer: that deployment bills nothing, so there is
// no credential to own per identity. That is the exemption, reached by this
// differently-named branch, never the same code path as the refusal above.
func (rs *IdentityLLMResolver) SnapshotFor(ctx context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	if rs == nil || rs.loader == nil {
		return llm.RuntimeSnapshot{}, errors.New("runner: nil identity LLM resolver")
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return llm.RuntimeSnapshot{}, errors.New("runner: empty identity id")
	}

	if cached, ok := rs.cachedSnapshot(identityID); ok {
		return cached, nil
	}

	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, err := rs.loader.Load(scoped)
	if err != nil {
		if errors.Is(err, identitykey.ErrNoKey) {
			return rs.noKeySnapshot(identityID)
		}
		return llm.RuntimeSnapshot{}, fmt.Errorf("runner: load identity llm key for %s: %w", identityID, err)
	}

	cfg := rs.base
	cfg.APIKey = rec.Key
	snapshot := llm.RuntimeSnapshot{Client: rs.newClient(cfg), Config: cfg}

	rs.mu.Lock()
	rs.cache[identityID] = snapshot
	rs.mu.Unlock()
	return snapshot, nil
}

// noKeySnapshot is the D-13 local-backend exemption, split out under its own name so
// the refusal and the exemption can never be mistaken for one another in the code:
// this is the ONLY place the resolver reaches for rs.runtime.Snapshot().
func (rs *IdentityLLMResolver) noKeySnapshot(identityID string) (llm.RuntimeSnapshot, error) {
	if allowsKeylessLocalLLMBaseURL(rs.base.BaseURL) {
		if rs.runtime == nil {
			return llm.RuntimeSnapshot{}, nil
		}
		return rs.runtime.Snapshot(), nil
	}
	return llm.RuntimeSnapshot{}, fmt.Errorf("%w: %s", ErrNoIdentityLLMKey, identityID)
}

func (rs *IdentityLLMResolver) cachedSnapshot(identityID string) (llm.RuntimeSnapshot, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	snapshot, ok := rs.cache[identityID]
	return snapshot, ok
}

// Invalidate drops identityID's cached client, forcing the next SnapshotFor to rebuild
// it — required after a cap change or a key re-mint so a stale client is never served.
func (rs *IdentityLLMResolver) Invalidate(identityID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.cache, identityID)
}

// ScopeContextToIdentitySnapshot resolves identityID's snapshot and returns a context
// carrying it via withLLMRuntimeSnapshot — the same context key runner_llm_runtime.go's
// llmSnapshot reads from, so a caller that wraps ctx this way needs no other wiring.
func (rs *IdentityLLMResolver) ScopeContextToIdentitySnapshot(ctx context.Context, identityID string) (context.Context, error) {
	snapshot, err := rs.SnapshotFor(ctx, identityID)
	if err != nil {
		return ctx, err
	}
	return withLLMRuntimeSnapshot(ctx, snapshot), nil
}

// allowsKeylessLocalLLMBaseURL classifies baseURL as a local, non-billing backend
// (D-13) — vLLM/llama.cpp/Ollama running on the box or the LAN. It mirrors
// cmd/aura/llm_client.go's allowsKeylessLLMBaseURL verbatim: internal/runner cannot
// import cmd/aura (the composition root; the import would cycle back through
// cmd/aura -> internal/runner), so the host classification is duplicated here by
// necessity, the same layering constraint that duplicates identityCreateCapability
// between cmd/aura and internal/agui. Keep both host lists in sync on change.
func allowsKeylessLocalLLMBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "host.docker.internal" || strings.HasSuffix(host, ".local") {
		return true
	}
	switch host {
	case "ollama", "vllm", "llama", "llama-cpp", "aura-llm", "aura-vllm-chat", "aura-llama-chat", "aura-llama":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
