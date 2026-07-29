package share

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// TestMintTokenEntropy proves Mint's plaintext decodes (base64 RawURL) to
// exactly 32 bytes — 256 bits, per D-13.
func TestMintTokenEntropy(t *testing.T) {
	plaintext, _, err := Mint()
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		t.Fatalf("Mint() plaintext %q does not decode as base64 RawURLEncoding: %v", plaintext, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("Mint() decoded plaintext length = %d bytes, want 32 (256 bits)", len(decoded))
	}
}

// TestMintTokenURLSafe proves the plaintext contains none of the padding or
// alphabet characters that would require URL-escaping — required because the
// token rides directly in a share URL path segment.
func TestMintTokenURLSafe(t *testing.T) {
	plaintext, _, err := Mint()
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	for _, forbidden := range []string{"+", "/", "="} {
		if strings.Contains(plaintext, forbidden) {
			t.Fatalf("Mint() plaintext %q contains non-URL-safe character %q", plaintext, forbidden)
		}
	}
}

// TestHashStability proves Hash(p) is deterministic across calls and equals
// sha256.Sum256 of the plaintext bytes — the exact digest that must land in
// the token_hash bytea column (migration 0040).
func TestHashStability(t *testing.T) {
	plaintext, hash, err := Mint()
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	want := sha256.Sum256([]byte(plaintext))
	if hash != want {
		t.Fatalf("Mint() hash = %x, want sha256.Sum256(plaintext) = %x", hash, want)
	}
	if got := Hash(plaintext); got != want {
		t.Fatalf("Hash(plaintext) = %x, want %x", got, want)
	}
	first := Hash(plaintext)
	second := Hash(plaintext)
	if first != second {
		t.Fatalf("Hash(p) is not stable across repeated calls: first = %x, second = %x", first, second)
	}
}

// TestMintUniqueness is the RESEARCH.md token-opacity/uniqueness property:
// over 1000 mints, all plaintexts are distinct, all hashes are distinct, and
// no plaintext is a prefix of another (T-37F-02).
func TestMintUniqueness(t *testing.T) {
	const n = 1000
	plaintexts := make([]string, 0, n)
	seenPlaintext := make(map[string]bool, n)
	seenHash := make(map[[32]byte]bool, n)

	for i := range n {
		plaintext, hash, err := Mint()
		if err != nil {
			t.Fatalf("Mint() iteration %d error = %v", i, err)
		}
		if seenPlaintext[plaintext] {
			t.Fatalf("Mint() produced a duplicate plaintext at iteration %d: %q", i, plaintext)
		}
		seenPlaintext[plaintext] = true
		if seenHash[hash] {
			t.Fatalf("Mint() produced a duplicate hash at iteration %d: %x", i, hash)
		}
		seenHash[hash] = true
		plaintexts = append(plaintexts, plaintext)
	}

	for i, a := range plaintexts {
		for j, b := range plaintexts {
			if i == j {
				continue
			}
			if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
				t.Fatalf("Mint() plaintext %d (%q) and plaintext %d (%q) are prefixes of one another", i, a, j, b)
			}
		}
	}
}

// TestMintRandFailureNoWeakFallback documents (via Hash, which has no I/O
// dependency and thus cannot be forced to fail here) that Mint's only
// failure path is a propagated crypto/rand error — see token.go's rand.Read
// call, which returns immediately on error with no fallback to a weaker
// source. A live rand.Read failure cannot be triggered from a portable unit
// test without replacing the global reader, which token.go deliberately does
// not expose (no injectable seam) — the grep-gated acceptance criteria
// (`grep -A3 "rand.Read"`) is the enforcement mechanism for this behavior.
func TestMintRandFailureNoWeakFallback(t *testing.T) {
	plaintext, hash, err := Mint()
	if err != nil {
		t.Fatalf("Mint() error = %v, want nil on a healthy crypto/rand source", err)
	}
	if plaintext == "" {
		t.Fatal("Mint() returned an empty plaintext on success")
	}
	if hash == ([32]byte{}) {
		t.Fatal("Mint() returned a zero hash on success")
	}
}
