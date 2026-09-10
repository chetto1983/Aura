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
	loader keyLoader
	// runtime is the live route: every identity-scoped client is built on its config with
	// only the API key swapped, and its client serves the D-13 exemption. It is never a
	// fallback credential on the OpenRouter path.
	runtime *llm.Runtime
	// base is the route used when no runtime is wired (tests, headless callers).
	base            llm.Config
	newClient       llmClientFactory
	exhaustedClient llm.Client // CRED-05 sentinel, injected from cmd/aura (see below)

	mu    sync.Mutex
	cache map[string]cachedSnapshot
}

// cachedSnapshot is one identity's resolved snapshot and the runtime version it was built
// on; a newer runtime (the operator switched route or model) makes it stale.
type cachedSnapshot struct {
	snapshot llm.RuntimeSnapshot
	version  uint64
}

// NewIdentityLLMResolver builds a resolver over loader (the encrypted key store),
// runtime (the live route: every identity-scoped client is built on its config with only
// the API key swapped, and its client serves the D-13 local-backend exemption — never a
// fallback credential on the OpenRouter path), base (the route used when no runtime is
// wired: tests, headless callers), and exhaustedClient — the CRED-05 refusal
// client a zero-credit identity's snapshot carries. exhaustedClient's concrete type
// (cmd/aura's creditExhaustedClient) lives in the composition root, which
// internal/runner cannot import; injecting the already-built value through this
// constructor param is how the sentinel reaches here without a second copy of it
// living in internal/ (two refusal payloads that must stay identical will not). A nil
// exhaustedClient degrades a zero-credit identity to the same refusal shape as a
// missing key (an error, no client) rather than panicking. A nil newClient defaults
// to openai_compat.New.
func NewIdentityLLMResolver(loader keyLoader, runtime *llm.Runtime, base llm.Config, newClient llmClientFactory, exhaustedClient llm.Client) *IdentityLLMResolver {
	if newClient == nil {
		newClient = func(cfg llm.Config) llm.Client { return openai_compat.New(cfg) }
	}
	return &IdentityLLMResolver{
		loader:          loader,
		runtime:         runtime,
		base:            base,
		newClient:       newClient,
		exhaustedClient: exhaustedClient,
		cache:           make(map[string]cachedSnapshot),
	}
}

// SnapshotFor resolves identityID's own RuntimeSnapshot by running
// identitykey.Decide over the stored record (or its absence) and this resolver's
// backend classification, then mapping the four-valued decision onto a snapshot:
//
//   - DecisionAllow: the identity's own client, built from its stored key, cached.
//   - DecisionRefuseNoKey: a refusal — ErrNoIdentityLLMKey and a nil-client
//     zero-value snapshot, never rs.runtime.Snapshot()'s process-wide client
//     (CRED-07). Not cached, so a key minted later is picked up on the next call
//     with no separate invalidation needed.
//   - DecisionRefuseNoCredit: a refusal carrying rs.exhaustedClient (CRED-05) —
//     Stream on it refuses before any network call. Cached like Allow, so a cap
//     raised later needs Invalidate to take effect (TestResolverCacheInvalidatedOnCapChange).
//   - DecisionExemptLocal: the process runtime's own snapshot (D-13) — that
//     deployment bills nothing, so there is no credential to own per identity.
//
// A cached client is reused only while the runtime version it was built on is still
// current.
//
// Getting DecisionRefuseNoKey and DecisionExemptLocal backwards is invisible in a
// happy-path test, which is why the test suite asserts on the returned client's
// IDENTITY, not merely on the absence of an error.
func (rs *IdentityLLMResolver) SnapshotFor(ctx context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	if rs == nil || rs.loader == nil {
		return llm.RuntimeSnapshot{}, errors.New("runner: nil identity LLM resolver")
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return llm.RuntimeSnapshot{}, errors.New("runner: empty identity id")
	}

	base, version := rs.liveBase()
	if cached, ok := rs.cachedSnapshot(identityID, version); ok {
		return cached, nil
	}

	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, loadErr := rs.loader.Load(scoped)
	hasKey := loadErr == nil
	if loadErr != nil && !errors.Is(loadErr, identitykey.ErrNoKey) {
		return llm.RuntimeSnapshot{}, fmt.Errorf("runner: load identity llm key for %s: %w", identityID, loadErr)
	}

	decision, decErr := identitykey.Decide(identitykey.DecisionInput{
		IdentityID:   identityID,
		HasKey:       hasKey,
		LimitUSD:     rec.LimitUSD,
		BackendBills: !llm.IsKeylessLocalBaseURL(base.BaseURL),
	})

	// A refusal keeps the route but never the services key the runtime holds (CRED-07).
	refusal := base
	refusal.APIKey = ""
	switch decision {
	case identitykey.DecisionExemptLocal:
		return rs.exemptionSnapshot(), nil
	case identitykey.DecisionAllow:
		cfg := base
		cfg.APIKey = rec.Key
		snapshot := llm.RuntimeSnapshot{Client: rs.newClient(cfg), Config: cfg}
		rs.cacheSnapshot(identityID, snapshot, version)
		return snapshot, nil
	case identitykey.DecisionRefuseNoCredit:
		if rs.exhaustedClient == nil {
			return llm.RuntimeSnapshot{}, fmt.Errorf("runner: %s: %w", identityID, decErr)
		}
		snapshot := llm.RuntimeSnapshot{Client: rs.exhaustedClient, Config: refusal}
		rs.cacheSnapshot(identityID, snapshot, version)
		return snapshot, nil
	case identitykey.DecisionRefuseNoKey:
		return llm.RuntimeSnapshot{}, fmt.Errorf("%w: %s", ErrNoIdentityLLMKey, identityID)
	default:
		// Deny by default (RBAC-09's discipline): an unrecognized Decision is a REFUSAL, never
		// treated as Allow. Decision is an int, not an enum, so the compiler would not catch a
		// fifth value added to identitykey.Decide without this switch.
		return llm.RuntimeSnapshot{}, fmt.Errorf("runner: %s: unrecognized credit decision %d", identityID, decision)
	}
}

// liveBase is the route every identity-scoped client is built on: the runtime the Settings
// API republishes when the operator switches route or model, and the boot config only when no
// runtime is wired.
func (rs *IdentityLLMResolver) liveBase() (llm.Config, uint64) {
	if rs.runtime == nil {
		return rs.base, 0
	}
	snap := rs.runtime.Snapshot()
	return snap.Config, snap.Version
}

// exemptionSnapshot is the D-13 local-backend exemption's ONLY caller of
// rs.runtime.Snapshot() for a client — split out under its own name so the refusal and
// the exemption can never be mistaken for one another in the code.
func (rs *IdentityLLMResolver) exemptionSnapshot() llm.RuntimeSnapshot {
	if rs.runtime == nil {
		return llm.RuntimeSnapshot{}
	}
	return rs.runtime.Snapshot()
}

func (rs *IdentityLLMResolver) cachedSnapshot(identityID string, version uint64) (llm.RuntimeSnapshot, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	entry, ok := rs.cache[identityID]
	if !ok || entry.version != version {
		return llm.RuntimeSnapshot{}, false
	}
	return entry.snapshot, true
}

func (rs *IdentityLLMResolver) cacheSnapshot(identityID string, snapshot llm.RuntimeSnapshot, version uint64) {
	rs.mu.Lock()
	rs.cache[identityID] = cachedSnapshot{snapshot: snapshot, version: version}
	rs.mu.Unlock()
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
