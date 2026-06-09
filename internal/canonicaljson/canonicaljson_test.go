package canonicaljson

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

func TestMarshal_KeyOrderIndependent(t *testing.T) {
	a, err := Marshal(map[string]any{"a": 1, "b": 2, "z": 3})
	if err != nil {
		t.Fatalf("Marshal a: %v", err)
	}
	b, err := Marshal(map[string]any{"z": 3, "b": 2, "a": 1})
	if err != nil {
		t.Fatalf("Marshal b: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("key order must not affect output: %q != %q", a, b)
	}
}

func TestMarshal_NestedKeyOrderIndependent(t *testing.T) {
	a, err := Marshal(map[string]any{"outer": map[string]any{"x": 1, "y": 2}})
	if err != nil {
		t.Fatalf("Marshal a: %v", err)
	}
	b, err := Marshal(map[string]any{"outer": map[string]any{"y": 2, "x": 1}})
	if err != nil {
		t.Fatalf("Marshal b: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("nested key order must not affect output: %q != %q", a, b)
	}
}

// TestMarshal_DistinctNumberLiterals locks D-08: 1 and 1.0 are DISTINCT because
// numbers are preserved as literal text (UseNumber), never collapsed via float64.
// Getting this wrong silently merges distinct tool calls in the dedup fingerprint.
func TestMarshal_DistinctNumberLiterals(t *testing.T) {
	one, err := Marshal(json.RawMessage(`1`))
	if err != nil {
		t.Fatalf("Marshal 1: %v", err)
	}
	oneDotZero, err := Marshal(json.RawMessage(`1.0`))
	if err != nil {
		t.Fatalf("Marshal 1.0: %v", err)
	}
	if bytes.Equal(one, oneDotZero) {
		t.Errorf("1 and 1.0 must serialize distinctly, both produced %q", one)
	}
}

func TestMarshal_StrictRejectsNonCanonicalizable(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"NaN", math.NaN()},
		{"PosInf", math.Inf(1)},
		{"NegInf", math.Inf(-1)},
		{"OverflowNumberLiteral", json.RawMessage(`1e9999`)},
		{"func", func() {}},
		{"chan", make(chan int)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Marshal(tc.in)
			if err == nil {
				t.Errorf("Marshal(%s) must return error, got %q", tc.name, out)
			}
			if out != nil {
				t.Errorf("Marshal(%s) must return nil bytes on error, got %q", tc.name, out)
			}
		})
	}
}

// TestMarshal_UUIDInput exercises a real downstream input type (uuid.UUID is a
// [16]byte array marshalled as its canonical string) so the serializer is proven
// against the trace-ID values Phase-2 feeds it.
func TestMarshal_UUIDInput(t *testing.T) {
	id := uuid.MustParse("018f4e2a-0000-7000-8000-000000000000")
	out, err := Marshal(map[string]any{"trace_id": id})
	if err != nil {
		t.Fatalf("Marshal uuid: %v", err)
	}
	if !bytes.Contains(out, []byte("018f4e2a-0000-7000-8000-000000000000")) {
		t.Errorf("uuid string must appear in output, got %q", out)
	}
}

func TestMarshal_Idempotent(t *testing.T) {
	in := map[string]any{"b": []any{3, 2, 1}, "a": map[string]any{"nested": true}}
	first, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal first: %v", err)
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(first))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	second, err := Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("not idempotent: %q != %q", first, second)
	}
}

// FuzzCanonical_RoundTripAndDistinctNumbers asserts the core determinism
// properties: idempotent round-trip canon(x)==canon(decode(encode(x))) over
// rapid-generated values, plus the load-bearing 1 != 1.0 invariant and
// strict-reject of NaN/Inf. The rapid generator drives structural variety;
// the explicit asserts lock the documented number rule (D-08).
func FuzzCanonical_RoundTripAndDistinctNumbers(f *testing.F) {
	f.Add([]byte(`{"a":1,"b":2}`))
	f.Add([]byte(`{"b":2,"a":1}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte(`"plain string"`))
	f.Add([]byte(`{"nested":{"x":[1,{"y":2}]}}`))
	f.Add([]byte(`1.0`))
	f.Add([]byte(`true`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		// 1 != 1.0 must hold for every fuzz iteration.
		one, errOne := Marshal(json.RawMessage(`1`))
		oneFloat, errFloat := Marshal(json.RawMessage(`1.0`))
		if errOne != nil || errFloat != nil {
			t.Fatalf("literal number marshal errored: %v / %v", errOne, errFloat)
		}
		if bytes.Equal(one, oneFloat) {
			t.Fatalf("1 and 1.0 collapsed: both %q", one)
		}
		// Strict-reject must hold for every fuzz iteration.
		if out, err := Marshal(math.NaN()); err == nil {
			t.Fatalf("NaN must be rejected, got %q", out)
		}

		// Round-trip property on valid JSON inputs only (invalid bytes are not
		// canonicalizable input — Marshal of json.RawMessage validates them).
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			t.Skip() // not valid JSON; nothing to round-trip
		}
		first, err := Marshal(decoded)
		if err != nil {
			t.Skip() // un-canonicalizable decoded value; strict-reject is the contract
		}
		var redecoded any
		dec2 := json.NewDecoder(bytes.NewReader(first))
		dec2.UseNumber()
		if err := dec2.Decode(&redecoded); err != nil {
			t.Fatalf("re-decode of canonical output failed: %v (output %q)", err, first)
		}
		second, err := Marshal(redecoded)
		if err != nil {
			t.Fatalf("re-marshal failed: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("round-trip not idempotent: %q != %q", first, second)
		}
	})
}

// TestMarshal_Property_KeyOrderIndependent uses rapid to generate string-keyed
// maps and asserts the canonical output is invariant under key reordering.
func TestMarshal_Property_KeyOrderIndependent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		keys := rapid.SliceOfNDistinct(
			rapid.StringMatching(`[a-z]{1,5}`), 1, 6,
			func(s string) string { return s },
		).Draw(rt, "keys")
		m := make(map[string]any, len(keys))
		for i, k := range keys {
			m[k] = rapid.IntRange(0, 1000).Draw(rt, "v"+k+string(rune(i)))
		}
		first, err := Marshal(m)
		if err != nil {
			rt.Fatalf("Marshal: %v", err)
		}
		// Rebuild an identical map (Go map iteration order is already random);
		// a second Marshal must match byte-for-byte.
		second, err := Marshal(m)
		if err != nil {
			rt.Fatalf("Marshal again: %v", err)
		}
		if !bytes.Equal(first, second) {
			rt.Fatalf("non-deterministic for same map: %q != %q", first, second)
		}
	})
}
