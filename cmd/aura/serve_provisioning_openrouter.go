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
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// serve_provisioning_openrouter.go wires the plan 02-06 credit leg's two saga ports
// (agui.OpenRouterKeyMinter / agui.OpenRouterKeyRevoker) over the management credential
// + the shared identitykey.Store. Split out of serve_provisioning.go on touch (that file
// was 579 lines before this addition; C-05's split-before-growth rule applies here the
// same way plan 01-01 already anticipated for this file, mirroring
// serve_provisioning_objectstore.go's own refactor-on-touch precedent).

var (
	_ agui.OpenRouterKeyMinter  = openRouterKeyMintAdapter{}
	_ agui.OpenRouterKeyRevoker = openRouterKeyRevokeAdapter{}
)

// openRouterKeyConfig is the shared dial info both adapters below need: the HTTP client,
// the Provisioning-API base URL (always OpenRouter's own, independent of the
// deployment's primary-LLM base URL — D-13's local-backend exemption never applies to
// this call), the management credential, and the encrypted per-identity key store.
type openRouterKeyConfig struct {
	client        *http.Client
	baseURL       string
	managementKey string
	store         *identitykey.Store
}

// openRouterKeyMintAdapter satisfies agui.OpenRouterKeyMinter (onboarding_provision_credit.go).
// Its own RevokeKey(ctx, hash) is the FORWARD saga's compensation, keyed on the hash
// MintKey returned — a DIFFERENT method from openRouterKeyRevokeAdapter's RevokeKey
// below despite the identical name: the two ports' RevokeKey methods take different
// arguments (a hash here, an identity id there) under the same method name, and one
// type satisfying both interfaces would silently accept either meaning depending on
// which interface variable held it. Keeping them as two distinct types makes that
// ambiguity a compile-time impossibility rather than a runtime foot-gun.
type openRouterKeyMintAdapter struct{ openRouterKeyConfig }

// MintKey mints identityID's own OpenRouter key at a zero cap (CRED-02) named after the
// identity id (keyName), persists it encrypted via identitykey.Store.Save, and returns
// only the hash/label — the raw key returned by the provider goes out of scope at the
// end of this call and never crosses back through the agui port (T-02-06c).
func (a openRouterKeyMintAdapter) MintKey(ctx context.Context, identityID, keyName string) (agui.MintedKey, error) {
	result, err := openrouterprovision.MintKey(ctx, a.client, a.baseURL, a.managementKey, openrouterprovision.MintRequest{
		IdentityID: identityID,
		Name:       keyName,
		Limit:      0,
		LimitReset: openrouterprovision.LimitResetMonthly,
	})
	if err != nil {
		return agui.MintedKey{}, fmt.Errorf("openrouter key minter: mint: %w", err)
	}
	// Save/Load's own requireIdentity(ctx) reads identityctx.IdentityID(ctx) — scope ctx
	// to the identity being PROVISIONED, not whatever ambient principal (the creator)
	// the saga's own ctx carries.
	saveCtx := identityctx.WithIdentityID(ctx, identityID)
	if err := a.store.Save(saveCtx, identitykey.Record{
		Key:        result.Key,
		Hash:       result.Record.Hash,
		Label:      result.Record.Label,
		LimitUSD:   0,
		LimitReset: string(openrouterprovision.LimitResetMonthly),
	}); err != nil {
		// The key exists at the provider but Aura never recorded it — revoke rather than
		// leave an orphan the operator pays for and cannot see (T-02-31).
		if rerr := openrouterprovision.RevokeKey(context.WithoutCancel(ctx), a.client, a.baseURL, a.managementKey, result.Record.Hash); rerr != nil {
			slog.Error("openrouter key minter: revoke after a failed persist also failed — an orphan key may exist at the provider", "step", "compensate")
		}
		return agui.MintedKey{}, fmt.Errorf("openrouter key minter: persist: %w", err)
	}
	return agui.MintedKey{Hash: result.Record.Hash, Label: result.Record.Label}, nil
}

// RevokeKey is the forward saga's own compensation (onboarding_provision.go's
// compCredit) — see the type doc for why this is hash-keyed and lives on a separate
// type from openRouterKeyRevokeAdapter's identity-keyed RevokeKey below.
func (a openRouterKeyMintAdapter) RevokeKey(ctx context.Context, hash string) error {
	return openrouterprovision.RevokeKey(ctx, a.client, a.baseURL, a.managementKey, hash)
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
// skip case rather than collapsed into it.
func (a openRouterKeyRevokeAdapter) RevokeKey(ctx context.Context, identityID string) error {
	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, err := a.store.Load(scoped)
	if err != nil {
		if errors.Is(err, identitykey.ErrNoKey) {
			return nil
		}
		return fmt.Errorf("openrouter key revoker: load key for %s: %w", identityID, err)
	}
	return openrouterprovision.RevokeKey(ctx, a.client, a.baseURL, a.managementKey, rec.Hash)
}

// openRouterKeyMinterFor builds the forward-saga port. Nil when the management
// credential is absent or the store cannot be built — see resolveOpenRouterKeyConfig.
func openRouterKeyMinterFor(chat *chatEnv) agui.OpenRouterKeyMinter {
	cfg, ok := resolveOpenRouterKeyConfig(chat)
	if !ok {
		return nil
	}
	return openRouterKeyMintAdapter{cfg}
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

// resolveOpenRouterKeyConfig is the single gate both constructors above share: nil
// (ok=false) when the management credential is absent (D-13: a deployment with none —
// including a local-backend-only deployment — degrades rather than boots fatal; an INFO
// line names what is degraded) or when the identitykey.Store cannot be built (a
// malformed AURA_AUTHULA_SECRET, logged and degraded rather than a boot panic,
// mirroring buildIdentityLLMResolver's own nil-guard in serve_delegation.go).
//
// A present-but-broken credential does NOT nil-skip here: ok is still true and the
// adapters built from it will fail loudly at call time, because a nil-port skip on a
// broken (not absent) credential is exactly the silently-under-provisioning hazard
// 02-RESEARCH.md Q5 names.
func resolveOpenRouterKeyConfig(chat *chatEnv) (openRouterKeyConfig, bool) {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return openRouterKeyConfig{}, false
	}
	key := strings.TrimSpace(chat.cfg.OpenRouterManagementKey)
	if key == "" {
		slog.Info("aura serve: no OpenRouter management credential (AURA_OPENROUTER_MANAGEMENT_KEY) — no per-identity OpenRouter keys will be minted or revoked")
		return openRouterKeyConfig{}, false
	}
	store, err := identitykey.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura serve: identitykey store unavailable — openrouter key mint/revoke disabled", "err", err)
		return openRouterKeyConfig{}, false
	}
	return openRouterKeyConfig{
		client:        http.DefaultClient,
		baseURL:       openrouterprovision.DefaultBaseURL,
		managementKey: key,
		store:         store,
	}, true
}
