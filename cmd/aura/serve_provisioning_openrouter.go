package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/openrouterprovision"
	"github.com/chetto1983/aura/internal/settings"
)

// serve_provisioning_openrouter.go wires the plan 02-06 credit leg's two saga ports
// (agui.OpenRouterKeyMinter / agui.OpenRouterKeyRevoker) over the management credential
// + the shared identitykey.Store. Split out of serve_provisioning.go on touch (that file
// was 579 lines before this addition; C-05's split-before-growth rule applies here the
// same way plan 01-01 already anticipated for this file, mirroring
// serve_provisioning_objectstore.go's own refactor-on-touch precedent).

var (
	_ agui.OpenRouterMinting     = openRouterMintingAdapter{}
	_ agui.OpenRouterKeyRevoker  = openRouterKeyRevokeAdapter{}
	_ agui.OpenRouterKeyDisabler = openRouterKeyDisableAdapter{}
)

// openRouterKeyConfig is the dial info every adapter below shares: the HTTP client, the
// Provisioning-API base URL (always OpenRouter's own, whatever the primary route — D-13's
// local exemption never applies to this call), the management key, and the encrypted
// per-identity key store.
type openRouterKeyConfig struct {
	client  *http.Client
	baseURL string
	// managementKey reads the credential on every call: an admin sets it in the wizard long
	// after boot, and a key captured at boot is why the first admin never got one.
	managementKey func(context.Context) (string, error)
	store         *identitykey.Store
}

// key returns the management key, or ErrManagementKeyUnset while no admin has set one.
func (c openRouterKeyConfig) key(ctx context.Context) (string, error) {
	key, err := c.managementKey(ctx)
	if err != nil {
		return "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", openrouterprovision.ErrManagementKeyUnset
	}
	return key, nil
}

// openRouterMintingAdapter satisfies agui.OpenRouterMinting over the management key it reads
// at call time. agui.IdentityKeyMinter owns the rules (who gets which cap, never two keys for
// one identity); this adapter only speaks to the provider.
type openRouterMintingAdapter struct{ openRouterKeyConfig }

func (a openRouterMintingAdapter) ManagementKeySet(ctx context.Context) (bool, error) {
	_, err := a.key(ctx)
	if errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		return false, nil
	}
	return err == nil, err
}

func (a openRouterMintingAdapter) Mint(ctx context.Context, req openrouterprovision.MintRequest) (openrouterprovision.MintResult, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return openrouterprovision.MintResult{}, err
	}
	return openrouterprovision.MintKey(ctx, a.client, a.baseURL, managementKey, req)
}

func (a openRouterMintingAdapter) List(ctx context.Context) ([]openrouterprovision.KeyRecord, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return nil, err
	}
	return openrouterprovision.ListKeys(ctx, a.client, a.baseURL, managementKey)
}

func (a openRouterMintingAdapter) Patch(ctx context.Context, hash string, patch openrouterprovision.KeyPatch) error {
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	_, err = openrouterprovision.PatchKey(ctx, a.client, a.baseURL, managementKey, hash, patch)
	return err
}

func (a openRouterMintingAdapter) Revoke(ctx context.Context, hash string) error {
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	return openrouterprovision.RevokeKey(ctx, a.client, a.baseURL, managementKey, hash)
}

// liveRouteBills reports whether the primary route the operator runs right now bills.
func liveRouteBills(chat *chatEnv) func() bool {
	return func() bool { return !llm.IsKeylessLocalBaseURL(chat.llmRuntime.Snapshot().Config.BaseURL) }
}

// openRouterKeyPatchAdapter satisfies agui/credit_api.go's unexported creditProvider
// port (plan 02-07, CRED-03): it PATCHes an identity's cap/reset-interval, taking the
// plaintext hash the caller already loaded via identitykey.Store (this adapter does
// not do its own store lookup, unlike the revoke adapter below, because credit_api.go
// already has the record in hand from its own creditKeyStore.Load call).
type openRouterKeyPatchAdapter struct{ openRouterKeyConfig }

func (a openRouterKeyPatchAdapter) PatchCap(ctx context.Context, hash string, patch openrouterprovision.KeyPatch) (openrouterprovision.KeyRecord, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return openrouterprovision.KeyRecord{}, err
	}
	return openrouterprovision.PatchKey(ctx, a.client, a.baseURL, managementKey, hash, patch)
}

// openRouterKeyRevokeAdapter satisfies agui.OpenRouterKeyRevoker (deprovision.go's
// reverse-saga port). It owns the identitykey.Store lookup — the port takes an identity
// id rather than a hash so deprovision.go stays free of the concrete store, per that
// file's own header — and passes the plaintext hash straight into RevokeKey's verified
// DELETE-then-GET pair (CRED-08) without re-deriving the verification here.
type openRouterKeyRevokeAdapter struct{ openRouterKeyConfig }

// RevokeKey revokes identityID's OpenRouter key. A missing key row is success (nothing
// to revoke — an identity provisioned before this phase, or on a local backend, has
// none); a load error that is NOT "no key" is a real failure, distinguished from the
// skip case rather than collapsed into it. The row is read before the management key, so an
// identity with no key is removed even while no management key is set.
func (a openRouterKeyRevokeAdapter) RevokeKey(ctx context.Context, identityID string) error {
	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, err := a.store.Load(scoped)
	if err != nil {
		if errors.Is(err, identitykey.ErrNoKey) {
			return nil
		}
		return fmt.Errorf("openrouter key revoker: load key for %s: %w", identityID, err)
	}
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	return openrouterprovision.RevokeKey(ctx, a.client, a.baseURL, managementKey, rec.Hash)
}

// openRouterKeyDisableAdapter satisfies agui.OpenRouterKeyDisabler: it looks the identity's
// key up and PATCHes it disabled. No stored key means nothing to disable.
type openRouterKeyDisableAdapter struct{ openRouterKeyConfig }

func (a openRouterKeyDisableAdapter) DisableKey(ctx context.Context, identityID string) error {
	rec, err := a.store.Load(identityctx.WithIdentityID(ctx, identityID))
	if errors.Is(err, identitykey.ErrNoKey) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("openrouter key disabler: load key for %s: %w", identityID, err)
	}
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	disabled := true
	_, err = openrouterprovision.PatchKey(ctx, a.client, a.baseURL, managementKey, rec.Hash, openrouterprovision.KeyPatch{Disabled: &disabled})
	return err
}

// openRouterKeyDisablerFor builds the deactivation port; nil under the same conditions as the
// revoker.
func openRouterKeyDisablerFor(chat *chatEnv) agui.OpenRouterKeyDisabler {
	cfg, ok := resolveOpenRouterKeyConfig(chat)
	if !ok {
		return nil
	}
	return openRouterKeyDisableAdapter{cfg}
}

// openRouterSpendAdapter satisfies agui/spend_overview_api.go's unexported
// spendReconciliation port (plan 02-09, RBAC-11/CRED-06): the three reconciliation calls
// (ListKeys, GetCredits, KPIWindows) over the SAME management credential + base URL the
// mint/revoke/patch adapters above already share. *identitykey.Store already satisfies
// the sibling spendCapReader port (Load) with no adapter of its own, exactly like
// credit_api.go's creditKeyStore reuses it directly.
type openRouterSpendAdapter struct{ openRouterKeyConfig }

func (a openRouterSpendAdapter) ListKeys(ctx context.Context) ([]openrouterprovision.KeyRecord, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return nil, err
	}
	return openrouterprovision.ListKeys(ctx, a.client, a.baseURL, managementKey)
}

func (a openRouterSpendAdapter) GetCredits(ctx context.Context) (openrouterprovision.Credits, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return openrouterprovision.Credits{}, err
	}
	return openrouterprovision.GetCredits(ctx, a.client, a.baseURL, managementKey)
}

func (a openRouterSpendAdapter) KPIWindows(ctx context.Context, current, prior openrouterprovision.TimeRange) ([]openrouterprovision.KPITile, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return nil, err
	}
	return openrouterprovision.KPIWindows(ctx, a.client, a.baseURL, managementKey, current, prior)
}

// openRouterKeyMinterFor builds the forward-saga port. Nil only when the stores cannot be
// built — see resolveOpenRouterKeyConfig.
func openRouterKeyMinterFor(chat *chatEnv) agui.OpenRouterKeyMinter {
	cfg, ok := resolveOpenRouterKeyConfig(chat)
	if !ok {
		return nil
	}
	return agui.NewIdentityKeyMinter(openRouterMintingAdapter{cfg}, cfg.store, chat.identity, liveRouteBills(chat))
}

// openRouterKeyRevokerFor builds the reverse-saga port. Nil under the same conditions as
// openRouterKeyMinterFor — a boot never wires one without the other, since both resolve
// through the identical resolveOpenRouterKeyConfig gate.
func openRouterKeyRevokerFor(chat *chatEnv) agui.OpenRouterKeyRevoker {
	cfg, ok := resolveOpenRouterKeyConfig(chat)
	if !ok {
		return nil
	}
	return openRouterKeyRevokeAdapter{cfg}
}

// resolveOpenRouterKeyConfig builds the dial info every OpenRouter adapter shares. The
// management key is read from aura.settings on each call, falling back to the value the
// daemon booted with, so the ports are wired as soon as the pool and AURA_AUTHULA_SECRET
// exist, and a missing key surfaces as ErrManagementKeyUnset at call time. ok is false only
// when a store cannot be built (a malformed AURA_AUTHULA_SECRET, logged rather than a boot
// panic, as identityLLMResolver does).
func resolveOpenRouterKeyConfig(chat *chatEnv) (openRouterKeyConfig, bool) {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return openRouterKeyConfig{}, false
	}
	keys, err := identitykey.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura serve: identitykey store unavailable — openrouter key mint/revoke disabled", "err", err)
		return openRouterKeyConfig{}, false
	}
	secrets, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura serve: settings store unavailable — openrouter key mint/revoke disabled", "err", err)
		return openRouterKeyConfig{}, false
	}
	bootKey := chat.cfg.OpenRouterManagementKey
	return openRouterKeyConfig{
		client:  http.DefaultClient,
		baseURL: openrouterprovision.DefaultBaseURL,
		store:   keys,
		managementKey: func(ctx context.Context) (string, error) {
			stored, err := secrets.Secret(ctx, "AURA_OPENROUTER_MANAGEMENT_KEY")
			if err != nil || stored != "" {
				return stored, err
			}
			return bootKey, nil
		},
	}, true
}
