package webauth

import (
	"strings"
	"testing"

	authulaconfig "github.com/Authula/authula/config"
	authularepos "github.com/Authula/authula/core/repositories"
)

// Ciphertexts produced by Authula v1.50.1 (NewConfig(WithSecret(raw)) then
// NewCryptoTokenRepository(cfg.Secret).Encrypt): the JWKS private keys and TOTP secrets
// stored under 1.50.1. authulaKey must open them through the v1.51 config pipeline, for a
// padded secret too, because v1.50.1 hashed the string exactly as received.
const goldenSecret = "3f1c9a7be0d24c6e8a5b1f0d9c7e3a2b4d6f8e0a1c3b5d7f9e2a4c6b8d0f1e3a"

var authulaV150Vectors = []struct {
	name, secret, ciphertext, plaintext string
}{
	{"plain", goldenSecret,
		"Z-HttJsKgnGO52K5FsXkpsoAwXo0LugtNRf6j63RL3o_1NFS1UgBdQ73-wh9Tzv5qWfa03fdJnPaVGTPi5c-juDALHS4tjRZwFH4faC1j2nf3wMjE0nKeXuaLo_pMdQ",
		"-----BEGIN PRIVATE KEY-----JWKS-or-TOTP-plaintext-plain"},
	{"trailing newline", goldenSecret + "\n",
		"-aaHfkDHK09e-Bnn5Saq0OEa3hU9KowXsjtqtvLzBlg285ACRus-BUb31jx3eQEeCWXVw_WDX64onjZjyG4knXhO2eFwSLPMWBpbvjjVYaN4DElI-ToBb7UZeIlNqsjdRg",
		"-----BEGIN PRIVATE KEY-----JWKS-or-TOTP-plaintext-newline"},
}

func TestAuthulaKeyDecryptsV150Ciphertext(t *testing.T) {
	for _, v := range authulaV150Vectors {
		t.Run(v.name, func(t *testing.T) {
			cfg := authulaconfig.NewConfig(authulaconfig.WithSecret(authulaKey(v.secret)))
			repo, err := authularepos.NewCryptoTokenRepository(cfg.EncryptionKeys())
			if err != nil {
				t.Fatalf("NewCryptoTokenRepository: %v", err)
			}
			got, err := repo.Decrypt(v.ciphertext)
			if err != nil {
				t.Fatalf("v1.50.1 ciphertext no longer decrypts: %v", err)
			}
			if got != v.plaintext {
				t.Fatalf("plaintext = %q, want %q", got, v.plaintext)
			}
		})
	}
}

func TestAuthulaKeyDistinguishesSecrets(t *testing.T) {
	if authulaKey(goldenSecret) == authulaKey(strings.Repeat("cd", 32)) {
		t.Fatal("distinct secrets must give distinct keys")
	}
}
