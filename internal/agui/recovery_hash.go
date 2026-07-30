package agui

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const recoveryAnswerHashVersion = "argon2id-v1"
const resetTokenPepperInfo = "aura-reset-token-pepper-v1"

// RecoveryHasher hashes and verifies normalized security-question answers.
type RecoveryHasher struct{}

// NormalizeRecoveryAnswer folds case, normalizes Unicode, and collapses whitespace.
func NormalizeRecoveryAnswer(s string) string {
	folded := cases.Fold().String(norm.NFKC.String(s))
	return strings.Join(strings.Fields(folded), " ")
}

// HashAnswer hashes a normalized recovery answer for durable storage.
func (RecoveryHasher) HashAnswer(answer string) (hash string, version string, err error) {
	normalized := NormalizeRecoveryAnswer(answer)
	if normalized == "" {
		return "", "", errors.New("empty recovery answer")
	}
	hash, err = hashArgon2id(normalized)
	if err != nil {
		return "", "", err
	}
	return hash, recoveryAnswerHashVersion, nil
}

// VerifyAnswer reports whether an answer matches a stored recovery-answer hash.
func (RecoveryHasher) VerifyAnswer(answer, encoded string) bool {
	normalized := NormalizeRecoveryAnswer(answer)
	if normalized == "" {
		return false
	}
	return verifyArgon2id(normalized, encoded)
}

// HashOpaqueSecret hashes a random one-time code or similarly opaque secret.
func HashOpaqueSecret(secret string) (string, error) {
	if secret == "" {
		return "", errors.New("empty opaque secret")
	}
	return hashArgon2id(secret)
}

// VerifyOpaqueSecret reports whether a one-time code matches a stored opaque-secret hash.
func VerifyOpaqueSecret(secret, encoded string) bool {
	if secret == "" {
		return false
	}
	return verifyArgon2id(secret, encoded)
}

// HashLookupToken computes the deterministic DB lookup key for a high-entropy
// reset token. The server-side pepper prevents a DB-only leak from producing
// useful token hashes while preserving primary-key lookup semantics.
func HashLookupToken(token string, pepper []byte) string {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// DeriveResetTokenPepper expands Authula's configured 32-byte secret into a
// domain-separated HMAC key. Invalid input fails closed; no default key exists.
func DeriveResetTokenPepper(secretHex string) ([]byte, error) {
	secret := strings.TrimSpace(secretHex)
	if len(secret) != 64 {
		return nil, errors.New("AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	pepper, err := hkdf.Key(sha256.New, raw, nil, resetTokenPepperInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("derive reset token pepper: %w", err)
	}
	return pepper, nil
}

func hashArgon2id(secret string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(secret), salt[:], 1, 64*1024, 4, 32)
	return fmt.Sprintf("$aura$argon2id$v=19$m=65536,t=1,p=4$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt[:]),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyArgon2id(secret, encoded string) bool {
	salt, want, err := parseArgon2id(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(secret), salt, 1, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func parseArgon2id(encoded string) ([]byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 7 || parts[0] != "" || parts[1] != "aura" || parts[2] != "argon2id" || parts[3] != "v=19" {
		return nil, nil, errors.New("invalid argon2id hash")
	}
	if parts[4] != "m=65536,t=1,p=4" {
		return nil, nil, errors.New("unsupported argon2id params")
	}
	salt, err := decodeCanonicalRawStdBase64(parts[5], 16)
	if err != nil {
		return nil, nil, err
	}
	hash, err := decodeCanonicalRawStdBase64(parts[6], 32)
	if err != nil {
		return nil, nil, err
	}
	return salt, hash, nil
}

func decodeCanonicalRawStdBase64(encoded string, wantLen int) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(decoded) != wantLen || base64.RawStdEncoding.EncodeToString(decoded) != encoded {
		return nil, errors.New("invalid argon2id field")
	}
	return decoded, nil
}
