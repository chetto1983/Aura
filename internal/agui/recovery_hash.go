package agui

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const recoveryAnswerHashVersion = "argon2id-v1"

type RecoveryHasher struct{}

func NormalizeRecoveryAnswer(s string) string {
	folded := cases.Fold().String(norm.NFKC.String(s))
	return strings.Join(strings.Fields(folded), " ")
}

func (RecoveryHasher) HashAnswer(answer string) (hash string, version string, err error) {
	normalized := NormalizeRecoveryAnswer(answer)
	hash, err = hashArgon2id(normalized)
	if err != nil {
		return "", "", err
	}
	return hash, recoveryAnswerHashVersion, nil
}

func (RecoveryHasher) VerifyAnswer(answer, encoded string) bool {
	return verifyArgon2id(NormalizeRecoveryAnswer(answer), encoded)
}

func HashOpaqueSecret(secret string) (string, error) {
	return hashArgon2id(secret)
}

func VerifyOpaqueSecret(secret, encoded string) bool {
	return verifyArgon2id(secret, encoded)
}

func HashLookupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
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
	if len(parts) != 7 || parts[1] != "aura" || parts[2] != "argon2id" || parts[3] != "v=19" {
		return nil, nil, errors.New("invalid argon2id hash")
	}
	params := map[string]int{}
	for _, p := range strings.Split(parts[4], ",") {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return nil, nil, errors.New("invalid argon2id params")
		}
		n, err := strconv.Atoi(kv[1])
		if err != nil {
			return nil, nil, err
		}
		params[kv[0]] = n
	}
	if params["m"] != 65536 || params["t"] != 1 || params["p"] != 4 {
		return nil, nil, errors.New("unsupported argon2id params")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[6])
	if err != nil {
		return nil, nil, err
	}
	return salt, hash, nil
}
