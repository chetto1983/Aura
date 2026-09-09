// Unit tier (no build tag): the crypto round trip, the error-classification pure
// helpers, and the guards that fire BEFORE any query — mirrors
// internal/mcpoauth/store_test.go's shape exactly. The live-Postgres RLS + cascade
// proof lives in store_integration_test.go (db_integration).
package identitykey

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
)

const testSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakePool is a pool that parses but never dials (mirrors internal/mcpoauth's
// helpers_test.go fakePool). pgxpool.New is lazy, so a store built on it exercises
// every guard that fires BEFORE the query without needing Postgres.
func fakePool() *pgxpool.Pool {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/nowhere?sslmode=disable")
	if err != nil {
		panic("identitykey: fake pool DSN no longer parses: " + err.Error())
	}
	return pool
}

func storeForCrypto(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(fakePool(), testSecret)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

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

// TestStoreSaveLoadRoundTrip proves the crypto layer Save/Load are built on: a sealed
// key comes back byte-identical, the ciphertext never contains the plaintext, and two
// seals of the same plaintext produce different ciphertexts (fresh nonce per seal).
func TestStoreSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	key := "sk-or-v1-super-secret-openrouter-key"

	sealed, err := s.seal([]byte(key))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(sealed), key) {
		t.Fatal("the ciphertext contains the plaintext key")
	}
	got, err := s.open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != key {
		t.Fatalf("round trip = %q, want %q", got, key)
	}

	again, err := s.seal([]byte(key))
	if err != nil {
		t.Fatalf("second seal: %v", err)
	}
	if string(sealed) == string(again) {
		t.Fatal("two seals of the same plaintext produced identical ciphertext")
	}
}

// TestStoreLoadMissingReturnsErrNoKey proves the pure error-classification boundary
// Load delegates to: a pgx.ErrNoRows becomes ErrNoKey, never a zero-value Record read
// as an empty-but-present key. The full path through a real missing row is proven live
// in store_integration_test.go's TestIdentityLLMKeyRLSAndCascade.
func TestStoreLoadMissingReturnsErrNoKey(t *testing.T) {
	t.Parallel()
	if err := classifyLoadErr(pgx.ErrNoRows); !errors.Is(err, ErrNoKey) {
		t.Fatalf("classifyLoadErr(pgx.ErrNoRows) = %v, want ErrNoKey", err)
	}
	other := errors.New("connection reset")
	if err := classifyLoadErr(other); !errors.Is(err, other) {
		t.Fatalf("classifyLoadErr(other) = %v, want wrapped %v", err, other)
	}
	if errors.Is(classifyLoadErr(other), ErrNoKey) {
		t.Error("a non-NoRows error must NOT map to ErrNoKey")
	}
}

// TestKeyDerivationInfoIsDomainSeparated (D-12): the package's HKDF info string is not
// equal to internal/mcpoauth's — reusing that string is the exact failure domain
// separation exists to prevent.
func TestKeyDerivationInfoIsDomainSeparated(t *testing.T) {
	t.Parallel()
	if keyDerivationInfo == "aura-mcp-oauth-identity-key-v1" {
		t.Fatal("identitykey's HKDF info string equals internal/mcpoauth's — one leaked key would unwrap both stores")
	}
	mine, err := deriveKey(testSecret)
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	// mcpoauth's own constant, spelled out rather than imported, so this test fails if
	// either side changes its info string.
	other, err := deriveKeyWithInfo(testSecret, "aura-mcp-oauth-identity-key-v1")
	if err != nil {
		t.Fatalf("deriveKeyWithInfo: %v", err)
	}
	if string(mine) == string(other) {
		t.Fatal("identitykey's derived key equals mcpoauth's — the HKDF info is not separating them")
	}
}

// TestListSelectsNoCiphertext (source-level): the generated listing row carries the
// hash, the label and the reset interval, and does NOT carry the ciphertext field —
// the admin roster needs the cap and the hash, never the key.
func TestListSelectsNoCiphertext(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[sqlc.ListIdentityLLMKeysRow]()
	names := make(map[string]bool, typ.NumField())
	for field := range typ.Fields() {
		names[field.Name] = true
	}
	for _, want := range []string{"KeyHash", "KeyLabel", "LimitUsd", "LimitReset"} {
		if !names[want] {
			t.Errorf("ListIdentityLLMKeysRow missing field %s", want)
		}
	}
	if names["KeyCiphertext"] {
		t.Error("ListIdentityLLMKeysRow carries KeyCiphertext — the listing query must never select ciphertext")
	}
}

// No principal on the context must never resolve to somebody's key. Unlike
// internal/objectstore, this store has no `local` fallback on purpose.
func TestEveryOperationFailsClosedWithNoIdentityOnContext(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	ctx := context.Background()

	if _, err := s.Load(ctx); err == nil {
		t.Error("Load succeeded with no identity on the context")
	}
	if err := s.Save(ctx, Record{Key: "k"}); err == nil {
		t.Error("Save succeeded with no identity on the context")
	}
	if _, err := s.List(ctx); err == nil {
		t.Error("List succeeded with no identity on the context")
	}
}

// A non-UUID principal must be refused before a query is built.
func TestNonUUIDIdentityIsRefusedBeforeAnyQuery(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	ctx := identityctx.WithIdentityID(context.Background(), "not-a-uuid")

	if _, err := s.Load(ctx); err == nil || !strings.Contains(err.Error(), "uuid") {
		t.Fatalf("Load err = %v, want a uuid complaint", err)
	}
}

// Saving without a key would make Load succeed and hand the runner an empty
// credential, which the provider reports as an auth failure rather than as the
// missing key it actually is.
func TestSaveRefusesAnEmptyKey(t *testing.T) {
	t.Parallel()
	s := storeForCrypto(t)
	ctx := identityctx.WithIdentityID(context.Background(), "11111111-1111-1111-1111-111111111111")

	if err := s.Save(ctx, Record{}); err == nil {
		t.Error("Save accepted a record with no key")
	}
}
