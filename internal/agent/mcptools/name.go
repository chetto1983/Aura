package mcptools

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

const (
	nsDelimiter    = "__"
	maxToolNameLen = 64
	hashLen        = 12
)

// sanitizeName restricts s to the LLM tool-name char class [a-zA-Z0-9_-], replacing
// every other rune with '_'. An empty result becomes "_" so a name is never blank.
// D-07 keeps '-' (Codex maps it to '_'); Aura is intentionally more permissive.
func sanitizeName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// hashSuffix returns a deterministic "_<12hex>" disambiguation suffix for rawIdentity.
// SHA-1 here is a non-cryptographic collision disambiguator (Codex parity), NOT a
// security control: a forced collision still hits Mount's all-or-nothing refusal.
func hashSuffix(rawIdentity string) string {
	h := sha1.Sum([]byte(rawIdentity))
	return "_" + hex.EncodeToString(h[:])[:hashLen]
}

// namespacedName builds the model-facing <namespace>__<tool> name: both sides are
// sanitized, joined with the double-underscore delimiter, and the whole capped at
// maxToolNameLen by truncating the tool part and appending a deterministic hash
// suffix when it would otherwise overflow. Cross-name collision disambiguation
// across a Mount batch is layered on top of this by the caller.
func namespacedName(namespace, tool string) string {
	prefix := sanitizeName(namespace) + nsDelimiter
	base := prefix + sanitizeName(tool)
	if len(base) <= maxToolNameLen {
		return base
	}
	suffix := hashSuffix(namespace + "\x00" + tool)
	keep := maxToolNameLen - len(prefix) - len(suffix)
	if keep < 0 {
		keep = 0
	}
	return prefix + sanitizeName(tool)[:keep] + suffix
}
