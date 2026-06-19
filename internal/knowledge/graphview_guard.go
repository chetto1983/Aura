// Belt-and-suspenders write-verb guard (Phase 27, GRAPH-04 / T-27-02). The intent
// compilers only ever emit read-Cypher, but every dispatched query is re-checked
// here before it reaches the reader — defense in depth on an authenticated REST
// endpoint, on top of the read-tx enforced at the mcp/bolt layer. String literals
// are stripped first so a write verb living in NODE-PROPERTY DATA (e.g. a note that
// says "please DELETE this") never trips the guard.
package knowledge

import (
	"errors"
	"regexp"
)

// errWriteVerb is the sanitized rejection — it never echoes the offending Cypher
// (T-27-05 info-disclosure: no query/DSN in the error).
var errWriteVerb = errors.New("graphview: refusing non-read query (write verb detected)")

// writeVerb matches a standalone write keyword OR a write verb inside a CALL{...}
// subquery. Case-insensitive. Applied AFTER stripStringLiterals.
var writeVerb = regexp.MustCompile(`(?i)\b(CREATE|MERGE|SET|DELETE|REMOVE|DROP|FOREACH)\b|CALL\s*\{[^}]*\b(CREATE|MERGE|SET|DELETE|REMOVE)\b`)

// assertReadOnly rejects any Cypher that contains a write verb (CREATE/MERGE/SET/
// DELETE/REMOVE/DROP/FOREACH or a write inside CALL{...}). Quoted string spans are
// removed first so verbs in data do not false-positive.
func assertReadOnly(cypher string) error {
	if writeVerb.MatchString(stripStringLiterals(cypher)) {
		return errWriteVerb
	}
	return nil
}

// stripStringLiterals replaces the contents of single- and double-quoted spans
// with empty strings, honoring Cypher's backslash escapes, so write verbs that
// live inside property-data strings are not seen by the guard. Backtick-quoted
// identifiers are left intact (a label/identifier cannot be a write verb keyword
// in a way the regex would catch, and stripping them could hide real structure).
func stripStringLiterals(s string) string {
	out := make([]byte, 0, len(s))
	var quote byte // 0 = outside a string; '\'' or '"' = inside
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			// inside a string literal: drop the char, track escapes + the close
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
				out = append(out, ' ') // collapse the whole span to a single space
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		out = append(out, c)
	}
	return string(out)
}
