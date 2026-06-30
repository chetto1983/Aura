package neostore

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the leaf-helper tests under goleak (Shared Pattern B). These are
// pure functions with no goroutines, so the assertion is structural insurance: a
// future helper that spawns a worker must reap it.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// The three source copies (reasoningstore.hashText, toolselectstore.hashQuery,
// activelearn.hashText for HashText; the asString/asFloats twins) were byte-identical
// before this extraction. This table is the canonical-home characterization (D-09/D-10):
// it encodes the union of inputs all three copies handled, and the pre-existing store
// tests (reasoningstore/toolselectstore) are the post-move parity gate.

func TestHashText_DeterministicSHA256Hex(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Known sha256 → lowercase hex (locks the algorithm + encoding).
		{"empty", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"single", "a", "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HashText(tc.in); got != tc.want {
				t.Errorf("HashText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// Determinism: same input → same hash; distinct inputs → distinct hashes.
	unicode := "ünïcödé 🚀 — café"
	long := ""
	for i := 0; i < 4096; i++ {
		long += "x"
	}
	for _, in := range []string{"", "a", unicode, long} {
		first := HashText(in)
		second := HashText(in)
		if first != second {
			t.Errorf("HashText(%q) not deterministic", in)
		}
		if len(first) != 64 {
			t.Errorf("HashText(%q) length = %d, want 64 hex chars", in, len(first))
		}
	}
	if HashText("a") == HashText("b") {
		t.Error("HashText must differ for distinct inputs")
	}
}

func TestAsString_NonStringFallsBackToEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hello", "hello"},
		{"empty string", "", ""},
		{"int", 42, ""},
		{"int64", int64(7), ""},
		{"float", 3.14, ""},
		{"nil", nil, ""},
		{"slice", []any{"x"}, ""},
		{"map", map[string]any{"k": "v"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AsString(tc.in); got != tc.want {
				t.Errorf("AsString(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAsFloats_AllTransportFormsAndNilVsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		want    []float64
		wantNil bool // distinguishes a nil return from a non-nil zero-length slice
	}{
		// APOC JSON string (the live read-query transport).
		{name: "apoc json string", in: "[-0.02,0.5,0.25]", want: []float64{-0.02, 0.5, 0.25}},
		// Malformed JSON string → nil (abort, never a partial vector).
		{name: "malformed json string", in: "{not json", wantNil: true},
		// Empty JSON array string → non-nil zero-length slice (NOT nil).
		{name: "empty json array string", in: "[]", want: []float64{}},
		// Raw []any of float64.
		{name: "float64 list", in: []any{float64(1), float64(2)}, want: []float64{1, 2}},
		// Raw []any with int64 + int elements → coerced to float64.
		{name: "mixed-int list", in: []any{int64(1), int(2), 3.0}, want: []float64{1, 2, 3}},
		// A non-numeric element aborts the parse → nil.
		{name: "list with string element", in: []any{1.0, "nope"}, wantNil: true},
		// Empty []any → non-nil zero-length slice (NOT nil).
		{name: "empty any list", in: []any{}, want: []float64{}},
		// nil → nil (the default/unsupported top-level branch).
		{name: "nil", in: nil, wantNil: true},
		// Unsupported top-level type → nil.
		{name: "int top-level", in: 42, wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AsFloats(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Errorf("AsFloats(%#v) = %#v, want nil", tc.in, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("AsFloats(%#v) = nil, want non-nil %#v", tc.in, tc.want)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("AsFloats(%#v) len = %d, want %d", tc.in, len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("AsFloats(%#v)[%d] = %v, want %v", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}
