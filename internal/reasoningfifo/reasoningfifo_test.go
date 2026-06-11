package reasoningfifo

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFIFOTailKeepsNewestUnderCap(t *testing.T) {
	t.Parallel()
	f := New(5)
	f.Push("abc")
	f.Push("defgh") // total 8 runes, cap 5 → keep the last 5: "defgh"
	if got, want := f.String(), "defgh"; got != want {
		t.Fatalf("window = %q, want %q (front-evicted oldest)", got, want)
	}
	if got, want := f.Len(), 5; got != want {
		t.Fatalf("Len = %d, want %d", got, want)
	}
}

func TestFIFOUnderCapKeepsAll(t *testing.T) {
	t.Parallel()
	f := New(4096)
	f.Push("ciao ")
	f.Push("mondo")
	if got, want := f.String(), "ciao mondo"; got != want {
		t.Fatalf("window = %q, want %q", got, want)
	}
}

// Multibyte runes must never be split at the cap — the whole point of a rune buffer.
func TestFIFORuneSafeAtBoundary(t *testing.T) {
	t.Parallel()
	f := New(3)
	// Each emoji + each accented char is one rune but multiple bytes; pushing 5 runes
	// into a 3-rune window must keep exactly the last 3 runes, byte-intact.
	f.Push("àé😀")
	f.Push("ç🚀") // sequence: à é 😀 ç 🚀 → keep last 3: 😀 ç 🚀
	got := f.String()
	if !utf8.ValidString(got) {
		t.Fatalf("window is not valid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("window contains the replacement rune (split multibyte): %q", got)
	}
	if want := "😀ç🚀"; got != want {
		t.Fatalf("window = %q, want %q", got, want)
	}
	if got, want := utf8.RuneCountInString(got), 3; got != want {
		t.Fatalf("rune count = %d, want %d", got, want)
	}
}

func TestFIFOResetClears(t *testing.T) {
	t.Parallel()
	f := New(8)
	f.Push("stale reasoning")
	f.Reset()
	if got := f.String(); got != "" {
		t.Fatalf("after Reset window = %q, want empty", got)
	}
	// Reuse after reset works and re-caps independently.
	f.Push("fresh")
	if got, want := f.String(), "fresh"; got != want {
		t.Fatalf("post-reset window = %q, want %q", got, want)
	}
}

func TestFIFOZeroCapRetainsNothing(t *testing.T) {
	t.Parallel()
	f := New(0)
	f.Push("anything")
	if got := f.String(); got != "" {
		t.Fatalf("zero-cap window = %q, want empty (disabled FIFO)", got)
	}
}

func TestFIFONegativeCapIsZero(t *testing.T) {
	t.Parallel()
	f := New(-10)
	f.Push("x")
	if f.Len() != 0 {
		t.Fatalf("negative cap should retain nothing, Len = %d", f.Len())
	}
}

func TestFIFONilReceiverIsNoOp(t *testing.T) {
	t.Parallel()
	var f *FIFO
	f.Push("x") // must not panic
	f.Reset()   // must not panic
	if got := f.String(); got != "" {
		t.Fatalf("nil FIFO String = %q, want empty", got)
	}
	if f.Len() != 0 {
		t.Fatalf("nil FIFO Len = %d, want 0", f.Len())
	}
}

func TestFIFOEmptyPushNoOp(t *testing.T) {
	t.Parallel()
	f := New(4)
	f.Push("")
	if f.Len() != 0 {
		t.Fatalf("empty push retained %d runes, want 0", f.Len())
	}
}

// Repeated over-cap pushes must keep the window bounded (no backing-array growth past
// the cap) and always show the most-recent runes.
func TestFIFOStableUnderManyPushes(t *testing.T) {
	t.Parallel()
	f := New(10)
	for range 1000 {
		f.Push("0123456789")
	}
	if got, want := f.Len(), 10; got != want {
		t.Fatalf("Len = %d, want %d (window must stay capped)", got, want)
	}
	if got, want := f.String(), "0123456789"; got != want {
		t.Fatalf("window = %q, want %q", got, want)
	}
}
