package agui

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNormalizeRecoveryAnswer(t *testing.T) {
	got := NormalizeRecoveryAnswer("  My   First   School  ")
	if got != "my first school" {
		t.Fatalf("normalized = %q, want %q", got, "my first school")
	}

	got = NormalizeRecoveryAnswer("  Ｓｔｒａｓｓｅ\t")
	if got != "strasse" {
		t.Fatalf("normalized full-width sharp-s form = %q, want %q", got, "strasse")
	}

	got = NormalizeRecoveryAnswer("Straße")
	if got != "strasse" {
		t.Fatalf("normalized sharp-s form = %q, want %q", got, "strasse")
	}
}

func TestRecoveryHasherHashVerify(t *testing.T) {
	h := RecoveryHasher{}
	hash, version, err := h.HashAnswer("Blue Guitar")
	if err != nil {
		t.Fatalf("HashAnswer: %v", err)
	}
	if version != recoveryAnswerHashVersion {
		t.Fatalf("version = %q, want %q", version, recoveryAnswerHashVersion)
	}
	if strings.Contains(hash, "Blue Guitar") {
		t.Fatal("hash leaked the raw answer")
	}
	if !h.VerifyAnswer(" blue   guitar ", hash) {
		t.Fatal("normalized answer should verify")
	}
	if h.VerifyAnswer("red guitar", hash) {
		t.Fatal("wrong answer verified")
	}

	sameAnswerHash, _, err := h.HashAnswer("Blue Guitar")
	if err != nil {
		t.Fatalf("HashAnswer same answer: %v", err)
	}
	if sameAnswerHash == hash {
		t.Fatal("same answer should produce different encoded hashes because salts differ")
	}
}

func TestRecoveryHasherHashVerifyRejectsEmptyAnswers(t *testing.T) {
	h := RecoveryHasher{}
	for _, answer := range []string{"", "   ", "\t\n"} {
		if _, _, err := h.HashAnswer(answer); err == nil {
			t.Fatalf("HashAnswer(%q) succeeded, want error", answer)
		}
	}

	hash, _, err := h.HashAnswer("Blue Guitar")
	if err != nil {
		t.Fatalf("HashAnswer non-empty: %v", err)
	}
	for _, answer := range []string{"", "   ", "\t\n"} {
		if h.VerifyAnswer(answer, hash) {
			t.Fatalf("VerifyAnswer(%q) succeeded, want false", answer)
		}
	}
}

func TestRecoveryHasherHashVerifyUnicodeNormalizedAnswers(t *testing.T) {
	h := RecoveryHasher{}
	hash, _, err := h.HashAnswer("Straße")
	if err != nil {
		t.Fatalf("HashAnswer: %v", err)
	}
	if !h.VerifyAnswer("  ＳＴＲＡＳＳＥ  ", hash) {
		t.Fatal("unicode normalized answer should verify")
	}
}

func TestHashOpaqueSecretVerify(t *testing.T) {
	raw := "123456"
	hash, err := HashOpaqueSecret(raw)
	if err != nil {
		t.Fatalf("HashOpaqueSecret: %v", err)
	}
	if strings.Contains(hash, raw) {
		t.Fatal("opaque hash leaked raw secret")
	}
	if !VerifyOpaqueSecret(raw, hash) {
		t.Fatal("opaque secret should verify")
	}
	if VerifyOpaqueSecret("654321", hash) {
		t.Fatal("wrong opaque secret verified")
	}

	sameSecretHash, err := HashOpaqueSecret(raw)
	if err != nil {
		t.Fatalf("HashOpaqueSecret same secret: %v", err)
	}
	if sameSecretHash == hash {
		t.Fatal("same opaque secret should produce different encoded hashes because salts differ")
	}
}

func TestOpaqueSecretRejectsEmptySecret(t *testing.T) {
	if _, err := HashOpaqueSecret(""); err == nil {
		t.Fatal("HashOpaqueSecret empty secret succeeded, want error")
	}

	hash, err := HashOpaqueSecret("123456")
	if err != nil {
		t.Fatalf("HashOpaqueSecret non-empty: %v", err)
	}
	if VerifyOpaqueSecret("", hash) {
		t.Fatal("VerifyOpaqueSecret empty secret succeeded, want false")
	}
}

func TestHashLookupTokenIsDeterministicAndNonReversible(t *testing.T) {
	token := "4PRDJ00sx9pu-pXfPIU7R35AqkWQurXieK8Kdbx1bUs"
	a := HashLookupToken(token)
	b := HashLookupToken(token)
	if a != b {
		t.Fatal("lookup token hash must be deterministic")
	}
	if strings.Contains(a, token) {
		t.Fatal("lookup token hash leaked raw token")
	}
}

func TestArgon2MalformedHashesRejected(t *testing.T) {
	hash, err := HashOpaqueSecret("secret")
	if err != nil {
		t.Fatalf("HashOpaqueSecret: %v", err)
	}
	noPrefixHash := strings.TrimPrefix(hash, "$")

	tests := []struct {
		name    string
		encoded string
	}{
		{
			name:    "non-empty prefix before aura",
			encoded: "prefix$" + noPrefixHash,
		},
		{
			name:    "duplicate param",
			encoded: strings.Replace(hash, "m=65536,t=1,p=4", "m=65536,t=1,p=4,p=4", 1),
		},
		{
			name:    "extra param",
			encoded: strings.Replace(hash, "m=65536,t=1,p=4", "m=65536,t=1,p=4,x=9", 1),
		},
		{
			name:    "short salt",
			encoded: replaceArgon2Field(hash, 5, base64.RawStdEncoding.EncodeToString(make([]byte, 15))),
		},
		{
			name:    "short hash",
			encoded: replaceArgon2Field(hash, 6, base64.RawStdEncoding.EncodeToString(make([]byte, 31))),
		},
		{
			name:    "non-canonical salt",
			encoded: replaceArgon2Field(hash, 5, nonCanonicalRawStdBase64(make([]byte, 16))),
		},
		{
			name:    "non-canonical hash",
			encoded: replaceArgon2Field(hash, 6, nonCanonicalRawStdBase64(make([]byte, 32))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := parseArgon2id(tt.encoded); err == nil {
				t.Fatal("parseArgon2id succeeded, want error")
			}
			if VerifyOpaqueSecret("secret", tt.encoded) {
				t.Fatal("VerifyOpaqueSecret succeeded, want false")
			}
		})
	}
}

func replaceArgon2Field(encoded string, field int, value string) string {
	parts := strings.Split(encoded, "$")
	parts[field] = value
	return strings.Join(parts, "$")
}

func nonCanonicalRawStdBase64(raw []byte) string {
	encoded := base64.RawStdEncoding.EncodeToString(raw)
	return encoded[:len(encoded)-1] + "B"
}
