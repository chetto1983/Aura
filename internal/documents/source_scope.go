package documents

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// SourceScopeKind says whether a Garage mention addresses one object or every indexed
// object below one folder prefix. It is a retrieval constraint, not an access grant: the
// caller identity still selects the tenant database and bucket.
type SourceScopeKind string

const (
	// SourceScopeFile addresses one Garage object key.
	SourceScopeFile SourceScopeKind = "file"
	// SourceScopeFolder addresses every indexed descendant of one Garage folder.
	SourceScopeFolder SourceScopeKind = "folder"

	// MaxSourceScopes bounds the strict run envelope and the predicate it creates.
	MaxSourceScopes = 16
	// MaxSourceScopePathRunes is deliberately independent from byte length so a Unicode
	// Garage name pays the same bound as an ASCII one.
	MaxSourceScopePathRunes = 2048
)

// SourceScope is the server-normalized form of one operator-selected Garage mention.
// Path has no leading/trailing slash; folder semantics are carried by Kind.
type SourceScope struct {
	Kind SourceScopeKind `json:"kind"`
	Path string          `json:"path"`
}

// NormalizeSourceScopes validates, canonicalizes, de-duplicates and sorts a turn scope.
// Traversal-shaped input is refused rather than silently cleaned: the visible @ token must
// name the same object the retrieval predicate addresses.
func NormalizeSourceScopes(scopes []SourceScope) ([]SourceScope, error) {
	if len(scopes) > MaxSourceScopes {
		return nil, fmt.Errorf("documents: source scope exceeds %d entries", MaxSourceScopes)
	}
	normalized := make([]SourceScope, 0, len(scopes))
	seen := make(map[SourceScope]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.Kind != SourceScopeFile && scope.Kind != SourceScopeFolder {
			return nil, fmt.Errorf("documents: unsupported source scope kind %q", scope.Kind)
		}
		raw := strings.ReplaceAll(strings.TrimSpace(scope.Path), `\`, "/")
		if strings.ContainsRune(raw, '\x00') {
			return nil, fmt.Errorf("documents: source scope contains NUL")
		}
		for segment := range strings.SplitSeq(raw, "/") {
			if segment == "." || segment == ".." {
				return nil, fmt.Errorf("documents: source scope contains a traversal segment")
			}
		}
		cleaned := strings.Trim(path.Clean("/"+raw), "/")
		if cleaned == "" || cleaned == "." {
			return nil, fmt.Errorf("documents: source scope path is required")
		}
		if utf8.RuneCountInString(cleaned) > MaxSourceScopePathRunes {
			return nil, fmt.Errorf(
				"documents: source scope path exceeds %d characters", MaxSourceScopePathRunes,
			)
		}
		canonical := SourceScope{Kind: scope.Kind, Path: cleaned}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Path == normalized[j].Path {
			return normalized[i].Kind < normalized[j].Kind
		}
		return normalized[i].Path < normalized[j].Path
	})
	return normalized, nil
}

type sourceScopesContextKey struct{}

// WithSourceScopes attaches a validated run scope to every tool call made by that turn.
func WithSourceScopes(ctx context.Context, scopes []SourceScope) context.Context {
	return context.WithValue(ctx, sourceScopesContextKey{}, append([]SourceScope(nil), scopes...))
}

// SourceScopesFromContext returns an isolated copy so a tool cannot mutate sibling calls.
func SourceScopesFromContext(ctx context.Context) []SourceScope {
	scopes, _ := ctx.Value(sourceScopesContextKey{}).([]SourceScope)
	return append([]SourceScope(nil), scopes...)
}

// ArcadeSourceFilters maps the public Garage scope to the index's two predicate families.
func ArcadeSourceFilters(scopes []SourceScope) (keys, prefixes []string) {
	for _, scope := range scopes {
		if scope.Kind == SourceScopeFile {
			keys = append(keys, scope.Path)
		} else {
			prefixes = append(prefixes, scope.Path+"/")
		}
	}
	return keys, prefixes
}
