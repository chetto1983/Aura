package wiki

import (
	"log/slog"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// AliasIndex is an in-memory map from normalized alias strings to canonical
// page slugs. It is maintained by the Store: warmAliasIndex rebuilds it at
// boot from disk (pages sorted by mtime, oldest first so earlier writes win
// on same-length alias conflicts), and WritePage / DeletePage refresh single-
// slug deltas incrementally.
//
// Normalization: trim + lowercase + collapse internal whitespace + NFC unicode.
// Conflict policy (per same normalized key):
//   - Longer original alias wins over shorter (more specific → more specific route).
//   - Same-length tie: first-indexed wins (mtime-ordered at boot, call-order at runtime);
//     the loser is logged at Warn level.
type AliasIndex struct {
	mu sync.RWMutex
	m  map[string]aliasEntry // normalized key → canonical entry
}

type aliasEntry struct {
	slug    string
	origLen int // len(trimmed original alias), for longest-wins comparison
}

// NewAliasIndex returns an empty, ready-to-use AliasIndex.
func NewAliasIndex() *AliasIndex {
	return &AliasIndex{m: make(map[string]aliasEntry)}
}

// normalizeAlias applies trim + lowercase + internal-whitespace collapse + NFC.
func normalizeAlias(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, unicode.IsSpace)
	s = strings.Join(fields, " ")
	return norm.NFC.String(s)
}

// Set replaces the alias entries for slug with the given aliases. Existing
// entries for other slugs that happen to claim the same normalized key are
// subject to conflict policy. Empty aliases are rejected silently.
//
// Call Set(slug, nil) or Set(slug, []string{}) to remove a slug's aliases.
func (a *AliasIndex) Set(slug string, aliases []string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Remove all existing entries for this slug before re-adding.
	for k, v := range a.m {
		if v.slug == slug {
			delete(a.m, k)
		}
	}

	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		key := normalizeAlias(alias)
		origLen := len(alias)

		existing, ok := a.m[key]
		switch {
		case !ok:
			a.m[key] = aliasEntry{slug: slug, origLen: origLen}
		case origLen > existing.origLen:
			// Longer original alias wins (more specific).
			a.m[key] = aliasEntry{slug: slug, origLen: origLen}
		case origLen == existing.origLen && existing.slug != slug:
			// Same-length conflict: first-write wins; log the loser.
			slog.Warn("alias conflict: same-length alias already claimed",
				"alias", alias,
				"normalized_key", key,
				"winner", existing.slug,
				"loser", slug,
			)
		// origLen < existing.origLen: shorter alias loses — keep existing, no action.
		}
	}
}

// Remove purges all alias entries for slug from the index.
func (a *AliasIndex) Remove(slug string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range a.m {
		if v.slug == slug {
			delete(a.m, k)
		}
	}
}

// Resolve returns the canonical slug for the given alias string (normalized)
// and whether a match was found. O(1) exact lookup after normalization.
func (a *AliasIndex) Resolve(alias string) (slug string, ok bool) {
	key := normalizeAlias(alias)
	a.mu.RLock()
	defer a.mu.RUnlock()
	e, ok := a.m[key]
	if !ok {
		return "", false
	}
	return e.slug, true
}

// Len returns the number of alias keys currently in the index.
func (a *AliasIndex) Len() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.m)
}
