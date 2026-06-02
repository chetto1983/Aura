// Package prompt owns the single wire-Request assembly chokepoint (PromptBuilder)
// and the content fingerprint (PrefixHash) that the cache-invariant gate reads.
// It lives under internal/agent (not internal/llm) because the manifest it
// assembles already imports internal/llm — putting the builder in internal/llm
// would close an import cycle (D-01a).
package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/chetto1983/aura/internal/canonicaljson"
	"github.com/chetto1983/aura/internal/llm"
)

// PrefixHash returns a stable SHA-256 hex fingerprint over the messages at the
// given index set, in the order the indices are supplied (D-06a). It is a
// CONTENT fingerprint for the cache-stability invariant, NOT a security
// primitive (ASVS V6: SHA-256 here detects accidental prefix drift, it is not a
// keyed MAC and guards no trust boundary).
//
// Indices that fall at or beyond len(msgs) are skipped, so {0,1,2} is
// forward-compatible: it equals {0} while only messages[0] is present today and
// transparently extends once Slice 10 (messages[1]) and Slice 11e (messages[2])
// land. Each present message is serialized with the deterministic
// canonicaljson.Marshal (sorted keys, json.Number) — two logically-equal
// messages always hash the same regardless of field iteration order.
func PrefixHash(msgs []llm.Message, indices []int) (string, error) {
	h := sha256.New()
	for _, i := range indices {
		if i < 0 || i >= len(msgs) {
			continue
		}
		canon, err := canonicaljson.Marshal(msgs[i])
		if err != nil {
			return "", fmt.Errorf("prefix hash: canonicalize message %d: %w", i, err)
		}
		h.Write(canon)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
