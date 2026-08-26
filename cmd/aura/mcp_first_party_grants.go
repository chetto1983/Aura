package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcpoauth"
	"github.com/chetto1983/aura/internal/webauth"
)

// mcp_first_party_grants.go keeps a stored OAuth grant alive for every sidecar AURA
// SHIPS — calendar, memory, whatsapp — for every human identity on the box.
//
// Why it has to exist: a05c92cfe turned those three into OAuth resource servers isolated
// by token subject, but the only issuance path was the browser consent flow. Aura's own
// memory therefore never mounted: mcpOwnerContext found no grant, StartReconnect skipped
// the server, and the daemon sat unhealthy on "required memory capability is not mounted"
// forever. Nothing was broken in the mount path — nobody had ever minted the credential.
//
// Why it also has to RENEW: an Aura access token lives 15 minutes (Authula's JWT plugin
// default), and the refresh leg of Aura's own token endpoint rotates against a LOGIN
// SESSION. A self-issued grant has no login session behind it, so its refresh cannot
// succeed — a first mint alone would restore memory for a quarter of an hour and then
// lose it again. The keeper re-mints before expiry instead, and the live mount picks the
// new token up without a remount because persistingTokenSource re-reads the stored grant
// before every call. That is the whole reason this is a keeper and not a boot one-shot.
const (
	// firstPartyGrantRenewBefore must stay comfortably above firstPartyGrantInterval so a
	// grant is re-minted a whole tick before anything expires.
	firstPartyGrantRenewBefore     = 7 * time.Minute
	firstPartyGrantInterval        = 5 * time.Minute
	firstPartyGrantStopJoinTimeout = 5 * time.Second
)

// firstPartyTokenIssuer is the production Aura authorization server, narrowed to the one
// call this file makes. An interface so the keeper is testable without Authula.
type firstPartyTokenIssuer interface {
	IssueFirstPartyToken(ctx context.Context, resource, identityID string) (webauth.IssuedMCPToken, error)
}

// firstPartyGrantStore is the identity-scoped grant store, narrowed the same way.
type firstPartyGrantStore interface {
	Load(ctx context.Context, serverName string) (mcpoauth.Grant, error)
	Save(ctx context.Context, grant mcpoauth.Grant) error
}

type firstPartyGrantKeeper struct {
	issuer      firstPartyTokenIssuer
	store       firstPartyGrantStore
	identities  memoryIdentityLister
	servers     func() (map[string]mcp.ManagedServer, error)
	now         func() time.Time
	renewBefore time.Duration
	interval    time.Duration

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

// newFirstPartyGrantKeeper returns nil when the deployment cannot self-issue at all (no
// authorization server, no grant store, no identity source). Nil is a working state, not
// a failure: it is what a passphrase-auth or Postgres-less deployment looks like, and
// every method below is nil-safe.
func newFirstPartyGrantKeeper(issuer firstPartyTokenIssuer, store firstPartyGrantStore, identities memoryIdentityLister) *firstPartyGrantKeeper {
	if issuer == nil || store == nil || identities == nil {
		return nil
	}
	return &firstPartyGrantKeeper{
		issuer:      issuer,
		store:       store,
		identities:  identities,
		servers:     firstPartyMCPServers,
		now:         time.Now,
		renewBefore: firstPartyGrantRenewBefore,
		interval:    firstPartyGrantInterval,
		stop:        make(chan struct{}),
	}
}

// firstPartyMCPServers is the ONLY place that decides which servers may be self-issued
// to. Both gates are load-bearing: FirstPartyRecipe refuses anything that is not a
// shipped recipe at its shipped address, and UsesOAuth skips a server the operator gave a
// static bearer or disabled OAuth on, where a minted token would be dead weight.
func firstPartyMCPServers() (map[string]mcp.ManagedServer, error) {
	_, policies, err := mcpRuntimeSet()
	if err != nil {
		return nil, fmt.Errorf("first-party MCP grants: load servers: %w", err)
	}
	out := make(map[string]mcp.ManagedServer, len(policies))
	for name, server := range policies {
		if !mcpmanager.FirstPartyRecipe(server) {
			continue
		}
		settings, err := mcp.OAuthSettingsFromEnv(server.Env)
		if err != nil || !mcp.UsesOAuth(server, settings) {
			continue
		}
		out[name] = server
	}
	return out, nil
}

// EnsureNow mints the grants that are missing or about to expire, for every identity that
// can own one, and reports what it could not do. The caller logs: a grant failure must
// never abort boot, because the daemon is more useful with a degraded memory than not at
// all.
func (k *firstPartyGrantKeeper) EnsureNow(ctx context.Context) error {
	if k == nil {
		return nil
	}
	rows, err := k.identities.ListIdentities(ctx)
	if err != nil {
		return fmt.Errorf("first-party MCP grants: list identities: %w", err)
	}
	servers, err := k.servers()
	if err != nil {
		return err
	}
	var joined error
	for _, row := range rows {
		// Same filter as reconcileArcadeMemoryTenants, and for the same reason: a
		// `service` identity such as aura-cli has no memory tenant, no cockpit and no
		// sidecar data of its own, so a credential minted for it would be a live token
		// nothing ever uses.
		if row.Kind != "user" || row.Deactivated {
			continue
		}
		joined = errors.Join(joined, k.ensureIdentity(ctx, row.ID, servers))
	}
	return joined
}

// EnsureIdentity provisions one identity's first-party grants. It runs at identity
// creation, beside the ArcadeDB tenant, so a new person's memory is reachable on their
// first turn instead of after the next restart.
func (k *firstPartyGrantKeeper) EnsureIdentity(ctx context.Context, identityID string) error {
	if k == nil {
		return nil
	}
	servers, err := k.servers()
	if err != nil {
		return err
	}
	return k.ensureIdentity(ctx, identityID, servers)
}

func (k *firstPartyGrantKeeper) ensureIdentity(ctx context.Context, identityID string, servers map[string]mcp.ManagedServer) error {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	var joined error
	for _, name := range names {
		if err := k.ensureGrant(ctx, identityID, name, servers[name]); err != nil {
			joined = errors.Join(joined, fmt.Errorf("first-party MCP grant %q for %s: %w", name, identityID, err))
		}
	}
	return joined
}

// ensureGrant is the idempotent unit: it mints only when there is nothing stored, when
// what is stored is about to expire, or when the sidecar moved (a grant's audience is
// pinned to the resource URL, so a port change makes the stored token unusable).
func (k *firstPartyGrantKeeper) ensureGrant(ctx context.Context, identityID, name string, server mcp.ManagedServer) error {
	scoped := identityctx.WithIdentityID(ctx, identityID)
	stored, err := k.store.Load(scoped, name)
	switch {
	case err == nil:
		if !k.needsRenewal(stored, server.URL) {
			return nil
		}
	case errors.Is(err, mcpoauth.ErrNoGrant):
	default:
		return err
	}

	issued, err := k.issuer.IssueFirstPartyToken(scoped, server.URL, identityID)
	if err != nil {
		return err
	}
	grant, err := mcp.AuraIssuedGrant{
		ServerName:   name,
		ResourceURL:  server.URL,
		ClientID:     issued.ClientID,
		Scopes:       strings.Fields(issued.Scope),
		AccessToken:  issued.AccessToken,
		RefreshToken: issued.RefreshToken,
		ExpiresAt:    issued.ExpiresAt,
	}.Grant()
	if err != nil {
		return err
	}
	if err := k.store.Save(scoped, grant); err != nil {
		return err
	}
	slog.Info("mcp oauth: self-issued a first-party grant",
		"server", name, "identity", identityID, "expires_at", issued.ExpiresAt.UTC().Format(time.RFC3339))
	return nil
}

func (k *firstPartyGrantKeeper) needsRenewal(stored mcpoauth.Grant, resourceURL string) bool {
	if stored.ResourceURL != resourceURL {
		return true
	}
	return stored.Expired(k.now(), k.renewBefore)
}

// Start runs the keeper's tick loop. The boot one-shot is NOT here: it is EnsureNow,
// called synchronously before the deferred OAuth mounts reconnect, because a grant that
// lands after StartReconnect has already resolved owners arrives one whole restart late.
func (k *firstPartyGrantKeeper) Start(ctx context.Context) {
	if k == nil || k.interval <= 0 {
		return
	}
	k.wg.Go(func() {
		ticker := time.NewTicker(k.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-k.stop:
				return
			case <-ticker.C:
				if err := k.EnsureNow(ctx); err != nil {
					slog.Warn("mcp oauth: could not keep the first-party grants current", "err", err)
				}
			}
		}
	})
}

// Stop signals the tick loop and joins it under a bounded wait, so a hung mint cannot
// wedge shutdown. Idempotent, and safe when Start was never called.
func (k *firstPartyGrantKeeper) Stop() {
	if k == nil {
		return
	}
	k.once.Do(func() { close(k.stop) })
	done := make(chan struct{})
	go func() {
		k.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(firstPartyGrantStopJoinTimeout):
	}
}

// buildFirstPartyGrantKeeper wires the keeper over the daemon's own authorization server
// and grant store. It returns nil — a working, silent state — when the deployment has no
// Authula provider, no Postgres or no AURA_AUTHULA_SECRET, which is exactly the shape of
// a passphrase-auth install that never had these sidecars to begin with.
func buildFirstPartyGrantKeeper(
	cfg *config.Config,
	pool *pgxpool.Pool,
	identities memoryIdentityLister,
	provider *webauth.Provider,
) *firstPartyGrantKeeper {
	if cfg == nil || pool == nil || provider == nil || strings.TrimSpace(cfg.AuthulaSecret) == "" {
		return nil
	}
	// Read into a local before the interface conversion: a typed-nil *OAuthServer inside
	// a non-nil interface is the classic way a nil check passes and the first call panics.
	oauth := provider.MCPOAuth()
	if oauth == nil {
		return nil
	}
	store, err := mcpoauth.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("mcp oauth: no grant store; Aura cannot self-issue its own sidecar grants", "err", err)
		return nil
	}
	return newFirstPartyGrantKeeper(oauth, store, identities)
}
