package mcpoauth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

const testSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// A weak or malformed wrapping key must stop the store from existing at all. The
// alternative — running with a key derived from a truncated secret — is worse than not
// running, because the ciphertext would look encrypted while being cheap to break.
func TestNewStoreRefusesAMalformedSecret(t *testing.T) {
	t.Parallel()
	for name, secret := range map[string]string{
		"empty":     "",
		"short":     "abcd",
		"not hex":   strings.Repeat("z", 64),
		"too long":  testSecret + "00",
		"truncated": testSecret[:63],
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewStore(fakePool(), secret); err == nil {
				t.Fatalf("NewStore accepted %q as a wrapping secret", name)
			}
		})
	}
}

func TestNewStoreRefusesANilPool(t *testing.T) {
	t.Parallel()
	if _, err := NewStore(nil, testSecret); err == nil {
		t.Fatal("NewStore accepted a nil pool")
	}
}

// The whole point of the package: what lands in the column must not be the token.
func TestSealDoesNotLeaveThePlaintextRecoverableByEye(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	token := "xoxp-super-secret-slack-token"

	sealed, err := s.seal([]byte(token))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(sealed), token) {
		t.Fatal("the ciphertext contains the plaintext token")
	}
	got, err := s.open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != token {
		t.Fatalf("round trip = %q, want %q", got, token)
	}
}

// A fresh nonce per seal, or two identical tokens would produce identical ciphertext and
// leak equality — enough to tell that two identities authorized with the same credential.
func TestSealUsesAFreshNoncePerCall(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	first, err := s.seal([]byte("same"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := s.seal([]byte("same"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(first) == string(second) {
		t.Fatal("two seals of the same plaintext produced identical ciphertext")
	}
}

// A row written under a different wrapping key must fail loudly, not return garbage that a
// caller would then send to a server as an Authorization header.
func TestOpenRefusesCiphertextFromAnotherKey(t *testing.T) {
	t.Parallel()
	mine := storeForCrypto(t)
	theirs, err := NewStore(fakePool(), strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sealed, err := theirs.seal([]byte("token"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := mine.open(sealed); err == nil {
		t.Fatal("a ciphertext from another wrapping key decrypted successfully")
	}
}

func TestOpenRefusesATruncatedCiphertext(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	if _, err := s.open([]byte{1, 2, 3}); err == nil {
		t.Fatal("a ciphertext shorter than the nonce decrypted successfully")
	}
}

// An absent refresh token must stay absent. Sealing the empty string would write a
// non-NULL column, and "the server issued no refresh token" would become indistinguishable
// from "the server issued an empty one".
func TestSealOptionalKeepsAbsenceAbsent(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	sealed, err := s.sealOptional(nil)
	if err != nil {
		t.Fatalf("sealOptional: %v", err)
	}
	if sealed != nil {
		t.Fatalf("sealOptional(nil) = %v, want nil", sealed)
	}
	opened, err := s.openOptional(nil)
	if err != nil {
		t.Fatalf("openOptional: %v", err)
	}
	if opened != nil {
		t.Fatalf("openOptional(nil) = %v, want nil", opened)
	}
}

// Two stores derived from the SAME secret must not be able to read each other's rows: the
// HKDF info string is what separates them, and a copy-pasted constant would silently make
// one leaked key open both stores.
func TestKeyIsDomainSeparatedFromTheObjectStore(t *testing.T) {
	t.Parallel()
	mine, err := deriveKey(testSecret)
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	// The object store's own constant, spelled out rather than imported, so this test
	// fails if either side changes its info string.
	other, err := deriveKeyWithInfo(testSecret, "aura-objectstore-identity-key-v1")
	if err != nil {
		t.Fatalf("deriveKeyWithInfo: %v", err)
	}
	if string(mine) == string(other) {
		t.Fatal("the MCP OAuth key equals the object-store key — the HKDF info is not separating them")
	}
}

// No principal on the context must never resolve to somebody's grant. Unlike
// internal/objectstore, this store has no `local` fallback on purpose.
func TestEveryOperationFailsClosedWithNoIdentityOnContext(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	ctx := context.Background()

	if _, err := s.Load(ctx, "slack"); err == nil {
		t.Error("Load succeeded with no identity on the context")
	}
	if err := s.Save(ctx, Grant{ServerName: "slack", AccessToken: "t"}); err == nil {
		t.Error("Save succeeded with no identity on the context")
	}
	if _, err := s.Delete(ctx, "slack"); err == nil {
		t.Error("Delete succeeded with no identity on the context")
	}
	if _, err := s.List(ctx); err == nil {
		t.Error("List succeeded with no identity on the context")
	}
}

// A non-UUID principal must be refused before a query is built, not turned into a cast
// error from Postgres — the RLS predicate casts app.current_identity to uuid, so a bad
// principal there is an error at the wrong layer.
func TestNonUUIDIdentityIsRefusedBeforeAnyQuery(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	ctx := identityctx.WithIdentityID(context.Background(), "not-a-uuid")

	if _, err := s.Load(ctx, "slack"); err == nil || !strings.Contains(err.Error(), "uuid") {
		t.Fatalf("Load err = %v, want a uuid complaint", err)
	}
}

// Saving without an access token would make Load succeed and hand the transport an empty
// Authorization header, which the server reports as an auth failure rather than as the
// missing grant it actually is.
func TestSaveRefusesAGrantWithNothingInIt(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	ctx := identityctx.WithIdentityID(context.Background(), "11111111-1111-1111-1111-111111111111")

	if err := s.Save(ctx, Grant{ServerName: "slack"}); err == nil {
		t.Error("Save accepted a grant with no access token")
	}
	if err := s.Save(ctx, Grant{AccessToken: "t"}); err == nil {
		t.Error("Save accepted a grant with no server name")
	}
}

func TestGrantExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	leeway := 30 * time.Second

	for name, tc := range map[string]struct {
		expiresAt time.Time
		want      bool
	}{
		// A server that issues no expiry is taken at its word rather than assumed dead.
		"no expiry issued":     {time.Time{}, false},
		"far in the future":    {now.Add(time.Hour), false},
		"already past":         {now.Add(-time.Second), true},
		"exactly now":          {now, true},
		"inside the leeway":    {now.Add(15 * time.Second), true},
		"just past the leeway": {now.Add(31 * time.Second), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := Grant{ExpiresAt: tc.expiresAt}
			if got := g.Expired(now, leeway); got != tc.want {
				t.Fatalf("Expired = %v, want %v (expiresAt=%v)", got, tc.want, tc.expiresAt)
			}
		})
	}
}

func TestTimestamptzKeepsAZeroTimeInvalid(t *testing.T) {
	t.Parallel()
	if got := timestamptz(time.Time{}); got.Valid {
		t.Error("a zero time became a valid timestamptz, which would write an epoch expiry")
	}
	at := time.Date(2026, 8, 24, 9, 30, 0, 0, time.UTC)
	got := timestamptz(at)
	if !got.Valid || !got.Time.Equal(at) {
		t.Fatalf("timestamptz(%v) = %+v", at, got)
	}
}
