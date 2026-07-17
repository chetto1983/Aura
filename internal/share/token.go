// token.go mints the D-13 opaque share token: a 256-bit crypto/rand value,
// base64url-encoded for URL-safety, plus its raw SHA-256 hash for the
// share.token_hash bytea column (migration 0040). Mirrors
// internal/agui/password_reset.go's newRecoveryCode (crypto/rand[32] +
// base64.RawURLEncoding) verbatim for the mint step — this exact shape has
// already shipped twice in this repo (password_reset.go, onboarding_session.go).
//
// Why a plain fast digest and not a slow password-hashing key-derivation
// function: a 256-bit crypto/rand token has no brute-force surface (a 2^256
// keyspace), so a deliberately-slow KDF buys nothing and only costs
// per-request latency on every public share-page open — this is
// session/API-key discipline (compare a bearer token), not password
// discipline (T-37F-26).
//
// Why Hash returns [32]byte, not password_reset.go's hashRequestValue base64
// string: OQ5 stores token_hash as a bytea column, not text. hashRequestValue's
// trim/net.SplitHostPort preamble is IP-hashing-specific and is deliberately
// dropped here — it has no bearing on an opaque random token.
//
// Standing rule (T-37F-11): the plaintext token is returned to the caller
// exactly once, at Mint time, and is never persisted. It must NEVER be
// logged, %w-wrapped into an error, or written to any column, audit row, or
// error string — only Hash's 32-byte digest is ever stored.
package share

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// Mint generates a fresh 256-bit opaque share token and its SHA-256 hash. A
// crypto/rand failure returns that error verbatim, with an empty plaintext
// and a zero hash — it never falls back to a weaker, non-cryptographic
// pseudo-random source or a time seed, because that would silently mint a
// guessable token instead of failing the request (T-37F-24).
func Mint() (string, [32]byte, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", [32]byte{}, err
	}
	plaintext := base64.RawURLEncoding.EncodeToString(b[:])
	return plaintext, Hash(plaintext), nil
}

// Hash returns the stable SHA-256 digest of a plaintext token, as raw bytes
// for the token_hash bytea column. It never returns a base64 or hex string —
// do not reintroduce hashRequestValue's encoding step here.
func Hash(plaintext string) [32]byte {
	return sha256.Sum256([]byte(plaintext))
}
