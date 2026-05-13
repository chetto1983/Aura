package toolindex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/aura/aura/internal/llm"
)

// ContentHash returns a stable lowercase-hex SHA-256 over a canonical JSON
// representation of (name, description, input_schema, embed_text, embed_model).
//
// The embed_model field is part of the hash on purpose: switching the
// embedder (e.g. from embeddinggemma to a future model) MUST invalidate
// every previously-cached vector because the vectors live in a different
// space. Without this field, a cache hit on an unchanged hash would return
// a vector incompatible with the new index.
//
// embedText is the exact bytes that will be sent to the Embedder if a
// re-embed is required. Hashing it (instead of recomputing it from name +
// description + schema) means a change to the text-rendering function
// itself (e.g. a fix to searchableToolEmbeddingText) automatically bumps
// every tool's hash and forces a full re-embed on the next reconcile.
func ContentHash(def llm.ToolDefinition, embedText, embedModel string) (string, error) {
	canonical, err := canonicalJSON(map[string]any{
		"name":         def.Name,
		"description":  def.Description,
		"input_schema": def.Parameters,
		"embed_text":   embedText,
		"embed_model":  embedModel,
	})
	if err != nil {
		return "", fmt.Errorf("toolindex: hash marshal: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalJSON marshals a Go value with every map sorted by key
// (recursively). This is the standard "canonical JSON" recipe — same bytes
// regardless of Go map iteration order, regardless of the order keys
// appeared in the source.
//
// We can't rely on json.Marshal alone because the Go encoder iterates maps
// in a randomized order. A canonical pre-pass into sorted ordered values
// gives us bytes-determinism across processes.
func canonicalJSON(v any) ([]byte, error) {
	normalized := canonicalize(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(normalized); err != nil {
		return nil, err
	}
	// json.Encoder appends a trailing newline; trim it so the byte
	// representation is exactly what a non-streaming Marshal would produce.
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}

// canonicalize walks the input recursively and rewrites every map[string]any
// into a sortedMap (which marshals in sorted-key order). Slices are walked
// in place. Scalars pass through. Other map types (e.g. map[string]string)
// are converted to map[string]any first so the sort treatment applies
// uniformly. nil values are emitted as JSON null.
func canonicalize(v any) any {
	switch m := v.(type) {
	case nil:
		return nil
	case map[string]any:
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(sortedMap, 0, len(keys))
		for _, k := range keys {
			out = append(out, sortedKV{Key: k, Value: canonicalize(m[k])})
		}
		return out
	case map[string]string:
		// Promote to map[string]any so the recursion handles it.
		promoted := make(map[string]any, len(m))
		for k, val := range m {
			promoted[k] = val
		}
		return canonicalize(promoted)
	case []any:
		out := make([]any, len(m))
		for i, item := range m {
			out[i] = canonicalize(item)
		}
		return out
	default:
		return v
	}
}

// sortedMap renders as a JSON object with keys in slice order (set by
// canonicalize). Implemented via json.Marshaler to bypass the default
// map encoder.
type sortedMap []sortedKV

type sortedKV struct {
	Key   string
	Value any
}

func (s sortedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range s {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(kv.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
