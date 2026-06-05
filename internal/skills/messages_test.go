package skills

import (
	"strings"
	"testing"
)

// TestRenderAlwaysBlockByteStable proves two calls over the same skill set produce
// byte-identical output (CAP-04 byte-stability the messages[0] prefix cache depends
// on) and that the bodies are emitted in alphabetical order regardless of input
// order.
func TestRenderAlwaysBlockByteStable(t *testing.T) {
	in := []Skill{
		{Name: "zebra", Always: true, Body: "Z body"},
		{Name: "alpha", Always: true, Body: "A body"},
		{Name: "not-always", Always: false, Body: "ignored"},
		{Name: "mango", Always: true, Body: "M body"},
	}
	b1, p1 := RenderAlwaysBlock(in)
	b2, p2 := RenderAlwaysBlock(in)
	if !p1 || !p2 {
		t.Fatal("present should be true when always skills exist")
	}
	if b1 != b2 {
		t.Errorf("RenderAlwaysBlock not byte-stable across calls:\n%q\nvs\n%q", b1, b2)
	}
	// Alphabetical ordering: A before M before Z; the non-always skill is excluded.
	idxA := strings.Index(b1, "A body")
	idxM := strings.Index(b1, "M body")
	idxZ := strings.Index(b1, "Z body")
	if idxA < 0 || idxM < 0 || idxZ < 0 {
		t.Fatalf("missing an always body in the block:\n%s", b1)
	}
	if idxA >= idxM || idxM >= idxZ {
		t.Errorf("bodies not alphabetical: A=%d M=%d Z=%d\n%s", idxA, idxM, idxZ, b1)
	}
	if strings.Contains(b1, "ignored") {
		t.Errorf("non-always skill body leaked into the block:\n%s", b1)
	}
	if !strings.HasPrefix(b1, alwaysBlockHeader) {
		t.Errorf("block must lead with the frozen header, got:\n%s", b1)
	}
}

// TestRenderAlwaysBlockEmptyWhenNoAlways proves an absent always:true skill yields an
// empty block + present=false, so the caller omits the messages[1] turn entirely.
func TestRenderAlwaysBlockEmptyWhenNoAlways(t *testing.T) {
	out, present := RenderAlwaysBlock([]Skill{
		{Name: "a", Always: false, Body: "x"},
		{Name: "b", Always: false, Body: "y"},
	})
	if present || out != "" {
		t.Errorf("no always skills: want ('', false), got (%q, %v)", out, present)
	}
	// Nil/empty input is also empty.
	if out2, p2 := RenderAlwaysBlock(nil); p2 || out2 != "" {
		t.Errorf("nil input: want ('', false), got (%q, %v)", out2, p2)
	}
}

// TestRenderAlwaysBlockOrderIndependent proves the output does not depend on input
// slice order (the alphabetical sort is the single source of order).
func TestRenderAlwaysBlockOrderIndependent(t *testing.T) {
	a := []Skill{{Name: "x", Always: true, Body: "BX"}, {Name: "y", Always: true, Body: "BY"}}
	b := []Skill{{Name: "y", Always: true, Body: "BY"}, {Name: "x", Always: true, Body: "BX"}}
	out1, _ := RenderAlwaysBlock(a)
	out2, _ := RenderAlwaysBlock(b)
	if out1 != out2 {
		t.Errorf("output depends on input order:\n%q\nvs\n%q", out1, out2)
	}
}
