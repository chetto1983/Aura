package arcadedb

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
)

// One database per identity, and the SERVER enforces it.
//
// The alternative was an owner_id column and a filter on every read. Measured on
// the live server, the difference is not stylistic:
//
//	User 'aura_memory' is not allowed to access database 'tenant_probe'
//
// That is a SecurityException from ArcadeDB. A filter is a WHERE clause that our
// code must never once forget — across search, facts-about, forget, merge,
// digest, entities and both retrieval legs — and the failure mode of forgetting
// it is one person reading another's memory, silently. A database boundary has no
// such failure mode: the credential simply cannot reach the data.
//
// It also makes deprovisioning exact. "Purge this person's memory" becomes
// `drop database`, not a sweep that has to find everything it wrote.

// The identity is a UUID and is parsed as one. A charset pattern was tried first
// and accepted "--------" and "0-0-0-0-0": combined with an endpoint that trusts
// the asserted identity, every novel string would provision a database AND a
// server user that nothing ever removes. uuid.Parse is the same check
// serve_deprovision_purgers.go already applies before purging.

// DatabaseFor maps an Aura identity to its database name.
func DatabaseFor(identityID string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		// Fail CLOSED. An empty identity used to mean "the shared database", which
		// is exactly the hole this closes: a caller that forgot to stamp its call
		// would read everyone's memory.
		return "", fmt.Errorf("user_identifier is required: memory is per identity")
	}
	parsed, err := uuid.Parse(identityID)
	if err != nil {
		return "", fmt.Errorf("user_identifier %q is not an identity: %w", identityID, err)
	}
	// parsed.String() is canonical lowercase 8-4-4-4-12, so the mapping is total
	// and injective: two spellings of one UUID land on one database, and two
	// UUIDs never land on the same one. `m` is not a hex digit, so no identity can
	// forge the `mem_` prefix that TenantUserFor strips.
	return "mem_" + strings.ReplaceAll(parsed.String(), "-", "_"), nil
}

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

// TenantCredentials derives per-identity passwords.
type TenantCredentials struct {
	secret []byte
}

// NewTenantCredentials reads the derivation secret from the environment.
func NewTenantCredentials() (*TenantCredentials, error) {
	secret := strings.TrimSpace(os.Getenv(tenantSecretEnv))
	if secret == "" {
		return nil, fmt.Errorf("%s is required to isolate memory per identity", tenantSecretEnv)
	}
	// Short secrets make the derived passwords guessable from one leaked pair.
	if len(secret) < 32 {
		return nil, fmt.Errorf("%s must be at least 32 characters", tenantSecretEnv)
	}
	return &TenantCredentials{secret: []byte(secret)}, nil
}

// TenantUserFor names the tenant's server user. It mirrors the database name so an
// operator reading server-users.jsonl can see at a glance which identity owns
// which credential.
//
// A free function rather than a method: it never touches the secret, and a purger
// that only has to DROP a user must not need the derivation key to name it — an
// absent or mid-rotation secret would otherwise leave an orphan server user that
// ArcadeDB gives no way to re-scope.
func TenantUserFor(database string) string {
	return "u_" + strings.TrimPrefix(database, "mem_")
}

// PasswordFor derives the credential. Base64 of an HMAC keeps it printable and
// well past the length any brute force reaches.
func (c *TenantCredentials) PasswordFor(database string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte("arcadedb-tenant\x00"))
	mac.Write([]byte(database))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
