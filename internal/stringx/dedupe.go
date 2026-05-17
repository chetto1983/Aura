// Package stringx holds small string-shaped helpers shared across packages.
//
// It deliberately stays narrow — utilities here must be domain-neutral. Helpers
// that touch business semantics (case-insensitive tool name matching, locale
// formatting, etc.) belong in the package that owns the semantic.
package stringx

import (
	"slices"
	"strings"
)

// AppendUnique appends each addition to values when it is non-empty (after
// TrimSpace) and not already present. The comparison is case-sensitive and
// trims surrounding whitespace from the additions only — values is treated as
// authoritative and never mutated except by the implicit append.
//
// Use this when a caller needs to merge string sets where order matters and
// case sensitivity is intentional. For case-insensitive deduplication
// (e.g. lower-cased tool name allowlists) keep the bespoke helper at the
// call site; the trade-off between an extra parameter and a tiny inline
// loop is not worth a wider API.
func AppendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" || slices.Contains(values, addition) {
			continue
		}
		values = append(values, addition)
	}
	return values
}

// NormalizeBaseURL trims surrounding whitespace and any trailing slash from a
// REST endpoint base URL. Callers append their own paths beginning with "/"
// (e.g. "/v1/embeddings"), so a base ending in "/" produced "//v1/embeddings"
// which some servers reject. Empty input returns "".
//
// This is a pure string operation; net/http normalization rules don't apply
// here. Callers that need full URL parsing should use net/url.Parse, not this.
func NormalizeBaseURL(rawURL string) string {
	return strings.TrimRight(strings.TrimSpace(rawURL), "/")
}
