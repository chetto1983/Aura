package objectstore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/profile"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// keyDerivationInfo domain-separates the object-store wrapping key from every other
// use of AURA_AUTHULA_SECRET (Authula HMAC/token keys). HKDF with this info label
// means the per-identity S3 secret's KEK is cryptographically distinct from the
// secret's other derivations, so compromising one derivation does not reveal another.
const keyDerivationInfo = "aura-objectstore-identity-key-v1"

// sharedIdentityName is the built-in principal that maps to the shared bucket
// regardless of the configured local UUID (skills/writer convention: "" -> "local").
const sharedIdentityName = "local"

// Credentials is a per-identity (or shared) S3 access triple. The caller builds a
// per-identity S3Store from Bucket + AccessKey + SecretKey layered on the shared
// endpoint/region/path-style config, so a request touches ONLY that bucket.
type Credentials struct {
	Bucket    string
	AccessKey string
	SecretKey string
}

// IdentityStore resolves and persists per-identity Garage/S3 credentials from
// aura.identity_object_store, decrypting the secret at read and encrypting it at
// write (AES-256-GCM, KEK derived from AURA_AUTHULA_SECRET). The `local` identity
// (and an empty/CLI principal) maps to the shared bucket for backward compatibility
// (D-11 operator-unchanged); every other identity resolves to its OWN bucket and a
// missing row fails CLOSED — it never falls through to the shared or a foreign bucket.
type IdentityStore struct {
	pool          *pgxpool.Pool
	aead          cipher.AEAD
	shared        Credentials
	localIdentity string // the local principal's id (UUID or name) that maps to shared
}

// NewIdentityStore builds the resolver. authulaSecretHex is the 64-hex-char (32-byte)
// AURA_AUTHULA_SECRET; shared is the existing static bucket+creds the `local` principal
// keeps using; localIdentity is that principal's id (empty -> "local"). It fails closed
// on a malformed secret so a mis-provisioned deployment never runs with a weak KEK.
func NewIdentityStore(pool *pgxpool.Pool, authulaSecretHex string, shared Credentials, localIdentity string) (*IdentityStore, error) {
	key, err := deriveObjectStoreKey(authulaSecretHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: gcm: %w", err)
	}
	localIdentity = strings.TrimSpace(localIdentity)
	if localIdentity == "" {
		localIdentity = sharedIdentityName
	}
	return &IdentityStore{pool: pool, aead: aead, shared: shared, localIdentity: localIdentity}, nil
}

// Resolve returns the bucket + credentials for the identity carried on ctx. An empty
// or `local` principal returns the shared credentials (backward compatible). Any other
// identity is looked up in aura.identity_object_store; a missing row is fail-closed
// (a foreign/unprovisioned identity resolves to NOTHING, never the shared bucket).
func (s *IdentityStore) Resolve(ctx context.Context) (Credentials, error) {
	id := strings.TrimSpace(identityctx.IdentityID(ctx))
	if s.isShared(id) {
		return s.shared, nil
	}
	pgID, err := validatedUUID(id)
	if err != nil {
		return Credentials{}, err
	}
	var bucket, accessKey string
	var enc []byte
	err = s.pool.QueryRow(ctx,
		`SELECT bucket, access_key, secret_key_enc FROM aura.identity_object_store WHERE identity_id = $1`,
		pgID,
	).Scan(&bucket, &accessKey, &enc)
	if err != nil {
		// pgx.ErrNoRows is intentionally NOT special-cased to the shared bucket: an
		// identity without its own provisioned key must resolve to nothing (fail closed),
		// never to the shared or another identity's bucket (F-007). Callers that want to
		// distinguish "not provisioned" can errors.Is(err, pgx.ErrNoRows).
		return Credentials{}, fmt.Errorf("objectstore identity: resolve %q: %w", id, err)
	}
	secret, err := s.decrypt(enc)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Bucket: bucket, AccessKey: accessKey, SecretKey: secret}, nil
}

// Put persists a per-identity credential, encrypting the secret at rest. It is
// idempotent (ON CONFLICT DO NOTHING) so the provisioning saga can re-run; a key
// rotation is Delete-then-Put (the table has no UPDATE grant by design). The
// identity MUST already exist in aura.identities (FK).
func (s *IdentityStore) Put(ctx context.Context, identity, bucket, accessKey, secretPlaintext string) error {
	pgID, err := validatedUUID(identity)
	if err != nil {
		return err
	}
	enc, err := s.encrypt(secretPlaintext)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO aura.identity_object_store (identity_id, bucket, access_key, secret_key_enc)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (identity_id) DO NOTHING`,
		pgID, bucket, accessKey, enc,
	)
	if err != nil {
		return fmt.Errorf("objectstore identity: put %q: %w", identity, err)
	}
	return nil
}

// Delete removes a per-identity credential. Idempotent: deleting an absent row is a
// no-op (the de-provisioning saga can re-run).
func (s *IdentityStore) Delete(ctx context.Context, identity string) error {
	pgID, err := validatedUUID(identity)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM aura.identity_object_store WHERE identity_id = $1`, pgID,
	); err != nil {
		return fmt.Errorf("objectstore identity: delete %q: %w", identity, err)
	}
	return nil
}

// isShared reports whether id maps to the shared bucket (empty/CLI, the built-in
// "local" name, or the configured local identity UUID).
func (s *IdentityStore) isShared(id string) bool {
	return id == "" || id == sharedIdentityName || id == s.localIdentity
}

// encrypt seals plaintext with a fresh random nonce prepended to the ciphertext.
func (s *IdentityStore) encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("objectstore identity: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// decrypt opens a nonce-prefixed ciphertext produced by encrypt.
func (s *IdentityStore) decrypt(ciphertext []byte) (string, error) {
	ns := s.aead.NonceSize()
	if len(ciphertext) < ns {
		return "", fmt.Errorf("objectstore identity: ciphertext too short (%d < %d)", len(ciphertext), ns)
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	plaintext, err := s.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("objectstore identity: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// validatedUUID applies the shared profile traversal/charset guard AND enforces that
// the identity is a real UUID (the identity_object_store PK is uuid) — so a hostile
// identity string can neither be spliced into the query nor address a foreign row.
func validatedUUID(identity string) (pgtype.UUID, error) {
	if err := profile.ValidateIdentity(identity); err != nil {
		return pgtype.UUID{}, fmt.Errorf("objectstore identity: %w", err)
	}
	u, err := uuid.Parse(identity)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("objectstore identity: invalid identity uuid %q: %w", identity, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

// deriveObjectStoreKey validates AURA_AUTHULA_SECRET (64 hex chars / 32 bytes) and
// HKDF-expands it into a domain-separated 32-byte AES-256 key.
func deriveObjectStoreKey(authulaSecretHex string) ([]byte, error) {
	secret := strings.TrimSpace(authulaSecretHex)
	if len(secret) != 64 {
		return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, keyDerivationInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: derive key: %w", err)
	}
	return key, nil
}
