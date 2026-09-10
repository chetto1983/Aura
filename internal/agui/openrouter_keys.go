// openrouter_keys.go mints a person's own OpenRouter key. The provisioning saga (as its credit
// port) and the reconciler (openrouter_reconcile.go) both go through IdentityKeyMinter, so the
// rules live in one place: nothing on a local route or before a management key exists, no
// limit for an admin, a zero cap for everyone else, and never two live keys for one identity.
package agui

import (
	"context"
	"errors"
	"log/slog"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// OpenRouterMinting is the provider side of minting, implemented in cmd/aura over the
// management key it reads at call time.
type OpenRouterMinting interface {
	ManagementKeySet(ctx context.Context) (bool, error)
	Mint(ctx context.Context, req openrouterprovision.MintRequest) (openrouterprovision.MintResult, error)
	Revoke(ctx context.Context, hash string) error
}

// identityKeyStore is identitykey.Store narrowed to what minting needs; the caller scopes ctx
// to the identity.
type identityKeyStore interface {
	Load(ctx context.Context) (identitykey.Record, error)
	InsertIfAbsent(ctx context.Context, r identitykey.Record) (bool, error)
}

// capabilityChecker answers whether an identity is an admin.
type capabilityChecker interface {
	HasCapability(ctx context.Context, identityID, capability string) (bool, error)
}

// The reasons minting waits, reported by the reconciler.
const (
	skipLocalRoute         = "local_route"
	skipManagementKeyUnset = "management_key_unset"
)

// IdentityKeyMinter mints identities' OpenRouter keys.
type IdentityKeyMinter struct {
	minting    OpenRouterMinting
	keys       identityKeyStore
	caps       capabilityChecker
	routeBills func() bool
}

// NewIdentityKeyMinter builds the minter. routeBills reports whether the live primary route
// bills; a local route bills nothing, so it gets no keys (D-13).
func NewIdentityKeyMinter(minting OpenRouterMinting, keys identityKeyStore, caps capabilityChecker, routeBills func() bool) *IdentityKeyMinter {
	return &IdentityKeyMinter{minting: minting, keys: keys, caps: caps, routeBills: routeBills}
}

var _ OpenRouterKeyMinter = (*IdentityKeyMinter)(nil)

// MintKey is the provisioning saga's credit leg. On a local route, or before an admin set the
// management key, it mints nothing and returns an empty key; the reconciler mints it later.
func (m *IdentityKeyMinter) MintKey(ctx context.Context, identityID, keyName string) (MintedKey, error) {
	skip, err := m.readiness(ctx)
	if err != nil || skip != "" {
		return MintedKey{}, err
	}
	minted, _, err := m.ensure(ctx, identityID, keyName)
	return minted, err
}

// RevokeKey is the saga's compensation. An empty hash is a mint that never happened.
func (m *IdentityKeyMinter) RevokeKey(ctx context.Context, hash string) error {
	if hash == "" {
		return nil
	}
	return m.minting.Revoke(ctx, hash)
}

// readiness is "" when keys can be minted now, else the reason they cannot.
func (m *IdentityKeyMinter) readiness(ctx context.Context) (string, error) {
	if m.routeBills != nil && !m.routeBills() {
		return skipLocalRoute, nil
	}
	set, err := m.minting.ManagementKeySet(ctx)
	if err != nil {
		return "", err
	}
	if !set {
		return skipManagementKeyUnset, nil
	}
	return "", nil
}

// ensure mints identityID's key unless it already has one, and reports whether it minted. An
// admin's key has no limit; everyone else's starts at zero (CRED-02). The row is written only
// if still absent: a mint that loses that race is revoked and the winner's key returned.
func (m *IdentityKeyMinter) ensure(ctx context.Context, identityID, keyName string) (MintedKey, bool, error) {
	scoped := identityctx.WithIdentityID(ctx, identityID)
	existing, err := m.keys.Load(scoped)
	if err == nil {
		return MintedKey{Hash: existing.Hash, Label: existing.Label}, false, nil
	}
	if !errors.Is(err, identitykey.ErrNoKey) {
		return MintedKey{}, false, err
	}
	admin, err := m.caps.HasCapability(ctx, identityID, identity.CapIdentityCreate)
	if err != nil {
		return MintedKey{}, false, err
	}
	var limit *openrouterprovision.USDCap
	var limitUSD *float64
	if !admin {
		limit, limitUSD = new(openrouterprovision.USDCap), new(float64)
	}
	res, err := m.minting.Mint(ctx, openrouterprovision.MintRequest{
		IdentityID: identityID, Name: keyName, Limit: limit, LimitReset: openrouterprovision.LimitResetMonthly,
	})
	if err != nil {
		return MintedKey{}, false, err
	}
	inserted, err := m.keys.InsertIfAbsent(scoped, identitykey.Record{
		Key: res.Key, Hash: res.Record.Hash, Label: res.Record.Label,
		LimitUSD: limitUSD, LimitReset: string(openrouterprovision.LimitResetMonthly),
	})
	if err != nil {
		m.revokeUnrecorded(ctx, res.Record.Hash)
		return MintedKey{}, false, err
	}
	if !inserted {
		m.revokeUnrecorded(ctx, res.Record.Hash)
		winner, err := m.keys.Load(scoped)
		if err != nil {
			return MintedKey{}, false, err
		}
		return MintedKey{Hash: winner.Hash, Label: winner.Label}, false, nil
	}
	return MintedKey{Hash: res.Record.Hash, Label: res.Record.Label}, true, nil
}

// revokeUnrecorded revokes a key Aura minted but did not record, so it does not stay live and
// billable at the provider. Its own failure is logged, not returned: the caller is already
// reporting the error that left the key unrecorded.
func (m *IdentityKeyMinter) revokeUnrecorded(ctx context.Context, hash string) {
	if err := m.minting.Revoke(context.WithoutCancel(ctx), hash); err != nil {
		slog.Error("openrouter keys: revoking an unrecorded key failed; an orphan key may be live at the provider", "err", err)
	}
}
