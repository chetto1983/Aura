package main

import (
	"encoding/hex"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/secret"
)

const (
	// browserStateKeyPath is where docker/aura-sandbox/agent-browser.sh reads the key. It is in
	// the container layer, not on the workspace volume that holds the state it encrypts.
	browserStateKeyPath = "/run/aura/agent-browser.key"
	// browserStateKeyDomain is this key's HKDF label; no other store may reuse it.
	browserStateKeyDomain = "aura-browser-state-v1"
)

// browserStateKeyFiles derives each identity's agent-browser state key from AURA_AUTHULA_SECRET
// on every Resolve (prd.md §12, authenticated browsing). Nothing is stored: the same secret and
// identity rebuild the same key, so a recreated box decrypts the state its volume kept.
//
// Without the secret (a lenient profile may leave it unset) no key is written, and the box's
// agent-browser entry point refuses to run rather than let the vault mint a key beside its own
// ciphertext. That is a missing feature on that profile, not a failed Resolve.
func browserStateKeyFiles(authulaSecret string) usersandbox.BoxFileSource {
	if strings.TrimSpace(authulaSecret) == "" {
		slog.Warn("sandbox: AURA_AUTHULA_SECRET is unset, so boxes get no browser state key and agent-browser will refuse to run")
		return nil
	}
	return func(identityID string) ([]usersandbox.BoxFile, error) {
		key, err := secret.IdentityKey(authulaSecret, browserStateKeyDomain, identityID)
		if err != nil {
			return nil, err
		}
		return []usersandbox.BoxFile{{Path: browserStateKeyPath, Content: []byte(hex.EncodeToString(key)), Mode: 0o600}}, nil
	}
}
