package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// One CREDENTIAL per identity, not just one database.
//
// A single application user granted access to every tenant database would put
// isolation back where a per-tenant design is supposed to take it from: our own
// code passing the right database name. Get the identity wrong once and the
// server hands over someone else's memory, because the credential was allowed
// there.
//
// With a credential per identity the server refuses at the door. ArcadeDB has no
// command to widen an existing user's databases — `update user` is not a server
// command and `ALTER USER` only changes a password — so the scope is fixed at
// creation, which is exactly the property wanted here.
//
// The password is DERIVED, never stored: HMAC-SHA256 over the DATABASE NAME with a
// server-side secret. There is no per-tenant secret to keep, rotate or leak, the
// sidecar can reproduce a credential it never wrote down, and it can only mint
// one for an identity it was actually given.

// tenantSecretEnv holds the derivation key. Without it the sidecar cannot mint
// tenant credentials and says so at boot rather than falling back to a shared
// user, which would silently undo the isolation.
const tenantSecretEnv = "AURA_ARCADEDB_TENANT_SECRET" //nolint:gosec // the NAME of an env var, not a credential

// tenantCredentials derives per-identity usernames and passwords.
type tenantCredentials struct {
	secret []byte
}

func newTenantCredentials() (*tenantCredentials, error) {
	secret := strings.TrimSpace(os.Getenv(tenantSecretEnv))
	if secret == "" {
		return nil, fmt.Errorf("%s is required to isolate memory per identity", tenantSecretEnv)
	}
	// Short secrets make the derived passwords guessable from one leaked pair.
	if len(secret) < 32 {
		return nil, fmt.Errorf("%s must be at least 32 characters", tenantSecretEnv)
	}
	return &tenantCredentials{secret: []byte(secret)}, nil
}

// userFor names the tenant's server user. It mirrors the database name so an
// operator reading server-users.jsonl can see at a glance which identity owns
// which credential.
func (c *tenantCredentials) userFor(database string) string {
	return "u_" + strings.TrimPrefix(database, "mem_")
}

// passwordFor derives the credential. Base64 of an HMAC keeps it printable and
// well past the length any brute force reaches.
func (c *tenantCredentials) passwordFor(database string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte("arcadedb-tenant\x00"))
	mac.Write([]byte(database))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
