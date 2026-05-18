package tokenjuice_test

import (
	"testing"

	"github.com/aura/aura/internal/tokenjuice"
)

func TestCompactPassthrough(t *testing.T) {
	in := tokenjuice.Input{Stdout: "hello"}
	r := tokenjuice.Compact(in, tokenjuice.Options{})
	if r.Applied {
		t.Errorf("Applied: want false, got true")
	}
	if r.InlineText != "hello" {
		t.Errorf("InlineText: want %q, got %q", "hello", r.InlineText)
	}
}
