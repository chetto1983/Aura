package agui

import "context"

// onboarding_provision_credit.go carries the CRED-01/CRED-02/CRED-08 credit leg the
// provisioning saga adds in plan 02-06: minting a freshly-provisioned identity's own
// OpenRouter key at a zero cap, and that leg's own compensation. Split out of
// onboarding_provision.go on purpose (C-05 required the split precede the growth,
// which plan 02-02 performed) rather than grown in place.
//
// Declared consumer-side, mirroring onboarding_provision_resources.go's ports, so this
// package stays free of the internal/openrouterprovision and internal/identitykey
// concretes. The composition-root adapter (cmd/aura/serve_provisioning.go) composes
// openrouterprovision.MintKey with identitykey.Store.Save for the forward half, and
// identitykey.Store.Load with openrouterprovision.RevokeKey for the reverse half — the
// raw key never crosses back through this port at all, only the hash/label a revoke or
// a journal entry needs.

// MintedKey is the saga-facing projection of a freshly-minted OpenRouter key. It never
// carries the raw credential (T-02-06c): the composition-root adapter mints it, stores
// it encrypted via identitykey.Store.Save, and hands back only what the saga itself
// needs to journal the step and, on a later leg's failure, revoke it.
type MintedKey struct {
	// Hash is OpenRouter's own hash for the minted key — the compensation address
	// RevokeKey takes, exactly as the provider's own DELETE/GET-by-hash routes do.
	Hash string
	// Label is OpenRouter's masked display form (e.g. "sk-or-v1-caa...61c"). Carried
	// here only so the saga can log/journal something human-legible; never the key
	// itself.
	Label string
}

// OpenRouterKeyMinter mints and revokes one identity's OpenRouter key. MintKey is the
// forward leg: it mints at a zero cap (D-09's default state) with the OpenRouter-side
// key named after identityID (and external.user set to the same id), persists the key
// encrypted, and returns only the hash/label — never the raw key, which the adapter
// mints, stores, and lets go out of scope in the same call. RevokeKey is that leg's own
// compensation, embedded on the same port for the same reason SandboxProvisioner
// embeds SandboxPurger (onboarding_provision_resources.go): the leg that needs the
// compensation must be able to reach it without a second wiring point.
type OpenRouterKeyMinter interface {
	MintKey(ctx context.Context, identityID, keyName string) (MintedKey, error)
	RevokeKey(ctx context.Context, hash string) error
}
