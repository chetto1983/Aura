// Package neostore holds the canonical Neo4j store-helper leaf shared by the
// oracle-labeled example stores (reasoningstore, toolselectstore) and the
// label-agnostic active-learning worker (activelearn). Before this extraction the
// GraphClient Cypher seam and the HashText/AsString/AsFloats coercers were copied
// byte-for-byte across all three packages (reasoningstore.hashText /
// toolselectstore.hashQuery / activelearn.hashText were the same sha256→hex; the
// asString/asFloats column coercers were verbatim twins). Keeping ONE copy prevents
// the divergence that would let one store decode an APOC embedding differently from
// another (D-06 leaf extraction, QUAL-03).
//
// It is a stdlib-only leaf (context, crypto/sha256, encoding/hex, encoding/json) so
// every store depends on the seam with no Neo4j concrete and no import cycle.
package neostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// GraphClient is the narrow Cypher seam the example stores need. *knowledge.Client
// satisfies it structurally (Read/Write decode rows to []map[string]any); keeping
// the interface here lets each store depend on the seam, not the Neo4j concrete.
type GraphClient interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
	Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}

// HashText returns the lowercase-hex sha256 of text. It is the content-MERGE key
// for the example nodes (re-labeling the same text is an idempotent UPSERT), NOT a
// password hash — bare sha256 is deliberate here (V6 note).
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// AsString coerces a Cypher scalar column to string, mapping any non-string value
// (int, nil, list, map, ...) to "" so a malformed column degrades to a skipped row
// rather than a panic.
func AsString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// AsFloats coerces a Cypher embedding column to []float64. The load queries return
// the embedding as an APOC JSON string ("[-0.02,...]") because the mcp-neo4j-cypher
// read tool returns NULL for list-valued columns; a raw []any list (other transports
// / tests) is also accepted. A malformed JSON string or a list element that is not a
// number aborts the parse with nil — never a partial vector. The nil-vs-empty
// distinction is load-bearing: "[]" and []any{} both decode to a non-nil zero-length
// slice, while a malformed string, an unsupported top-level type, and nil return nil.
func AsFloats(v any) []float64 {
	switch t := v.(type) {
	case string:
		var out []float64
		if json.Unmarshal([]byte(t), &out) != nil {
			return nil
		}
		return out
	case []any:
		out := make([]float64, 0, len(t))
		for _, x := range t {
			switch n := x.(type) {
			case float64:
				out = append(out, n)
			case int64:
				out = append(out, float64(n))
			case int:
				out = append(out, float64(n))
			default:
				return nil
			}
		}
		return out
	default:
		return nil
	}
}
