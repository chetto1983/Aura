// Package canonicaljson is a deterministic serializer for Aura-internal hashing.
// NOT RFC-8785 (no cross-system crypto-signature consumer → float-canonicalization
// minefield avoided).
//
// It feeds the dedup fingerprint sha256(name + canonical_json(args)) and is reused
// by Phase 4 (conversation hash) + Phase 11 (skill content_hash). Determinism here
// is load-bearing: an unstable key order or a silent 1==1.0 coercion would merge
// distinct tool calls under one hash, suppressing real work as a false dedup hit.
//
// Three rules, per D-08:
//   - map keys are emitted sorted by Go byte order (one documented order; there is
//     no cross-impl consumer, so RFC-8785 string/float canonicalization is skipped);
//   - numbers are preserved as literal text via json.Number (UseNumber), so 1 and 1.0
//     stay DISTINCT and never round-trip through float64;
//   - un-canonicalizable input (NaN, Inf, func, chan, ...) is strict-rejected with an
//     error and nil bytes — never silently coerced.
package canonicaljson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Marshal returns the deterministic canonical JSON encoding of v. It returns a
// non-nil error and nil bytes for any un-canonicalizable input (NaN, Inf, func,
// chan, or a value json.Marshal cannot encode). Numeric literals are preserved
// exactly (1 != 1.0) via json.Number.
func Marshal(v any) ([]byte, error) {
	normalized, err := normalize(v)
	if err != nil {
		return nil, fmt.Errorf("canonicalize value: %w", err)
	}
	var buf bytes.Buffer
	if err := encode(&buf, normalized); err != nil {
		return nil, fmt.Errorf("encode canonical json: %w", err)
	}
	return buf.Bytes(), nil
}

// normalize converts v into a tree of canonical Go values (string, json.Number,
// bool, nil, []any, map[string]any) by round-tripping through encoding/json with
// UseNumber. This both validates the input (NaN/Inf/func/chan surface as errors)
// and preserves numeric literals as text. json.RawMessage / json.Number inputs are
// decoded in place so callers may pass pre-parsed fragments.
func normalize(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// encode writes the canonical form of a normalized value. The input is always one
// of the json-decoded shapes: nil, bool, string, json.Number, []any, map[string]any.
func encode(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case string:
		return encodeString(buf, t)
	case json.Number:
		return encodeNumber(buf, t)
	case []any:
		return encodeArray(buf, t)
	case map[string]any:
		return encodeObject(buf, t)
	default:
		return fmt.Errorf("unsupported canonical type %T", v)
	}
}

func encodeString(buf *bytes.Buffer, s string) error {
	enc, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal string: %w", err)
	}
	buf.Write(enc)
	return nil
}

// encodeNumber emits the literal number text unchanged so 1 and 1.0 stay distinct.
// It rejects non-finite floats defensively; UseNumber decoding never yields NaN/Inf
// as a json.Number, but a literal that parses to a non-finite float must not pass.
func encodeNumber(buf *bytes.Buffer, n json.Number) error {
	// ParseFloat returns ±Inf (with ErrRange) on overflow literals such as 1e999, so
	// inspect the parsed value directly rather than gating on err == nil. Underflow
	// (1e-999 → 0 with ErrRange) is finite and correctly passes through.
	if f, _ := strconv.ParseFloat(n.String(), 64); math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("non-finite number %q", n.String())
	}
	buf.WriteString(n.String())
	return nil
}

func encodeArray(buf *bytes.Buffer, arr []any) error {
	buf.WriteByte('[')
	for i, e := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encode(buf, e); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

// encodeObject emits keys sorted by Go byte order — the one documented order that
// makes the output hash-stable regardless of input map iteration order.
func encodeObject(buf *bytes.Buffer, obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeString(buf, k); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := encode(buf, obj[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}
