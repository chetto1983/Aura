package arcadedb

import (
	"strconv"
	"strings"
)

// SourcePredicate narrows IndexedDocument rows to a scope: the objects keys names and every
// descendant of the folders prefixes names, joined by OR. It binds its values into params and
// returns "" for an empty scope. The search and the library list share it, so the two can never
// disagree about what a scope admits.
func SourcePredicate(keys, prefixes []string, params map[string]any) string {
	if len(keys) == 0 && len(prefixes) == 0 {
		return ""
	}
	parts := make([]string, 0, 1+len(prefixes))
	if len(keys) > 0 {
		parts = append(parts, "source_key IN :source_keys")
		params["source_keys"] = keys
	}
	for index, prefix := range prefixes {
		name := "source_prefix_" + strconv.Itoa(index)
		parts = append(parts, "source_key LIKE :"+name)
		params[name] = escapeLikePrefix(prefix)
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// likeLiteral escapes ArcadeDB's LIKE wildcards, % and ?, and the escape itself.
var likeLiteral = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `?`, `\?`)

// EscapeLike keeps text literal inside a LIKE pattern: a caller adds the wildcards it means.
func EscapeLike(text string) string { return likeLiteral.Replace(text) }

// escapeLikePrefix keeps Garage keys containing ArcadeDB LIKE wildcards literal. The only
// wildcard this function introduces is the final %, which means "descendant of folder".
func escapeLikePrefix(prefix string) string { return EscapeLike(prefix) + "%" }
