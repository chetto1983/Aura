// Package identitykey persists each identity's own OpenRouter API key, one row per
// identity, encrypted at rest.
//
// It exists because the deployment-wide OPENROUTER_API_KEY is a single credential
// billed to the operator; CRED-01/CRED-07 promote a per-identity key to the primary
// representation so an identity's turn is billed to its own account, and a missing key
// is a refusal rather than a silent fallback to the deployment key (D-11).
//
// Structured on internal/mcpoauth's store (D-12): same AES-256-GCM-at-rest shape, same
// HKDF derivation from AURA_AUTHULA_SECRET, same db.WithIdentityTx RLS scoping, same
// pgx.ErrNoRows -> sentinel translation. key_hash is stored in the clear alongside the
// ciphertext — unlike mcpoauth's tokens, OpenRouter's key hash addresses the credential
// for a revoke (PATCH/DELETE) and appears in the provider's own error messages, so a
// revoke must never need to decrypt anything.
package identitykey

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/pgnumeric"
)

// keyDerivationInfo domain-separates this store's wrapping key from every other key
// derived from the same AURA_AUTHULA_SECRET — in particular from
// internal/mcpoauth's "aura-mcp-oauth-identity-key-v1". Reusing that string would mean
// one leaked key unwraps both an identity's remote-MCP tokens AND its LLM credential,
// which is the exact failure HKDF's info parameter exists to prevent.
const keyDerivationInfo = "aura-mcp-oauth-identity-key-v1" // TDD RED scaffold — GREEN fixes this

// ErrNoKey reports that this identity has no stored OpenRouter key. It is a normal
// answer, not a failure: on the OpenRouter path it is what makes the caller refuse the
// turn instead of silently reaching for the deployment-wide client (CRED-07).
var ErrNoKey = errors.New("identitykey: no OpenRouter key for this identity")

// Record is one decrypted OpenRouter key, in memory only. It has no String(), no
// MarshalJSON and no struct tag — nothing here gives the plaintext a route to a log
// line or a wire response. Handlers project it into a response type that carries Label
// and Hash but never Key.
type Record struct {
	// Key is the plaintext OpenRouter API key.
	Key string
	// Hash is OpenRouter's own hash for this key — plaintext by design (see package
	// doc): a revoke addresses the key by this hash without decrypting anything.
	Hash string
	// Label is OpenRouter's masked display form (e.g. "sk-or-v1-caa...61c"), safe to
	// show in the cockpit.
	Label      string
	LimitUSD   float64
	LimitReset string
	UpdatedAt  time.Time
}

// Summary is one entry in the key roster — the admin-facing projection that carries no
// ciphertext AND no plaintext key, only what a roster needs to decide "does this
// identity have a key, and what does OpenRouter call it".
type Summary struct {
	IdentityID string
	Hash       string
	Label      string
	LimitUSD   float64
	LimitReset string
	UpdatedAt  time.Time
}

// Store reads and writes the OpenRouter key for the identity carried on the request
// context.
type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

// NewStore builds the store. authulaSecretHex is the 64-hex-char AURA_AUTHULA_SECRET —
// the same secret and the same trust boundary internal/mcpoauth and internal/objectstore
// use. It fails closed on a malformed secret so a mis-provisioned deployment never runs
// with a weak wrapping key.
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("identitykey: nil pool")
	}
	key, err := deriveKey(authulaSecretHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("identitykey: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("identitykey: gcm: %w", err)
	}
	return &Store{pool: pool, aead: aead}, nil
}

// Save writes the key, replacing any earlier one for the same identity (ON CONFLICT
// DO UPDATE — a rotation rewrites the same row rather than accumulating history).
func (s *Store) Save(ctx context.Context, r Record) error {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return err
	}
	if strings.TrimSpace(r.Key) == "" {
		return errors.New("identitykey: save needs a key")
	}
	ciphertext, err := s.seal([]byte(r.Key))
	if err != nil {
		return err
	}
	limitUSD, err := pgnumeric.NumericFromFloat(r.LimitUSD)
	if err != nil {
		return fmt.Errorf("identitykey: save: %w", err)
	}
	limitReset := strings.TrimSpace(r.LimitReset)
	if limitReset == "" {
		limitReset = "monthly"
	}
	params := sqlc.UpsertIdentityLLMKeyParams{
		IdentityID:    id,
		KeyCiphertext: ciphertext,
		KeyHash:       r.Hash,
		KeyLabel:      r.Label,
		LimitUsd:      limitUSD,
		LimitReset:    limitReset,
	}
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		return q.UpsertIdentityLLMKey(ctx, params)
	})
	if err != nil {
		return fmt.Errorf("identitykey: save: %w", err)
	}
	return nil
}

// Load returns the OpenRouter key for the identity on ctx, or ErrNoKey — never a
// zero-value Record that reads as an empty-but-present key.
func (s *Store) Load(ctx context.Context) (Record, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return Record{}, err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return Record{}, err
	}
	var row sqlc.AuraIdentityLlmKey
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		row, qerr = q.GetIdentityLLMKey(ctx, id)
		return qerr
	})
	if err != nil {
		return Record{}, classifyLoadErr(err)
	}
	return s.decodeRow(row)
}

// classifyLoadErr translates a pgx.ErrNoRows into the ErrNoKey sentinel and wraps
// every other error — pulled out of Load as a pure function so the boundary is unit
// testable without a live database (TestStoreLoadMissingReturnsErrNoKey).
func classifyLoadErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoKey
	}
	return fmt.Errorf("identitykey: load: %w", err)
}

// List returns the key roster for the identity on ctx, ordered by identity id. The
// table's primary key is identity_id alone, so under today's RLS scoping this returns
// at most one row — the caller's own — mirroring internal/mcpoauth's List shape (and
// its ListIdentityMCPOAuthServers precedent) exactly, ready for an admin-bypass role a
// later plan may add.
func (s *Store) List(ctx context.Context) ([]Summary, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.ListIdentityLLMKeysRow
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		rows, qerr = q.ListIdentityLLMKeys(ctx, id)
		return qerr
	})
	if err != nil {
		return nil, fmt.Errorf("identitykey: list: %w", err)
	}
	out := make([]Summary, 0, len(rows))
	for _, r := range rows {
		out = append(out, Summary{
			IdentityID: uuidString(r.IdentityID),
			Hash:       r.KeyHash,
			Label:      r.KeyLabel,
			LimitUSD:   pgnumeric.FloatFromNumeric(r.LimitUsd),
			LimitReset: r.LimitReset,
			UpdatedAt:  r.UpdatedAt.Time,
		})
	}
	return out, nil
}

func (s *Store) decodeRow(row sqlc.AuraIdentityLlmKey) (Record, error) {
	plaintext, err := s.open(row.KeyCiphertext)
	if err != nil {
		return Record{}, err
	}
	return Record{
		Key:        string(plaintext),
		Hash:       row.KeyHash,
		Label:      row.KeyLabel,
		LimitUSD:   pgnumeric.FloatFromNumeric(row.LimitUsd),
		LimitReset: row.LimitReset,
		UpdatedAt:  row.UpdatedAt.Time,
	}, nil
}

// seal prepends a fresh random nonce to the ciphertext, matching internal/mcpoauth and
// internal/objectstore.IdentityStore so every AES-GCM-at-rest store in this repo reads
// the same on disk.
func (s *Store) seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("identitykey: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Store) open(ciphertext []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("identitykey: ciphertext too short (%d < %d)", len(ciphertext), ns)
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		// Deliberately does not echo the ciphertext: a decrypt failure means the
		// wrapping key changed or the row was tampered with, and neither is
		// diagnosed by dumping bytes into a log.
		return nil, fmt.Errorf("identitykey: decrypt: %w", err)
	}
	return plaintext, nil
}

// requireIdentity fails closed on a context with no principal — a key belongs to a
// person, so "no principal" must never resolve to somebody's credential.
func requireIdentity(ctx context.Context) (string, error) {
	id := strings.TrimSpace(identityctx.IdentityID(ctx))
	if id == "" {
		return "", errors.New("identitykey: no identity on context")
	}
	return id, nil
}

func parseUUID(identity string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(identity); err != nil {
		return pgtype.UUID{}, fmt.Errorf("identitykey: identity %q is not a uuid: %w", identity, err)
	}
	return out, nil
}

func uuidString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}

func deriveKey(authulaSecretHex string) ([]byte, error) {
	return deriveKeyWithInfo(authulaSecretHex, keyDerivationInfo)
}

// deriveKeyWithInfo takes the info string as a parameter so a test can derive a key
// under ANOTHER store's info and assert the two differ — domain separation asserted in
// a comment is domain separation nobody checks (mirrors internal/mcpoauth).
func deriveKeyWithInfo(authulaSecretHex, info string) ([]byte, error) {
	secret := strings.TrimSpace(authulaSecretHex)
	if len(secret) != 64 {
		return nil, errors.New("identitykey: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("identitykey: AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("identitykey: derive key: %w", err)
	}
	return key, nil
}
