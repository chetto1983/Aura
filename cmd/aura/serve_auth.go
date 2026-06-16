// serve_auth.go builds the WEB-03 web-auth dependency bundle (agui.AuthDeps) the
// `aura serve` daemon threads into newServeHandler. It is split out of serve.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC) and owns two things:
//
//   - identityCheckerAdapter: a thin bridge so *identity.Store satisfies the agui
//     consumer-side identityChecker seam. identity.Store returns identity.Identity;
//     agui declares its own narrow Identity projection so the agui package does not
//     import internal/identity. The adapter does the trivial field copy.
//   - buildAuthDeps: derives the HMAC signing key from AURA_WEB_AUTH_SECRET (one
//     operator secret governs both login and signing — RESEARCH A2), binds the
//     session to the seeded `local` identity, and sets SecretConfigured from the
//     non-empty secret so RequireAuth no-ops on loopback dev (where the Plan-01 boot
//     guard permits an unconfigured secret).
package main

import (
	"context"
	"crypto/sha256"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

// identityCheckerAdapter bridges *identity.Store onto the agui.identityChecker seam:
// agui declares its own Identity projection (so it need not import internal/identity),
// and this adapter converts identity.Identity → agui.Identity on GetIdentityByID while
// passing HasCapability straight through. HasCapability/GetIdentityByID names + the
// `*` wildcard semantics are owned by internal/identity (migration 0004 seeds the
// `local` wildcard).
type identityCheckerAdapter struct {
	store *identity.Store
}

func (a identityCheckerAdapter) GetIdentityByID(ctx context.Context, id string) (agui.Identity, error) {
	idn, err := a.store.GetIdentityByID(ctx, id)
	if err != nil {
		return agui.Identity{}, err
	}
	return agui.Identity{ID: idn.ID, Name: idn.Name, Kind: idn.Kind}, nil
}

func (a identityCheckerAdapter) HasCapability(ctx context.Context, id, capability string) (bool, error) {
	return a.store.HasCapability(ctx, id, capability)
}

// buildAuthDeps assembles the WEB-03 auth bundle from the booted composition root. The
// secret comes from AURA_WEB_AUTH_SECRET (chat.cfg.WebAuthSecret); SecretConfigured is
// true only for a non-empty trimmed secret, which gates whether RequireAuth is active.
// The signing key is sha256(secret) so a single operator secret governs both the login
// compare and the cookie signature. The session binds to the seeded `local` identity
// (resolved by name, the existing fail-soft helper) — its `*` wildcard passes the
// capability gate.
func buildAuthDeps(ctx context.Context, chat *chatEnv) agui.AuthDeps {
	secret := strings.TrimSpace(chat.cfg.WebAuthSecret)
	key := sha256.Sum256([]byte(secret))
	return agui.AuthDeps{
		Secret:           secret,
		SigningKey:       key[:],
		SecretConfigured: secret != "",
		LocalIdentityID:  resolveLocalIdentityID(ctx, chat.identity),
		Identities:       identityCheckerAdapter{store: chat.identity},
		LoginPath:        "/login",
	}
}
